/*
 * Copyright 2019-present Open Networking Foundation

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
package techprofile

import (
	"github.com/opencord/voltha-lib-go/v2/pkg/db"
)

// tech profile default constants
const (
	defaultTechProfileName        = "Default_1tcont_1gem_Profile"
	DEFAULT_TECH_PROFILE_TABLE_ID = 64
	defaultVersion                = 1.0
	defaultLogLevel               = 0
	defaultGemportsCount          = 1
	defaultNumTconts              = 1
	defaultPbits                  = "0b11111111"

	defaultKVStoreType    = "etcd"
	defaultKVStoreTimeout = 5 //in seconds
	defaultKVStoreHost    = "127.0.0.1"
	defaultKVStorePort    = 2379 // Consul = 8500; Etcd = 2379

	// Tech profile path prefix in kv store
	defaultKVPathPrefix = "service/voltha/technology_profiles"

	// Tech profile path in kv store
	defaultTechProfileKVPath = "%s/%d" // <technology>/<tech_profile_tableID>

	// Tech profile instance path in kv store
	// Format: <technology>/<tech_profile_tableID>/<uni_port_name>
	defaultTPInstanceKVPath = "%s/%d/%s"
)

//Tech-Profile JSON String Keys
// NOTE: Tech profile templeate JSON file should comply with below keys
const (
	NAME                               = "name"
	PROFILE_TYPE                       = "profile_type"
	VERSION                            = "version"
	NUM_GEM_PORTS                      = "num_gem_ports"
	INSTANCE_CONTROL                   = "instance_control"
	US_SCHEDULER                       = "us_scheduler"
	DS_SCHEDULER                       = "ds_scheduler"
	UPSTREAM_GEM_PORT_ATTRIBUTE_LIST   = "upstream_gem_port_attribute_list"
	DOWNSTREAM_GEM_PORT_ATTRIBUTE_LIST = "downstream_gem_port_attribute_list"
	ONU                                = "onu"
	UNI                                = "uni"
	MAX_GEM_PAYLOAD_SIZE               = "max_gem_payload_size"
	DIRECTION                          = "direction"
	ADDITIONAL_BW                      = "additional_bw"
	PRIORITY                           = "priority"
	Q_SCHED_POLICY                     = "q_sched_policy"
	WEIGHT                             = "weight"
	PBIT_MAP                           = "pbit_map"
	DISCARD_CONFIG                     = "discard_config"
	MAX_THRESHOLD                      = "max_threshold"
	MIN_THRESHOLD                      = "min_threshold"
	MAX_PROBABILITY                    = "max_probability"
	DISCARD_POLICY                     = "discard_policy"
	PRIORITY_Q                         = "priority_q"
	SCHEDULING_POLICY                  = "scheduling_policy"
	MAX_Q_SIZE                         = "max_q_size"
	AES_ENCRYPTION                     = "aes_encryption"
)

// TechprofileFlags represents the set of configurations used
type TechProfileFlags struct {
	KVStoreHost          string
	KVStorePort          int
	KVStoreType          string
	KVStoreTimeout       int
	KVBackend            *db.Backend
	TPKVPathPrefix       string
	TPFileKVPath         string
	TPInstanceKVPath     string
	DefaultTPName        string
	TPVersion            int
	NumGemPorts          uint32
	DefaultPbits         []string
	LogLevel             int
	DefaultTechProfileID uint32
	DefaultNumGemPorts   uint32
}

func NewTechProfileFlags(KVStoreType string, KVStoreHost string, KVStorePort int) *TechProfileFlags {
	// initialize with default values
	var techProfileFlags = TechProfileFlags{
		KVBackend:            nil,
		KVStoreHost:          KVStoreHost,
		KVStorePort:          KVStorePort,
		KVStoreType:          KVStoreType,
		KVStoreTimeout:       defaultKVStoreTimeout,
		DefaultTPName:        defaultTechProfileName,
		TPKVPathPrefix:       defaultKVPathPrefix,
		TPVersion:            defaultVersion,
		TPFileKVPath:         defaultTechProfileKVPath,
		TPInstanceKVPath:     defaultTPInstanceKVPath,
		DefaultTechProfileID: DEFAULT_TECH_PROFILE_TABLE_ID,
		DefaultNumGemPorts:   defaultGemportsCount,
		DefaultPbits:         []string{defaultPbits},
		LogLevel:             defaultLogLevel,
	}

	return &techProfileFlags
}
