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

	help := "RW core endpoint address"
	flag.StringVar(&(cf.RWCoreEndpoint), "vcore-endpoint", defaultRWCoreEndpoint, help)

	help = "GRPC server - address"
	flag.StringVar(&(cf.GrpcAddress), "grpc_address", defaultGrpcAddress, help)

	help = "Kafka - Adapter messaging address"
	flag.StringVar(&(cf.KafkaAdapterAddress), "kafka_adapter_address", defaultKafkaAdapterAddress, help)

	help = "Kafka - Cluster messaging address"
	flag.StringVar(&(cf.KafkaClusterAddress), "kafka_cluster_address", defaultKafkaClusterAddress, help)

	help = "RW Core topic"
	flag.StringVar(&(cf.CoreTopic), "rw_core_topic", defaultCoreTopic, help)

	help = "RW Core Event topic"
	flag.StringVar(&(cf.EventTopic), "event_topic", defaultEventTopic, help)

	flag.Bool("in_competing_mode", false, "deprecated")

	help = "KV store type"
	flag.StringVar(&(cf.KVStoreType), "kv_store_type", defaultKVStoreType, help)

	help = "The default timeout when making a kv store request"
	flag.DurationVar(&(cf.KVStoreTimeout), "kv_store_request_timeout", defaultKVStoreTimeout, help)

	help = "KV store address"
	flag.StringVar(&(cf.KVStoreAddress), "kv_store_address", defaultKVStoreAddress, help)

	help = "The time to wait before deleting a completed transaction key"
	flag.IntVar(&(cf.KVTxnKeyDelTime), "kv_txn_delete_time", defaultKVTxnKeyDelTime, help)

	help = "Log level"
	flag.StringVar(&(cf.LogLevel), "log_level", defaultLogLevel, help)

	help = "Timeout for long running request"
	flag.DurationVar(&(cf.LongRunningRequestTimeout), "timeout_long_request", defaultLongRunningRequestTimeout, help)

	help = "Default timeout for regular request"
	flag.DurationVar(&(cf.DefaultRequestTimeout), "timeout_request", defaultDefaultRequestTimeout, help)

	help = "Default Core timeout"
	flag.DurationVar(&(cf.DefaultCoreTimeout), "core_timeout", defaultCoreTimeout, help)

	help = "Show startup banner log lines"
	flag.BoolVar(&cf.Banner, "banner", defaultBanner, help)

	help = "Show version information and exit"
	flag.BoolVar(&cf.DisplayVersionOnly, "version", defaultDisplayVersionOnly, help)

	help = "The name of the meta-key whose value is the rw-core group to which the ofagent is bound"
	flag.StringVar(&(cf.CoreBindingKey), "core_binding_key", defaultCoreBindingKey, help)

	help = "The number of retries to connect to a dependent component"
	flag.IntVar(&(cf.MaxConnectionRetries), "max_connection_retries", defaultMaxConnectionRetries, help)

	help = "The number of seconds between each connection retry attempt"
	flag.DurationVar(&(cf.ConnectionRetryInterval), "connection_retry_interval", defaultConnectionRetryInterval, help)

	help = "The number of seconds between liveness probes while in a live state"
	flag.DurationVar(&(cf.LiveProbeInterval), "live_probe_interval", defaultLiveProbeInterval, help)

	help = "The number of seconds between liveness probes while in a not live state"
	flag.DurationVar(&(cf.NotLiveProbeInterval), "not_live_probe_interval", defaultNotLiveProbeInterval, help)

	help = "The address on which to listen to answer liveness and readiness probe queries over HTTP."
	flag.StringVar(&(cf.ProbeAddress), "probe_address", defaultProbeAddress, help)

	help = "Whether to send logs to tracing agent?"
	flag.BoolVar(&(cf.TraceEnabled), "trace_enabled", defaultTraceEnabled, help)

	help = "The address of tracing agent to which span info should be sent."
	flag.StringVar(&(cf.TraceAgentAddress), "trace_agent_address", defaultTraceAgentAddress, help)

	help = "Whether to enrich log statements with fields denoting operation being executed for achieving correlation?"
	flag.BoolVar(&(cf.LogCorrelationEnabled), "log_correlation_enabled", defaultLogCorrelationEnabled, help)

	help = "ID for the current voltha stack"
	flag.StringVar(&cf.VolthaStackID, "stack_id", defaultVolthaStackID, help)

	help = "The initial number of milliseconds an exponential backoff will wait before a retry"
	flag.DurationVar(&(cf.BackoffRetryInitialInterval), "backoff_retry_initial_interval", defaultBackoffRetryInitialInterval, help)

	help = "The maximum number of milliseconds an exponential backoff can elasped"
	flag.DurationVar(&(cf.BackoffRetryMaxElapsedTime), "backoff_retry_max_elapsed_time", defaultBackoffRetryMaxElapsedTime, help)

	help = "The maximum number of milliseconds of an exponential backoff interval"
	flag.DurationVar(&(cf.BackoffRetryMaxInterval), "backoff_retry_max_interval", defaultBackoffRetryMaxInterval, help)

	flag.Parse()
}
