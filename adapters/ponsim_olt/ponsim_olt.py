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
from uuid import uuid4

import arrow
import adapters.common.openflow.utils as fd
import grpc
import structlog
from scapy.layers.l2 import Ether, Dot1Q
from twisted.internet import reactor
from twisted.internet.defer import inlineCallbacks, returnValue
from grpc._channel import _Rendezvous

from adapters.common.frameio.frameio import BpfProgramFilter, hexify
from adapters.common.utils.asleep import asleep
from twisted.internet.task import LoopingCall
from adapters.iadapter import OltAdapter
from adapters.protos import third_party
from adapters.protos import openflow_13_pb2 as ofp
from adapters.protos import ponsim_pb2
from adapters.protos.common_pb2 import OperStatus, ConnectStatus, AdminState
from adapters.protos.device_pb2 import Port, PmConfig, PmConfigs
from adapters.protos.events_pb2 import KpiEvent, KpiEventType, MetricValuePairs
from google.protobuf.empty_pb2 import Empty
from adapters.protos.logical_device_pb2 import LogicalPort, LogicalDevice
from adapters.protos.openflow_13_pb2 import OFPPS_LIVE, OFPPF_FIBER, \
    OFPPF_1GB_FD, \
    OFPC_GROUP_STATS, OFPC_PORT_STATS, OFPC_TABLE_STATS, OFPC_FLOW_STATS, \
    ofp_switch_features, ofp_desc
from adapters.protos.openflow_13_pb2 import ofp_port
from adapters.protos.ponsim_pb2 import FlowTable, PonSimFrame
from adapters.protos.core_adapter_pb2 import SwitchCapability, PortCapability
from adapters.common.utils.registry import registry
from adapters.kafka.kafka_proxy import get_kafka_proxy
from simplejson import dumps
from google.protobuf.json_format import MessageToDict
from google.protobuf.message import Message


_ = third_party
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
        stub = ponsim_pb2.PonSimStub(channel)
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

    def send_alarm(self, context_data, alarm_data):
        try:
            current_context = {}
            for key, value in context_data.__dict__.items():
                current_context[key] = str(value)

            alarm_event = self.adapter.adapter_agent.create_alarm(
                resource_id=self.device.id,
                description="{}.{} - {}".format(self.adapter.name,
                                                self.device.id,
                                                alarm_data[
                                                    'description']) if 'description' in alarm_data else None,
                type=alarm_data['type'] if 'type' in alarm_data else None,
                category=alarm_data[
                    'category'] if 'category' in alarm_data else None,
                severity=alarm_data[
                    'severity'] if 'severity' in alarm_data else None,
                state=alarm_data['state'] if 'state' in alarm_data else None,
                raised_ts=alarm_data['ts'] if 'ts' in alarm_data else 0,
                context=current_context
            )

            self.adapter.adapter_agent.submit_alarm(self.device.id,
                                                    alarm_event)

        except Exception as e:
            log.exception('failed-to-send-alarm', e=e)


