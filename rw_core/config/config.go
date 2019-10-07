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
	"github.com/opencord/voltha-go/common/log"
)

// RW Core service default constants
const (
	ConsulStoreName                   = "consul"
	EtcdStoreName                     = "etcd"
	default_GrpcPort                  = 50057
	default_GrpcHost                  = ""
	default_KafkaAdapterHost          = "127.0.0.1"
	default_KafkaAdapterPort          = 9092
	default_KafkaClusterHost          = "127.0.0.1"
	default_KafkaClusterPort          = 9094
	default_KVStoreType               = EtcdStoreName
	default_KVStoreTimeout            = 5 //in seconds
	default_KVStoreHost               = "127.0.0.1"
	default_KVStorePort               = 2379 // Consul = 8500; Etcd = 2379
	default_KVTxnKeyDelTime           = 60
	default_KVStoreDataPrefix         = "service/voltha"
	default_LogLevel                  = 0
	default_Banner                    = false
	default_DisplayVersionOnly        = false
	default_CoreTopic                 = "rwcore"
	default_RWCoreEndpoint            = "rwcore"
	default_RWCoreKey                 = "pki/voltha.key"
	default_RWCoreCert                = "pki/voltha.crt"
	default_RWCoreCA                  = "pki/voltha-CA.pem"
	default_AffinityRouterTopic       = "affinityRouter"
	default_InCompetingMode           = true
	default_LongRunningRequestTimeout = int64(2000)
	default_DefaultRequestTimeout     = int64(500)
	default_CoreTimeout               = int64(500)
	default_CoreBindingKey            = "voltha_backend_name"
	default_CorePairTopic             = "rwcore_1"
	default_MaxConnectionRetries      = -1 // retries forever
	default_ConnectionRetryInterval   = 2  // in seconds
	default_ProbeHost                 = ""
	default_ProbePort                 = 8080
)

