#
# Copyright 2017 the original author or authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

"""
Fully simulated OLT adapter.
"""

import arrow
import grpc
import structlog
from google.protobuf.empty_pb2 import Empty
from google.protobuf.json_format import MessageToDict
from scapy.layers.inet import Raw
import json
from google.protobuf.message import Message
from grpc._channel import _Rendezvous
from scapy.layers.l2 import Ether, Dot1Q
from simplejson import dumps
from twisted.internet import reactor
from twisted.internet.defer import inlineCallbacks, returnValue
from twisted.internet.task import LoopingCall

from pyvoltha.adapters.common.frameio.frameio import BpfProgramFilter, hexify
from pyvoltha.common.utils.asleep import asleep
from pyvoltha.common.utils.registry import registry
from pyvoltha.adapters.iadapter import OltAdapter
from pyvoltha.adapters.kafka.kafka_proxy import get_kafka_proxy
from voltha_protos.ponsim_pb2_grpc import PonSimStub
from voltha_protos.common_pb2 import OperStatus, ConnectStatus
from voltha_protos.inter_container_pb2 import SwitchCapability, PortCapability, \
    InterAdapterMessageType, InterAdapterResponseBody
from voltha_protos.device_pb2 import Port, PmConfig, PmConfigs
from voltha_protos.events_pb2 import KpiEvent, KpiEventType, MetricValuePairs
from voltha_protos.logical_device_pb2 import LogicalPort
from voltha_protos.openflow_13_pb2 import OFPPS_LIVE, OFPPF_FIBER, \
    OFPPF_1GB_FD, \
    OFPC_GROUP_STATS, OFPC_PORT_STATS, OFPC_TABLE_STATS, OFPC_FLOW_STATS, \
    ofp_switch_features, ofp_desc
from voltha_protos.openflow_13_pb2 import ofp_port
from voltha_protos.ponsim_pb2 import FlowTable, PonSimFrame, PonSimMetricsRequest

log = structlog.get_logger()

PACKET_IN_VLAN = 4000
is_inband_frame = BpfProgramFilter('(ether[14:2] & 0xfff) = 0x{:03x}'.format(
    PACKET_IN_VLAN))


def mac_str_to_tuple(mac):
    return tuple(int(d, 16) for d in mac.split(':'))


