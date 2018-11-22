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
The superclass for all kafka proxy subclasses.
"""

import structlog
from twisted.internet.defer import inlineCallbacks, returnValue
from twisted.python import failure
from zope.interface import implementer

from python.common.utils.deferred_utils import DeferredWithTimeout, \
    TimeOutError
from python.common.utils.registry import IComponent

log = structlog.get_logger()


class KafkaMessagingError(BaseException):
    def __init__(self, error):
        self.error = error


@implementer(IComponent)
class ContainerProxy(object):

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

    @classmethod
    def wrap_request(cls, return_cls):
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
                        log.warn("unsuccessful-request", func=func, args=args,
                                 kw=kw)
                        returnValue(d)
                except Exception as e:
                    log.exception("request-wrapper-exception", func=func, e=e)
                    raise

            return wrapper

        return real_wrapper

    @inlineCallbacks
    def invoke(self, rpc, to_topic=None, reply_topic=None, **kwargs):
        @inlineCallbacks
        def _send_request(rpc, m_callback, to_topic, reply_topic, **kwargs):
            try:
                log.debug("sending-request",
                          rpc=rpc,
                          to_topic=to_topic,
                          reply_topic=reply_topic)
                if to_topic is None:
                    to_topic = self.core_topic
                if reply_topic is None:
                    reply_topic = self.listening_topic
                result = yield self.kafka_proxy.send_request(rpc=rpc,
                                                             to_topic=to_topic,
                                                             reply_topic=reply_topic,
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

        # We are going to resend the request on the to_topic if there is a
        # timeout error. This time the timeout will be longer.  If the second
        # request times out then we will send the request to the default
        # core_topic.
        timeouts = [self.default_timeout,
                    self.default_timeout*2,
                    self.default_timeout]
        retry = 0
        max_retry = 2
        for timeout in timeouts:
            cb = DeferredWithTimeout(timeout=timeout)
            _send_request(rpc, cb, to_topic, reply_topic, **kwargs)
            try:
                res = yield cb
                returnValue(res)
            except TimeOutError as e:
                log.warn('invoke-timeout', e=e)
                if retry == max_retry:
                    raise e
                retry += 1
                if retry == max_retry:
                    to_topic = self.core_topic
