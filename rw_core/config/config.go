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
	"os"
)

// RW Core service default constants
const (
	ConsulStoreName          = "consul"
	EtcdStoreName            = "etcd"
	default_InstanceID       = "rwcore001"
	default_GrpcPort         = 50057
	default_GrpcHost         = "127.0.0.1"
	default_KafkaAdapterHost = "10.176.230.190"
	default_KafkaAdapterPort = 9092
	default_KafkaClusterHost = "10.176.215.107"
	default_KafkaClusterPort = 9094
	default_KVStoreType      = ConsulStoreName
	default_KVStoreTimeout   = 5 //in seconds
	default_KVStoreHost      = "10.176.230.190"
	default_KVStorePort      = 8500 // Etcd = 2379
	default_LogLevel         = 0
	default_Banner           = false
	default_CoreTopic        = "rwcore"
	default_RWCoreEndpoint   = "rwcore"
	default_RWCoreKey        = "pki/voltha.key"
	default_RWCoreCert       = "pki/voltha.crt"
	default_RWCoreCA         = "pki/voltha-CA.pem"
)

// RWCoreFlags represents the set of configurations used by the read-write core service
type RWCoreFlags struct {
	// Command line parameters
	InstanceID       string
	RWCoreEndpoint   string
	GrpcHost         string
	GrpcPort         int
	KafkaAdapterHost string
	KafkaAdapterPort int
	KafkaClusterHost string
	KafkaClusterPort int
	KVStoreType      string
	KVStoreTimeout   int // in seconds
	KVStoreHost      string
	KVStorePort      int
	CoreTopic        string
	LogLevel         int
	Banner           bool
	RWCoreKey        string
	RWCoreCert       string
	RWCoreCA         string
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// NewRWCoreFlags returns a new RWCore config
func NewRWCoreFlags() *RWCoreFlags {
	var rwCoreFlag = RWCoreFlags{ // Default values
		InstanceID:       default_InstanceID,
		RWCoreEndpoint:   default_RWCoreEndpoint,
		GrpcHost:         default_GrpcHost,
		GrpcPort:         default_GrpcPort,
		KafkaAdapterHost: default_KafkaAdapterHost,
		KafkaAdapterPort: default_KafkaAdapterPort,
		KafkaClusterHost: default_KafkaClusterHost,
		KafkaClusterPort: default_KafkaClusterPort,
		KVStoreType:      default_KVStoreType,
		KVStoreTimeout:   default_KVStoreTimeout,
		KVStoreHost:      default_KVStoreHost,
		KVStorePort:      default_KVStorePort,
		CoreTopic:        default_CoreTopic,
		LogLevel:         default_LogLevel,
		Banner:           default_Banner,
		RWCoreKey:        default_RWCoreKey,
		RWCoreCert:       default_RWCoreCert,
		RWCoreCA:         default_RWCoreCA,
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

	help = fmt.Sprintf("KV store type")
	flag.StringVar(&(cf.KVStoreType), "kv_store_type", default_KVStoreType, help)

	help = fmt.Sprintf("The default timeout when making a kv store request")
	flag.IntVar(&(cf.KVStoreTimeout), "kv_store_request_timeout", default_KVStoreTimeout, help)

	help = fmt.Sprintf("KV store host")
	flag.StringVar(&(cf.KVStoreHost), "kv_store_host", default_KVStoreHost, help)

	help = fmt.Sprintf("KV store port")
	flag.IntVar(&(cf.KVStorePort), "kv_store_port", default_KVStorePort, help)

	help = fmt.Sprintf("Log level")
	flag.IntVar(&(cf.LogLevel), "log_level", default_LogLevel, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&cf.Banner, "banner", default_Banner, help)

	flag.Parse()

	containerName := getContainerInfo()
	if len(containerName) > 0 {
		cf.InstanceID = containerName
	}

}

func getContainerInfo() string {
	return os.Getenv("HOSTNAME")
}
