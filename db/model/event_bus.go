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
package model

import (
	"encoding/json"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/voltha"
)

type EventBus struct {
	client *EventBusClient
	topic  string
}

var (
	IGNORED_CALLBACKS = map[CallbackType]struct{}{
		PRE_ADD:         {},
		GET:             {},
		POST_LISTCHANGE: {},
		PRE_REMOVE:      {},
		PRE_UPDATE:      {},
	}
)

func NewEventBus() *EventBus {
	bus := &EventBus{
		client: NewEventBusClient(),
		topic:  "model-change-events",
	}
	return bus
}

//func (bus *EventBus) Advertise(eventType CallbackType, data interface{}, hash string) {
func (bus *EventBus) Advertise(args ...interface{}) interface{} {
	eventType := args[0].(CallbackType)
	hash := args[1].(string)
	data := args[2:]

	if _, ok := IGNORED_CALLBACKS[eventType]; ok {
		log.Debugf("ignoring event - type:%s, data:%+v", eventType, data)
	}
	var kind voltha.ConfigEventType_ConfigEventType
	switch eventType {
	case POST_ADD:
		kind = voltha.ConfigEventType_add
	case POST_REMOVE:
		kind = voltha.ConfigEventType_remove
	default:
		kind = voltha.ConfigEventType_update
	}

	var msg []byte
	var err error
	if IsProtoMessage(data) {
		if msg, err = proto.Marshal(data[0].(proto.Message)); err != nil {
			log.Errorf("problem marshalling proto data: %+v, err:%s", data[0], err.Error())
		}
	} else if data[0] != nil {
		if msg, err = json.Marshal(data[0]); err != nil {
			log.Errorf("problem marshalling json data: %+v, err:%s", data[0], err.Error())
		}
	} else {
		log.Errorf("no data to advertise : %+v", data[0])
	}

	event := voltha.ConfigEvent{
		Type: kind,
		Hash: hash,
		Data: string(msg),
	}

	bus.client.Publish(bus.topic, event)

	return nil
}
