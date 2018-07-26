package model

import (
	"encoding/json"
	"fmt"
	"github.com/opencord/voltha/protos/go/voltha"
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

func (bus *EventBus) Advertise(eventType CallbackType, data interface{}, hash string) {
	if _, ok := IGNORED_CALLBACKS[eventType]; ok {
		fmt.Printf("ignoring event - type:%s, data:%+v\n", eventType, data)
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
		if msg, err = json.Marshal(data); err != nil {
			fmt.Errorf("problem marshalling data: %+v, err:%s\n", data, err.Error())
		}
	} else {
		msg = data.([]byte)
	}

	event := voltha.ConfigEvent{
		Type: kind,
		Hash: hash,
		Data: string(msg),
	}

	bus.client.Publish(bus.topic, event)
}
