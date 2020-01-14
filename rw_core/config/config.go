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

	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

// RW Core service default constants
const (
	ConsulStoreName                  = "consul"
	EtcdStoreName                    = "etcd"
	defaultGrpcPort                  = 50057
	defaultGrpcHost                  = ""
	defaultKafkaAdapterHost          = "127.0.0.1"
	defaultKafkaAdapterPort          = 9092
	defaultKafkaClusterHost          = "127.0.0.1"
	defaultKafkaClusterPort          = 9094
	defaultKVStoreType               = EtcdStoreName
	defaultKVStoreTimeout            = 5 //in seconds
	defaultKVStoreHost               = "127.0.0.1"
	defaultKVStorePort               = 2379 // Consul = 8500; Etcd = 2379
	defaultKVTxnKeyDelTime           = 60
	defaultKVStoreDataPrefix         = "service/voltha"
	defaultLogLevel                  = "DEBUG"
	defaultBanner                    = false
	defaultDisplayVersionOnly        = false
	defaultCoreTopic                 = "rwcore"
	defaultRWCoreEndpoint            = "rwcore"
	defaultRWCoreKey                 = "pki/voltha.key"
	defaultRWCoreCert                = "pki/voltha.crt"
	defaultRWCoreCA                  = "pki/voltha-CA.pem"
	defaultAffinityRouterTopic       = "affinityRouter"
	defaultInCompetingMode           = true
	defaultLongRunningRequestTimeout = int64(2000)
	defaultDefaultRequestTimeout     = int64(500)
	defaultCoreTimeout               = int64(500)
	defaultCoreBindingKey            = "voltha_backend_name"
	defaultCorePairTopic             = "rwcore_1"
	defaultMaxConnectionRetries      = -1 // retries forever
	defaultConnectionRetryInterval   = 2 * time.Second
	defaultLiveProbeInterval         = 60 * time.Second
	defaultNotLiveProbeInterval      = 5 * time.Second // Probe more frequently when not alive
	defaultProbeHost                 = ""
	defaultProbePort                 = 8080
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
	LogLevel                  string
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
	ConnectionRetryInterval   time.Duration
	LiveProbeInterval         time.Duration
	NotLiveProbeInterval      time.Duration
	ProbeHost                 string
	ProbePort                 int
}

func init() {
	_, err := log.AddPackage(log.JSON, log.WarnLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-a-package-to-the-log-map", log.Fields{"error": err})
	}
}

// NewRWCoreFlags returns a new RWCore config
func NewRWCoreFlags() *RWCoreFlags {
	var rwCoreFlag = RWCoreFlags{ // Default values
		RWCoreEndpoint:            defaultRWCoreEndpoint,
		GrpcHost:                  defaultGrpcHost,
		GrpcPort:                  defaultGrpcPort,
		KafkaAdapterHost:          defaultKafkaAdapterHost,
		KafkaAdapterPort:          defaultKafkaAdapterPort,
		KafkaClusterHost:          defaultKafkaClusterHost,
		KafkaClusterPort:          defaultKafkaClusterPort,
		KVStoreType:               defaultKVStoreType,
		KVStoreTimeout:            defaultKVStoreTimeout,
		KVStoreHost:               defaultKVStoreHost,
		KVStorePort:               defaultKVStorePort,
		KVStoreDataPrefix:         defaultKVStoreDataPrefix,
		KVTxnKeyDelTime:           defaultKVTxnKeyDelTime,
		CoreTopic:                 defaultCoreTopic,
		LogLevel:                  defaultLogLevel,
		Banner:                    defaultBanner,
		DisplayVersionOnly:        defaultDisplayVersionOnly,
		RWCoreKey:                 defaultRWCoreKey,
		RWCoreCert:                defaultRWCoreCert,
		RWCoreCA:                  defaultRWCoreCA,
		AffinityRouterTopic:       defaultAffinityRouterTopic,
		InCompetingMode:           defaultInCompetingMode,
		DefaultRequestTimeout:     defaultDefaultRequestTimeout,
		LongRunningRequestTimeout: defaultLongRunningRequestTimeout,
		DefaultCoreTimeout:        defaultCoreTimeout,
		CoreBindingKey:            defaultCoreBindingKey,
		CorePairTopic:             defaultCorePairTopic,
		MaxConnectionRetries:      defaultMaxConnectionRetries,
		ConnectionRetryInterval:   defaultConnectionRetryInterval,
		LiveProbeInterval:         defaultLiveProbeInterval,
		NotLiveProbeInterval:      defaultNotLiveProbeInterval,
		ProbeHost:                 defaultProbeHost,
		ProbePort:                 defaultProbePort,
	}
	return &rwCoreFlag
}

