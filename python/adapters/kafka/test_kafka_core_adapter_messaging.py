#!/usr/bin/env python

import sys
from datetime import datetime
from simplejson import dumps, loads
from zope.interface import implementer
from python.protos import third_party
from kafka_proxy import KafkaProxy, get_kafka_proxy
from twisted.internet import reactor, protocol
from twisted.internet.defer import inlineCallbacks, returnValue, Deferred
from afkak.client import KafkaClient
from afkak.consumer import OFFSET_LATEST, Consumer
from twisted.internet import defer, task
from python.adapters.interface import IAdapterInterface
import structlog
import ast
import json
from python.protos.common_pb2 import LogLevel
from google.protobuf.json_format import MessageToDict, ParseDict
from google.protobuf.message import Message
from pprint import pprint
from python.protos.common_pb2 import ConnectStatus
from python.protos.device_pb2 import Device, Ports, Port
from python.protos.inter_container_pb2 import IntType, StrType, BoolType, Error
from google.protobuf.json_format import MessageToJson
from python.common.utils.deferred_utils import DeferredWithTimeout, TimeOutError
from python.common.utils import asleep
from kafka_inter_container_library import IKafkaMessagingProxy
import traceback

_ = third_party

log = structlog.get_logger()
stopped = False

@implementer(IAdapterInterface)
class IPonsimAdapter(object):
    # def __init__(self, adapter_agent, config, device_handler_class, name,
    #              vendor, version, device_type, vendor_id):
    #     log.debug(
    #         'Initializing adapter: {} {} {}'.format(vendor, name, version))
    #     self.adapter_agent = adapter_agent
    #     self.config = config
    #     self.name = name
    #     self.supported_device_types = None
    #     self.descriptor = None
    #     self.devices_handlers = dict()
    #     self.device_handler_class = device_handler_class

    def __init__(self):
        log.debug("initialized")

    def start(self):
        raise NotImplementedError()

    def stop(self):
        raise NotImplementedError()

    def adapter_descriptor(self):
        raise NotImplementedError()

    def device_types(self):
        raise NotImplementedError()

    def health(self):
        raise NotImplementedError()

    def change_master_state(self, master):
        raise NotImplementedError()

    def adopt_device(self, device):
        log.debug("adopt_device called :{}".format(device))
        return "Was adopted"

    def reconcile_device(self, device):
        raise NotImplementedError()

    def abandon_device(self, device):
        raise NotImplementedError()

    def disable_device(self, device):
        raise NotImplementedError()

    def reenable_device(self, device):
        raise NotImplementedError()

    def reboot_device(self, device):
        raise NotImplementedError()

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
        raise NotImplementedError()

    def delete_device(self, device):
        raise NotImplementedError()

    def get_device_details(self, device):
        raise NotImplementedError()

    def update_flows_bulk(self, device, flows, groups):
        raise NotImplementedError()

    def update_flows_incrementally(self, device, flow_changes, group_changes):
        log.debug("update_flows_incrementally :{} {} {}".format(device,
                                                                flow_changes, group_changes))

    def update_pm_config(self, device, pm_config):
        raise NotImplementedError()

    def send_proxied_message(self, proxy_address, msg):
        raise NotImplementedError()

    def receive_proxied_message(self, proxy_address, msg):
        raise NotImplementedError()

    def receive_packet_out(self, logical_device_id, egress_port_no, msg):
        raise NotImplementedError()

    def receive_inter_adapter_message(self, msg):
        raise NotImplementedError()

    def suppress_alarm(self, filter):
        raise NotImplementedError()

    def unsuppress_alarm(self, filter):
        raise NotImplementedError()

    def create_interface(self, device, data):
        raise NotImplementedError()

    def update_interface(self, device, data):
        raise NotImplementedError()

    def remove_interface(self, device, data):
        raise NotImplementedError()

    def receive_onu_detect_state(self, proxy_address, state):
        raise NotImplementedError()

    def create_tcont(self, device, tcont_data, traffic_descriptor_data):
        raise NotImplementedError()

    def update_tcont(self, device, tcont_data, traffic_descriptor_data):
        raise NotImplementedError()

    def remove_tcont(self, device, tcont_data, traffic_descriptor_data):
        raise NotImplementedError()

    def create_gemport(self, device, data):
        raise NotImplementedError()

    def update_gemport(self, device, data):
        raise NotImplementedError()

    def remove_gemport(self, device, data):
        raise NotImplementedError()

    def create_multicast_gemport(self, device, data):
        raise NotImplementedError()

    def update_multicast_gemport(self, device, data):
        raise NotImplementedError()

    def remove_multicast_gemport(self, device, data):
        raise NotImplementedError()

    def create_multicast_distribution_set(self, device, data):
        raise NotImplementedError()

    def update_multicast_distribution_set(self, device, data):
        raise NotImplementedError()

    def remove_multicast_distribution_set(self, device, data):
        raise NotImplementedError()

    def _get_handler(self, device):
        raise NotImplementedError()


