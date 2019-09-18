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
	"fmt"
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
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

func MakeRoundRobinTestConfig(numBackends int, numConnections int) (*RouteConfig, *RouterConfig) {

	var backends []BackendConfig
	for backendIndex := 0; backendIndex < numBackends; backendIndex++ {
		var connections []ConnectionConfig
		for connectionIndex := 0; connectionIndex < numConnections; connectionIndex++ {
			connectionConfig := ConnectionConfig{
				Name: fmt.Sprintf("ro_vcore%d%d", backendIndex, connectionIndex+1),
				Addr: "foo",
				Port: "123",
			}
			connections = append(connections, connectionConfig)
		}

		backendConfig := BackendConfig{
			Name:        fmt.Sprintf("ro_vcore%d", backendIndex),
			Type:        BackendSingleServer,
			Connections: connections,
		}

		backends = append(backends, backendConfig)
	}

	backendClusterConfig := BackendClusterConfig{
		Name:     "ro_vcore",
		Backends: backends,
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
	return &routeConfig, &routerConfig
}

// Route() requires an open connection, so pretend we have one.
func PretendRoundRobinOpenConnection(router Router, clusterName string, backendIndex int, connectionName string) {
	cluster := router.FindBackendCluster(clusterName)

	// Route Method expects an open connection
	conn := cluster.backends[backendIndex].connections[connectionName]
	cluster.backends[backendIndex].openConns[conn] = &grpc.ClientConn{}
}

// Common setup to run before each unit test
func RoundRobinTestSetup() {
	// reset globals that need to be clean for each unit test

	clusters = make(map[string]*cluster)
}

// Test creation of a new RoundRobinRouter, and the Service(), Name(), FindBackendCluster(), and
// ReplyHandler() methods.
func TestRoundRobinRouterInit(t *testing.T) {
	RoundRobinTestSetup()

	routeConfig, routerConfig := MakeRoundRobinTestConfig(1, 1)

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

// Tests a cluster with only one Backend
func TestRoundRobinRouteOne(t *testing.T) {
	RoundRobinTestSetup()

	routeConfig, routerConfig := MakeRoundRobinTestConfig(1, 1)

	router, err := newRoundRobinRouter(routerConfig, routeConfig)
	assert.Nil(t, err)

	PretendRoundRobinOpenConnection(router, "ro_vcore", 0, "ro_vcore01")

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		methodInfo: newMethodDetails("/voltha.VolthaService/ListDevicePorts")}

	backend, connection := router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "ro_vcore0", backend.name)
	assert.Nil(t, connection)

	// Since we only have one backend, calling Route a second time should return the same one

	backend, connection = router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "ro_vcore0", backend.name)
	assert.Nil(t, connection)
}

// Tests a cluster with two Backends
func TestRoundRobinRouteTwo(t *testing.T) {
	RoundRobinTestSetup()

	routeConfig, routerConfig := MakeRoundRobinTestConfig(2, 1)

	router, err := newRoundRobinRouter(routerConfig, routeConfig)
	assert.Nil(t, err)

	PretendRoundRobinOpenConnection(router, "ro_vcore", 0, "ro_vcore01")
	PretendRoundRobinOpenConnection(router, "ro_vcore", 1, "ro_vcore11")

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		methodInfo: newMethodDetails("/voltha.VolthaService/ListDevicePorts")}

	backend, connection := router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "ro_vcore0", backend.name)
	assert.Nil(t, connection)

	// Since we have two backends, calling Route a second time should return the second

	backend, connection = router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "ro_vcore1", backend.name)
	assert.Nil(t, connection)

	// Calling Route a third time should return the first again

	backend, connection = router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "ro_vcore0", backend.name)
	assert.Nil(t, connection)
}
