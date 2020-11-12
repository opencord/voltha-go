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
"context"
"errors"
"fmt"
"strconv"
"strings"
"time"

"github.com/golang/protobuf/ptypes"
"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
"github.com/opencord/voltha-lib-go/v4/pkg/log"
"github.com/opencord/voltha-protos/v4/go/voltha"
)

const (
	EventTypeVersion = "0.1"
)

type (
	EventType        = voltha.EventType_Types
	EventCategory    = voltha.EventCategory_Types
	EventSubCategory = voltha.EventSubCategory_Types
)

type EventProxy struct {
	kafkaClient kafka.Client
	eventTopic  kafka.Topic
}

func NewEventProxy(opts ...EventProxyOption) *EventProxy {
	var proxy EventProxy
	for _, option := range opts {
		option(&proxy)
	}
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
	category EventCategory,
	subCategory *EventSubCategory,
	eventType EventType,
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
	}
	header.Type = eventType
	header.TypeVersion = EventTypeVersion

	// raisedTs is in nanoseconds
	timestamp, err := ptypes.TimestampProto(time.Unix(0, raisedTs))
	if err != nil {
		return nil, err
	}
	header.RaisedTs = timestamp

	timestamp, err = ptypes.TimestampProto(time.Now())
	if err != nil {
		return nil, err
	}
	header.ReportedTs = timestamp

	return &header, nil
}

/* Send out rpc events*/
func (ep *EventProxy) SendRPCEvent(ctx context.Context, id string, rpcEvent *voltha.RPCEvent, category EventCategory, subCategory *EventSubCategory, raisedTs int64) error {
	if rpcEvent == nil {
		logger.Error(ctx, "Received empty rpc event")
		return errors.New("rpc event nil")
	}
	var event voltha.Event
	var rpce voltha.Event_RpcEvent
	var err error
	rpce.RpcEvent = rpcEvent
	if event.Header, err = ep.getEventHeader(id, category, subCategory, voltha.EventType_RPC_EVENT, raisedTs); err != nil {
		return err
	}
	event.EventType = &rpce
	fmt.Println(event)
	if err := ep.sendEvent(ctx, &event); err != nil {
		logger.Errorw(ctx, "Failed to send rpc event to KAFKA bus", log.Fields{"rpc-event": rpcEvent})
		return err
	}
	logger.Infow(ctx, "Successfully sent RPC event to KAFKA bus", log.Fields{"Id": event.Header.Id, "Category": event.Header.Category,
		"SubCategory": event.Header.SubCategory, "Type": event.Header.Type, "TypeVersion": event.Header.TypeVersion,
		"ReportedTs": event.Header.ReportedTs, "ResourceId": rpcEvent.ResourceId, "Context": rpcEvent.Context,
		"RPCEventName": id})

	return nil

}

func (ep *EventProxy) sendEvent(ctx context.Context, event *voltha.Event) error {
	logger.Infow(ctx, "Sent event to kafka", log.Fields{"event": event})
	fmt.Println(ep.kafkaClient, ep.eventTopic)
	if err := ep.kafkaClient.Send(ctx, event, &ep.eventTopic); err != nil {
		return err
	}
	logger.Debugw(ctx, "Sent event to kafka", log.Fields{"event": event})

	return nil
}

