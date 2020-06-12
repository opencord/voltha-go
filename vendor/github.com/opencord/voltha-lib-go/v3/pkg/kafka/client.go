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
package kafka

import (
	"context"
	ca "github.com/opencord/voltha-protos/v3/go/inter_container"
	"time"
)

const (
	PartitionConsumer = iota
	GroupCustomer     = iota
)

const (
	OffsetNewest = -1
	OffsetOldest = -2
)

const (
	GroupIdKey = "groupId"
	Offset     = "offset"
)

const (
	DefaultKafkaHost                = "127.0.0.1"
	DefaultKafkaPort                = 9092
	DefaultKafkaAddress             = DefaultKafkaHost + ":" + string(DefaultKafkaPort)
	DefaultGroupName                = "voltha"
	DefaultSleepOnError             = 1
	DefaultProducerFlushFrequency   = 10
	DefaultProducerFlushMessages    = 10
	DefaultProducerFlushMaxmessages = 100
	DefaultProducerReturnSuccess    = true
	DefaultProducerReturnErrors     = true
	DefaultProducerRetryMax         = 3
	DefaultProducerRetryBackoff     = time.Millisecond * 100
	DefaultConsumerMaxwait          = 100
	DefaultMaxProcessingTime        = 100
	DefaultConsumerType             = PartitionConsumer
	DefaultNumberPartitions         = 3
	DefaultNumberReplicas           = 1
	DefaultAutoCreateTopic          = false
	DefaultMetadataMaxRetry         = 3
	DefaultLivenessChannelInterval  = time.Second * 30
)

// MsgClient represents the set of APIs  a Kafka MsgClient must implement
type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	CreateTopic(ctx context.Context, topic *Topic, numPartition int, repFactor int) error
	DeleteTopic(ctx context.Context, topic *Topic) error
	Subscribe(ctx context.Context, topic *Topic, kvArgs ...*KVArg) (<-chan *ca.InterContainerMessage, error)
	UnSubscribe(ctx context.Context, topic *Topic, ch <-chan *ca.InterContainerMessage) error
	SubscribeForMetadata(context.Context, func(fromTopic string, timestamp time.Time))
	Send(ctx context.Context, msg interface{}, topic *Topic, keys ...string) error
	SendLiveness(ctx context.Context) error
	EnableLivenessChannel(ctx context.Context, enable bool) chan bool
	EnableHealthinessChannel(ctx context.Context, enable bool) chan bool
}
