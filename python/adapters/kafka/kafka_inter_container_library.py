#!/usr/bin/env python

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

import time
from uuid import uuid4

import structlog
from afkak.client import KafkaClient
from afkak.consumer import OFFSET_LATEST, Consumer
from twisted.internet import reactor
from twisted.internet.defer import inlineCallbacks, returnValue, Deferred, \
    DeferredQueue, gatherResults
from zope.interface import implementer

from python.common.utils import asleep
from python.common.utils.registry import IComponent
from kafka_proxy import KafkaProxy, get_kafka_proxy
from python.protos.core_adapter_pb2 import MessageType, Argument, \
    InterContainerRequestBody, InterContainerMessage, Header, \
    InterContainerResponseBody

log = structlog.get_logger()


class KafkaMessagingError(BaseException):
    def __init__(self, error):
        self.error = error


@implementer(IComponent)
class IKafkaMessagingProxy(object):
    _kafka_messaging_instance = None

    def __init__(self,
                 kafka_host_port,
                 kv_store,
                 default_topic,
                 target_cls):
        """
        Initialize the kafka proxy.  This is a singleton (may change to
        non-singleton if performance is better)
        :param kafka_host_port: Kafka host and port
        :param kv_store: Key-Value store
        :param default_topic: Default topic to subscribe to
        :param target_cls: target class - method of that class is invoked
        when a message is received on the default_topic
        """
        # return an exception if the object already exist
        if IKafkaMessagingProxy._kafka_messaging_instance:
            raise Exception(
                'Singleton-exist', cls=IKafkaMessagingProxy)

        log.debug("Initializing-KafkaProxy")
        self.kafka_host_port = kafka_host_port
        self.kv_store = kv_store
        self.default_topic = default_topic
        self.target_cls = target_cls
        self.topic_target_cls_map = {}
        self.topic_consumer_map = {}
        self.topic_callback_map = {}
        self.subscribers = {}
        self.kafka_client = None
        self.kafka_proxy = None
        self.transaction_id_deferred_map = {}
        self.received_msg_queue = DeferredQueue()

        self.init_time = 0
        self.init_received_time = 0

        self.init_resp_time = 0
        self.init_received_resp_time = 0

        self.num_messages = 0
        self.total_time = 0
        self.num_responses = 0
        self.total_time_responses = 0
        log.debug("KafkaProxy-initialized")

    def start(self):
        try:
            # Create the kafka client
            # assert self.kafka_host is not None
            # assert self.kafka_port is not None
            # kafka_host_port = ":".join((self.kafka_host, self.kafka_port))
            self.kafka_client = KafkaClient(self.kafka_host_port)

            # Get the kafka proxy instance.  If it does not exist then
            # create it
            self.kafka_proxy = get_kafka_proxy()
            if self.kafka_proxy == None:
                KafkaProxy(kafka_endpoint=self.kafka_host_port).start()
                self.kafka_proxy = get_kafka_proxy()

            # Subscribe the default topic and target_cls
            self.topic_target_cls_map[self.default_topic] = self.target_cls

            # Start the queue to handle incoming messages
            reactor.callLater(0, self._received_message_processing_loop)

            # Start listening for incoming messages
            reactor.callLater(0, self.subscribe, self.default_topic,
                              target_cls=self.target_cls)

            # Setup the singleton instance
            IKafkaMessagingProxy._kafka_messaging_instance = self
        except Exception as e:
            log.exception("Failed-to-start-proxy", e=e)

    def stop(self):
        """
        Invoked to stop the kafka proxy
        :return: None on success, Exception on failure
        """
        log.debug("Stopping-messaging-proxy ...")
        try:
            # Stop all the consumers
            deferred_list = []
            for key, values in self.topic_consumer_map.iteritems():
                deferred_list.extend([c.stop() for c in values])

            if not deferred_list:
                d = gatherResults(deferred_list)
                d.addCallback(lambda result: self.kafka_client.close())
            log.debug("Messaging-proxy-stopped.")
        except Exception as e:
            log.exception("Exception-when-stopping-messaging-proxy:", e=e)

    @inlineCallbacks
    def _wait_until_topic_is_ready(self, client, topic):
        e = True
        while e:
            yield client.load_metadata_for_topics(topic)
            e = client.metadata_error_for_topic(topic)
            if e:
                log.debug("Topic-not-ready-retrying...", topic=topic)

    def _clear_backoff(self):
        if self.retries:
            log.info('reconnected-to-consul', after_retries=self.retries)
            self.retries = 0

    @inlineCallbacks
    def _subscribe(self, topic, callback=None, target_cls=None):
        try:
            yield self._wait_until_topic_is_ready(self.kafka_client, topic)
            partitions = self.kafka_client.topic_partitions[topic]
            consumers = []

            # First setup the generic callback - all received messages will
            # go through that queue
            if topic not in self.topic_consumer_map:
                consumers = [Consumer(self.kafka_client, topic, partition,
                                      self._enqueue_received_message)
                             for partition in partitions]
                self.topic_consumer_map[topic] = consumers

            log.debug("_subscribe", topic=topic,
                      consumermap=self.topic_consumer_map)

            if target_cls is not None and callback is None:
                # Scenario #1
                if topic not in self.topic_target_cls_map:
                    self.topic_target_cls_map[topic] = target_cls
            elif target_cls is None and callback is not None:
                # Scenario #2
                log.debug("custom-callback", topic=topic,
                          callback_map=self.topic_callback_map)
                if topic not in self.topic_callback_map:
                    self.topic_callback_map[topic] = [callback]
                else:
                    self.topic_callback_map[topic].extend([callback])
            else:
                log.warn("invalid-parameters")

            def cb_closed(result):
                """
                Called when a consumer cleanly stops.
                """
                log.debug("Consumers-cleanly-stopped")

            def eb_failed(failure):
                """
                Called when a consumer fails due to an uncaught exception in the
                processing callback or a network error on shutdown. In this case we
                simply log the error.
                """
                log.warn("Consumers-failed", failure=failure)

            for c in consumers:
                c.start(OFFSET_LATEST).addCallbacks(cb_closed, eb_failed)

            returnValue(True)
        except Exception as e:
            log.exception("Exception-during-subscription", e=e)
            returnValue(False)

    def subscribe(self, topic, callback=None, target_cls=None,
                  max_retry=3):
        """
        Scenario 1:  invoked to subscribe to a specific topic with a
        target_cls to invoke when a message is received on that topic.  This
        handles the case of request/response where this library performs the
        heavy lifting. In this case the m_callback must to be None

        Scenario 2:  invoked to subscribe to a specific topic with a
        specific callback to invoke when a message is received on that topic.
        This handles the case where the caller wants to process the message
        received itself. In this case the target_cls must to be None

        :param topic: topic to subscribe to
        :param callback: Callback to invoke when a message is received on
        the topic. Either one of callback or target_cls needs can be none
        :param target_cls:  Target class to use when a message is
        received on the topic. There can only be 1 target_cls per topic.
        Either one of callback or target_cls needs can be none
        :param max_retry:  the number of retries before reporting failure
        to subscribe.  This caters for scenario where the kafka topic is not
        ready.
        :return: True on success, False on failure
        """
        RETRY_BACKOFF = [0.05, 0.1, 0.2, 0.5, 1, 2, 5]

        def _backoff(msg, retries):
            wait_time = RETRY_BACKOFF[min(retries,
                                          len(RETRY_BACKOFF) - 1)]
            log.info(msg, retry_in=wait_time)
            return asleep(wait_time)

        retry = 0
        while not self._subscribe(topic, callback=callback,
                                  target_cls=target_cls):
            if retry > max_retry:
                return False
            else:
                _backoff("subscription-not-complete", retry)
                retry += 1
        return True

    def unsubscribe(self, topic):
        """
        Invoked when unsubscribing to a topic
        :param topic: topic to unsubscibe from
        :return: None on success or Exception on failure
        """
        log.debug("Unsubscribing-to-topic", topic=topic)

        def remove_topic(topic):
            if topic in self.topic_consumer_map:
                del self.topic_consumer_map[topic]

        try:
            if topic in self.topic_consumer_map:
                consumers = self.topic_consumer_map[topic]
                d = gatherResults([c.stop() for c in consumers])
                d.addCallback(remove_topic, topic)
                log.debug("Unsubscribed-to-topic.", topic=topic)
            else:
                log.debug("Topic-does-not-exist.", topic=topic)
        except Exception as e:
            log.exception("Exception-when-stopping-messaging-proxy:", e=e)

    @inlineCallbacks
    def _enqueue_received_message(self, reactor, message_list):
        """
        Internal method to continuously queue all received messaged
        irrespective of topic
        :param reactor: A requirement by the Twisted Python kafka library
        :param message_list: Received list of messages
        :return: None on success, Exception on failure
        """
        try:
            for m in message_list:
                log.debug("received-msg", msg=m)
                yield self.received_msg_queue.put(m)
        except Exception as e:
            log.exception("Failed-enqueueing-received-message", e=e)

    @inlineCallbacks
    def _received_message_processing_loop(self):
        """
        Internal method to continuously process all received messages one
        at a time
        :return: None on success, Exception on failure
        """
        while True:
            try:
                message = yield self.received_msg_queue.get()
                yield self._process_message(message)
            except Exception as e:
                log.exception("Failed-dequeueing-received-message", e=e)

    def _to_string(self, unicode_str):
        if unicode_str is not None:
            if type(unicode_str) == unicode:
                return unicode_str.encode('ascii', 'ignore')
            else:
                return unicode_str
        else:
            return None

    def _format_request(self,
                        rpc,
                        to_topic,
                        reply_topic,
                        **kwargs):
        """
        Format a request to send over kafka
        :param rpc: Requested remote API
        :param to_topic: Topic to send the request
        :param reply_topic: Topic to receive the resulting response, if any
        :param kwargs: Dictionary of key-value pairs to pass as arguments to
        the remote rpc API.
        :return: A InterContainerMessage message type on success or None on
        failure
        """
        try:
            transaction_id = uuid4().hex
            request = InterContainerMessage()
            request_body = InterContainerRequestBody()
            request.header.id = transaction_id
            request.header.type = MessageType.Value("REQUEST")
            request.header.from_topic = self.default_topic
            request.header.to_topic = to_topic

            response_required = False
            if reply_topic:
                request_body.reply_to_topic = reply_topic
                response_required = True

            request.header.timestamp = int(round(time.time() * 1000))
            request_body.rpc = rpc
            for a, b in kwargs.iteritems():
                arg = Argument()
                arg.key = a
                try:
                    arg.value.Pack(b)
                    request_body.args.extend([arg])
                except Exception as e:
                    log.exception("Failed-parsing-value", e=e)
            request_body.reply_to_topic = self.default_topic
            request_body.response_required = response_required
            request.body.Pack(request_body)
            return request, transaction_id, response_required
        except Exception as e:
            log.exception("formatting-request-failed",
                          rpc=rpc,
                          to_topic=to_topic,
                          reply_topic=reply_topic,
                          args=kwargs)
            return None, None, None

    def _format_response(self, msg_header, msg_body, status):
        """
        Format a response
        :param msg_header: The header portion of a received request
        :param msg_body: The response body
        :param status: True is this represents a successful response
        :return: a InterContainerMessage message type
        """
        try:
            assert isinstance(msg_header, Header)
            response = InterContainerMessage()
            response_body = InterContainerResponseBody()
            response.header.id = msg_header.id
            response.header.timestamp = int(
                round(time.time() * 1000))
            response.header.type = MessageType.Value("RESPONSE")
            response.header.from_topic = msg_header.to_topic
            response.header.to_topic = msg_header.from_topic
            if msg_body is not None:
                response_body.result.Pack(msg_body)
            response_body.success = status
            response.body.Pack(response_body)
            return response
        except Exception as e:
            log.exception("formatting-response-failed", header=msg_header,
                          body=msg_body, status=status, e=e)
            return None

    def _parse_response(self, msg):
        try:
            message = InterContainerMessage()
            message.ParseFromString(msg)
            resp = InterContainerResponseBody()
            if message.body.Is(InterContainerResponseBody.DESCRIPTOR):
                message.body.Unpack(resp)
            else:
                log.debug("unsupported-msg", msg_type=type(message.body))
                return None
            log.debug("parsed-response", input=message, output=resp)
            return resp
        except Exception as e:
            log.exception("parsing-response-failed", msg=msg, e=e)
            return None

    @inlineCallbacks
    def _process_message(self, m):
        """
        Default internal method invoked for every batch of messages received
        from Kafka.
        """

        def _toDict(args):
            """
            Convert a repeatable Argument type into a python dictionary
            :param args: Repeatable core_adapter.Argument type
            :return: a python dictionary
            """
            if args is None:
                return None
            result = {}
            for arg in args:
                assert isinstance(arg, Argument)
                result[arg.key] = arg.value
            return result

        current_time = int(round(time.time() * 1000))
        # log.debug("Got Message", message=m)
        try:
            val = m.message.value
            # print m.topic

            # Go over customized callbacks first
            if m.topic in self.topic_callback_map:
                for c in self.topic_callback_map[m.topic]:
                    yield c(val)

            #  Check whether we need to process request/response scenario
            if m.topic not in self.topic_target_cls_map:
                return

            # Process request/response scenario
            message = InterContainerMessage()
            message.ParseFromString(val)

            if message.header.type == MessageType.Value("REQUEST"):
                # Get the target class for that specific topic
                targetted_topic = self._to_string(message.header.to_topic)
                msg_body = InterContainerRequestBody()
                if message.body.Is(InterContainerRequestBody.DESCRIPTOR):
                    message.body.Unpack(msg_body)
                else:
                    log.debug("unsupported-msg", msg_type=type(message.body))
                    return
                if targetted_topic in self.topic_target_cls_map:
                    if msg_body.args:
                        log.debug("message-body-args-present", body=msg_body)
                        (status, res) = yield getattr(
                            self.topic_target_cls_map[targetted_topic],
                            self._to_string(msg_body.rpc))(
                            **_toDict(msg_body.args))
                    else:
                        log.debug("message-body-args-absent", body=msg_body,
                                  rpc=msg_body.rpc)
                        (status, res) = yield getattr(
                            self.topic_target_cls_map[targetted_topic],
                            self._to_string(msg_body.rpc))()
                    if msg_body.response_required:
                        response = self._format_response(
                            msg_header=message.header,
                            msg_body=res,
                            status=status,
                        )
                        if response is not None:
                            res_topic = self._to_string(
                                response.header.to_topic)
                            self._send_kafka_message(res_topic, response)

                        log.debug("Response-sent", response=response.body)
            elif message.header.type == MessageType.Value("RESPONSE"):
                trns_id = self._to_string(message.header.id)
                if trns_id in self.transaction_id_deferred_map:
                    resp = self._parse_response(val)

                    self.transaction_id_deferred_map[trns_id].callback(resp)
            else:
                log.error("!!INVALID-TRANSACTION-TYPE!!")

        except Exception as e:
            log.exception("Failed-to-process-message", message=m, e=e)

    @inlineCallbacks
    def _send_kafka_message(self, topic, msg):
        try:
            yield self.kafka_proxy.send_message(topic, msg.SerializeToString())
        except Exception, e:
            log.exception("Failed-sending-message", message=msg, e=e)

    @inlineCallbacks
    def send_request(self,
                     rpc,
                     to_topic,
                     reply_topic=None,
                     callback=None,
                     **kwargs):
        """
        Invoked to send a message to a remote container and receive a
        response if required.
        :param rpc: The remote API to invoke
        :param to_topic: Send the message to this kafka topic
        :param reply_topic: If not None then a response is expected on this
        topic.  If set to None then no response is required.
        :param callback: Callback to invoke when a response is received.
        :param kwargs: Key-value pairs representing arguments to pass to the
        rpc remote API.
        :return: Either no response is required, or a response is returned
        via the callback or the response is a tuple of (status, return_cls)
        """
        try:
            # Ensure all strings are not unicode encoded
            rpc = self._to_string(rpc)
            to_topic = self._to_string(to_topic)
            reply_topic = self._to_string(reply_topic)

            request, transaction_id, response_required = \
                self._format_request(
                    rpc=rpc,
                    to_topic=to_topic,
                    reply_topic=reply_topic,
                    **kwargs)

            if request is None:
                return

            # Add the transaction to the transaction map before sending the
            # request.  This will guarantee the eventual response will be
            # processed.
            wait_for_result = None
            if response_required:
                wait_for_result = Deferred()
                self.transaction_id_deferred_map[
                    self._to_string(request.header.id)] = wait_for_result

            yield self._send_kafka_message(to_topic, request)
            log.debug("message-sent", to_topic=to_topic,
                      from_topic=reply_topic)

            if response_required:
                res = yield wait_for_result

                if res is None or not res.success:
                    raise KafkaMessagingError(error="Failed-response:{"
                                                    "}".format(res))

                # Remove the transaction from the transaction map
                del self.transaction_id_deferred_map[transaction_id]

                log.debug("send-message-response", rpc=rpc, result=res)

                if callback:
                    callback((res.success, res.result))
                else:
                    returnValue((res.success, res.result))
        except Exception as e:
            log.exception("Exception-sending-request", e=e)
            raise KafkaMessagingError(error=e)


# Common method to get the singleton instance of the kafka proxy class
def get_messaging_proxy():
    return IKafkaMessagingProxy._kafka_messaging_instance
