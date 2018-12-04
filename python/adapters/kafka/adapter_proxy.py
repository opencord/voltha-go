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
Agent to play gateway between adapters.
"""

import structlog
from uuid import uuid4
from twisted.internet.defer import inlineCallbacks, returnValue
from container_proxy import ContainerProxy
from python.protos import third_party
from python.protos.inter_container_pb2 import InterAdapterHeader, \
    InterAdapterMessage
import time

_ = third_party
log = structlog.get_logger()


class AdapterProxy(ContainerProxy):

    def __init__(self, kafka_proxy, core_topic, my_listening_topic):
        super(AdapterProxy, self).__init__(kafka_proxy,
                                           core_topic,
                                           my_listening_topic)

    def _to_string(self, unicode_str):
        if unicode_str is not None:
            if type(unicode_str) == unicode:
                return unicode_str.encode('ascii', 'ignore')
            else:
                return unicode_str
        else:
            return ""

    @ContainerProxy.wrap_request(None)
    @inlineCallbacks
    def send_inter_adapter_message(self,
                                   msg,
                                   type,
                                   from_adapter,
                                   to_adapter,
                                   to_device_id=None,
                                   proxy_device_id=None,
                                   message_id=None):
        """
        Sends a message directly to an adapter. This is typically used to send
        proxied messages from one adapter to another.  An initial ACK response
        is sent back to the invoking adapter.  If there is subsequent response
        to be sent back (async) then the adapter receiving this request will
        use this same API to send back the async response.
        :param msg : GRPC message to send
        :param type : InterAdapterMessageType of the message to send
        :param from_adapter: Name of the adapter making the request.
        :param to_adapter: Name of the remote adapter.
        :param to_device_id: The ID of the device for to the message is
        intended. if it's None then the message is not intended to a specific
        device.  Its interpretation is adapter specific.
        :param proxy_device_id: The ID of the device which will proxy that
        message. If it's None then there is no specific device to proxy the
        message.  Its interpretation is adapter specific.
        :param message_id: A unique number for this transaction that the
        adapter may use to correlate a request and an async response.
        """

        try:
            # validate params
            assert msg
            assert from_adapter
            assert to_adapter

            # Build the inter adapter message
            h = InterAdapterHeader()
            h.type = type
            h.from_topic = self._to_string(from_adapter)
            h.to_topic = self._to_string(to_adapter)
            h.to_device_id = self._to_string(to_device_id)
            h.proxy_device_id = self._to_string(proxy_device_id)

            if message_id:
                h.id = self._to_string(message_id)
            else:
                h.id = uuid4().hex

            h.timestamp = int(round(time.time() * 1000))
            iaMsg = InterAdapterMessage()
            iaMsg.header.CopyFrom(h)
            iaMsg.body.Pack(msg)

            log.debug("sending-inter-adapter-message", header=iaMsg.header)
            res = yield self.invoke(rpc="process_inter_adapter_message",
                                    to_topic=iaMsg.header.to_topic,
                                    msg=iaMsg)
            returnValue(res)
        except Exception as e:
            log.exception("error-sending-request", e=e)
