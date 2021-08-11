/*
 * Copyright 2020-present Open Networking Foundation

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

package events

import (
	"container/ring"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/events/eventif"
	"github.com/opencord/voltha-lib-go/v7/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TODO: Make configurable through helm chart
const EVENT_THRESHOLD = 1000

type lastEvent struct{}

type EventProxy struct {
	kafkaClient    kafka.Client
	eventTopic     kafka.Topic
	eventQueue     *EventQueue
	queueCtx       context.Context
	queueCancelCtx context.CancelFunc
}

func NewEventProxy(opts ...EventProxyOption) *EventProxy {
	var proxy EventProxy
	for _, option := range opts {
		option(&proxy)
	}
	proxy.eventQueue = newEventQueue()
	proxy.queueCtx, proxy.queueCancelCtx = context.WithCancel(context.Background())
	return &proxy
}

type EventProxyOption func(*EventProxy)

func MsgClient(client kafka.Client) EventProxyOption {
	return func(args *EventProxy) {
		args.kafkaClient = client
	}
}

func MsgTopic(topic kafka.Topic) EventProxyOption {
	return func(args *EventProxy) {
		args.eventTopic = topic
	}
}

func (ep *EventProxy) formatId(eventName string) string {
	return fmt.Sprintf("Voltha.openolt.%s.%s", eventName, strconv.FormatInt(time.Now().UnixNano(), 10))
}

func (ep *EventProxy) getEventHeader(eventName string,
	category eventif.EventCategory,
	subCategory *eventif.EventSubCategory,
	eventType eventif.EventType,
	raisedTs int64) (*voltha.EventHeader, error) {
	var header voltha.EventHeader
	if strings.Contains(eventName, "_") {
		eventName = strings.Join(strings.Split(eventName, "_")[:len(strings.Split(eventName, "_"))-2], "_")
	} else {
		eventName = "UNKNOWN_EVENT"
	}
	/* Populating event header */
	header.Id = ep.formatId(eventName)
	header.Category = category
	if subCategory != nil {
		header.SubCategory = *subCategory
	} else {
		header.SubCategory = voltha.EventSubCategory_NONE
	}
	header.Type = eventType
	header.TypeVersion = eventif.EventTypeVersion

	// raisedTs is in seconds
	header.RaisedTs = timestamppb.New(time.Unix(raisedTs, 0))
	header.ReportedTs = timestamppb.New(time.Now())

	return &header, nil
}

/* Send out rpc events*/
func (ep *EventProxy) SendRPCEvent(ctx context.Context, id string, rpcEvent *voltha.RPCEvent, category eventif.EventCategory, subCategory *eventif.EventSubCategory, raisedTs int64) error {
	if rpcEvent == nil {
		logger.Error(ctx, "Received empty rpc event")
		return errors.New("rpc event nil")
	}
	var event voltha.Event
	var err error
	if event.Header, err = ep.getEventHeader(id, category, subCategory, voltha.EventType_RPC_EVENT, raisedTs); err != nil {
		return err
	}
	event.EventType = &voltha.Event_RpcEvent{RpcEvent: rpcEvent}
	ep.eventQueue.push(&event)
	return nil

}

/* Send out device events*/
func (ep *EventProxy) SendDeviceEvent(ctx context.Context, deviceEvent *voltha.DeviceEvent, category eventif.EventCategory, subCategory eventif.EventSubCategory, raisedTs int64) error {
	if deviceEvent == nil {
		logger.Error(ctx, "Recieved empty device event")
		return errors.New("Device event nil")
	}
	var event voltha.Event
	var de voltha.Event_DeviceEvent
	var err error
	de.DeviceEvent = deviceEvent
	if event.Header, err = ep.getEventHeader(deviceEvent.DeviceEventName, category, &subCategory, voltha.EventType_DEVICE_EVENT, raisedTs); err != nil {
		return err
	}
	event.EventType = &de
	if err := ep.sendEvent(ctx, &event); err != nil {
		logger.Errorw(ctx, "Failed to send device event to KAFKA bus", log.Fields{"device-event": deviceEvent})
		return err
	}
	logger.Infow(ctx, "Successfully sent device event KAFKA", log.Fields{"Id": event.Header.Id, "Category": event.Header.Category,
		"SubCategory": event.Header.SubCategory, "Type": event.Header.Type, "TypeVersion": event.Header.TypeVersion,
		"ReportedTs": event.Header.ReportedTs, "ResourceId": deviceEvent.ResourceId, "Context": deviceEvent.Context,
		"DeviceEventName": deviceEvent.DeviceEventName})

	return nil

}

