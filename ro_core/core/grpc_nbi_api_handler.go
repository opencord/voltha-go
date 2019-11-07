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
	"errors"

	"github.com/golang/protobuf/ptypes/empty"
	da "github.com/opencord/voltha-go/common/core/northbound/grpc"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/omci"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// APIHandler represents API handler related information
type APIHandler struct {
	commonMgr        *ModelProxyManager
	deviceMgr        *DeviceManager
	logicalDeviceMgr *LogicalDeviceManager
	da.DefaultAPIHandler
}

// NewAPIHandler creates API handler
func NewAPIHandler(generalMgr *ModelProxyManager, deviceMgr *DeviceManager, lDeviceMgr *LogicalDeviceManager) *APIHandler {
	handler := &APIHandler{
		commonMgr:        generalMgr,
		deviceMgr:        deviceMgr,
		logicalDeviceMgr: lDeviceMgr,
	}
	return handler
}

// isTestMode is a helper function to determine a function is invoked for testing only
func isTestMode(ctx context.Context) bool {
	md, _ := metadata.FromIncomingContext(ctx)
	_, exist := md[common.TestModeKeys_api_test.String()]
	return exist
}

// waitForNilResponseOnSuccess is a helper function to wait for a response on channel ch where an nil
// response is expected in a successful scenario
func waitForNilResponseOnSuccess(ctx context.Context, ch chan interface{}) (*empty.Empty, error) {
	select {
	case res := <-ch:
		if res == nil {
			return new(empty.Empty), nil
		} else if err, ok := res.(error); ok {
			return new(empty.Empty), err
		} else {
			log.Warnw("unexpected-return-type", log.Fields{"result": res})
			err = status.Errorf(codes.Internal, "%s", res)
			return new(empty.Empty), err
		}
	case <-ctx.Done():
		log.Debug("client-timeout")
		return nil, ctx.Err()
	}
}

// UpdateLogLevel updates log level to the requested level in specific packaged if mentioned
func (*APIHandler) UpdateLogLevel(ctx context.Context, logging *voltha.Logging) (*empty.Empty, error) {
	log.Debugw("UpdateLogLevel-request", log.Fields{"package": logging.PackageName, "intval": int(logging.Level)})
	out := new(empty.Empty)
	if logging.PackageName == "" {
		log.SetAllLogLevel(int(logging.Level))
		log.SetDefaultLogLevel(int(logging.Level))
	} else if logging.PackageName == "default" {
		log.SetDefaultLogLevel(int(logging.Level))
	} else {
		log.SetPackageLogLevel(logging.PackageName, int(logging.Level))
	}

	return out, nil
}

// GetLogLevels returns log levels for requested packages
func (APIHandler) GetLogLevels(ctx context.Context, in *voltha.LoggingComponent) (*voltha.Loggings, error) {
	logLevels := &voltha.Loggings{}

	// do the per-package log levels
	for _, packageName := range log.GetPackageNames() {
		level, err := log.GetPackageLogLevel(packageName)
		if err != nil {
			return nil, err
		}
		logLevel := &voltha.Logging{
			ComponentName: in.ComponentName,
			PackageName:   packageName,
			Level:         voltha.LogLevel_LogLevel(level)}
		logLevels.Items = append(logLevels.Items, logLevel)
	}

	// now do the default log level
	logLevel := &voltha.Logging{
		ComponentName: in.ComponentName,
		PackageName:   "default",
		Level:         voltha.LogLevel_LogLevel(log.GetDefaultLogLevel())}
	logLevels.Items = append(logLevels.Items, logLevel)

	return logLevels, nil
}

// GetVoltha returns the contents of all components (i.e. devices, logical_devices, ...)
func (handler *APIHandler) GetVoltha(ctx context.Context, empty *empty.Empty) (*voltha.Voltha, error) {
	log.Debug("GetVoltha")
	return handler.commonMgr.GetVoltha(ctx)
}

// ListCoreInstances returns details on the running core containers
func (handler *APIHandler) ListCoreInstances(ctx context.Context, empty *empty.Empty) (*voltha.CoreInstances, error) {
	log.Debug("ListCoreInstances")
	return handler.commonMgr.ListCoreInstances(ctx)
}

// GetCoreInstance returns the details of a specific core container
func (handler *APIHandler) GetCoreInstance(ctx context.Context, id *voltha.ID) (*voltha.CoreInstance, error) {
	log.Debugw("GetCoreInstance", log.Fields{"id": id})
	return handler.commonMgr.GetCoreInstance(ctx, id.Id)
}

