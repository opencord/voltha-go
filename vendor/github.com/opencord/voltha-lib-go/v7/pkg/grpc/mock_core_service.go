/*
* Copyright 2021-2024 Open Networking Foundation (ONF) and the ONF Contributors

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
	"fmt"
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/health"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

// MockCoreServiceHandler implements the methods in the core service
type MockCoreServiceHandler struct {
	exitChannel chan struct{}
}

func NewMockCoreServiceHandler() *MockCoreServiceHandler {
	return &MockCoreServiceHandler{exitChannel: make(chan struct{})}
}

func (handler *MockCoreServiceHandler) Start() {
	logger.Debug(context.Background(), "starting-mock-core-service")
}

func (handler *MockCoreServiceHandler) Stop() {
	logger.Debug(context.Background(), "stopping-mock-core-service")
	close(handler.exitChannel)
}

func (handler *MockCoreServiceHandler) RegisterAdapter(ctx context.Context, reg *ca.AdapterRegistration) (*empty.Empty, error) {
	//logger.Debugw(ctx, "registration-received", log.Fields{"input": reg})
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DeviceUpdate(context.Context, *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) PortCreated(context.Context, *voltha.Port) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) PortsStateUpdate(context.Context, *ca.PortStateFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DeleteAllPorts(context.Context, *common.ID) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetDevicePort(context.Context, *ca.PortFilter) (*voltha.Port, error) {
	return &voltha.Port{}, nil
}

func (handler *MockCoreServiceHandler) ListDevicePorts(context.Context, *common.ID) (*voltha.Ports, error) {
	return &voltha.Ports{}, nil
}

func (handler *MockCoreServiceHandler) DeviceStateUpdate(context.Context, *ca.DeviceStateFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DevicePMConfigUpdate(context.Context, *voltha.PmConfigs) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) ChildDeviceDetected(context.Context, *ca.DeviceDiscovery) (*voltha.Device, error) {
	return &voltha.Device{}, nil
}

func (handler *MockCoreServiceHandler) ChildDevicesLost(context.Context, *common.ID) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) ChildDevicesDetected(context.Context, *common.ID) (*empty.Empty, error) {
	time.Sleep(50 * time.Millisecond)
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetDevice(ctx context.Context, id *common.ID) (*voltha.Device, error) {
	time.Sleep(50 * time.Millisecond)
	vlan, _ := strconv.Atoi(id.Id)
	return &voltha.Device{
		Id:   id.Id,
		Type: "test-1234",
		Vlan: uint32(vlan),
	}, nil
}

func (handler *MockCoreServiceHandler) GetChildDevice(context.Context, *ca.ChildDeviceFilter) (*voltha.Device, error) {
	return nil, nil
}

func (handler *MockCoreServiceHandler) GetChildDevices(context.Context, *common.ID) (*voltha.Devices, error) {
	return &voltha.Devices{}, nil
}

func (handler *MockCoreServiceHandler) SendPacketIn(context.Context, *ca.PacketIn) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DeviceReasonUpdate(context.Context, *ca.DeviceReason) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) PortStateUpdate(context.Context, *ca.PortState) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// Additional API found in the Core - unused?
func (handler *MockCoreServiceHandler) ReconcileChildDevices(context.Context, *common.ID) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetChildDeviceWithProxyAddress(context.Context, *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	return &voltha.Device{}, nil
}

func (handler *MockCoreServiceHandler) GetPorts(context.Context, *ca.PortFilter) (*voltha.Ports, error) {
	return &voltha.Ports{}, nil
}

func (handler *MockCoreServiceHandler) ChildrenStateUpdate(context.Context, *ca.DeviceStateFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) UpdateImageDownload(context.Context, *voltha.ImageDownload) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetHealthStatus(stream core_service.CoreService_GetHealthStatusServer) error {
	logger.Debugw(context.Background(), "keep-alive-connection", log.Fields{"stream": stream})
	if stream == nil {
		return fmt.Errorf("stream-is-nil %v", stream)
	}
	var err error
	var remoteClient *common.Connection
	var tempClient *common.Connection
	ctx := context.Background()
loop:
	for {
		tempClient, err = stream.Recv()
		if err != nil {
			logger.Warnw(ctx, "received-stream-error", log.Fields{"remote-client": remoteClient, "error": err})
			break loop
		}
		// Send a response back
		err = stream.Send(&health.HealthStatus{State: health.HealthStatus_HEALTHY})
		if err != nil {
			logger.Warnw(ctx, "sending-stream-error", log.Fields{"remote-client": remoteClient, "error": err})
			break loop
		}

		remoteClient = tempClient
		logger.Debugw(ctx, "received-keep-alive", log.Fields{"remote-client": remoteClient})
		select {
		case <-stream.Context().Done():
			logger.Infow(ctx, "stream-keep-alive-context-done", log.Fields{"remote-client": remoteClient, "error": stream.Context().Err()})
			break loop
		case <-handler.exitChannel:
			logger.Warnw(ctx, "received-stop", log.Fields{"remote-client": remoteClient})
			break loop
		default:
		}
	}
	logger.Errorw(context.Background(), "connection-down", log.Fields{"remote-client": remoteClient, "error": err})
	return err
}
