package model

import (
	"fmt"
	"github.com/opencord/voltha/protos/go/voltha"
)

type EventBusClient struct {
}

func NewEventBusClient() *EventBusClient {
	return &EventBusClient{}
}

func (ebc *EventBusClient) Publish(topic string, event voltha.ConfigEvent) {
	fmt.Printf("publishing event:%+v, topic:%s\n", event, topic)
}
