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

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/flow"
	"github.com/opencord/voltha-go/rw_core/core/device/group"
	"github.com/opencord/voltha-go/rw_core/core/device/port"
	"github.com/opencord/voltha-go/rw_core/core/device/remote"
	"github.com/opencord/voltha-go/rw_core/core/device/transientstate"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/extension"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Agent represents device agent attributes
type Agent struct {
	deviceID       string
	parentID       string
	deviceType     string
	isRootDevice   bool
	adapterProxy   *remote.AdapterProxy
	adapterMgr     *adapter.Manager
	deviceMgr      *Manager
	dbProxy        *model.Proxy
	exitChannel    chan int
	device         *voltha.Device
	requestQueue   *coreutils.RequestQueue
	defaultTimeout time.Duration
	startOnce      sync.Once
	stopOnce       sync.Once
	stopped        bool

	flowLoader           *flow.Loader
	groupLoader          *group.Loader
	portLoader           *port.Loader
	transientStateLoader *transientstate.Loader
}

//newAgent creates a new device agent. The device will be initialized when start() is called.
func newAgent(ap *remote.AdapterProxy, device *voltha.Device, deviceMgr *Manager, dbPath *model.Path, deviceProxy *model.Proxy, timeout time.Duration) *Agent {
	deviceID := device.Id
	if deviceID == "" {
		deviceID = coreutils.CreateDeviceID()
	}

	return &Agent{
		deviceID:             deviceID,
		adapterProxy:         ap,
		isRootDevice:         device.Root,
		parentID:             device.ParentId,
		deviceType:           device.Type,
		deviceMgr:            deviceMgr,
		adapterMgr:           deviceMgr.adapterMgr,
		exitChannel:          make(chan int, 1),
		dbProxy:              deviceProxy,
		defaultTimeout:       timeout,
		device:               proto.Clone(device).(*voltha.Device),
		requestQueue:         coreutils.NewRequestQueue(),
		flowLoader:           flow.NewLoader(dbPath.SubPath("flows").Proxy(deviceID)),
		groupLoader:          group.NewLoader(dbPath.SubPath("groups").Proxy(deviceID)),
		portLoader:           port.NewLoader(dbPath.SubPath("ports").Proxy(deviceID)),
		transientStateLoader: transientstate.NewLoader(dbPath.SubPath("core").Proxy("transientstate"), deviceID),
	}
}