// ListAdapters returns the contents of all adapters known to the system
func (handler *APIHandler) ListAdapters(ctx context.Context, empty *empty.Empty) (*voltha.Adapters, error) {
	log.Debug("ListDevices")
	return handler.commonMgr.ListAdapters(ctx)
}

// GetDevice returns the details a specific device
func (handler *APIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	log.Debugw("GetDevice-request", log.Fields{"id": id})
	return handler.deviceMgr.GetDevice(id.Id)
}

// ListDevices returns the contents of all devices known to the system
func (handler *APIHandler) ListDevices(ctx context.Context, empty *empty.Empty) (*voltha.Devices, error) {
	log.Debug("ListDevices")
	devices, err := handler.deviceMgr.ListDevices(); if err != nil {
		log.Errorw("failed-to-list-devices", log.Fields{"error": err})
		return nil, err
	}
	return devices, nil
}

// ListDeviceIds returns the list of device ids managed by a voltha core
func (handler *APIHandler) ListDeviceIds(ctx context.Context, empty *empty.Empty) (*voltha.IDs, error) {
	log.Debug("ListDeviceIDs")
	if isTestMode(ctx) {
		out := &voltha.IDs{Items: make([]*voltha.ID, 0)}
		return out, nil
	}
	return handler.deviceMgr.ListDeviceIDs()
}

// ListDevicePorts returns the ports details for a specific device entry
func (handler *APIHandler) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	log.Debugw("ListDevicePorts", log.Fields{"deviceid": id})
	return handler.deviceMgr.ListDevicePorts(ctx, id.Id)
}

// ListDevicePmConfigs returns the PM config details for a specific device entry
func (handler *APIHandler) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs", log.Fields{"deviceid": id})
	return handler.deviceMgr.ListDevicePmConfigs(ctx, id.Id)
}

// ListDeviceFlows returns the flow details for a specific device entry
func (handler *APIHandler) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*voltha.Flows, error) {
	log.Debugw("ListDeviceFlows", log.Fields{"deviceid": id})
	return handler.deviceMgr.ListDeviceFlows(ctx, id.Id)
}

// ListDeviceFlowGroups returns the flow group details for a specific device entry
func (handler *APIHandler) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*voltha.FlowGroups, error) {
	log.Debugw("ListDeviceFlowGroups", log.Fields{"deviceid": id})
	return handler.deviceMgr.ListDeviceFlowGroups(ctx, id.Id)
}

// ListDeviceTypes returns all the device types known to the system
func (handler *APIHandler) ListDeviceTypes(ctx context.Context, empty *empty.Empty) (*voltha.DeviceTypes, error) {
	log.Debug("ListDeviceTypes")
	return handler.commonMgr.ListDeviceTypes(ctx)
}

// GetDeviceType returns the device type for a specific device entry
func (handler *APIHandler) GetDeviceType(ctx context.Context, id *voltha.ID) (*voltha.DeviceType, error) {
	log.Debugw("GetDeviceType", log.Fields{"typeid": id})
	return handler.commonMgr.GetDeviceType(ctx, id.Id)
}

// ListDeviceGroups returns all the device groups known to the system
func (handler *APIHandler) ListDeviceGroups(ctx context.Context, empty *empty.Empty) (*voltha.DeviceGroups, error) {
	log.Debug("ListDeviceGroups")
	return handler.commonMgr.ListDeviceGroups(ctx)
}

// GetDeviceGroup returns a specific device group entry
func (handler *APIHandler) GetDeviceGroup(ctx context.Context, id *voltha.ID) (*voltha.DeviceGroup, error) {
	log.Debugw("GetDeviceGroup", log.Fields{"groupid": id})
	return handler.commonMgr.GetDeviceGroup(ctx, id.Id)
}

// GetImageDownloadStatus returns the download status for a specific image entry
func (handler *APIHandler) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownloadStatus", log.Fields{"deviceid": img.GetId(), "imagename": img.GetName()})
	return handler.deviceMgr.GetImageDownloadStatus(ctx, img.GetId(), img.GetName())
}

// GetImageDownload return the download details for a specific image entry
func (handler *APIHandler) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownload", log.Fields{"deviceid": img.GetId(), "imagename": img.GetName()})
	return handler.deviceMgr.GetImageDownload(ctx, img.GetId(), img.GetName())
}

