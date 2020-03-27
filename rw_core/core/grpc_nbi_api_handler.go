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

package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	da "github.com/opencord/voltha-go/common/core/northbound/grpc"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/version"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/omci"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Image related constants
const (
	ImageDownload       = iota
	CancelImageDownload = iota
	ActivateImage       = iota
	RevertImage         = iota
)

// APIHandler represent attributes of API handler
type APIHandler struct {
	deviceMgr                 *DeviceManager
	logicalDeviceMgr          *LogicalDeviceManager
	adapterMgr                *AdapterManager
	packetInQueue             chan openflow_13.PacketIn
	changeEventQueue          chan openflow_13.ChangeEvent
	packetInQueueDone         chan bool
	changeEventQueueDone      chan bool
	coreInCompetingMode       bool
	longRunningRequestTimeout time.Duration
	defaultRequestTimeout     time.Duration
	da.DefaultAPIHandler
	core *Core
}

// NewAPIHandler creates API handler instance
func NewAPIHandler(core *Core) *APIHandler {
	handler := &APIHandler{
		deviceMgr:                 core.deviceMgr,
		logicalDeviceMgr:          core.logicalDeviceMgr,
		adapterMgr:                core.adapterMgr,
		coreInCompetingMode:       core.config.InCompetingMode,
		longRunningRequestTimeout: core.config.LongRunningRequestTimeout,
		defaultRequestTimeout:     core.config.DefaultRequestTimeout,
		packetInQueue:             make(chan openflow_13.PacketIn, 100),
		changeEventQueue:          make(chan openflow_13.ChangeEvent, 100),
		packetInQueueDone:         make(chan bool, 1),
		changeEventQueueDone:      make(chan bool, 1),
		core:                      core,
	}
	return handler
}

// isTestMode is a helper function to determine a function is invoked for testing only
func isTestMode(ctx context.Context) bool {
	md, _ := metadata.FromIncomingContext(ctx)
	_, exist := md[common.TestModeKeys_api_test.String()]
	return exist
}

// waitForNilResponseOnSuccess is a helper function to wait for a response on channel monitorCh where an nil
// response is expected in a successful scenario
func waitForNilResponseOnSuccess(ctx context.Context, ch chan interface{}) (*empty.Empty, error) {
	select {
	case res := <-ch:
		if res == nil {
			return &empty.Empty{}, nil
		} else if err, ok := res.(error); ok {
			return &empty.Empty{}, err
		} else {
			logger.Warnw("unexpected-return-type", log.Fields{"result": res})
			err = status.Errorf(codes.Internal, "%s", res)
			return &empty.Empty{}, err
		}
	case <-ctx.Done():
		logger.Debug("client-timeout")
		return nil, ctx.Err()
	}
}

// ListCoreInstances returns details on the running core containers
func (handler *APIHandler) ListCoreInstances(ctx context.Context, empty *empty.Empty) (*voltha.CoreInstances, error) {
	logger.Debug("ListCoreInstances")
	// TODO: unused stub
	return &voltha.CoreInstances{}, status.Errorf(codes.NotFound, "no-core-instances")
}

// GetCoreInstance returns the details of a specific core container
func (handler *APIHandler) GetCoreInstance(ctx context.Context, id *voltha.ID) (*voltha.CoreInstance, error) {
	logger.Debugw("GetCoreInstance", log.Fields{"id": id})
	//TODO: unused stub
	return &voltha.CoreInstance{}, status.Errorf(codes.NotFound, "core-instance-%s", id.Id)
}

// GetLogicalDevicePort returns logical device port details
func (handler *APIHandler) GetLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	logger.Debugw("GetLogicalDevicePort-request", log.Fields{"id": *id})

	return handler.logicalDeviceMgr.getLogicalPort(ctx, id)
}