@defer.inlineCallbacks
def confluentProducer():
    from kafka_proxy import get_kafka_proxy
    try:
        proxy = get_kafka_proxy()
        if proxy == None:
            KafkaProxy(kafka_endpoint='10.176.215.229:9092').start()

        while True:
            message = {}
            message['rpc'] = "adopt_me"
            message['deviceid'] = "heres my id"
            send_kafka_message("testTopic1", dumps(message))
            yield asleep.asleep(1)
            if stopped:
                break
    except Exception as e:
        print "Exception do beta"
        print e.message



@defer.inlineCallbacks
def confluentConsumer():
    from twisted.internet.threads import deferToThread
    from confluent_kafka import Consumer, KafkaError

    try:
        c = Consumer({
            'bootstrap.servers': '10.176.215.229:9092',
            'group.id': 'testgroup',
            'auto.offset.reset': 'earliest'
        })

        c.subscribe(['testTopic'])

        while True:
            msg = yield deferToThread(c.poll)
            # msg = yield c.poll(1.0)

            if msg is None:
                continue
            if msg.error():
                print("Consumer error: {}".format(msg.error()))
                continue

            print('Received message: {}'.format(msg.value().decode('utf-8')))

        c.close()

    except Exception, e:
        print "Ala mo la"
        print e.message


def consumeForAdapter():
    proxy = get_kafka_proxy()
    if proxy == None:
        KafkaProxy(kafka_endpoint='10.176.215.229:9092').start()
        proxy = get_kafka_proxy()
    proxy.subscribe('ponsim_olt', myConfluentCallback1, 'testGroup1')


def consumeForAdapterWithMsgProxy():
    # proxy = get_kafka_proxy()
    # if proxy == None:
    #     KafkaProxy(kafka_endpoint='172.20.10.3:9092').start()
    #     proxy = get_kafka_proxy()

    ponsim_request_handler = IPonsimAdapter()
    mspProxy = IKafkaMessagingProxy(
            kafka_host_port="10.176.215.229:9092",
            # TODO: Add KV Store object reference
            kv_store='etcd',
            default_topic='ponsim_olt',
            group_id_prefix="hello",
            # Needs to assign a real class
            target_cls=ponsim_request_handler
        )
    mspProxy.start()

@inlineCallbacks
def stopMsgProxy():
    global stopped
    stopped = True
    proxy = get_kafka_proxy()
    if proxy == None:
        KafkaProxy(kafka_endpoint='10.176.215.229:9092').start()
        proxy = get_kafka_proxy()
    yield proxy.unsubscribe('ponsim_olt',myConfluentCallback1)
    # asleep.asleep(1)
    proxy.stop()

@inlineCallbacks
def consumeViaProxy():
    proxy = get_kafka_proxy()
    if proxy == None:
        KafkaProxy(kafka_endpoint='10.176.215.229:9092').start()
        proxy = get_kafka_proxy()
    yield proxy.subscribe('testTopic1', myConfluentCallback1, 'testGroup1')
    yield proxy.subscribe('testTopic1', myConfluentCallback1, 'testGroup1')
    yield proxy.subscribe('testTopic2', myConfluentCallback2, 'testGroup2')
    yield proxy.subscribe('testTopic3', myConfluentCallback3, 'testGroup3')

