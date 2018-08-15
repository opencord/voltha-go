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
package grpc

import (
	"context"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"os"
	"testing"
)

var conn *grpc.ClientConn
var stub voltha.VolthaServiceClient
var testMode string

func setup() {
	var err error
	if _, err = log.SetLogger(log.JSON, 3, log.Fields{"instanceId": "testing"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	conn, err = grpc.Dial("localhost:50057", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %s", err)
	}

	stub = voltha.NewVolthaServiceClient(conn)
	testMode = common.TestModeKeys_api_test.String()
}

func TestGetDevice(t *testing.T) {
	var id common.ID
	id.Id = "anyid"
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetDevice(ctx, &id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())

}

func TestUpdateLogLevelError(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	level := voltha.Logging{Level: common.LogLevel_ERROR}
	response, err := stub.UpdateLogLevel(ctx, &level)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestGetVoltha(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetVoltha(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestUpdateLogLevelDebug(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	level := voltha.Logging{Level: common.LogLevel_DEBUG}
	response, err := stub.UpdateLogLevel(ctx, &level)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}


func TestGetCoreInstance(t *testing.T) {
	id := &voltha.ID{Id: "getCoreInstance"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetCoreInstance(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestGetLogicalDevice(t *testing.T) {
	id := &voltha.ID{Id: "getLogicalDevice"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetLogicalDevice(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestGetLogicalDevicePort(t *testing.T) {
	id := &voltha.LogicalPortId{Id: "GetLogicalDevicePort"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetLogicalDevicePort(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDevicePorts(t *testing.T) {
	id := &voltha.ID{Id: "listLogicalDevicePorts"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDevicePorts(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDeviceFlows(t *testing.T) {
	id := &voltha.ID{Id: "ListLogicalDeviceFlows"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDeviceFlows(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDeviceFlowGroups(t *testing.T) {
	id := &voltha.ID{Id: "ListLogicalDeviceFlowGroups"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDeviceFlowGroups(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListDevices(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListDevices(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListAdapters(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListAdapters(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDevices(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDevices(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListCoreInstances(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListCoreInstances(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestCreateDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	device := &voltha.Device{Id: "newdevice"}
	response, err := stub.CreateDevice(ctx, device)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &voltha.Device{Id: "newdevice"}, response)
	assert.Nil(t, err)
}

func TestEnableDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "enabledevice"}
	response, err := stub.EnableDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestDisableDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "DisableDevice"}
	response, err := stub.DisableDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestRebootDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "RebootDevice"}
	response, err := stub.RebootDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestDeleteDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "DeleteDevice"}
	response, err := stub.DeleteDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestEnableLogicalDevicePort(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.LogicalPortId{Id: "EnableLogicalDevicePort"}
	response, err := stub.EnableLogicalDevicePort(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestDisableLogicalDevicePort(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.LogicalPortId{Id: "DisableLogicalDevicePort"}
	response, err := stub.DisableLogicalDevicePort(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestUpdateLogicalDeviceFlowGroupTable(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	flow := &openflow_13.FlowGroupTableUpdate{Id: "UpdateLogicalDeviceFlowGroupTable"}
	response, err := stub.UpdateLogicalDeviceFlowGroupTable(ctx, flow)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestGetImageDownloadStatus(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	img := &voltha.ImageDownload{Id: "GetImageDownloadStatus"}
	response, err := stub.GetImageDownloadStatus(ctx, img)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

// TODO: complete the remaining tests

func shutdown() {
	conn.Close()
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