// SendKpiEvent is to send kpi events to voltha.event topic
func (ep *EventProxy) SendKpiEvent(ctx context.Context, id string, kpiEvent *voltha.KpiEvent2, category eventif.EventCategory, subCategory eventif.EventSubCategory, raisedTs int64) error {
	if kpiEvent == nil {
		logger.Error(ctx, "Recieved empty kpi event")
		return errors.New("KPI event nil")
	}
	var event voltha.Event
	var de voltha.Event_KpiEvent2
	var err error
	de.KpiEvent2 = kpiEvent
	if event.Header, err = ep.getEventHeader(id, category, &subCategory, voltha.EventType_KPI_EVENT2, raisedTs); err != nil {
		return err
	}
	event.EventType = &de
	if err := ep.sendEvent(ctx, &event); err != nil {
		logger.Errorw(ctx, "Failed to send kpi event to KAFKA bus", log.Fields{"device-event": kpiEvent})
		return err
	}
	logger.Infow(ctx, "Successfully sent kpi event to KAFKA", log.Fields{"Id": event.Header.Id, "Category": event.Header.Category,
		"SubCategory": event.Header.SubCategory, "Type": event.Header.Type, "TypeVersion": event.Header.TypeVersion,
		"ReportedTs": event.Header.ReportedTs, "KpiEventName": "STATS_EVENT"})

	return nil

}

func (ep *EventProxy) sendEvent(ctx context.Context, event *voltha.Event) error {
	logger.Debugw(ctx, "Send event to kafka", log.Fields{"event": event})
	if err := ep.kafkaClient.Send(ctx, event, &ep.eventTopic); err != nil {
		return err
	}
	logger.Debugw(ctx, "Sent event to kafka", log.Fields{"event": event})

	return nil
}

func (ep *EventProxy) EnableLivenessChannel(ctx context.Context, enable bool) chan bool {
	return ep.kafkaClient.EnableLivenessChannel(ctx, enable)
}

func (ep *EventProxy) SendLiveness(ctx context.Context) error {
	return ep.kafkaClient.SendLiveness(ctx)
}

// Start the event proxy
func (ep *EventProxy) Start() error {
	eq := ep.eventQueue
	go eq.start(ep.queueCtx)
	logger.Debugw(context.Background(), "event-proxy-starting...", log.Fields{"events-threashold": EVENT_THRESHOLD})
	for {
		// Notify the queue I am ready
		eq.readyToSendToKafkaCh <- struct{}{}
		// Wait for an event
		elem, ok := <-eq.eventChannel
		if !ok {
			logger.Debug(context.Background(), "event-channel-closed-exiting")
			break
		}
		// Check for last event
		if _, ok := elem.(*lastEvent); ok {
			// close the queuing loop
			logger.Info(context.Background(), "received-last-event")
			ep.queueCancelCtx()
			break
		}
		ctx := context.Background()
		event, ok := elem.(*voltha.Event)
		if !ok {
			logger.Warnw(ctx, "invalid-event", log.Fields{"element": elem})
			continue
		}
		if err := ep.sendEvent(ctx, event); err != nil {
			logger.Errorw(ctx, "failed-to-send-event-to-kafka-bus", log.Fields{"event": event})
		} else {
			logger.Debugw(ctx, "successfully-sent-rpc-event-to-kafka-bus", log.Fields{"id": event.Header.Id, "category": event.Header.Category,
				"sub-category": event.Header.SubCategory, "type": event.Header.Type, "type-version": event.Header.TypeVersion,
				"reported-ts": event.Header.ReportedTs, "event-type": event.EventType})
		}
	}
	return nil
}

