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
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewEventBus(t *testing.T) {
	eventBus := NewEventBus()
	assert.NotNil(t, eventBus)
	t.Logf("Event Bus --> %v", eventBus)
	t.Logf("Event Bus Client --> %v EventBusTopic --> %s", eventBus.client, eventBus.topic)
}

func TestAdvertiseNoData(t *testing.T) {
	eventBus := NewEventBus()
	assert.NotNil(t, eventBus)
	assert.Nil(t, eventBus.Advertise(POST_ADD, "stringa ", nil))
}

func TestAdvertiseAddData(t *testing.T) {
	eventBus := NewEventBus()
	assert.NotNil(t, eventBus)
	assert.Nil(t, eventBus.Advertise(POST_ADD, "event", "data1", "data2"))
}
func TestAdvertiseRemoveData(t *testing.T) {
	eventBus := NewEventBus()
	assert.NotNil(t, eventBus)
	assert.Nil(t, eventBus.Advertise(POST_ADD, "event", "data1", "data2"))
	assert.Nil(t, eventBus.Advertise(POST_REMOVE, "event", "data1"))
}
func TestAdvertiseUpdate(t *testing.T) {
	eventBus := NewEventBus()
	assert.NotNil(t, eventBus)
	assert.Nil(t, eventBus.Advertise(POST_ADD, "event", "data1", "data2"))
	assert.Nil(t, eventBus.Advertise(POST_REMOVE, "event", "data1"))
	assert.Nil(t, eventBus.Advertise(PRE_UPDATE, "event", "data2"))
}
