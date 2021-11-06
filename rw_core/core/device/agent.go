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

package device

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/core"
	"github.com/opencord/voltha-protos/v5/go/omci"

	"github.com/cenkalti/backoff/v3"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/flow"
	"github.com/opencord/voltha-go/rw_core/core/device/group"
	"github.com/opencord/voltha-go/rw_core/core/device/port"
	"github.com/opencord/voltha-go/rw_core/core/device/transientstate"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/extension"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

var errReconcileAborted = errors.New("reconcile aborted")
var errContextExpired = errors.New("context expired")
var errNoConnection = errors.New("no connection")

// Agent represents device agent attributes
type Agent struct {
	deviceID             string
	parentID             string
	deviceType           string
	adapterEndpoint      string
	isRootDevice         bool
	adapterMgr           *adapter.Manager
	deviceMgr            *Manager
	dbProxy              *model.Proxy
	exitChannel          chan int
	device               *voltha.Device
	requestQueue         *coreutils.RequestQueue
	internalTimeout      time.Duration
	rpcTimeout           time.Duration
	startOnce            sync.Once
	stopOnce             sync.Once
	stopped              bool
	stopReconciling      chan int
	stopReconcilingMutex sync.RWMutex
	config               *config.RWCoreFlags

	flowCache            *flow.Cache
	groupCache           *group.Cache
	portLoader           *port.Loader
	transientStateLoader *transientstate.Loader
}

//newAgent creates a new device agent. The device will be initialized when start() is called.
func newAgent(device *voltha.Device, deviceMgr *Manager, dbPath *model.Path, deviceProxy *model.Proxy, internalTimeout, rpcTimeout time.Duration) *Agent {
	deviceID := device.Id
	if deviceID == "" {
		deviceID = coreutils.CreateDeviceID()
	}

	return &Agent{
		deviceID:             deviceID,
		isRootDevice:         device.Root,
		parentID:             device.ParentId,
		deviceType:           device.Type,
		adapterEndpoint:      device.AdapterEndpoint,
		deviceMgr:            deviceMgr,
		adapterMgr:           deviceMgr.adapterMgr,
		exitChannel:          make(chan int, 1),
		dbProxy:              deviceProxy,
		internalTimeout:      internalTimeout,
		rpcTimeout:           rpcTimeout,
		device:               proto.Clone(device).(*voltha.Device),
		requestQueue:         coreutils.NewRequestQueue(),
		config:               deviceMgr.config,
		flowCache:            flow.NewCache(),
		groupCache:           group.NewCache(),
		portLoader:           port.NewLoader(dbPath.SubPath("ports").Proxy(deviceID)),
		transientStateLoader: transientstate.NewLoader(dbPath.SubPath("core").Proxy("transientstate"), deviceID),
	}
}

// start() saves the device to the data model and registers for callbacks on that device if deviceToCreate!=nil.
// Otherwise, it will load the data from the dB and setup the necessary callbacks and proxies. Returns the device that
// was started.
func (agent *Agent) start(ctx context.Context, deviceExist bool, deviceToCreate *voltha.Device) (*voltha.Device, error) {
	needToStart := false
	if agent.startOnce.Do(func() { needToStart = true }); !needToStart {
		return agent.getDeviceReadOnly(ctx)
	}
	var startSucceeded bool
	defer func() {
		if !startSucceeded {
			if err := agent.stop(ctx); err != nil {
				logger.Errorw(ctx, "failed-to-cleanup-after-unsuccessful-start", log.Fields{"device-id": agent.deviceID, "error": err})
			}
		}
	}()
	if deviceExist {
		device := deviceToCreate
		if device == nil {
			// Load from dB
			device = &voltha.Device{}
			have, err := agent.dbProxy.Get(ctx, agent.deviceID, device)
			if err != nil {
				return nil, err
			} else if !have {
				return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
			}
			logger.Infow(ctx, "device-loaded-from-db", log.Fields{"device-id": agent.deviceID, "adapter-endpoint": device.AdapterEndpoint, "type": device.Type})
		}
		agent.deviceType = device.Type
		agent.adapterEndpoint = device.AdapterEndpoint
		agent.device = proto.Clone(device).(*voltha.Device)
		// load the ports from KV to cache
		agent.portLoader.Load(ctx)
		agent.transientStateLoader.Load(ctx)
	} else {
		// Create a new device
		var desc string
		var err error
		prevState := common.AdminState_UNKNOWN
		currState := common.AdminState_UNKNOWN
		requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}

		defer func() { agent.logDeviceUpdate(ctx, &prevState, &currState, requestStatus, err, desc) }()

		// Assumption is that AdminState, FlowGroups, and Flows are uninitialized since this
		// is a new device, so populate them here before passing the device to ldProxy.Set.
		// agent.deviceId will also have been set during newAgent().
		device := (proto.Clone(deviceToCreate)).(*voltha.Device)
		device.Id = agent.deviceID
		device.AdminState = voltha.AdminState_PREPROVISIONED
		currState = device.AdminState
		if !deviceToCreate.GetRoot() && deviceToCreate.ProxyAddress != nil {
			// Set the default vlan ID to the one specified by the parent adapter.  It can be
			// overwritten by the child adapter during a device update request
			device.Vlan = deviceToCreate.ProxyAddress.ChannelId
		}

		// Save the device to the model
		if err = agent.dbProxy.Set(ctx, agent.deviceID, device); err != nil {
			err = status.Errorf(codes.Aborted, "failed-adding-device-%s: %s", agent.deviceID, err)
			return nil, err
		}
		_ = agent.deviceMgr.Agent.SendDeviceStateChangeEvent(ctx, device.OperStatus, device.ConnectStatus, prevState, device, time.Now().Unix())
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
		agent.device = device
	}
	startSucceeded = true
	log.EnrichSpan(ctx, log.Fields{"device-id": agent.deviceID})
	logger.Debugw(ctx, "device-agent-started", log.Fields{"device-id": agent.deviceID})

	return agent.getDeviceReadOnly(ctx)
}

