#
# Copyright 2018 the original author or authors.
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
Adapter abstract base class
"""

import structlog
from twisted.internet import reactor
from zope.interface import implementer

from interface import IAdapterInterface
from python.protos.adapter_pb2 import Adapter
from python.protos.adapter_pb2 import AdapterConfig
from python.protos.common_pb2 import AdminState
from python.protos.common_pb2 import LogLevel
from python.protos.device_pb2 import DeviceType, DeviceTypes
from python.protos.health_pb2 import HealthStatus


log = structlog.get_logger()


@implementer(IAdapterInterface)
class IAdapter(object):
    def __init__(self,
                 core_proxy,
                 adapter_proxy,
                 config,
                 device_handler_class,
                 name,
                 vendor,
                 version,
                 device_type, vendor_id,
                 accepts_bulk_flow_update=True,
                 accepts_add_remove_flow_updates=False):
        log.debug(
            'Initializing adapter: {} {} {}'.format(vendor, name, version))
        self.core_proxy = core_proxy
        self.adapter_proxy = adapter_proxy
        self.config = config
        self.name = name
        self.supported_device_types = [
            DeviceType(
                id=device_type,
                vendor_id=vendor_id,
                adapter=name,
                accepts_bulk_flow_update=accepts_bulk_flow_update,
                accepts_add_remove_flow_updates=accepts_add_remove_flow_updates
            )
        ]
        self.descriptor = Adapter(
            id=self.name,
            vendor=vendor,
            version=version,
            config=AdapterConfig(log_level=LogLevel.INFO)
        )
        self.devices_handlers = dict()  # device_id -> Olt/OnuHandler()
        self.device_handler_class = device_handler_class

    def start(self):
        log.info('Starting adapter: {}'.format(self.name))

    def stop(self):
        log.info('Stopping adapter: {}'.format(self.name))

    def adapter_descriptor(self):
        return self.descriptor

    def device_types(self):
        return DeviceTypes(items=self.supported_device_types)

    def health(self):
        # return HealthStatus(state=HealthStatus.HealthState.HEALTHY)
        return HealthStatus(state=HealthStatus.HEALTHY)

    def change_master_state(self, master):
        raise NotImplementedError()

    def get_ofp_device_info(self, device):
        log.debug('get_ofp_device_info_start', device_id=device.id)
        ofp_device_info = self.devices_handlers[device.id].get_ofp_device_info(
            device)
        log.debug('get_ofp_device_info_ends', device_id=device.id)
        return ofp_device_info

    def get_ofp_port_info(self, device, port_no):
        log.debug('get_ofp_port_info_start', device_id=device.id,
                  port_no=port_no)
        ofp_port_info = self.devices_handlers[device.id].get_ofp_port_info(
            device, port_no)
        log.debug('get_ofp_port_info_ends', device_id=device.id,
                  port_no=port_no)
        return ofp_port_info

    def adopt_device(self, device):
        log.debug('adopt_device', device_id=device.id)
        self.devices_handlers[device.id] = self.device_handler_class(self,
                                                                     device.id)
        reactor.callLater(0, self.devices_handlers[device.id].activate, device)
        log.debug('adopt_device_done', device_id=device.id)
        return device

    def reconcile_device(self, device):
        raise NotImplementedError()

    def abandon_device(self, device):
        raise NotImplementedError()

    def disable_device(self, device):
        log.info('disable-device', device_id=device.id)
        reactor.callLater(0, self.devices_handlers[device.id].disable)
        log.debug('disable-device-done', device_id=device.id)
        return device

    def reenable_device(self, device):
        log.info('reenable-device', device_id=device.id)
        reactor.callLater(0, self.devices_handlers[device.id].reenable)
        log.info('reenable-device-done', device_id=device.id)
        return device

    def reboot_device(self, device):
        log.info('reboot-device', device_id=device.id)
        reactor.callLater(0, self.devices_handlers[device.id].reboot)
        log.info('reboot-device-done', device_id=device.id)
        return device

    def download_image(self, device, request):
        raise NotImplementedError()

    def get_image_download_status(self, device, request):
        raise NotImplementedError()

    def cancel_image_download(self, device, request):
        raise NotImplementedError()

    def activate_image_update(self, device, request):
        raise NotImplementedError()

    def revert_image_update(self, device, request):
        raise NotImplementedError()

    def self_test_device(self, device):
        log.info('self-test', device_id=device.id)
        result = reactor.callLater(0, self.devices_handlers[
            device.id].self_test_device)
        log.info('self-test-done', device_id=device.id)
        return result

    def delete_device(self, device):
        log.info('delete-device', device_id=device.id)
        reactor.callLater(0, self.devices_handlers[device.id].delete)
        log.info('delete-device-done', device_id=device.id)
        return device

    def get_device_details(self, device):
        raise NotImplementedError()

    def update_flows_bulk(self, device, flows, groups):
        log.info('bulk-flow-update', device_id=device.id,
                 flows=flows, groups=groups)
        assert len(groups.items) == 0
        reactor.callLater(0, self.devices_handlers[device.id].update_flow_table,
                          flows.items)
        return device

    def update_flows_incrementally(self, device, flow_changes, group_changes):
        log.info('incremental-flow-update', device_id=device.id,
                 flows=flow_changes, groups=group_changes)
        # For now, there is no support for group changes
        assert len(group_changes.to_add.items) == 0
        assert len(group_changes.to_remove.items) == 0

        handler = self.devices_handlers[device.id]
        # Remove flows
        if len(flow_changes.to_remove.items) != 0:
            reactor.callLater(0, handler.remove_from_flow_table,
                              flow_changes.to_remove.items)

        # Add flows
        if len(flow_changes.to_add.items) != 0:
            reactor.callLater(0, handler.add_to_flow_table,
                              flow_changes.to_add.items)
        return device

    def update_pm_config(self, device, pm_config):
        log.info("adapter-update-pm-config", device=device,
                 pm_config=pm_config)
        handler = self.devices_handlers[device.id]
        if handler:
            reactor.callLater(0, handler.update_pm_config, device, pm_config)

    def process_inter_adapter_message(self, msg):
        raise NotImplementedError()

    def receive_packet_out(self, device_id, egress_port_no, msg):
        raise NotImplementedError()

    def suppress_alarm(self, filter):
        raise NotImplementedError()

    def unsuppress_alarm(self, filter):
        raise NotImplementedError()

    def _get_handler(self, device):
        if device.id in self.devices_handlers:
            handler = self.devices_handlers[device.id]
            if handler is not None:
                return handler
            return None


"""
OLT Adapter base class
"""


class OltAdapter(IAdapter):
    def __init__(self,
                 core_proxy,
                 adapter_proxy,
                 config,
                 device_handler_class,
                 name,
                 vendor,
                 version, device_type,
                 accepts_bulk_flow_update=True,
                 accepts_add_remove_flow_updates=False):
        super(OltAdapter, self).__init__(core_proxy=core_proxy,
                                         adapter_proxy=adapter_proxy,
                                         config=config,
                                         device_handler_class=device_handler_class,
                                         name=name,
                                         vendor=vendor,
                                         version=version,
                                         device_type=device_type,
                                         vendor_id=None,
                                         accepts_bulk_flow_update=accepts_bulk_flow_update,
                                         accepts_add_remove_flow_updates=accepts_add_remove_flow_updates)
        self.logical_device_id_to_root_device_id = dict()

    def reconcile_device(self, device):
        try:
            self.devices_handlers[device.id] = self.device_handler_class(self,
                                                                         device.id)
            # Work only required for devices that are in ENABLED state
            if device.admin_state == AdminState.ENABLED:
                reactor.callLater(0,
                                  self.devices_handlers[device.id].reconcile,
                                  device)
            else:
                # Invoke the children reconciliation which would setup the
                # basic children data structures
                self.core_proxy.reconcile_child_devices(device.id)
            return device
        except Exception, e:
            log.exception('Exception', e=e)

    def send_proxied_message(self, proxy_address, msg):
        log.info('send-proxied-message', proxy_address=proxy_address, msg=msg)
        handler = self.devices_handlers[proxy_address.device_id]
        handler.send_proxied_message(proxy_address, msg)

    def process_inter_adapter_message(self, msg):
        log.debug('process-inter-adapter-message', msg=msg)
        # Unpack the header to know which device needs to handle this message
        handler = None
        if msg.header.proxy_device_id:
            # typical request
            handler = self.devices_handlers[msg.header.proxy_device_id]
        elif msg.header.to_device_id and \
                msg.header.to_device_id in self.devices_handlers:
            # typical response
            handler = self.devices_handlers[msg.header.to_device_id]
        if handler:
            reactor.callLater(0, handler.process_inter_adapter_message, msg)

    def receive_packet_out(self, device_id, egress_port_no, msg):
        try:
            log.info('receive_packet_out', device_id=device_id,
                     egress_port=egress_port_no, msg=msg)
            handler = self.devices_handlers[device_id]
            if handler:
                reactor.callLater(0, handler.packet_out, egress_port_no, msg.data)
        except Exception, e:
            log.exception('packet-out-failure', e=e)


"""
ONU Adapter base class
"""


class OnuAdapter(IAdapter):
    def __init__(self,
                 core_proxy,
                 adapter_proxy,
                 config,
                 device_handler_class,
                 name,
                 vendor,
                 version,
                 device_type,
                 vendor_id,
                 accepts_bulk_flow_update=True,
                 accepts_add_remove_flow_updates=False):
        super(OnuAdapter, self).__init__(core_proxy=core_proxy,
                                         adapter_proxy=adapter_proxy,
                                         config=config,
                                         device_handler_class=device_handler_class,
                                         name=name,
                                         vendor=vendor,
                                         version=version,
                                         device_type=device_type,
                                         vendor_id=vendor_id,
                                         accepts_bulk_flow_update=accepts_bulk_flow_update,
                                         accepts_add_remove_flow_updates=accepts_add_remove_flow_updates)

    def reconcile_device(self, device):
        self.devices_handlers[device.id] = self.device_handler_class(self,
                                                                     device.id)
        # Reconcile only if state was ENABLED
        if device.admin_state == AdminState.ENABLED:
            reactor.callLater(0,
                              self.devices_handlers[device.id].reconcile,
                              device)
        return device

    def receive_proxied_message(self, proxy_address, msg):
        log.info('receive-proxied-message', proxy_address=proxy_address,
                 device_id=proxy_address.device_id, msg=msg)
        # Device_id from the proxy_address is the olt device id. We need to
        # get the onu device id using the port number in the proxy_address
        device = self.core_proxy. \
            get_child_device_with_proxy_address(proxy_address)
        if device:
            handler = self.devices_handlers[device.id]
            handler.receive_message(msg)

    def process_inter_adapter_message(self, msg):
        log.info('process-inter-adapter-message', msg=msg)
        # Unpack the header to know which device needs to handle this message
        if msg.header:
            handler = self.devices_handlers[msg.header.to_device_id]
            handler.process_inter_adapter_message(msg)
