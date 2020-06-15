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
	"fmt"
	"time"
)

// RW Core service default constants
const (
	ConsulStoreName                  = "consul"
	EtcdStoreName                    = "etcd"
	defaultGrpcAddress               = ":50057"
	defaultKafkaAdapterAddress       = "127.0.0.1:9092"
	defaultKafkaClusterAddress       = "127.0.0.1:9094"
	defaultKVStoreType               = EtcdStoreName
	defaultKVStoreTimeout            = 5 * time.Second
	defaultKVStoreAddress            = "127.0.0.1:2379" // Consul = 8500; Etcd = 2379
	defaultKVTxnKeyDelTime           = 60
	defaultLogLevel                  = "WARN"
	defaultBanner                    = false
	defaultDisplayVersionOnly        = false
	defaultCoreTopic                 = "rwcore"
	defaultRWCoreEndpoint            = "rwcore"
	defaultRWCoreKey                 = "pki/voltha.key"
	defaultRWCoreCert                = "pki/voltha.crt"
	defaultRWCoreCA                  = "pki/voltha-CA.pem"
	defaultLongRunningRequestTimeout = 2000 * time.Millisecond
	defaultDefaultRequestTimeout     = 1000 * time.Millisecond
	defaultCoreTimeout               = 1000 * time.Millisecond
	defaultCoreBindingKey            = "voltha_backend_name"
	defaultMaxConnectionRetries      = -1 // retries forever
	defaultConnectionRetryInterval   = 2 * time.Second
	defaultLiveProbeInterval         = 60 * time.Second
	defaultNotLiveProbeInterval      = 5 * time.Second // Probe more frequently when not alive
	defaultProbeAddress              = ":8080"
	defaultTraceEnabled              = false
	defaultTraceAgentAddress         = "127.0.0.1:6831"
	defaultLogCorrelationEnabled     = true
)

// RWCoreFlags represents the set of configurations used by the read-write core service
type RWCoreFlags struct {
	// Command line parameters
	RWCoreEndpoint            string
	GrpcAddress               string
	KafkaAdapterAddress       string
	KafkaClusterAddress       string
	KVStoreType               string
	KVStoreTimeout            time.Duration
	KVStoreAddress            string
	KVTxnKeyDelTime           int
	CoreTopic                 string
	LogLevel                  string
	Banner                    bool
	DisplayVersionOnly        bool
	RWCoreKey                 string
	RWCoreCert                string
	RWCoreCA                  string
	LongRunningRequestTimeout time.Duration
	DefaultRequestTimeout     time.Duration
	DefaultCoreTimeout        time.Duration
	CoreBindingKey            string
	MaxConnectionRetries      int
	ConnectionRetryInterval   time.Duration
	LiveProbeInterval         time.Duration
	NotLiveProbeInterval      time.Duration
	ProbeAddress              string
	TraceEnabled              bool
	TraceAgentAddress         string
	LogCorrelationEnabled     bool
}

// NewRWCoreFlags returns a new RWCore config
func NewRWCoreFlags() *RWCoreFlags {
	var rwCoreFlag = RWCoreFlags{ // Default values
		RWCoreEndpoint:            defaultRWCoreEndpoint,
		GrpcAddress:               defaultGrpcAddress,
		KafkaAdapterAddress:       defaultKafkaAdapterAddress,
		KafkaClusterAddress:       defaultKafkaClusterAddress,
		KVStoreType:               defaultKVStoreType,
		KVStoreTimeout:            defaultKVStoreTimeout,
		KVStoreAddress:            defaultKVStoreAddress,
		KVTxnKeyDelTime:           defaultKVTxnKeyDelTime,
		CoreTopic:                 defaultCoreTopic,
		LogLevel:                  defaultLogLevel,
		Banner:                    defaultBanner,
		DisplayVersionOnly:        defaultDisplayVersionOnly,
		RWCoreKey:                 defaultRWCoreKey,
		RWCoreCert:                defaultRWCoreCert,
		RWCoreCA:                  defaultRWCoreCA,
		DefaultRequestTimeout:     defaultDefaultRequestTimeout,
		LongRunningRequestTimeout: defaultLongRunningRequestTimeout,
		DefaultCoreTimeout:        defaultCoreTimeout,
		CoreBindingKey:            defaultCoreBindingKey,
		MaxConnectionRetries:      defaultMaxConnectionRetries,
		ConnectionRetryInterval:   defaultConnectionRetryInterval,
		LiveProbeInterval:         defaultLiveProbeInterval,
		NotLiveProbeInterval:      defaultNotLiveProbeInterval,
		ProbeAddress:              defaultProbeAddress,
		TraceEnabled:              defaultTraceEnabled,
		TraceAgentAddress:         defaultTraceAgentAddress,
		LogCorrelationEnabled:     defaultLogCorrelationEnabled,
	}
	return &rwCoreFlag
}

