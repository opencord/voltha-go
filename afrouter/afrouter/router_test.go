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

package afrouter

import (
	"github.com/opencord/voltha-go/common/log"
	"github.com/stretchr/testify/assert"
	"testing"
)
func init() {
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func TestRouter(t *testing.T) {
	routeConfig, routerConfig := MakeRoundRobinTestConfig(1, 1)	
	router, err := newRouter(routerConfig)
	assert.NotNil(t, router)
	assert.NotNil(t, routeConfig)
	assert.Nil(t, err)
}

func TestSubRouter(t *testing.T) {	
	routeConfig, routerConfig := MakeRoundRobinTestConfig(1, 1)	
	router, err := newSubRouter(routerConfig, routeConfig)
	assert.NotNil(t, router)
	assert.Nil(t, err)
}


