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
Represents an ONU device
"""

from uuid import uuid4

import arrow
import structlog
from google.protobuf.json_format import MessageToDict
from google.protobuf.message import Message
from simplejson import dumps
from twisted.internet.defer import DeferredQueue, inlineCallbacks, \
    returnValue, Deferred
from twisted.internet.task import LoopingCall

from python.common.utils.asleep import asleep
from python.adapters.iadapter import OnuAdapter
from python.adapters.kafka.kafka_proxy import get_kafka_proxy
from python.protos import third_party
from python.protos.common_pb2 import OperStatus, ConnectStatus, AdminState
from python.protos.core_adapter_pb2 import PortCapability, \
    InterAdapterMessageType, InterAdapterResponseBody
from python.protos.device_pb2 import Port, PmConfig, PmConfigs
from python.protos.events_pb2 import KpiEvent, KpiEventType, MetricValuePairs
from python.protos.logical_device_pb2 import LogicalPort
from python.protos.openflow_13_pb2 import OFPPS_LIVE, OFPPF_FIBER, \
    OFPPF_1GB_FD
from python.protos.openflow_13_pb2 import ofp_port
from python.protos.ponsim_pb2 import FlowTable, PonSimMetricsRequest, PonSimMetrics

_ = third_party
log = structlog.get_logger()


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
        self.name = 'ponsim_onu'
        self.default_freq = 150
        self.grouped = False
        self.freq_override = False
        self.pm_metrics = None
        self.pon_metrics_config = dict()
        self.uni_metrics_config = dict()
        self.lc = None
        for m in self.pm_names:
            self.pon_metrics_config[m] = PmConfig(name=m,
                                                  type=PmConfig.COUNTER,
                                                  enabled=True)
            self.uni_metrics_config[m] = PmConfig(name=m,
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
            self.uni_metrics_config[m.name].enabled = m.enabled

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

    def extract_metrics(self, stats):
        rtrn_port_metrics = dict()
        rtrn_port_metrics['pon'] = self.extract_pon_metrics(stats)
        rtrn_port_metrics['uni'] = self.extract_uni_metrics(stats)
        return rtrn_port_metrics

    def extract_pon_metrics(self, stats):
        rtrn_pon_metrics = dict()
        for m in stats.metrics:
            if m.port_name == "pon":
                for p in m.packets:
                    if self.pon_metrics_config[p.name].enabled:
                        rtrn_pon_metrics[p.name] = p.value
                return rtrn_pon_metrics

    def extract_uni_metrics(self, stats):
        rtrn_pon_metrics = dict()
        for m in stats.metrics:
            if m.port_name == "uni":
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


class PonSimOnuAdapter(OnuAdapter):
    def __init__(self, core_proxy, adapter_proxy, config):
        # DeviceType of ONU should be same as VENDOR ID of ONU Serial Number
        # as specified by standard
        # requires for identifying correct adapter or ranged ONU
        super(PonSimOnuAdapter, self).__init__(core_proxy=core_proxy,
                                               adapter_proxy=adapter_proxy,
                                               config=config,
                                               device_handler_class=PonSimOnuHandler,
                                               name='ponsim_onu',
                                               vendor='Voltha project',
                                               version='0.4',
                                               device_type='ponsim_onu',
                                               vendor_id='PSMO',
                                               accepts_bulk_flow_update=True,
                                               accepts_add_remove_flow_updates=False)


class PonSimOnuHandler(object):
    def __init__(self, adapter, device_id):
        self.adapter = adapter
        self.core_proxy = adapter.core_proxy
        self.adapter_proxy = adapter.adapter_proxy
        self.device_id = device_id
        self.device_parent_id = None
        self.log = structlog.get_logger(device_id=device_id)
        self.incoming_messages = DeferredQueue()
        self.inter_adapter_message_deferred_map = {}
        self.proxy_address = None
        # reference of uni_port is required when re-enabling the device if
        # it was disabled previously
        self.uni_port = None
        self.pon_port = None

    def _to_string(self, unicode_str):
        if unicode_str is not None:
            if type(unicode_str) == unicode:
                return unicode_str.encode('ascii', 'ignore')
            else:
                return unicode_str
        else:
            return ""

    def receive_message(self, msg):
        trns_id = self._to_string(msg.header.id)
        if trns_id in self.inter_adapter_message_deferred_map:
            self.inter_adapter_message_deferred_map[trns_id].callback(msg)
            # self.incoming_messages.put(msg)

    @inlineCallbacks
    def activate(self, device):
        self.log.info('activating')

        self.device_parent_id = device.parent_id
        self.proxy_address = device.proxy_address

        # populate device info
        device.root = False
        device.vendor = 'ponsim'
        device.model = 'n/a'
        yield self.core_proxy.device_update(device)

        # Now set the initial PM configuration for this device
        self.pm_metrics = AdapterPmMetrics(device)
        pm_config = self.pm_metrics.make_proto()
        log.info("initial-pm-config", pm_config=pm_config)
        self.core_proxy.device_pm_config_update(pm_config, init=True)

        # register physical ports
        self.uni_port = Port(
            port_no=2,
            label='UNI facing Ethernet port',
            type=Port.ETHERNET_UNI,
            admin_state=AdminState.ENABLED,
            oper_status=OperStatus.ACTIVE
        )
        self.pon_port = Port(
            port_no=1,
            label='PON port',
            type=Port.PON_ONU,
            admin_state=AdminState.ENABLED,
            oper_status=OperStatus.ACTIVE,
            peers=[
                Port.PeerPort(
                    device_id=device.parent_id,
                    port_no=device.parent_port_no
                )
            ]
        )
        self.core_proxy.port_created(device.id, self.uni_port)
        self.core_proxy.port_created(device.id, self.pon_port)

        yield self.core_proxy.device_state_update(device.id,
                                                  connect_status=ConnectStatus.REACHABLE,
                                                  oper_status=OperStatus.ACTIVE)

        # Start collecting stats from the device after a brief pause
        self.start_kpi_collection(device.id)

    # TODO: Return only port specific info
    def get_ofp_port_info(self, device, port_no):
        # Since the adapter created the device port then it has the reference
        #  of the port to
        # return the capability.   TODO:  Do a lookup on the UNI port number
        # and return the
        # appropriate attributes
        self.log.info('get_ofp_port_info', port_no=port_no, device_id=device.id)
        cap = OFPPF_1GB_FD | OFPPF_FIBER
        return PortCapability(
            port=LogicalPort(
                ofp_port=ofp_port(
                    hw_addr=mac_str_to_tuple('00:00:00:00:00:%02x' % port_no),
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

    @inlineCallbacks
    def _get_uni_port(self):
        ports = yield self.core_proxy.get_ports(self.device_id,
                                                Port.ETHERNET_UNI)
        returnValue(ports)

    @inlineCallbacks
    def _get_pon_port(self):
        ports = yield self.core_proxy.get_ports(self.device_id, Port.PON_ONU)
        returnValue(ports)

    def reconcile(self, device):
        self.log.info('reconciling-ONU-device-starts')
        # TODO: complete code

    @inlineCallbacks
    def update_flow_table(self, flows):
        trnsId = None
        try:
            self.log.info('update_flow_table', flows=flows)
            # we need to proxy through the OLT to get to the ONU

            fb = FlowTable(
                port=self.proxy_address.channel_id,
                flows=flows
            )

            # Create a deferred to wait for the result as well as a transid
            wait_for_result = Deferred()
            trnsId = uuid4().hex
            self.inter_adapter_message_deferred_map[
                self._to_string(trnsId)] = wait_for_result

            # Sends the request via proxy and wait for an ACK
            yield self.adapter_proxy.send_inter_adapter_message(
                msg=fb,
                type=InterAdapterMessageType.FLOW_REQUEST,
                from_adapter=self.adapter.name,
                to_adapter=self.proxy_address.device_type,
                to_device_id=self.device_id,
                proxy_device_id=self.proxy_address.device_id,
                message_id=trnsId
            )
            # Wait for the full response from the proxied adapter
            res = yield wait_for_result
            if res.header.type == InterAdapterMessageType.FLOW_RESPONSE:
                body = InterAdapterResponseBody()
                res.body.Unpack(body)
                self.log.info('response-received', result=body.status)
        except Exception as e:
            self.log.exception("update-flow-error", e=e)
        finally:
            if trnsId in self.inter_adapter_message_deferred_map:
                del self.inter_adapter_message_deferred_map[trnsId]

    def process_inter_adapter_message(self, msg):
        # We expect only responses on the ONU side
        self.log.info('process-inter-adapter-message', msg=msg)
        self.receive_message(msg)

    def remove_from_flow_table(self, flows):
        self.log.debug('remove-from-flow-table', flows=flows)
        # TODO: Update PONSIM code to accept incremental flow changes.
        # Once completed, the accepts_add_remove_flow_updates for this
        # device type can be set to True

    def add_to_flow_table(self, flows):
        self.log.debug('add-to-flow-table', flows=flows)
        # TODO: Update PONSIM code to accept incremental flow changes
        # Once completed, the accepts_add_remove_flow_updates for this
        # device type can be set to True

    @inlineCallbacks
    def reboot(self):
        self.log.info('rebooting', device_id=self.device_id)

        # Update the connect status to UNREACHABLE
        yield self.core_proxy.device_state_update(self.device_id,
                                                  connect_status=ConnectStatus.UNREACHABLE)

        # Sleep 10 secs, simulating a reboot
        # TODO: send alert and clear alert after the reboot
        yield asleep(10)

        # Change the connection status back to REACHABLE.  With a
        # real ONU the connection state must be the actual state
        yield self.core_proxy.device_state_update(self.device_id,
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

        # Update the device operational status to UNKNOWN
        yield self.core_proxy.device_state_update(self.device_id,
                                                  oper_status=OperStatus.UNKNOWN,
                                                  connect_status=ConnectStatus.UNREACHABLE)

        self.stop_kpi_collection()

        # TODO:
        # 1) Remove all flows from the device
        # 2) Remove the device from ponsim
        self.log.info('disabled', device_id=self.device_id)

    @inlineCallbacks
    def reenable(self):
        self.log.info('re-enabling', device_id=self.device_id)
        try:

            # Refresh the port reference - we only use one port for now
            ports = yield self._get_uni_port()
            self.log.info('re-enabling-uni-ports', ports=ports)
            if ports.items:
                self.uni_port = ports.items[0]

            ports = yield self._get_pon_port()
            self.log.info('re-enabling-pon-ports', ports=ports)
            if ports.items:
                self.pon_port = ports.items[0]

            # Update the state of the UNI port
            yield self.core_proxy.port_state_update(self.device_id,
                                                    port_type=Port.ETHERNET_UNI,
                                                    port_no=self.uni_port.port_no,
                                                    oper_status=OperStatus.ACTIVE)

            # Update the state of the PON port
            yield self.core_proxy.port_state_update(self.device_id,
                                                    port_type=Port.PON_ONU,
                                                    port_no=self.pon_port.port_no,
                                                    oper_status=OperStatus.ACTIVE)

            yield self.core_proxy.device_state_update(self.device_id,
                                                      oper_status=OperStatus.ACTIVE,
                                                      connect_status=ConnectStatus.REACHABLE)

            self.start_kpi_collection(self.device_id)

            self.log.info('re-enabled', device_id=self.device_id)
        except Exception, e:
            self.log.exception('error-reenabling', e=e)

    def delete(self):
        self.log.info('deleting', device_id=self.device_id)

        # TODO:
        # 1) Remove all flows from the device
        # 2) Remove the device from ponsim

        self.log.info('deleted', device_id=self.device_id)

    def start_kpi_collection(self, device_id):
        kafka_cluster_proxy = get_kafka_proxy()

        @inlineCallbacks
        def _collect(device_id, prefix):
            try:
                self.log.debug("pm-collection-interval")
                # Proxy a message to ponsim_olt. The OLT will then query the ONU
                # for statistics. The reply will
                # arrive proxied back to us in self.receive_message().
                msg = PonSimMetricsRequest(port=self.proxy_address.channel_id)

                # Create a deferred to wait for the result as well as a transid
                wait_for_result = Deferred()
                trnsId = uuid4().hex
                self.inter_adapter_message_deferred_map[
                    self._to_string(trnsId)] = wait_for_result

                # Sends the request via proxy and wait for an ACK
                yield self.adapter_proxy.send_inter_adapter_message(
                    msg=msg,
                    type=InterAdapterMessageType.METRICS_REQUEST,
                    from_adapter=self.adapter.name,
                    to_adapter=self.proxy_address.device_type,
                    to_device_id=self.device_id,
                    proxy_device_id=self.proxy_address.device_id,
                    message_id=trnsId
                )
                # Wait for the full response from the proxied adapter
                res = yield wait_for_result
                # Remove the transaction from the transaction map
                del self.inter_adapter_message_deferred_map[self._to_string(trnsId)]

                # Message is a reply to an ONU statistics request. Push it out to
                #  Kafka via adapter.submit_kpis().
                if res.header.type == InterAdapterMessageType.METRICS_RESPONSE:
                    msg = InterAdapterResponseBody()
                    res.body.Unpack(msg)
                    self.log.debug('metrics-response-received', result=msg.status)
                    if self.pm_metrics:
                        self.log.debug('Handling incoming ONU metrics')
                        response = PonSimMetrics()
                        msg.body.Unpack(response)
                        port_metrics = self.pm_metrics.extract_metrics(response)
                        try:
                            ts = arrow.utcnow().timestamp
                            kpi_event = KpiEvent(
                                type=KpiEventType.slice,
                                ts=ts,
                                prefixes={
                                    # OLT NNI port
                                    prefix + '.uni': MetricValuePairs(
                                        metrics=port_metrics['uni']),
                                    # OLT PON port
                                    prefix + '.pon': MetricValuePairs(
                                        metrics=port_metrics['pon'])
                                }
                            )

                            self.log.debug(
                                'Submitting KPI for incoming ONU mnetrics')

                            # Step 3: submit directly to the kafka bus
                            if kafka_cluster_proxy:
                                if isinstance(kpi_event, Message):
                                    kpi_event = dumps(
                                        MessageToDict(kpi_event, True, True))
                                kafka_cluster_proxy.send_message("voltha.kpis",
                                                                 kpi_event)

                        except Exception as e:
                            log.exception('failed-to-submit-kpis', e=e)
            except Exception as e:
                log.exception('failed-to-collect-metrics', e=e)

        self.pm_metrics.start_collector(_collect)

    def stop_kpi_collection(self):
        self.pm_metrics.stop_collector()