@inlineCallbacks
def stopProxy():
    global stopped
    stopped = True
    proxy = get_kafka_proxy()
    yield proxy.unsubscribe('testTopic1',myConfluentCallback1)
    yield proxy.unsubscribe('testTopic2',myConfluentCallback2)
    yield proxy.unsubscribe('testTopic3',myConfluentCallback3)
    yield proxy.unsubscribe('testTopic1',myConfluentCallback1)
    # asleep.asleep(1)
    yield proxy.stop()

def myConfluentCallback1(msg):
    print('Received callback 1 message: {}'.format(msg.value().decode('utf-8')))


def myConfluentCallback2(msg):
    print('Received callback 2 message: {}'.format(msg.value().decode('utf-8')))

def myConfluentCallback3(msg):
    print('Received callback 3 message: {}'.format(msg.value().decode('utf-8')))


@defer.inlineCallbacks
def consume():
    try:
        print "I am here -1"
        topic = "khentopic"
        client = KafkaClient('10.100.198.220:9092')
        print "I am here -2"
        target = IPonsimAdapter()

        e = True
        while e:
            yield client.load_metadata_for_topics(topic)
            e = client.metadata_error_for_topic(topic)
            if e:
                print "Need to retry"

        partitions = client.topic_partitions[topic]
        print "I am here -3"

        def process(reactor, message_list):
            """
            This function is called for every batch of messages received from
            Kafka. It may return a Deferred, but this implementation just logs the
            messages received.
            """
            for m in message_list:
                log.debug("Got Message :{}".format(m))
                print m.message.value
                val = m.message.value
                jval = json.loads(val)
                import unicodedata
                res = unicodedata.normalize('NFKD', jval).encode('ascii',
                                                                 'ignore')
                val = ast.literal_eval(res)
                log.debug("Val :{}".format(val))
                log.debug("Val[rpc] :{}".format(val['rpc']))
                log.debug("Val[kw] :{}".format(val['kw']))
                res = getattr(target, val['rpc'])(**val['kw'])
                log.debug("Result produced :{}".format(res))
                # Now return a result
                #     return res
                topic = "abcdef1234"
                send_kafka_message(topic, dumps(res))

        print "I am here 0"

        consumers = [Consumer(client, topic, partition, process)
                     for partition in partitions]

        print "I am here 1"

        def cb_closed(result):
            """
            Called when a consumer cleanly stops.
            """
            print "Consumer stopped"

        def eb_failed(failure):
            """
            Called when a consumer fails due to an uncaught exception in the
            processing callback or a network error on shutdown. In this case we
            simply log the error.
            """
            print "Consumer failed: %s".format(failure)
            print failure

        def stop_consumers():
            print "Time is up, stopping consumers..."
            d = defer.gatherResults([c.stop() for c in consumers])
            d.addCallback(lambda result: client.close())
            return d

        print "I am here 2"

        # yield defer.gatherResults(
        #     [c.start(OFFSET_LATEST).addCallbacks(cb_closed, eb_failed) for c in consumers]
        # )

        yield defer.gatherResults(
            [c.start(OFFSET_LATEST).addCallbacks(cb_closed, eb_failed) for c in consumers]
            + [task.deferLater(reactor, 60.0, stop_consumers)]
        )

        print "I am here 3"

    except Exception, e:
        print "Ala mo la"
        print e.message


def process_kafka_messages():
    halted = False
    try:
        log.info('process_kafka_messages')
        #1 yield until a message is received from any registered topic

    except Exception, e:
        log.exception('process_kafka_messages', e=e)
        # to prevent flood
        # yield asleep(self.members_tracking_sleep_to_prevent_flood)
    finally:
        if not halted:
            reactor.callLater(1, process_kafka_messages)


