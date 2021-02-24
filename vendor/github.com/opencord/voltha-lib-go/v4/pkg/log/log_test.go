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
	"testing"
)

/*
Prerequite:  Start the kafka/zookeeper containers.
*/

var testLogger CLogger

func TestInit(t *testing.T) {
	var err error
	testLogger, err = RegisterPackage(JSON, ErrorLevel, nil)
	assert.NotNil(t, testLogger)
	assert.Nil(t, err)
}

func verifyLogLevel(t *testing.T, minimumLevel int) {
	SetAllLogLevel(LogLevel(minimumLevel))
	var success bool
	for i := 0; i < 5; i++ {
		success = testLogger.V(LogLevel(i))
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
	for i := 0; i < 5; i++ {
		verifyLogLevel(t, i)
	}
}

func TestUpdateAllLoggers(t *testing.T) {
	err := UpdateAllLoggers(Fields{"update": "update"})
	assert.Nil(t, err)
}

func TestUpdateLoggers(t *testing.T) {
	err := UpdateLogger(Fields{"update": "update"})
	assert.Nil(t, err)
}

func TestUpdateLogLevel(t *testing.T) {
	//	Let's create a bunch of logger each with a separate package
	myLoggers := make(map[string]CLogger)
	pkgNames := []string{"/rw_core/core", "/db/model", "/kafka"}
	for _, name := range pkgNames {
		myLoggers[name], _ = RegisterPackage(JSON, ErrorLevel, nil, []string{name}...)
	}
	//Test updates to log levels
	levels := []LogLevel{DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel}
	for _, expectedLevel := range levels {
		for _, name := range pkgNames {
			SetPackageLogLevel(name, expectedLevel)
			l, err := GetPackageLogLevel(name)
			assert.Nil(t, err)
			assert.Equal(t, l, expectedLevel)
			// Get the package log level by invoking the specific logger created for this package
			// This is a less expensive operation that the GetPackageLogLevel() request
			level := myLoggers[name].GetLogLevel()
			assert.Equal(t, level, expectedLevel)
			// Check the verbosity level
			for _, level := range levels {
				toDisplay := myLoggers[name].V(level)
				if level < expectedLevel {
					assert.False(t, toDisplay)
				} else {
					assert.True(t, toDisplay)
				}
			}
		}
	}
	//Test set all package level
	for _, expectedLevel := range levels {
		SetAllLogLevel(LogLevel(expectedLevel))
		for _, name := range pkgNames {
			l, err := GetPackageLogLevel(name)
			assert.Nil(t, err)
			assert.Equal(t, l, LogLevel(expectedLevel))
			level := myLoggers[name].GetLogLevel()
			assert.Equal(t, level, LogLevel(expectedLevel))
			// Check the verbosity level
			for _, level := range levels {
				toDisplay := myLoggers[name].V(level)
				if level < expectedLevel {
					assert.False(t, toDisplay)
				} else {
					assert.True(t, toDisplay)
				}
			}
		}
	}
}
