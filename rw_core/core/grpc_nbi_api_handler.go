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
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-protos/go/common"
	"github.com/opencord/voltha-protos/go/omci"
	"github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"sync"
	"time"
)

const (
	IMAGE_DOWNLOAD        = iota
	CANCEL_IMAGE_DOWNLOAD = iota
	ACTIVATE_IMAGE        = iota
	REVERT_IMAGE          = iota
)

type APIHandler struct {
	deviceMgr                 *DeviceManager
	logicalDeviceMgr          *LogicalDeviceManager
	adapterMgr                *AdapterManager
	packetInQueue             chan openflow_13.PacketIn
	changeEventQueue          chan openflow_13.ChangeEvent
	packetInQueueDone         chan bool
	changeEventQueueDone      chan bool
	coreInCompetingMode       bool
	longRunningRequestTimeout int64
	defaultRequestTimeout     int64
	da.DefaultAPIHandler
	core *Core
}

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

// This function attempts to extract the serial number from the request metadata
// and create a KV transaction for that serial number for the current core.
func (handler *APIHandler) createKvTransaction(ctx context.Context) (*KVTransaction, error) {
	var (
		err    error
		ok     bool
		md     metadata.MD
		serNum []string
	)
	if md, ok = metadata.FromIncomingContext(ctx); !ok {
		err = errors.New("metadata-not-found")
	} else if serNum, ok = md["voltha_serial_number"]; !ok {
		err = errors.New("serial-number-not-found")
	}
	if !ok || serNum == nil {
		log.Error(err)
		return nil, err
	}
	// Create KV transaction
	txn := NewKVTransaction(serNum[0])
	return txn, nil
}

// isOFControllerRequest is a helper function to determine if a request was initiated
// from the OpenFlow controller (or its proxy, e.g. OFAgent)
func (handler *APIHandler) isOFControllerRequest(ctx context.Context) bool {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		// Metadata in context
		if _, ok = md[handler.core.config.CoreBindingKey]; ok {
			// OFAgent field in metadata
			log.Debug("OFController-request")
			return true
		}
	}
	log.Debug("not-OFController-request")
	return false
}

// competeForTransaction is a helper function to determine whether every request needs to compete with another
// Core to execute the request
func (handler *APIHandler) competeForTransaction() bool {
	return handler.coreInCompetingMode
}

// acquireRequest handles transaction processing for device creation and  list requests, i.e. when there are no
// specific id requested (list scenario) or id present in the request (creation use case).
func (handler *APIHandler) acquireRequest(ctx context.Context, maxTimeout ...int64) (*KVTransaction, error) {
	timeout := handler.defaultRequestTimeout
	if len(maxTimeout) > 0 {
		timeout = maxTimeout[0]
	}
	log.Debugw("transaction-timeout", log.Fields{"timeout": timeout})
	txn, err := handler.createKvTransaction(ctx)
	if txn == nil {
		return nil, err
	} else if txn.Acquired(timeout) {
		return txn, nil
	} else {
		return nil, errors.New("failed-to-seize-request")
	}
}