@defer.inlineCallbacks
def wait_until_topic_is_ready(client, topic):
    e = True
    while e:
        yield client.load_metadata_for_topics(topic)
        e = client.metadata_error_for_topic(topic)
        if e:
            print "Need to retry"


@defer.inlineCallbacks
def wait_for_response(topic, m_func):
    client = KafkaClient('10.100.198.220:9092')
    yield wait_until_topic_is_ready(client, topic)
    partitions = client.topic_partitions[topic]

    def process(reactor, message_list):
        for m in message_list:
            log.debug("Received Response :{}".format(m))
            print m.message.value
            val = m.message.value
            jval = json.loads(val)
            import unicodedata
            res = unicodedata.normalize('NFKD', jval).encode('ascii',
                                                             'ignore')
            val = ast.literal_eval(res)
            log.debug("Val :{}".format(val))
            # print res
            return val

    consumer = Consumer(client, topic, partitions[0], m_func)

    def cb_closed(result):
        """
        Called when a consumer cleanly stops.
        """
        print "Consumer stopped"

    def eb_failed(failure):
        """
        Called when a consumer fails due to an uncaught exception in the
        processing callback or a network error on shutdown. In this case we
        simply log the error.
        """
        print "Consumer failed: %s".format(failure)
        print failure

    def stop_consumers():
        print "Time is up, stopping consumers..."
        d = defer.gatherResults(consumer.stop())
        d.addCallback(lambda result: client.close())
        return d

    log.debug("before starting ....")
    try:
        consumer.start(OFFSET_LATEST).addCallbacks(cb_closed, eb_failed)

        # yield defer.gatherResults(
        #     consumer.start(OFFSET_LATEST).addCallbacks(cb_closed, eb_failed)
        # # log.debug("wait_for_response returns :{}".format(res))
        #     + task.deferLater(reactor, 60.0, stop_consumers))
        # # defer.returnValue(res)
    except Exception as e:
        log.exception("Eta gogot:", e=e)


def send_kafka_message(topic, msg):
    try:
        kafka_proxy = get_kafka_proxy()
        if kafka_proxy == None:
            KafkaProxy(kafka_endpoint='10.176.212.108:9092').start()

        kafka_proxy = get_kafka_proxy()
        if kafka_proxy:
            kafka_proxy.send_message(topic, dumps(msg))
        else:
            print "kafka-proxy-unavailable"
    except Exception, e:
        print e

def send_message_api(rpc, m_callback=None, deviceId=None,
                     device=None, topic=None, *args,
                     **kwargs):
    log.debug("send_message_api :{} {} {} {}".format(deviceId, rpc, topic,
                                                     device))
    message={}
    message['rpc'] = rpc
    message['deviceid'] = deviceId
    message['args'] = [a for a in args]
    message['kw'] = {a:b for a, b in kwargs.iteritems()}
    send_kafka_message(topic, dumps(message))

@defer.inlineCallbacks
def send_message(rpc, m_callback=None, topic=None, **kwargs):

    wait_for_result = Deferred()

    def local_func(reactor, message_list):
        for m in message_list:
            log.debug("send_message - Received Response :{}".format(m))
            print m.message.value
            val = m.message.value
            jval = json.loads(val)
            import unicodedata
            res = unicodedata.normalize('NFKD', jval).encode('ascii',
                                                             'ignore')
            val = ast.literal_eval(res)
            log.debug("Val :{}".format(val))
            wait_for_result.callback(val)
            # print res
            # return val

    log.debug("send_message :{} {} {}".format(rpc, topic, kwargs ))
    message={}
    message['rpc'] = rpc
    for a, b in kwargs.iteritems():
        print a, b
    message['kw'] = {a:b for a, b in kwargs.iteritems()}
    # message['kw'] = {'device': 1}
    k = dumps(message)
    pprint(k)
    jk = json.loads(k)
    pprint(jk)
    print jk['rpc']
    send_kafka_message(topic, dumps(message))
    topic="abcdef1234"
    try:
        log.debug("before waiting for response")
        res = yield wait_for_response(topic, local_func)
        log.debug("after waiting for response")
        res = yield wait_for_result
        log.debug("send_message - received result:{}".format(res))
        returnValue(res)
    except Exception as e:
        log.debug("Exception:{}".format(e))


