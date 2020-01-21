/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package config

import (
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

const (
	/*
	 * This sets the LogLevel of the Voltha logger. It's pinned to FatalLevel here, as we
	 * generally don't want to see logger output, even when running go test in verbose
	 * mode. Even "Error" level messages are expected to be output by some unit tests.
	 *
	 * If you are developing a unit test, and experiencing problems or wish additional
	 * debugging from Voltha, then changing this constant to log.DebugLevel may be
	 * useful.
	 */

	VOLTHALogLevel = log.FatalLevel
)

func localInit() {

	// Logger must be configured or bad things happen
	_, err := log.SetDefaultLogger(log.JSON, VOLTHALogLevel, log.Fields{"instanceId": 1})
	if err != nil {
		panic(err)
	}
}

func checkConfigFlags(t *testing.T, cf *ROCoreFlags) {

	if cf.ROCoreEndpoint != defaultROCoreEndpoint {
		t.Errorf("ROCoreEndpoint want %s, got %s", defaultROCoreEndpoint, cf.ROCoreEndpoint)
	}
	if cf.GrpcHost != defaultGrpcHost {
		t.Errorf("GrpcHost want %s, got %s", defaultGrpcHost, cf.GrpcHost)
	}
	if cf.GrpcPort != defaultGrpcPort {
		t.Errorf("GrpcPort want %d, got %d", defaultGrpcPort, cf.GrpcPort)
	}
	if cf.KVStoreType != defaultKVStoreType {
		t.Errorf("KVStoreType want %s, got %s", defaultKVStoreType, cf.KVStoreType)
	}
	if cf.KVStoreTimeout != defaultKVStoreTimeout {
		t.Errorf("KVStoreTimeout want %d, got %d", defaultKVStoreTimeout, cf.KVStoreTimeout)
	}
	if cf.KVStoreHost != defaultKVStoreHost {
		t.Errorf("KVStoreHost want %s, got %s", defaultKVStoreHost, cf.KVStoreHost)
	}
	if cf.KVStorePort != defaultKVStorePort {
		t.Errorf("KVStorePort want %d, got %d", defaultKVStorePort, cf.KVStorePort)
	}
	if cf.KVTxnKeyDelTime != defaultKVTxnKeyDelTime {
		t.Errorf("KVTxnKeyDelTime want %d, got %d", defaultKVTxnKeyDelTime, cf.KVTxnKeyDelTime)
	}
	if cf.CoreTopic != defaultCoreTopic {
		t.Errorf("CoreTopic want %s, got %s", defaultCoreTopic, cf.CoreTopic)
	}
	if cf.LogLevel != defaultLogLevel {
		t.Errorf("LogLevel want %d, got %d", defaultLogLevel, cf.LogLevel)
	}
	if cf.Banner != defaultBanner {
		t.Errorf("Banner want %v, got %v", defaultBanner, cf.Banner)
	}
	if cf.DisplayVersionOnly != defaultDisplayVersionOnly {
		t.Errorf("DisplayVersionOnly want %v, got %v", defaultDisplayVersionOnly, cf.DisplayVersionOnly)
	}
	if cf.ROCoreKey != defaultROCoreKey {
		t.Errorf("ROCoreKey want %s, got %s", defaultROCoreKey, cf.ROCoreKey)
	}
	if cf.ROCoreCert != defaultROCoreCert {
		t.Errorf("ROCoreCert want %s, got %s", defaultROCoreCert, cf.ROCoreCert)
	}
	if cf.ROCoreCA != defaultROCoreCA {
		t.Errorf("ROCoreCA want %s, got %s", defaultROCoreCA, cf.ROCoreCA)
	}
	if cf.AffinityRouterTopic != defaultAffinityRouterTopic {
		t.Errorf("AffinityRouterTopic want %s, got %s", defaultAffinityRouterTopic, cf.AffinityRouterTopic)
	}
	if cf.ProbeHost != defaultProbeHost {
		t.Errorf("ProbeHost want %s, got %s", defaultProbeHost, cf.ProbeHost)
	}
	if cf.ProbePort != defaultProbePort {
		t.Errorf("ProbePort want %d, got %d", defaultProbePort, cf.ProbePort)
	}
}

func TestNewROCoreFlags(t *testing.T) {
	localInit()

	var testStr string

	configFlags := NewROCoreFlags()
	assert.NotNil(t, configFlags)

	configFlags.ParseCommandArguments()
	checkConfigFlags(t, configFlags)

	seErr := os.Setenv("HOSTNAME", "PC-4")
	if seErr == nil {
		testStr = getContainerInfo()
		assert.NotNil(t, testStr)
		t.Logf("hostname: %s \n", testStr)
		if testStr != "PC-4" {
			t.Errorf("getContainerInfo failed. want: %s, got: %s", "PC-3", testStr)
		}
	} else {
		testStr = getContainerInfo()
		assert.NotNil(t, testStr)
	}
}
