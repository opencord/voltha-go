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
	"fmt"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/db"
)

// tech profile default constants
const (
	defaultTechProfileName        = "Default_1tcont_1gem_Profile"
	DEFAULT_TECH_PROFILE_TABLE_ID = 64
	defaultVersion                = 1.0
	defaultLogLevel               = 0
	defaultGemportsCount          = 1
	defaultPbits                  = "0b11111111"

	defaultKVStoreTimeout = 5 * time.Second //in seconds

	// Tech profile path prefix in kv store (for the TP template)
	// NOTE that this hardcoded to service/voltha as the TP template is shared across stacks
	defaultTpKvPathPrefix = "service/voltha/technology_profiles"

	// Tech profile path prefix in kv store (for TP instances)
	defaultKVPathPrefix = "%s/technology_profiles"

	// Resource instance path prefix in KV store (for Resource Instances)
	defaultResourceInstancePathPrefix = "%s/resource_instances"

	// Tech profile path in kv store
	defaultTechProfileKVPath = "%s/%d" // <technology>/<tech_profile_tableID>

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
	// String Keys for EPON
	EPON_ATTRIBUTE              = "epon_attribute"
	PACKAGE_TYPE                = "package_type"
	TRAFFIC_TYPE                = "traffic type"
	UNSOLICITED_GRANT_SIZE      = "unsolicited_grant_size"
	NOMINAL_INTERVAL            = "nominal_interval"
	TOLERATED_POLL_JITTER       = "tolerated_poll_jitter"
	REQUEST_TRANSMISSION_POLICY = "request_transmission_policy"
	NUM_Q_SETS                  = "num_q_sets"
	Q_THRESHOLDS                = "q_thresholds"
	Q_THRESHOLD1                = "q_threshold1"
	Q_THRESHOLD2                = "q_threshold2"
	Q_THRESHOLD3                = "q_threshold3"
	Q_THRESHOLD4                = "q_threshold4"
	Q_THRESHOLD5                = "q_threshold5"
	Q_THRESHOLD6                = "q_threshold6"
	Q_THRESHOLD7                = "q_threshold7"
)

// TechprofileFlags represents the set of configurations used
type TechProfileFlags struct {
	KVStoreAddress               string
	KVStoreType                  string
	KVStoreTimeout               time.Duration
	KVBackend                    *db.Backend // this is the backend used to store TP instances
	DefaultTpKVBackend           *db.Backend // this is the backend used to read the TP profile
	ResourceInstanceKVBacked     *db.Backend // this is the backed used to read/write Resource Instances
	TPKVPathPrefix               string
	defaultTpKvPathPrefix        string
	TPFileKVPath                 string
	ResourceInstanceKVPathPrefix string
	DefaultTPName                string
	TPVersion                    uint32
	NumGemPorts                  uint32
	DefaultPbits                 []string
	LogLevel                     int
	DefaultTechProfileID         uint32
	DefaultNumGemPorts           uint32
}

func NewTechProfileFlags(KVStoreType string, KVStoreAddress string, basePathKvStore string) *TechProfileFlags {
	// initialize with default values
	var techProfileFlags = TechProfileFlags{
		KVBackend:                    nil,
		KVStoreAddress:               KVStoreAddress,
		KVStoreType:                  KVStoreType,
		KVStoreTimeout:               defaultKVStoreTimeout,
		DefaultTPName:                defaultTechProfileName,
		TPKVPathPrefix:               fmt.Sprintf(defaultKVPathPrefix, basePathKvStore),
		defaultTpKvPathPrefix:        defaultTpKvPathPrefix,
		TPVersion:                    defaultVersion,
		TPFileKVPath:                 defaultTechProfileKVPath,
		ResourceInstanceKVPathPrefix: fmt.Sprintf(defaultResourceInstancePathPrefix, basePathKvStore),
		DefaultTechProfileID:         DEFAULT_TECH_PROFILE_TABLE_ID,
		DefaultNumGemPorts:           defaultGemportsCount,
		DefaultPbits:                 []string{defaultPbits},
		LogLevel:                     defaultLogLevel,
	}

	return &techProfileFlags
}