// takeRequestOwnership creates a transaction in the dB for this request and handles the logic of transaction
// acquisition.  If the device is owned by this Core (in a core-pair) then acquire the transaction with a
// timeout value (in the event of a timeout the other Core in the core-pair will proceed with the transaction).  If the
// device is not owned then this Core will just monitor the transaction for potential timeouts.
func (handler *APIHandler) takeRequestOwnership(ctx context.Context, id interface{}, maxTimeout ...int64) (*KVTransaction, error) {
	t := time.Now()
	timeout := handler.defaultRequestTimeout
	if len(maxTimeout) > 0 {
		timeout = maxTimeout[0]
	}
	txn, err := handler.createKvTransaction(ctx)
	if txn == nil {
		return nil, err
	}

	owned := false
	if id != nil {
		owned = handler.core.deviceOwnership.OwnedByMe(id)
	}
	if owned {
		if txn.Acquired(timeout) {
			log.Debugw("acquired-transaction", log.Fields{"transaction-timeout": timeout})
			return txn, nil
		} else {
			return nil, errors.New("failed-to-seize-request")
		}
	} else {
		if txn.Monitor(timeout) {
			log.Debugw("acquired-transaction-after-timeout", log.Fields{"timeout": timeout, "waited-time": time.Since(t)})
			return txn, nil
		} else {
			log.Debugw("transaction-completed-by-other", log.Fields{"timeout": timeout, "waited-time": time.Since(t)})
			return nil, errors.New(string(COMPLETED_BY_OTHER))
		}
	}
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

func (handler *APIHandler) UpdateLogLevel(ctx context.Context, logging *voltha.Logging) (*empty.Empty, error) {
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

func (aa APIHandler) GetLogLevels(ctx context.Context, in *voltha.LoggingComponent) (*voltha.Loggings, error) {
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

func (handler *APIHandler) GetLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	log.Debugw("GetLogicalDevicePort-request", log.Fields{"id": *id})

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return &voltha.LogicalPort{}, err
		} else {
			defer txn.Close()
		}
	}
	return handler.logicalDeviceMgr.getLogicalPort(id)
}

func (handler *APIHandler) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("EnableLogicalDevicePort-request", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.enableLogicalPort(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	log.Debugw("DisableLogicalDevicePort-request", log.Fields{"id": id, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.disableLogicalPort(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowTable-request", log.Fields{"flow": flow, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: flow.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.updateFlowTable(ctx, flow.Id, flow.FlowMod, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceFlowGroupTable-request", log.Fields{"flow": flow, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: flow.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.updateGroupTable(ctx, flow.Id, flow.GroupMod, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// GetDevice must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	log.Debugw("GetDevice-request", log.Fields{"id": id})
	return handler.deviceMgr.GetDevice(id.Id)
}

// GetDevice must be implemented in the read-only containers - should it also be implemented here?
func (handler *APIHandler) ListDevices(ctx context.Context, empty *empty.Empty) (*voltha.Devices, error) {
	log.Debug("ListDevices")
	return handler.deviceMgr.ListDevices()
}

// ListDeviceIds returns the list of device ids managed by a voltha core
func (handler *APIHandler) ListDeviceIds(ctx context.Context, empty *empty.Empty) (*voltha.IDs, error) {
	log.Debug("ListDeviceIDs")
	if isTestMode(ctx) {
		out := &voltha.IDs{Items: make([]*voltha.ID, 0)}
		return out, nil
	}
	return handler.deviceMgr.ListDeviceIds()
}

//ReconcileDevices is a request to a voltha core to managed a list of devices  based on their IDs
func (handler *APIHandler) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error) {
	log.Debug("ReconcileDevices")
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}

	// No need to grab a transaction as this request is core specific

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.ReconcileDevices(ctx, ids, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	log.Debugw("GetLogicalDevice-request", log.Fields{"id": id})
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return &voltha.LogicalDevice{}, err
		} else {
			defer txn.Close()
		}
	}
	return handler.logicalDeviceMgr.getLogicalDevice(id.Id)
}

func (handler *APIHandler) ListLogicalDevices(ctx context.Context, empty *empty.Empty) (*voltha.LogicalDevices, error) {
	log.Debug("ListLogicalDevices-request")
	if handler.competeForTransaction() {
		if txn, err := handler.acquireRequest(ctx); err != nil {
			return &voltha.LogicalDevices{}, err
		} else {
			defer txn.Close()
		}
	}
	if handler.isOFControllerRequest(ctx) {
		//	Since an OF controller is only interested in the set of logical devices managed by thgis Core then return
		//	only logical devices managed/monitored by this Core.
		return handler.logicalDeviceMgr.listManagedLogicalDevices()
	}
	return handler.logicalDeviceMgr.listLogicalDevices()
}

// ListAdapters returns the contents of all adapters known to the system
func (handler *APIHandler) ListAdapters(ctx context.Context, empty *empty.Empty) (*voltha.Adapters, error) {
	log.Debug("ListDevices")
	return handler.adapterMgr.listAdapters(ctx)
}

func (handler *APIHandler) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	log.Debugw("ListLogicalDeviceFlows", log.Fields{"id": *id})
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return &openflow_13.Flows{}, err
		} else {
			defer txn.Close()
		}
	}
	return handler.logicalDeviceMgr.ListLogicalDeviceFlows(ctx, id.Id)
}

