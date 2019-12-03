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
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

// DefaultAPIHandler represent default API handler
type DefaultAPIHandler struct {
}

func init() {
	_, err := log.AddPackage(log.JSON, log.WarnLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

// NewDefaultAPIHandler creates default API handler instance
func NewDefaultAPIHandler() *DefaultAPIHandler {
	handler := &DefaultAPIHandler{}
	return handler
}

// UpdateLogLevel updates log level
func (handler *DefaultAPIHandler) UpdateLogLevel(ctx context.Context, logging *voltha.Logging) (*empty.Empty, error) {
	log.Debugw("UpdateLogLevel-request", log.Fields{"newloglevel": logging.Level, "intval": int(logging.Level)})
	return nil, errors.New("UnImplemented")
}

// GetMembership returns membership
func (handler *DefaultAPIHandler) GetMembership(ctx context.Context, empty *empty.Empty) (*voltha.Membership, error) {
	log.Debug("GetMembership-request")
	return nil, errors.New("UnImplemented")
}

// UpdateMembership updates membership
func (handler *DefaultAPIHandler) UpdateMembership(ctx context.Context, membership *voltha.Membership) (*empty.Empty, error) {
	log.Debugw("UpdateMembership-request", log.Fields{"membership": membership})
	return nil, errors.New("UnImplemented")
}

// GetVoltha returns voltha details
func (handler *DefaultAPIHandler) GetVoltha(ctx context.Context, empty *empty.Empty) (*voltha.Voltha, error) {
	log.Debug("GetVoltha-request")
	return nil, errors.New("UnImplemented")
}

// ListCoreInstances returns core instances
func (handler *DefaultAPIHandler) ListCoreInstances(ctx context.Context, empty *empty.Empty) (*voltha.CoreInstances, error) {
	log.Debug("ListCoreInstances-request")
	return nil, errors.New("UnImplemented")
}

// GetCoreInstance returns core instance
func (handler *DefaultAPIHandler) GetCoreInstance(ctx context.Context, id *voltha.ID) (*voltha.CoreInstance, error) {
	log.Debugw("GetCoreInstance-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListAdapters returns core adapters
func (handler *DefaultAPIHandler) ListAdapters(ctx context.Context, empty *empty.Empty) (*voltha.Adapters, error) {
	log.Debug("ListAdapters-request")
	return nil, errors.New("UnImplemented")
}

// ListLogicalDevices returns all logical devices
func (handler *DefaultAPIHandler) ListLogicalDevices(ctx context.Context, empty *empty.Empty) (*voltha.LogicalDevices, error) {
	log.Debug("ListLogicalDevices-request")
	return nil, errors.New("UnImplemented")
}

// GetLogicalDevice returns logical device
func (handler *DefaultAPIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	log.Debugw("GetLogicalDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListLogicalDevicePorts returns logical device ports
func (handler *DefaultAPIHandler) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	log.Debugw("ListLogicalDevicePorts-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// GetLogicalDevicePort returns logical device port
func (handler *DefaultAPIHandler) GetLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	log.Debugw("GetLogicalDevicePort-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// EnableLogicalDevicePort enables logical device port
func (handler *DefaultAPIHandler) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("EnableLogicalDevicePort-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// DisableLogicalDevicePort -disables logical device port
func (handler *DefaultAPIHandler) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("DisableLogicalDevicePort-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListLogicalDeviceFlows returns logical device flows
func (handler *DefaultAPIHandler) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	log.Debugw("ListLogicalDeviceFlows-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// UpdateLogicalDeviceFlowTable updates logical device flow table
func (handler *DefaultAPIHandler) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowTable-request", log.Fields{"flow": *flow})
	return nil, errors.New("UnImplemented")
}

// UpdateLogicalDeviceFlowGroupTable updates logical device flow group table
func (handler *DefaultAPIHandler) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowGroupTable-request", log.Fields{"flow": *flow})
	return nil, errors.New("UnImplemented")
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (handler *DefaultAPIHandler) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	log.Debugw("ListLogicalDeviceFlowGroups-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListDevices returns devices
func (handler *DefaultAPIHandler) ListDevices(ctx context.Context, empty *empty.Empty) (*voltha.Devices, error) {
	log.Debug("ListDevices-request")
	return nil, errors.New("UnImplemented")
}

// ListDeviceIDs returns device ids
func (handler *DefaultAPIHandler) ListDeviceIDs(ctx context.Context, empty *empty.Empty) (*voltha.IDs, error) {
	log.Debug("ListDeviceIDs-request")
	return nil, errors.New("UnImplemented")
}

// ReconcileDevices reconciles devices
func (handler *DefaultAPIHandler) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error) {
	if ids != nil {
		log.Debugw("ReconcileDevices-request", log.Fields{"length": len(ids.Items)})
		return nil, errors.New("UnImplemented")
	}
	return nil, errors.New("ids-null")
}

// GetDevice returns device
func (handler *DefaultAPIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	log.Debugw("GetDevice-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

// CreateDevice creates device
func (handler *DefaultAPIHandler) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	log.Debugw("CreateDevice-request", log.Fields{"device": *device})
	return nil, errors.New("UnImplemented")
}

// EnableDevice enables device
func (handler *DefaultAPIHandler) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("EnableDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// DisableDevice disables device
func (handler *DefaultAPIHandler) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("DisableDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// RebootDevice reboots device
func (handler *DefaultAPIHandler) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("RebootDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// DeleteDevice deletes device
func (handler *DefaultAPIHandler) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("DeleteDevice-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// DownloadImage downloads image
func (handler *DefaultAPIHandler) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("DownloadImage-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

// GetImageDownloadStatus returns status of image download
func (handler *DefaultAPIHandler) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownloadStatus-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

// GetImageDownload returns image download
func (handler *DefaultAPIHandler) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("getdevice-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

// ListImageDownloads returns image downloads
func (handler *DefaultAPIHandler) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// CancelImageDownload cancels image download
func (handler *DefaultAPIHandler) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("CancelImageDownload-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

// ActivateImageUpdate activates image update
func (handler *DefaultAPIHandler) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("ActivateImageUpdate-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

// RevertImageUpdate reverts image update
func (handler *DefaultAPIHandler) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("RevertImageUpdate-request", log.Fields{"img": *img})
	return nil, errors.New("UnImplemented")
}

// ListDevicePorts returns device ports
func (handler *DefaultAPIHandler) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	log.Debugw("ListDevicePorts-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListDevicePmConfigs returns device pm configs
func (handler *DefaultAPIHandler) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// UpdateDevicePmConfigs updates device pm configs
func (handler *DefaultAPIHandler) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	log.Debugw("UpdateDevicePmConfigs-request", log.Fields{"configs": *configs})
	return nil, errors.New("UnImplemented")
}

// ListDeviceFlows returns device flows
func (handler *DefaultAPIHandler) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	log.Debugw("ListDeviceFlows-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListDeviceFlowGroups returns device flow groups
func (handler *DefaultAPIHandler) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	log.Debugw("ListDeviceFlowGroups-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListDeviceTypes returns device types
func (handler *DefaultAPIHandler) ListDeviceTypes(ctx context.Context, empty *empty.Empty) (*voltha.DeviceTypes, error) {
	log.Debug("ListDeviceTypes-request")
	return nil, errors.New("UnImplemented")
}

// GetDeviceType returns device type
func (handler *DefaultAPIHandler) GetDeviceType(ctx context.Context, id *voltha.ID) (*voltha.DeviceType, error) {
	log.Debugw("GetDeviceType-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// ListDeviceGroups returns device groups
func (handler *DefaultAPIHandler) ListDeviceGroups(ctx context.Context, empty *empty.Empty) (*voltha.DeviceGroups, error) {
	log.Debug("ListDeviceGroups-request")
	return nil, errors.New("UnImplemented")
}

// GetDeviceGroup returns device group
func (handler *DefaultAPIHandler) GetDeviceGroup(ctx context.Context, id *voltha.ID) (*voltha.DeviceGroup, error) {
	log.Debugw("GetDeviceGroup-request", log.Fields{"id": *id})
	return nil, errors.New("UnImplemented")
}

// CreateEventFilter creates event filter
func (handler *DefaultAPIHandler) CreateEventFilter(ctx context.Context, filter *voltha.EventFilter) (*voltha.EventFilter, error) {
	log.Debugw("CreateEventFilter-request", log.Fields{"filter": *filter})
	return nil, errors.New("UnImplemented")
}

// GetEventFilter returns event filter
func (handler *DefaultAPIHandler) GetEventFilter(ctx context.Context, id *voltha.ID) (*voltha.EventFilter, error) {
	log.Debugw("GetEventFilter-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

// UpdateEventFilter updates event filter
func (handler *DefaultAPIHandler) UpdateEventFilter(ctx context.Context, filter *voltha.EventFilter) (*voltha.EventFilter, error) {
	log.Debugw("UpdateEventFilter-request", log.Fields{"filter": *filter})
	return nil, errors.New("UnImplemented")
}

// DeleteEventFilter deletes event filter
func (handler *DefaultAPIHandler) DeleteEventFilter(ctx context.Context, filterInfo *voltha.EventFilter) (*empty.Empty, error) {
	log.Debugw("DeleteEventFilter-request", log.Fields{"filter-details": *filterInfo})
	return nil, errors.New("UnImplemented")
}

// ListEventFilters returns event filters
func (handler *DefaultAPIHandler) ListEventFilters(ctx context.Context, empty *empty.Empty) (*voltha.EventFilters, error) {
	log.Debug("ListEventFilters-request")
	return nil, errors.New("UnImplemented")
}

// GetImages returns images
func (handler *DefaultAPIHandler) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	log.Debugw("GetImages-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

// SelfTest requests self test
func (handler *DefaultAPIHandler) SelfTest(ctx context.Context, id *voltha.ID) (*voltha.SelfTestResponse, error) {
	log.Debugw("SelfTest-request", log.Fields{"id": id})
	return nil, errors.New("UnImplemented")
}

// StreamPacketsOut sends packet to adapter
func (handler *DefaultAPIHandler) StreamPacketsOut(packetsOut voltha.VolthaService_StreamPacketsOutServer) error {
	log.Debugw("StreamPacketsOut-request", log.Fields{"packetsOut": packetsOut})
	return errors.New("UnImplemented")
}

// ReceivePacketsIn receives packets from adapter
func (handler *DefaultAPIHandler) ReceivePacketsIn(
	empty *empty.Empty,
	packetsIn voltha.VolthaService_ReceivePacketsInServer,
) error {
	log.Debugw("ReceivePacketsIn-request", log.Fields{"packetsIn": packetsIn})
	return errors.New("UnImplemented")
}

// ReceiveChangeEvents receives change events
func (handler *DefaultAPIHandler) ReceiveChangeEvents(
	empty *empty.Empty,
	changeEvents voltha.VolthaService_ReceiveChangeEventsServer,
) error {
	log.Debugw("ReceiveChangeEvents-request", log.Fields{"changeEvents": changeEvents})
	return errors.New("UnImplemented")
}

// Subscribe requests for subscribe
func (handler *DefaultAPIHandler) Subscribe(
	ctx context.Context,
	ofAgent *voltha.OfAgentSubscriber,
) (*voltha.OfAgentSubscriber, error) {
	log.Debugw("Subscribe-request", log.Fields{"ofAgent": ofAgent})
	return nil, errors.New("UnImplemented")
}

// UpdateLogicalDeviceMeterTable updates logical device meter table
func (handler *DefaultAPIHandler) UpdateLogicalDeviceMeterTable(ctx context.Context, mod *openflow_13.MeterModUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceMeterTable-request", log.Fields{"meter": mod})
	return nil, errors.New("UnImplemented")
}

// ListLogicalDeviceMeters returns logical device meters
func (handler *DefaultAPIHandler) ListLogicalDeviceMeters(ctx context.Context, id *voltha.ID) (*openflow_13.Meters, error) {
	log.Debugw("ListLogicalDeviceMeters-unimplemented", log.Fields{"id": id})
	return nil, nil
}

func (handler *DefaultAPIHandler) StartOmciTestAction(ctx context.Context, id *voltha.ID) (*voltha.TestResponse, error) {
        log.Debugw("Test-request", log.Fields{"id": id})
        return nil, errors.New("UnImplemented")
}
