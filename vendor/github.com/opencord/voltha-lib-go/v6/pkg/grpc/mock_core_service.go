package grpc

import (
	"context"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-protos/v4/go/common"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"strconv"
	"time"
)

//MockCoreServiceHandler implements the methods in the core service
type MockCoreServiceHandler struct{}

func (handler *MockCoreServiceHandler) RegisterAdapter(ctx context.Context, reg *ic.AdapterRegistration) (*empty.Empty, error) {
	//logger.Debugw(ctx, "registration-received", log.Fields{"input": reg})
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DeviceUpdate(context.Context, *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) PortCreated(context.Context, *voltha.Port) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) PortsStateUpdate(context.Context, *ic.PortStateFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DeleteAllPorts(context.Context, *common.ID) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetDevicePort(context.Context, *ic.PortFilter) (*voltha.Port, error) {
	return &voltha.Port{}, nil
}

func (handler *MockCoreServiceHandler) ListDevicePorts(context.Context, *common.ID) (*voltha.Ports, error) {
	return &voltha.Ports{}, nil
}

func (handler *MockCoreServiceHandler) DeviceStateUpdate(context.Context, *ic.DeviceStateFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DevicePMConfigUpdate(context.Context, *voltha.PmConfigs) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) ChildDeviceDetected(context.Context, *ic.DeviceDiscovery) (*voltha.Device, error) {
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

func (handler *MockCoreServiceHandler) GetChildDevice(context.Context, *ic.ChildDeviceFilter) (*voltha.Device, error) {
	return nil, nil
}

func (handler *MockCoreServiceHandler) GetChildDevices(context.Context, *common.ID) (*voltha.Devices, error) {
	return &voltha.Devices{}, nil
}

func (handler *MockCoreServiceHandler) SendPacketIn(context.Context, *ic.PacketIn) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) DeviceReasonUpdate(context.Context, *ic.DeviceReason) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) PortStateUpdate(context.Context, *ic.PortState) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// Additional API found in the Core - unused?
func (handler *MockCoreServiceHandler) ReconcileChildDevices(context.Context, *common.ID) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetChildDeviceWithProxyAddress(context.Context, *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	return &voltha.Device{}, nil
}

func (handler *MockCoreServiceHandler) GetPorts(context.Context, *ic.PortFilter) (*voltha.Ports, error) {
	return &voltha.Ports{}, nil
}

func (handler *MockCoreServiceHandler) ChildrenStateUpdate(context.Context, *ic.DeviceStateFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) UpdateImageDownload(context.Context, *voltha.ImageDownload) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (handler *MockCoreServiceHandler) GetHealthStatus(ctx context.Context, empty *empty.Empty) (*voltha.HealthStatus, error) {
	return &voltha.HealthStatus{State: voltha.HealthStatus_HEALTHY}, nil
}
