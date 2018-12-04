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
	ca "github.com/opencord/voltha-go/protos/inter_container"
	"time"
)

const (
	PartitionConsumer = iota
	GroupCustomer     = iota
)

const (
	DefaultKafkaHost                = "127.0.0.1"
	DefaultKafkaPort                = 9092
	DefaultGroupName                = "rw_core"
	DefaultSleepOnError             = 1
	DefaultProducerFlushFrequency   = 5
	DefaultProducerFlushMessages    = 1
	DefaultProducerFlushMaxmessages = 5
	DefaultProducerReturnSuccess    = true
	DefaultProducerReturnErrors     = true
	DefaultProducerRetryMax         = 3
	DefaultProducerRetryBackoff     = time.Millisecond * 100
	DefaultConsumerMaxwait          = 10
	DefaultMaxProcessingTime        = 100
	DefaultConsumerType             = PartitionConsumer
	DefaultNumberPartitions         = 3
	DefaultNumberReplicas           = 1
	DefaultAutoCreateTopic          = false
)

// MsgClient represents the set of APIs  a Kafka MsgClient must implement
type Client interface {
	Start() error
	Stop()
	CreateTopic(topic *Topic, numPartition int, repFactor int) error
	DeleteTopic(topic *Topic) error
	Subscribe(topic *Topic) (<-chan *ca.InterContainerMessage, error)
	UnSubscribe(topic *Topic, ch <-chan *ca.InterContainerMessage) error
	Send(msg interface{}, topic *Topic, keys ...string) error
}