func (handler *APIHandler) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	log.Debugw("ListLogicalDeviceFlowGroups", log.Fields{"id": *id})
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return &openflow_13.FlowGroups{}, err
		} else {
			defer txn.Close()
		}
	}
	return handler.logicalDeviceMgr.ListLogicalDeviceFlowGroups(ctx, id.Id)
}

func (handler *APIHandler) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	log.Debugw("ListLogicalDevicePorts", log.Fields{"logicaldeviceid": id})
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return &voltha.LogicalPorts{}, err
		} else {
			defer txn.Close()
		}
	}
	return handler.logicalDeviceMgr.ListLogicalDevicePorts(ctx, id.Id)
}

// CreateDevice creates a new parent device in the data model
func (handler *APIHandler) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	log.Debugw("createdevice", log.Fields{"device": *device})
	if isTestMode(ctx) {
		return &voltha.Device{Id: device.Id}, nil
	}

	if handler.competeForTransaction() {
		// There are no device Id present in this function.
		if txn, err := handler.acquireRequest(ctx); err != nil {
			return &voltha.Device{}, err
		} else {
			defer txn.Close()
		}
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
				handler.core.deviceOwnership.OwnedByMe(&utils.DeviceID{Id: d.Id})
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

// EnableDevice activates a device by invoking the adopt_device API on the appropriate adapter
func (handler *APIHandler) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("enabledevice", log.Fields{"id": id})
	if isTestMode(ctx) {
		return new(empty.Empty), nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: id.Id}, handler.longRunningRequestTimeout); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.enableDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// DisableDevice disables a device along with any child device it may have
func (handler *APIHandler) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("disabledevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		return new(empty.Empty), nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: id.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.disableDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

//RebootDevice invoked the reboot API to the corresponding adapter
func (handler *APIHandler) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("rebootDevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		return new(empty.Empty), nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: id.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.rebootDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// DeleteDevice removes a device from the data model
func (handler *APIHandler) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	log.Debugw("deletedevice-request", log.Fields{"id": id})
	if isTestMode(ctx) {
		return new(empty.Empty), nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: id.Id}); err != nil {
			// Remove the device in memory
			if err.Error() == (errors.New(string(COMPLETED_BY_OTHER)).Error()) {
				handler.deviceMgr.stopManagingDevice(id.Id)
			}
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.deleteDevice(ctx, id, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

// processImageRequest is a helper method to execute an image download request
func (handler *APIHandler) processImageRequest(ctx context.Context, img *voltha.ImageDownload, requestType int) (*common.OperationResp, error) {
	log.Debugw("processImageDownload", log.Fields{"img": *img, "requestType": requestType})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: img.Id}); err != nil {
			return &common.OperationResp{}, err
		} else {
			defer txn.Close()
		}
	}

	failedresponse := &common.OperationResp{Code: voltha.OperationResp_OPERATION_FAILURE}

	ch := make(chan interface{})
	defer close(ch)
	switch requestType {
	case IMAGE_DOWNLOAD:
		go handler.deviceMgr.downloadImage(ctx, img, ch)
	case CANCEL_IMAGE_DOWNLOAD:
		go handler.deviceMgr.cancelImageDownload(ctx, img, ch)
	case ACTIVATE_IMAGE:
		go handler.deviceMgr.activateImage(ctx, img, ch)
	case REVERT_IMAGE:
		go handler.deviceMgr.revertImage(ctx, img, ch)
	default:
		log.Warn("invalid-request-type", log.Fields{"requestType": requestType})
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
		log.Warnw("download-image-unexpected-return-type", log.Fields{"result": res})
		return failedresponse, status.Errorf(codes.Internal, "%s", res)
	case <-ctx.Done():
		log.Debug("downloadImage-client-timeout")
		return nil, ctx.Err()
	}
}

