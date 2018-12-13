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

// Simulated OLT default constants
const (
	EtcdStoreName            = "etcd"
	default_InstanceID       = "simulatedOnu001"
	default_KafkaAdapterHost = "127.0.0.1"
	default_KafkaAdapterPort = 9092
	default_KafkaClusterHost = "127.0.0.1"
	default_KafkaClusterPort = 9094
	default_KVStoreType      = EtcdStoreName
	default_KVStoreTimeout   = 5 //in seconds
	default_KVStoreHost      = "127.0.0.1"
	default_KVStorePort      = 2379 // Consul = 8500; Etcd = 2379
	default_LogLevel         = 0
	default_Banner           = false
	default_Topic            = "simulated_onu"
	default_CoreTopic        = "rwcore"
)

// AdapterFlags represents the set of configurations used by the read-write adaptercore service
type AdapterFlags struct {
	// Command line parameters
	InstanceID       string
	KafkaAdapterHost string
	KafkaAdapterPort int
	KafkaClusterHost string
	KafkaClusterPort int
	KVStoreType      string
	KVStoreTimeout   int // in seconds
	KVStoreHost      string
	KVStorePort      int
	Topic            string
	CoreTopic        string
	LogLevel         int
	Banner           bool
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// NewRWCoreFlags returns a new RWCore config
func NewAdapterFlags() *AdapterFlags {
	var adapterFlags = AdapterFlags{ // Default values
		InstanceID:       default_InstanceID,
		KafkaAdapterHost: default_KafkaAdapterHost,
		KafkaAdapterPort: default_KafkaAdapterPort,
		KafkaClusterHost: default_KafkaClusterHost,
		KafkaClusterPort: default_KafkaClusterPort,
		KVStoreType:      default_KVStoreType,
		KVStoreTimeout:   default_KVStoreTimeout,
		KVStoreHost:      default_KVStoreHost,
		KVStorePort:      default_KVStorePort,
		Topic:            default_Topic,
		CoreTopic:        default_CoreTopic,
		LogLevel:         default_LogLevel,
		Banner:           default_Banner,
	}
	return &adapterFlags
}

// ParseCommandArguments parses the arguments when running read-write adaptercore service
func (so *AdapterFlags) ParseCommandArguments() {

	var help string

	help = fmt.Sprintf("Kafka - Adapter messaging host")
	flag.StringVar(&(so.KafkaAdapterHost), "kafka_adapter_host", default_KafkaAdapterHost, help)

	help = fmt.Sprintf("Kafka - Adapter messaging port")
	flag.IntVar(&(so.KafkaAdapterPort), "kafka_adapter_port", default_KafkaAdapterPort, help)

	help = fmt.Sprintf("Kafka - Cluster messaging host")
	flag.StringVar(&(so.KafkaClusterHost), "kafka_cluster_host", default_KafkaClusterHost, help)

	help = fmt.Sprintf("Kafka - Cluster messaging port")
	flag.IntVar(&(so.KafkaClusterPort), "kafka_cluster_port", default_KafkaClusterPort, help)

	help = fmt.Sprintf("Simulated ONU topic")
	flag.StringVar(&(so.Topic), "simulator_topic", default_Topic, help)

	help = fmt.Sprintf("Core topic")
	flag.StringVar(&(so.CoreTopic), "core_topic", default_CoreTopic, help)

	help = fmt.Sprintf("KV store type")
	flag.StringVar(&(so.KVStoreType), "kv_store_type", default_KVStoreType, help)

	help = fmt.Sprintf("The default timeout when making a kv store request")
	flag.IntVar(&(so.KVStoreTimeout), "kv_store_request_timeout", default_KVStoreTimeout, help)

	help = fmt.Sprintf("KV store host")
	flag.StringVar(&(so.KVStoreHost), "kv_store_host", default_KVStoreHost, help)

	help = fmt.Sprintf("KV store port")
	flag.IntVar(&(so.KVStorePort), "kv_store_port", default_KVStorePort, help)

	help = fmt.Sprintf("Log level")
	flag.IntVar(&(so.LogLevel), "log_level", default_LogLevel, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&so.Banner, "banner", default_Banner, help)

	flag.Parse()

	containerName := getContainerInfo()
	if len(containerName) > 0 {
		so.InstanceID = containerName
	}

}

func getContainerInfo() string {
	return os.Getenv("HOSTNAME")
}