// stop stops the device agent.  Not much to do for now
func (agent *Agent) stop(ctx context.Context) error {
	needToStop := false
	if agent.stopOnce.Do(func() { needToStop = true }); !needToStop {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Infow(ctx, "stopping-device-agent", log.Fields{"device-id": agent.deviceID, "parent-id": agent.parentID})
	// Remove the device transient loader
	if err := agent.deleteTransientState(ctx); err != nil {
		return err
	}
	//	Remove the device from the KV store
	if err := agent.dbProxy.Remove(ctx, agent.deviceID); err != nil {
		return err
	}

	close(agent.exitChannel)

	agent.stopped = true

	logger.Infow(ctx, "device-agent-stopped", log.Fields{"device-id": agent.deviceID, "parent-id": agent.parentID})

	return nil
}

// Load the most recent state from the KVStore for the device.
func (agent *Agent) reconcileWithKVStore(ctx context.Context) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Warnw(ctx, "request-aborted", log.Fields{"device-id": agent.deviceID, "error": err})
		return
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debug(ctx, "reconciling-device-agent-devicetype")
	// TODO: context timeout
	device := &voltha.Device{}
	if have, err := agent.dbProxy.Get(ctx, agent.deviceID, device); err != nil {
		logger.Errorw(ctx, "kv-get-failed", log.Fields{"device-id": agent.deviceID, "error": err})
		return
	} else if !have {
		return // not found in kv
	}

	agent.deviceType = device.Type
	agent.device = device
	agent.adapterEndpoint = device.AdapterEndpoint
	agent.portLoader.Load(ctx)
	agent.transientStateLoader.Load(ctx)

	logger.Debugw(ctx, "reconciled-device-agent-devicetype", log.Fields{"device-id": agent.deviceID, "type": agent.deviceType})
}

// onSuccess is a common callback for scenarios where we receive a nil response following a request to an adapter
func (agent *Agent) onSuccess(ctx context.Context, prevState, currState *common.AdminState_Types, deviceUpdateLog bool) {
	if deviceUpdateLog {
		requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		desc := "adapter-response"
		agent.logDeviceUpdate(ctx, prevState, currState, requestStatus, nil, desc)
		return
	}
	logger.Debugw(ctx, "successful-operation", log.Fields{"device-id": agent.deviceID, "rpc": coreutils.GetRPCMetadataFromContext(ctx)})
}

// onFailure is a common callback for scenarios where we receive an error response following a request to an adapter
// and the only action required is to publish the failed result on kafka
func (agent *Agent) onFailure(ctx context.Context, err error, prevState, currState *common.AdminState_Types, deviceUpdateLog bool) {
	// Send an event on kafka
	rpce := agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
	go agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce,
		voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())

	// Log the device update event
	if deviceUpdateLog {
		requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
		desc := "adapter-response"
		agent.logDeviceUpdate(ctx, prevState, currState, requestStatus, err, desc)
		return
	}
	logger.Errorw(ctx, "failed-operation", log.Fields{"error": err, "device-id": agent.deviceID, "rpc": coreutils.GetRPCMetadataFromContext(ctx)})
}

// onDeleteSuccess is a common callback for scenarios where we receive a nil response following a delete request
// to an adapter.
func (agent *Agent) onDeleteSuccess(ctx context.Context, prevState, currState *common.AdminState_Types) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Errorw(ctx, "delete-device-failure", log.Fields{"device-id": agent.deviceID, "error": err})
	}
	previousDeviceTransientState := agent.getTransientState()
	newDevice := agent.cloneDeviceWithoutLock()
	if err := agent.updateDeviceWithTransientStateAndReleaseLock(ctx, newDevice,
		core.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE, previousDeviceTransientState); err != nil {
		logger.Errorw(ctx, "delete-device-failure", log.Fields{"device-id": agent.deviceID, "error": err})
	}
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
	desc := "adapter-response"
	agent.logDeviceUpdate(ctx, prevState, currState, requestStatus, nil, desc)
}

// onDeleteFailure is a common callback for scenarios where we receive an error response following a delete request
//  to an adapter and the only action required is to return the error response.
func (agent *Agent) onDeleteFailure(ctx context.Context, err error, prevState, currState *common.AdminState_Types) {
	logger.Errorw(ctx, "rpc-failed", log.Fields{"rpc": coreutils.GetRPCMetadataFromContext(ctx), "device-id": agent.deviceID, "error": err})

	//Only updating of transient state is required, no transition.
	if er := agent.updateTransientState(ctx, core.DeviceTransientState_DELETE_FAILED); er != nil {
		logger.Errorw(ctx, "failed-to-update-transient-state-as-delete-failed", log.Fields{"device-id": agent.deviceID, "error": er})
	}
	rpce := agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
	go agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce,
		voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())

	// Log the device update event
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	desc := "adapter-response"
	agent.logDeviceUpdate(ctx, prevState, currState, requestStatus, err, desc)
}

// getDeviceReadOnly returns a device which MUST NOT be modified, but is safe to keep forever.
func (agent *Agent) getDeviceReadOnly(ctx context.Context) (*voltha.Device, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	return agent.device, nil
}

// getDeviceReadOnlyWithoutLock returns a device which MUST NOT be modified, but is safe to keep forever.  This is very efficient.
// The device lock MUST be held by the caller.
func (agent *Agent) getDeviceReadOnlyWithoutLock() *voltha.Device {
	return agent.device
}

// cloneDeviceWithoutLock returns a copy of the device which is safe to modify.
// The device lock MUST be held by the caller.
func (agent *Agent) cloneDeviceWithoutLock() *voltha.Device {
	return proto.Clone(agent.device).(*voltha.Device)
}

func (agent *Agent) updateDeviceTypeAndEndpoint(ctx context.Context) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	changed := false
	cloned := agent.cloneDeviceWithoutLock()
	if cloned.Type == "" {
		adapterType, err := agent.adapterMgr.GetAdapterType(cloned.Type)
		if err != nil {
			agent.requestQueue.RequestComplete()
			return err
		}
		cloned.Type = adapterType
		changed = true
	}

	if cloned.AdapterEndpoint == "" {
		var err error
		if cloned.AdapterEndpoint, err = agent.adapterMgr.GetAdapterEndpoint(ctx, cloned.Id, cloned.Type); err != nil {
			agent.requestQueue.RequestComplete()
			return err
		}
		agent.adapterEndpoint = cloned.AdapterEndpoint
		changed = true
	}

	if changed {
		return agent.updateDeviceAndReleaseLock(ctx, cloned)
	}
	agent.requestQueue.RequestComplete()
	return nil
}

