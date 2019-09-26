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
	voltha_pb "github.com/opencord/voltha-protos/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"testing"
)

const (
	METHOD_ROUTER_PROTOFILE = "../../vendor/github.com/opencord/voltha-protos/go/voltha.pb"
)

// Unit test initialization
func init() {
	// Logger must be configured or bad things happen
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// Build an method router configuration
func MakeMethodTestConfig(numBackends int, numConnections int) (*RouteConfig, *RouterConfig) {

	var backends []BackendConfig
	for backendIndex := 0; backendIndex < numBackends; backendIndex++ {
		var connections []ConnectionConfig
		for connectionIndex := 0; connectionIndex < numConnections; connectionIndex++ {
			connectionConfig := ConnectionConfig{
				Name: fmt.Sprintf("rw_vcore%d%d", backendIndex, connectionIndex+1),
				Addr: "foo",
				Port: "123",
			}
			connections = append(connections, connectionConfig)
		}

		backendConfig := BackendConfig{
			Name:        fmt.Sprintf("rw_vcore%d", backendIndex),
			Type:        BackendSingleServer,
			Connections: connections,
		}

		backends = append(backends, backendConfig)
	}

	backendClusterConfig := BackendClusterConfig{
		Name:     "vcore",
		Backends: backends,
	}

	routeConfig := RouteConfig{
		Name:             "dev_manager",
		Type:             RouteTypeRpcAffinityMessage,
		Association:      AssociationRoundRobin,
		BackendCluster:   "vcore",
		backendCluster:   &backendClusterConfig,
		RouteField:       "id",
		Methods:          []string{"CreateDevice", "EnableDevice"},
		NbBindingMethods: []string{"CreateDevice"},
	}

	routerConfig := RouterConfig{
		Name:         "vcore",
		ProtoService: "VolthaService",
		ProtoPackage: "voltha",
		Routes:       []RouteConfig{routeConfig},
		ProtoFile:    METHOD_ROUTER_PROTOFILE,
	}
	return &routeConfig, &routerConfig
}

// Route() requires an open connection, so pretend we have one.
func PretendMethodOpenConnection(router Router, clusterName string, backendIndex int, connectionName string) {
	cluster := router.FindBackendCluster(clusterName)

	// Route Method expects an open connection
	conn := cluster.backends[backendIndex].connections[connectionName]
	cluster.backends[backendIndex].openConns[conn] = &grpc.ClientConn{}
}

// Common setup to run before each unit test
func MethodTestSetup() {
	// reset globals that need to be clean for each unit test

	clusters = make(map[string]*cluster)
	allRouters = make(map[string]Router)
}

// Test creation of a new AffinityRouter, and the Service(), Name(), FindBackendCluster(), and
// methods.
func TestMethodRouterInit(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	assert.Equal(t, router.Service(), "VolthaService")
	assert.Equal(t, router.Name(), "vcore")

	cluster, err := router.BackendCluster("EnableDevice", NoMeta)
	assert.Equal(t, cluster, clusters["vcore"])
	assert.Nil(t, err)

	assert.Equal(t, router.FindBackendCluster("vcore"), clusters["vcore"])
}

// Passing an invalid meta should return an error
func TestMethodRouterBackendClusterInvalidMeta(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	cluster, err := router.BackendCluster("EnableDevice", "wrongmeta")
	assert.EqualError(t, err, "No backend cluster exists for method 'EnableDevice' using meta key 'wrongmeta'")
	assert.Nil(t, cluster)
}

// Passing an invalid method name should return an error
func TestMethodRouterBackendClusterInvalidMethod(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	cluster, err := router.BackendCluster("WrongMethod", NoMeta)
	assert.EqualError(t, err, "No backend cluster exists for method 'WrongMethod' using meta key 'nometa'")
	assert.Nil(t, cluster)
}

// Search for a backend cluster that doesn't exist
func TestMethodRouterFindBackendClusterNoExist(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	assert.Nil(t, router.FindBackendCluster("wrong"))
}

// MethodRouter's route will cause another router's route, in this case AffinityRouter.
func TestMethodRoute(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)
	assert.Nil(t, err)

	PretendMethodOpenConnection(router, "vcore", 0, "rw_vcore01")

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    NoMeta,
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice")}

	backend, connection := router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "rw_vcore0", backend.name)
	assert.Nil(t, connection)

	// Since we only have one backend, calling Route a second time should return the same one

	backend, connection = router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "rw_vcore0", backend.name)
	assert.Nil(t, connection)
}