class PonSimOltAdapter(OltAdapter):
    def __init__(self, adapter_agent, config):
        super(PonSimOltAdapter, self).__init__(adapter_agent=adapter_agent,
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
        self.adapter_agent = adapter.adapter_agent
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
                device = yield self.adapter_agent.get_device(self.device_id)
                self.log.info('device-info', device=device, host_port=device.host_and_port)
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
        ports = yield self.adapter_agent.get_ports(self.device_id, Port.ETHERNET_NNI)
        returnValue(ports)

    @inlineCallbacks
    def activate(self, device):
        try:
            self.log.info('activating')

            if not device.host_and_port:
                device.oper_status = OperStatus.FAILED
                device.reason = 'No host_and_port field provided'
                self.adapter_agent.device_update(device)
                return

            yield self.get_channel()
            stub = ponsim_pb2.PonSimStub(self.channel)
            info = stub.GetDeviceInfo(Empty())
            log.info('got-info', info=info, device_id=device.id)
            self.ofp_port_no = info.nni_port

            device.root = True
            device.vendor = 'ponsim'
            device.model = 'n/a'
            device.serial_number = device.host_and_port
            device.mac_address = "AA:BB:CC:DD:EE:FF"
            # device.connect_status = ConnectStatus.REACHABLE
            yield self.adapter_agent.device_update(device)

            # Now set the initial PM configuration for this device
            self.pm_metrics = AdapterPmMetrics(device)
            pm_config = self.pm_metrics.make_proto()
            log.info("initial-pm-config", pm_config=pm_config)
            self.adapter_agent.device_pm_config_update(pm_config, init=True)

            # Setup alarm handler
            self.alarms = AdapterAlarms(self.adapter, device)

            nni_port = Port(
                port_no=info.nni_port,
                label='NNI facing Ethernet port',
                type=Port.ETHERNET_NNI,
                # admin_state=AdminState.ENABLED,
                oper_status=OperStatus.ACTIVE
            )
            self.nni_port = nni_port
            yield self.adapter_agent.port_created(device.id, nni_port)
            yield self.adapter_agent.port_created(device.id, Port(
                port_no=1,
                label='PON port',
                type=Port.PON_OLT,
                # admin_state=AdminState.ENABLED,
                oper_status=OperStatus.ACTIVE
            ))

            yield self.adapter_agent.device_state_update(device.id, connect_status=ConnectStatus.REACHABLE, oper_status=OperStatus.ACTIVE)

            # register ONUS
            self.log.info('onu-found', onus=info.onus, len=len(info.onus))
            for onu in info.onus:
                vlan_id = onu.uni_port
                yield self.adapter_agent.child_device_detected(
                    parent_device_id=device.id,
                    parent_port_no=1,
                    child_device_type='ponsim_onu',
                    channel_id=vlan_id,
                )

            self.log.info('starting-frame-grpc-stream')
            reactor.callInThread(self.rcv_grpc)
            self.log.info('started-frame-grpc-stream')

            # TODO
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
        self.log.info('get_ofp_port_info', port_no=port_no, info=self.ofp_port_no, device_id=device.id)
        cap = OFPPF_1GB_FD | OFPPF_FIBER
        return PortCapability(
            port = LogicalPort (
                id='nni',
                ofp_port=ofp_port(
                    port_no=port_no,
                    hw_addr=mac_str_to_tuple(
                    '00:00:00:00:00:%02x' % self.ofp_port_no),
                    name='nni',
                    config=0,
                    state=OFPPS_LIVE,
                    curr=cap,
                    advertised=cap,
                    peer=cap,
                    curr_speed=OFPPF_1GB_FD,
                    max_speed=OFPPF_1GB_FD
                )
            )
        )

    # TODO - change for core 2.0
    def reconcile(self, device):
        self.log.info('reconciling-OLT-device-starts')

        if not device.host_and_port:
            device.oper_status = OperStatus.FAILED
            device.reason = 'No host_and_port field provided'
            self.adapter_agent.device_update(device)
            return

        try:
            stub = ponsim_pb2.PonSimStub(self.get_channel())
            info = stub.GetDeviceInfo(Empty())
            log.info('got-info', info=info)
            # TODO: Verify we are connected to the same device we are
            # reconciling - not much data in ponsim to differentiate at the
            # time
            device.oper_status = OperStatus.ACTIVE
            self.adapter_agent.device_update(device)
            self.ofp_port_no = info.nni_port
            self.nni_port = self._get_nni_port()
        except Exception, e:
            log.exception('device-unreachable', e=e)
            device.connect_status = ConnectStatus.UNREACHABLE
            device.oper_status = OperStatus.UNKNOWN
            self.adapter_agent.device_update(device)
            return

        # Now set the initial PM configuration for this device
        self.pm_metrics = AdapterPmMetrics(device)
        pm_config = self.pm_metrics.make_proto()
        log.info("initial-pm-config", pm_config=pm_config)
        self.adapter_agent.device_update_pm_config(pm_config, init=True)

        # Setup alarm handler
        self.alarms = AdapterAlarms(self.adapter, device)

        # TODO: Is there anything required to verify nni and PON ports

        # Set the logical device id
        device = self.adapter_agent.get_device(device.id)
        if device.parent_id:
            self.logical_device_id = device.parent_id
            self.adapter_agent.reconcile_logical_device(device.parent_id)
        else:
            self.log.info('no-logical-device-set')

        # Reconcile child devices
        self.adapter_agent.reconcile_child_devices(device.id)

        reactor.callInThread(self.rcv_grpc)

        # Start collecting stats from the device after a brief pause
        self.start_kpi_collection(device.id)

        self.log.info('reconciling-OLT-device-ends')

    @inlineCallbacks
    def rcv_grpc(self):
        """
        This call establishes a GRPC stream to receive frames.
        """
        yield self.get_channel()
        stub = ponsim_pb2.PonSimStub(self.channel)
        # stub = ponsim_pb2.PonSimStub(self.get_channel())

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


    # VOLTHA's flow decomposition removes the information about which flows
    # are trap flows where traffic should be forwarded to the controller.
    # We'll go through the flows and change the output port of flows that we
    # know to be trap flows to the OF CONTROLLER port.
    def update_flow_table(self, flows):
        stub = ponsim_pb2.PonSimStub(self.get_channel())
        self.log.info('pushing-olt-flow-table')
        for flow in flows:
            classifier_info = {}
            for field in fd.get_ofb_fields(flow):
                if field.type == fd.ETH_TYPE:
                    classifier_info['eth_type'] = field.eth_type
                    self.log.debug('field-type-eth-type',
                                   eth_type=classifier_info['eth_type'])
                elif field.type == fd.IP_PROTO:
                    classifier_info['ip_proto'] = field.ip_proto
                    self.log.debug('field-type-ip-proto',
                                   ip_proto=classifier_info['ip_proto'])
            if ('ip_proto' in classifier_info and (
                    classifier_info['ip_proto'] == 17 or
                    classifier_info['ip_proto'] == 2)) or (
                    'eth_type' in classifier_info and
                    classifier_info['eth_type'] == 0x888e):
                for action in fd.get_actions(flow):
                    if action.type == ofp.OFPAT_OUTPUT:
                        action.output.port = ofp.OFPP_CONTROLLER
            self.log.info('out_port', out_port=fd.get_out_port(flow))

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
            stub = ponsim_pb2.PonSimStub(self.get_channel())
            self.log.info('pushing-onu-flow-table', port=msg.port)
            res = stub.UpdateFlowTable(msg)
            self.adapter_agent.receive_proxied_message(proxy_address, res)

    def packet_out(self, egress_port, msg):
        self.log.info('sending-packet-out', egress_port=egress_port,
                      msg=hexify(msg))
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
        stub = ponsim_pb2.PonSimStub(self.get_channel())
        frame = PonSimFrame(id=self.device_id, payload=str(out_pkt), out_port=out_port)
        stub.SendFrame(frame)


    @inlineCallbacks
    def reboot(self):
        self.log.info('rebooting', device_id=self.device_id)

        yield self.adapter_agent.device_state_update(self.device_id, connect_status=ConnectStatus.UNREACHABLE)

       # Update the child devices connect state to UNREACHABLE
        yield self.adapter_agent.children_state_update(self.device_id,
                                                      connect_status=ConnectStatus.UNREACHABLE)

        # Sleep 10 secs, simulating a reboot
        # TODO: send alert and clear alert after the reboot
        yield asleep(10)

        # Change the connection status back to REACHABLE.  With a
        # real OLT the connection state must be the actual state
        yield self.adapter_agent.device_state_update(self.device_id, connect_status=ConnectStatus.REACHABLE)


        # Update the child devices connect state to REACHABLE
        yield self.adapter_agent.children_state_update(self.device_id,
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
        yield self.adapter_agent.device_state_update(self.device_id, oper_status=OperStatus.UNKNOWN, connect_status=ConnectStatus.UNREACHABLE)

        self.close_channel()
        self.log.info('disabled-grpc-channel')

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
            stub = ponsim_pb2.PonSimStub(self.channel)
            info = stub.GetDeviceInfo(Empty())
            log.info('got-info', info=info)
            self.ofp_port_no = info.nni_port
            ports = yield self._get_nni_port()
            # For ponsim, we are using only 1 NNI port
            if ports.items:
                self.nni_port = ports.items[0]

        # Update the state of the NNI port
        yield self.adapter_agent.port_state_update(self.device_id,
                                                   port_type=Port.ETHERNET_NNI,
                                                   port_no=self.ofp_port_no,
                                                   oper_status=OperStatus.ACTIVE)

        # Update the state of the PON port
        yield self.adapter_agent.port_state_update(self.device_id,
                                                   port_type=Port.PON_OLT,
                                                   port_no=1,
                                                   oper_status=OperStatus.ACTIVE)

        # Set the operational state of the device to ACTIVE and connect status to REACHABLE
        yield self.adapter_agent.device_state_update(self.device_id,
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

                # Step 3: submit directlt to kafka bus
                if kafka_cluster_proxy:
                    if isinstance(kpi_event, Message):
                        kpi_event = dumps(MessageToDict(kpi_event, True, True))
                    kafka_cluster_proxy.send_message("voltha.kpis", kpi_event)

            except Exception as e:
                log.exception('failed-to-submit-kpis', e=e)

        self.pm_metrics.start_collector(_collect)

    def stop_kpi_collection(self):
        self.pm_metrics.stop_collector()