// ParseCommandArguments parses the arguments when running read-write core service
func (cf *RWCoreFlags) ParseCommandArguments() {

	help := fmt.Sprintf("RW core endpoint address")
	flag.StringVar(&(cf.RWCoreEndpoint), "vcore-endpoint", defaultRWCoreEndpoint, help)

	help = fmt.Sprintf("GRPC server - address")
	flag.StringVar(&(cf.GrpcAddress), "grpc_address", defaultGrpcAddress, help)

	help = fmt.Sprintf("Kafka - Adapter messaging address")
	flag.StringVar(&(cf.KafkaAdapterAddress), "kafka_adapter_address", defaultKafkaAdapterAddress, help)

	help = fmt.Sprintf("Kafka - Cluster messaging address")
	flag.StringVar(&(cf.KafkaClusterAddress), "kafka_cluster_address", defaultKafkaClusterAddress, help)

	help = fmt.Sprintf("RW Core topic")
	flag.StringVar(&(cf.CoreTopic), "rw_core_topic", defaultCoreTopic, help)

	flag.Bool("in_competing_mode", false, "deprecated")

	help = fmt.Sprintf("KV store type")
	flag.StringVar(&(cf.KVStoreType), "kv_store_type", defaultKVStoreType, help)

	help = fmt.Sprintf("The default timeout when making a kv store request")
	flag.DurationVar(&(cf.KVStoreTimeout), "kv_store_request_timeout", defaultKVStoreTimeout, help)

	help = fmt.Sprintf("KV store address")
	flag.StringVar(&(cf.KVStoreAddress), "kv_store_address", defaultKVStoreAddress, help)

	help = fmt.Sprintf("The time to wait before deleting a completed transaction key")
	flag.IntVar(&(cf.KVTxnKeyDelTime), "kv_txn_delete_time", defaultKVTxnKeyDelTime, help)

	help = fmt.Sprintf("Log level")
	flag.StringVar(&(cf.LogLevel), "log_level", defaultLogLevel, help)

	help = fmt.Sprintf("Timeout for long running request")
	flag.DurationVar(&(cf.LongRunningRequestTimeout), "timeout_long_request", defaultLongRunningRequestTimeout, help)

	help = fmt.Sprintf("Default timeout for regular request")
	flag.DurationVar(&(cf.DefaultRequestTimeout), "timeout_request", defaultDefaultRequestTimeout, help)

	help = fmt.Sprintf("Default Core timeout")
	flag.DurationVar(&(cf.DefaultCoreTimeout), "core_timeout", defaultCoreTimeout, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&cf.Banner, "banner", defaultBanner, help)

	help = fmt.Sprintf("Show version information and exit")
	flag.BoolVar(&cf.DisplayVersionOnly, "version", defaultDisplayVersionOnly, help)

	help = fmt.Sprintf("The name of the meta-key whose value is the rw-core group to which the ofagent is bound")
	flag.StringVar(&(cf.CoreBindingKey), "core_binding_key", defaultCoreBindingKey, help)

	help = fmt.Sprintf("The number of retries to connect to a dependent component")
	flag.IntVar(&(cf.MaxConnectionRetries), "max_connection_retries", defaultMaxConnectionRetries, help)

	help = fmt.Sprintf("The number of seconds between each connection retry attempt")
	flag.DurationVar(&(cf.ConnectionRetryInterval), "connection_retry_interval", defaultConnectionRetryInterval, help)

	help = fmt.Sprintf("The number of seconds between liveness probes while in a live state")
	flag.DurationVar(&(cf.LiveProbeInterval), "live_probe_interval", defaultLiveProbeInterval, help)

	help = fmt.Sprintf("The number of seconds between liveness probes while in a not live state")
	flag.DurationVar(&(cf.NotLiveProbeInterval), "not_live_probe_interval", defaultNotLiveProbeInterval, help)

	help = fmt.Sprintf("The address on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.StringVar(&(cf.ProbeAddress), "probe_address", defaultProbeAddress, help)

	help = fmt.Sprintf("Whether to send logs to tracing agent?")
	flag.BoolVar(&(cf.TraceEnabled), "trace_enabled", defaultTraceEnabled, help)

	help = fmt.Sprintf("The address of tracing agent to which span info should be sent.")
	flag.StringVar(&(cf.TraceAgentAddress), "trace_agent_address", defaultTraceAgentAddress, help)

	help = fmt.Sprintf("Whether to enrich log statements with fields denoting operation being executed for achieving correlation?")
	flag.BoolVar(&(cf.LogCorrelationEnabled), "log_correlation_enabled", defaultLogCorrelationEnabled, help)

	flag.Parse()
}
