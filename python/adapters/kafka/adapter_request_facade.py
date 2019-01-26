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
This facade handles kafka-formatted messages from the Core, extracts the kafka
formatting and forwards the request to the concrete handler.
"""
import structlog
from twisted.internet.defer import inlineCallbacks
from zope.interface import implementer
from twisted.internet import reactor

from afkak.consumer import OFFSET_LATEST, OFFSET_EARLIEST
from python.adapters.interface import IAdapterInterface
from python.protos.inter_container_pb2 import IntType, InterAdapterMessage, StrType, Error, ErrorCode
from python.protos.device_pb2 import Device, ImageDownload
from python.protos.openflow_13_pb2 import FlowChanges, FlowGroups, Flows, \
    FlowGroupChanges, ofp_packet_out
from python.adapters.kafka.kafka_inter_container_library import IKafkaMessagingProxy, \
    get_messaging_proxy, KAFKA_OFFSET_LATEST, KAFKA_OFFSET_EARLIEST

log = structlog.get_logger()

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
        log.debug('starting')

    @inlineCallbacks
    def stop(self):
        log.debug('stopping')

    @inlineCallbacks
    def createKafkaDeviceTopic(self, deviceId):
        log.debug("subscribing-to-topic", device_id=deviceId)
        kafka_proxy = get_messaging_proxy()
        device_topic = kafka_proxy.get_default_topic() + "_" + deviceId
        # yield kafka_proxy.create_topic(topic=device_topic)
        yield kafka_proxy.subscribe(topic=device_topic, group_id=device_topic, target_cls=self, offset=KAFKA_OFFSET_EARLIEST)
        log.debug("subscribed-to-topic", topic=device_topic)

    def adopt_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)

            # Start the creation of a device specific topic to handle all
            # subsequent requests from the Core. This adapter instance will
            # handle all requests for that device.
            reactor.callLater(0, self.createKafkaDeviceTopic, d.id)

            result = self.adapter.adopt_device(d)
            # return True, self.adapter.adopt_device(d)

            return True, result
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")

    def get_ofp_device_info(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return True, self.adapter.get_ofp_device_info(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")

    def get_ofp_port_info(self, device, port_no):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        p = IntType()
        if port_no:
            port_no.Unpack(p)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="port-no-invalid")

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
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")

    def reenable_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return True, self.adapter.reenable_device(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")

    def reboot_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            return (True, self.adapter.reboot_device(d))
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")

    def download_image(self, device, request):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        img = ImageDownload()
        if request:
            request.Unpack(img)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="port-no-invalid")

        return True, self.adapter.download_image(device, request)

    def get_image_download_status(self, device, request):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        img = ImageDownload()
        if request:
            request.Unpack(img)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="port-no-invalid")

        return True, self.adapter.get_image_download_status(device, request)

    def cancel_image_download(self, device, request):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        img = ImageDownload()
        if request:
            request.Unpack(img)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="port-no-invalid")

        return True, self.adapter.cancel_image_download(device, request)

    def activate_image_update(self, device, request):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        img = ImageDownload()
        if request:
            request.Unpack(img)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="port-no-invalid")

        return True, self.adapter.activate_image_update(device, request)

    def revert_image_update(self, device, request):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        img = ImageDownload()
        if request:
            request.Unpack(img)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="port-no-invalid")

        return True, self.adapter.revert_image_update(device, request)


    def self_test(self, device):
        return self.adapter.self_test_device(device)

    def delete_device(self, device):
        d = Device()
        if device:
            device.Unpack(d)
            result = self.adapter.delete_device(d)
            # return (True, self.adapter.delete_device(d))

            # Before we return, delete the device specific topic as we will no
            # longer receive requests from the Core for that device
            kafka_proxy = get_messaging_proxy()
            device_topic = kafka_proxy.get_default_topic() + "/" + d.id
            kafka_proxy.unsubscribe(topic=device_topic)

            return (True, result)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")

    def get_device_details(self, device):
        return self.adapter.get_device_details(device)

    def update_flows_bulk(self, device, flows, groups):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        f = Flows()
        if flows:
            flows.Unpack(f)

        g = FlowGroups()
        if groups:
            groups.Unpack(g)

        return (True, self.adapter.update_flows_bulk(d, f, g))

    def update_flows_incrementally(self, device, flow_changes, group_changes):
        d = Device()
        if device:
            device.Unpack(d)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="device-invalid")
        f = FlowChanges()
        if flow_changes:
            flow_changes.Unpack(f)

        g = FlowGroupChanges()
        if group_changes:
            group_changes.Unpack(g)

        return (True, self.adapter.update_flows_incrementally(d, f, g))

    def suppress_alarm(self, filter):
        return self.adapter.suppress_alarm(filter)

    def unsuppress_alarm(self, filter):
        return self.adapter.unsuppress_alarm(filter)

    def process_inter_adapter_message(self, msg):
        m = InterAdapterMessage()
        if msg:
            msg.Unpack(m)
        else:
            return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                reason="msg-invalid")

        return (True, self.adapter.process_inter_adapter_message(m))


    def receive_packet_out(self, deviceId, outPort, packet):
        try:
            d_id = StrType()
            if deviceId:
                deviceId.Unpack(d_id)
            else:
                return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                    reason="deviceid-invalid")

            op = IntType()
            if outPort:
                outPort.Unpack(op)
            else:
                return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                    reason="outport-invalid")

            p = ofp_packet_out()
            if packet:
                packet.Unpack(p)
            else:
                return False, Error(code=ErrorCode.INVALID_PARAMETERS,
                                    reason="packet-invalid")

            return (True, self.adapter.receive_packet_out(d_id.val, op.val, p))
        except Exception as e:
            log.exception("error-processing-receive_packet_out", e=e)

