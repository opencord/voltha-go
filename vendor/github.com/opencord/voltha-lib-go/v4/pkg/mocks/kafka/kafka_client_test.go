/*
 * Copyright 2019-present Open Networking Foundation
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

package kafka

import (
	"context"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestKafkaClientCreateTopic(t *testing.T) {
	ctx := context.Background()
	cTkc := NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}
	err := cTkc.CreateTopic(ctx, &topic, 1, 1)
	assert.Nil(t, err)
	err = cTkc.CreateTopic(ctx, &topic, 1, 1)
	assert.NotNil(t, err)
}

func TestKafkaClientDeleteTopic(t *testing.T) {
	cTkc := NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}
	err := cTkc.DeleteTopic(context.Background(), &topic)
	assert.Nil(t, err)
}

func TestKafkaClientSubscribeSend(t *testing.T) {
	cTkc := NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}
	ch, err := cTkc.Subscribe(context.Background(), &topic)
	assert.Nil(t, err)
	assert.NotNil(t, ch)
	testCh := make(chan bool)
	maxWait := 5 * time.Millisecond
	msg := &ic.InterContainerMessage{
		Header: &ic.Header{Id: "1234", ToTopic: topic.Name},
		Body:   nil,
	}
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	go func() {
		select {
		case val, ok := <-ch:
			assert.True(t, ok)
			assert.Equal(t, val, msg)
			testCh <- true
		case <-timer.C:
			testCh <- false
		}
	}()
	err = cTkc.Send(context.Background(), msg, &topic)
	assert.Nil(t, err)
	res := <-testCh
	assert.True(t, res)
}

func TestKafkaClientUnSubscribe(t *testing.T) {
	cTkc := NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}
	ch, err := cTkc.Subscribe(context.Background(), &topic)
	assert.Nil(t, err)
	assert.NotNil(t, ch)
	err = cTkc.UnSubscribe(context.Background(), &topic, ch)
	assert.Nil(t, err)
}

func TestKafkaClientStop(t *testing.T) {
	cTkc := NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}
	ch, err := cTkc.Subscribe(context.Background(), &topic)
	assert.Nil(t, err)
	assert.NotNil(t, ch)
	err = cTkc.UnSubscribe(context.Background(), &topic, ch)
	assert.Nil(t, err)
	cTkc.Stop(context.Background())
}