// start() saves the device to the data model and registers for callbacks on that device if deviceToCreate!=nil.
// Otherwise, it will load the data from the dB and setup the necessary callbacks and proxies. Returns the device that
// was started.
func (agent *Agent) start(ctx context.Context, deviceToCreate *voltha.Device) (*voltha.Device, error) {
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

	var device *voltha.Device
	if deviceToCreate == nil {
		// Load the existing device
		device := &voltha.Device{}
		have, err := agent.dbProxy.Get(ctx, agent.deviceID, device)
		if err != nil {
			return nil, err
		} else if !have {
			return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
		}

		agent.deviceType = device.Adapter
		agent.device = proto.Clone(device).(*voltha.Device)
		// load the flows and groups from KV to cache
		agent.flowLoader.Load(ctx)
		agent.groupLoader.Load(ctx)
		agent.portLoader.Load(ctx)
		agent.transientStateLoader.Load(ctx)

		logger.Infow(ctx, "device-loaded-from-dB", log.Fields{"device-id": agent.deviceID})
	} else {
		// Create a new device
		// Assumption is that AdminState, FlowGroups, and Flows are uninitialized since this
		// is a new device, so populate them here before passing the device to ldProxy.Set.
		// agent.deviceId will also have been set during newAgent().
		device = (proto.Clone(deviceToCreate)).(*voltha.Device)
		device.Id = agent.deviceID
		device.AdminState = voltha.AdminState_PREPROVISIONED
		if !deviceToCreate.GetRoot() && deviceToCreate.ProxyAddress != nil {
			// Set the default vlan ID to the one specified by the parent adapter.  It can be
			// overwritten by the child adapter during a device update request
			device.Vlan = deviceToCreate.ProxyAddress.ChannelId
		}

		// Add the initial device to the local model
		if err := agent.dbProxy.Set(ctx, agent.deviceID, device); err != nil {
			return nil, status.Errorf(codes.Aborted, "failed-adding-device-%s: %s", agent.deviceID, err)
		}
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

	logger.Infow(ctx, "stopping-device-agent", log.Fields{"device-id": agent.deviceID, "parentId": agent.parentID})
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

	agent.deviceType = device.Adapter
	agent.device = device
	agent.flowLoader.Load(ctx)
	agent.groupLoader.Load(ctx)
	agent.portLoader.Load(ctx)
	agent.transientStateLoader.Load(ctx)

	logger.Debugw(ctx, "reconciled-device-agent-devicetype", log.Fields{"device-id": agent.deviceID, "type": agent.deviceType})
}

// onSuccess is a common callback for scenarios where we receive a nil response following a request to an adapter
// and the only action required is to publish a successful result on kafka
func (agent *Agent) onSuccess(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{}) {
	logger.Debugw(ctx, "response-successful", log.Fields{"rpc": rpc, "device-id": agent.deviceID})
	// TODO: Post success message onto kafka
}

// onFailure is a common callback for scenarios where we receive an error response following a request to an adapter
// and the only action required is to publish the failed result on kafka
func (agent *Agent) onFailure(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{}) {
	if res, ok := response.(error); ok {
		logger.Errorw(ctx, "rpc-failed", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": res, "args": reqArgs})
	} else {
		logger.Errorw(ctx, "rpc-failed-invalid-error", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "args": reqArgs})
	}
	// TODO: Post failure message onto kafka
}

func (agent *Agent) waitForAdapterResponse(ctx context.Context, cancel context.CancelFunc, rpc string, ch chan *kafka.RpcResponse,
	onSuccess coreutils.ResponseCallback, onFailure coreutils.ResponseCallback, reqArgs ...interface{}) {
	defer cancel()
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			onFailure(ctx, rpc, status.Errorf(codes.Aborted, "channel-closed"), reqArgs)
		} else if rpcResponse.Err != nil {
			onFailure(ctx, rpc, rpcResponse.Err, reqArgs)
		} else {
			onSuccess(ctx, rpc, rpcResponse.Reply, reqArgs)
		}
	case <-ctx.Done():
		onFailure(ctx, rpc, ctx.Err(), reqArgs)
	}
}

// onDeleteSuccess is a common callback for scenarios where we receive a nil response following a delete request
// to an adapter.
func (agent *Agent) onDeleteSuccess(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{}) {
	logger.Debugw(ctx, "response-successful", log.Fields{"rpc": rpc, "device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Errorw(ctx, "delete-device-failure", log.Fields{"device-id": agent.deviceID, "error": err, "args": reqArgs})
	}
	previousDeviceTransientState := agent.getTransientState()
	newDevice := agent.cloneDeviceWithoutLock()
	if err := agent.updateDeviceWithTransientStateAndReleaseLock(ctx, newDevice,
		voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE, previousDeviceTransientState); err != nil {
		logger.Errorw(ctx, "delete-device-failure", log.Fields{"device-id": agent.deviceID, "error": err, "args": reqArgs})
	}
}

// onDeleteFailure is a common callback for scenarios where we receive an error response following a delete request
//  to an adapter and the only action required is to return the error response.
func (agent *Agent) onDeleteFailure(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{}) {
	if res, ok := response.(error); ok {
		logger.Errorw(ctx, "rpc-failed", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": res, "args": reqArgs})
	} else {
		logger.Errorw(ctx, "rpc-failed-invalid-error", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "args": reqArgs})
	}
	//Only updating of transient state is required, no transition.
	if err := agent.updateTransientState(ctx, voltha.DeviceTransientState_DELETE_FAILED); err != nil {
		logger.Errorw(ctx, "failed-to-update-transient-state-as-delete-failed", log.Fields{"device-id": agent.deviceID})
	}

}

func (agent *Agent) waitForAdapterDeleteResponse(ctx context.Context, cancel context.CancelFunc, rpc string, ch chan *kafka.RpcResponse,
	onSuccess coreutils.ResponseCallback, onFailure coreutils.ResponseCallback, reqArgs ...interface{}) {
	defer cancel()
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			onFailure(ctx, rpc, status.Errorf(codes.Aborted, "channel-closed"), reqArgs)
		} else if rpcResponse.Err != nil {
			onFailure(ctx, rpc, rpcResponse.Err, reqArgs)
		} else {
			onSuccess(ctx, rpc, rpcResponse.Reply, reqArgs)
		}
	case <-ctx.Done():
		onFailure(ctx, rpc, ctx.Err(), reqArgs)
	}
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