func (handler *APIHandler) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("DownloadImage-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return handler.processImageRequest(ctx, img, IMAGE_DOWNLOAD)
}

func (handler *APIHandler) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("cancelImageDownload-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}
	return handler.processImageRequest(ctx, img, CANCEL_IMAGE_DOWNLOAD)
}

func (handler *APIHandler) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("activateImageUpdate-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return handler.processImageRequest(ctx, img, ACTIVATE_IMAGE)
}

func (handler *APIHandler) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	log.Debugw("revertImageUpdate-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		return resp, nil
	}

	return handler.processImageRequest(ctx, img, REVERT_IMAGE)
}

func (handler *APIHandler) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("getImageDownloadStatus-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_SUCCEEDED}
		return resp, nil
	}

	failedresponse := &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: img.Id}); err != nil {
			return failedresponse, err
		} else {
			defer txn.Close()
		}
	}

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
		log.Warnw("download-image-status", log.Fields{"result": res})
		return failedresponse, status.Errorf(codes.Internal, "%s", res)
	case <-ctx.Done():
		log.Debug("downloadImage-client-timeout")
		return failedresponse, ctx.Err()
	}
}

func (handler *APIHandler) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownload-request", log.Fields{"img": *img})
	if isTestMode(ctx) {
		resp := &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_SUCCEEDED}
		return resp, nil
	}

	if download, err := handler.deviceMgr.getImageDownload(ctx, img); err != nil {
		return &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN}, err
	} else {
		return download, nil
	}
}

func (handler *APIHandler) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads-request", log.Fields{"deviceId": id.Id})
	if isTestMode(ctx) {
		resp := &voltha.ImageDownloads{Items: []*voltha.ImageDownload{}}
		return resp, nil
	}

	if downloads, err := handler.deviceMgr.listImageDownloads(ctx, id.Id); err != nil {
		failedResp := &voltha.ImageDownloads{
			Items: []*voltha.ImageDownload{
				{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN},
			},
		}
		return failedResp, err
	} else {
		return downloads, nil
	}
}

func (handler *APIHandler) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	log.Debugw("UpdateDevicePmConfigs-request", log.Fields{"configs": *configs})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: configs.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.updatePmConfigs(ctx, configs, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}

func (handler *APIHandler) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs-request", log.Fields{"deviceId": *id})
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return &voltha.PmConfigs{}, err
		} else {
			defer txn.Close()
		}
	}
	return handler.deviceMgr.listPmConfigs(ctx, id.Id)
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

func (handler *APIHandler) forwardPacketOut(packet *openflow_13.PacketOut) {
	log.Debugw("forwardPacketOut-request", log.Fields{"packet": packet})
	//TODO: Update this logic once the OF Controller (OFAgent in this case) can include a transaction Id in its
	// request.  For performance reason we can let both Cores in a Core-Pair forward the Packet to the adapters and
	// let once of the shim layer (kafka proxy or adapter request handler filters out the duplicate packet)
	if handler.core.deviceOwnership.OwnedByMe(&utils.LogicalDeviceID{Id: packet.Id}) {
		agent := handler.logicalDeviceMgr.getLogicalDeviceAgent(packet.Id)
		agent.packetOut(packet.PacketOut)
	}
}

