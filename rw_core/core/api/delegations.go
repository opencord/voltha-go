/*
 * Copyright 2018-2023 Open Networking Foundation (ONF) and the ONF Contributors
 * Licensed under the Apache License, Version 2.0 (the "License")
 */

package api

//go:generate go run generate_delegations.go

// This file contains delegation methods to resolve ambiguities between
// embedded UnimplementedVolthaServiceServer/UnimplementedCoreServiceServer
// and the device.Manager that has the actual implementations.
//
// When a method exists in both the Unimplemented stub and the Manager,
// Go cannot determine which one to use, so we explicitly delegate to Manager.
//
// To regenerate this file after proto updates, run: go generate ./rw_core/core/api

import (
	"context"

	"github.com/opencord/voltha-protos/v5/go/common"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/extension"
	"github.com/opencord/voltha-protos/v5/go/omci"
	"github.com/opencord/voltha-protos/v5/go/openflow_13"
	voip_system_profile "github.com/opencord/voltha-protos/v5/go/voip_system_profile"
	voip_user_profile "github.com/opencord/voltha-protos/v5/go/voip_user_profile"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/protobuf/types/known/emptypb"
)

// All methods below delegate to handler.Manager, handler.LogicalManager, or handler.adapterManager
// to resolve ambiguity with embedded UnimplementedVolthaServiceServer/UnimplementedCoreServiceServer

func (handler *APIHandler) AbortImageUpgradeToDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return handler.Manager.AbortImageUpgradeToDevice(ctx, request)
}

func (handler *APIHandler) ActivateImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return handler.Manager.ActivateImage(ctx, request)
}

func (handler *APIHandler) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	return handler.Manager.ActivateImageUpdate(ctx, img)
}

func (handler *APIHandler) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	return handler.Manager.CancelImageDownload(ctx, img)
}

func (handler *APIHandler) ChildDeviceDetected(ctx context.Context, dd *ca.DeviceDiscovery) (*voltha.Device, error) {
	return handler.Manager.ChildDeviceDetected(ctx, dd)
}

func (handler *APIHandler) ChildDevicesDetected(ctx context.Context, parentDeviceID *common.ID) (*emptypb.Empty, error) {
	return handler.Manager.ChildDevicesDetected(ctx, parentDeviceID)
}

func (handler *APIHandler) ChildDevicesLost(ctx context.Context, parentID *common.ID) (*emptypb.Empty, error) {
	return handler.Manager.ChildDevicesLost(ctx, parentID)
}

func (handler *APIHandler) ChildrenStateUpdate(ctx context.Context, ds *ca.DeviceStateFilter) (*emptypb.Empty, error) {
	return handler.Manager.ChildrenStateUpdate(ctx, ds)
}

func (handler *APIHandler) CommitImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return handler.Manager.CommitImage(ctx, request)
}

func (handler *APIHandler) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	return handler.Manager.CreateDevice(ctx, device)
}

func (handler *APIHandler) DeleteAllPorts(ctx context.Context, deviceID *common.ID) (*emptypb.Empty, error) {
	return handler.Manager.DeleteAllPorts(ctx, deviceID)
}

func (handler *APIHandler) DeleteDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.DeleteDevice(ctx, id)
}

func (handler *APIHandler) DeleteVoipSystemProfile(ctx context.Context, key *common.Key) (*emptypb.Empty, error) {
	return handler.Manager.DeleteVoipSystemProfile(ctx, key)
}

func (handler *APIHandler) DeleteVoipUserProfile(ctx context.Context, key *common.Key) (*emptypb.Empty, error) {
	return handler.Manager.DeleteVoipUserProfile(ctx, key)
}

func (handler *APIHandler) DevicePMConfigUpdate(ctx context.Context, pc *voltha.PmConfigs) (*emptypb.Empty, error) {
	return handler.Manager.DevicePMConfigUpdate(ctx, pc)
}