// enableDevice activates a preprovisioned or a disable device
func (agent *Agent) enableDevice(ctx context.Context) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "enableDevice", log.Fields{"device-id": agent.deviceID})

	oldDevice := agent.getDeviceReadOnlyWithoutLock()
	if oldDevice.AdminState == voltha.AdminState_ENABLED {
		logger.Warnw(ctx, "device-already-enabled", log.Fields{"device-id": agent.deviceID})
		agent.requestQueue.RequestComplete()
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("cannot-enable-an-already-enabled-device: %s", oldDevice.Id))
	}
	if agent.isDeletionInProgress() {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device deletion is in progress.", agent.deviceID)
	}
	// First figure out which adapter will handle this device type.  We do it at this stage as allow devices to be
	// pre-provisioned with the required adapter not registered.   At this stage, since we need to communicate
	// with the adapter then we need to know the adapter that will handle this request
	adapterName, err := agent.adapterMgr.GetAdapterType(oldDevice.Type)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return err
	}

	newDevice := agent.cloneDeviceWithoutLock()
	newDevice.Adapter = adapterName

	// Update the Admin State and set the operational state to activating before sending the request to the Adapters
	newDevice.AdminState = voltha.AdminState_ENABLED
	newDevice.OperStatus = voltha.OperStatus_ACTIVATING
	if err := agent.updateDeviceAndReleaseLock(ctx, newDevice); err != nil {
		return err
	}

	// Adopt the device if it was in pre-provision state.  In all other cases, try to re-enable it.
	var ch chan *kafka.RpcResponse
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	if oldDevice.AdminState == voltha.AdminState_PREPROVISIONED {
		ch, err = agent.adapterProxy.AdoptDevice(subCtx, newDevice)
	} else {
		ch, err = agent.adapterProxy.ReEnableDevice(subCtx, newDevice)
	}
	if err != nil {
		cancel()
		return err
	}
	// Wait for response
	go agent.waitForAdapterResponse(subCtx, cancel, "enableDevice", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) waitForAdapterFlowResponse(ctx context.Context, cancel context.CancelFunc, ch chan *kafka.RpcResponse, response coreutils.Response) {
	defer cancel()
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			response.Error(status.Errorf(codes.Aborted, "channel-closed"))
		} else if rpcResponse.Err != nil {
			response.Error(rpcResponse.Err)
		} else {
			response.Done()
		}
	case <-ctx.Done():
		response.Error(ctx.Err())
	}
}

//addFlowsAndGroups adds the "newFlows" and "newGroups" from the existing flows/groups and sends the update to the
//adapters
func (agent *Agent) addFlowsAndGroups(ctx context.Context, newFlows []*ofp.OfpFlowStats, newGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
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
	if errs := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, flwResponse, grpResponse); errs != nil {
		logger.Warnw(ctx, "no-adapter-response", log.Fields{"device-id": agent.deviceID, "result": errs})
		return status.Errorf(codes.Aborted, "flow-failure-device-%s", agent.deviceID)
	}
	return nil
}

//deleteFlowsAndGroups removes the "flowsToDel" and "groupsToDel" from the existing flows/groups and sends the update to the
//adapters
func (agent *Agent) deleteFlowsAndGroups(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	var flwResponse, grpResponse coreutils.Response
	var err error
	if flwResponse, err = agent.deleteFlowsFromAdapter(ctx, flowsToDel, flowMetadata); err != nil {
		return err
	}
	if grpResponse, err = agent.deleteGroupsFromAdapter(ctx, groupsToDel, flowMetadata); err != nil {
		return err
	}

	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, flwResponse, grpResponse); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//updateFlowsAndGroups replaces the existing flows and groups with "updatedFlows" and "updatedGroups" respectively. It
//also sends the updates to the adapters
func (agent *Agent) updateFlowsAndGroups(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	var flwResponse, grpResponse coreutils.Response
	var err error
	if flwResponse, err = agent.updateFlowsToAdapter(ctx, updatedFlows, flowMetadata); err != nil {
		return err
	}
	if grpResponse, err = agent.updateGroupsToAdapter(ctx, updatedGroups, flowMetadata); err != nil {
		return err
	}

	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, flwResponse, grpResponse); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//disableDevice disable a device