// RWCoreFlags represents the set of configurations used by the read-write core service
type RWCoreFlags struct {
	// Command line parameters
	RWCoreEndpoint            string
	GrpcHost                  string
	GrpcPort                  int
	KafkaAdapterHost          string
	KafkaAdapterPort          int
	KafkaClusterHost          string
	KafkaClusterPort          int
	KVStoreType               string
	KVStoreTimeout            int // in seconds
	KVStoreHost               string
	KVStorePort               int
	KVTxnKeyDelTime           int
	KVStoreDataPrefix         string
	CoreTopic                 string
	LogLevel                  int
	Banner                    bool
	DisplayVersionOnly        bool
	RWCoreKey                 string
	RWCoreCert                string
	RWCoreCA                  string
	AffinityRouterTopic       string
	InCompetingMode           bool
	LongRunningRequestTimeout int64
	DefaultRequestTimeout     int64
	DefaultCoreTimeout        int64
	CoreBindingKey            string
	CorePairTopic             string
	MaxConnectionRetries      int
	ConnectionRetryInterval   int
	ProbeHost                 string
	ProbePort                 int
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// NewRWCoreFlags returns a new RWCore config
func NewRWCoreFlags() *RWCoreFlags {
	var rwCoreFlag = RWCoreFlags{ // Default values
		RWCoreEndpoint:            default_RWCoreEndpoint,
		GrpcHost:                  default_GrpcHost,
		GrpcPort:                  default_GrpcPort,
		KafkaAdapterHost:          default_KafkaAdapterHost,
		KafkaAdapterPort:          default_KafkaAdapterPort,
		KafkaClusterHost:          default_KafkaClusterHost,
		KafkaClusterPort:          default_KafkaClusterPort,
		KVStoreType:               default_KVStoreType,
		KVStoreTimeout:            default_KVStoreTimeout,
		KVStoreHost:               default_KVStoreHost,
		KVStorePort:               default_KVStorePort,
		KVStoreDataPrefix:         default_KVStoreDataPrefix,
		KVTxnKeyDelTime:           default_KVTxnKeyDelTime,
		CoreTopic:                 default_CoreTopic,
		LogLevel:                  default_LogLevel,
		Banner:                    default_Banner,
		DisplayVersionOnly:        default_DisplayVersionOnly,
		RWCoreKey:                 default_RWCoreKey,
		RWCoreCert:                default_RWCoreCert,
		RWCoreCA:                  default_RWCoreCA,
		AffinityRouterTopic:       default_AffinityRouterTopic,
		InCompetingMode:           default_InCompetingMode,
		DefaultRequestTimeout:     default_DefaultRequestTimeout,
		LongRunningRequestTimeout: default_LongRunningRequestTimeout,
		DefaultCoreTimeout:        default_CoreTimeout,
		CoreBindingKey:            default_CoreBindingKey,
		CorePairTopic:             default_CorePairTopic,
		MaxConnectionRetries:      default_MaxConnectionRetries,
		ConnectionRetryInterval:   default_ConnectionRetryInterval,
		ProbeHost:                 default_ProbeHost,
		ProbePort:                 default_ProbePort,
	}
	return &rwCoreFlag
}

// ParseCommandArguments parses the arguments when running read-write core service
func (cf *RWCoreFlags) ParseCommandArguments() {

	var help string

	help = fmt.Sprintf("RW core endpoint address")
	flag.StringVar(&(cf.RWCoreEndpoint), "vcore-endpoint", default_RWCoreEndpoint, help)

	help = fmt.Sprintf("GRPC server - host")
	flag.StringVar(&(cf.GrpcHost), "grpc_host", default_GrpcHost, help)

	help = fmt.Sprintf("GRPC server - port")
	flag.IntVar(&(cf.GrpcPort), "grpc_port", default_GrpcPort, help)

	help = fmt.Sprintf("Kafka - Adapter messaging host")
	flag.StringVar(&(cf.KafkaAdapterHost), "kafka_adapter_host", default_KafkaAdapterHost, help)

	help = fmt.Sprintf("Kafka - Adapter messaging port")
	flag.IntVar(&(cf.KafkaAdapterPort), "kafka_adapter_port", default_KafkaAdapterPort, help)

	help = fmt.Sprintf("Kafka - Cluster messaging host")
	flag.StringVar(&(cf.KafkaClusterHost), "kafka_cluster_host", default_KafkaClusterHost, help)

	help = fmt.Sprintf("Kafka - Cluster messaging port")
	flag.IntVar(&(cf.KafkaClusterPort), "kafka_cluster_port", default_KafkaClusterPort, help)

	help = fmt.Sprintf("RW Core topic")
	flag.StringVar(&(cf.CoreTopic), "rw_core_topic", default_CoreTopic, help)

	help = fmt.Sprintf("Affinity Router topic")
	flag.StringVar(&(cf.AffinityRouterTopic), "affinity_router_topic", default_AffinityRouterTopic, help)

	help = fmt.Sprintf("In competing Mode - two cores competing to handle a transaction ")
	flag.BoolVar(&cf.InCompetingMode, "in_competing_mode", default_InCompetingMode, help)

	help = fmt.Sprintf("KV store type")
	flag.StringVar(&(cf.KVStoreType), "kv_store_type", default_KVStoreType, help)

	help = fmt.Sprintf("The default timeout when making a kv store request")
	flag.IntVar(&(cf.KVStoreTimeout), "kv_store_request_timeout", default_KVStoreTimeout, help)

	help = fmt.Sprintf("KV store host")
	flag.StringVar(&(cf.KVStoreHost), "kv_store_host", default_KVStoreHost, help)

	help = fmt.Sprintf("KV store port")
	flag.IntVar(&(cf.KVStorePort), "kv_store_port", default_KVStorePort, help)

	help = fmt.Sprintf("The time to wait before deleting a completed transaction key")
	flag.IntVar(&(cf.KVTxnKeyDelTime), "kv_txn_delete_time", default_KVTxnKeyDelTime, help)

	help = fmt.Sprintf("KV store data prefix")
	flag.StringVar(&(cf.KVStoreDataPrefix), "kv_store_data_prefix", default_KVStoreDataPrefix, help)

	help = fmt.Sprintf("Log level")
	flag.IntVar(&(cf.LogLevel), "log_level", default_LogLevel, help)

	help = fmt.Sprintf("Timeout for long running request")
	flag.Int64Var(&(cf.LongRunningRequestTimeout), "timeout_long_request", default_LongRunningRequestTimeout, help)

	help = fmt.Sprintf("Default timeout for regular request")
	flag.Int64Var(&(cf.DefaultRequestTimeout), "timeout_request", default_DefaultRequestTimeout, help)

	help = fmt.Sprintf("Default Core timeout")
	flag.Int64Var(&(cf.DefaultCoreTimeout), "core_timeout", default_CoreTimeout, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&cf.Banner, "banner", default_Banner, help)

	help = fmt.Sprintf("Show version information and exit")
	flag.BoolVar(&cf.DisplayVersionOnly, "version", default_DisplayVersionOnly, help)

	help = fmt.Sprintf("The name of the meta-key whose value is the rw-core group to which the ofagent is bound")
	flag.StringVar(&(cf.CoreBindingKey), "core_binding_key", default_CoreBindingKey, help)

	help = fmt.Sprintf("Core pairing group topic")
	flag.StringVar(&cf.CorePairTopic, "core_pair_topic", default_CorePairTopic, help)

	help = fmt.Sprintf("The number of retries to connect to a dependent component")
	flag.IntVar(&(cf.MaxConnectionRetries), "max_connection_retries", default_MaxConnectionRetries, help)

	help = fmt.Sprintf("The number of seconds between each connection retry attempt ")
	flag.IntVar(&(cf.ConnectionRetryInterval), "connection_retry_interval", default_ConnectionRetryInterval, help)

	help = fmt.Sprintf("The host on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.StringVar(&(cf.ProbeHost), "probe_host", default_ProbeHost, help)

	help = fmt.Sprintf("The port on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.IntVar(&(cf.ProbePort), "probe_port", default_ProbePort, help)

	flag.Parse()
}
