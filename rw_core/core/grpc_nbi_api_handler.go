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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type APIHandler struct {
	deviceMgr        *DeviceManager
	logicalDeviceMgr *LogicalDeviceManager
	da.DefaultAPIHandler
}

func NewAPIHandler(deviceMgr *DeviceManager, lDeviceMgr *LogicalDeviceManager) *APIHandler {
	handler := &APIHandler{deviceMgr: deviceMgr,
		logicalDeviceMgr: lDeviceMgr}
	return handler
}
func isTestMode(ctx context.Context) bool {
	md, _ := metadata.FromIncomingContext(ctx)
	_, exist := md[common.TestModeKeys_api_test.String()]
	return exist
}

func (handler *APIHandler) UpdateLogLevel(ctx context.Context, logging *voltha.Logging) (*empty.Empty, error) {
	log.Debugw("UpdateLogLevel-request", log.Fields{"newloglevel": logging.Level, "intval": int(logging.Level)})
	out := new(empty.Empty)
	log.SetPackageLogLevel(logging.PackageName, int(logging.Level))
	return out, nil
}

func processEnableDevicePort(ctx context.Context, id *voltha.LogicalPortId, ch chan error) {
	log.Debugw("processEnableDevicePort", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	ch <- status.Errorf(100, "%d-%s", 100, "erreur")
}

func (handler *APIHandler) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("EnableLogicalDevicePort-request", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	ch := make(chan error)
	go processEnableDevicePort(ctx, id, ch)
	select {
	case resp := <-ch:
		close(ch)
		return new(empty.Empty), resp
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (handler *APIHandler) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("DisableLogicalDevicePort-request", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("Unimplemented")
}

func (handler *APIHandler) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowTable-request", log.Fields{"flow": flow, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("Unimplemented")
}

func (handler *APIHandler) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowGroupTable-request", log.Fields{"flow": flow, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("Unimplemented")
}

// GetDevice must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	log.Debugw("GetDevice-request", log.Fields{"id": id})
	return handler.deviceMgr.getDevice(id.Id)
}

// GetDevice must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) ListDevices(ctx context.Context, empty *empty.Empty) (*voltha.Devices, error) {
	log.Debug("ListDevices")
	return handler.deviceMgr.ListDevices()
}

// GetLogicalDevice must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	log.Debugw("GetLogicalDevice-request", log.Fields{"id": id})
	return handler.logicalDeviceMgr.getLogicalDevice(id.Id)
}

// ListLogicalDevices must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) ListLogicalDevices(ctx context.Context, empty *empty.Empty) (*voltha.LogicalDevices, error) {
	log.Debug("ListLogicalDevices")
	return handler.logicalDeviceMgr.listLogicalDevices()
}

func (handler *APIHandler) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	log.Debugw("createdevice", log.Fields{"device": *device})
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
				return &voltha.Device{}, err
			}
			if d, ok := res.(*voltha.Device); ok {
				return d, nil
			}
		}
		log.Warnw("create-device-unexpected-return-type", log.Fields{"result": res})
		err := status.Errorf(codes.Internal, "%s", res)
		return &voltha.Device{}, err
	case <-ctx.Done():
		log.Debug("createdevice-client-timeout")
		return nil, ctx.Err()
	}
}

func (handler *APIHandler) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("enabledevice", log.Fields{"id": id})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.enableDevice(ctx, id, ch)
	select {
	case res := <-ch:
		if res == nil {
			return new(empty.Empty), nil
		} else if err, ok := res.(error); ok {
			return new(empty.Empty), err
		} else {
			log.Warnw("enable-device-unexpected-return-type", log.Fields{"result": res})
			err = status.Errorf(codes.Internal, "%s", res)
			return new(empty.Empty), err
		}
	case <-ctx.Done():
		log.Debug("enabledevice-client-timeout")
		return nil, ctx.Err()
	}
}

func (handler *APIHandler) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("disabledevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.disableDevice(ctx, id, ch)
	select {
	case res := <-ch:
		if res == nil {
			return new(empty.Empty), nil
		} else if err, ok := res.(error); ok {
			return new(empty.Empty), err
		} else {
			log.Warnw("disable-device-unexpected-return-type", log.Fields{"result": res})
			err = status.Errorf(codes.Internal, "%s", res)
			return new(empty.Empty), err
		}
	case <-ctx.Done():
		log.Debug("enabledevice-client-timeout")
		return nil, ctx.Err()
	}
	return nil, errors.New("Unimplemented")
}

func (handler *APIHandler) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("disabledevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("Unimplemented")
}

func (handler *APIHandler) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("deletedevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("Unimplemented")
}

func (handler *APIHandler) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("DownloadImage-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("CancelImageDownload-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("ActivateImageUpdate-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("RevertImageUpdate-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	log.Debugw("UpdateDevicePmConfigs-request", log.Fields{"configs": *configs})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) CreateAlarmFilter(ctx context.Context, filter *voltha.AlarmFilter) (*voltha.AlarmFilter, error) {
	log.Debugw("CreateAlarmFilter-request", log.Fields{"filter": *filter})
	if isTestMode(ctx) {
		f := &voltha.AlarmFilter{Id: filter.Id}
		return f, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) UpdateAlarmFilter(ctx context.Context, filter *voltha.AlarmFilter) (*voltha.AlarmFilter, error) {
	log.Debugw("UpdateAlarmFilter-request", log.Fields{"filter": *filter})
	if isTestMode(ctx) {
		f := &voltha.AlarmFilter{Id: filter.Id}
		return f, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) DeleteAlarmFilter(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("DeleteAlarmFilter-request", log.Fields{"id": *id})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	return nil, errors.New("UnImplemented")
}

func (handler *APIHandler) SelfTest(ctx context.Context, id *voltha.ID) (*voltha.SelfTestResponse, error) {
	log.Debugw("SelfTest-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		resp := &voltha.SelfTestResponse{Result: voltha.SelfTestResponse_SUCCESS}
		return resp, nil
	}
	return nil, errors.New("UnImplemented")
}
