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

// This file implements an exit handler that tries to shut down all the
// running servers before finally exiting. There are 2 triggers to this
// clean exit thread: signals and an exit channel.

package afrouter

import (
	"github.com/opencord/voltha-go/common/log"
    "github.com/stretchr/testify/assert"
	"testing"
)

const (
	SIGNALS_PROTOFILE = "../../vendor/github.com/opencord/voltha-protos/go/voltha.pb"
)

func init() {
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// Test for function signalHandler and cleanExit
func TestSignalsInitExitHandler(t *testing.T) {
	
	assert.Nil(t, InitExitHandler())

}