// enableDevice activates a preprovisioned or a disable device
func (agent *Agent) enableDevice(ctx context.Context) error {
	//To preserve and use oldDevice state as prev state in new device
	var err error
	var desc string
	var prevAdminState, currAdminState common.AdminState_Types
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}

	defer func() { agent.logDeviceUpdate(ctx, &prevAdminState, &currAdminState, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "enable-device", log.Fields{"device-id": agent.deviceID})

	oldDevice := agent.getDeviceReadOnlyWithoutLock()
	prevAdminState = oldDevice.AdminState

	if !agent.proceedWithRequest(oldDevice) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "cannot complete operation as device deletion is in progress or reconciling is in progress/failed: %s", agent.deviceID)
		return err
	}
	//vol-4275 TST meeting 08/04/2021: Let EnableDevice to be called again if device is in FAILED operational state,
	//even the admin state is ENABLED.
	if oldDevice.AdminState == voltha.AdminState_ENABLED && oldDevice.OperStatus != voltha.OperStatus_FAILED {
		logger.Warnw(ctx, "device-already-enabled", log.Fields{"device-id": agent.deviceID})
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, fmt.Sprintf("cannot-enable-an-already-enabled-device: %s", oldDevice.Id))
		return err
	}

	// Verify whether there is a device type that supports this device type
	_, err = agent.adapterMgr.GetAdapterType(oldDevice.Type)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return err
	}

	// Update device adapter endpoint if not set.  This is set once by the Core and use as is by the adapters.  E.g if this is a
	// child device then the parent adapter will use this device's adapter endpoint (set here) to communicate with it.
	newDevice := agent.cloneDeviceWithoutLock()
	if newDevice.AdapterEndpoint == "" {
		if newDevice.AdapterEndpoint, err = agent.adapterMgr.GetAdapterEndpoint(ctx, newDevice.Id, newDevice.Type); err != nil {
			agent.requestQueue.RequestComplete()
			return err
		}
		agent.adapterEndpoint = newDevice.AdapterEndpoint
	}

	// Update the Admin State and set the operational state to activating before sending the request to the Adapters
	newDevice.AdminState = voltha.AdminState_ENABLED
	newDevice.OperStatus = voltha.OperStatus_ACTIVATING

	// Adopt the device if it was in pre-provision state.  In all other cases, try to re-enable it.
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": newDevice.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		var err error
		if oldDevice.AdminState == voltha.AdminState_PREPROVISIONED {
			_, err = client.AdoptDevice(subCtx, newDevice)
		} else {
			_, err = client.ReEnableDevice(subCtx, newDevice)
		}
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()

	// Update device
	if err = agent.updateDeviceAndReleaseLock(ctx, newDevice); err != nil {
		return err
	}
	currAdminState = newDevice.AdminState
	return nil
}

//addFlowsAndGroups adds the "newFlows" and "newGroups" from the existing flows/groups and sends the update to the
//adapters
func (agent *Agent) addFlowsAndGroups(ctx context.Context, newFlows []*ofp.OfpFlowStats, newGroups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) error {
	var flwResponse, grpResponse coreutils.Response
	var err error
	//if new flow list is empty then the called function returns quickly
	if flwResponse, err = agent.addFlowsToAdapter(ctx, newFlows, flowMetadata); err != nil {
		return err
	}
	//if new group list is empty then the called function returns quickly
	if grpResponse, err = agent.addGroupsToAdapter(ctx, newGroups, flowMetadata); err != nil {
		return err
	}
	if errs := coreutils.WaitForNilOrErrorResponses(agent.rpcTimeout, flwResponse, grpResponse); errs != nil {
		logger.Warnw(ctx, "adapter-response", log.Fields{"device-id": agent.deviceID, "result": errs})
		return status.Errorf(codes.Aborted, "flow-failure-device-%s", agent.deviceID)
	}
	return nil
}

//deleteFlowsAndGroups removes the "flowsToDel" and "groupsToDel" from the existing flows/groups and sends the update to the
//adapters
func (agent *Agent) deleteFlowsAndGroups(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) error {
	var flwResponse, grpResponse coreutils.Response
	var err error
	if flwResponse, err = agent.deleteFlowsFromAdapter(ctx, flowsToDel, flowMetadata); err != nil {
		return err
	}
	if grpResponse, err = agent.deleteGroupsFromAdapter(ctx, groupsToDel, flowMetadata); err != nil {
		return err
	}

	if res := coreutils.WaitForNilOrErrorResponses(agent.rpcTimeout, flwResponse, grpResponse); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//updateFlowsAndGroups replaces the existing flows and groups with "updatedFlows" and "updatedGroups" respectively. It
//also sends the updates to the adapters
func (agent *Agent) updateFlowsAndGroups(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) error {
	var flwResponse, grpResponse coreutils.Response
	var err error
	if flwResponse, err = agent.updateFlowsToAdapter(ctx, updatedFlows, flowMetadata); err != nil {
		return err
	}
	if grpResponse, err = agent.updateGroupsToAdapter(ctx, updatedGroups, flowMetadata); err != nil {
		return err
	}

	if res := coreutils.WaitForNilOrErrorResponses(agent.rpcTimeout, flwResponse, grpResponse); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//disableDevice disable a device
func (agent *Agent) disableDevice(ctx context.Context) error {
	var err error
	var desc string
	var prevAdminState, currAdminState common.AdminState_Types
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, &prevAdminState, &currAdminState, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "disable-device", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()
	prevAdminState = agent.device.AdminState

	if !agent.proceedWithRequest(cloned) {
		err = status.Errorf(codes.FailedPrecondition, "cannot complete operation as device deletion is in progress or reconciling is in progress/failed: %s", agent.deviceID)
		agent.requestQueue.RequestComplete()
		return err
	}

	if cloned.AdminState == voltha.AdminState_DISABLED {
		desc = "device-already-disabled"
		agent.requestQueue.RequestComplete()
		return nil
	}
	if cloned.AdminState == voltha.AdminState_PREPROVISIONED {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "deviceId:%s, invalid-admin-state:%s", agent.deviceID, cloned.AdminState)
		return err
	}

	// Update the Admin State and operational state before sending the request out
	cloned.AdminState = voltha.AdminState_DISABLED
	cloned.OperStatus = voltha.OperStatus_UNKNOWN

	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err := client.DisableDevice(subCtx, cloned)
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()

	// Update device
	if err = agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return err
	}
	currAdminState = cloned.AdminState

	return nil
}

