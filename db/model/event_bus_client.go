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
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// EventBusClient is an abstraction layer structure to communicate with an event bus mechanism
type EventBusClient struct {
}

// NewEventBusClient creates a new EventBusClient instance
func NewEventBusClient() *EventBusClient {
	return &EventBusClient{}
}

// Publish sends a event to the bus
func (ebc *EventBusClient) Publish(topic string, event voltha.ConfigEvent) {
	log.Debugf("publishing event:%+v, topic:%s\n", event, topic)
}
