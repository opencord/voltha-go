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
from twisted.python import failure
from zope.interface import implementer

from adapters.common.event_bus import EventBusClient
from adapters.common.frameio.frameio import hexify
from adapters.common.utils.id_generation import create_cluster_logical_device_ids
from adapters.interface import IAdapterInterface
from adapters.protos import third_party
from adapters.protos.device_pb2 import Device, Port, PmConfigs
from adapters.protos.events_pb2 import AlarmEvent, AlarmEventType, \
    AlarmEventSeverity, AlarmEventState, AlarmEventCategory
from adapters.protos.events_pb2 import KpiEvent
from adapters.protos.voltha_pb2 import DeviceGroup, LogicalDevice, \
    LogicalPort, AdminState, OperStatus, AlarmFilterRuleKey, CoreInstance
from adapters.common.utils.registry import registry, IComponent
from adapters.common.utils.id_generation import create_cluster_device_id
import re
from adapters.interface import ICoreSouthBoundInterface
from adapters.protos.core_adapter_pb2 import StrType, BoolType, IntType
from adapters.protos.common_pb2 import ID
from google.protobuf.message import Message
from adapters.common.utils.deferred_utils import DeferredWithTimeout, TimeOutError

log = structlog.get_logger()

class KafkaMessagingError(BaseException):
    def __init__(self, error):
        self.error = error

def wrap_request(return_cls):
    def real_wrapper(func):
        @inlineCallbacks
        def wrapper(*args, **kw):
            try:
                (success, d) = yield func(*args, **kw)
                if success:
                    log.debug("successful-response", func=func, val=d)
                    if return_cls is not None:
                        rc = return_cls()
                        if d is not None:
                            d.Unpack(rc)
                        returnValue(rc)
                    else:
                        log.debug("successful-response-none", func=func,
                                  val=None)
                        returnValue(None)
                else:
                    log.warn("unsuccessful-request", func=func, args=args, kw=kw)
                    returnValue(d)
            except Exception as e:
                log.exception("request-wrapper-exception", func=func, e=e)
                raise
        return wrapper
    return real_wrapper


@implementer(IComponent, ICoreSouthBoundInterface)
class CoreProxy(object):

    def __init__(self, kafka_proxy, core_topic, my_listening_topic):
        self.kafka_proxy = kafka_proxy
        self.listening_topic = my_listening_topic
        self.core_topic = core_topic
        self.default_timeout = 3

    def start(self):
        log.info('started')

        return self

    def stop(self):
        log.info('stopped')

    @inlineCallbacks
    def invoke(self, rpc, to_topic=None, **kwargs):
        @inlineCallbacks
        def _send_request(rpc, m_callback,to_topic, **kwargs):
            try:
                log.debug("sending-request", rpc=rpc)
                if to_topic is None:
                    to_topic = self.core_topic
                result = yield self.kafka_proxy.send_request(rpc=rpc,
                                                             to_topic=to_topic,
                                                             reply_topic=self.listening_topic,
                                                             callback=None,
                                                             **kwargs)
                if not m_callback.called:
                    m_callback.callback(result)
                else:
                    log.debug('timeout-already-occurred', rpc=rpc)
            except Exception as e:
                log.exception("Failure-sending-request", rpc=rpc, kw=kwargs)
                if not m_callback.called:
                    m_callback.errback(failure.Failure())

        log.debug('invoke-request', rpc=rpc)
        cb = DeferredWithTimeout(timeout=self.default_timeout)
        _send_request(rpc, cb, to_topic, **kwargs)
        try:
            res = yield cb
            returnValue(res)
        except TimeOutError as e:
            log.warn('invoke-timeout', e=e)
            raise e


    @wrap_request(CoreInstance)
    @inlineCallbacks
    def register(self, adapter):
        log.debug("register")
        try:
            res = yield self.invoke(rpc="Register", adapter=adapter)
            log.info("registration-returned", res=res)
            returnValue(res)
        except Exception as e:
            log.exception("registration-exception", e=e)
            raise

    @wrap_request(Device)
    @inlineCallbacks
    def get_device(self, device_id):
        log.debug("get-device")
        id = ID()
        id.id = device_id
        res = yield self.invoke(rpc="GetDevice", device_id=id)
        returnValue(res)

    @wrap_request(Device)
    @inlineCallbacks
    def get_child_device(self, parent_device_id, **kwargs):
        raise NotImplementedError()

    # def add_device(self, device):
    #     raise NotImplementedError()

    def get_ports(self, device_id, port_type):
        raise NotImplementedError()

    def get_child_devices(self, parent_device_id):
        raise NotImplementedError()

    def get_child_device_with_proxy_address(self, proxy_address):
        raise NotImplementedError()

    def _to_proto(self, **kwargs):
        encoded = {}
        for k,v in kwargs.iteritems():
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


    @wrap_request(None)
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

        args = self._to_proto(**kw)
        res = yield self.invoke(rpc="ChildDeviceDetected",
                                parent_device_id=id,
                                parent_port_no = ppn,
                                child_device_type= cdt,
                                channel_id=channel,
                                **args)
        returnValue(res)


    @wrap_request(None)
    @inlineCallbacks
    def device_update(self, device):
        log.debug("device_update")
        res = yield self.invoke(rpc="DeviceUpdate", device=device)
        returnValue(res)

    def child_device_removed(parent_device_id, child_device_id):
        raise NotImplementedError()


    @wrap_request(None)
    @inlineCallbacks
    def device_state_update(self, device_id,
                                   oper_status=None,
                                   connect_status=None):

        id = ID()
        id.id = device_id
        o_status = IntType()
        if oper_status:
            o_status.val = oper_status
        else:
            o_status.val = -1
        c_status = IntType()
        if connect_status:
            c_status.val = connect_status
        else:
            c_status.val = -1
        a_status = IntType()

        res = yield self.invoke(rpc="DeviceStateUpdate",
                                device_id=id,
                                oper_status=o_status,
                                connect_status=c_status)
        returnValue(res)

    @wrap_request(None)
    @inlineCallbacks
    def child_devices_state_update(self, parent_device_id,
                                   oper_status=None,
                                   connect_status=None,
                                   admin_state=None):

        id = ID()
        id.id = parent_device_id
        o_status = IntType()
        if oper_status:
            o_status.val = oper_status
        else:
            o_status.val = -1
        c_status = IntType()
        if connect_status:
            c_status.val = connect_status
        else:
            c_status.val = -1
        a_status = IntType()
        if admin_state:
            a_status.val = admin_state
        else:
            a_status.val = -1

        res = yield self.invoke(rpc="child_devices_state_update",
                                parent_device_id=id,
                                oper_status=o_status,
                                connect_status=c_status,
                                admin_state=a_status)
        returnValue(res)


    def child_devices_removed(parent_device_id):
        raise NotImplementedError()


    @wrap_request(None)
    @inlineCallbacks
    def device_pm_config_update(self, device_pm_config, init=False):
        log.debug("device_pm_config_update")
        b = BoolType()
        b.val = init
        res = yield self.invoke(rpc="DevicePMConfigUpdate",
                                device_pm_config=device_pm_config, init=b)
        returnValue(res)

    @wrap_request(None)
    @inlineCallbacks
    def port_created(self, device_id, port):
        log.debug("port_created")
        proto_id = ID()
        proto_id.id = device_id
        res = yield self.invoke(rpc="PortCreated", device_id=proto_id, port=port)
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

    def packet_in(device_id, egress_port_no, packet):
        raise NotImplementedError()