func (agent *Agent) rebootDevice(ctx context.Context) error {
	var desc string
	var err error
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		desc = err.Error()
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "reboot-device", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceReadOnlyWithoutLock()

	if !agent.proceedWithRequest(device) {
		err = status.Errorf(codes.FailedPrecondition, "cannot complete operation as device deletion is in progress or reconciling is in progress/failed:%s", agent.deviceID)
		return err
	}

	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err := client.RebootDevice(subCtx, device)
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()
	return nil
}

func (agent *Agent) deleteDeviceForce(ctx context.Context) error {
	logger.Debugw(ctx, "delete-device-force", log.Fields{"device-id": agent.deviceID})

	var desc string
	var err error
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	// Get the device Transient state, return err if it is DELETING
	previousDeviceTransientState := agent.getTransientState()
	device := agent.cloneDeviceWithoutLock()
	if !agent.isForceDeletingAllowed(previousDeviceTransientState, device) {
		agent.requestQueue.RequestComplete()
		err = status.Error(codes.FailedPrecondition, fmt.Sprintf("deviceId:%s, force deletion is in progress", agent.deviceID))
		return err
	}

	previousAdminState := device.AdminState
	if previousAdminState != common.AdminState_PREPROVISIONED {
		var client adapter_service.AdapterServiceClient
		client, err = agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
		if err != nil {
			logger.Errorw(ctx, "grpc-client-nil",
				log.Fields{
					"error":            err,
					"device-id":        agent.deviceID,
					"device-type":      agent.deviceType,
					"adapter-endpoint": device.AdapterEndpoint,
				})
			agent.requestQueue.RequestComplete()
			return fmt.Errorf("remote-not-reachable %w", errNoConnection)
		}
		subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
		requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
		go func() {
			defer cancel()
			_, err := client.DeleteDevice(subCtx, device)
			if err == nil {
				agent.onSuccess(subCtx, nil, nil, true)
			} else {
				agent.onFailure(subCtx, err, nil, nil, true)
			}
		}()
	}

	// Update device
	if err = agent.updateDeviceWithTransientStateAndReleaseLock(ctx, device,
		core.DeviceTransientState_FORCE_DELETING, previousDeviceTransientState); err != nil {
		return err
	}
	return nil
}

func (agent *Agent) deleteDevice(ctx context.Context) error {
	logger.Debugw(ctx, "delete-device", log.Fields{"device-id": agent.deviceID})

	var desc string
	var err error
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		desc = err.Error()
		return err
	}

	device := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(device) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "cannot complete operation as device deletion is in progress or reconciling is in progress/failed: %s", agent.deviceID)
		return err
	}

	// Get the device Transient state, return err if it is DELETING
	previousDeviceTransientState := agent.getTransientState()

	previousAdminState := device.AdminState
	// Change the device transient state to DELETING_FROM_ADAPTER  state till the device is removed from adapters.
	currentDeviceTransientState := core.DeviceTransientState_DELETING_FROM_ADAPTER

	if previousAdminState == common.AdminState_PREPROVISIONED {
		// Change the state to DELETING POST ADAPTER RESPONSE directly as adapters have no info of the device.
		currentDeviceTransientState = core.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE
	}
	// If the device was in pre-prov state (only parent device are in that state) then do not send the request to the
	// adapter
	if previousAdminState != common.AdminState_PREPROVISIONED {
		var client adapter_service.AdapterServiceClient
		client, err = agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
		if err != nil {
			logger.Errorw(ctx, "grpc-client-nil",
				log.Fields{
					"error":            err,
					"device-id":        agent.deviceID,
					"device-type":      agent.deviceType,
					"adapter-endpoint": device.AdapterEndpoint,
				})
			agent.requestQueue.RequestComplete()
			return err
		}
		subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
		requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
		go func() {
			defer cancel()
			_, err := client.DeleteDevice(subCtx, device)
			if err == nil {
				agent.onDeleteSuccess(subCtx, nil, nil)
			} else {
				agent.onDeleteFailure(subCtx, err, nil, nil)
			}
		}()
	}

	// Update device and release lock
	if err = agent.updateDeviceWithTransientStateAndReleaseLock(ctx, device,
		currentDeviceTransientState, previousDeviceTransientState); err != nil {
		desc = err.Error()
		return err
	}

	return nil
}

func (agent *Agent) setParentID(ctx context.Context, device *voltha.Device, parentID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "set-parent-id", log.Fields{"device-id": device.Id, "parent-id": parentID})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.ParentId = parentID
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

// getSwitchCapability retrieves the switch capability of a parent device
func (agent *Agent) getSwitchCapability(ctx context.Context) (*ca.SwitchCapability, error) {
	logger.Debugw(ctx, "get-switch-capability", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, err
	}

	// Get the gRPC client
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		return nil, err
	}

	return client.GetOfpDeviceInfo(ctx, device)
}

func (agent *Agent) onPacketFailure(ctx context.Context, err error, packet *ofp.OfpPacketOut) {
	logger.Errorw(ctx, "packet-out-error", log.Fields{
		"device-id": agent.deviceID,
		"error":     err.Error(),
		"packet":    hex.EncodeToString(packet.Data),
	})
	rpce := agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
	go agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce,
		voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())
}

