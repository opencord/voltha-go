/*
 * Copyright 2020-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package event

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/utils"
	ev "github.com/opencord/voltha-lib-go/v4/pkg/events"
	"github.com/opencord/voltha-lib-go/v4/pkg/events/eventif"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/opentracing/opentracing-go"
	jtracing "github.com/uber/jaeger-client-go"
)

type Manager struct {
	packetInQueue        chan openflow_13.PacketIn
	packetInQueueDone    chan bool
	changeEventQueue     chan openflow_13.ChangeEvent
	changeEventQueueDone chan bool
	Agent                *Agent
}

type Agent struct {
	eventProxy     eventif.EventProxy
	coreInstanceID string
	stackID        string
}

func NewManager(proxyForEvents eventif.EventProxy, instanceID string, stackID string) *Manager {
	return &Manager{
		packetInQueue:        make(chan openflow_13.PacketIn, 100),
		packetInQueueDone:    make(chan bool, 1),
		changeEventQueue:     make(chan openflow_13.ChangeEvent, 100),
		changeEventQueueDone: make(chan bool, 1),
		Agent:                NewAgent(proxyForEvents, instanceID, stackID),
	}
}

func NewAgent(proxyForEvents eventif.EventProxy, instanceID string, stackID string) *Agent {
	return &Agent{
		eventProxy:     proxyForEvents,
		coreInstanceID: instanceID,
		stackID:        stackID,
	}
}
func (q *Manager) SendPacketIn(ctx context.Context, deviceID string, transationID string, packet *openflow_13.OfpPacketIn) {
	// TODO: Augment the OF PacketIn to include the transactionId
	packetIn := openflow_13.PacketIn{Id: deviceID, PacketIn: packet}
	logger.Debugw(ctx, "send-packet-in", log.Fields{"packet-in": packetIn})
	q.packetInQueue <- packetIn
}

type callTracker struct {
	failedPacket interface{}
}
type streamTracker struct {
	calls map[string]*callTracker
	sync.Mutex
}

var streamingTracker = &streamTracker{calls: make(map[string]*callTracker)}

func (q *Manager) getStreamingTracker(ctx context.Context, method string, done chan<- bool) *callTracker {
	streamingTracker.Lock()
	defer streamingTracker.Unlock()
	if _, ok := streamingTracker.calls[method]; ok {
		// bail out the other packet in thread
		logger.Debugf(ctx, "%s-streaming-call-already-running-exiting-it", method)
		done <- true
		logger.Debugf(ctx, "last-%s-exited-continuing", method)
	} else {
		streamingTracker.calls[method] = &callTracker{failedPacket: nil}
	}
	return streamingTracker.calls[method]
}

func (q *Manager) flushFailedPackets(ctx context.Context, tracker *callTracker) error {
	if tracker.failedPacket != nil {
		switch failedPacket := tracker.failedPacket.(type) {
		case openflow_13.PacketIn:
			logger.Debug(ctx, "enqueueing-last-failed-packet-in")
			q.packetInQueue <- failedPacket
		case openflow_13.ChangeEvent:
			logger.Debug(ctx, "enqueueing-last-failed-change-event")
			q.changeEventQueue <- failedPacket
		}
	}
	return nil
}

// ReceivePacketsIn receives packets from adapter
func (q *Manager) ReceivePacketsIn(_ *empty.Empty, packetsIn voltha.VolthaService_ReceivePacketsInServer) error {
	ctx := context.Background()
	ctx = utils.WithRPCMetadataContext(ctx, "ReceivePacketsIn")
	var streamingTracker = q.getStreamingTracker(ctx, "ReceivePacketsIn", q.packetInQueueDone)
	logger.Debugw(ctx, "receive-packets-in-request", log.Fields{"packets-in": packetsIn})

	err := q.flushFailedPackets(ctx, streamingTracker)
	if err != nil {
		logger.Errorw(ctx, "unable-to-flush-failed-packets", log.Fields{"error": err})
	}

loop:
	for {
		select {
		case packet := <-q.packetInQueue:
			logger.Debugw(ctx, "sending-packet-in", log.Fields{
				"packet": hex.EncodeToString(packet.PacketIn.Data),
			})
			if err := packetsIn.Send(&packet); err != nil {
				logger.Errorw(ctx, "failed-to-send-packet", log.Fields{"error": err})
				go q.Agent.GetAndSendRPCEvent(ctx, packet.Id, err.Error(),
					nil, "RPC_ERROR_RAISE_EVENT", voltha.EventCategory_COMMUNICATION,
					nil, time.Now().Unix())
				// save the last failed packet in
				streamingTracker.failedPacket = packet
			} else if streamingTracker.failedPacket != nil {
				// reset last failed packet saved to avoid flush
				streamingTracker.failedPacket = nil
			}
		case <-q.packetInQueueDone:
			logger.Debug(ctx, "another-receive-packets-in-running-bailing-out")
			break loop
		}
	}

	//TODO: Find an elegant way to get out of the above loop when the Core is stopped
	return nil
}

func (q *Manager) SendChangeEvent(ctx context.Context, deviceID string, reason openflow_13.OfpPortReason, desc *openflow_13.OfpPort) {
	logger.Debugw(ctx, "send-change-event", log.Fields{"device-id": deviceID, "reason": reason, "desc": desc})
	q.changeEventQueue <- openflow_13.ChangeEvent{
		Id: deviceID,
		Event: &openflow_13.ChangeEvent_PortStatus{
			PortStatus: &openflow_13.OfpPortStatus{
				Reason: reason,
				Desc:   desc,
			},
		},
	}
}

func (q *Manager) SendFlowChangeEvent(ctx context.Context, deviceID string, res []error, xid uint32, flowCookie uint64) {
	logger.Debugw(ctx, "send-change-event", log.Fields{"device-id": deviceID,
		"flow-id": xid, "flow-cookie": flowCookie, "errors": res})
	errorType := openflow_13.OfpErrorType_OFPET_FLOW_MOD_FAILED
	//Manually creating the data payload for the flow error message
	bs := make([]byte, 2)
	//OF 1.3
	bs[0] = byte(4)
	//Flow Mod
	bs[1] = byte(14)
	//Length of the message
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, 56)
	bs = append(bs, length...)
	emptyArr := []byte{0, 0, 0, 0}
	bs = append(bs, emptyArr...)
	//Cookie of the Flow
	cookie := make([]byte, 52)
	binary.BigEndian.PutUint64(cookie, flowCookie)
	bs = append(bs, cookie...)
	q.changeEventQueue <- openflow_13.ChangeEvent{
		Id: deviceID,
		Event: &openflow_13.ChangeEvent_Error{
			Error: &openflow_13.OfpErrorMsg{
				Header: &openflow_13.OfpHeader{
					Type: openflow_13.OfpType_OFPT_FLOW_MOD,
					Xid:  xid,
				},
				Type: uint32(errorType),
				Code: uint32(openflow_13.OfpFlowModFailedCode_OFPFMFC_UNKNOWN),
				Data: bs,
			},
		},
	}
}

// ReceiveChangeEvents receives change in events
func (q *Manager) ReceiveChangeEvents(_ *empty.Empty, changeEvents voltha.VolthaService_ReceiveChangeEventsServer) error {
	ctx := context.Background()
	ctx = utils.WithRPCMetadataContext(ctx, "ReceiveChangeEvents")
	var streamingTracker = q.getStreamingTracker(ctx, "ReceiveChangeEvents", q.changeEventQueueDone)
	logger.Debugw(ctx, "receive-change-events-request", log.Fields{"change-events": changeEvents})

	err := q.flushFailedPackets(ctx, streamingTracker)
	if err != nil {
		logger.Errorw(ctx, "unable-to-flush-failed-packets", log.Fields{"error": err})
	}

loop:
	for {
		select {
		// Dequeue a change event
		case event := <-q.changeEventQueue:
			logger.Debugw(ctx, "sending-change-event", log.Fields{"event": event})
			if err := changeEvents.Send(&event); err != nil {
				logger.Errorw(ctx, "failed-to-send-change-event", log.Fields{"error": err})
				go q.Agent.GetAndSendRPCEvent(ctx, event.Id, err.Error(),
					nil, "RPC_ERROR_RAISE_EVENT", voltha.EventCategory_COMMUNICATION, nil,
					time.Now().Unix())
				// save last failed change event
				streamingTracker.failedPacket = event
			} else if streamingTracker.failedPacket != nil {
				// reset last failed event saved on success to avoid flushing
				streamingTracker.failedPacket = nil
			}
		case <-q.changeEventQueueDone:
			logger.Debug(ctx, "another-receive-change-events-already-running-bailing-out")
			break loop
		}
	}

	return nil
}

func (q *Manager) GetChangeEventsQueueForTest() <-chan openflow_13.ChangeEvent {
	return q.changeEventQueue
}

func (q *Agent) NewRPCEvent(ctx context.Context, resourceID, desc string, context map[string]string) *voltha.RPCEvent {
	logger.Debugw(ctx, "new-rpc-event", log.Fields{"resource-id": resourceID})
	var opID string
	var rpc string

	if span := opentracing.SpanFromContext(ctx); span != nil {
		if jSpan, ok := span.(*jtracing.Span); ok {
			opID = fmt.Sprintf("%016x", jSpan.SpanContext().TraceID().Low) // Using Sprintf to avoid removal of leading 0s
		}
	}
	rpc = utils.GetRPCMetadataFromContext(ctx)
	rpcev := &voltha.RPCEvent{
		Rpc:         rpc,
		OperationId: opID,
		ResourceId:  resourceID,
		Service:     q.coreInstanceID,
		StackId:     q.stackID,
		Status: &common.OperationResp{
			Code: common.OperationResp_OPERATION_FAILURE,
		},
		Description: desc,
		Context:     context,
	}
	return rpcev
}

func (q *Agent) SendRPCEvent(ctx context.Context, id string, rpcEvent *voltha.RPCEvent, category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	//TODO Instead of directly sending to the kafka bus, queue the message and send it asynchronously
	if rpcEvent.Rpc != "" {
		if err := q.eventProxy.SendRPCEvent(ctx, id, rpcEvent, category, subCategory, raisedTs); err != nil {
			logger.Errorw(ctx, "failed-to-send-rpc-event", log.Fields{"resource-id": id})
		}
	}
}

func (q *Agent) GetAndSendRPCEvent(ctx context.Context, resourceID, desc string, context map[string]string,
	id string, category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	rpcEvent := q.NewRPCEvent(ctx, resourceID, desc, context)
	if rpcEvent.Rpc != "" {
		if err := q.eventProxy.SendRPCEvent(ctx, id, rpcEvent, category, subCategory, raisedTs); err != nil {
			logger.Errorw(ctx, "failed-to-send-rpc-event", log.Fields{"resource-id": id})
		}
	}
}

// SendDeviceStateChangeEvent sends Device State Change Event to message bus
func (q *Agent) SendDeviceStateChangeEvent(ctx context.Context,
	prevOperStatus voltha.OperStatus_Types, prevConnStatus voltha.ConnectStatus_Types, prevAdminStatus voltha.AdminState_Types,
	device *voltha.Device, raisedTs int64) error {
	de := ev.CreateDeviceStateChangeEvent(device.SerialNumber, device.Id, device.ParentId,
		prevOperStatus, prevConnStatus, prevAdminStatus,
		device.OperStatus, device.ConnectStatus, device.AdminState,
		device.ParentPortNo, device.Root)

	subCategory := voltha.EventSubCategory_ONU
	if device.Root {
		subCategory = voltha.EventSubCategory_OLT
	}
	if err := q.eventProxy.SendDeviceEvent(ctx, de, voltha.EventCategory_EQUIPMENT, subCategory, raisedTs); err != nil {
		logger.Errorw(ctx, "error-sending-device-event", log.Fields{"id": device.Id, "err": err})
		return err
	}
	logger.Debugw(ctx, "device-state-change-sent", log.Fields{"event": *de})
	return nil
}
