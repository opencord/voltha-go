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
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
	"os"
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

	VOLTHA_LOGLEVEL = log.FatalLevel
)

func localInit() {

	// Logger must be configured or bad things happen
	_, err := log.SetDefaultLogger(log.JSON, VOLTHA_LOGLEVEL, log.Fields{"instanceId": 1})
	if err != nil {
		panic(err)
	}
}

func checkConfigFlags(t *testing.T, cf *ROCoreFlags) {

	if (cf.ROCoreEndpoint != default_ROCoreEndpoint) {
    	t.Errorf("ROCoreEndpoint want %s, got %s", default_ROCoreEndpoint, cf.ROCoreEndpoint)
    }
	if (cf.GrpcHost != default_GrpcHost) {
    	t.Errorf("GrpcHost want %s, got %s", default_GrpcHost, cf.GrpcHost)
    }
	if (cf.GrpcPort != default_GrpcPort) {
    	t.Errorf("GrpcPort want %d, got %d", default_GrpcPort, cf.GrpcPort)
    }
	if (cf.KVStoreType != default_KVStoreType) {
    	t.Errorf("KVStoreType want %s, got %s", default_KVStoreType, cf.KVStoreType)
    }
	if (cf.KVStoreTimeout != default_KVStoreTimeout) {
    	t.Errorf("KVStoreTimeout want %d, got %d", default_KVStoreTimeout, cf.KVStoreTimeout)
    }
	if (cf.KVStoreHost != default_KVStoreHost) {
    	t.Errorf("KVStoreHost want %s, got %s", default_KVStoreHost, cf.KVStoreHost)
    }
	if (cf.KVStorePort != default_KVStorePort) {
    	t.Errorf("KVStorePort want %d, got %d", default_KVStorePort, cf.KVStorePort)
    }
	if (cf.KVTxnKeyDelTime != default_KVTxnKeyDelTime) {
    	t.Errorf("KVTxnKeyDelTime want %d, got %d", default_KVTxnKeyDelTime, cf.KVTxnKeyDelTime)
    }
	if (cf.CoreTopic != default_CoreTopic) {
    	t.Errorf("CoreTopic want %s, got %s", default_CoreTopic, cf.CoreTopic)
    }		
	if (cf.LogLevel != default_LogLevel) {
    	t.Errorf("LogLevel want %d, got %d", default_LogLevel, cf.LogLevel)
    }	
	if (cf.Banner != default_Banner) {
    	t.Errorf("Banner want %v, got %v", default_Banner, cf.Banner)
    }
	if (cf.DisplayVersionOnly != default_DisplayVersionOnly) {
    	t.Errorf("DisplayVersionOnly want %v, got %v", default_DisplayVersionOnly, cf.DisplayVersionOnly)
    }	
	if (cf.ROCoreKey != default_ROCoreKey) {
    	t.Errorf("ROCoreKey want %s, got %s", default_ROCoreKey, cf.ROCoreKey)
    }	
	if (cf.ROCoreCert != default_ROCoreCert) {
    	t.Errorf("ROCoreCert want %s, got %s", default_ROCoreCert, cf.ROCoreCert)
    }		
	if (cf.ROCoreCA != default_ROCoreCA) {
    	t.Errorf("ROCoreCA want %s, got %s", default_ROCoreCA, cf.ROCoreCA)
    }
	if (cf.AffinityRouterTopic != default_Affinity_Router_Topic) {
    	t.Errorf("AffinityRouterTopic want %s, got %s", default_Affinity_Router_Topic, cf.AffinityRouterTopic)
    }
	if (cf.ProbeHost != default_ProbeHost) {
    	t.Errorf("ProbeHost want %s, got %s", default_ProbeHost, cf.ProbeHost)
    }
	if (cf.ProbePort != default_ProbePort) {
    	t.Errorf("ProbePort want %d, got %d", default_ProbePort, cf.ProbePort)
    }			
}


func TestNewROCoreFlags(t *testing.T) {
	localInit()

	var fSign1 func() *ROCoreFlags
	var fSign2 func()
	var fSign3 func() string
	var testStr string

	// Test function signature
	if reflect.TypeOf(fSign1) != reflect.TypeOf(NewROCoreFlags) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(NewROCoreFlags), reflect.TypeOf(fSign1))
	}

	configFlags := NewROCoreFlags()
	assert.NotNil(t, configFlags)

	// Test function signature
	if reflect.TypeOf(fSign2) != reflect.TypeOf(configFlags.ParseCommandArguments) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(configFlags.ParseCommandArguments), reflect.TypeOf(fSign2))
	}
	configFlags.ParseCommandArguments()
	checkConfigFlags(t, configFlags)
    
	// Test function signature
	if reflect.TypeOf(fSign3) != reflect.TypeOf(getContainerInfo) {
		t.Errorf("Function signature mismatch. current: %s, was: %s", reflect.TypeOf(getContainerInfo), reflect.TypeOf(fSign3))
	}
	
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