func (ep *EventProxy) Stop() {
	if ep.eventQueue != nil {
		ep.eventQueue.stop()
	}
}

type EventQueue struct {
	mutex                sync.RWMutex
	eventChannel         chan interface{}
	insertPosition       *ring.Ring
	popPosition          *ring.Ring
	dataToSendAvailable  chan struct{}
	readyToSendToKafkaCh chan struct{}
	eventQueueStopped    chan struct{}
}

func newEventQueue() *EventQueue {
	ev := &EventQueue{
		eventChannel:         make(chan interface{}),
		insertPosition:       ring.New(EVENT_THRESHOLD),
		dataToSendAvailable:  make(chan struct{}),
		readyToSendToKafkaCh: make(chan struct{}),
		eventQueueStopped:    make(chan struct{}),
	}
	ev.popPosition = ev.insertPosition
	return ev
}

// push is invoked to push an event at the back of a queue
func (eq *EventQueue) push(event interface{}) {
	eq.mutex.Lock()

	if eq.insertPosition != nil {
		// Handle Queue is full.
		// TODO: Current default is to overwrite old data if queue is full. Is there a need to
		// block caller if max threshold is reached?
		if eq.insertPosition.Value != nil && eq.insertPosition == eq.popPosition {
			eq.popPosition = eq.popPosition.Next()
		}

		// Insert data and move pointer to next empty position
		eq.insertPosition.Value = event
		eq.insertPosition = eq.insertPosition.Next()

		// Check for last event
		if _, ok := event.(*lastEvent); ok {
			eq.insertPosition = nil
		}
		eq.mutex.Unlock()
		// Notify waiting thread of data availability
		eq.dataToSendAvailable <- struct{}{}

	} else {
		logger.Debug(context.Background(), "event-queue-is-closed-as-insert-position-is-cleared")
		eq.mutex.Unlock()
	}
}

// start starts the routine that extracts an element from the event queue and
// send it to the kafka sending routine to process.
func (eq *EventQueue) start(ctx context.Context) {
	logger.Info(ctx, "starting-event-queue")
loop:
	for {
		select {
		case <-eq.dataToSendAvailable:
		//	Do nothing - use to prevent caller pushing data to block
		case <-eq.readyToSendToKafkaCh:
			{
				// Kafka sending routine is ready to process an event
				eq.mutex.Lock()
				element := eq.popPosition.Value
				if element == nil {
					// No events to send. Wait
					eq.mutex.Unlock()
					select {
					case _, ok := <-eq.dataToSendAvailable:
						if !ok {
							// channel closed
							eq.eventQueueStopped <- struct{}{}
							return
						}
					case <-ctx.Done():
						logger.Info(ctx, "event-queue-context-done")
						eq.eventQueueStopped <- struct{}{}
						return
					}
					eq.mutex.Lock()
					element = eq.popPosition.Value
				}
				eq.popPosition.Value = nil
				eq.popPosition = eq.popPosition.Next()
				eq.mutex.Unlock()
				eq.eventChannel <- element
			}
		case <-ctx.Done():
			logger.Info(ctx, "event-queue-context-done")
			eq.eventQueueStopped <- struct{}{}
			break loop
		}
	}
	logger.Info(ctx, "event-queue-stopped")

}

func (eq *EventQueue) stop() {
	// Flush all
	eq.push(&lastEvent{})
	<-eq.eventQueueStopped
	eq.mutex.Lock()
	close(eq.readyToSendToKafkaCh)
	close(eq.dataToSendAvailable)
	close(eq.eventChannel)
	eq.mutex.Unlock()

}
