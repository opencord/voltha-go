package coreIf

import (
	"context"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

type APIHandler interface {
	UpdateLogLevel(context.Context, *voltha.Logging) (*empty.Empty, error)
	GetMembership(context.Context, *empty.Empty) (*voltha.Membership, error)
	UpdateMembership(context.Context, *voltha.Membership) (*empty.Empty, error)
	GetVoltha(context.Context, *empty.Empty) (*voltha.Voltha, error)
	ListCoreInstances(context.Context, *empty.Empty) (*voltha.CoreInstances, error)
	GetCoreInstance(context.Context, *voltha.ID) (*voltha.CoreInstance, error)
	ListAdapters(context.Context, *empty.Empty) (*voltha.Adapters, error)
	ListLogicalDevices(context.Context, *empty.Empty) (*voltha.LogicalDevices, error)
	GetLogicalDevice(context.Context, *voltha.ID) (*voltha.LogicalDevice, error)
	ListLogicalDevicePorts(context.Context, *voltha.ID) (*voltha.LogicalPorts, error)
	GetLogicalDevicePort(context.Context, *voltha.LogicalPortId) (*voltha.LogicalPort, error)
	EnableLogicalDevicePort(context.Context, *voltha.LogicalPortId) (*empty.Empty, error)
	DisableLogicalDevicePort(context.Context, *voltha.LogicalPortId) (*empty.Empty, error)
	ListLogicalDeviceFlows(context.Context, *voltha.ID) (*openflow_13.Flows, error)
	UpdateLogicalDeviceFlowTable(context.Context, *openflow_13.FlowTableUpdate) (*empty.Empty, error)
	UpdateLogicalDeviceFlowGroupTable(context.Context, *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error)
	ListLogicalDeviceFlowGroups(context.Context, *voltha.ID) (*openflow_13.FlowGroups, error)
	ListDevices(context.Context, *empty.Empty) (*voltha.Devices, error)
	ListDeviceIds(context.Context, *empty.Empty) (*voltha.IDs, error)
	ReconcileDevices(context.Context, *voltha.IDs) (*empty.Empty, error)
	GetDevice(context.Context, *voltha.ID) (*voltha.Device, error)
	CreateDevice(context.Context, *voltha.Device) (*voltha.Device, error)
	EnableDevice(context.Context, *voltha.ID) (*empty.Empty, error)
	DisableDevice(context.Context, *voltha.ID) (*empty.Empty, error)
	RebootDevice(context.Context, *voltha.ID) (*empty.Empty, error)
	DeleteDevice(context.Context, *voltha.ID) (*empty.Empty, error)
	DownloadImage(context.Context, *voltha.ImageDownload) (*common.OperationResp, error)
	GetImageDownloadStatus(context.Context, *voltha.ImageDownload) (*voltha.ImageDownload, error)
	GetImageDownload(context.Context, *voltha.ImageDownload) (*voltha.ImageDownload, error)
	ListImageDownloads(context.Context, *voltha.ID) (*voltha.ImageDownloads, error)
	CancelImageDownload(context.Context, *voltha.ImageDownload) (*common.OperationResp, error)
	ActivateImageUpdate(context.Context, *voltha.ImageDownload) (*common.OperationResp, error)
	RevertImageUpdate(context.Context, *voltha.ImageDownload) (*common.OperationResp, error)
	ListDevicePorts(context.Context, *voltha.ID) (*voltha.Ports, error)
	ListDevicePmConfigs(context.Context, *voltha.ID) (*voltha.PmConfigs, error)
	UpdateDevicePmConfigs(context.Context, *voltha.PmConfigs) (*empty.Empty, error)
	ListDeviceFlows(context.Context, *voltha.ID) (*openflow_13.Flows, error)
	ListDeviceFlowGroups(context.Context, *voltha.ID) (*openflow_13.FlowGroups, error)
	ListDeviceTypes(context.Context, *empty.Empty) (*voltha.DeviceTypes, error)
	GetDeviceType(context.Context, *voltha.ID) (*voltha.DeviceType, error)
	ListDeviceGroups(context.Context, *empty.Empty) (*voltha.DeviceGroups, error)
	GetDeviceGroup(context.Context, *voltha.ID) (*voltha.DeviceGroup, error)
	CreateAlarmFilter(context.Context, *voltha.AlarmFilter) (*voltha.AlarmFilter, error)
	GetAlarmFilter(context.Context, *voltha.ID) (*voltha.AlarmFilter, error)
	UpdateAlarmFilter(context.Context, *voltha.AlarmFilter) (*voltha.AlarmFilter, error)
	DeleteAlarmFilter(context.Context, *voltha.ID) (*empty.Empty, error)
	ListAlarmFilters(context.Context, *empty.Empty) (*voltha.AlarmFilters, error)
	GetImages(context.Context, *voltha.ID) (*voltha.Images, error)
	SelfTest(context.Context, *voltha.ID) (*voltha.SelfTestResponse, error)
	StreamPacketsOut(packetsOut voltha.VolthaService_StreamPacketsOutServer) error
	ReceivePacketsIn(*empty.Empty, voltha.VolthaService_ReceivePacketsInServer) error
	ReceiveChangeEvents(*empty.Empty, voltha.VolthaService_ReceiveChangeEventsServer) error
	Subscribe(context.Context, *voltha.OfAgentSubscriber) (*voltha.OfAgentSubscriber, error)
	UpdateLogicalDeviceMeterTable(context.Context, *openflow_13.MeterModUpdate) (*empty.Empty, error)
	ListLogicalDeviceMeters(context.Context, *voltha.ID) (*openflow_13.Meters, error)
}
