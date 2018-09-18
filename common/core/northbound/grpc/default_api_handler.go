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
	"errors"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
)

type DefaultAPIHandler struct {
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func NewDefaultAPIHandler() *DefaultAPIHandler {
	handler := &DefaultAPIHandler{}
	return handler
}

func (handler *DefaultAPIHandler) UpdateLogLevel(ctx context.Context, logging *voltha.Logging) (*empty.Empty, error) {
	log.Debugw("UpdateLogLevel-request", log.Fields{"newloglevel": logging.Level, "intval": int(logging.Level)})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetVoltha(ctx context.Context, empty *empty.Empty) (*voltha.Voltha, error) {
	log.Debug("GetVoltha-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListCoreInstances(ctx context.Context, empty *empty.Empty) (*voltha.CoreInstances, error) {
	log.Debug("ListCoreInstances-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetCoreInstance(ctx context.Context, id *voltha.ID) (*voltha.CoreInstance, error) {
	log.Debugw("GetCoreInstance-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListAdapters(ctx context.Context, empty *empty.Empty) (*voltha.Adapters, error) {
	log.Debug("ListAdapters-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListLogicalDevices(ctx context.Context, empty *empty.Empty) (*voltha.LogicalDevices, error) {
	log.Debug("ListLogicalDevices-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	log.Debugw("GetLogicalDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	log.Debugw("ListLogicalDevicePorts-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	log.Debugw("GetLogicalDevicePort-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("EnableLogicalDevicePort-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("DisableLogicalDevicePort-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	log.Debugw("ListLogicalDeviceFlows-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowTable-request", log.Fields{"flow": *flow})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowGroupTable-request", log.Fields{"flow": *flow})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	log.Debugw("ListLogicalDeviceFlowGroups-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDevices(ctx context.Context, empty *empty.Empty) (*voltha.Devices, error) {
	log.Debug("ListDevices-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	log.Debugw("GetDevice-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	log.Debugw("CreateDevice-request", log.Fields{"device": *device})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("EnableDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("DisableDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("RebootDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("DeleteDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("DownloadImage-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownloadStatus-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("getdevice-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("CancelImageDownload-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("ActivateImageUpdate-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("RevertImageUpdate-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	log.Debugw("ListDevicePorts-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	log.Debugw("UpdateDevicePmConfigs-request", log.Fields{"configs": *configs})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	log.Debugw("ListDeviceFlows-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	log.Debugw("ListDeviceFlowGroups-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDeviceTypes(ctx context.Context, empty *empty.Empty) (*voltha.DeviceTypes, error) {
	log.Debug("ListDeviceTypes-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetDeviceType(ctx context.Context, id *voltha.ID) (*voltha.DeviceType, error) {
	log.Debugw("GetDeviceType-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListDeviceGroups(ctx context.Context, empty *empty.Empty) (*voltha.DeviceGroups, error) {
	log.Debug("ListDeviceGroups-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetDeviceGroup(ctx context.Context, id *voltha.ID) (*voltha.DeviceGroup, error) {
	log.Debugw("GetDeviceGroup-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) CreateAlarmFilter(ctx context.Context, filter *voltha.AlarmFilter) (*voltha.AlarmFilter, error) {
	log.Debugw("CreateAlarmFilter-request", log.Fields{"filter": *filter})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetAlarmFilter(ctx context.Context, id *voltha.ID) (*voltha.AlarmFilter, error) {
	log.Debugw("GetAlarmFilter-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) UpdateAlarmFilter(ctx context.Context, filter *voltha.AlarmFilter) (*voltha.AlarmFilter, error) {
	log.Debugw("UpdateAlarmFilter-request", log.Fields{"filter": *filter})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) DeleteAlarmFilter(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("DeleteAlarmFilter-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) ListAlarmFilters(ctx context.Context, empty *empty.Empty) (*voltha.AlarmFilters, error) {
	log.Debug("ListAlarmFilters-request")
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	log.Debugw("GetImages-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

func (handler *DefaultAPIHandler) SelfTest(ctx context.Context, id *voltha.ID) (*voltha.SelfTestResponse, error) {
	log.Debugw("SelfTest-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}
