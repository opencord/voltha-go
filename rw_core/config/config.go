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
	"os"
	"strconv"
	"time"
)

// RW Core service default constants
const (
	EtcdStoreName = "etcd"
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

func lookupEnvOrString(key, def string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return def
}

func lookupEnvOrBool(key string, def bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		if bVal, err := strconv.ParseBool(val); err == nil {
			return bVal
		}
		return def
	}
	return def
}
func lookupEnvOrInt(key string, def int) int {
	if val, ok := os.LookupEnv(key); ok {
		if iVal, err := strconv.Atoi(val); err == nil {
			return iVal
		}
		return def
	}
	return def
}
func lookupEnvOrDuration(key string, def time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		if dVal, err := time.ParseDuration(val); err == nil {
			return dVal
		}
		return def
	}
	return def
}

// ParseCommandArguments parses the arguments when running read-write core service
func (cf *RWCoreFlags) ParseCommandArguments(args []string) {

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	fs.StringVar(&cf.RWCoreEndpoint, "vcore-endpoint",
		lookupEnvOrString("VCORE_ENDPOINT", "rwcore"),
		"RW core endpoint address")

	fs.StringVar(&cf.GrpcAddress, "grpc_address",
		lookupEnvOrString("GRPC_ADDRESS", ":50057"),
		"GRPC server - address")

	fs.StringVar(&cf.KafkaAdapterAddress, "kafka_adapter_address",
		lookupEnvOrString("KAFKA_ADAPTER_ADDRESS", "127.0.0.1:9092"),
		"Kafka - Adapter messaging address")

	fs.StringVar(&cf.KafkaClusterAddress, "kafka_cluster_address",
		lookupEnvOrString("KAFKA_CLUSTER_ADDRESS", "127.0.0.1:9094"),
		"Kafka - Cluster messaging address")

	fs.StringVar(&cf.CoreTopic, "rw_core_topic",
		lookupEnvOrString("RW_CORE_TOPIC", "rwcore"), "RW Core topic")

	fs.StringVar(&cf.EventTopic, "event_topic",
		lookupEnvOrString("EVENT_TOPIC", "voltha.events"), "RW Core Event topic")

	fs.StringVar(&cf.KVStoreType, "kv_store_type",
		lookupEnvOrString("KV_STORE_TYPE", EtcdStoreName), "KV store type")

	fs.DurationVar(&cf.KVStoreTimeout, "kv_store_request_timeout",
		lookupEnvOrDuration("KV_STORE_REQUEST_TIMEOUT", 5*time.Second), "The default timeout when making a kv store request")

	fs.StringVar(&cf.KVStoreAddress, "kv_store_address",
		lookupEnvOrString("KV_STORE_ADDRESS", "127.0.0.1:2379"), "KV store address")

	fs.IntVar(&cf.KVTxnKeyDelTime, "kv_txn_delete_time",
		lookupEnvOrInt("KV_TXN_DELETE_TIME", 60), "The time to wait before deleting a completed transaction key")

	fs.StringVar(&cf.LogLevel, "log_level",
		lookupEnvOrString("LOG_LEVEL", "warn"), "Log level")

	fs.DurationVar(&cf.LongRunningRequestTimeout, "timeout_long_request",
		lookupEnvOrDuration("TIMEOUT_LONG_REQUEST", 2000*time.Millisecond), "Timeout for long running request")

	fs.DurationVar(&cf.DefaultRequestTimeout, "timeout_request",
		lookupEnvOrDuration("TIMEOUT_REQUEST", 1000*time.Millisecond), "Default timeout for regular request")

	fs.DurationVar(&cf.DefaultCoreTimeout, "core_timeout",
		lookupEnvOrDuration("CORE_TIMEOUT", 1000*time.Millisecond), "Default Core timeout")

	fs.BoolVar(&cf.Banner, "banner",
		lookupEnvOrBool("BANNER", false), "Show startup banner log lines")

	fs.BoolVar(&cf.DisplayVersionOnly, "version",
		lookupEnvOrBool("VERSION", false), "Show version information and exit")

	fs.StringVar(&cf.CoreBindingKey, "core_binding_key",
		lookupEnvOrString("CORE_BINDING_KEY", "voltha_backend_name"), "The name of the meta-key whose value is the rw-core group to which the ofagent is bound")

	fs.IntVar(&cf.MaxConnectionRetries, "max_connection_retries",
		lookupEnvOrInt("MAX_CONNECTION_RETRIES", -1), "The number of retries to connect to a dependent component")

	fs.DurationVar(&cf.ConnectionRetryInterval, "connection_retry_interval",
		lookupEnvOrDuration("CONNECTION_RETRY_INTERVAL", 2*time.Second), "The number of seconds between each connection retry attempt")

	fs.DurationVar(&cf.LiveProbeInterval, "live_probe_interval",
		lookupEnvOrDuration("LIVE_PROBE_INTERVAL", 60*time.Second), "The number of seconds between liveness probes while in a live state")

	fs.DurationVar(&cf.NotLiveProbeInterval, "not_live_probe_interval",
		lookupEnvOrDuration("NOT_LIVE_PROBE_INTERVAL", 5*time.Second), "The number of seconds between liveness probes while in a not live state")

	fs.StringVar(&cf.ProbeAddress, "probe_address",
		lookupEnvOrString("PROBE_ADDRESS", ":8080"), "The address on which to listen to answer liveness and readiness probe queries over HTTP")

	fs.BoolVar(&(cf.TraceEnabled), "trace_enabled",
		lookupEnvOrBool("TRACE_ENABLED", false), "Whether to send logs to tracing agent?")

	fs.StringVar(&cf.TraceAgentAddress, "trace_agent_address",
		lookupEnvOrString("TRACE_AGENT_ADDRESS", "127.0.0.1:6831"), "The address of tracing agent to which span info should be sent")

	fs.BoolVar(&cf.LogCorrelationEnabled, "log_correlation_enabled",
		lookupEnvOrBool("LOG_CORRELATION_ENABLED", true), "Whether to enrich log statements with fields denoting operation being executed for achieving correlation?")

	fs.StringVar(&cf.VolthaStackID, "stack_id",
		lookupEnvOrString("STACK_ID", "voltha"), "ID for the current voltha stack")

	fs.DurationVar(&cf.BackoffRetryInitialInterval, "backoff_retry_initial_interval",
		lookupEnvOrDuration("BACKOFF_RETRY_INITIAL_INTERVAL", 500*time.Millisecond),
		"The initial number of milliseconds an exponential backoff will wait before a retry")

	fs.DurationVar(&cf.BackoffRetryMaxElapsedTime, "backoff_retry_max_elapsed_time",
		lookupEnvOrDuration("BACKOFF_RETRY_MAX_ELAPSED_TIME", 500*time.Second),
		"The maximum number of milliseconds an exponential backoff can elasped")

	fs.DurationVar(&cf.BackoffRetryMaxInterval, "backoff_retry_max_interval",
		lookupEnvOrDuration("BACKOFF_RETRY_MAX_INTERVAL", 1*time.Minute),
		"The maximum number of milliseconds of an exponential backoff interval")

	_ = fs.Parse(args)
}