class AdapterPmMetrics:
    def __init__(self, device):
        self.pm_names = {'tx_64_pkts', 'tx_65_127_pkts', 'tx_128_255_pkts',
                         'tx_256_511_pkts', 'tx_512_1023_pkts',
                         'tx_1024_1518_pkts', 'tx_1519_9k_pkts',
                         'rx_64_pkts', 'rx_65_127_pkts',
                         'rx_128_255_pkts', 'rx_256_511_pkts',
                         'rx_512_1023_pkts', 'rx_1024_1518_pkts',
                         'rx_1519_9k_pkts'}
        self.device = device
        self.id = device.id
        self.name = 'ponsim_olt'
        self.default_freq = 150
        self.grouped = False
        self.freq_override = False
        self.pon_metrics_config = dict()
        self.nni_metrics_config = dict()
        self.lc = None
        for m in self.pm_names:
            self.pon_metrics_config[m] = PmConfig(name=m,
                                                  type=PmConfig.COUNTER,
                                                  enabled=True)
            self.nni_metrics_config[m] = PmConfig(name=m,
                                                  type=PmConfig.COUNTER,
                                                  enabled=True)

    def update(self, pm_config):
        if self.default_freq != pm_config.default_freq:
            # Update the callback to the new frequency.
            self.default_freq = pm_config.default_freq
            self.lc.stop()
            self.lc.start(interval=self.default_freq / 10)
        for m in pm_config.metrics:
            self.pon_metrics_config[m.name].enabled = m.enabled
            self.nni_metrics_config[m.name].enabled = m.enabled

    def make_proto(self):
        pm_config = PmConfigs(
            id=self.id,
            default_freq=self.default_freq,
            grouped=False,
            freq_override=False)
        for m in sorted(self.pon_metrics_config):
            pm = self.pon_metrics_config[m]  # Either will do they're the same
            pm_config.metrics.extend([PmConfig(name=pm.name,
                                               type=pm.type,
                                               enabled=pm.enabled)])
        return pm_config

    def collect_port_metrics(self, channel):
        rtrn_port_metrics = dict()
        stub = PonSimStub(channel)
        stats = stub.GetStats(Empty())
        rtrn_port_metrics['pon'] = self.extract_pon_metrics(stats)
        rtrn_port_metrics['nni'] = self.extract_nni_metrics(stats)
        return rtrn_port_metrics

    def extract_pon_metrics(self, stats):
        rtrn_pon_metrics = dict()
        for m in stats.metrics:
            if m.port_name == "pon":
                for p in m.packets:
                    if self.pon_metrics_config[p.name].enabled:
                        rtrn_pon_metrics[p.name] = p.value
                return rtrn_pon_metrics

    def extract_nni_metrics(self, stats):
        rtrn_pon_metrics = dict()
        for m in stats.metrics:
            if m.port_name == "nni":
                for p in m.packets:
                    if self.pon_metrics_config[p.name].enabled:
                        rtrn_pon_metrics[p.name] = p.value
                return rtrn_pon_metrics

    def start_collector(self, callback):
        log.info("starting-pm-collection", device_name=self.name,
                 device_id=self.device.id)
        prefix = 'voltha.{}.{}'.format(self.name, self.device.id)
        self.lc = LoopingCall(callback, self.device.id, prefix)
        self.lc.start(interval=self.default_freq / 10)

    def stop_collector(self):
        log.info("stopping-pm-collection", device_name=self.name,
                 device_id=self.device.id)
        self.lc.stop()


class AdapterAlarms:
    def __init__(self, adapter, device):
        self.adapter = adapter
        self.device = device
        self.lc = None

    # TODO: Implement code to send to kafka cluster directly instead of
    # going through the voltha core.
    def send_alarm(self, context_data, alarm_data):
        log.debug("send-alarm-not-implemented")
        return


class PonSimOltAdapter(OltAdapter):
    def __init__(self, core_proxy, adapter_proxy, config):
        super(PonSimOltAdapter, self).__init__(core_proxy=core_proxy,
                                               adapter_proxy=adapter_proxy,
                                               config=config,
                                               device_handler_class=PonSimOltHandler,
                                               name='ponsim_olt',
                                               vendor='Voltha project',
                                               version='0.4',
                                               device_type='ponsim_olt',
                                               accepts_bulk_flow_update=True,
                                               accepts_add_remove_flow_updates=False)

    def update_pm_config(self, device, pm_config):
        log.info("adapter-update-pm-config", device=device,
                 pm_config=pm_config)
        handler = self.devices_handlers[device.id]
        handler.update_pm_config(device, pm_config)


