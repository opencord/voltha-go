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
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v6/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v6/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxConcurrentMessage = 100
)

// static check to ensure KafkaClient implements kafka.Client
var _ kafka.Client = &KafkaClient{}

type KafkaClient struct {
	topicsChannelMap map[string][]chan proto.Message
	lock             sync.RWMutex
	alive            bool
	livenessMutex    sync.Mutex
	liveness         chan bool
}

func NewKafkaClient() *KafkaClient {
	return &KafkaClient{
		topicsChannelMap: make(map[string][]chan proto.Message),
		lock:             sync.RWMutex{},
	}
}

func (kc *KafkaClient) Start(ctx context.Context) error {
	logger.Debug(ctx, "kafka-client-started")
	return nil
}

func (kc *KafkaClient) Stop(ctx context.Context) {
	kc.lock.Lock()
	defer kc.lock.Unlock()
	for topic, chnls := range kc.topicsChannelMap {
		for _, c := range chnls {
			close(c)
		}
		delete(kc.topicsChannelMap, topic)
	}
	logger.Debug(ctx, "kafka-client-stopped")
}

func (kc *KafkaClient) CreateTopic(ctx context.Context, topic *kafka.Topic, numPartition int, repFactor int) error {
	logger.Debugw(ctx, "CreatingTopic", log.Fields{"topic": topic.Name, "numPartition": numPartition, "replicationFactor": repFactor})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	if _, ok := kc.topicsChannelMap[topic.Name]; ok {
		return fmt.Errorf("Topic %s already exist", topic.Name)
	}
	ch := make(chan proto.Message)
	kc.topicsChannelMap[topic.Name] = append(kc.topicsChannelMap[topic.Name], ch)
	return nil
}

func (kc *KafkaClient) DeleteTopic(ctx context.Context, topic *kafka.Topic) error {
	logger.Debugw(ctx, "DeleteTopic", log.Fields{"topic": topic.Name})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	delete(kc.topicsChannelMap, topic.Name)
	return nil
}

func (kc *KafkaClient) Subscribe(ctx context.Context, topic *kafka.Topic, kvArgs ...*kafka.KVArg) (<-chan proto.Message, error) {
	logger.Debugw(ctx, "Subscribe", log.Fields{"topic": topic.Name, "args": kvArgs})
	kc.lock.Lock()
	defer kc.lock.Unlock()
	ch := make(chan proto.Message, maxConcurrentMessage)
	kc.topicsChannelMap[topic.Name] = append(kc.topicsChannelMap[topic.Name], ch)
	return ch, nil
}

func removeChannel(s []chan proto.Message, i int) []chan proto.Message {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

func (kc *KafkaClient) UnSubscribe(ctx context.Context, topic *kafka.Topic, ch <-chan proto.Message) error {
	logger.Debugw(ctx, "UnSubscribe", log.Fields{"topic": topic.Name})
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

func (kc *KafkaClient) SubscribeForMetadata(ctx context.Context, _ func(fromTopic string, timestamp time.Time)) {
	logger.Debug(ctx, "SubscribeForMetadata - unimplemented")
}

func (kc *KafkaClient) Send(ctx context.Context, msg interface{}, topic *kafka.Topic, keys ...string) error {
	// Assert message is a proto message
	protoMsg, ok := msg.(proto.Message)
	if !ok {
		logger.Warnw(ctx, "message-not-a-proto-message", log.Fields{"msg": msg})
		return status.Error(codes.InvalidArgument, "msg-not-a-proto-msg")
	}
	kc.lock.RLock()
	defer kc.lock.RUnlock()
	for _, ch := range kc.topicsChannelMap[topic.Name] {
		select {
		case ch <- protoMsg:
			logger.Debugw(ctx, "publishing", log.Fields{"toTopic": topic.Name, "msg": protoMsg})
		default:
			logger.Debugw(ctx, "ignoring-event-channel-busy", log.Fields{"toTopic": topic.Name, "msg": protoMsg})
		}
	}
	return nil
}

func (kc *KafkaClient) SendLiveness(ctx context.Context) error {
	kc.livenessMutex.Lock()
	defer kc.livenessMutex.Unlock()
	if kc.liveness != nil {
		kc.liveness <- true // I am a mock
	}
	return nil
}

func (kc *KafkaClient) EnableLivenessChannel(ctx context.Context, enable bool) chan bool {
	logger.Infow(ctx, "kafka-enable-liveness-channel", log.Fields{"enable": enable})
	if enable {
		kc.livenessMutex.Lock()
		defer kc.livenessMutex.Unlock()
		if kc.liveness == nil {
			logger.Info(ctx, "kafka-create-liveness-channel")
			kc.liveness = make(chan bool, 10)
			// post intial state to the channel
			kc.liveness <- kc.alive
		}
	} else {
		panic("Turning off liveness reporting is not supported")
	}
	return kc.liveness
}

func (kc *KafkaClient) EnableHealthinessChannel(ctx context.Context, enable bool) chan bool {
	logger.Debug(ctx, "EnableHealthinessChannel - unimplemented")
	return nil
}
