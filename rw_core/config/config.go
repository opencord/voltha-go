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
	"flag"
	"time"
)

// RW Core service default constants
const (
	EtcdStoreName                      = "etcd"
	defaultGrpcAddress                 = ":50057"
	defaultKafkaAdapterAddress         = "127.0.0.1:9092"
	defaultKafkaClusterAddress         = "127.0.0.1:9094"
	defaultKVStoreType                 = EtcdStoreName
	defaultKVStoreTimeout              = 5 * time.Second
	defaultKVStoreAddress              = "127.0.0.1:2379" // Etcd = 2379
	defaultKVTxnKeyDelTime             = 60
	defaultLogLevel                    = "WARN"
	defaultBanner                      = false
	defaultDisplayVersionOnly          = false
	defaultCoreTopic                   = "rwcore"
	defaultEventTopic                  = "voltha.events"
	defaultRWCoreEndpoint              = "rwcore"
	defaultRWCoreKey                   = "pki/voltha.key"
	defaultRWCoreCert                  = "pki/voltha.crt"
	defaultRWCoreCA                    = "pki/voltha-CA.pem"
	defaultLongRunningRequestTimeout   = 2000 * time.Millisecond
	defaultDefaultRequestTimeout       = 1000 * time.Millisecond
	defaultCoreTimeout                 = 1000 * time.Millisecond
	defaultCoreBindingKey              = "voltha_backend_name"
	defaultMaxConnectionRetries        = -1 // retries forever
	defaultConnectionRetryInterval     = 2 * time.Second
	defaultLiveProbeInterval           = 60 * time.Second
	defaultNotLiveProbeInterval        = 5 * time.Second // Probe more frequently when not alive
	defaultProbeAddress                = ":8080"
	defaultTraceEnabled                = false
	defaultTraceAgentAddress           = "127.0.0.1:6831"
	defaultLogCorrelationEnabled       = true
	defaultVolthaStackID               = "voltha"
	defaultBackoffRetryInitialInterval = 500 * time.Millisecond
	defaultBackoffRetryMaxElapsedTime  = 0
	defaultBackoffRetryMaxInterval     = 1 * time.Minute
)

// RWCoreFlags represents the set of configurations used by the read-write core service
type RWCoreFlags struct {
	// Command line parameters
	RWCoreEndpoint              string
	GrpcAddress                 string
	KafkaAdapterAddress         string
	KafkaClusterAddress         string
	KVStoreType                 string
	KVStoreTimeout              time.Duration
	KVStoreAddress              string
	KVTxnKeyDelTime             int
	CoreTopic                   string
	EventTopic                  string
	LogLevel                    string
	Banner                      bool
	DisplayVersionOnly          bool
	RWCoreKey                   string
	RWCoreCert                  string
	RWCoreCA                    string
	LongRunningRequestTimeout   time.Duration
	DefaultRequestTimeout       time.Duration
	DefaultCoreTimeout          time.Duration
	CoreBindingKey              string
	MaxConnectionRetries        int
	ConnectionRetryInterval     time.Duration
	LiveProbeInterval           time.Duration
	NotLiveProbeInterval        time.Duration
	ProbeAddress                string
	TraceEnabled                bool
	TraceAgentAddress           string
	LogCorrelationEnabled       bool
	VolthaStackID               string
	BackoffRetryInitialInterval time.Duration
	BackoffRetryMaxElapsedTime  time.Duration
	BackoffRetryMaxInterval     time.Duration
}

// NewRWCoreFlags returns a new RWCore config
func NewRWCoreFlags() *RWCoreFlags {
	var rwCoreFlag = RWCoreFlags{ // Default values
		RWCoreEndpoint:              defaultRWCoreEndpoint,
		GrpcAddress:                 defaultGrpcAddress,
		KafkaAdapterAddress:         defaultKafkaAdapterAddress,
		KafkaClusterAddress:         defaultKafkaClusterAddress,
		KVStoreType:                 defaultKVStoreType,
		KVStoreTimeout:              defaultKVStoreTimeout,
		KVStoreAddress:              defaultKVStoreAddress,
		KVTxnKeyDelTime:             defaultKVTxnKeyDelTime,
		CoreTopic:                   defaultCoreTopic,
		EventTopic:                  defaultEventTopic,
		LogLevel:                    defaultLogLevel,
		Banner:                      defaultBanner,
		DisplayVersionOnly:          defaultDisplayVersionOnly,
		RWCoreKey:                   defaultRWCoreKey,
		RWCoreCert:                  defaultRWCoreCert,
		RWCoreCA:                    defaultRWCoreCA,
		DefaultRequestTimeout:       defaultDefaultRequestTimeout,
		LongRunningRequestTimeout:   defaultLongRunningRequestTimeout,
		DefaultCoreTimeout:          defaultCoreTimeout,
		CoreBindingKey:              defaultCoreBindingKey,
		MaxConnectionRetries:        defaultMaxConnectionRetries,
		ConnectionRetryInterval:     defaultConnectionRetryInterval,
		LiveProbeInterval:           defaultLiveProbeInterval,
		NotLiveProbeInterval:        defaultNotLiveProbeInterval,
		ProbeAddress:                defaultProbeAddress,
		TraceEnabled:                defaultTraceEnabled,
		TraceAgentAddress:           defaultTraceAgentAddress,
		LogCorrelationEnabled:       defaultLogCorrelationEnabled,
		VolthaStackID:               defaultVolthaStackID,
		BackoffRetryInitialInterval: defaultBackoffRetryInitialInterval,
		BackoffRetryMaxElapsedTime:  defaultBackoffRetryMaxElapsedTime,
		BackoffRetryMaxInterval:     defaultBackoffRetryMaxInterval,
	}
	return &rwCoreFlag
}

