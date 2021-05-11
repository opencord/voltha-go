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
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	mock_kafka "github.com/opencord/voltha-lib-go/v4/pkg/mocks/kafka"
	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
	"time"
)

const waitForKafkaEventsTimeout = 20 * time.Second
const waitForEventProxyTimeout = 10 * time.Second

func waitForEventProxyToStop(ep *EventProxy, resp chan string) {
	timer := time.NewTimer(waitForEventProxyTimeout)
	defer timer.Stop()
	for {
		select {
		case <-time.After(2 * time.Millisecond):
			if ep.eventQueue.insertPosition == nil {
				resp <- "ok"
				return
			}
		case <-timer.C:
			resp <- "timer expired"
			return
		}

	}
}
func waitForKafkaEvents(kc kafka.Client, topic *kafka.Topic, numEvents int, resp chan string) {
	kafkaChnl, err := kc.Subscribe(context.Background(), topic)
	if err != nil {
		resp <- err.Error()
		return
	}
	defer func() {
		if kafkaChnl != nil {
			if err = kc.UnSubscribe(context.Background(), topic, kafkaChnl); err != nil {
				logger.Errorw(context.Background(), "unsubscribe-failed", log.Fields{"error": err})
			}
		}
	}()
	timer := time.NewTimer(waitForKafkaEventsTimeout)
	defer timer.Stop()
	count := 0
loop:
	for {
		select {
		case msg := <-kafkaChnl:
			if msg.Body != nil {
				event := voltha.Event{}
				if err := ptypes.UnmarshalAny(msg.Body, &event); err == nil {
					count += 1
					if count == numEvents {
						resp <- "ok"
						break loop
					}
				}
			}
		case <-timer.C:
			resp <- "timer expired"
			break loop
		}
	}
}

func createAndSendEvent(proxy *EventProxy, ID string) error {
	eventMsg := &voltha.RPCEvent{
		Rpc:         "dummy",
		OperationId: ID,
		ResourceId:  "dummy",
		Service:     "dummy",
		StackId:     "dummy",
		Status: &common.OperationResp{
			Code: common.OperationResp_OPERATION_FAILURE,
		},
		Description: "dummy",
		Context:     nil,
	}
	var event voltha.Event
	raisedTS := time.Now().Unix()
	event.Header, _ = proxy.getEventHeader("RPC_ERROR_RAISE_EVENT", voltha.EventCategory_COMMUNICATION, nil, voltha.EventType_RPC_EVENT, raisedTS)
	event.EventType = &voltha.Event_RpcEvent{RpcEvent: eventMsg}
	err := proxy.SendRPCEvent(context.Background(), "RPC_ERROR_RAISE_EVENT", eventMsg, voltha.EventCategory_COMMUNICATION,
		nil, time.Now().Unix())
	return err
}

func TestEventProxyReceiveAndSendMessage(t *testing.T) {
	// Init Kafka client
	log.SetAllLogLevel(log.FatalLevel)
	cTkc := mock_kafka.NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}

	numEvents := 10
	resp := make(chan string)
	go waitForKafkaEvents(cTkc, &topic, numEvents, resp)

	// Init Event Proxy
	ep := NewEventProxy(MsgClient(cTkc), MsgTopic(topic))
	go ep.Start()
	time.Sleep(1 * time.Millisecond)
	for i := 0; i < numEvents; i++ {
		go func(ID int) {
			err := createAndSendEvent(ep, strconv.Itoa(ID))
			assert.Nil(t, err)
		}(i)
	}
	val := <-resp
	assert.Equal(t, val, "ok")
	go ep.Stop()
	go waitForEventProxyToStop(ep, resp)
	val = <-resp
	assert.Equal(t, val, "ok")
}

func TestEventProxyStopWhileSendingEvents(t *testing.T) {
	// Init Kafka client
	log.SetAllLogLevel(log.FatalLevel)
	cTkc := mock_kafka.NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}

	numEvents := 10
	resp := make(chan string)
	// Init Event Proxy
	ep := NewEventProxy(MsgClient(cTkc), MsgTopic(topic))
	go ep.Start()
	time.Sleep(1 * time.Millisecond)
	for i := 0; i < numEvents; i++ {
		go func(ID int) {
			err := createAndSendEvent(ep, strconv.Itoa(ID))
			assert.Nil(t, err)
		}(i)
	}
	// In this case we cannot guarantee how many events are send before
	// sending the last event(stopping event proxy), any event send before Stop would be received.
	go ep.Stop()
	go waitForEventProxyToStop(ep, resp)
	val := <-resp
	assert.Equal(t, val, "ok")
}

func TestEventProxyStopWhenNoEventsSend(t *testing.T) {
	// Init Kafka client
	log.SetAllLogLevel(log.FatalLevel)
	cTkc := mock_kafka.NewKafkaClient()
	topic := kafka.Topic{Name: "myTopic"}
	resp := make(chan string)
	// Init Event Proxy
	ep := NewEventProxy(MsgClient(cTkc), MsgTopic(topic))
	go ep.Start()
	time.Sleep(1 * time.Millisecond)
	go ep.Stop()
	go waitForEventProxyToStop(ep, resp)
	val := <-resp
	assert.Equal(t, val, "ok")
}
