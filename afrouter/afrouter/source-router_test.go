/*
 * Portions copyright 2019-present Open Networking Foundation
 * Original copyright 2019-present Ciena Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the"github.com/stretchr/testify/assert" "License");
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
package afrouter

import (
	"github.com/opencord/voltha-go/common/log"
	"github.com/stretchr/testify/assert"
	"testing"
)

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func MakeTestConfig() (*RouteConfig, *RouterConfig) {
	bcc := BackendClusterConfig{Name: "ro_vcore"}

	routeConfig := RouteConfig{
		Name:           "logger",
		Type:           RouteTypeSource,
		RouteField:     "component_name",
		BackendCluster: "ro_vcore",
		backendCluster: &bcc,
		Methods:        []string{"UpdateLogLevel", "GetLogLevel"},
		ProtoFile:      "../../../voltha-go/vendor/github.com/opencord/voltha-protos/go/voltha.pb",
	}

	routerConfig := RouterConfig{
		Name:         "vcore",
		ProtoService: "VolthaService",
		ProtoPackage: "voltha",
		Routes:       []RouteConfig{routeConfig},
	}
	return &routeConfig, &routerConfig
}

func TestSourceRouterInit(t *testing.T) {
	routeConfig, routerConfig := MakeTestConfig()

	router, err := newSourceRouter(routerConfig, routeConfig)

	assert.NotEqual(t, router, nil)
	assert.Equal(t, err, nil)
}
