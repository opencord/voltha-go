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

package config

import (
	"time"
)

// RW Core service default constants
const (
	ConsulStoreName = "consul"
	EtcdStoreName   = "etcd"
)

// RWCoreFlags represents the set of configurations used by the read-write core service
type RWCoreFlags struct {
	// Command line parameters
	RWCoreEndpoint            string        `long:"vcore-endpoint" short:"e" default:"rwcore" desc:"RW core endpoint address"`
	GrpcHost                  string        `default:"0.0.0.0" desc:"GRPC server - host"`
	GrpcPort                  int           `default:"50057" desc:"GRPC server - port"`
	KafkaAdapterHost          string        `default:"127.0.0.1" desc:"Kafka - Adapter messaging host"`
	KafkaAdapterPort          int           `default:"9092" desc:"Kafka - Adapter messaging port"`
	KafkaClusterHost          string        `default:"127.0.0.1" desc:"Kafka - Cluster messaging host"`
	KafkaClusterPort          int           `default:"9094" desc:"Kafka - Cluster messaging port"`
	KVStoreType               string        `default:"etcd" desc:"KV store type"`
	KVStoreTimeout            time.Duration `long:"kv_store_request_timeout" default:"5s" desc:"The default timeout when making a KV store request"`
	KVStoreHost               string        `default:"127.0.0.1" desc:"KV store host"`
	KVStorePort               int           `default:"2379" desc:"KV store port"`
	KVTxnKeyDelTime           int           `long:"kv_txn_delete_time" default:"60" desc:"The time to wait before deleting a completed transaction key"`
	CoreTopic                 string        `long:"rw_core_topic" default:"rwcore" desc:"RW Core topic"`
	LogLevel                  string        `default:"WARN" desc:"Initial log level"`
	Banner                    bool          `default:"true" desc:"Show startup banner"`
	DisplayVersionOnly        bool          `default:"version" default:"false" desc:"Show version information and exit"`
	RWCoreKey                 string        `ignored:"true"`
	RWCoreCert                string        `ignored:"true"`
	RWCoreCA                  string        `ignored:"true"`
	AffinityRouterTopic       string        `default:"affinityRouter" desc:"Affinity Router topic"`
	InCompetingMode           bool          `default:"false" desc:"In competing mode - two cores competing to handle a transaction"`
	LongRunningRequestTimeout time.Duration `long:"timeout_long_request" default:"2s" desc:"Timeout for long running request"`
	DefaultRequestTimeout     time.Duration `long:"timeout_request" default:"1s" desc:"Default timeout for regular request"`
	DefaultCoreTimeout        time.Duration `long:"core_timeout" default:"1s" desc:"Default Core timeout"`
	CoreBindingKey            string        `default:"voltha_backend_name" desc:"The name of the meta-key whose value is the rw-core group to which the ofagent is bound"`
	MaxConnectionRetries      int           `default:"-1" desc:"The number of retries to connect to a dependent component"`
	ConnectionRetryInterval   time.Duration `default:"2s" desc:"The number of seconds between each connection retry attempt"`
	LiveProbeInterval         time.Duration `default:"60s" desc:"The number of seconds between liveness probes while in a live state"`
	NotLiveProbeInterval      time.Duration `default:"5s" desc:"The number of seconds between liveness probes while in a not live state"`
	ProbeHost                 string        `default:"0.0.0.0" desc:"The host on which to listen to answer liveness and readiness probe queries over HTTP"`
	ProbePort                 int           `default:"8080" desc:"The port on which to listen to answer liveness and readiness probe queries over HTTP"`
}
