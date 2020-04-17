/*
 * Copyright 2019-present Open Networking Foundation

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
package kafka

import (
	"fmt"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// static check to ensure KafkaClient implements kafka.Client
var _ kafka.Client = &KafkaClient{}

type KafkaClient struct {
	topicsChannelMap map[string][]chan *ic.InterContainerMessage
	lock             sync.RWMutex
}

func NewKafkaClient() *KafkaClient {
	return &KafkaClient{
		topicsChannelMap: make(map[string][]chan *ic.InterContainerMessage),
		lock:             sync.RWMutex{},
	}
}

func (kc *KafkaClient) Start() error {
	logger.Debug("kafka-client-started")
	return nil
}

func (kc *KafkaClient) Stop() {
	kc.lock.Lock()
	defer kc.lock.Unlock()
	for topic, chnls := range kc.topicsChannelMap {
		for _, c := range chnls {
			close(c)
		}
		delete(kc.topicsChannelMap, topic)
	}
	logger.Debug("kafka-client-stopped")
}

func (kc *KafkaClient) CreateTopic(topic *kafka.Topic, numPartition int, repFactor int) error {
	logger.Debugw("CreatingTopic", log.Fields{"topic": topic.Name, "numPartition": numPartition, "replicationFactor": repFactor})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	if _, ok := kc.topicsChannelMap[topic.Name]; ok {
		return fmt.Errorf("Topic %s already exist", topic.Name)
	}
	ch := make(chan *ic.InterContainerMessage)
	kc.topicsChannelMap[topic.Name] = append(kc.topicsChannelMap[topic.Name], ch)
	return nil
}

func (kc *KafkaClient) DeleteTopic(topic *kafka.Topic) error {
	logger.Debugw("DeleteTopic", log.Fields{"topic": topic.Name})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	delete(kc.topicsChannelMap, topic.Name)
	return nil
}

func (kc *KafkaClient) Subscribe(topic *kafka.Topic, kvArgs ...*kafka.KVArg) (<-chan *ic.InterContainerMessage, error) {
	logger.Debugw("Subscribe", log.Fields{"topic": topic.Name, "args": kvArgs})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	ch := make(chan *ic.InterContainerMessage)
	kc.topicsChannelMap[topic.Name] = append(kc.topicsChannelMap[topic.Name], ch)
	return ch, nil
}

func removeChannel(s []chan *ic.InterContainerMessage, i int) []chan *ic.InterContainerMessage {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

func (kc *KafkaClient) UnSubscribe(topic *kafka.Topic, ch <-chan *ic.InterContainerMessage) error {
	logger.Debugw("UnSubscribe", log.Fields{"topic": topic.Name})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	if chnls, ok := kc.topicsChannelMap[topic.Name]; ok {
		idx := -1
		for i, c := range chnls {
			if c == ch {
				close(c)
				idx = i
			}
		}
		if idx >= 0 {
			kc.topicsChannelMap[topic.Name] = removeChannel(kc.topicsChannelMap[topic.Name], idx)
		}
	}
	return nil
}

func (kc *KafkaClient) SubscribeForMetadata(_ func(fromTopic string, timestamp time.Time)) {
	logger.Debug("SubscribeForMetadata - unimplemented")
}

func (kc *KafkaClient) Send(msg interface{}, topic *kafka.Topic, keys ...string) error {
	req, ok := msg.(*ic.InterContainerMessage)
	if !ok {
		return status.Error(codes.InvalidArgument, "msg-not-InterContainerMessage-type")
	}
	if req == nil {
		return status.Error(codes.InvalidArgument, "msg-nil")
	}
	kc.lock.RLock()
	defer kc.lock.RUnlock()
	for _, ch := range kc.topicsChannelMap[topic.Name] {
		logger.Debugw("Publishing", log.Fields{"fromTopic": req.Header.FromTopic, "toTopic": topic.Name, "id": req.Header.Id})
		ch <- req
	}
	return nil
}

func (kc *KafkaClient) SendLiveness() error {
	return status.Error(codes.Unimplemented, "SendLiveness")
}

func (kc *KafkaClient) EnableLivenessChannel(enable bool) chan bool {
	logger.Debug("EnableLivenessChannel - unimplemented")
	return nil
}

func (kc *KafkaClient) EnableHealthinessChannel(enable bool) chan bool {
	logger.Debug("EnableHealthinessChannel - unimplemented")
	return nil
}
