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
	"google.golang.org/grpc"
	"testing"
)

const (
	ROUND_ROBIN_ROUTER_PROTOFILE = "../../vendor/github.com/opencord/voltha-protos/go/voltha.pb"
)

func init() {
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func MakeRoundRobinTestConfig() (*ConnectionConfig, *BackendConfig, *BackendClusterConfig, *RouteConfig, *RouterConfig) {
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
		Name:           "read_only",
		Type:           RouteTypeRoundRobin,
		Association:    AssociationRoundRobin,
		BackendCluster: "ro_vcore",
		backendCluster: &backendClusterConfig,
		Methods:        []string{"ListDevicePorts"},
	}

	routerConfig := RouterConfig{
		Name:         "vcore",
		ProtoService: "VolthaService",
		ProtoPackage: "voltha",
		Routes:       []RouteConfig{routeConfig},
		ProtoFile:    ROUND_ROBIN_ROUTER_PROTOFILE,
	}
	return &connectionConfig, &backendConfig, &backendClusterConfig, &routeConfig, &routerConfig
}

func TestRoundRobinRouterInit(t *testing.T) {
	_, _, _, routeConfig, routerConfig := MakeRoundRobinTestConfig()

	router, err := newRoundRobinRouter(routerConfig, routeConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	assert.Equal(t, router.Service(), "VolthaService")
	assert.Equal(t, router.Name(), "read_only")

	cluster, err := router.BackendCluster("foo", "bar")
	assert.Equal(t, cluster, clusters["ro_vcore"])
	assert.Nil(t, err)

	assert.Equal(t, router.FindBackendCluster("ro_vcore"), clusters["ro_vcore"])
	assert.Nil(t, router.ReplyHandler("foo"))
}

func TestRoundRobinRoute(t *testing.T) {
	_, _, _, routeConfig, routerConfig := MakeRoundRobinTestConfig()

	router, err := newRoundRobinRouter(routerConfig, routeConfig)
	assert.Equal(t, err, nil)

	cluster := router.FindBackendCluster("ro_vcore")
	assert.Equal(t, nil, err)

	conn := cluster.backends[0].connections["ro_cvore01"]
	cluster.backends[0].openConns[conn] = &grpc.ClientConn{}

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Equal(t, err, nil)

	sel := &requestFrame{payload: idData,
		err:        nil,
		methodInfo: newMethodDetails("/volta.VolthaService/ListDevicePorts")}

	backend, connection := router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "ro_vcore0", backend.name)
	assert.Nil(t, connection)
}
