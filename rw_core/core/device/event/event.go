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
	"encoding/hex"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"sync"
)

type Manager struct {
	packetInQueue        chan openflow_13.PacketIn
	packetInQueueDone    chan bool
	changeEventQueue     chan openflow_13.ChangeEvent
	changeEventQueueDone chan bool
}

func NewManager(ctx context.Context) *Manager {
	return &Manager{
		packetInQueue:        make(chan openflow_13.PacketIn, 100),
		packetInQueueDone:    make(chan bool, 1),
		changeEventQueue:     make(chan openflow_13.ChangeEvent, 100),
		changeEventQueueDone: make(chan bool, 1),
	}
}

func (q *Manager) SendPacketIn(ctx context.Context, deviceID string, transationID string, packet *openflow_13.OfpPacketIn) {
	// TODO: Augment the OF PacketIn to include the transactionId
	packetIn := openflow_13.PacketIn{Id: deviceID, PacketIn: packet}
	logger.Debugw("SendPacketIn", log.Fields{"packetIn": packetIn})
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
		logger.Debugf("%s streaming call already running. Exiting it", method)
		done <- true
		logger.Debugf("Last %s exited. Continuing ...", method)
	} else {
		streamingTracker.calls[method] = &callTracker{failedPacket: nil}
	}
	return streamingTracker.calls[method]
}

func (q *Manager) flushFailedPackets(ctx context.Context, tracker *callTracker) error {
	if tracker.failedPacket != nil {
		switch tracker.failedPacket.(type) {
		case openflow_13.PacketIn:
			logger.Debug("Enqueueing last failed packetIn")
			q.packetInQueue <- tracker.failedPacket.(openflow_13.PacketIn)
		case openflow_13.ChangeEvent:
			logger.Debug("Enqueueing last failed changeEvent")
			q.changeEventQueue <- tracker.failedPacket.(openflow_13.ChangeEvent)
		}
	}
	return nil
}

// ReceivePacketsIn receives packets from adapter
func (q *Manager) ReceivePacketsIn(_ *empty.Empty, packetsIn voltha.VolthaService_ReceivePacketsInServer) error {
	ctx := context.Background()
	var streamingTracker = q.getStreamingTracker(ctx, "ReceivePacketsIn", q.packetInQueueDone)
	logger.Debugw("ReceivePacketsIn-request", log.Fields{"packetsIn": packetsIn})

	err := q.flushFailedPackets(ctx, streamingTracker)
	if err != nil {
		logger.Errorw("unable-to-flush-failed-packets", log.Fields{"error": err})
	}

loop:
	for {
		select {
		case packet := <-q.packetInQueue:
			logger.Debugw("sending-packet-in", log.Fields{
				"packet": hex.EncodeToString(packet.PacketIn.Data),
			})
			if err := packetsIn.Send(&packet); err != nil {
				logger.Errorw("failed-to-send-packet", log.Fields{"error": err})
				// save the last failed packet in
				streamingTracker.failedPacket = packet
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed packet saved to avoid flush
					streamingTracker.failedPacket = nil
				}
			}
		case <-q.packetInQueueDone:
			logger.Debug("Another ReceivePacketsIn running. Bailing out ...")
			break loop
		}
	}

	//TODO: Find an elegant way to get out of the above loop when the Core is stopped
	return nil
}

func (q *Manager) SendChangeEvent(ctx context.Context, deviceID string, portStatus *openflow_13.OfpPortStatus) {
	// TODO: validate the type of portStatus parameter
	//if _, ok := portStatus.(*openflow_13.OfpPortStatus); ok {
	//}
	event := openflow_13.ChangeEvent{Id: deviceID, Event: &openflow_13.ChangeEvent_PortStatus{PortStatus: portStatus}}
	logger.Debugw("SendChangeEvent", log.Fields{"event": event})
	q.changeEventQueue <- event
}

// ReceiveChangeEvents receives change in events
func (q *Manager) ReceiveChangeEvents(_ *empty.Empty, changeEvents voltha.VolthaService_ReceiveChangeEventsServer) error {
	ctx := context.Background()
	var streamingTracker = q.getStreamingTracker(ctx, "ReceiveChangeEvents", q.changeEventQueueDone)
	logger.Debugw("ReceiveChangeEvents-request", log.Fields{"changeEvents": changeEvents})

	err := q.flushFailedPackets(ctx, streamingTracker)
	if err != nil {
		logger.Errorw("unable-to-flush-failed-packets", log.Fields{"error": err})
	}

loop:
	for {
		select {
		// Dequeue a change event
		case event := <-q.changeEventQueue:
			logger.Debugw("sending-change-event", log.Fields{"event": event})
			if err := changeEvents.Send(&event); err != nil {
				logger.Errorw("failed-to-send-change-event", log.Fields{"error": err})
				// save last failed changeevent
				streamingTracker.failedPacket = event
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed event saved on success to avoid flushing
					streamingTracker.failedPacket = nil
				}
			}
		case <-q.changeEventQueueDone:
			logger.Debug("Another ReceiveChangeEvents already running. Bailing out ...")
			break loop
		}
	}

	return nil
}

func (q *Manager) GetChangeEventsQueueForTest(ctx context.Context) <-chan openflow_13.ChangeEvent {
	return q.changeEventQueue
}