def _dict_test():
    m_dict = {}
    m_dict['lolo'] = [1,2,3,4]
    m_dict['lolo'].extend([5,6,7])
    print m_dict

def start():
    message={}
    topic = "khentopic"
    message['rpc'] = "adopt_device"
    message['args'] = {'device': 1}
    send_kafka_message(topic, message)


def trap_exception(func):
    def wrapper(*args, **kw):
        try:
            return func(*args, **kw)
        except Exception as e:
            log.exception("Failure", func=func, e=e)
            return False, e
    return wrapper

@trap_exception
def get_device(val):
    return 10/val

def _to_proto(**kwargs):
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

def printkw(**kwargs):
    print kwargs

def prototest():
    conn = ConnectStatus.UNREACHABLE
    print type(conn), conn
    k = Device(id="nursimulu", proxy_address=Device.ProxyAddress(
        device_id="nursimulu",
        channel_id=30))
    topic = 'tx:' + MessageToJson(k.proxy_address)
    print topic

    # res = _to_proto(d=k, s="12234", i=232, b=True, f=Device())
    # printkw(**res)


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
                    log.warn("Unsuccessful-request", func=func, args=args,
                             kw=kw)
                    returnValue(d)
            except Exception as e:
                log.exception("Exception-", func=func, e=e)
                raise
        return wrapper
    return real_wrapper

@inlineCallbacks
def test_example():
    yield asleep.asleep(2)
    raise Exception('oops call')
    # returnValue(10)
    # err = Error()
    # err.reason = "Failure"
    # returnValue((False, err))

from twisted.python import failure, util

@inlineCallbacks
def invoke(rpc, to_topic=None, **kwargs):
    @inlineCallbacks
    def _send_request(rpc, m_callback,to_topic, **kwargs):
        result = ((False, None))
        try:
            log.debug("sending-request", kafka_proxy="test")
            if to_topic is None:
                to_topic = "Core"
            result = yield test_example()
            if not m_callback.called:
                log.info('trigger-callback-to-cancel-timout-timer')
                m_callback.callback(result)
            else:
                log.debug('timeout-already-occurred')
        except Exception as e:
            log.exception("Failure-sending-requester", rpc=rpc, kw=kwargs, e=e)
            if not m_callback.called:
                m_callback.errback(failure.Failure())

        # finally:
        #     if not m_callback.called:
        #         log.info('finally-trigger-callback-if-not-done')
        #         m_callback.callback(result)
        #     else:
        #         log.debug('callback-already-invoked')

    log.debug('invoke-request')
    cb = DeferredWithTimeout(timeout=1)
    # _send_request(rpc, cb, to_topic, **kwargs)
    try:
        _send_request(rpc, cb, to_topic, **kwargs)

        res = yield cb
        log.info('invoke-request-after-result', called=cb.called)
        # returnValue((True, res))
        # (success, result) = res
        # if not success:
        #     log.info('not success', result=result)
        #     raise KafkaMessagingError(error=result.reason)
        # else:
        #     returnValue(res)
        returnValue(res)
    except TimeOutError as e:
        log.info('invoke-timeout', e=e)
        # err = Error()
        # err.reason = "Timeout"
        # returnValue((False, err))
        raise
        # returnValue((False, None))
        # raise KafkaMessagingError(error=e)
        # raise e
    # except Exception as e:
    #     log.debug('invoke-request-exception')
    #     raise
    # finally:
    #     log.debug('invoke-request-done')
    #     (a,b) = res
    #     if not a:
    #         raise KafkaMessagingError(error=b)
    log.debug('invoke-request-done')





@wrap_request(None)
@inlineCallbacks
def hello(name):
    log.debug("hello")
    proto_str = StrType()
    proto_str.val = name
    try:
        res = yield invoke(rpc="Hello", name=proto_str)
        log.info("hello-returned", res=res)
        returnValue(res)
    except Exception as e:
        # traceback.print_exc(file=sys.stdout)
        log.exception("hello-exception", e=e)
        raise


    # (a,b) = res
    # if not a:
    #     raise KafkaMessagingError(b.reason)
    # else:
    #     returnValue(res)