// ParseCommandArguments parses the arguments when running read-write core service
func (cf *RWCoreFlags) ParseCommandArguments() {

	help := fmt.Sprintf("RW core endpoint address")
	flag.StringVar(&(cf.RWCoreEndpoint), "vcore-endpoint", defaultRWCoreEndpoint, help)

	help = fmt.Sprintf("GRPC server - host")
	flag.StringVar(&(cf.GrpcHost), "grpc_host", defaultGrpcHost, help)

	help = fmt.Sprintf("GRPC server - port")
	flag.IntVar(&(cf.GrpcPort), "grpc_port", defaultGrpcPort, help)

	help = fmt.Sprintf("Kafka - Adapter messaging host")
	flag.StringVar(&(cf.KafkaAdapterHost), "kafka_adapter_host", defaultKafkaAdapterHost, help)

	help = fmt.Sprintf("Kafka - Adapter messaging port")
	flag.IntVar(&(cf.KafkaAdapterPort), "kafka_adapter_port", defaultKafkaAdapterPort, help)

	help = fmt.Sprintf("Kafka - Cluster messaging host")
	flag.StringVar(&(cf.KafkaClusterHost), "kafka_cluster_host", defaultKafkaClusterHost, help)

	help = fmt.Sprintf("Kafka - Cluster messaging port")
	flag.IntVar(&(cf.KafkaClusterPort), "kafka_cluster_port", defaultKafkaClusterPort, help)

	help = fmt.Sprintf("RW Core topic")
	flag.StringVar(&(cf.CoreTopic), "rw_core_topic", defaultCoreTopic, help)

	help = fmt.Sprintf("Affinity Router topic")
	flag.StringVar(&(cf.AffinityRouterTopic), "affinity_router_topic", defaultAffinityRouterTopic, help)

	help = fmt.Sprintf("In competing Mode - two cores competing to handle a transaction ")
	flag.BoolVar(&cf.InCompetingMode, "in_competing_mode", defaultInCompetingMode, help)

	help = fmt.Sprintf("KV store type")
	flag.StringVar(&(cf.KVStoreType), "kv_store_type", defaultKVStoreType, help)

	help = fmt.Sprintf("The default timeout when making a kv store request")
	flag.IntVar(&(cf.KVStoreTimeout), "kv_store_request_timeout", defaultKVStoreTimeout, help)

	help = fmt.Sprintf("KV store host")
	flag.StringVar(&(cf.KVStoreHost), "kv_store_host", defaultKVStoreHost, help)

	help = fmt.Sprintf("KV store port")
	flag.IntVar(&(cf.KVStorePort), "kv_store_port", defaultKVStorePort, help)

	help = fmt.Sprintf("The time to wait before deleting a completed transaction key")
	flag.IntVar(&(cf.KVTxnKeyDelTime), "kv_txn_delete_time", defaultKVTxnKeyDelTime, help)

	help = fmt.Sprintf("KV store data prefix")
	flag.StringVar(&(cf.KVStoreDataPrefix), "kv_store_data_prefix", defaultKVStoreDataPrefix, help)

	help = fmt.Sprintf("Log level")
	flag.StringVar(&(cf.LogLevel), "log_level", defaultLogLevel, help)

	help = fmt.Sprintf("Timeout for long running request")
	flag.Int64Var(&(cf.LongRunningRequestTimeout), "timeout_long_request", defaultLongRunningRequestTimeout, help)

	help = fmt.Sprintf("Default timeout for regular request")
	flag.Int64Var(&(cf.DefaultRequestTimeout), "timeout_request", defaultDefaultRequestTimeout, help)

	help = fmt.Sprintf("Default Core timeout")
	flag.Int64Var(&(cf.DefaultCoreTimeout), "core_timeout", defaultCoreTimeout, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&cf.Banner, "banner", defaultBanner, help)

	help = fmt.Sprintf("Show version information and exit")
	flag.BoolVar(&cf.DisplayVersionOnly, "version", defaultDisplayVersionOnly, help)

	help = fmt.Sprintf("The name of the meta-key whose value is the rw-core group to which the ofagent is bound")
	flag.StringVar(&(cf.CoreBindingKey), "core_binding_key", defaultCoreBindingKey, help)

	help = fmt.Sprintf("Core pairing group topic")
	flag.StringVar(&cf.CorePairTopic, "core_pair_topic", defaultCorePairTopic, help)

	help = fmt.Sprintf("The number of retries to connect to a dependent component")
	flag.IntVar(&(cf.MaxConnectionRetries), "max_connection_retries", defaultMaxConnectionRetries, help)

	help = fmt.Sprintf("The number of seconds between each connection retry attempt")
	flag.DurationVar(&(cf.ConnectionRetryInterval), "connection_retry_interval", defaultConnectionRetryInterval, help)

	help = fmt.Sprintf("The number of seconds between liveness probes while in a live state")
	flag.DurationVar(&(cf.LiveProbeInterval), "live_probe_interval", defaultLiveProbeInterval, help)

	help = fmt.Sprintf("The number of seconds between liveness probes while in a not live state")
	flag.DurationVar(&(cf.NotLiveProbeInterval), "not_live_probe_interval", defaultNotLiveProbeInterval, help)

	help = fmt.Sprintf("The host on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.StringVar(&(cf.ProbeHost), "probe_host", defaultProbeHost, help)

	help = fmt.Sprintf("The port on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.IntVar(&(cf.ProbePort), "probe_port", defaultProbePort, help)

	flag.Parse()
}
