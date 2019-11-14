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

func TestNewROCoreFlags(t *testing.T) {
	localInit()

	var fSign1 func() *ROCoreFlags
	var fSign2 func()
	var fSign3 func() string

	// Test function signature
	if reflect.TypeOf(fSign1) != reflect.TypeOf(NewROCoreFlags) {
		t.Skip()
	}

	configFlags := NewROCoreFlags()
	assert.NotNil(t, configFlags)

	// Test function signature
	if reflect.TypeOf(fSign2) != reflect.TypeOf(configFlags.ParseCommandArguments) {
		t.Skip()
	}
	configFlags.ParseCommandArguments()

	// Test function signature
	if reflect.TypeOf(fSign3) != reflect.TypeOf(getContainerInfo) {
		t.Skip()
	}
	testStr := getContainerInfo()
	assert.NotNil(t, testStr)
}