// ListImageDownloads returns all image downloads known to the system
func (handler *APIHandler) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	log.Debugw("GetImageDownload", log.Fields{"deviceid": id})
	return handler.deviceMgr.ListImageDownloads(ctx, id.Id)
}

// GetImages returns all images for a specific device entry
func (handler *APIHandler) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	log.Debugw("GetImages", log.Fields{"deviceid": id})
	return handler.deviceMgr.GetImages(ctx, id.Id)
}

// ListAlarmFilters return all alarm filters known to the system
func (handler *APIHandler) ListAlarmFilters(ctx context.Context, empty *empty.Empty) (*voltha.AlarmFilters, error) {
	log.Debug("ListAlarmFilters")
	return handler.commonMgr.ListAlarmFilters(ctx)
}

// GetAlarmFilter returns a specific alarm filter entry
func (handler *APIHandler) GetAlarmFilter(ctx context.Context, id *voltha.ID) (*voltha.AlarmFilter, error) {
	log.Debugw("GetAlarmFilter", log.Fields{"alarmid": id})
	return handler.commonMgr.GetAlarmFilter(ctx, id.Id)
}

//ReconcileDevices is a request to a voltha core to managed a list of devices  based on their IDs
func (handler *APIHandler) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error) {
	log.Debug("ReconcileDevices")
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.ReconcileDevices(ctx, ids, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// GetLogicalDevice returns the details for a specific logical device entry
func (handler *APIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	log.Debugw("GetLogicalDevice-request", log.Fields{"id": id})
	return handler.logicalDeviceMgr.getLogicalDevice(id.Id)
}

// ListLogicalDevices returns all logical devices known to the system
func (handler *APIHandler) ListLogicalDevices(ctx context.Context, empty *empty.Empty) (*voltha.LogicalDevices, error) {
	log.Debug("ListLogicalDevices")
	return handler.logicalDeviceMgr.listLogicalDevices()
}

// ListLogicalDevicePorts returns port details for a specific logical device entry
func (handler *APIHandler) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	log.Debugw("ListLogicalDevicePorts", log.Fields{"logicaldeviceid": id})
	return handler.logicalDeviceMgr.ListLogicalDevicePorts(ctx, id.Id)
}

// ListLogicalDeviceFlows returns flow details for a specific logical device entry
func (handler *APIHandler) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*voltha.Flows, error) {
	log.Debugw("ListLogicalDeviceFlows", log.Fields{"logicaldeviceid": id})
	return handler.logicalDeviceMgr.ListLogicalDeviceFlows(ctx, id.Id)
}

// ListLogicalDeviceFlowGroups returns flow group details for a specific logical device entry
func (handler *APIHandler) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*voltha.FlowGroups, error) {
	log.Debugw("ListLogicalDeviceFlows", log.Fields{"logicaldeviceid": id})
	return handler.logicalDeviceMgr.ListLogicalDeviceFlowGroups(ctx, id.Id)
}

// SelfTest - TODO
func (handler *APIHandler) SelfTest(ctx context.Context, id *voltha.ID) (*voltha.SelfTestResponse, error) {
	log.Debugw("SelfTest-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		resp := &voltha.SelfTestResponse{Result: voltha.SelfTestResponse_SUCCESS}
		return resp, nil
	}
	return nil, errors.New("UnImplemented")
}

// GetAlarmDeviceData - @TODO useless stub, what should this actually do?
func (handler *APIHandler) GetAlarmDeviceData(
	ctx context.Context,
	in *common.ID,
) (*omci.AlarmDeviceData, error) {
	log.Debug("GetAlarmDeviceData-stub")
	return nil, nil
}

// GetMeterStatsOfLogicalDevice - @TODO useless stub, what should this actually do?
func (handler *APIHandler) GetMeterStatsOfLogicalDevice(
	ctx context.Context,
	in *common.ID,
) (*openflow_13.MeterStatsReply, error) {
	log.Debug("GetMeterStatsOfLogicalDevice-stub")
	return nil, nil
}

// GetMibDeviceData - @TODO useless stub, what should this actually do?
func (handler *APIHandler) GetMibDeviceData(
	ctx context.Context,
	in *common.ID,
) (*omci.MibDeviceData, error) {
	log.Debug("GetMibDeviceData-stub")
	return nil, nil
}

// SimulateAlarm - @TODO useless stub, what should this actually do?
func (handler *APIHandler) SimulateAlarm(
	ctx context.Context,
	in *voltha.SimulateAlarmRequest,
) (*common.OperationResp, error) {
	log.Debug("SimulateAlarm-stub")
	return nil, nil
}