class PonSimOltHandler(object):
    def __init__(self, adapter, device_id):
        self.adapter = adapter
        self.core_proxy = adapter.core_proxy
        self.adapter_proxy = adapter.adapter_proxy
        self.device_id = device_id
        self.log = structlog.get_logger(device_id=device_id)
        self.channel = None
        self.io_port = None
        self.logical_device_id = None
        self.nni_port = None
        self.ofp_port_no = None
        self.interface = registry('main').get_args().interface
        self.pm_metrics = None
        self.alarms = None
        self.frames = None

    @inlineCallbacks
    def get_channel(self):
        if self.channel is None:
            try:
                device = yield self.core_proxy.get_device(self.device_id)
                self.log.info('device-info', device=device,
                              host_port=device.host_and_port)
                self.channel = grpc.insecure_channel(device.host_and_port)
            except Exception as e:
                log.exception("ponsim-connection-failure", e=e)

        # returnValue(self.channel)

    def close_channel(self):
        if self.channel is None:
            self.log.info('grpc-channel-already-closed')
            return
        else:
            if self.frames is not None:
                self.frames.cancel()
                self.frames = None
                self.log.info('cancelled-grpc-frame-stream')

            self.channel.unsubscribe(lambda *args: None)
            self.channel = None

            self.log.info('grpc-channel-closed')

    @inlineCallbacks
    def _get_nni_port(self):
        ports = yield self.core_proxy.get_ports(self.device_id,
                                                Port.ETHERNET_NNI)
        returnValue(ports)

    @inlineCallbacks
    def activate(self, device):
        try:
            self.log.info('activating')

            if not device.host_and_port:
                device.oper_status = OperStatus.FAILED
                device.reason = 'No host_and_port field provided'
                self.core_proxy.device_update(device)
                return

            yield self.get_channel()
            stub = PonSimStub(self.channel)
            info = stub.GetDeviceInfo(Empty())
            log.info('got-info', info=info, device_id=device.id)
            self.ofp_port_no = info.nni_port

            device.root = True
            device.vendor = 'ponsim'
            device.model = 'n/a'
            device.serial_number = device.host_and_port
            device.mac_address = "AA:BB:CC:DD:EE:FF"
            yield self.core_proxy.device_update(device)

            # Now set the initial PM configuration for this device
            self.pm_metrics = AdapterPmMetrics(device)
            pm_config = self.pm_metrics.make_proto()
            log.info("initial-pm-config", pm_config=pm_config)
            yield self.core_proxy.device_pm_config_update(pm_config, init=True)

            # Setup alarm handler
            self.alarms = AdapterAlarms(self.adapter, device)

            nni_port = Port(
                port_no=info.nni_port,
                label='nni-'+ str(info.nni_port),
                type=Port.ETHERNET_NNI,
                oper_status=OperStatus.ACTIVE
            )
            self.nni_port = nni_port
            yield self.core_proxy.port_created(device.id, nni_port)
            yield self.core_proxy.port_created(device.id, Port(
                port_no=1,
                label='pon-1',
                type=Port.PON_OLT,
                oper_status=OperStatus.ACTIVE
            ))

            yield self.core_proxy.device_state_update(device.id,
                                                      connect_status=ConnectStatus.REACHABLE,
                                                      oper_status=OperStatus.ACTIVE)

            # register ONUS
            self.log.info('onu-found', onus=info.onus, len=len(info.onus))
            for onu in info.onus:
                vlan_id = onu.uni_port
                yield self.core_proxy.child_device_detected(
                    parent_device_id=device.id,
                    parent_port_no=1,
                    child_device_type='ponsim_onu',
                    channel_id=vlan_id,
                    serial_number=onu.serial_number,
                )

            self.log.info('starting-frame-grpc-stream')
            reactor.callInThread(self.rcv_grpc)
            self.log.info('started-frame-grpc-stream')

            # Start collecting stats from the device after a brief pause
            self.start_kpi_collection(device.id)
        except Exception as e:
            log.exception("Exception-activating", e=e)

    def get_ofp_device_info(self, device):
        return SwitchCapability(
            desc=ofp_desc(
                hw_desc='ponsim pon',
                sw_desc='ponsim pon',
                serial_num=device.serial_number,
                mfr_desc="VOLTHA Project",
                dp_desc='n/a'
            ),
            switch_features=ofp_switch_features(
                n_buffers=256,  # TODO fake for now
                n_tables=2,  # TODO ditto
                capabilities=(  # TODO and ditto
                        OFPC_FLOW_STATS
                        | OFPC_TABLE_STATS
                        | OFPC_PORT_STATS
                        | OFPC_GROUP_STATS
                )
            )
        )

    def get_ofp_port_info(self, device, port_no):
        # Since the adapter created the device port then it has the reference of the port to
        # return the capability.   TODO:  Do a lookup on the NNI port number and return the
        # appropriate attributes
        self.log.info('get_ofp_port_info', port_no=port_no,
                      info=self.ofp_port_no, device_id=device.id)
        cap = OFPPF_1GB_FD | OFPPF_FIBER
        return PortCapability(
            port=LogicalPort(
                ofp_port=ofp_port(
                    hw_addr=mac_str_to_tuple(
                        'AA:BB:CC:DD:EE:%02x' % port_no),
                    config=0,
                    state=OFPPS_LIVE,
                    curr=cap,
                    advertised=cap,
                    peer=cap,
                    curr_speed=OFPPF_1GB_FD,
                    max_speed=OFPPF_1GB_FD
                ),
                device_id=device.id,
                device_port_no=port_no
            )
        )

    # TODO - change for core 2.0
    def reconcile(self, device):
        self.log.info('reconciling-OLT-device')

    @inlineCallbacks
    def _rcv_frame(self, frame):
        pkt = Ether(frame)

        if pkt.haslayer(Dot1Q):
            outer_shim = pkt.getlayer(Dot1Q)

            if isinstance(outer_shim.payload, Dot1Q):
                inner_shim = outer_shim.payload
                cvid = inner_shim.vlan
                popped_frame = (
                        Ether(src=pkt.src, dst=pkt.dst, type=inner_shim.type) /
                        inner_shim.payload
                )
                self.log.info('sending-packet-in',device_id=self.device_id, port=cvid)
                yield self.core_proxy.send_packet_in(device_id=self.device_id,
                                               port=cvid,
                                               packet=str(popped_frame))
            elif pkt.haslayer(Raw):
                raw_data = json.loads(pkt.getlayer(Raw).load)
                self.alarms.send_alarm(self, raw_data)

    @inlineCallbacks
    def rcv_grpc(self):
        """
        This call establishes a GRPC stream to receive frames.
        """
        yield self.get_channel()
        stub = PonSimStub(self.channel)

        # Attempt to establish a grpc stream with the remote ponsim service
        self.frames = stub.ReceiveFrames(Empty())

        self.log.info('start-receiving-grpc-frames')

        try:
            for frame in self.frames:
                self.log.info('received-grpc-frame',
                              frame_len=len(frame.payload))
                self._rcv_frame(frame.payload)

        except _Rendezvous, e:
            log.warn('grpc-connection-lost', message=e.message)

        self.log.info('stopped-receiving-grpc-frames')

    @inlineCallbacks
    def update_flow_table(self, flows):
        yield self.get_channel()
        stub = PonSimStub(self.channel)

        self.log.info('pushing-olt-flow-table')
        stub.UpdateFlowTable(FlowTable(
            port=0,
            flows=flows
        ))
        self.log.info('success')

    def remove_from_flow_table(self, flows):
        self.log.debug('remove-from-flow-table', flows=flows)
        # TODO: Update PONSIM code to accept incremental flow changes
        # Once completed, the accepts_add_remove_flow_updates for this
        # device type can be set to True

    def add_to_flow_table(self, flows):
        self.log.debug('add-to-flow-table', flows=flows)
        # TODO: Update PONSIM code to accept incremental flow changes
        # Once completed, the accepts_add_remove_flow_updates for this
        # device type can be set to True

    def update_pm_config(self, device, pm_config):
        log.info("handler-update-pm-config", device=device,
                 pm_config=pm_config)
        self.pm_metrics.update(pm_config)

    def send_proxied_message(self, proxy_address, msg):
        self.log.info('sending-proxied-message')
        if isinstance(msg, FlowTable):
            stub = PonSimStub(self.get_channel())
            self.log.info('pushing-onu-flow-table', port=msg.port)
            res = stub.UpdateFlowTable(msg)
            self.core_proxy.receive_proxied_message(proxy_address, res)

    @inlineCallbacks
    def process_inter_adapter_message(self, request):
        self.log.info('process-inter-adapter-message', msg=request)
        try:
            if request.header.type == InterAdapterMessageType.FLOW_REQUEST:
                f = FlowTable()
                if request.body:
                    request.body.Unpack(f)
                    stub = PonSimStub(self.channel)
                    self.log.info('pushing-onu-flow-table')
                    res = stub.UpdateFlowTable(f)
                    # Send response back
                    reply = InterAdapterResponseBody()
                    reply.status = True
                    self.log.info('sending-response-back', reply=reply)
                    yield self.adapter_proxy.send_inter_adapter_message(
                        msg=reply,
                        type=InterAdapterMessageType.FLOW_RESPONSE,
                        from_adapter=self.adapter.name,
                        to_adapter=request.header.from_topic,
                        to_device_id=request.header.to_device_id,
                        message_id=request.header.id
                    )
            elif request.header.type == InterAdapterMessageType.METRICS_REQUEST:
                m = PonSimMetricsRequest()
                if request.body:
                    request.body.Unpack(m)
                    stub = PonSimStub(self.channel)
                    self.log.info('proxying onu stats request', port=m.port)
                    res = stub.GetStats(m)
                    # Send response back
                    reply = InterAdapterResponseBody()
                    reply.status = True
                    reply.body.Pack(res)
                    self.log.info('sending-response-back', reply=reply)
                    yield self.adapter_proxy.send_inter_adapter_message(
                        msg=reply,
                        type=InterAdapterMessageType.METRICS_RESPONSE,
                        from_adapter=self.adapter.name,
                        to_adapter=request.header.from_topic,
                        to_device_id=request.header.to_device_id,
                        message_id=request.header.id
                    )
        except Exception as e:
            self.log.exception("error-processing-inter-adapter-message", e=e)

    def packet_out(self, egress_port, msg):
        self.log.info('sending-packet-out', egress_port=egress_port,
                      msg=hexify(msg))
        try:
            pkt = Ether(msg)
            out_pkt = pkt
            if egress_port != self.nni_port.port_no:
                # don't do the vlan manipulation for the NNI port, vlans are already correct
                out_pkt = (
                        Ether(src=pkt.src, dst=pkt.dst) /
                        Dot1Q(vlan=egress_port, type=pkt.type) /
                        pkt.payload
                )

            # TODO need better way of mapping logical ports to PON ports
            out_port = self.nni_port.port_no if egress_port == self.nni_port.port_no else 1

            # send over grpc stream
            stub = PonSimStub(self.channel)
            frame = PonSimFrame(id=self.device_id, payload=str(out_pkt),
                                out_port=out_port)
            stub.SendFrame(frame)
        except Exception as e:
            self.log.exception("error-processing-packet-out", e=e)


    @inlineCallbacks
    def reboot(self):
        self.log.info('rebooting', device_id=self.device_id)

        yield self.core_proxy.device_state_update(self.device_id,
                                                  connect_status=ConnectStatus.UNREACHABLE)

        # Update the child devices connect state to UNREACHABLE
        yield self.core_proxy.children_state_update(self.device_id,
                                                    connect_status=ConnectStatus.UNREACHABLE)

        # Sleep 10 secs, simulating a reboot
        # TODO: send alert and clear alert after the reboot
        yield asleep(10)

        # Change the connection status back to REACHABLE.  With a
        # real OLT the connection state must be the actual state
        yield self.core_proxy.device_state_update(self.device_id,
                                                  connect_status=ConnectStatus.REACHABLE)

        # Update the child devices connect state to REACHABLE
        yield self.core_proxy.children_state_update(self.device_id,
                                                    connect_status=ConnectStatus.REACHABLE)

        self.log.info('rebooted', device_id=self.device_id)

    def self_test_device(self, device):
        """
        This is called to Self a device based on a NBI call.
        :param device: A Voltha.Device object.
        :return: Will return result of self test
        """
        log.info('self-test-device', device=device.id)
        raise NotImplementedError()

    @inlineCallbacks
    def disable(self):
        self.log.info('disabling', device_id=self.device_id)

        self.stop_kpi_collection()

        # Update the operational status to UNKNOWN and connection status to UNREACHABLE
        yield self.core_proxy.device_state_update(self.device_id,
                                                  oper_status=OperStatus.UNKNOWN,
                                                  connect_status=ConnectStatus.UNREACHABLE)

        self.close_channel()
        self.log.info('disabled-grpc-channel')

        self.stop_kpi_collection()

        # TODO:
        # 1) Remove all flows from the device
        # 2) Remove the device from ponsim

        self.log.info('disabled', device_id=self.device_id)

    @inlineCallbacks
    def reenable(self):
        self.log.info('re-enabling', device_id=self.device_id)

        # Set the ofp_port_no and nni_port in case we bypassed the reconcile
        # process if the device was in DISABLED state on voltha restart
        if not self.ofp_port_no and not self.nni_port:
            yield self.get_channel()
            stub = PonSimStub(self.channel)
            info = stub.GetDeviceInfo(Empty())
            log.info('got-info', info=info)
            self.ofp_port_no = info.nni_port
            ports = yield self._get_nni_port()
            # For ponsim, we are using only 1 NNI port
            if ports.items:
                self.nni_port = ports.items[0]

        # Update the state of the NNI port
        yield self.core_proxy.port_state_update(self.device_id,
                                                port_type=Port.ETHERNET_NNI,
                                                port_no=self.ofp_port_no,
                                                oper_status=OperStatus.ACTIVE)

        # Update the state of the PON port
        yield self.core_proxy.port_state_update(self.device_id,
                                                port_type=Port.PON_OLT,
                                                port_no=1,
                                                oper_status=OperStatus.ACTIVE)

        # Set the operational state of the device to ACTIVE and connect status to REACHABLE
        yield self.core_proxy.device_state_update(self.device_id,
                                                  connect_status=ConnectStatus.REACHABLE,
                                                  oper_status=OperStatus.ACTIVE)

        # TODO: establish frame grpc-stream
        # yield reactor.callInThread(self.rcv_grpc)

        self.start_kpi_collection(self.device_id)

        self.log.info('re-enabled', device_id=self.device_id)

    def delete(self):
        self.log.info('deleting', device_id=self.device_id)

        self.close_channel()
        self.log.info('disabled-grpc-channel')

        # TODO:
        # 1) Remove all flows from the device
        # 2) Remove the device from ponsim

        self.log.info('deleted', device_id=self.device_id)

    def start_kpi_collection(self, device_id):

        kafka_cluster_proxy = get_kafka_proxy()

        def _collect(device_id, prefix):

            try:
                # Step 1: gather metrics from device
                port_metrics = \
                    self.pm_metrics.collect_port_metrics(self.channel)

                # Step 2: prepare the KpiEvent for submission
                # we can time-stamp them here (or could use time derived from OLT
                ts = arrow.utcnow().timestamp
                kpi_event = KpiEvent(
                    type=KpiEventType.slice,
                    ts=ts,
                    prefixes={
                        # OLT NNI port
                        prefix + '.nni': MetricValuePairs(
                            metrics=port_metrics['nni']),
                        # OLT PON port
                        prefix + '.pon': MetricValuePairs(
                            metrics=port_metrics['pon'])
                    }
                )

                # Step 3: submit directly to the kafka bus
                if kafka_cluster_proxy:
                    if isinstance(kpi_event, Message):
                        kpi_event = dumps(MessageToDict(kpi_event, True, True))
                    kafka_cluster_proxy.send_message("voltha.kpis", kpi_event)

            except Exception as e:
                log.exception('failed-to-submit-kpis', e=e)

        self.pm_metrics.start_collector(_collect)

    def stop_kpi_collection(self):
        self.pm_metrics.stop_collector()
