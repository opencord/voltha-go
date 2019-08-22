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
	//	"github.com/fullstorydev/grpcurl"
	"github.com/golang/protobuf/proto"
	//	descpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	//	"github.com/jhump/protoreflect/dynamic"
	"github.com/opencord/voltha-go/common/log"
	common_pb "github.com/opencord/voltha-protos/go/common"
	"github.com/stretchr/testify/assert"
	//"io/ioutil"
	"testing"
)

const (
	PROTOFILE = "../../../voltha-go/vendor/github.com/opencord/voltha-protos/go/voltha.pb"
)

func init() {
	log.SetDefaultLogger(log.JSON, log.DebugLevel, nil)
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

/*
func MakeProto(filename string, messageName string) (*dynamic.Message, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var fds descpb.FileDescriptorSet
	err = proto.Unmarshal(data, &fds)
	if err != nil {
		return nil, err
	}
	desc, err := grpcurl.DescriptorSourceFromFileDescriptorSet(&fds)
	if err != nil {
		return nil, err
	}
	symbol, err := desc.FindSymbol(messageName)
	if err != nil {
		return nil, err
	}
	file := symbol.GetFile()
	messageDescriptor := file.FindMessage(messageName)

	message := dynamic.NewMessage(messageDescriptor)

	return message, nil
}
*/

func MakeTestConfig() (*ConnectionConfig, *BackendConfig, *BackendClusterConfig, *RouteConfig, *RouterConfig) {
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
		ProtoFile:      PROTOFILE,
	}

	routerConfig := RouterConfig{
		Name:         "vcore",
		ProtoService: "VolthaService",
		ProtoPackage: "voltha",
		Routes:       []RouteConfig{routeConfig},
	}
	return &connectionConfig, &backendConfig, &backendClusterConfig, &routeConfig, &routerConfig
}

func TestSourceRouterInit(t *testing.T) {
	_, _, _, routeConfig, routerConfig := MakeTestConfig()

	router, err := newSourceRouter(routerConfig, routeConfig)

	assert.NotEqual(t, router, nil)
	assert.Equal(t, err, nil)

	assert.Equal(t, router.Service(), "VolthaService")
	assert.Equal(t, router.Name(), "logger")

	cluster, err := router.BackendCluster("foo", "bar")
	assert.Equal(t, cluster, clusters["ro_vcore"])
	assert.Equal(t, err, nil)

	assert.Equal(t, router.FindBackendCluster("ro_vcore"), clusters["ro_vcore"])
	assert.Equal(t, router.ReplyHandler("foo"), nil)
}

func TestDecodeProtoField(t *testing.T) {
	_, _, _, routeConfig, routerConfig := MakeTestConfig()

	router, err := newSourceRouter(routerConfig, routeConfig)
	assert.Equal(t, err, nil)

	loggingMessage := &common_pb.Logging{Level: 1,
		PackageName:   "default",
		ComponentName: "ro_vcore0.ro_vcore01"}

	loggingData, err := proto.Marshal(loggingMessage)
	assert.Equal(t, err, nil)

	s, err := router.(SourceRouter).decodeProtoField(loggingData, 2) // field 2 is package_name
	assert.Equal(t, s, "default")

	s, err = router.(SourceRouter).decodeProtoField(loggingData, 3) // field 2 is component_name
	assert.Equal(t, s, "ro_vcore0.ro_vcore01")
}

func TestRoute(t *testing.T) {
	_, _, _, routeConfig, routerConfig := MakeTestConfig()

	router, err := newSourceRouter(routerConfig, routeConfig)
	assert.Equal(t, err, nil)

	loggingMessage := &common_pb.Logging{Level: 1,
		PackageName:   "default",
		ComponentName: "ro_vcore0.ro_vcore01"}

	loggingData, err := proto.Marshal(loggingMessage)
	assert.Equal(t, err, nil)

	sel := &nbFrame{payload: loggingData,
		err:        nil,
		methodInfo: newMethodDetails("/volta.VolthaService/UpdateLogLevel")}

	backend, connection := router.Route(sel)

	assert.Equal(t, sel.err, nil)
	assert.NotEqual(t, backend, nil)
	assert.Equal(t, backend.name, "ro_vcore0")
	assert.NotEqual(t, connection, nil)
	assert.Equal(t, connection.name, "ro_vcore01")
}
