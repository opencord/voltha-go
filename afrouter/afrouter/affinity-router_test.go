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
	AFFINITY_ROUTER_PROTOFILE = "../../vendor/github.com/opencord/voltha-protos/go/voltha.pb"
)

// Unit test initialization
func init() {
	// Logger must be configured or bad things happen
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

// Build an affinity router configuration
func MakeAffinityTestConfig(numBackends int, numConnections int) (*RouteConfig, *RouterConfig) {

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
		ProtoFile:    AFFINITY_ROUTER_PROTOFILE,
	}
	return &routeConfig, &routerConfig
}

// Route() requires an open connection, so pretend we have one.
func PretendAffinityOpenConnection(router Router, clusterName string, backendIndex int, connectionName string) {
	cluster := router.FindBackendCluster(clusterName)

	// Route Method expects an open connection
	conn := cluster.backends[backendIndex].connections[connectionName]
	cluster.backends[backendIndex].openConns[conn] = &grpc.ClientConn{}
}

// Common setup to run before each unit test
func AffinityTestSetup() {
	// reset globals that need to be clean for each unit test

	clusters = make(map[string]*cluster)
	allRouters = make(map[string]Router)
}

// Test creation of a new AffinityRouter, and the Service(), Name(), FindBackendCluster(), and
// methods.
func TestAffinityRouterInit(t *testing.T) {
	AffinityTestSetup()

	routeConfig, routerConfig := MakeAffinityTestConfig(1, 1)

	router, err := newAffinityRouter(routerConfig, routeConfig)

	assert.NotNil(t, router)
	assert.Nil(t, err)

	assert.Equal(t, router.Service(), "VolthaService")
	assert.Equal(t, router.Name(), "dev_manager")

	cluster, err := router.BackendCluster("foo", "bar")
	assert.Equal(t, cluster, clusters["vcore"])
	assert.Nil(t, err)

	assert.Equal(t, router.FindBackendCluster("vcore"), clusters["vcore"])
}

// Should throw error if no name in configuration
func TestAffinityRouterInitNoName(t *testing.T) {
	AffinityTestSetup()

	routeConfig, routerConfig := MakeAffinityTestConfig(1, 1)
	routeConfig.Name = ""

	_, err := newAffinityRouter(routerConfig, routeConfig)

	assert.EqualError(t, err, "Failed to create a new router ''")
}

// Should thow error if now ProtoPackage in configuration
func TestAffinityRouterInitNoProtoPackage(t *testing.T) {
	AffinityTestSetup()

	routeConfig, routerConfig := MakeAffinityTestConfig(1, 1)
	routerConfig.ProtoPackage = ""

	_, err := newAffinityRouter(routerConfig, routeConfig)

	assert.EqualError(t, err, "Failed to create a new router 'dev_manager'")
}

// Should throw error if no ProtoServer in configuration
func TestAffinityRouterInitNoProtoService(t *testing.T) {
	AffinityTestSetup()

	routeConfig, routerConfig := MakeAffinityTestConfig(1, 1)
	routerConfig.ProtoService = ""

	_, err := newAffinityRouter(routerConfig, routeConfig)

	assert.EqualError(t, err, "Failed to create a new router 'dev_manager'")
}