func (agent *Agent) disableDevice(ctx context.Context) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "disableDevice", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()

	if cloned.AdminState == voltha.AdminState_DISABLED {
		logger.Debugw(ctx, "device-already-disabled", log.Fields{"device-id": agent.deviceID})
		agent.requestQueue.RequestComplete()
		return nil
	}
	if cloned.AdminState == voltha.AdminState_PREPROVISIONED {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, invalid-admin-state:%s", agent.deviceID, cloned.AdminState)
	}
	if agent.isDeletionInProgress() {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device deletion is in progress.", agent.deviceID)
	}
	// Update the Admin State and operational state before sending the request out
	cloned.AdminState = voltha.AdminState_DISABLED
	cloned.OperStatus = voltha.OperStatus_UNKNOWN
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return err
	}

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.DisableDevice(subCtx, cloned)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "disableDevice", ch, agent.onSuccess, agent.onFailure)

	return nil
}

func (agent *Agent) rebootDevice(ctx context.Context) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "rebootDevice", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceReadOnlyWithoutLock()
	if agent.isDeletionInProgress() {
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device deletion is in progress.", agent.deviceID)
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.RebootDevice(subCtx, device)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "rebootDevice", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) deleteDeviceForce(ctx context.Context) error {
	logger.Debugw(ctx, "deleteDeviceForce", log.Fields{"device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	// Get the device Transient state, return err if it is DELETING
	previousDeviceTransientState := agent.getTransientState()

	if agent.isStateDeleting(previousDeviceTransientState) {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device Deletion is in progress",
			agent.deviceID)
	}
	device := agent.cloneDeviceWithoutLock()
	if err := agent.updateDeviceWithTransientStateAndReleaseLock(ctx, device, voltha.DeviceTransientState_FORCE_DELETING,
		previousDeviceTransientState); err != nil {
		return err
	}
	previousAdminState := device.AdminState
	if previousAdminState != ic.AdminState_PREPROVISIONED {
		subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
		ch, err := agent.adapterProxy.DeleteDevice(subCtx, device)
		if err != nil {
			cancel()
			return err
		}
		// Since it is a case of force delete, nothing needs to be done on adapter responses.
		go agent.waitForAdapterResponse(subCtx, cancel, "deleteDeviceForce", ch, agent.onSuccess,
			agent.onFailure)
	}
	return nil
}

func (agent *Agent) deleteDevice(ctx context.Context) error {
	logger.Debugw(ctx, "deleteDevice", log.Fields{"device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	// Get the device Transient state, return err if it is DELETING
	previousDeviceTransientState := agent.getTransientState()

	if agent.isStateDeleting(previousDeviceTransientState) {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device Deletion is in progress", agent.deviceID)
	}
	device := agent.cloneDeviceWithoutLock()
	previousAdminState := device.AdminState
	// Change the device transient state to DELETING_FROM_ADAPTER  state till the device is removed from adapters.
	currentDeviceTransientState := voltha.DeviceTransientState_DELETING_FROM_ADAPTER

	if previousAdminState == ic.AdminState_PREPROVISIONED {
		// Change the state to DELETING POST ADAPTER RESPONSE directly as adapters have no info of the device.
		currentDeviceTransientState = voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE
	}
	if err := agent.updateDeviceWithTransientStateAndReleaseLock(ctx, device, currentDeviceTransientState,
		previousDeviceTransientState); err != nil {
		return err
	}
	// If the device was in pre-prov state (only parent device are in that state) then do not send the request to the
	// adapter
	if previousAdminState != ic.AdminState_PREPROVISIONED {
		subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
		ch, err := agent.adapterProxy.DeleteDevice(subCtx, device)
		if err != nil {
			cancel()
			//updating of transient state is required in error
			if err := agent.updateTransientState(ctx, voltha.DeviceTransientState_DELETE_FAILED); err != nil {
				logger.Errorw(ctx, "failed-to-update-transient-state-as-delete-failed", log.Fields{"device-id": agent.deviceID})
			}
			return err
		}
		go agent.waitForAdapterDeleteResponse(subCtx, cancel, "deleteDevice", ch, agent.onDeleteSuccess,
			agent.onDeleteFailure)
	}
	return nil
}

func (agent *Agent) setParentID(ctx context.Context, device *voltha.Device, parentID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "setParentId", log.Fields{"device-id": device.Id, "parent-id": parentID})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.ParentId = parentID
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

// getSwitchCapability retrieves the switch capability of a parent device
func (agent *Agent) getSwitchCapability(ctx context.Context) (*ic.SwitchCapability, error) {
	logger.Debugw(ctx, "getSwitchCapability", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := agent.adapterProxy.GetOfpDeviceInfo(ctx, device)
	if err != nil {
		return nil, err
	}

	// Wait for adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed")
	}
	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}
	// Successful response
	switchCap := &ic.SwitchCapability{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, switchCap); err != nil {
		return nil, err
	}
	return switchCap, nil
}

func (agent *Agent) onPacketFailure(ctx context.Context, rpc string, response interface{}, args ...interface{}) {
	// packet data is encoded in the args param as the first parameter
	var packet []byte
	if len(args) >= 1 {
		if pkt, ok := args[0].([]byte); ok {
			packet = pkt
		}
	}
	var errResp error
	if err, ok := response.(error); ok {
		errResp = err
	}
	logger.Warnw(ctx, "packet-out-error", log.Fields{
		"device-id": agent.deviceID,
		"error":     errResp,
		"packet":    hex.EncodeToString(packet),
	})
}

func (agent *Agent) packetOut(ctx context.Context, outPort uint32, packet *ofp.OfpPacketOut) error {
	// If deviceType=="" then we must have taken ownership of this device.
	// Fixes VOL-2226 where a core would take ownership and have stale data
	if agent.deviceType == "" {
		agent.reconcileWithKVStore(ctx)
	}
	//	Send packet to adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.PacketOut(subCtx, agent.deviceType, agent.deviceID, outPort, packet)
	if err != nil {
		cancel()
		return nil
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "packetOut", ch, agent.onSuccess, agent.onPacketFailure, packet.Data)
	return nil
}

func (agent *Agent) updateDeviceUsingAdapterData(ctx context.Context, device *voltha.Device) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "updateDeviceUsingAdapterData", log.Fields{"device-id": device.Id})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.Root = device.Root
	cloned.Vendor = device.Vendor
	cloned.Model = device.Model
	cloned.SerialNumber = device.SerialNumber
	cloned.MacAddress = device.MacAddress
	cloned.Vlan = device.Vlan
	cloned.Reason = device.Reason
	cloned.ImageDownloads = device.ImageDownloads
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

func (agent *Agent) updateDeviceStatus(ctx context.Context, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}

	cloned := agent.cloneDeviceWithoutLock()
	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if s, ok := voltha.ConnectStatus_Types_name[int32(connStatus)]; ok {
		logger.Debugw(ctx, "updateDeviceStatus-conn", log.Fields{"ok": ok, "val": s})
		cloned.ConnectStatus = connStatus
	}
	if s, ok := voltha.OperStatus_Types_name[int32(operStatus)]; ok {
		logger.Debugw(ctx, "updateDeviceStatus-oper", log.Fields{"ok": ok, "val": s})
		cloned.OperStatus = operStatus
	}
	logger.Debugw(ctx, "updateDeviceStatus", log.Fields{"device-id": cloned.Id, "operStatus": cloned.OperStatus, "connectStatus": cloned.ConnectStatus})
	// Store the device
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
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
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "simulateAlarm", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceReadOnlyWithoutLock()

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.SimulateAlarm(subCtx, device, simulateReq)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "simulateAlarm", ch, agent.onSuccess, agent.onFailure)
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

	// release lock before processing transition
	agent.requestQueue.RequestComplete()

	if err := agent.deviceMgr.stateTransitions.ProcessTransition(log.WithSpanFromContext(context.Background(), ctx),
		device, prevDevice, voltha.DeviceTransientState_NONE, voltha.DeviceTransientState_NONE); err != nil {
		logger.Errorw(ctx, "failed-process-transition", log.Fields{"device-id": device.Id, "previousAdminState": prevDevice.AdminState, "currentAdminState": device.AdminState})
	}
	return nil
}

