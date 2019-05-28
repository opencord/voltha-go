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

// RO Core service default constants
const (
	ConsulStoreName               = "consul"
	EtcdStoreName                 = "etcd"
	default_InstanceID            = "rocore001"
	default_GrpcPort              = 50057
	default_GrpcHost              = ""
	default_KVStoreType           = EtcdStoreName
	default_KVStoreTimeout        = 5 //in seconds
	default_KVStoreHost           = "127.0.0.1"
	default_KVStorePort           = 2379 // Consul = 8500; Etcd = 2379
	default_KVTxnKeyDelTime       = 60
	default_LogLevel              = 0
	default_Banner                = false
	default_DisplayVersionOnly    = false
	default_CoreTopic             = "rocore"
	default_ROCoreEndpoint        = "rocore"
	default_ROCoreKey             = "pki/voltha.key"
	default_ROCoreCert            = "pki/voltha.crt"
	default_ROCoreCA              = "pki/voltha-CA.pem"
	default_Affinity_Router_Topic = "affinityRouter"
)

// ROCoreFlags represents the set of configurations used by the read-only core service
type ROCoreFlags struct {
	// Command line parameters
	InstanceID          string
	ROCoreEndpoint      string
	GrpcHost            string
	GrpcPort            int
	KVStoreType         string
	KVStoreTimeout      int // in seconds
	KVStoreHost         string
	KVStorePort         int
	KVTxnKeyDelTime     int
	CoreTopic           string
	LogLevel            int
	Banner              bool
	DisplayVersionOnly  bool
	ROCoreKey           string
	ROCoreCert          string
	ROCoreCA            string
	AffinityRouterTopic string
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// NewROCoreFlags returns a new ROCore config
func NewROCoreFlags() *ROCoreFlags {
	var roCoreFlag = ROCoreFlags{ // Default values
		InstanceID:          default_InstanceID,
		ROCoreEndpoint:      default_ROCoreEndpoint,
		GrpcHost:            default_GrpcHost,
		GrpcPort:            default_GrpcPort,
		KVStoreType:         default_KVStoreType,
		KVStoreTimeout:      default_KVStoreTimeout,
		KVStoreHost:         default_KVStoreHost,
		KVStorePort:         default_KVStorePort,
		KVTxnKeyDelTime:     default_KVTxnKeyDelTime,
		CoreTopic:           default_CoreTopic,
		LogLevel:            default_LogLevel,
		Banner:              default_Banner,
		DisplayVersionOnly:  default_DisplayVersionOnly,
		ROCoreKey:           default_ROCoreKey,
		ROCoreCert:          default_ROCoreCert,
		ROCoreCA:            default_ROCoreCA,
		AffinityRouterTopic: default_Affinity_Router_Topic,
	}
	return &roCoreFlag
}

// ParseCommandArguments parses the arguments when running read-only core service
func (cf *ROCoreFlags) ParseCommandArguments() {

	var help string

	help = fmt.Sprintf("RO core endpoint address")
	flag.StringVar(&(cf.ROCoreEndpoint), "vcore-endpoint", default_ROCoreEndpoint, help)

	help = fmt.Sprintf("GRPC server - host")
	flag.StringVar(&(cf.GrpcHost), "grpc_host", default_GrpcHost, help)

	help = fmt.Sprintf("GRPC server - port")
	flag.IntVar(&(cf.GrpcPort), "grpc_port", default_GrpcPort, help)

	help = fmt.Sprintf("RO Core topic")
	flag.StringVar(&(cf.CoreTopic), "ro_core_topic", default_CoreTopic, help)

	help = fmt.Sprintf("Affinity Router topic")
	flag.StringVar(&(cf.AffinityRouterTopic), "affinity_router_topic", default_Affinity_Router_Topic, help)

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

	help = fmt.Sprintf("Log level")
	flag.IntVar(&(cf.LogLevel), "log_level", default_LogLevel, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&cf.Banner, "banner", default_Banner, help)

	help = fmt.Sprintf("Show version information and exit")
	flag.BoolVar(&cf.DisplayVersionOnly, "version", default_DisplayVersionOnly, help)

	flag.Parse()

	containerName := getContainerInfo()
	if len(containerName) > 0 {
		cf.InstanceID = containerName
	}

}

func getContainerInfo() string {
	return os.Getenv("HOSTNAME")
}
