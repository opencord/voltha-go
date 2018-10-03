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
Agent to play gateway between CORE and an individual adapter.
"""
from uuid import uuid4

import arrow
import structlog
from google.protobuf.json_format import MessageToJson
from scapy.packet import Packet
from twisted.internet.defer import inlineCallbacks, returnValue
from zope.interface import implementer

from adapters.common.event_bus import EventBusClient
from adapters.common.frameio.frameio import hexify
from adapters.common.utils.id_generation import create_cluster_logical_device_ids
from adapters.interface import IAdapterInterface
from adapters.protos.device_pb2 import Device

from adapters.protos import third_party
from adapters.protos.device_pb2 import Device, Port, PmConfigs
from adapters.protos.events_pb2 import AlarmEvent, AlarmEventType, \
    AlarmEventSeverity, AlarmEventState, AlarmEventCategory
from adapters.protos.events_pb2 import KpiEvent
from adapters.protos.voltha_pb2 import DeviceGroup, LogicalDevice, \
    LogicalPort, AdminState, OperStatus, AlarmFilterRuleKey
from adapters.common.utils.registry import registry
from adapters.common.utils.id_generation import create_cluster_device_id
from adapters.protos.core_adapter_pb2 import IntType
import re


class MacAddressError(BaseException):
    def __init__(self, error):
        self.error = error


class IDError(BaseException):
    def __init__(self, error):
        self.error = error


@implementer(IAdapterInterface)
class AdapterRequestFacade(object):
    """
    Gate-keeper between CORE and device adapters.

    On one side it interacts with Core's internal model and update/dispatch
    mechanisms.

    On the other side, it interacts with the adapters standard interface as
    defined in
    """

    def __init__(self, adapter):
        self.adapter = adapter

    @inlineCallbacks
    def start(self):
        self.log.debug('starting')

    @inlineCallbacks
    def stop(self):
        self.log.debug('stopping')

    def adopt_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return True, self.adapter.adopt_device(d)
        else:
            return False, d

    def get_ofp_device_info(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return True, self.adapter.get_ofp_device_info(d)
        else:
            return False, d

    def get_ofp_port_info(self, device, port_no):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return (False, d)

        p = IntType()
        port_no.Unpack(p)

        return True, self.adapter.get_ofp_port_info(d, p.val)


    def reconcile_device(self, device):
        return self.adapter.reconcile_device(device)

    def abandon_device(self, device):
        return self.adapter.abandon_device(device)

    def disable_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return True, self.adapter.disable_device(d)
        else:
            return False, d

    def reenable_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return True, self.adapter.reenable_device(d)
        else:
            return False, d

    def reboot_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return (True, self.adapter.reboot_device(d))
        else:
            return (False, d)

    def download_image(self, device, request):
        return self.adapter.download_image(device, request)

    def get_image_download_status(self, device, request):
        return self.adapter.get_image_download_status(device, request)

    def cancel_image_download(self, device, request):
        return self.adapter.cancel_image_download(device, request)

    def activate_image_update(self, device, request):
        return self.adapter.activate_image_update(device, request)

    def revert_image_update(self, device, request):
        return self.adapter.revert_image_update(device, request)

    def self_test(self, device):
        return self.adapter.self_test_device(device)

    def delete_device(self, device):
        # Remove all child devices
        self.delete_all_child_devices(device.id)

        return self.adapter.delete_device(device)

    def get_device_details(self, device):
        return self.adapter.get_device_details(device)

    def update_flows_bulk(self, device, flows, groups):
        return self.adapter.update_flows_bulk(device, flows, groups)

    def update_flows_incrementally(self, device, flow_changes, group_changes):
        return self.adapter.update_flows_incrementally(device, flow_changes, group_changes)

    def suppress_alarm(self, filter):
        return self.adapter.suppress_alarm(filter)

    def unsuppress_alarm(self, filter):
        return self.adapter.unsuppress_alarm(filter)