func (agent *Agent) packetOut(ctx context.Context, outPort uint32, packet *ofp.OfpPacketOut) error {
	if agent.deviceType == "" {
		agent.reconcileWithKVStore(ctx)
	}
	//	Send packet to adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":       err,
				"device-id":   agent.deviceID,
				"device-type": agent.deviceType,
			})
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	go func() {
		defer cancel()
		_, err := client.SendPacketOut(subCtx, &ca.PacketOut{
			DeviceId:     agent.deviceID,
			EgressPortNo: outPort,
			Packet:       packet,
		})
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, false)
		} else {
			agent.onPacketFailure(subCtx, err, packet)
		}
	}()
	return nil
}

func (agent *Agent) updateDeviceUsingAdapterData(ctx context.Context, device *voltha.Device) error {
	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "update-device-using-adapter-data", log.Fields{"device-id": device.Id})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.Root = device.Root
	cloned.Vendor = device.Vendor
	cloned.Model = device.Model
	cloned.SerialNumber = device.SerialNumber
	cloned.MacAddress = device.MacAddress
	cloned.Vlan = device.Vlan
	cloned.Reason = device.Reason
	cloned.ImageDownloads = device.ImageDownloads
	cloned.OperStatus = device.OperStatus
	cloned.ConnectStatus = device.ConnectStatus
	if err = agent.updateDeviceAndReleaseLock(ctx, cloned); err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return err
}

func (agent *Agent) updateDeviceStatus(ctx context.Context, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	var err error
	var desc string
	opStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, opStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}

	cloned := agent.cloneDeviceWithoutLock()
	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if s, ok := voltha.ConnectStatus_Types_name[int32(connStatus)]; ok {
		logger.Debugw(ctx, "update-device-conn-status", log.Fields{"ok": ok, "val": s})
		cloned.ConnectStatus = connStatus
	}
	if s, ok := voltha.OperStatus_Types_name[int32(operStatus)]; ok {
		logger.Debugw(ctx, "update-device-oper-status", log.Fields{"ok": ok, "val": s})
		cloned.OperStatus = operStatus
	}
	logger.Debugw(ctx, "update-device-status", log.Fields{"device-id": cloned.Id, "oper-status": cloned.OperStatus, "connect-status": cloned.ConnectStatus})
	// Store the device
	if err = agent.updateDeviceAndReleaseLock(ctx, cloned); err == nil {
		opStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return err
}

// TODO: A generic device update by attribute
func (agent *Agent) updateDeviceAttribute(ctx context.Context, name string, value interface{}) {
	if value == nil {
		return
	}

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Warnw(ctx, "request-aborted", log.Fields{"device-id": agent.deviceID, "name": name, "error": err})
		return
	}

	cloned := agent.cloneDeviceWithoutLock()
	updated := false
	s := reflect.ValueOf(cloned).Elem()
	if s.Kind() == reflect.Struct {
		// exported field
		f := s.FieldByName(name)
		if f.IsValid() && f.CanSet() {
			switch f.Kind() {
			case reflect.String:
				f.SetString(value.(string))
				updated = true
			case reflect.Uint32:
				f.SetUint(uint64(value.(uint32)))
				updated = true
			case reflect.Bool:
				f.SetBool(value.(bool))
				updated = true
			}
		}
	}
	logger.Debugw(ctx, "update-field-status", log.Fields{"device-id": cloned.Id, "name": name, "updated": updated})
	//	Save the data

	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		logger.Warnw(ctx, "attribute-update-failed", log.Fields{"attribute": name, "value": value})
	}
}

func (agent *Agent) simulateAlarm(ctx context.Context, simulateReq *voltha.SimulateAlarmRequest) error {
	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "simulate-alarm", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceReadOnlyWithoutLock()

	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err := client.SimulateAlarm(subCtx, &ca.SimulateAlarmMessage{Device: device, Request: simulateReq})
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, false)
		} else {
			agent.onFailure(subCtx, err, nil, nil, false)
		}
	}()
	return nil
}

// This function updates the device in the DB, releases the device lock, and runs any state transitions.
// The calling function MUST hold the device lock.  The caller MUST NOT modify the device after this is called.
func (agent *Agent) updateDeviceAndReleaseLock(ctx context.Context, device *voltha.Device) error {
	// fail early if this agent is no longer valid
	if agent.stopped {
		agent.requestQueue.RequestComplete()
		return errors.New("device-agent-stopped")
	}

	// update in db
	if err := agent.dbProxy.Set(ctx, agent.deviceID, device); err != nil {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.Internal, "failed-update-device:%s: %s", agent.deviceID, err)
	}
	logger.Debugw(ctx, "updated-device-in-store", log.Fields{"device-id: ": agent.deviceID})

	prevDevice := agent.device
	// update the device
	agent.device = device
	//If any of the states has chenged, send the change event.
	if prevDevice.OperStatus != device.OperStatus || prevDevice.ConnectStatus != device.ConnectStatus || prevDevice.AdminState != device.AdminState {
		_ = agent.deviceMgr.Agent.SendDeviceStateChangeEvent(ctx, prevDevice.OperStatus, prevDevice.ConnectStatus, prevDevice.AdminState, device, time.Now().Unix())
	}
	deviceTransientState := agent.getTransientState()

	// release lock before processing transition
	agent.requestQueue.RequestComplete()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)

	if err := agent.deviceMgr.stateTransitions.ProcessTransition(subCtx,
		device, prevDevice, deviceTransientState, deviceTransientState); err != nil {
		logger.Errorw(ctx, "failed-process-transition", log.Fields{"device-id": device.Id, "previous-admin-state": prevDevice.AdminState, "current-admin-state": device.AdminState})
		// Sending RPC EVENT here
		rpce := agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
		agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce, voltha.EventCategory_COMMUNICATION,
			nil, time.Now().Unix())

	}
	return nil
}