func (handler *APIHandler) DeviceReasonUpdate(ctx context.Context, dr *ca.DeviceReason) (*emptypb.Empty, error) {
	return handler.Manager.DeviceReasonUpdate(ctx, dr)
}

func (handler *APIHandler) DeviceStateUpdate(ctx context.Context, ds *ca.DeviceStateFilter) (*emptypb.Empty, error) {
	return handler.Manager.DeviceStateUpdate(ctx, ds)
}

func (handler *APIHandler) DeviceUpdate(ctx context.Context, device *voltha.Device) (*emptypb.Empty, error) {
	return handler.Manager.DeviceUpdate(ctx, device)
}

func (handler *APIHandler) DisableDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.DisableDevice(ctx, id)
}

func (handler *APIHandler) DisableOnuDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.DisableOnuDevice(ctx, id)
}

func (handler *APIHandler) DisableOnuSerialNumber(ctx context.Context, device *voltha.OnuSerialNumberOnOLTPon) (*emptypb.Empty, error) {
	return handler.Manager.DisableOnuSerialNumber(ctx, device)
}

func (handler *APIHandler) DisablePort(ctx context.Context, port *voltha.Port) (*emptypb.Empty, error) {
	return handler.Manager.DisablePort(ctx, port)
}

func (handler *APIHandler) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	return handler.Manager.DownloadImage(ctx, img)
}

func (handler *APIHandler) DownloadImageToDevice(ctx context.Context, request *voltha.DeviceImageDownloadRequest) (*voltha.DeviceImageResponse, error) {
	return handler.Manager.DownloadImageToDevice(ctx, request)
}

func (handler *APIHandler) EnableDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.EnableDevice(ctx, id)
}

func (handler *APIHandler) EnableOnuDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.EnableOnuDevice(ctx, id)
}

func (handler *APIHandler) EnableOnuSerialNumber(ctx context.Context, device *voltha.OnuSerialNumberOnOLTPon) (*emptypb.Empty, error) {
	return handler.Manager.EnableOnuSerialNumber(ctx, device)
}

func (handler *APIHandler) EnablePort(ctx context.Context, port *voltha.Port) (*emptypb.Empty, error) {
	return handler.Manager.EnablePort(ctx, port)
}

func (handler *APIHandler) ForceDeleteDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.ForceDeleteDevice(ctx, id)
}

func (handler *APIHandler) GetChildDevice(ctx context.Context, df *ca.ChildDeviceFilter) (*voltha.Device, error) {
	return handler.Manager.GetChildDevice(ctx, df)
}

func (handler *APIHandler) GetChildDeviceWithProxyAddress(ctx context.Context, proxyAddress *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	return handler.Manager.GetChildDeviceWithProxyAddress(ctx, proxyAddress)
}

func (handler *APIHandler) GetChildDevices(ctx context.Context, parentDeviceID *common.ID) (*voltha.Devices, error) {
	return handler.Manager.GetChildDevices(ctx, parentDeviceID)
}

func (handler *APIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	return handler.Manager.GetDevice(ctx, id)
}

func (handler *APIHandler) GetDevicePort(ctx context.Context, pf *ca.PortFilter) (*voltha.Port, error) {
	return handler.Manager.GetDevicePort(ctx, pf)
}

func (handler *APIHandler) GetExtValue(ctx context.Context, value *extension.ValueSpecifier) (*extension.ReturnValues, error) {
	return handler.Manager.GetExtValue(ctx, value)
}

func (handler *APIHandler) GetHealthStatus(stream core_service.CoreService_GetHealthStatusServer) error {
	return handler.Manager.GetHealthStatus(stream)
}

func (handler *APIHandler) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return handler.Manager.GetImageDownload(ctx, img)
}

func (handler *APIHandler) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return handler.Manager.GetImageDownloadStatus(ctx, img)
}

func (handler *APIHandler) GetImageStatus(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return handler.Manager.GetImageStatus(ctx, request)
}

