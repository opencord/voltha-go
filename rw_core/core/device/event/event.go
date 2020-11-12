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
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v4/pkg/events/eventif"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/opentracing/opentracing-go"
	jtracing "github.com/uber/jaeger-client-go"
	"sync"
	"time"
)

type Manager struct {
	packetInQueue        chan openflow_13.PacketIn
	packetInQueueDone    chan bool
	changeEventQueue     chan openflow_13.ChangeEvent
	changeEventQueueDone chan bool
	RPCEventManager      *RPCEventManager
}

type RPCEventManager struct {
	eventProxy     eventif.EventProxy
	coreInstanceID string
}

func NewManager(proxyForRPCEvents eventif.EventProxy, instanceID string) *Manager {
	return &Manager{
		packetInQueue:        make(chan openflow_13.PacketIn, 100),
		packetInQueueDone:    make(chan bool, 1),
		changeEventQueue:     make(chan openflow_13.ChangeEvent, 100),
		changeEventQueueDone: make(chan bool, 1),
		RPCEventManager:      NewRPCEventManager(proxyForRPCEvents, instanceID),
	}
}

func NewRPCEventManager(proxyForRPCEvents eventif.EventProxy, instanceID string) *RPCEventManager {
	return &RPCEventManager{
		eventProxy:     proxyForRPCEvents,
		coreInstanceID: instanceID,
	}
}
func (q *Manager) SendPacketIn(ctx context.Context, deviceID string, transationID string, packet *openflow_13.OfpPacketIn) {
	// TODO: Augment the OF PacketIn to include the transactionId
	packetIn := openflow_13.PacketIn{Id: deviceID, PacketIn: packet}
	logger.Debugw(ctx, "SendPacketIn", log.Fields{"packetIn": packetIn})
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
		logger.Debugf(ctx, "%s streaming call already running. Exiting it", method)
		done <- true
		logger.Debugf(ctx, "Last %s exited. Continuing ...", method)
	} else {
		streamingTracker.calls[method] = &callTracker{failedPacket: nil}
	}
	return streamingTracker.calls[method]
}

func (q *Manager) flushFailedPackets(ctx context.Context, tracker *callTracker) error {
	if tracker.failedPacket != nil {
		switch tracker.failedPacket.(type) {
		case openflow_13.PacketIn:
			logger.Debug(ctx, "Enqueueing last failed packetIn")
			q.packetInQueue <- tracker.failedPacket.(openflow_13.PacketIn)
		case openflow_13.ChangeEvent:
			logger.Debug(ctx, "Enqueueing last failed changeEvent")
			q.changeEventQueue <- tracker.failedPacket.(openflow_13.ChangeEvent)
		}
	}
	return nil
}

// ReceivePacketsIn receives packets from adapter
func (q *Manager) ReceivePacketsIn(_ *empty.Empty, packetsIn voltha.VolthaService_ReceivePacketsInServer) error {
	ctx := context.Background()
	ctx = utils.SetRPCMetadataInContext(ctx, "ReceivePacketsIn")
	var streamingTracker = q.getStreamingTracker(ctx, "ReceivePacketsIn", q.packetInQueueDone)
	logger.Debugw(ctx, "ReceivePacketsIn-request", log.Fields{"packetsIn": packetsIn})

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
				go q.RPCEventManager.GetAndSendRPCEvent(ctx, packet.Id, err.Error(),
					nil, "RPC_ERROR_RAISE_EVENT", voltha.EventCategory_COMMUNICATION,
					nil, time.Now().UnixNano())
				// save the last failed packet in
				streamingTracker.failedPacket = packet
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed packet saved to avoid flush
					streamingTracker.failedPacket = nil
				}
			}
		case <-q.packetInQueueDone:
			logger.Debug(ctx, "Another ReceivePacketsIn running. Bailing out ...")
			break loop
		}
	}

	//TODO: Find an elegant way to get out of the above loop when the Core is stopped
	return nil
}

func (q *Manager) SendChangeEvent(ctx context.Context, deviceID string, reason openflow_13.OfpPortReason, desc *openflow_13.OfpPort) {
	logger.Debugw(ctx, "SendChangeEvent", log.Fields{"device-id": deviceID, "reason": reason, "desc": desc})
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
	logger.Debugw(ctx, "SendChangeEvent", log.Fields{"device-id": deviceID,
		"flowId": xid, "flowCookie": flowCookie, "errors": res})
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
	ctx = utils.SetRPCMetadataInContext(ctx, "ReceiveChangeEvents")
	var streamingTracker = q.getStreamingTracker(ctx, "ReceiveChangeEvents", q.changeEventQueueDone)
	logger.Debugw(ctx, "ReceiveChangeEvents-request", log.Fields{"changeEvents": changeEvents})

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
				go q.RPCEventManager.GetAndSendRPCEvent(ctx, event.Id, err.Error(),
					nil, "RPC_ERROR_RAISE_EVENT", voltha.EventCategory_COMMUNICATION, nil,
					time.Now().UnixNano())
				// save last failed change event
				streamingTracker.failedPacket = event
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed event saved on success to avoid flushing
					streamingTracker.failedPacket = nil
				}
			}
		case <-q.changeEventQueueDone:
			logger.Debug(ctx, "Another ReceiveChangeEvents already running. Bailing out ...")
			break loop
		}
	}

	return nil
}

func (q *Manager) GetChangeEventsQueueForTest() <-chan openflow_13.ChangeEvent {
	return q.changeEventQueue
}

func (q *RPCEventManager) NewRPCEvent(ctx context.Context, resourceID, desc string, context map[string]string) *voltha.RPCEvent {
	logger.Debugw(ctx, "newRPCEvent", log.Fields{"resource-id": resourceID})
	var opID string
	var rpc string

	if span := opentracing.SpanFromContext(ctx); span != nil {
		if jSpan, ok := span.(*jtracing.Span); ok {
			opID = fmt.Sprintf("%016x", jSpan.SpanContext().TraceID().Low) // Using Sprintf to avoid removal of leading 0s
		}
	}
	rpc = utils.GetRPCNameFromContext(ctx)
	rpcev := &voltha.RPCEvent{
		Rpc:         rpc,
		OperationId: opID,
		ResourceId:  resourceID,
		Service:     q.coreInstanceID,
		Status: &common.OperationResp{
			Code: common.OperationResp_OPERATION_FAILURE,
		},
		Description: desc,
		Context:     context,
	}
	return rpcev
}

func (q *RPCEventManager) SendRPCEvent(ctx context.Context, id string, rpcEvent *voltha.RPCEvent, category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	//TODO Instead of directly sending to the kafka bus, queue the message and send it asynchronously
	if rpcEvent.Rpc != "" {
		_ = q.eventProxy.SendRPCEvent(ctx, id, rpcEvent, category, subCategory, raisedTs)
	}
}

func (q *RPCEventManager) GetAndSendRPCEvent(ctx context.Context, resourceID, desc string, context map[string]string,
	id string, category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	rpcEvent := q.NewRPCEvent(ctx, resourceID, desc, context)
	//TODO Instead of directly sending to the kafka bus, queue the message and send it asynchronously
	if rpcEvent.Rpc != "" {
		_ = q.eventProxy.SendRPCEvent(ctx, id, rpcEvent, category, subCategory, raisedTs)
	}
}