// This function updates the device transient in the DB through loader, releases the device lock, and runs any state transitions.
// The calling function MUST hold the device lock.  The caller MUST NOT modify the device after this is called.
func (agent *Agent) updateDeviceWithTransientStateAndReleaseLock(ctx context.Context, device *voltha.Device,
	transientState, prevTransientState core.DeviceTransientState_Types) error {
	// fail early if this agent is no longer valid
	if agent.stopped {
		agent.requestQueue.RequestComplete()
		return errors.New("device-agent-stopped")
	}
	//update device TransientState
	if err := agent.updateTransientState(ctx, transientState); err != nil {
		agent.requestQueue.RequestComplete()
		return err
	}
	// update in db
	if err := agent.dbProxy.Set(ctx, agent.deviceID, device); err != nil {
		//Reverting TransientState update
		if errTransient := agent.updateTransientState(ctx, prevTransientState); errTransient != nil {
			logger.Errorw(ctx, "failed-to-revert-transient-state-update-on-error", log.Fields{"device-id": device.Id,
				"previous-transient-state": prevTransientState, "current-transient-state": transientState, "error": errTransient})
		}
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.Internal, "failed-update-device:%s: %s", agent.deviceID, err)
	}

	logger.Debugw(ctx, "updated-device-in-store", log.Fields{"device-id: ": agent.deviceID})

	prevDevice := agent.device
	// update the device
	agent.device = device
	//If any of the states has chenged, send the change event.
	if prevDevice.OperStatus != device.OperStatus || prevDevice.ConnectStatus != device.ConnectStatus || prevDevice.AdminState != device.AdminState {
		_ = agent.deviceMgr.Agent.SendDeviceStateChangeEvent(ctx, prevDevice.OperStatus, prevDevice.ConnectStatus, prevDevice.AdminState, device, time.Now().Unix())
	}

	// release lock before processing transition
	agent.requestQueue.RequestComplete()
	go func() {
		subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
		if err := agent.deviceMgr.stateTransitions.ProcessTransition(subCtx,
			device, prevDevice, transientState, prevTransientState); err != nil {
			logger.Errorw(ctx, "failed-process-transition", log.Fields{"device-id": device.Id, "previous-admin-state": prevDevice.AdminState, "current-admin-state": device.AdminState})
			// Sending RPC EVENT here
			rpce := agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
			agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce, voltha.EventCategory_COMMUNICATION,
				nil, time.Now().Unix())
		}
	}()
	return nil
}
func (agent *Agent) updateDeviceReason(ctx context.Context, reason string) error {
	logger.Debugw(ctx, "update-device-reason", log.Fields{"device-id": agent.deviceID, "reason": reason})

	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}

	cloned := agent.cloneDeviceWithoutLock()
	cloned.Reason = reason
	if err = agent.updateDeviceAndReleaseLock(ctx, cloned); err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return err
}

func (agent *Agent) ChildDeviceLost(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "child-device-lost", log.Fields{"child-device-id": device.Id, "parent-device-id": agent.deviceID})

	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	// Remove the associated peer ports on the parent device
	for portID := range agent.portLoader.ListIDs() {
		if portHandle, have := agent.portLoader.Lock(portID); have {
			oldPort := portHandle.GetReadOnly()
			updatedPeers := make([]*voltha.Port_PeerPort, 0)
			for _, peerPort := range oldPort.Peers {
				if peerPort.DeviceId != device.Id {
					updatedPeers = append(updatedPeers, peerPort)
				}
			}
			newPort := *oldPort
			newPort.Peers = updatedPeers
			if err := portHandle.Update(ctx, &newPort); err != nil {
				portHandle.Unlock()
				return nil
			}
			portHandle.Unlock()
		}
	}

	//send request to adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err := client.ChildDeviceLost(subCtx, device)
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()
	return nil
}

func (agent *Agent) startOmciTest(ctx context.Context, omcitestrequest *omci.OmciTestRequest) (*omci.TestResponse, error) {
	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	// OMCI test may be performed on a pre-provisioned device.  If a device is in that state both its device type and endpoint
	// may not have been set yet.
	// First check if we need to update the type or endpoint
	cloned, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, err
	}
	if cloned.Type == "" || cloned.AdapterEndpoint == "" {
		if err = agent.updateDeviceTypeAndEndpoint(ctx); err != nil {
			return nil, err
		}
		cloned, err = agent.getDeviceReadOnly(ctx)
		if err != nil {
			return nil, err
		}
	}

	// Send request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		return nil, err
	}

	res, err := client.StartOmciTest(ctx, &ca.OMCITest{
		Device:  cloned,
		Request: omcitestrequest,
	})
	if err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return res, err
}

func (agent *Agent) getExtValue(ctx context.Context, pdevice *voltha.Device, cdevice *voltha.Device, valueparam *extension.ValueSpecifier) (*extension.ReturnValues, error) {
	logger.Debugw(ctx, "get-ext-value", log.Fields{"device-id": agent.deviceID, "onu-id": valueparam.Id, "value-type": valueparam.Value})
	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	//send request to adapter synchronously
	client, err := agent.adapterMgr.GetAdapterClient(ctx, pdevice.AdapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": pdevice.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	// Release lock before sending to adapter
	agent.requestQueue.RequestComplete()

	retVal, err := client.GetExtValue(ctx, &ca.GetExtValueMessage{
		ParentDevice: pdevice,
		ChildDevice:  cdevice,
		ValueType:    valueparam.Value,
	})
	if err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return retVal, err
}

func (agent *Agent) setExtValue(ctx context.Context, device *voltha.Device, value *extension.ValueSet) (*empty.Empty, error) {
	logger.Debugw(ctx, "set-ext-value", log.Fields{"device-id": value.Id})

	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	//send request to adapter
	//send request to adapter synchronously
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	// Release lock before sending request to adapter
	agent.requestQueue.RequestComplete()

	retVal, err := client.SetExtValue(ctx, &ca.SetExtValueMessage{
		Device: device,
		Value:  value,
	})
	if err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return retVal, err
}

func (agent *Agent) getSingleValue(ctx context.Context, request *extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	logger.Debugw(ctx, "get-single-value", log.Fields{"device-id": request.TargetId})

	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	//send request to adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        cloned.Id,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	// Release lock before sending request to adapter
	agent.requestQueue.RequestComplete()

	resp, err := client.GetSingleValue(ctx, request)
	if err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return resp, err
}

func (agent *Agent) setSingleValue(ctx context.Context, request *extension.SingleSetValueRequest) (*extension.SingleSetValueResponse, error) {
	logger.Debugw(ctx, "set-single-value", log.Fields{"device-id": request.TargetId})

	var err error
	var desc string
	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc) }()

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	//send request to adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	// Release lock before sending request to adapter
	agent.requestQueue.RequestComplete()

	resp, err := client.SetSingleValue(ctx, request)
	if err == nil {
		requestStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return resp, err
}