func (handler *APIHandler) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	return handler.Manager.GetImages(ctx, id)
}

func (handler *APIHandler) GetOnuImages(ctx context.Context, id *common.ID) (*voltha.OnuImages, error) {
	return handler.Manager.GetOnuImages(ctx, id)
}

func (handler *APIHandler) GetPorts(ctx context.Context, pf *ca.PortFilter) (*voltha.Ports, error) {
	return handler.Manager.GetPorts(ctx, pf)
}

func (handler *APIHandler) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	return handler.Manager.ListDeviceFlowGroups(ctx, id)
}

func (handler *APIHandler) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	return handler.Manager.ListDeviceFlows(ctx, id)
}

func (handler *APIHandler) ListDeviceIds(ctx context.Context, arg1 *emptypb.Empty) (*voltha.IDs, error) {
	return handler.Manager.ListDeviceIds(ctx, arg1)
}

func (handler *APIHandler) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	return handler.Manager.ListDevicePmConfigs(ctx, id)
}

func (handler *APIHandler) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	return handler.Manager.ListDevicePorts(ctx, id)
}

func (handler *APIHandler) ListDevices(ctx context.Context, arg1 *emptypb.Empty) (*voltha.Devices, error) {
	return handler.Manager.ListDevices(ctx, arg1)
}

func (handler *APIHandler) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	return handler.Manager.ListImageDownloads(ctx, id)
}

func (handler *APIHandler) PortCreated(ctx context.Context, port *voltha.Port) (*emptypb.Empty, error) {
	return handler.Manager.PortCreated(ctx, port)
}

func (handler *APIHandler) PortStateUpdate(ctx context.Context, ps *ca.PortState) (*emptypb.Empty, error) {
	return handler.Manager.PortStateUpdate(ctx, ps)
}

func (handler *APIHandler) PortsStateUpdate(ctx context.Context, ps *ca.PortStateFilter) (*emptypb.Empty, error) {
	return handler.Manager.PortsStateUpdate(ctx, ps)
}

func (handler *APIHandler) PutVoipSystemProfile(ctx context.Context, voipSystemProfileRequest *voip_system_profile.VoipSystemProfileRequest) (*emptypb.Empty, error) {
	return handler.Manager.PutVoipSystemProfile(ctx, voipSystemProfileRequest)
}

func (handler *APIHandler) PutVoipUserProfile(ctx context.Context, voipUserProfileRequest *voip_user_profile.VoipUserProfileRequest) (*emptypb.Empty, error) {
	return handler.Manager.PutVoipUserProfile(ctx, voipUserProfileRequest)
}

func (handler *APIHandler) RebootDevice(ctx context.Context, id *voltha.ID) (*emptypb.Empty, error) {
	return handler.Manager.RebootDevice(ctx, id)
}

func (handler *APIHandler) ReconcileChildDevices(ctx context.Context, parentDeviceID *common.ID) (*emptypb.Empty, error) {
	return handler.Manager.ReconcileChildDevices(ctx, parentDeviceID)
}

func (handler *APIHandler) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*emptypb.Empty, error) {
	return handler.Manager.ReconcileDevices(ctx, ids)
}

func (handler *APIHandler) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	return handler.Manager.RevertImageUpdate(ctx, img)
}

func (handler *APIHandler) SendPacketIn(ctx context.Context, pi *ca.PacketIn) (*emptypb.Empty, error) {
	return handler.Manager.SendPacketIn(ctx, pi)
}

func (handler *APIHandler) SetExtValue(ctx context.Context, value *extension.ValueSet) (*emptypb.Empty, error) {
	return handler.Manager.SetExtValue(ctx, value)
}

func (handler *APIHandler) SimulateAlarm(ctx context.Context, simulateReq *voltha.SimulateAlarmRequest) (*common.OperationResp, error) {
	return handler.Manager.SimulateAlarm(ctx, simulateReq)
}

