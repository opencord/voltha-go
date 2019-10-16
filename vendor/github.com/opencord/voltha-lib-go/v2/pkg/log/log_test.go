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
package log

import (
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/grpclog"
	"testing"
)

/*
Prerequite:  Start the kafka/zookeeper containers.
*/

var testLogger Logger

func TestInit(t *testing.T) {
	var err error
	testLogger, err = AddPackage(JSON, ErrorLevel, nil)
	assert.NotNil(t, testLogger)
	assert.Nil(t, err)
}

func verifyLogLevel(t *testing.T, minimumLevel int) {
	SetAllLogLevel(minimumLevel)
	var success bool
	for i := 0; i < 6; i++ {
		success = testLogger.V(i)
		if i == 1 && minimumLevel == 2 {
			// TODO: Update the test when a new version of Zap logger is available.  It has a bug with that
			// specific combination
			continue
		}
		if i < minimumLevel {
			assert.False(t, success)
		} else {
			assert.True(t, success)
		}
	}
}

func TestLogLevelDebug(t *testing.T) {
	for i := 0; i < 6; i++ {
		verifyLogLevel(t, i)
	}
}

func TestUpdateAllLoggers(t *testing.T) {
	err := UpdateAllLoggers(Fields{"update": "update"})
	assert.Nil(t, err)
}

func TestUpdateLoggers(t *testing.T) {
	testLogger, err := UpdateLogger(Fields{"update": "update"})
	assert.Nil(t, err)
	assert.NotNil(t, testLogger)
}

func TestUseAsGrpcLoggerV2(t *testing.T) {
	var grpcLogger grpclog.LoggerV2
	thisLogger, _ := AddPackage(JSON, ErrorLevel, nil)
	grpcLogger = thisLogger
	assert.NotNil(t, grpcLogger)
}

func TestUpdateLogLevel(t *testing.T) {
	//	Let's create a bunch of logger each with a separate package
	myLoggers := make(map[string]Logger)
	pkgNames := []string{"/rw_core/core", "/db/model", "/kafka"}
	for _, name := range pkgNames {
		myLoggers[name], _ = AddPackage(JSON, ErrorLevel, nil, []string{name}...)
	}
	//Test updates to log levels
	levels := []int{0, 1, 2, 3, 4, 5}
	for _, expectedLevel := range levels {
		for _, name := range pkgNames {
			SetPackageLogLevel(name, expectedLevel)
			l, err := GetPackageLogLevel(name)
			assert.Nil(t, err)
			assert.Equal(t, l, expectedLevel)
		}
	}
	//Test set all package level
	for _, expectedLevel := range levels {
		SetAllLogLevel(expectedLevel)
		for _, name := range pkgNames {
			l, err := GetPackageLogLevel(name)
			assert.Nil(t, err)
			assert.Equal(t, l, expectedLevel)
		}
	}
}