// This function updates the device transient in the DB through loader, releases the device lock, and runs any state transitions.
// The calling function MUST hold the device lock.  The caller MUST NOT modify the device after this is called.
func (agent *Agent) updateDeviceWithTransientStateAndReleaseLock(ctx context.Context, device *voltha.Device,
	transientState, prevTransientState voltha.DeviceTransientState_Types) error {
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
		err := agent.updateTransientState(ctx, prevTransientState)
		logger.Errorw(ctx, "failed-to-revert-transient-state-update-on-error", log.Fields{"device-id": device.Id,
			"previousTransientState": prevTransientState, "currentTransientState": transientState})
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.Internal, "failed-update-device:%s: %s", agent.deviceID, err)
	}

	logger.Debugw(ctx, "updated-device-in-store", log.Fields{"device-id: ": agent.deviceID})

	prevDevice := agent.device
	// update the device
	agent.device = device

	// release lock before processing transition
	agent.requestQueue.RequestComplete()

	if err := agent.deviceMgr.stateTransitions.ProcessTransition(log.WithSpanFromContext(context.Background(), ctx),
		device, prevDevice, transientState, prevTransientState); err != nil {
		logger.Errorw(ctx, "failed-process-transition", log.Fields{"device-id": device.Id, "previousAdminState": prevDevice.AdminState, "currentAdminState": device.AdminState})
	}
	return nil
}
func (agent *Agent) updateDeviceReason(ctx context.Context, reason string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "updateDeviceReason", log.Fields{"device-id": agent.deviceID, "reason": reason})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.Reason = reason
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