// ParseCommandArguments parses the arguments when running read-write core service
func (cf *RWCoreFlags) ParseCommandArguments() {

	flag.StringVar(&(cf.RWCoreEndpoint), "vcore-endpoint", defaultRWCoreEndpoint, "RW core endpoint address")

	flag.StringVar(&(cf.GrpcAddress), "grpc_address", defaultGrpcAddress, "GRPC server - address")

	flag.StringVar(&(cf.KafkaAdapterAddress), "kafka_adapter_address", defaultKafkaAdapterAddress, "Kafka - Adapter messaging address")

	flag.StringVar(&(cf.KafkaClusterAddress), "kafka_cluster_address", defaultKafkaClusterAddress, "Kafka - Cluster messaging address")

	flag.StringVar(&(cf.CoreTopic), "rw_core_topic", defaultCoreTopic,  "RW Core topic")

	flag.StringVar(&(cf.EventTopic), "event_topic", defaultEventTopic, "RW Core Event topic")

	flag.Bool("in_competing_mode", false, "deprecated")

	flag.StringVar(&(cf.KVStoreType), "kv_store_type", defaultKVStoreType, "KV store type")

	flag.DurationVar(&(cf.KVStoreTimeout), "kv_store_request_timeout", defaultKVStoreTimeout, "The default timeout when making a kv store request")

	flag.StringVar(&(cf.KVStoreAddress), "kv_store_address", defaultKVStoreAddress, "KV store address")

	flag.IntVar(&(cf.KVTxnKeyDelTime), "kv_txn_delete_time", defaultKVTxnKeyDelTime, "The time to wait before deleting a completed transaction key")

	flag.StringVar(&(cf.LogLevel), "log_level", defaultLogLevel, "Log level")

	flag.DurationVar(&(cf.LongRunningRequestTimeout), "timeout_long_request", defaultLongRunningRequestTimeout, "Timeout for long running request")

	flag.DurationVar(&(cf.DefaultRequestTimeout), "timeout_request", defaultDefaultRequestTimeout, "Default timeout for regular request")

	flag.DurationVar(&(cf.DefaultCoreTimeout), "core_timeout", defaultCoreTimeout, "Default Core timeout")

	flag.BoolVar(&cf.Banner, "banner", defaultBanner, "Show startup banner log lines")

	flag.BoolVar(&cf.DisplayVersionOnly, "version", defaultDisplayVersionOnly, "Show version information and exit")

	flag.StringVar(&(cf.CoreBindingKey), "core_binding_key", defaultCoreBindingKey, "The name of the meta-key whose value is the rw-core group to which the ofagent is bound")

	flag.IntVar(&(cf.MaxConnectionRetries), "max_connection_retries", defaultMaxConnectionRetries, "The number of retries to connect to a dependent component")

	flag.DurationVar(&(cf.ConnectionRetryInterval), "connection_retry_interval", defaultConnectionRetryInterval, "The number of seconds between each connection retry attempt")

	flag.DurationVar(&(cf.LiveProbeInterval), "live_probe_interval", defaultLiveProbeInterval, "The number of seconds between liveness probes while in a live state")

	flag.DurationVar(&(cf.NotLiveProbeInterval), "not_live_probe_interval", defaultNotLiveProbeInterval, "The number of seconds between liveness probes while in a not live state")

	flag.StringVar(&(cf.ProbeAddress), "probe_address", defaultProbeAddress, "The address on which to listen to answer liveness and readiness probe queries over HTTP")

	flag.BoolVar(&(cf.TraceEnabled), "trace_enabled", defaultTraceEnabled, "Whether to send logs to tracing agent?")

	flag.StringVar(&(cf.TraceAgentAddress), "trace_agent_address", defaultTraceAgentAddress, "The address of tracing agent to which span info should be sent")

	flag.BoolVar(&(cf.LogCorrelationEnabled), "log_correlation_enabled", defaultLogCorrelationEnabled, "Whether to enrich log statements with fields denoting operation being executed for achieving correlation?")

	flag.StringVar(&cf.VolthaStackID, "stack_id", defaultVolthaStackID, "ID for the current voltha stack")

	flag.DurationVar(&(cf.BackoffRetryInitialInterval), "backoff_retry_initial_interval", defaultBackoffRetryInitialInterval, "The initial number of milliseconds an exponential backoff will wait before a retry")

	flag.DurationVar(&(cf.BackoffRetryMaxElapsedTime), "backoff_retry_max_elapsed_time", defaultBackoffRetryMaxElapsedTime, "The maximum number of milliseconds an exponential backoff can elasped")

	flag.DurationVar(&(cf.BackoffRetryMaxInterval), "backoff_retry_max_interval", defaultBackoffRetryMaxInterval, "The maximum number of milliseconds of an exponential backoff interval")

	flag.Parse()
}