// EnableLogicalDevicePort enables logical device port
func (handler *APIHandler) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	logger.Debugw("EnableLogicalDevicePort-request", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.enableLogicalPort(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// DisableLogicalDevicePort disables logical device port
func (handler *APIHandler) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	logger.Debugw("DisableLogicalDevicePort-request", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.disableLogicalPort(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// UpdateLogicalDeviceFlowTable updates logical device flow table
func (handler *APIHandler) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	logger.Debugw("UpdateLogicalDeviceFlowTable-request", log.Fields{"flow": flow, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.updateFlowTable(ctx, flow.Id, flow.FlowMod, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// UpdateLogicalDeviceFlowGroupTable updates logical device flow group table
func (handler *APIHandler) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	logger.Debugw("UpdateLogicalDeviceFlowGroupTable-request", log.Fields{"flow": flow, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.updateGroupTable(ctx, flow.Id, flow.GroupMod, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// GetDevice must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	logger.Debugw("GetDevice-request", log.Fields{"id": id})
	return handler.deviceMgr.GetDevice(ctx, id.Id)
}

// GetDevice must be implemented in the read-only containers - should it also be implemented here?

// ListDevices retrieves the latest devices from the data model
func (handler *APIHandler) ListDevices(ctx context.Context, empty *empty.Empty) (*voltha.Devices, error) {
	logger.Debug("ListDevices")
	devices, err := handler.deviceMgr.ListDevices(ctx)
	if err != nil {
		logger.Errorw("Failed to list devices", log.Fields{"error": err})
		return nil, err
	}
	return devices, nil
}

// ListDeviceIds returns the list of device ids managed by a voltha core
func (handler *APIHandler) ListDeviceIds(ctx context.Context, empty *empty.Empty) (*voltha.IDs, error) {
	logger.Debug("ListDeviceIDs")
	if isTestMode(ctx) {
		return &voltha.IDs{Items: make([]*voltha.ID, 0)}, nil
	}
	return handler.deviceMgr.ListDeviceIds()
}

//ReconcileDevices is a request to a voltha core to managed a list of devices  based on their IDs
func (handler *APIHandler) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error) {
	logger.Debug("ReconcileDevices")
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	// No need to grab a transaction as this request is core specific

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.ReconcileDevices(ctx, ids, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// GetLogicalDevice provides a cloned most up to date logical device
func (handler *APIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	logger.Debugw("GetLogicalDevice-request", log.Fields{"id": id})
	return handler.logicalDeviceMgr.getLogicalDevice(ctx, id.Id)
}

// ListLogicalDevices returns the list of all logical devices
func (handler *APIHandler) ListLogicalDevices(ctx context.Context, empty *empty.Empty) (*voltha.LogicalDevices, error) {
	logger.Debug("ListLogicalDevices-request")
	return handler.logicalDeviceMgr.listLogicalDevices(ctx)
}

// ListAdapters returns the contents of all adapters known to the system
func (handler *APIHandler) ListAdapters(ctx context.Context, empty *empty.Empty) (*voltha.Adapters, error) {
	logger.Debug("ListAdapters")
	return handler.adapterMgr.listAdapters(ctx)
}

// ListLogicalDeviceFlows returns the flows of logical device
func (handler *APIHandler) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	logger.Debugw("ListLogicalDeviceFlows", log.Fields{"id": *id})
	return handler.logicalDeviceMgr.ListLogicalDeviceFlows(ctx, id.Id)
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (handler *APIHandler) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	logger.Debugw("ListLogicalDeviceFlowGroups", log.Fields{"id": *id})
	return handler.logicalDeviceMgr.ListLogicalDeviceFlowGroups(ctx, id.Id)
}

// ListLogicalDevicePorts returns ports of logical device
func (handler *APIHandler) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	logger.Debugw("ListLogicalDevicePorts", log.Fields{"logicaldeviceid": id})
	return handler.logicalDeviceMgr.ListLogicalDevicePorts(ctx, id.Id)
}

// CreateDevice creates a new parent device in the data model
func (handler *APIHandler) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	if device.MacAddress == "" && device.GetHostAndPort() == "" {
		logger.Errorf("No Device Info Present")
		return &voltha.Device{}, errors.New("no-device-info-present; MAC or HOSTIP&PORT")
	}
	logger.Debugw("create-device", log.Fields{"device": *device})
	if isTestMode(ctx) {
		return &voltha.Device{Id: device.Id}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.createDevice(ctx, device, ch)
	select {
	case res := <-ch:
		if res != nil {
			if err, ok := res.(error); ok {
				logger.Errorw("create-device-failed", log.Fields{"error": err})
				return nil, err
			}
			if d, ok := res.(*voltha.Device); ok {
				return d, nil
			}
		}
		logger.Warnw("create-device-unexpected-return-type", log.Fields{"result": res})
		err := status.Errorf(codes.Internal, "%s", res)
		return &voltha.Device{}, err
	case <-ctx.Done():
		logger.Debug("createdevice-client-timeout")
		return &voltha.Device{}, ctx.Err()
	}
}

// EnableDevice activates a device by invoking the adopt_device API on the appropriate adapter
func (handler *APIHandler) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	logger.Debugw("enabledevice", log.Fields{"id": id})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.enableDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// DisableDevice disables a device along with any child device it may have
func (handler *APIHandler) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	logger.Debugw("disabledevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.disableDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

//RebootDevice invoked the reboot API to the corresponding adapter
func (handler *APIHandler) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	logger.Debugw("rebootDevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.rebootDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// DeleteDevice removes a device from the data model
func (handler *APIHandler) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	logger.Debugw("deletedevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.deleteDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// ListDevicePorts returns the ports details for a specific device entry
func (handler *APIHandler) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	logger.Debugw("listdeviceports-request", log.Fields{"id": id})
	device, err := handler.deviceMgr.GetDevice(ctx, id.Id)
	if err != nil {
		return &voltha.Ports{}, err
	}
	ports := &voltha.Ports{}
	ports.Items = append(ports.Items, device.Ports...)
	return ports, nil
}

// ListDeviceFlows returns the flow details for a specific device entry
func (handler *APIHandler) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	logger.Debugw("listdeviceflows-request", log.Fields{"id": id})

	device, err := handler.deviceMgr.GetDevice(ctx, id.Id)
	if err != nil {
		return &openflow_13.Flows{}, err
	}
	flows := &openflow_13.Flows{}
	flows.Items = append(flows.Items, device.Flows.Items...)
	return flows, nil
}

// ListDeviceFlowGroups returns the flow group details for a specific device entry
func (handler *APIHandler) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*voltha.FlowGroups, error) {
	logger.Debugw("ListDeviceFlowGroups", log.Fields{"deviceid": id})

	if device, _ := handler.deviceMgr.GetDevice(ctx, id.Id); device != nil {
		return device.GetFlowGroups(), nil
	}
	return &voltha.FlowGroups{}, status.Errorf(codes.NotFound, "device-%s", id.Id)
}

// ListDeviceGroups returns all the device groups known to the system
func (handler *APIHandler) ListDeviceGroups(ctx context.Context, empty *empty.Empty) (*voltha.DeviceGroups, error) {
	logger.Debug("ListDeviceGroups")
	return &voltha.DeviceGroups{}, errors.New("UnImplemented")
}

// GetDeviceGroup returns a specific device group entry
func (handler *APIHandler) GetDeviceGroup(ctx context.Context, id *voltha.ID) (*voltha.DeviceGroup, error) {
	logger.Debug("GetDeviceGroup")
	return &voltha.DeviceGroup{}, errors.New("UnImplemented")
}

// ListDeviceTypes returns all the device types known to the system
func (handler *APIHandler) ListDeviceTypes(ctx context.Context, _ *empty.Empty) (*voltha.DeviceTypes, error) {
	logger.Debug("ListDeviceTypes")

	return &voltha.DeviceTypes{Items: handler.adapterMgr.listDeviceTypes()}, nil
}

// GetDeviceType returns the device type for a specific device entry
func (handler *APIHandler) GetDeviceType(ctx context.Context, id *voltha.ID) (*voltha.DeviceType, error) {
	logger.Debugw("GetDeviceType", log.Fields{"typeid": id})

	if deviceType := handler.adapterMgr.getDeviceType(id.Id); deviceType != nil {
		return deviceType, nil
	}
	return &voltha.DeviceType{}, status.Errorf(codes.NotFound, "device_type-%s", id.Id)
}

// GetVoltha returns the contents of all components (i.e. devices, logical_devices, ...)
func (handler *APIHandler) GetVoltha(ctx context.Context, empty *empty.Empty) (*voltha.Voltha, error) {

	logger.Debug("GetVoltha")
	/*
	 * For now, encode all the version information into a JSON object and
	 * pass that back as "version" so the client can get all the
	 * information associated with the version. Long term the API should
	 * better accomidate this, but for now this will work.
	 */
	data, err := json.Marshal(&version.VersionInfo)
	info := version.VersionInfo.Version
	if err != nil {
		logger.Warnf("Unable to encode version information as JSON: %s", err.Error())
	} else {
		info = string(data)
	}

	return &voltha.Voltha{
		Version: info,
	}, nil
}

// processImageRequest is a helper method to execute an image download request
func (handler *APIHandler) processImageRequest(ctx context.Context, img *voltha.ImageDownload, requestType int) (*common.OperationResp, error) {
	logger.Debugw("processImageDownload", log.Fields{"img": *img, "requestType": requestType})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	failedresponse := &common.OperationResp{Code: voltha.OperationResp_OPERATION_FAILURE}

	ch := make(chan interface{})
	defer close(ch)
	switch requestType {
	case ImageDownload:
		go handler.deviceMgr.downloadImage(ctx, img, ch)
	case CancelImageDownload:
		go handler.deviceMgr.cancelImageDownload(ctx, img, ch)
	case ActivateImage:
		go handler.deviceMgr.activateImage(ctx, img, ch)
	case RevertImage:
		go handler.deviceMgr.revertImage(ctx, img, ch)
	default:
		logger.Warn("invalid-request-type", log.Fields{"requestType": requestType})
		return failedresponse, status.Errorf(codes.InvalidArgument, "%d", requestType)
	}
	select {
	case res := <-ch:
		if res != nil {
			if err, ok := res.(error); ok {
				return failedresponse, err
			}
			if opResp, ok := res.(*common.OperationResp); ok {
				return opResp, nil
			}
		}
		logger.Warnw("download-image-unexpected-return-type", log.Fields{"result": res})
		return failedresponse, status.Errorf(codes.Internal, "%s", res)
	case <-ctx.Done():
		logger.Debug("downloadImage-client-timeout")
		return &common.OperationResp{}, ctx.Err()
	}
}

// DownloadImage execute an image download request
func (handler *APIHandler) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	logger.Debugw("DownloadImage-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return handler.processImageRequest(ctx, img, ImageDownload)
}

// CancelImageDownload cancels image download request
func (handler *APIHandler) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	logger.Debugw("cancelImageDownload-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}
	return handler.processImageRequest(ctx, img, CancelImageDownload)
}

// ActivateImageUpdate activates image update request
func (handler *APIHandler) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	logger.Debugw("activateImageUpdate-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return handler.processImageRequest(ctx, img, ActivateImage)
}

// RevertImageUpdate reverts image update
func (handler *APIHandler) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	logger.Debugw("revertImageUpdate-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return handler.processImageRequest(ctx, img, RevertImage)
}

// GetImageDownloadStatus returns status of image download
func (handler *APIHandler) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw("getImageDownloadStatus-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_SUCCEEDED}
		return resp, nil
	}

	failedresponse := &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.getImageDownloadStatus(ctx, img, ch)

	select {
	case res := <-ch:
		if res != nil {
			if err, ok := res.(error); ok {
				return failedresponse, err
			}
			if downloadResp, ok := res.(*voltha.ImageDownload); ok {
				return downloadResp, nil
			}
		}
		logger.Warnw("download-image-status", log.Fields{"result": res})
		return failedresponse, status.Errorf(codes.Internal, "%s", res)
	case <-ctx.Done():
		logger.Debug("downloadImage-client-timeout")
		return failedresponse, ctx.Err()
	}
}

// GetImageDownload returns image download
func (handler *APIHandler) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw("GetImageDownload-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_SUCCEEDED}
		return resp, nil
	}

	download, err := handler.deviceMgr.getImageDownload(ctx, img)
	if err != nil {
		return &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN}, err
	}
	return download, nil
}

// ListImageDownloads returns image downloads
func (handler *APIHandler) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	logger.Debugw("ListImageDownloads-request", log.Fields{"deviceId": id.Id})
	if isTestMode(ctx) {
		resp := &voltha.ImageDownloads{Items: []*voltha.ImageDownload{}}
		return resp, nil
	}

	downloads, err := handler.deviceMgr.listImageDownloads(ctx, id.Id)
	if err != nil {
		failedResp := &voltha.ImageDownloads{
			Items: []*voltha.ImageDownload{
				{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN},
			},
		}
		return failedResp, err
	}
	return downloads, nil
}

// GetImages returns all images for a specific device entry
func (handler *APIHandler) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	logger.Debugw("GetImages", log.Fields{"deviceid": id.Id})
	device, err := handler.deviceMgr.GetDevice(ctx, id.Id)
	if err != nil {
		return &voltha.Images{}, err
	}
	return device.GetImages(), nil
}

// UpdateDevicePmConfigs updates the PM configs
func (handler *APIHandler) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	logger.Debugw("UpdateDevicePmConfigs-request", log.Fields{"configs": *configs})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.updatePmConfigs(ctx, configs, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// ListDevicePmConfigs returns pm configs of device
func (handler *APIHandler) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	logger.Debugw("ListDevicePmConfigs-request", log.Fields{"deviceId": *id})
	return handler.deviceMgr.listPmConfigs(ctx, id.Id)
}

func (handler *APIHandler) CreateEventFilter(ctx context.Context, filter *voltha.EventFilter) (*voltha.EventFilter, error) {
	logger.Debugw("CreateEventFilter-request", log.Fields{"filter": *filter})
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) UpdateEventFilter(ctx context.Context, filter *voltha.EventFilter) (*voltha.EventFilter, error) {
	logger.Debugw("UpdateEventFilter-request", log.Fields{"filter": *filter})
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) DeleteEventFilter(ctx context.Context, filterInfo *voltha.EventFilter) (*empty.Empty, error) {
	logger.Debugw("DeleteEventFilter-request", log.Fields{"device-id": filterInfo.DeviceId, "filter-id": filterInfo.Id})
	return nil, errors.New("UnImplemented")
}

// GetEventFilter returns all the filters present for a device
func (handler *APIHandler) GetEventFilter(ctx context.Context, id *voltha.ID) (*voltha.EventFilters, error) {
	logger.Debugw("GetEventFilter-request", log.Fields{"device-id": id})
	return nil, errors.New("UnImplemented")
}

// ListEventFilters returns all the filters known to the system
func (handler *APIHandler) ListEventFilters(ctx context.Context, empty *empty.Empty) (*voltha.EventFilters, error) {
	logger.Debug("ListEventFilter-request")
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) SelfTest(ctx context.Context, id *voltha.ID) (*voltha.SelfTestResponse, error) {
	logger.Debugw("SelfTest-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		resp := &voltha.SelfTestResponse{Result: voltha.SelfTestResponse_SUCCESS}
		return resp, nil
	}
	return &voltha.SelfTestResponse{}, errors.New("UnImplemented")
}

func (handler *APIHandler) forwardPacketOut(ctx context.Context, packet *openflow_13.PacketOut) {
	logger.Debugw("forwardPacketOut-request", log.Fields{"packet": packet})
	//TODO: Update this logic once the OF Controller (OFAgent in this case) can include a transaction Id in its
	// request.  For performance reason we can let both Cores in a Core-Pair forward the Packet to the adapters and
	// let once of the shim layer (kafka proxy or adapter request handler filters out the duplicate packet)
	if agent := handler.logicalDeviceMgr.getLogicalDeviceAgent(ctx, packet.Id); agent != nil {
		agent.packetOut(ctx, packet.PacketOut)
	} else {
		logger.Errorf("No logical device agent present", log.Fields{"logicaldeviceID": packet.Id})
	}
}

// StreamPacketsOut sends packets to adapter
func (handler *APIHandler) StreamPacketsOut(packets voltha.VolthaService_StreamPacketsOutServer) error {
	logger.Debugw("StreamPacketsOut-request", log.Fields{"packets": packets})
loop:
	for {
		select {
		case <-packets.Context().Done():
			logger.Infow("StreamPacketsOut-context-done", log.Fields{"packets": packets, "error": packets.Context().Err()})
			break loop
		default:
		}

		packet, err := packets.Recv()

		if err == io.EOF {
			logger.Debugw("Received-EOF", log.Fields{"packets": packets})
			break loop
		}

		if err != nil {
			logger.Errorw("Failed to receive packet out", log.Fields{"error": err})
			continue
		}

		handler.forwardPacketOut(packets.Context(), packet)
	}

	logger.Debugw("StreamPacketsOut-request-done", log.Fields{"packets": packets})
	return nil
}

func (handler *APIHandler) sendPacketIn(deviceID string, transationID string, packet *openflow_13.OfpPacketIn) {
	// TODO: Augment the OF PacketIn to include the transactionId
	packetIn := openflow_13.PacketIn{Id: deviceID, PacketIn: packet}
	logger.Debugw("sendPacketIn", log.Fields{"packetIn": packetIn})
	handler.packetInQueue <- packetIn
}

type callTracker struct {
	failedPacket interface{}
}
type streamTracker struct {
	calls map[string]*callTracker
	sync.Mutex
}

var streamingTracker = &streamTracker{calls: make(map[string]*callTracker)}

func (handler *APIHandler) getStreamingTracker(method string, done chan<- bool) *callTracker {
	streamingTracker.Lock()
	defer streamingTracker.Unlock()
	if _, ok := streamingTracker.calls[method]; ok {
		// bail out the other packet in thread
		logger.Debugf("%s streaming call already running. Exiting it", method)
		done <- true
		logger.Debugf("Last %s exited. Continuing ...", method)
	} else {
		streamingTracker.calls[method] = &callTracker{failedPacket: nil}
	}
	return streamingTracker.calls[method]
}

func (handler *APIHandler) flushFailedPackets(tracker *callTracker) error {
	if tracker.failedPacket != nil {
		switch tracker.failedPacket.(type) {
		case openflow_13.PacketIn:
			logger.Debug("Enqueueing last failed packetIn")
			handler.packetInQueue <- tracker.failedPacket.(openflow_13.PacketIn)
		case openflow_13.ChangeEvent:
			logger.Debug("Enqueueing last failed changeEvent")
			handler.changeEventQueue <- tracker.failedPacket.(openflow_13.ChangeEvent)
		}
	}
	return nil
}

// ReceivePacketsIn receives packets from adapter
func (handler *APIHandler) ReceivePacketsIn(empty *empty.Empty, packetsIn voltha.VolthaService_ReceivePacketsInServer) error {
	var streamingTracker = handler.getStreamingTracker("ReceivePacketsIn", handler.packetInQueueDone)
	logger.Debugw("ReceivePacketsIn-request", log.Fields{"packetsIn": packetsIn})

	err := handler.flushFailedPackets(streamingTracker)
	if err != nil {
		logger.Errorw("unable-to-flush-failed-packets", log.Fields{"error": err})
	}

loop:
	for {
		select {
		case packet := <-handler.packetInQueue:
			logger.Debugw("sending-packet-in", log.Fields{
				"packet": hex.EncodeToString(packet.PacketIn.Data),
			})
			if err := packetsIn.Send(&packet); err != nil {
				logger.Errorw("failed-to-send-packet", log.Fields{"error": err})
				// save the last failed packet in
				streamingTracker.failedPacket = packet
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed packet saved to avoid flush
					streamingTracker.failedPacket = nil
				}
			}
		case <-handler.packetInQueueDone:
			logger.Debug("Another ReceivePacketsIn running. Bailing out ...")
			break loop
		}
	}

	//TODO: Find an elegant way to get out of the above loop when the Core is stopped
	return nil
}

func (handler *APIHandler) sendChangeEvent(deviceID string, portStatus *openflow_13.OfpPortStatus) {
	// TODO: validate the type of portStatus parameter
	//if _, ok := portStatus.(*openflow_13.OfpPortStatus); ok {
	//}
	event := openflow_13.ChangeEvent{Id: deviceID, Event: &openflow_13.ChangeEvent_PortStatus{PortStatus: portStatus}}
	logger.Debugw("sendChangeEvent", log.Fields{"event": event})
	handler.changeEventQueue <- event
}

// ReceiveChangeEvents receives change in events
func (handler *APIHandler) ReceiveChangeEvents(empty *empty.Empty, changeEvents voltha.VolthaService_ReceiveChangeEventsServer) error {
	var streamingTracker = handler.getStreamingTracker("ReceiveChangeEvents", handler.changeEventQueueDone)
	logger.Debugw("ReceiveChangeEvents-request", log.Fields{"changeEvents": changeEvents})

	err := handler.flushFailedPackets(streamingTracker)
	if err != nil {
		logger.Errorw("unable-to-flush-failed-packets", log.Fields{"error": err})
	}

loop:
	for {
		select {
		// Dequeue a change event
		case event := <-handler.changeEventQueue:
			logger.Debugw("sending-change-event", log.Fields{"event": event})
			if err := changeEvents.Send(&event); err != nil {
				logger.Errorw("failed-to-send-change-event", log.Fields{"error": err})
				// save last failed changeevent
				streamingTracker.failedPacket = event
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed event saved on success to avoid flushing
					streamingTracker.failedPacket = nil
				}
			}
		case <-handler.changeEventQueueDone:
			logger.Debug("Another ReceiveChangeEvents already running. Bailing out ...")
			break loop
		}
	}

	return nil
}

// Subscribe subscribing request of ofagent
func (handler *APIHandler) Subscribe(
	ctx context.Context,
	ofAgent *voltha.OfAgentSubscriber,
) (*voltha.OfAgentSubscriber, error) {
	logger.Debugw("Subscribe-request", log.Fields{"ofAgent": ofAgent})
	return &voltha.OfAgentSubscriber{OfagentId: ofAgent.OfagentId, VolthaId: ofAgent.VolthaId}, nil
}

// GetAlarmDeviceData @TODO useless stub, what should this actually do?
func (handler *APIHandler) GetAlarmDeviceData(ctx context.Context, in *common.ID) (*omci.AlarmDeviceData, error) {
	logger.Debug("GetAlarmDeviceData-stub")
	return &omci.AlarmDeviceData{}, errors.New("UnImplemented")
}

// ListLogicalDeviceMeters returns logical device meters
func (handler *APIHandler) ListLogicalDeviceMeters(ctx context.Context, id *voltha.ID) (*openflow_13.Meters, error) {

	logger.Debugw("ListLogicalDeviceMeters", log.Fields{"id": *id})
	return handler.logicalDeviceMgr.ListLogicalDeviceMeters(ctx, id.Id)
}

// GetMeterStatsOfLogicalDevice @TODO useless stub, what should this actually do?
func (handler *APIHandler) GetMeterStatsOfLogicalDevice(ctx context.Context, in *common.ID) (*openflow_13.MeterStatsReply, error) {
	logger.Debug("GetMeterStatsOfLogicalDevice")
	return &openflow_13.MeterStatsReply{}, errors.New("UnImplemented")
}

// GetMibDeviceData @TODO useless stub, what should this actually do?
func (handler *APIHandler) GetMibDeviceData(ctx context.Context, in *common.ID) (*omci.MibDeviceData, error) {
	logger.Debug("GetMibDeviceData")
	return &omci.MibDeviceData{}, errors.New("UnImplemented")
}

// SimulateAlarm sends simulate alarm request
func (handler *APIHandler) SimulateAlarm(
	ctx context.Context,
	in *voltha.SimulateAlarmRequest,
) (*common.OperationResp, error) {
	logger.Debugw("SimulateAlarm-request", log.Fields{"id": in.Id})
	successResp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
	if isTestMode(ctx) {
		return successResp, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.simulateAlarm(ctx, in, ch)
	return successResp, nil
}

// UpdateLogicalDeviceMeterTable - This function sends meter mod request to logical device manager and waits for response
func (handler *APIHandler) UpdateLogicalDeviceMeterTable(ctx context.Context, meter *openflow_13.MeterModUpdate) (*empty.Empty, error) {
	logger.Debugw("UpdateLogicalDeviceMeterTable-request",
		log.Fields{"meter": meter, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.updateMeterTable(ctx, meter.Id, meter.MeterMod, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// GetMembership returns membership
func (handler *APIHandler) GetMembership(context.Context, *empty.Empty) (*voltha.Membership, error) {
	return &voltha.Membership{}, errors.New("UnImplemented")
}

// UpdateMembership updates membership
func (handler *APIHandler) UpdateMembership(context.Context, *voltha.Membership) (*empty.Empty, error) {
	return &empty.Empty{}, errors.New("UnImplemented")
}

func (handler *APIHandler) EnablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	logger.Debugw("EnablePort-request", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.enablePort(ctx, port, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) DisablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {

	logger.Debugw("DisablePort-request", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	if isTestMode(ctx) {
		return &empty.Empty{}, nil
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.disablePort(ctx, port, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) StartOmciTestAction(ctx context.Context, omcitestrequest *voltha.OmciTestRequest) (*voltha.TestResponse, error) {
	logger.Debugw("Omci_test_Request", log.Fields{"id": omcitestrequest.Id, "uuid": omcitestrequest.Uuid})
	return handler.deviceMgr.startOmciTest(ctx, omcitestrequest)
}