import gc

@inlineCallbacks
def start_hello():
    try:
        log.info("start_hello")
        res = yield hello("khen")
        log.info("RESPONSE", res=res)
    except Exception as e:
        log.info("Got exception", e=e)


def start_trial():
    def callback(res):
        raise Exception('oops call')

    d = Deferred()
    d.addCallback(callback)
    d.callback('Here is your result.')
    d = None
    gc.collect()
    print "Finished"

@inlineCallbacks
def test_consul():
    import consul
    try:
        log.info("start")
        c = consul.Consul(host="2353", port="632827")
        session_id = yield c.session.create(
            behavior='release', ttl=10,
            lock_delay=1)
        log.info("session_id", id=session_id)
    except Exception as e:
        traceback.print_exc(file=sys.stdout)
        log.exception("Exception amigo",e)


class TestTwistedVariable(object):
    def __init__(self, a, b):
        self.a = a
        self.b = b
        self.c = None

    @inlineCallbacks
    def changeData(self):
        self.c = 1
        yield asleep.asleep(5)
        print "done with change"

    def getData(self):
        return self.c

@inlineCallbacks
def testTwistedVariableInit():
    try:
        t = TestTwistedVariable(1,2)
        reactor.callLater(0, t.changeData)
        yield asleep.asleep(0)
        print t.getData()
    except Exception as e:
        print e

@inlineCallbacks
def testTwistedReturnedValue():
    ports = Ports(items=[Port(port_no=1)])
    yield asleep.asleep(2)
    returnValue(ports)

@inlineCallbacks
def testUseReturnedValue():
    try:
        # yield asleep.asleep(1)
        b = yield testTwistedReturnedValue()
        returnValue(b)
    except Exception as e:
        print e

@inlineCallbacks
def testUseReturnedValue1():
    try:
        # yield asleep.asleep(1)
        b = yield testTwistedReturnedValue()
        print b.items[0]
    except Exception as e:
        print e

# this connects the protocol to a server runing on port 8000
def main():
    # reactor.callLater(1, start)
    # m_device = device_pb2.Device()
    # m_device.id = "abcdef1234567890"
    # m_device.adapter = "ponsim_adapter"
    # _dict_test()

    # prototest()
    # print get_device(0)
    # deferred_list = []
    # if deferred_list:
    #     print "I am here if"
    # else:
    #     print "I am here else"
    # flows = FlowChanges()
    # print len(flows.to_add.items)
    # print len(flows.to_remove.items)
    #
    # if len(flows.to_remove.items) == 0 and len(
    #         flows.to_add.items) == 0:
    #     print "ok"
    #
    # one = ofp_flow_stats()
    # one.id = 123
    # # two = Flows(items=[one])
    # flows.to_add.items.extend([one])
    # print len(flows.to_add.items)
    # print flows
    # if len(flows.to_remove.items) == 0 or len(
    #         flows.to_add.items) == 0:
    #     print "ok"

    # reactor.callLater(1, send_message, "adopt_device", topic="khentopic",
    #                   device="1000")
    # # reactor.callLater(1, send_message, "update_flows_incrementally", topic="khentopic",
    # #                   device="2000", flow_changes="flow",
    # #                   group_changes="group")
    # reactor.callLater(0, consume)
    # reactor.callLater(0, start_hello)
    # reactor.callLater(0, test_consul)

    # reactor.callLater(0, testUseReturnedValue1)
    reactor.callLater(0, consumeViaProxy)
    reactor.callLater(1, confluentProducer)
    reactor.callLater(5, stopProxy)

    # reactor.callLater(0, consumeForAdapterWithMsgProxy)
    # # reactor.callLater(1, confluentProducer)
    # # reactor.callLater(5, stopMsgProxy)

    reactor.run()


# this only runs if the module was *not* imported
if __name__ == '__main__':
    main()