func (handler *APIHandler) StreamPacketsOut(
	packets voltha.VolthaService_StreamPacketsOutServer,
) error {
	log.Debugw("StreamPacketsOut-request", log.Fields{"packets": packets})
loop:
	for {
		select {
		case <-packets.Context().Done():
			log.Infow("StreamPacketsOut-context-done", log.Fields{"packets": packets, "error": packets.Context().Err()})
			break loop
		default:
		}

		packet, err := packets.Recv()

		if err == io.EOF {
			log.Debugw("Received-EOF", log.Fields{"packets": packets})
			break loop
		}

		if err != nil {
			log.Errorw("Failed to receive packet out", log.Fields{"error": err})
			continue
		}

		handler.forwardPacketOut(packet)
	}

	log.Debugw("StreamPacketsOut-request-done", log.Fields{"packets": packets})
	return nil
}

func (handler *APIHandler) sendPacketIn(deviceId string, transationId string, packet *openflow_13.OfpPacketIn) {
	// TODO: Augment the OF PacketIn to include the transactionId
	packetIn := openflow_13.PacketIn{Id: deviceId, PacketIn: packet}
	log.Debugw("sendPacketIn", log.Fields{"packetIn": packetIn})
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
		log.Debugf("%s streaming call already running. Exiting it", method)
		done <- true
		log.Debugf("Last %s exited. Continuing ...", method)
	} else {
		streamingTracker.calls[method] = &callTracker{failedPacket: nil}
	}
	return streamingTracker.calls[method]
}

func (handler *APIHandler) flushFailedPackets(tracker *callTracker) error {
	if tracker.failedPacket != nil {
		switch tracker.failedPacket.(type) {
		case openflow_13.PacketIn:
			log.Debug("Enqueueing last failed packetIn")
			handler.packetInQueue <- tracker.failedPacket.(openflow_13.PacketIn)
		case openflow_13.ChangeEvent:
			log.Debug("Enqueueing last failed changeEvent")
			handler.changeEventQueue <- tracker.failedPacket.(openflow_13.ChangeEvent)
		}
	}
	return nil
}

func (handler *APIHandler) ReceivePacketsIn(
	empty *empty.Empty,
	packetsIn voltha.VolthaService_ReceivePacketsInServer,
) error {
	var streamingTracker = handler.getStreamingTracker("ReceivePacketsIn", handler.packetInQueueDone)
	log.Debugw("ReceivePacketsIn-request", log.Fields{"packetsIn": packetsIn})

	handler.flushFailedPackets(streamingTracker)

loop:
	for {
		select {
		case packet := <-handler.packetInQueue:
			log.Debugw("sending-packet-in", log.Fields{"packet": packet})
			if err := packetsIn.Send(&packet); err != nil {
				log.Errorw("failed-to-send-packet", log.Fields{"error": err})
				// save the last failed packet in
				streamingTracker.failedPacket = packet
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed packet saved to avoid flush
					streamingTracker.failedPacket = nil
				}
			}
		case <-handler.packetInQueueDone:
			log.Debug("Another ReceivePacketsIn running. Bailing out ...")
			break loop
		}
	}

	//TODO: Find an elegant way to get out of the above loop when the Core is stopped
	return nil
}

func (handler *APIHandler) sendChangeEvent(deviceId string, portStatus *openflow_13.OfpPortStatus) {
	// TODO: validate the type of portStatus parameter
	//if _, ok := portStatus.(*openflow_13.OfpPortStatus); ok {
	//}
	event := openflow_13.ChangeEvent{Id: deviceId, Event: &openflow_13.ChangeEvent_PortStatus{PortStatus: portStatus}}
	log.Debugw("sendChangeEvent", log.Fields{"event": event})
	handler.changeEventQueue <- event
}