// Tests a cluster with only one Backend
func TestAffinityRouteOne(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(1, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	PretendAffinityOpenConnection(router, "vcore", 0, "rw_vcore01")

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

// Tests a cluster with two Backends
func TestAffinityRouteTwo(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(2, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	PretendAffinityOpenConnection(router, "vcore", 0, "rw_vcore01")
	PretendAffinityOpenConnection(router, "vcore", 1, "rw_vcore11")

	idMessage := &common_pb.ID{Id: "1234"}
	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    NoMeta,
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice")}

	// We should Route to the first core and bind affinity to it

	backend, connection := router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "rw_vcore0", backend.name)
	assert.Nil(t, connection)

	// We should have established affinity, and trying Route again should return the same core

	backend, connection = router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "rw_vcore0", backend.name)
	assert.Nil(t, connection)

	// Make up a message with a different id
	idMessage = &common_pb.ID{Id: "1235"}
	idData, err = proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel = &requestFrame{payload: idData,
		err:        nil,
		metaKey:    NoMeta,
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice")}

	// Calling Route with the new ID should cause it to bind affinity to the second core

	backend, connection = router.Route(sel)

	assert.Nil(t, sel.err)
	assert.NotNil(t, backend)
	assert.Equal(t, "rw_vcore1", backend.name)
	assert.Nil(t, connection)
}

// Tests a cluster with one backend but no open connections
func TestAffinityRouteOneNoOpenConnection(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(1, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	idMessage := &common_pb.ID{Id: "1234"}

	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    NoMeta,
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice")}

	backend, connection := router.Route(sel)

	assert.EqualError(t, sel.err, "No backend with open connections found")
	assert.Nil(t, backend)
	assert.Nil(t, connection)
}

// Tests binding on reply
func TestAffinityRouteReply(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(2, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	aRouter := allRouters["vcoredev_manager"].(AffinityRouter)

	PretendAffinityOpenConnection(router, "vcore", 0, "rw_vcore01")
	PretendAffinityOpenConnection(router, "vcore", 1, "rw_vcore11")

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

	err = aRouter.ReplyHandler(sel)
	assert.Nil(t, err)

	// affinity should now be set
	assert.NotEmpty(t, aRouter.affinity)
	assert.Equal(t, router.FindBackendCluster("vcore").backends[0], aRouter.affinity["1234"])
}

// Tests binding on reply, with incorrect frame type
func TestAffinityRouteReplyIncorrectFrame(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(2, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	aRouter := allRouters["vcoredev_manager"].(AffinityRouter)

	PretendAffinityOpenConnection(router, "vcore", 0, "rw_vcore01")
	PretendAffinityOpenConnection(router, "vcore", 1, "rw_vcore11")

	idMessage := &voltha_pb.Device{Id: "1234"}
	idData, err := proto.Marshal(idMessage)
	assert.Nil(t, err)

	sel := &requestFrame{payload: idData,
		err:        nil,
		metaKey:    NoMeta,
		methodInfo: newMethodDetails("/voltha.VolthaService/EnableDevice"),
	}

	// ReplyHandler expects a replyFrame and we're giving it a requestFrame instead

	err = aRouter.ReplyHandler(sel)
	assert.EqualError(t, err, "Internal: invalid data type in ReplyHander call &{[10 4 49 50 51 52] <nil> <nil> <nil> <nil> {/voltha.VolthaService/EnableDevice voltha VolthaService EnableDevice}  nometa }")
}

func TestAffinityRouterDecodeProtoField(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(2, 1)

	_, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	aRouter := allRouters["vcoredev_manager"].(AffinityRouter)

	// Pick something to test with lots of field types. Port is a good candidate.
	portMessage := &voltha_pb.Port{PortNo: 123,
		Label:     "testlabel",
		Type:      3,
		DeviceId:  "5678",
		RxPackets: 9876,
	}

	portData, err := proto.Marshal(portMessage)
	assert.Nil(t, err)

	/*
	 * Decode various fields in the protobuf. Decoding each subsequent
	 * field implies skipfield() is called on its predecessor.
	 */

	s, err := aRouter.decodeProtoField(portData, 1) // field 1 is PortNo
	assert.Equal(t, "123", s)

	// Test VOL-1882, skipping of varint field. Note: May cause infinite loop if not fixed!
	s, err = aRouter.decodeProtoField(portData, 2) // field 2 is Label
	assert.Equal(t, "testlabel", s)

	s, err = aRouter.decodeProtoField(portData, 3) // field 3 is PortType
	assert.Equal(t, "3", s)

	s, err = aRouter.decodeProtoField(portData, 7) // field 7 is DeviceId
	assert.Equal(t, "5678", s)

	// TODO: Seems like an int64 ought to be allowed...
	s, err = aRouter.decodeProtoField(portData, 9) // field 7 is RxPackets
	assert.EqualError(t, err, "Only integer and string route selectors are permitted")
}

// Test setting affinity for a key to a backend
func TestAffinitySetAffinity(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(2, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	aRouter := allRouters["vcoredev_manager"].(AffinityRouter)

	backend := router.FindBackendCluster("vcore").backends[0]
	err = aRouter.setAffinity("1234", backend)

	assert.Nil(t, err)
}

// Trying to set affinity when it has already been set should fail.
func TestAffinitySetAffinityChange(t *testing.T) {
	AffinityTestSetup()

	_, routerConfig := MakeAffinityTestConfig(2, 1)

	router, err := newRouter(routerConfig)
	assert.Nil(t, err)

	// Get the created AffinityRouter so we can inspect its state
	aRouter := allRouters["vcoredev_manager"].(AffinityRouter)

	backend := router.FindBackendCluster("vcore").backends[0]
	err = aRouter.setAffinity("1234", backend)

	assert.Nil(t, err)

	// Now pick a different backend
	backend = router.FindBackendCluster("vcore").backends[1]
	err = aRouter.setAffinity("1234", backend)

	assert.EqualError(t, err, "Attempting multiple sets of affinity for key 1234 to backend rw_vcore1 from rw_vcore0 on router dev_manager")
}