func (agent *Agent) ChildDeviceLost(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "childDeviceLost", log.Fields{"child-device-id": device.Id, "parent-device-id": agent.deviceID})

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
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.ChildDeviceLost(ctx, agent.deviceType, agent.deviceID, device.ParentPortNo, device.ProxyAddress.OnuId)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "childDeviceLost", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) startOmciTest(ctx context.Context, omcitestrequest *voltha.OmciTestRequest) (*voltha.TestResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	if cloned.Adapter == "" {
		adapterName, err := agent.adapterMgr.GetAdapterType(cloned.Type)
		if err != nil {
			agent.requestQueue.RequestComplete()
			return nil, err
		}
		cloned.Adapter = adapterName
	}

	// Send request to the adapter
	ch, err := agent.adapterProxy.StartOmciTest(ctx, cloned, omcitestrequest)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	// Wait for the adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
	}
	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}

	// Unmarshal and return the response
	testResp := &voltha.TestResponse{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, testResp); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}
	logger.Debugw(ctx, "Omci_test_Request-Success-device-agent", log.Fields{"testResp": testResp})
	return testResp, nil
}

func (agent *Agent) getExtValue(ctx context.Context, pdevice *voltha.Device, cdevice *voltha.Device, valueparam *voltha.ValueSpecifier) (*voltha.ReturnValues, error) {
	logger.Debugw(ctx, "getExtValue", log.Fields{"device-id": agent.deviceID, "onuid": valueparam.Id, "valuetype": valueparam.Value})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	//send request to adapter
	ch, err := agent.adapterProxy.GetExtValue(ctx, pdevice, cdevice, valueparam.Id, valueparam.Value)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	// Wait for the adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
	}
	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}

	// Unmarshal and return the response
	Resp := &voltha.ReturnValues{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, Resp); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}
	logger.Debugw(ctx, "getExtValue-Success-device-agent", log.Fields{"Resp": Resp})
	return Resp, nil
}

func (agent *Agent) setExtValue(ctx context.Context, device *voltha.Device, value *voltha.ValueSet) (*empty.Empty, error) {
	logger.Debugw(ctx, "setExtValue", log.Fields{"device-id": value.Id})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	//send request to adapter
	ch, err := agent.adapterProxy.SetExtValue(ctx, device, value)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	// Wait for the adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
	}
	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}

	// Unmarshal and return the response
	logger.Debug(ctx, "setExtValue-Success-device-agent")
	return &empty.Empty{}, nil
}

func (agent *Agent) getSingleValue(ctx context.Context, request *extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	logger.Debugw(ctx, "getSingleValue", log.Fields{"device-id": request.TargetId})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	//send request to adapter
	ch, err := agent.adapterProxy.GetSingleValue(ctx, cloned.Adapter, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	// Wait for the adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
	}

	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}

	resp := &extension.SingleGetValueResponse{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, resp); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}

	return resp, nil
}

func (agent *Agent) setSingleValue(ctx context.Context, request *extension.SingleSetValueRequest) (*extension.SingleSetValueResponse, error) {
	logger.Debugw(ctx, "setSingleValue", log.Fields{"device-id": request.TargetId})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	//send request to adapter
	ch, err := agent.adapterProxy.SetSingleValue(ctx, cloned.Adapter, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	// Wait for the adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed-cloned-id-%s", agent.deviceID)
	}

	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}

	resp := &extension.SingleSetValueResponse{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, resp); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}

	return resp, nil
}
