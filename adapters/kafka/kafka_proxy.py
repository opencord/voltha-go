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

from afkak.client import KafkaClient as _KafkaClient
from afkak.common import (
    PRODUCER_ACK_LOCAL_WRITE, PRODUCER_ACK_NOT_REQUIRED
)
from afkak.producer import Producer as _kafkaProducer
from structlog import get_logger
from twisted.internet.defer import inlineCallbacks, returnValue
from zope.interface import implementer

from adapters.common.utils.consulhelpers import get_endpoint_from_consul
from adapters.kafka.event_bus_publisher import EventBusPublisher
from adapters.common.utils.registry import IComponent
import time

log = get_logger()


@implementer(IComponent)
class KafkaProxy(object):
    """
    This is a singleton proxy kafka class to hide the kafka client details.
    """
    _kafka_instance = None

    def __init__(self,
                 consul_endpoint='localhost:8500',
                 kafka_endpoint='localhost:9092',
                 ack_timeout=1000,
                 max_req_attempts=10,
                 config={}):

        # return an exception if the object already exist
        if KafkaProxy._kafka_instance:
            raise Exception('Singleton exist for :{}'.format(KafkaProxy))

        log.debug('initializing', endpoint=kafka_endpoint)
        self.ack_timeout = ack_timeout
        self.max_req_attempts = max_req_attempts
        self.consul_endpoint = consul_endpoint
        self.kafka_endpoint = kafka_endpoint
        self.config = config
        self.kclient = None
        self.kproducer = None
        self.event_bus_publisher = None
        self.stopping = False
        self.faulty = False
        log.debug('initialized', endpoint=kafka_endpoint)

    @inlineCallbacks
    def start(self):
        log.debug('starting')
        self._get_kafka_producer()
        KafkaProxy._kafka_instance = self
        self.event_bus_publisher = yield EventBusPublisher(
            self, self.config.get('event_bus_publisher', {})).start()
        log.info('started')
        KafkaProxy.faulty = False
        self.stopping = False
        returnValue(self)

    @inlineCallbacks
    def stop(self):
        try:
            log.debug('stopping-kafka-proxy')
            try:
                if self.kclient:
                    yield self.kclient.close()
                    self.kclient = None
                    log.debug('stopped-kclient-kafka-proxy')
            except Exception, e:
                log.exception('failed-stopped-kclient-kafka-proxy', e=e)
                pass

            try:
                if self.kproducer:
                    yield self.kproducer.stop()
                    self.kproducer = None
                    log.debug('stopped-kproducer-kafka-proxy')
            except Exception, e:
                log.exception('failed-stopped-kproducer-kafka-proxy', e=e)
                pass

            #try:
            #    if self.event_bus_publisher:
            #        yield self.event_bus_publisher.stop()
            #        self.event_bus_publisher = None
            #        log.debug('stopped-event-bus-publisher-kafka-proxy')
            #except Exception, e:
            #    log.debug('failed-stopped-event-bus-publisher-kafka-proxy')
            #    pass

            log.debug('stopped-kafka-proxy')

        except Exception, e:
            self.kclient = None
            self.kproducer = None
            #self.event_bus_publisher = None
            log.exception('failed-stopped-kafka-proxy', e=e)
            pass

    def _get_kafka_producer(self):
        # PRODUCER_ACK_LOCAL_WRITE : server will wait till the data is written
        #  to a local log before sending response
        try:

            if self.kafka_endpoint.startswith('@'):
                try:
                    _k_endpoint = get_endpoint_from_consul(self.consul_endpoint,
                                                           self.kafka_endpoint[1:])
                    log.debug('found-kafka-service', endpoint=_k_endpoint)

                except Exception as e:
                    log.exception('no-kafka-service-in-consul', e=e)

                    self.kproducer = None
                    self.kclient = None
                    return
            else:
                _k_endpoint = self.kafka_endpoint

            self.kclient = _KafkaClient(_k_endpoint)
            self.kproducer = _kafkaProducer(self.kclient,
                                            req_acks=PRODUCER_ACK_NOT_REQUIRED,
                                            # req_acks=PRODUCER_ACK_LOCAL_WRITE,
                                            # ack_timeout=self.ack_timeout,
                                            # max_req_attempts=self.max_req_attempts)
                                            )
        except Exception, e:
            log.exception('failed-get-kafka-producer', e=e)
            return

    @inlineCallbacks
    def send_message(self, topic, msg):
        assert topic is not None
        assert msg is not None

        # first check whether we have a kafka producer.  If there is none
        # then try to get one - this happens only when we try to lookup the
        # kafka service from consul
        try:
            if self.faulty is False:

                if self.kproducer is None:
                    self._get_kafka_producer()
                    # Lets the next message request do the retry if still a failure
                    if self.kproducer is None:
                        log.error('no-kafka-producer', endpoint=self.kafka_endpoint)
                        return

                # log.debug('sending-kafka-msg', topic=topic, msg=msg)
                msgs = [msg]

                if self.kproducer and self.kclient and \
                        self.event_bus_publisher and self.faulty is False:
                    # log.debug('sending-kafka-msg-I-am-here0', time=int(round(time.time() * 1000)))

                    yield self.kproducer.send_messages(topic, msgs=msgs)
                    # self.kproducer.send_messages(topic, msgs=msgs)
                    # log.debug('sent-kafka-msg', topic=topic, msg=msg)
                else:
                    return

        except Exception, e:
            self.faulty = True
            log.error('failed-to-send-kafka-msg', topic=topic, msg=msg, e=e)

            # set the kafka producer to None.  This is needed if the
            # kafka docker went down and comes back up with a different
            # port number.
            if self.stopping is False:
                log.debug('stopping-kafka-proxy')
                try:
                    self.stopping = True
                    self.stop()
                    self.stopping = False
                    self.faulty = False
                    log.debug('stopped-kafka-proxy')
                except Exception, e:
                    log.exception('failed-stopping-kafka-proxy', e=e)
                    pass
            else:
                log.info('already-stopping-kafka-proxy')

            return

    def is_faulty(self):
        return self.faulty


# Common method to get the singleton instance of the kafka proxy class
def get_kafka_proxy():
    return KafkaProxy._kafka_instance