func (handler *APIHandler) ReceiveChangeEvents(
	empty *empty.Empty,
	changeEvents voltha.VolthaService_ReceiveChangeEventsServer,
) error {
	var streamingTracker = handler.getStreamingTracker("ReceiveChangeEvents", handler.changeEventQueueDone)
	log.Debugw("ReceiveChangeEvents-request", log.Fields{"changeEvents": changeEvents})

	handler.flushFailedPackets(streamingTracker)

loop:
	for {
		select {
		// Dequeue a change event
		case event := <-handler.changeEventQueue:
			log.Debugw("sending-change-event", log.Fields{"event": event})
			if err := changeEvents.Send(&event); err != nil {
				log.Errorw("failed-to-send-change-event", log.Fields{"error": err})
				// save last failed changeevent
				streamingTracker.failedPacket = event
			} else {
				if streamingTracker.failedPacket != nil {
					// reset last failed event saved on success to avoid flushing
					streamingTracker.failedPacket = nil
				}
			}
		case <-handler.changeEventQueueDone:
			log.Debug("Another ReceiveChangeEvents already running. Bailing out ...")
			break loop
		}
	}

	return nil
}

func (handler *APIHandler) Subscribe(
	ctx context.Context,
	ofAgent *voltha.OfAgentSubscriber,
) (*voltha.OfAgentSubscriber, error) {
	log.Debugw("Subscribe-request", log.Fields{"ofAgent": ofAgent})
	return &voltha.OfAgentSubscriber{OfagentId: ofAgent.OfagentId, VolthaId: ofAgent.VolthaId}, nil
}

//@TODO useless stub, what should this actually do?
func (handler *APIHandler) GetAlarmDeviceData(
	ctx context.Context,
	in *common.ID,
) (*omci.AlarmDeviceData, error) {
	log.Debug("GetAlarmDeviceData-stub")
	return nil, nil
}

func (handler *APIHandler) ListLogicalDeviceMeters(ctx context.Context, id *voltha.ID) (*openflow_13.Meters, error) {

	log.Debugw("ListLogicalDeviceMeters", log.Fields{"id": *id})
	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: id.Id}); err != nil {
			return nil, err // TODO: Return empty meter entry
		} else {
			defer txn.Close()
		}
	}
	return handler.logicalDeviceMgr.ListLogicalDeviceMeters(ctx, id.Id)
}

//@TODO useless stub, what should this actually do?
func (handler *APIHandler) GetMibDeviceData(
	ctx context.Context,
	in *common.ID,
) (*omci.MibDeviceData, error) {
	log.Debug("GetMibDeviceData-stub")
	return nil, nil
}

func (handler *APIHandler) SimulateAlarm(
	ctx context.Context,
	in *voltha.SimulateAlarmRequest,
) (*common.OperationResp, error) {
	log.Debugw("SimulateAlarm-request", log.Fields{"id": in.Id})
	successResp := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
	if isTestMode(ctx) {
		return successResp, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.DeviceID{Id: in.Id}, handler.longRunningRequestTimeout); err != nil {
			failedresponse := &common.OperationResp{Code: voltha.OperationResp_OPERATION_FAILURE}
			return failedresponse, err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.deviceMgr.simulateAlarm(ctx, in, ch)
	return successResp, nil
}

// This function sends meter mod request to logical device manager and waits for response
func (handler *APIHandler) UpdateLogicalDeviceMeterTable(ctx context.Context, meter *openflow_13.MeterModUpdate) (*empty.Empty, error) {
	log.Debugw("UpdateLogicalDeviceMeterTable-request",
		log.Fields{"meter": meter, "test": common.TestModeKeys_api_test.String()})
	if isTestMode(ctx) {
		out := new(empty.Empty)
		return out, nil
	}

	if handler.competeForTransaction() {
		if txn, err := handler.takeRequestOwnership(ctx, &utils.LogicalDeviceID{Id: meter.Id}); err != nil {
			return new(empty.Empty), err
		} else {
			defer txn.Close()
		}
	}

	ch := make(chan interface{})
	defer close(ch)
	go handler.logicalDeviceMgr.updateMeterTable(ctx, meter.Id, meter.MeterMod, ch)
	return waitForNilResponseOnSuccess(ctx, ch)
}
