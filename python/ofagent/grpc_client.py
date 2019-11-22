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
The gRPC client layer for the OpenFlow agent
"""
from Queue import Queue, Empty
import os
import uuid

from grpc import StatusCode
from grpc._channel import _Rendezvous
from structlog import get_logger
from twisted.internet import reactor
from twisted.internet import threads
from twisted.internet.defer import inlineCallbacks, returnValue, DeferredQueue

from voltha_protos.voltha_pb2_grpc import VolthaServiceStub
from voltha_protos.voltha_pb2 import ID, FlowTableUpdate, MeterModUpdate, \
    FlowGroupTableUpdate, PacketOut
from voltha_protos.logical_device_pb2 import LogicalPortId
from google.protobuf import empty_pb2
from binascii import hexlify


log = get_logger()


class GrpcClient(object):

    def __init__(self, connection_manager, channel, grpc_timeout, core_binding_key, core_transaction_key):

        self.connection_manager = connection_manager
        self.channel = channel
        self.grpc_timeout = grpc_timeout
        self.grpc_stub = VolthaServiceStub(channel)

        # This is the rw-core cluster to which an OFAgent is bound.
        # It is the affinity router that forwards all OFAgent
        # requests to a specific rw-core in this back-end cluster.
        self.core_group_id = ''
        self.core_group_id_key = core_binding_key

        # Since the api-router binds an OFAgent to two RW Cores in a pair and
        # transparently forward requests between the two then the onus is on
        # the OFAgent to fulfill part of the function of the api-server which
        # involves sending a transaction key to both RW Cores for the latter
        # to figure out which Core will handle the transaction. To prevent
        # collision between the api-server ID and the one from OFAgent then the
        # OFAgent ID will be prefixed with "O-".
        self.core_transaction_key = core_transaction_key

        self.stopped = False

        self.packet_out_queue = Queue()  # queue to send out PacketOut msgs
        self.packet_in_queue = DeferredQueue()  # queue to receive PacketIn
        self.change_event_queue = DeferredQueue()  # queue change events

    def start(self):
        log.debug('starting', grpc_timeout=self.grpc_timeout,
                  core_binding_key=self.core_group_id_key,
                  core_transaction_key=self.core_transaction_key)
        self.start_packet_out_stream()
        self.start_packet_in_stream()
        self.start_change_event_in_stream()
        reactor.callLater(0, self.packet_in_forwarder_loop)
        reactor.callLater(0, self.change_event_processing_loop)
        log.info('started')
        return self

    def stop(self):
        log.debug('stop requested')
        if self.stopped:
            log.debug('already stopped, no action taken')
            return
        log.debug('stopping')
        self.stopped = True
        self.connection_manager.grpc_client_terminated()
        log.info('stopped')

    def get_core_transaction_metadata(self):
        return (self.core_transaction_key, "O-" + uuid.uuid4().hex)

    def start_packet_out_stream(self):

        def packet_generator():
            while True:
                try:
                    packet = self.packet_out_queue.get(block=True, timeout=1.0)
                except Empty:
                    if self.stopped:
                        return
                else:
                    yield packet

        def stream_packets_out():
            generator = packet_generator()
            try:
                self.grpc_stub.StreamPacketsOut(generator,
                                                metadata=((self.core_group_id_key, self.core_group_id),
                                                           self.get_core_transaction_metadata(),))
            except _Rendezvous, e:
                log.error('grpc-exception', status=e.code())
                if e.code() == StatusCode.UNAVAILABLE:
                    self.stop()

        reactor.callInThread(stream_packets_out)

    def start_packet_in_stream(self):

        def receive_packet_in_stream():
            streaming_rpc_method = self.grpc_stub.ReceivePacketsIn
            iterator = streaming_rpc_method(empty_pb2.Empty(),
                                            metadata=((self.core_group_id_key, self.core_group_id),
                                                      self.get_core_transaction_metadata(),))
            try:
                for packet_in in iterator:
                    reactor.callFromThread(self.packet_in_queue.put,
                                           packet_in)
                    log.debug('enqueued-packet-in',
                              packet_in=packet_in,
                              queue_len=len(self.packet_in_queue.pending),
                              packet=hexlify(packet_in.packet_in.data))
            except _Rendezvous, e:
                log.error('grpc-exception', status=e.code())
                if e.code() == StatusCode.UNAVAILABLE:
                    self.stop()

        reactor.callInThread(receive_packet_in_stream)

    def start_change_event_in_stream(self):

        def receive_change_events():
            streaming_rpc_method = self.grpc_stub.ReceiveChangeEvents
            iterator = streaming_rpc_method(empty_pb2.Empty(),
                                            metadata=((self.core_group_id_key, self.core_group_id),
                                                      self.get_core_transaction_metadata(),))
            try:
                for event in iterator:
                    reactor.callFromThread(self.change_event_queue.put, event)
                    log.debug('enqueued-change-event',
                              change_event=event,
                              queue_len=len(self.change_event_queue.pending))
            except _Rendezvous, e:
                log.error('grpc-exception', status=e.code())
                if e.code() == StatusCode.UNAVAILABLE:
                    self.stop()

        reactor.callInThread(receive_change_events)

    @inlineCallbacks
    def change_event_processing_loop(self):
        while True:
            try:
                event = yield self.change_event_queue.get()
                device_id = event.id
                self.connection_manager.forward_change_event(device_id, event)
            except Exception, e:
                log.exception('failed-in-packet-in-handler', e=e)
            if self.stopped:
                break

    @inlineCallbacks
    def packet_in_forwarder_loop(self):
        while True:
            packet_in = yield self.packet_in_queue.get()
            device_id = packet_in.id
            ofp_packet_in = packet_in.packet_in
            log.debug('grpc client to send packet-in', packet=hexlify(packet_in.packet_in.data))
            self.connection_manager.forward_packet_in(device_id, ofp_packet_in)
            if self.stopped:
                break

    def send_packet_out(self, device_id, packet_out):
        log.debug('grpc client to send packet-out', packet=hexlify(packet_out.data))
        packet_out = PacketOut(id=device_id, packet_out=packet_out)
        self.packet_out_queue.put(packet_out)

    @inlineCallbacks
    def get_port(self, device_id, port_id):
        req = LogicalPortId(id=device_id, port_id=port_id)
        res = yield threads.deferToThread(
            self.grpc_stub.GetLogicalDevicePort, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res)

    @inlineCallbacks
    def get_port_list(self, device_id):
        req = ID(id=device_id)
        res = yield threads.deferToThread(
            self.grpc_stub.ListLogicalDevicePorts, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res.items)

    @inlineCallbacks
    def enable_port(self, device_id, port_id):
        req = LogicalPortId(
            id=device_id,
            port_id=port_id
        )
        res = yield threads.deferToThread(
            self.grpc_stub.EnableLogicalDevicePort, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res)

    @inlineCallbacks
    def disable_port(self, device_id, port_id):
        req = LogicalPortId(
            id=device_id,
            port_id=port_id
        )
        res = yield threads.deferToThread(
            self.grpc_stub.DisableLogicalDevicePort, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res)

    @inlineCallbacks
    def get_device_info(self, device_id):
        req = ID(id=device_id)
        res = yield threads.deferToThread(
            self.grpc_stub.GetLogicalDevice, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res)

    @inlineCallbacks
    def update_flow_table(self, device_id, flow_mod):
        req = FlowTableUpdate(
            id=device_id,
            flow_mod=flow_mod
        )
        res = yield threads.deferToThread(
            self.grpc_stub.UpdateLogicalDeviceFlowTable, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res)

    @inlineCallbacks
    def update_meter_mod_table(self, device_id, meter_mod):
        log.debug('In update_meter_mod_table grpc')
        req = MeterModUpdate(
            id=device_id,
            meter_mod=meter_mod
        )
        res = yield threads.deferToThread(
            self.grpc_stub.UpdateLogicalDeviceMeterTable, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        log.debug('update_meter_mod_table grpc done')
        returnValue(res)

    @inlineCallbacks
    def update_group_table(self, device_id, group_mod):
        req = FlowGroupTableUpdate(
            id=device_id,
            group_mod=group_mod
        )
        res = yield threads.deferToThread(
            self.grpc_stub.UpdateLogicalDeviceFlowGroupTable, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res)

    @inlineCallbacks
    def list_flows(self, device_id):
        req = ID(id=device_id)
        res = yield threads.deferToThread(
            self.grpc_stub.ListLogicalDeviceFlows, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res.items)

    @inlineCallbacks
    def list_groups(self, device_id):
        req = ID(id=device_id)
        res = yield threads.deferToThread(
            self.grpc_stub.ListLogicalDeviceFlowGroups, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res.items)

    @inlineCallbacks
    def list_ports(self, device_id):
        req = ID(id=device_id)
        res = yield threads.deferToThread(
            self.grpc_stub.ListLogicalDevicePorts, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res.items)

    @inlineCallbacks
    def list_logical_devices(self):
        res = yield threads.deferToThread(
            self.grpc_stub.ListLogicalDevices, empty_pb2.Empty(), timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returnValue(res.items)

    @inlineCallbacks
    def subscribe(self, subscriber):
        res, call = yield threads.deferToThread(
            self.grpc_stub.Subscribe.with_call, subscriber, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        returned_metadata = call.initial_metadata()

        # Update the core_group_id if present in the returned metadata
        if returned_metadata is None:
            log.debug('header-metadata-missing')
        else:
            log.debug('metadata-returned', metadata=returned_metadata)
            for pair in returned_metadata:
                if pair[0] == self.core_group_id_key:
                    self.core_group_id = pair[1]
                    log.debug('core-binding', core_group=self.core_group_id)
        returnValue(res)

    @inlineCallbacks
    def list_meters(self, device_id):
        log.debug('list_meters')
        req = ID(id=device_id)
        res = yield threads.deferToThread(
            self.grpc_stub.ListLogicalDeviceMeters, req, timeout=self.grpc_timeout,
            metadata=((self.core_group_id_key, self.core_group_id),
                      self.get_core_transaction_metadata(),))
        log.debug('done stat query', resp=res)
        returnValue(res.items)