// Try to route to a nonexistent method
func TestMethodRouteNonexistent(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)
	assert.Nil(t, err)

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    NoMeta,
		methodInfo: newMethodDetails("/voltha.VolthaService/NonexistentMethod")}

	backend, connection := router.Route(sel)

	assert.Nil(t, backend)
	assert.Nil(t, connection)

	assert.EqualError(t, sel.err, "MethodRouter.Route unable to resolve meta nometa, method NonexistentMethod")
}

// Try to route to a nonexistent meta key
func TestMethodRouteWrongMeta(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)
	assert.Nil(t, err)

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    "wrongkey",
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice")}

	backend, connection := router.Route(sel)

	assert.Nil(t, backend)
	assert.Nil(t, connection)

	assert.EqualError(t, sel.err, "MethodRouter.Route unable to resolve meta wrongkey, method EnableDevice")
}

// Try to route to a the wrong type of key
func TestMethodRouteWrongFrame(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newMethodRouter(routerConfig)
	assert.Nil(t, err)

	idMessage := &voltha_pb.Device{Id: "1234"}
	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	// Note that sel.backend must be set. As this is a response, it must
	// have come from a backend and that backend must be known.

	sel := &responseFrame{payload: idData,
		metaKey: NoMeta,
		backend: router.FindBackendCluster("vcore").backends[0],
		method:  "CreateDevice",
	}

	// Note: Does not return any error, but does print an error message. Returns nil.

	backend, connection := router.Route(sel)

	assert.Nil(t, backend)
	assert.Nil(t, connection)
}

// MethodRouter calls another Router's ReplyHandler, in this case AffinityRouter
func TestMethodRouteReply(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	aRouter := allRouters["vcoredev_manager"].(AffinityRouter)

	PretendMethodOpenConnection(router, "vcore", 0, "rw_vcore01")

	idMessage := &voltha_pb.Device{Id: "1234"}
	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	// Note that sel.backend must be set. As this is a response, it must
	// have come from a backend and that backend must be known.

	sel := &responseFrame{payload: idData,
		metaKey: NoMeta,
		backend: router.FindBackendCluster("vcore").backends[0],
		method:  "CreateDevice",
	}

	// affinity should be unset as we have not routed yet
	assert.Empty(t, aRouter.affinity)

	err = router.ReplyHandler(sel)
	assert.Nil(t, err)

	// affinity should now be set
	assert.NotEmpty(t, aRouter.affinity)
	assert.Equal(t, router.FindBackendCluster("vcore").backends[0], aRouter.affinity["1234"])
}

// Call ReplyHandler with the wrong type of frame
func TestMethodRouteReplyWrongFrame(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	PretendMethodOpenConnection(router, "vcore", 0, "rw_vcore01")

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    "wrongkey",
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice")}

	err = router.ReplyHandler(sel)
	assert.EqualError(t, err, "MethodRouter.ReplyHandler called with non-reponseFrame")
}

// Call ReplyHandler with an invalid method name
func TestMethodRouteReplyWrongMethod(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	PretendMethodOpenConnection(router, "vcore", 0, "rw_vcore01")

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    "wrongkey",
		methodInfo: newMethodDetails("/voltha.VolthaService/WrongMethod")}

	err = router.ReplyHandler(sel)
	assert.EqualError(t, err, "MethodRouter.ReplyHandler called with non-reponseFrame")
}

func TestMethodIsMethodStreaming(t *testing.T) {
	MethodTestSetup()

	_, routerConfig := MakeMethodTestConfig(1, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	request, response := router.IsStreaming("EnableDevice")
	assert.False(t, request)
	assert.False(t, response)
}
