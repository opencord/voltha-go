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
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	common_pb "github.com/opencord/voltha-protos/go/common"
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	SOURCE_ROUTER_PROTOFILE = "../../vendor/github.com/opencord/voltha-protos/go/voltha.pb"
)

func init() {
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func MakeSourceRouterTestConfig() (*RouteConfig, *RouterConfig) {
	connectionConfig := ConnectionConfig{
		Name: "ro_vcore01",
		Addr: "foo",
		Port: "123",
	}

	backendConfig := BackendConfig{
		Name:        "ro_vcore0",
		Type:        BackendSingleServer,
		Connections: []ConnectionConfig{connectionConfig},
	}

	backendClusterConfig := BackendClusterConfig{
		Name:     "ro_vcore",
		Backends: []BackendConfig{backendConfig},
	}

	routeConfig := RouteConfig{
		Name:           "logger",
		Type:           RouteTypeSource,
		RouteField:     "component_name",
		BackendCluster: "ro_vcore",
		backendCluster: &backendClusterConfig,
		Methods:        []string{"UpdateLogLevel", "GetLogLevel"},
	}

	routerConfig := RouterConfig{
		Name:         "vcore",
		ProtoService: "VolthaService",
		ProtoPackage: "voltha",
		Routes:       []RouteConfig{routeConfig},
		ProtoFile:    SOURCE_ROUTER_PROTOFILE,
	}
	return &routeConfig, &routerConfig
}

func TestSourceRouterInit(t *testing.T) {
	routeConfig, routerConfig := MakeSourceRouterTestConfig()

	router, err := newSourceRouter(routerConfig, routeConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	assert.Equal(t, router.Service(), "VolthaService")
	assert.Equal(t, router.Name(), "logger")

	cluster, err := router.BackendCluster("foo", "bar")
	assert.Equal(t, cluster, clusters["ro_vcore"])
	assert.Nil(t, err)

	assert.Equal(t, router.FindBackendCluster("ro_vcore"), clusters["ro_vcore"])
	assert.Nil(t, router.ReplyHandler("foo"))
}

func TestSourceRouterDecodeProtoField(t *testing.T) {
	_, routerConfig := MakeSourceRouterTestConfig()
	_, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	sourceRouter := allRouters["vcorelogger"].(SourceRouter)

	loggingMessage := &common_pb.Logging{Level: 1,
		PackageName:   "default",
		ComponentName: "ro_vcore0.ro_vcore01"}

	loggingData, err := proto.Marshal(loggingMessage)
	assert.Nil(t, err)

	s, err := sourceRouter.decodeProtoField(loggingData, 2) // field 2 is package_name
	assert.Equal(t, s, "default")

	s, err = sourceRouter.decodeProtoField(loggingData, 3) // field 2 is component_name
	assert.Equal(t, s, "ro_vcore0.ro_vcore01")
}

func TestSourceRouterRoute(t *testing.T) {
	_, routerConfig := MakeSourceRouterTestConfig()
	_, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	sourceRouter := allRouters["vcorelogger"].(SourceRouter)

	loggingMessage := &common_pb.Logging{Level: 1,
		PackageName:   "default",
		ComponentName: "ro_vcore0.ro_vcore01"}

	loggingData, err := proto.Marshal(loggingMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: loggingData,
		err:        nil,
		methodInfo: newMethodDetails("/voltha.VolthaService/UpdateLogLevel")}

	backend, connection := sourceRouter.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, backend.name, "ro_vcore0")
	assert.NotNil(t, connection)
	assert.Equal(t, connection.name, "ro_vcore01")
}