func (handler *APIHandler) StartOmciTestAction(ctx context.Context, request *omci.OmciTestRequest) (*omci.TestResponse, error) {
	return handler.Manager.StartOmciTestAction(ctx, request)
}

func (handler *APIHandler) UpdateDevice(ctx context.Context, config *voltha.UpdateDevice) (*emptypb.Empty, error) {
	return handler.Manager.UpdateDevice(ctx, config)
}

func (handler *APIHandler) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*emptypb.Empty, error) {
	return handler.Manager.UpdateDevicePmConfigs(ctx, configs)
}

func (handler *APIHandler) UpdateImageDownload(ctx context.Context, img *voltha.ImageDownload) (*emptypb.Empty, error) {
	return handler.Manager.UpdateImageDownload(ctx, img)
}

// LogicalManager delegations to resolve ambiguity
func (handler *APIHandler) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*emptypb.Empty, error) {
	return handler.LogicalManager.DisableLogicalDevicePort(ctx, id)
}

func (handler *APIHandler) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*emptypb.Empty, error) {
	return handler.LogicalManager.EnableLogicalDevicePort(ctx, id)
}

func (handler *APIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	return handler.LogicalManager.GetLogicalDevice(ctx, id)
}

func (handler *APIHandler) GetLogicalDevicePort(ctx context.Context, lPortID *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	return handler.LogicalManager.GetLogicalDevicePort(ctx, lPortID)
}

func (handler *APIHandler) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	return handler.LogicalManager.ListLogicalDeviceFlowGroups(ctx, id)
}

func (handler *APIHandler) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	return handler.LogicalManager.ListLogicalDeviceFlows(ctx, id)
}

func (handler *APIHandler) ListLogicalDeviceMeters(ctx context.Context, id *voltha.ID) (*openflow_13.Meters, error) {
	return handler.LogicalManager.ListLogicalDeviceMeters(ctx, id)
}

func (handler *APIHandler) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	return handler.LogicalManager.ListLogicalDevicePorts(ctx, id)
}

func (handler *APIHandler) ListLogicalDevices(ctx context.Context, arg1 *emptypb.Empty) (*voltha.LogicalDevices, error) {
	return handler.LogicalManager.ListLogicalDevices(ctx, arg1)
}

func (handler *APIHandler) StreamPacketsOut(packets voltha.VolthaService_StreamPacketsOutServer) error {
	return handler.LogicalManager.StreamPacketsOut(packets)
}

func (handler *APIHandler) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*emptypb.Empty, error) {
	return handler.LogicalManager.UpdateLogicalDeviceFlowGroupTable(ctx, flow)
}

func (handler *APIHandler) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*emptypb.Empty, error) {
	return handler.LogicalManager.UpdateLogicalDeviceFlowTable(ctx, flow)
}

func (handler *APIHandler) UpdateLogicalDeviceMeterTable(ctx context.Context, meter *openflow_13.MeterModUpdate) (*emptypb.Empty, error) {
	return handler.LogicalManager.UpdateLogicalDeviceMeterTable(ctx, meter)
}

// adapter.Manager delegations to resolve ambiguity
func (handler *APIHandler) GetDeviceType(ctx context.Context, deviceType *common.ID) (*voltha.DeviceType, error) {
	return handler.adapterManager.GetDeviceType(ctx, deviceType)
}

func (handler *APIHandler) ListAdapters(ctx context.Context, arg1 *emptypb.Empty) (*voltha.Adapters, error) {
	return handler.adapterManager.ListAdapters(ctx, arg1)
}

func (handler *APIHandler) ListDeviceTypes(ctx context.Context, arg1 *emptypb.Empty) (*voltha.DeviceTypes, error) {
	return handler.adapterManager.ListDeviceTypes(ctx, arg1)
}

func (handler *APIHandler) RegisterAdapter(ctx context.Context, registration *ca.AdapterRegistration) (*emptypb.Empty, error) {
	return handler.adapterManager.RegisterAdapter(ctx, registration)
}
