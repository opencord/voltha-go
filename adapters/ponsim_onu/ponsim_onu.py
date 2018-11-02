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

import structlog
from twisted.internet.defer import DeferredQueue, inlineCallbacks, returnValue

from adapters.common.utils.asleep import asleep
from adapters.iadapter import OnuAdapter
from adapters.protos import third_party
from adapters.protos.common_pb2 import OperStatus, ConnectStatus, AdminState
from adapters.protos.core_adapter_pb2 import PortCapability, \
    InterAdapterMessageType, InterAdapterResponseBody
from adapters.protos.device_pb2 import Port
from adapters.protos.logical_device_pb2 import LogicalPort
from adapters.protos.openflow_13_pb2 import OFPPS_LIVE, OFPPF_FIBER, \
    OFPPF_1GB_FD
from adapters.protos.openflow_13_pb2 import ofp_port
from adapters.protos.ponsim_pb2 import FlowTable

_ = third_party
log = structlog.get_logger()


def mac_str_to_tuple(mac):
    return tuple(int(d, 16) for d in mac.split(':'))


class PonSimOnuAdapter(OnuAdapter):
    def __init__(self, core_proxy, adapter_proxy, config):
        # DeviceType of ONU should be same as VENDOR ID of ONU Serial Number as specified by standard
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
        self.proxy_address = None
        # reference of uni_port is required when re-enabling the device if
        # it was disabled previously
        self.uni_port = None
        self.pon_port = None

    def receive_message(self, msg):
        self.incoming_messages.put(msg)

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

    # TODO: Return only port specific info
    def get_ofp_port_info(self, device, port_no):
        # Since the adapter created the device port then it has the reference of the port to
        # return the capability.   TODO:  Do a lookup on the UNI port number and return the
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
        try:
            self.log.info('update_flow_table', flows=flows)
            # we need to proxy through the OLT to get to the ONU

            # reset response queue
            while self.incoming_messages.pending:
                yield self.incoming_messages.get()

            fb = FlowTable(
                port=self.proxy_address.channel_id,
                flows=flows
            )
            # Sends the request via proxy and wait for an ACK
            yield self.adapter_proxy.send_inter_adapter_message(
                msg=fb,
                type=InterAdapterMessageType.FLOW_REQUEST,
                from_adapter=self.adapter.name,
                to_adapter=self.proxy_address.device_type,
                to_device_id=self.device_id,
                proxy_device_id=self.proxy_address.device_id
            )
            # Wait for the full response from the proxied adapter
            res = yield self.incoming_messages.get()
            self.log.info('response-received', result=res)
        except Exception as e:
            self.log.exception("update-flow-error", e=e)

    def process_inter_adapter_message(self, msg):
        self.log.info('process-inter-adapter-message', msg=msg)
        if msg.header.type == InterAdapterMessageType.FLOW_RESPONSE:
            body = InterAdapterResponseBody()
            msg.body.Unpack(body)
            self.log.info('received-response', status=body.success)
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

            self.log.info('re-enabled', device_id=self.device_id)
        except Exception, e:
            self.log.exception('error-reenabling', e=e)

    def delete(self):
        self.log.info('deleting', device_id=self.device_id)

        # TODO:
        # 1) Remove all flows from the device
        # 2) Remove the device from ponsim

        self.log.info('deleted', device_id=self.device_id)