func (agent *Agent) proceedWithRequest(device *voltha.Device) bool {
	return !agent.isDeletionInProgress() && !agent.isInReconcileState(device)
}

func (agent *Agent) stopReconcile() {
	agent.stopReconcilingMutex.Lock()
	if agent.stopReconciling != nil {
		agent.stopReconciling <- 0
	}
	agent.stopReconcilingMutex.Unlock()
}

// abortAllProcessing is invoked when an adapter managing this device is restarted
func (agent *Agent) abortAllProcessing(ctx context.Context) error {
	logger.Infow(ctx, "aborting-current-running-requests", log.Fields{"device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	// If any reconciling is in progress just abort it. The adapter is gone.
	agent.stopReconcile()

	// Update the Core device transient state accordingly
	var updatedState core.DeviceTransientState_Types
	switch agent.getTransientState() {
	case core.DeviceTransientState_RECONCILE_IN_PROGRESS:
		updatedState = core.DeviceTransientState_NONE
	case core.DeviceTransientState_FORCE_DELETING:
		updatedState = core.DeviceTransientState_DELETE_FAILED
	case core.DeviceTransientState_DELETING_FROM_ADAPTER:
		updatedState = core.DeviceTransientState_DELETE_FAILED
	case core.DeviceTransientState_DELETE_FAILED:
		// do not change state
		return nil
	default:
		updatedState = core.DeviceTransientState_NONE
	}
	if err := agent.updateTransientState(ctx, updatedState); err != nil {
		logger.Errorf(ctx, "transient-state-update-failed", log.Fields{"error": err})
		return err
	}
	return nil
}

func (agent *Agent) DeleteDevicePostAdapterRestart(ctx context.Context) error {
	logger.Debugw(ctx, "delete-post-restart", log.Fields{"device-id": agent.deviceID})
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "DelteDevicePostAdapterRestart")

	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	var desc string

	defer func() {
		agent.logDeviceUpdate(ctx, nil, nil, requestStatus, nil, desc)
	}()

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}

	device := agent.getDeviceReadOnlyWithoutLock()
	if device.AdminState == voltha.AdminState_PREPROVISIONED {
		logger.Debugw(ctx, "device-in-preprovisioning-state-reconcile-not-needed", log.Fields{"device-id": device.Id})
		agent.requestQueue.RequestComplete()
		return nil
	}
	// Change device transient state to FORCE_DELETING
	if err := agent.updateTransientState(ctx, core.DeviceTransientState_FORCE_DELETING); err != nil {
		logger.Errorw(ctx, "failure-updating-transient-state", log.Fields{"error": err, "device-id": agent.deviceID})
		agent.requestQueue.RequestComplete()
		return err
	}

	// Ensure we have a valid grpc client available as we have just restarted
	deleteBackoff := backoff.NewExponentialBackOff()
	deleteBackoff.InitialInterval = agent.config.BackoffRetryInitialInterval
	deleteBackoff.MaxElapsedTime = agent.config.BackoffRetryMaxElapsedTime
	deleteBackoff.MaxInterval = agent.config.BackoffRetryMaxInterval
	var backoffTimer *time.Timer
	var err error
	var client adapter_service.AdapterServiceClient
retry:
	for {
		client, err = agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
		if err == nil {
			break retry
		}
		duration := deleteBackoff.NextBackOff()
		if duration == backoff.Stop {
			deleteBackoff.Reset()
			duration = deleteBackoff.NextBackOff()
		}
		backoffTimer = time.NewTimer(duration)
		select {
		case <-backoffTimer.C:
			logger.Debugw(ctx, "backoff-timer-expires", log.Fields{"device-id": agent.deviceID})
		case <-ctx.Done():
			err = ctx.Err()
			break retry
		}
	}
	if backoffTimer != nil && !backoffTimer.Stop() {
		select {
		case <-backoffTimer.C:
		default:
		}
	}
	if err != nil || client == nil {
		agent.requestQueue.RequestComplete()
		return err
	}

	// Invoke force delete on the adapter
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	requestStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	defer cancel()

	// Release the device lock to allow for device state updated
	agent.requestQueue.RequestComplete()
	if _, err = client.DeleteDevice(subCtx, device); err != nil {
		agent.onDeleteFailure(subCtx, err, nil, nil)
	} else {
		agent.onDeleteSuccess(subCtx, nil, nil)
	}
	requestStatus = &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
	desc = "adapter-delete-force-response"
	return nil
}

func (agent *Agent) ReconcileDevice(ctx context.Context) {
	// Do not reconcile if the device was in DELETE_FAILED transient state.  Just invoke the force delete on that device.
	state := agent.getTransientState()
	logger.Debugw(ctx, "starting-reconcile", log.Fields{"device-id": agent.deviceID, "state": state})
	if agent.getTransientState() == core.DeviceTransientState_DELETE_FAILED {
		if err := agent.DeleteDevicePostAdapterRestart(ctx); err != nil {
			logger.Errorw(ctx, "delete-post-restart-failed", log.Fields{"error": err, "device-id": agent.deviceID})
		}
		return
	}

	requestStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	var desc string

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc)
		return
	}

	device := agent.getDeviceReadOnlyWithoutLock()
	if device.AdminState == voltha.AdminState_PREPROVISIONED {
		agent.requestQueue.RequestComplete()
		logger.Debugw(ctx, "device-in-preprovisioning-state-reconcile-not-needed", log.Fields{"device-id": device.Id})
		return
	}

	if !agent.proceedWithRequest(device) {
		agent.requestQueue.RequestComplete()
		err := fmt.Errorf("cannot complete operation as device deletion/reconciling is in progress or reconcile failed for device : %s", device.Id)
		logger.Errorw(ctx, "reconcile-failed", log.Fields{"error": err})
		agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc)
		return
	}

	//set transient state to RECONCILE IN PROGRESS
	err := agent.updateTransientState(ctx, core.DeviceTransientState_RECONCILE_IN_PROGRESS)
	if err != nil {
		agent.requestQueue.RequestComplete()
		logger.Errorw(ctx, "setting-transient-state-failed", log.Fields{"error": err})
		agent.logDeviceUpdate(ctx, nil, nil, requestStatus, nil, desc)
		return
	}

	reconcilingBackoff := backoff.NewExponentialBackOff()
	reconcilingBackoff.InitialInterval = agent.config.BackoffRetryInitialInterval
	reconcilingBackoff.MaxElapsedTime = agent.config.BackoffRetryMaxElapsedTime
	reconcilingBackoff.MaxInterval = agent.config.BackoffRetryMaxInterval

	//making here to keep lifecycle of this channel within the scope of retryReconcile
	agent.stopReconcilingMutex.Lock()
	agent.stopReconciling = make(chan int)
	agent.stopReconcilingMutex.Unlock()

	// defined outside the retry loop so it can be cleaned
	// up when the loop breaks
	var backoffTimer *time.Timer

