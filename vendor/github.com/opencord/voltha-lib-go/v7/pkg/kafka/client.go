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
 *
 *
 * NOTE:  The kafka client is used to publish events on Kafka in voltha
 * release 2.9.  It is no longer used for inter voltha container
 * communication.
 */
package kafka

import (
	"context"
	"time"

	"github.com/golang/protobuf/proto"
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
	DefaultKafkaAddress             = "127.0.0.1:9092"
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
	DefaultMaxRetries               = 3
	DefaultLivenessChannelInterval  = time.Second * 30
)

// MsgClient represents the set of APIs  a Kafka MsgClient must implement
type Client interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	CreateTopic(ctx context.Context, topic *Topic, numPartition int, repFactor int) error
	DeleteTopic(ctx context.Context, topic *Topic) error
	Subscribe(ctx context.Context, topic *Topic, kvArgs ...*KVArg) (<-chan proto.Message, error)
	UnSubscribe(ctx context.Context, topic *Topic, ch <-chan proto.Message) error
	SubscribeForMetadata(context.Context, func(fromTopic string, timestamp time.Time))
	Send(ctx context.Context, msg interface{}, topic *Topic, keys ...string) error
	SendLiveness(ctx context.Context) error
	EnableLivenessChannel(ctx context.Context, enable bool) chan bool
	EnableHealthinessChannel(ctx context.Context, enable bool) chan bool
}
