/*
 * Copyright 2018-present Open Networking Foundation
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ofagent

import (
	"github.com/golang-collections/go-datastructures/queue"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	//"bytes"
	//"strings"
	//"strconv"
	"fmt"
	//"os"
	//"crypto/tls"
)

type GrpcClient struct {
	connection_manager ConnectionManager
	channel            *grpc.ClientConn
	grpc_timeout       int
	local_stub         int
	core_group_id      string
	core_group_id_key  string
	stopped            bool
	// PriorityQueue or Queue
	packet_out_queue   *queue.Queue
	packet_in_queue    *queue.Queue
	change_event_queue *queue.Queue
}

func NewGrpcClient(connection_manager ConnectionManager, channel *grpc.ClientConn, grpc_timeout int, core_binding_key string) *GrpcClient {
	var grpcCli GrpcClient

	grpcCli.connection_manager = connection_manager
	grpcCli.channel = channel
	grpcCli.grpc_timeout = grpc_timeout
	grpcCli.local_stub = 0 // VolthaServiceStub(channel)

	// This is the rw-core cluster to which an OFAgent is bound.
	// It is the affinity router that forwards all OFAgent
	// requests to a specific rw-core in this back-end cluster.

	grpcCli.core_group_id = ""
	grpcCli.core_group_id_key = core_binding_key

	grpcCli.stopped = false

	grpcCli.packet_out_queue = queue.New(64)   // queue to send out PacketOut msgs
	grpcCli.packet_in_queue = queue.New(64)    // queue to receive PacketIn
	grpcCli.change_event_queue = queue.New(64) // queue change events

	fmt.Println("NewGrpcClient")
	//log.Debugln("NewGrpcClient", grpcCli)

	return &grpcCli
}

func (e GrpcClient) start() GrpcClient {
	fmt.Println("start()")
	//log.Debugln("start()", e)

	e.start_packet_out_stream()
	e.start_packet_in_stream()
	e.start_change_event_in_stream()
	go e.packet_in_forwarder_loop()
	go e.change_event_processing_loop()
	//log.Debugln("started", e)
	return e
}

func (e GrpcClient) stop() {
	log.Debugln("stop()", e)
	e.stopped = true
	log.Debugln("stopped", e)
}

func (e GrpcClient) start_packet_out_stream() {
	log.Debugln("start_packet_out_stream()", e)
}

func (e GrpcClient) packet_generator() {
	log.Debugln("packet_generator()", e)
	for 1 == 1 {
		//packet []interface
		//packet = e.packet_out_queue.Get(1)			//block=True, timeout=1.0)
		if true == e.stopped {
			return
		}
		// yield equiv in go?
		//	yield packet
	}
}

func (e GrpcClient) stream_packets_out() {
	//generator = e.packet_generator()
	//try:
	//e.local_stub.StreamPacketsOut(generator, metadata=((e.core_group_id_key, e.core_group_id), ))
	//except _Rendezvous, e:
	//    log.error('grpc-exception', status=e.code())
	//    if e.code() == StatusCode.UNAVAILABLE:
	//        os.system("kill -15 {}".format(os.getpid()))
	go e.stream_packets_out()
}

func (e GrpcClient) start_packet_in_stream() {
	log.Debugln("start_packet_in_stream()", e)

	go e.receive_packet_in_stream()
}

func (e GrpcClient) receive_packet_in_stream() {
	log.Debugln("receive_packet_in_stream()", e)
	/*
	   streaming_rpc_method = e.local_stub.ReceivePacketsIn
	   iterator = streaming_rpc_method(empty_pb2.Empty(),
	                                           metadata=((e.core_group_id_key, e.core_group_id),))
	   try:
	      for packet_in in iterator:
	        reactor.callFromThread(e.packet_in_queue.put,
	                                          packet_in)
	       log.debug('enqueued-packet-in',
	                             packet_in=packet_in,
	                             queue_len=len(e.packet_in_queue.pending))
	      except _Rendezvous, e:
	               log.error('grpc-exception', status=e.code())
	               if e.code() == StatusCode.UNAVAILABLE:
	                   os.system("kill -15 {}".format(os.getpid()))
	*/
}

func (e GrpcClient) start_change_event_in_stream() {
	log.Debugln("start_packet_in_stream()", e)

	go e.receive_change_events()
}

func (e GrpcClient) receive_change_events() {
	log.Debugln("receive_change_events()", e)
	//streaming_rpc_method = e.local_stub.ReceiveChangeEvents
	//iterator = streaming_rpc_method(empty_pb2.Empty(),
	//                                        metadata=((e.core_group_id_key, e.core_group_id),))
	// try:
	//    for event in iterator:
	//       reactor.callFromThread(e.change_event_queue.put, event)
	//       log.debug('enqueued-change-event',
	//                           change_event=event,
	//                           queue_len=len(e.change_event_queue.pending))
	// except _Rendezvous, e:
	//    log.error('grpc-exception', status=e.code())
	//    if e.code() == StatusCode.UNAVAILABLE:
	//                 os.system("kill -15 {}".format(os.getpid()))
}

func (e GrpcClient) change_event_processing_loop() {

	log.Debugln("change_event_processing_loop()", e)
	for {
		//try:
		//    event = yield e.change_event_queue.get()
		//    device_id = event.id
		//      e.connection_manager.forward_change_event(device_id, event)
		//except Exception, e:
		//    log.exception('failed-in-packet-in-handler', e=e)
		if true == e.stopped {
			break
		}
	}
}