retry:
	for {
		// If the operations state of the device is RECONCILING_FAILED then we do not
		// want to continue to attempt reconciliation.
		deviceRef := agent.getDeviceReadOnlyWithoutLock()
		if deviceRef.OperStatus == common.OperStatus_RECONCILING_FAILED {
			logger.Warnw(ctx, "reconciling-failed-halting-retries",
				log.Fields{"device-id": device.Id})
			agent.requestQueue.RequestComplete()
			break retry
		}

		// Use an exponential back off to prevent getting into a tight loop
		duration := reconcilingBackoff.NextBackOff()
		//This case should never occur in default case as max elapsed time for backoff is 0(by default) , so it will never return stop
		if duration == backoff.Stop {
			// If we reach a maximum then warn and reset the backoff
			// timer and keep attempting.
			logger.Warnw(ctx, "maximum-reconciling-backoff-reached--resetting-backoff-timer",
				log.Fields{"max-reconciling-backoff": reconcilingBackoff.MaxElapsedTime,
					"device-id": device.Id})
			reconcilingBackoff.Reset()
			duration = reconcilingBackoff.NextBackOff()
		}

		backoffTimer = time.NewTimer(duration)

		logger.Debugw(ctx, "retrying-reconciling", log.Fields{"deviceID": device.Id, "endpoint": device.AdapterEndpoint})
		// Release lock before sending request to adapter
		agent.requestQueue.RequestComplete()

		// Send a reconcile request to the adapter.
		err := agent.sendReconcileRequestToAdapter(ctx, device)
		if errors.Is(err, errContextExpired) || errors.Is(err, errReconcileAborted) {
			logger.Errorw(ctx, "reconcile-aborted", log.Fields{"error": err})
			requestStatus = &common.OperationResp{Code: common.OperationResp_OperationReturnCode(common.OperStatus_FAILED)}
			desc = "aborted"
			agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc)
			break retry
		}
		if err != nil {
			agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc)
			<-backoffTimer.C
			// backoffTimer expired continue
			// Take lock back before retrying
			if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
				agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc)
				break retry
			}
			continue
		}
		// Success
		requestStatus = &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}
		desc = "adapter-response"
		agent.logDeviceUpdate(ctx, nil, nil, requestStatus, err, desc)
		break retry
	}

	// Retry loop is broken, so stop any timers and drain the channel
	if backoffTimer != nil && !backoffTimer.Stop() {

		// As per documentation and stack overflow when a timer is stopped its
		// channel should be drained. The issue is that Stop returns false
		// either if the timer has already been fired "OR" if the timer can be
		// stopped before being fired. This means that in some cases the
		// channel has already be emptied so attempting to read from it means
		// a blocked thread. To get around this use a select so if the
		// channel is already empty the default case hits and we are not
		// blocked.
		select {
		case <-backoffTimer.C:
		default:
		}
	}
}

func (agent *Agent) sendReconcileRequestToAdapter(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "sending-reconcile-to-adapter", log.Fields{"device-id": device.Id, "endpoint": agent.adapterEndpoint})
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		return err
	}
	adapterResponse := make(chan error)
	go func() {
		_, err := client.ReconcileDevice(ctx, device)
		adapterResponse <- err
	}()
	select {
	// wait for response
	case err := <-adapterResponse:
		if err != nil {
			return err
		}
		//In case of success quit retrying and wait for adapter to reset operation state of device
		agent.stopReconcilingMutex.Lock()
		agent.stopReconciling = nil
		agent.stopReconcilingMutex.Unlock()
		return nil

	//if reconciling need to be stopped
	case _, ok := <-agent.stopReconciling:
		agent.stopReconcilingMutex.Lock()
		agent.stopReconciling = nil
		agent.stopReconcilingMutex.Unlock()
		if !ok {
			//channel-closed
			return fmt.Errorf("reconcile channel closed:%w", errReconcileAborted)
		}
		return fmt.Errorf("reconciling aborted:%w", errReconcileAborted)
	// Context expired
	case <-ctx.Done():
		return fmt.Errorf("context expired:%s :%w", ctx.Err(), errContextExpired)
	}
}

func (agent *Agent) reconcilingCleanup(ctx context.Context) error {
	var desc string
	var err error
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		desc = "reconcile-cleanup-failed"
		return err
	}
	defer agent.requestQueue.RequestComplete()
	err = agent.updateTransientState(ctx, core.DeviceTransientState_NONE)
	if err != nil {
		logger.Errorf(ctx, "transient-state-update-failed", log.Fields{"error": err})
		return err
	}
	operStatus.Code = common.OperationResp_OPERATION_SUCCESS
	return nil
}

func (agent *Agent) isAdapterConnectionUp(ctx context.Context) bool {
	c, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	return c != nil && err == nil
}

func (agent *Agent) canDeviceRequestProceed(ctx context.Context) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	if agent.proceedWithRequest(agent.device) {
		return nil
	}
	return fmt.Errorf("device-cannot-process-request-%s", agent.deviceID)
}
