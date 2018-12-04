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
Agent to play gateway between CORE and an adapter.
"""
import structlog
from google.protobuf.message import Message
from twisted.internet.defer import inlineCallbacks, returnValue

from container_proxy import ContainerProxy
from python.protos.common_pb2 import ID, ConnectStatus, OperStatus
from python.protos.inter_container_pb2 import StrType, BoolType, IntType, Packet
from python.protos.device_pb2 import Device, Ports
from python.protos.voltha_pb2 import CoreInstance

log = structlog.get_logger()


def createSubTopic(*args):
    return '_'.join(args)

class CoreProxy(ContainerProxy):

    def __init__(self, kafka_proxy, core_topic, my_listening_topic):
        super(CoreProxy, self).__init__(kafka_proxy, core_topic,
                                        my_listening_topic)

    @ContainerProxy.wrap_request(CoreInstance)
    @inlineCallbacks
    def register(self, adapter, deviceTypes):
        log.debug("register")
        try:
            res = yield self.invoke(rpc="Register",
                                    adapter=adapter,
                                    deviceTypes=deviceTypes)
            log.info("registration-returned", res=res)
            returnValue(res)
        except Exception as e:
            log.exception("registration-exception", e=e)
            raise

    @ContainerProxy.wrap_request(Device)
    @inlineCallbacks
    def get_device(self, device_id):
        log.debug("get-device")
        id = ID()
        id.id = device_id
        # Once we have a device being managed, all communications between the
        # the adapter and the core occurs over a topic associated with that
        # device
        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="GetDevice",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=id)
        returnValue(res)

    @ContainerProxy.wrap_request(Device)
    @inlineCallbacks
    def get_child_device(self, parent_device_id, **kwargs):
        raise NotImplementedError()

    @ContainerProxy.wrap_request(Ports)
    @inlineCallbacks
    def get_ports(self, device_id, port_type):
        id = ID()
        id.id = device_id
        p_type = IntType()
        p_type.val = port_type
        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="GetPorts",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=id,
                                port_type=p_type)
        returnValue(res)

    def get_child_devices(self, parent_device_id):
        raise NotImplementedError()

    def get_child_device_with_proxy_address(self, proxy_address):
        raise NotImplementedError()

    def _to_proto(self, **kwargs):
        encoded = {}
        for k, v in kwargs.iteritems():
            if isinstance(v, Message):
                encoded[k] = v
            elif type(v) == int:
                i_proto = IntType()
                i_proto.val = v
                encoded[k] = i_proto
            elif type(v) == str:
                s_proto = StrType()
                s_proto.val = v
                encoded[k] = s_proto
            elif type(v) == bool:
                b_proto = BoolType()
                b_proto.val = v
                encoded[k] = b_proto
        return encoded

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def child_device_detected(self,
                              parent_device_id,
                              parent_port_no,
                              child_device_type,
                              channel_id,
                              **kw):
        id = ID()
        id.id = parent_device_id
        ppn = IntType()
        ppn.val = parent_port_no
        cdt = StrType()
        cdt.val = child_device_type
        channel = IntType()
        channel.val = channel_id
        to_topic = createSubTopic(self.core_topic, parent_device_id)
        reply_topic = createSubTopic(self.listening_topic, parent_device_id)
        args = self._to_proto(**kw)
        res = yield self.invoke(rpc="ChildDeviceDetected",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                parent_device_id=id,
                                parent_port_no=ppn,
                                child_device_type=cdt,
                                channel_id=channel,
                                **args)
        returnValue(res)

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def device_update(self, device):
        log.debug("device_update")
        to_topic = createSubTopic(self.core_topic, device.id)
        reply_topic = createSubTopic(self.listening_topic, device.id)
        res = yield self.invoke(rpc="DeviceUpdate",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device=device)
        returnValue(res)

    def child_device_removed(parent_device_id, child_device_id):
        raise NotImplementedError()

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def device_state_update(self, device_id,
                            oper_status=None,
                            connect_status=None):
        id = ID()
        id.id = device_id
        o_status = IntType()
        if oper_status or oper_status == OperStatus.UNKNOWN:
            o_status.val = oper_status
        else:
            o_status.val = -1
        c_status = IntType()
        if connect_status or connect_status == ConnectStatus.UNKNOWN:
            c_status.val = connect_status
        else:
            c_status.val = -1

        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="DeviceStateUpdate",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=id,
                                oper_status=o_status,
                                connect_status=c_status)
        returnValue(res)

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def children_state_update(self, device_id,
                              oper_status=None,
                              connect_status=None):
        id = ID()
        id.id = device_id
        o_status = IntType()
        if oper_status or oper_status == OperStatus.UNKNOWN:
            o_status.val = oper_status
        else:
            o_status.val = -1
        c_status = IntType()
        if connect_status or connect_status == ConnectStatus.UNKNOWN:
            c_status.val = connect_status
        else:
            c_status.val = -1

        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="ChildrenStateUpdate",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=id,
                                oper_status=o_status,
                                connect_status=c_status)
        returnValue(res)

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def port_state_update(self,
                          device_id,
                          port_type,
                          port_no,
                          oper_status):
        id = ID()
        id.id = device_id
        pt = IntType()
        pt.val = port_type
        pNo = IntType()
        pNo.val = port_no
        o_status = IntType()
        o_status.val = oper_status

        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="PortStateUpdate",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=id,
                                port_type=pt,
                                port_no=pNo,
                                oper_status=o_status)
        returnValue(res)

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def child_devices_state_update(self, parent_device_id,
                                   oper_status=None,
                                   connect_status=None):

        id = ID()
        id.id = parent_device_id
        o_status = IntType()
        if oper_status or oper_status == OperStatus.UNKNOWN:
            o_status.val = oper_status
        else:
            o_status.val = -1
        c_status = IntType()
        if connect_status or connect_status == ConnectStatus.UNKNOWN:
            c_status.val = connect_status
        else:
            c_status.val = -1

        to_topic = createSubTopic(self.core_topic, parent_device_id)
        reply_topic = createSubTopic(self.listening_topic, parent_device_id)
        res = yield self.invoke(rpc="child_devices_state_update",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                parent_device_id=id,
                                oper_status=o_status,
                                connect_status=c_status)
        returnValue(res)

    def child_devices_removed(parent_device_id):
        raise NotImplementedError()

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def device_pm_config_update(self, device_pm_config, init=False):
        log.debug("device_pm_config_update")
        b = BoolType()
        b.val = init
        to_topic = createSubTopic(self.core_topic, device_pm_config.id)
        reply_topic = createSubTopic(self.listening_topic, device_pm_config.id)
        res = yield self.invoke(rpc="DevicePMConfigUpdate",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_pm_config=device_pm_config,
                                init=b)
        returnValue(res)

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def port_created(self, device_id, port):
        log.debug("port_created")
        proto_id = ID()
        proto_id.id = device_id
        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="PortCreated",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=proto_id,
                                port=port)
        returnValue(res)

    def port_removed(device_id, port):
        raise NotImplementedError()

    def ports_enabled(device_id):
        raise NotImplementedError()

    def ports_disabled(device_id):
        raise NotImplementedError()

    def ports_oper_status_update(device_id, oper_status):
        raise NotImplementedError()

    def image_download_update(img_dnld):
        raise NotImplementedError()

    def image_download_deleted(img_dnld):
        raise NotImplementedError()

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def send_packet_in(self, device_id, port, packet):
        log.debug("send_packet_in", device_id=device_id)
        proto_id = ID()
        proto_id.id = device_id
        p = IntType()
        p.val = port
        pac = Packet()
        pac.payload = packet
        to_topic = createSubTopic(self.core_topic, device_id)
        reply_topic = createSubTopic(self.listening_topic, device_id)
        res = yield self.invoke(rpc="PacketIn",
                                to_topic=to_topic,
                                reply_topic=reply_topic,
                                device_id=proto_id,
                                port=p,
                                packet=pac)
        returnValue(res)