func (e GrpcClient) packet_in_forwarder_loop() {
	log.Debugln("packet_in_forwarder_loop()", e)
	for {
		//packet_in = yield e.packet_in_queue.get()
		//packet_in = e.packet_in_queue.Get(1)
		//device_id = packet_in.id
		//ofp_packet_in = packet_in.packet_in
		//e.connection_manager.forward_packet_in(device_id, ofp_packet_in)
		if true == e.stopped {
			break
		}
	}
}

func (e GrpcClient) send_packet_out(device_id string, packet_out string) {
	// need a PacketOut data type
	//packet_out = PacketOut(id=device_id, packet_out=packet_out)
	//e.packet_out_queue.Put(packet_out)
}

func (e GrpcClient) get_port(device_id string, port_id int) {
	//        req = LogicalPortId(id=device_id, port_id=port_id)
	//        res = yield threads.deferToThread(
	///            e.local_stub.GetLogicalDevicePort, req, timeout=e.grpc_timeout,
	//            metadata=((e.core_group_id_key, e.core_group_id),))
	//        returnValue(res)
}

func (e GrpcClient) get_port_list(device_id string) {
	//req = ID(id=device_id)
	//res = yield threads.deferToThread(
	//    e.local_stub.ListLogicalDevicePorts, req, timeout=e.grpc_timeout,
	//    metadata=((e.core_group_id_key, e.core_group_id),))
	///returnValue(res.items)
}

//@inlineCallbacks
func (e GrpcClient) enable_port(device_id string, port_id int) {
	//req := LogicalPortId(device_id, port_id)
	//res = yield threads.deferToThread(
	//    e.local_stub.EnableLogicalDevicePort, req, timeout=e.grpc_timeout,
	//    metadata=((e.core_group_id_key, e.core_group_id),))
	//returnValue(res)
}

//@inlineCallbacks
func (e GrpcClient) disable_port(device_id string, port_id int) {
	//req = LogicalPortId(device_id, port_id)
	//res = yield threads.deferToThread(
	//e.local_stub.DisableLogicalDevicePort, req, timeout=e.grpc_timeout,
	//       metadata=((e.core_group_id_key, e.core_group_id),))
	//returnValue(res)
}

//@inlineCallbacks
func (e GrpcClient) get_device_info(device_id string) {
	//req = ID(device_id)
	//res = yield threads.deferToThread(e.local_stub.GetLogicalDevice, req, timeout=e.grpc_timeout,
	//        metadata=((e.core_group_id_key, e.core_group_id),))
	//returnValue(res)
}

//@inlineCallbacks
func (e GrpcClient) update_flow_table(device_id string, flow_mod int) {
	//req = FlowTableUpdate(device_id, flow_mod)
	//res = yield threads.deferToThread(
	//    e.local_stub.UpdateLogicalDeviceFlowTable, req, timeout=e.grpc_timeout,
	//    metadata=((e.core_group_id_key, e.core_group_id),))
	//returnValue(res)
}

//@inlineCallbacks
func (e GrpcClient) update_group_table(device_id string, group_mod int) {

	//   req = FlowGroupTableUpdate( id=device_id, group_mod=group_mod)
	//   res = yield threads.deferToThread(
	//       e.local_stub.UpdateLogicalDeviceFlowGroupTable, req, timeout=e.grpc_timeout,
	//       metadata=((e.core_group_id_key, e.core_group_id),))
	//   returnValue(res)
}

//@inlineCallbacks
func (e GrpcClient) list_flows(device_id string) {
	//    req = ID(id=device_id)
	//    res = yield threads.deferToThread(
	//        e.local_stub.ListLogicalDeviceFlows, req, timeout=e.grpc_timeout,
	//        metadata=((e.core_group_id_key, e.core_group_id),))
	//    returnValue(res.items)
}

//@inlineCallbacks
func (e GrpcClient) list_groups(device_id string) {
	//     req = ID(id=device_id)
	//     res = yield threads.deferToThread(
	//         e.local_stub.ListLogicalDeviceFlowGroups, req, timeout=e.grpc_timeout,
	//         metadata=((e.core_group_id_key, e.core_group_id),))
	//     returnValue(res.items)
}

//@inlineCallbacks
func (e GrpcClient) list_ports(device_id string) {
	//    req = ID(id=device_id)
	//    res = yield threads.deferToThread(
	//        e.local_stub.ListLogicalDevicePorts, req, timeout=e.grpc_timeout,
	//        metadata=((e.core_group_id_key, e.core_group_id),))
	//    returnValue(res.items)
}

//@inlineCallbacks
func (e GrpcClient) list_logical_devices() {
	//    res = yield threads.deferToThread(
	//        e.local_stub.ListLogicalDevices, empty_pb2.Empty(), timeout=e.grpc_timeout,
	//        metadata=((e.core_group_id_key, e.core_group_id), ))
	//    returnValue(res.items)
}

//@inlineCallbacks
func (e GrpcClient) subscribe(subscriber int) int {
	fmt.Println("GrpcClient::subscribe")

	//     res, call = yield threads.deferToThread(
	//         e.local_stub.Subscribe.with_call, subscriber, timeout=e.grpc_timeout,
	//         metadata=((e.core_group_id_key, e.core_group_id), ))
	//     returned_metadata = call.initial_metadata()

	// Update the core_group_id if present in the returned metadata
	//     if returned_metadata is None:
	//         log.debug('header-metadata-missing')
	//     else:
	//         log.debug('metadata-returned', metadata=returned_metadata)
	//         for pair in returned_metadata:
	//             if pair[0] == e.core_group_id_key:
	//                 e.core_group_id = pair[1]
	//                 log.debug('core-binding', core_group=e.core_group_id)
	//     returnValue(res)
	return 1
}
