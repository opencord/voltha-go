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
	"os"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

// RO Core service default constants
const (
	ConsulStoreName                = "consul"
	EtcdStoreName                  = "etcd"
	defaultInstanceID              = "rocore001"
	defaultGrpcPort                = 50057
	defaultGrpcHost                = ""
	defaultKVStoreType             = EtcdStoreName
	defaultKVStoreTimeout          = 5 //in seconds
	defaultKVStoreHost             = "127.0.0.1"
	defaultKVStorePort             = 2379 // Consul = 8500; Etcd = 2379
	defaultKVTxnKeyDelTime         = 60
	defaultLogLevel                = 0
	defaultBanner                  = false
	defaultDisplayVersionOnly      = false
	defaultCoreTopic               = "rocore"
	defaultROCoreEndpoint          = "rocore"
	defaultROCoreKey               = "pki/voltha.key"
	defaultROCoreCert              = "pki/voltha.crt"
	defaultROCoreCA                = "pki/voltha-CA.pem"
	defaultAffinityRouterTopic     = "affinityRouter"
	defaultProbeHost               = ""
	defaultProbePort               = 8080
	defaultLiveProbeInterval       = 60 * time.Second
	defaultNotLiveProbeInterval    = 5 * time.Second // Probe more frequently to detect Recovery early
	defaultCoreTimeout             = 59 * time.Second
	defaultMaxConnectionRetries    = -1              // retries forever
	defaultConnectionRetryInterval = 2 * time.Second // in seconds
)

// ROCoreFlags represents the set of configurations used by the read-only core service
type ROCoreFlags struct {
	// Command line parameters
	InstanceID              string
	ROCoreEndpoint          string
	GrpcHost                string
	GrpcPort                int
	KVStoreType             string
	KVStoreTimeout          int // in seconds
	KVStoreHost             string
	KVStorePort             int
	KVTxnKeyDelTime         int
	CoreTopic               string
	LogLevel                int
	Banner                  bool
	DisplayVersionOnly      bool
	ROCoreKey               string
	ROCoreCert              string
	ROCoreCA                string
	AffinityRouterTopic     string
	ProbeHost               string
	ProbePort               int
	LiveProbeInterval       time.Duration
	NotLiveProbeInterval    time.Duration
	CoreTimeout             time.Duration
	MaxConnectionRetries    int
	ConnectionRetryInterval time.Duration
}

func init() {
	_, err := log.AddPackage(log.JSON, log.WarnLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

// NewROCoreFlags returns a new ROCore config
func NewROCoreFlags() *ROCoreFlags {
	var roCoreFlag = ROCoreFlags{ // Default values
		InstanceID:              defaultInstanceID,
		ROCoreEndpoint:          defaultROCoreEndpoint,
		GrpcHost:                defaultGrpcHost,
		GrpcPort:                defaultGrpcPort,
		KVStoreType:             defaultKVStoreType,
		KVStoreTimeout:          defaultKVStoreTimeout,
		KVStoreHost:             defaultKVStoreHost,
		KVStorePort:             defaultKVStorePort,
		KVTxnKeyDelTime:         defaultKVTxnKeyDelTime,
		CoreTopic:               defaultCoreTopic,
		LogLevel:                defaultLogLevel,
		Banner:                  defaultBanner,
		DisplayVersionOnly:      defaultDisplayVersionOnly,
		ROCoreKey:               defaultROCoreKey,
		ROCoreCert:              defaultROCoreCert,
		ROCoreCA:                defaultROCoreCA,
		AffinityRouterTopic:     defaultAffinityRouterTopic,
		ProbeHost:               defaultProbeHost,
		ProbePort:               defaultProbePort,
		LiveProbeInterval:       defaultLiveProbeInterval,
		NotLiveProbeInterval:    defaultNotLiveProbeInterval,
		CoreTimeout:             defaultCoreTimeout,
		MaxConnectionRetries:    defaultMaxConnectionRetries,
		ConnectionRetryInterval: defaultConnectionRetryInterval,
	}
	return &roCoreFlag
}

// ParseCommandArguments parses the arguments when running read-only core service
func (cf *ROCoreFlags) ParseCommandArguments() {

	help := fmt.Sprintf("RO core endpoint address")
	flag.StringVar(&(cf.ROCoreEndpoint), "vcore-endpoint", defaultROCoreEndpoint, help)

	help = fmt.Sprintf("GRPC server - host")
	flag.StringVar(&(cf.GrpcHost), "grpc_host", defaultGrpcHost, help)

	help = fmt.Sprintf("GRPC server - port")
	flag.IntVar(&(cf.GrpcPort), "grpc_port", defaultGrpcPort, help)

	help = fmt.Sprintf("RO Core topic")
	flag.StringVar(&(cf.CoreTopic), "ro_core_topic", defaultCoreTopic, help)

	help = fmt.Sprintf("Affinity Router topic")
	flag.StringVar(&(cf.AffinityRouterTopic), "affinity_router_topic", defaultAffinityRouterTopic, help)

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

	help = fmt.Sprintf("Log level")
	flag.IntVar(&(cf.LogLevel), "log_level", defaultLogLevel, help)

	help = fmt.Sprintf("Show startup banner log lines")
	flag.BoolVar(&cf.Banner, "banner", defaultBanner, help)

	help = fmt.Sprintf("Show version information and exit")
	flag.BoolVar(&cf.DisplayVersionOnly, "version", defaultDisplayVersionOnly, help)

	help = fmt.Sprintf("The address on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.StringVar(&(cf.ProbeHost), "probe_host", defaultProbeHost, help)

	help = fmt.Sprintf("The port on which to listen to answer liveness and readiness probe queries over HTTP.")
	flag.IntVar(&(cf.ProbePort), "probe_port", defaultProbePort, help)

	help = fmt.Sprintf("Time interval between liveness probes while in a live state")
	flag.DurationVar(&(cf.LiveProbeInterval), "live_probe_interval", defaultLiveProbeInterval, help)

	help = fmt.Sprintf("Time interval between liveness probes while in a not live state")
	flag.DurationVar(&(cf.NotLiveProbeInterval), "not_live_probe_interval", defaultNotLiveProbeInterval, help)

	help = fmt.Sprintf("The maximum time the core will wait while attempting to connect to a dependent component duration")
	flag.DurationVar(&(cf.CoreTimeout), "core_timeout", defaultCoreTimeout, help)

	help = fmt.Sprintf("The number of retries to connect to a dependent component")
	flag.IntVar(&(cf.MaxConnectionRetries), "max_connection_retries", defaultMaxConnectionRetries, help)

	help = fmt.Sprintf("The duration between each connection retry attempt ")
	flag.DurationVar(&(cf.ConnectionRetryInterval), "connection_retry_interval", defaultConnectionRetryInterval, help)

	flag.Parse()

	containerName := getContainerInfo()
	if len(containerName) > 0 {
		cf.InstanceID = containerName
	}

}

func getContainerInfo() string {
	return os.Getenv("HOSTNAME")
}
