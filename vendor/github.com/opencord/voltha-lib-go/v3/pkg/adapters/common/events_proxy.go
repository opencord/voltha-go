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

package common

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v3/pkg/adapters/adapterif"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
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
	category adapterif.EventCategory,
	subCategory adapterif.EventSubCategory,
	eventType adapterif.EventType,
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
	header.SubCategory = subCategory
	header.Type = eventType
	header.TypeVersion = adapterif.EventTypeVersion

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

/* Send out device events*/
func (ep *EventProxy) SendDeviceEvent(ctx context.Context, deviceEvent *voltha.DeviceEvent, category adapterif.EventCategory, subCategory adapterif.EventSubCategory, raisedTs int64) error {
	if deviceEvent == nil {
		logger.Error(ctx, "Recieved empty device event")
		return errors.New("Device event nil")
	}
	var event voltha.Event
	var de voltha.Event_DeviceEvent
	var err error
	de.DeviceEvent = deviceEvent
	if event.Header, err = ep.getEventHeader(deviceEvent.DeviceEventName, category, subCategory, voltha.EventType_DEVICE_EVENT, raisedTs); err != nil {
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
func (ep *EventProxy) SendKpiEvent(ctx context.Context, id string, kpiEvent *voltha.KpiEvent2, category adapterif.EventCategory, subCategory adapterif.EventSubCategory, raisedTs int64) error {
	if kpiEvent == nil {
		logger.Error(ctx, "Recieved empty kpi event")
		return errors.New("KPI event nil")
	}
	var event voltha.Event
	var de voltha.Event_KpiEvent2
	var err error
	de.KpiEvent2 = kpiEvent
	if event.Header, err = ep.getEventHeader(id, category, subCategory, voltha.EventType_KPI_EVENT2, raisedTs); err != nil {
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

/* TODO: Send out KPI events*/

func (ep *EventProxy) sendEvent(ctx context.Context, event *voltha.Event) error {
	if err := ep.kafkaClient.Send(ctx, event, &ep.eventTopic); err != nil {
		return err
	}
	logger.Debugw(ctx, "Sent event to kafka", log.Fields{"event": event})

	return nil
}
