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

	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/flow"
	"github.com/opencord/voltha-go/rw_core/core/device/group"
	"github.com/opencord/voltha-go/rw_core/core/device/remote"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Agent represents device agent attributes
type Agent struct {
	deviceID       string
	parentID       string
	deviceType     string
	isRootdevice   bool
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

	flowLoader  *flow.Loader
	groupLoader *group.Loader
}

//newAgent creates a new device agent. The device will be initialized when start() is called.
func newAgent(ap *remote.AdapterProxy, device *voltha.Device, deviceMgr *Manager, dbProxy *model.Path, deviceProxy *model.Proxy, timeout time.Duration) *Agent {
	var agent Agent
	agent.adapterProxy = ap
	if device.Id == "" {
		agent.deviceID = coreutils.CreateDeviceID()
	} else {
		agent.deviceID = device.Id
	}

	agent.isRootdevice = device.Root
	agent.parentID = device.ParentId
	agent.deviceType = device.Type
	agent.deviceMgr = deviceMgr
	agent.adapterMgr = deviceMgr.adapterMgr
	agent.exitChannel = make(chan int, 1)
	agent.dbProxy = deviceProxy
	agent.defaultTimeout = timeout
	agent.device = proto.Clone(device).(*voltha.Device)
	agent.requestQueue = coreutils.NewRequestQueue()
	agent.flowLoader = flow.NewLoader(dbProxy.SubPath("flows").Proxy(device.Id))
	agent.groupLoader = group.NewLoader(dbProxy.SubPath("groups").Proxy(device.Id))

	return &agent
}

// start() saves the device to the data model and registers for callbacks on that device if deviceToCreate!=nil.
// Otherwise, it will load the data from the dB and setup the necessary callbacks and proxies. Returns the device that
// was started.
func (agent *Agent) start(ctx context.Context, deviceToCreate *voltha.Device) (*voltha.Device, error) {
	needToStart := false
	if agent.startOnce.Do(func() { needToStart = true }); !needToStart {
		return agent.getDevice(ctx)
	}
	var startSucceeded bool
	defer func() {
		if !startSucceeded {
			if err := agent.stop(ctx); err != nil {
				logger.Errorw("failed-to-cleanup-after-unsuccessful-start", log.Fields{"device-id": agent.deviceID, "error": err})
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

		logger.Infow("device-loaded-from-dB", log.Fields{"device-id": agent.deviceID})
	} else {
		// Create a new device
		// Assumption is that AdminState, FlowGroups, and Flows are uninitialized since this
		// is a new device, so populate them here before passing the device to ldProxy.Set.
		// agent.deviceId will also have been set during newAgent().
		device = (proto.Clone(deviceToCreate)).(*voltha.Device)
		device.Id = agent.deviceID
		device.AdminState = voltha.AdminState_PREPROVISIONED
		device.FlowGroups = &ofp.FlowGroups{Items: nil}
		device.Flows = &ofp.Flows{Items: nil}
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
	logger.Debugw("device-agent-started", log.Fields{"device-id": agent.deviceID})

	return agent.getDevice(ctx)
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

	logger.Infow("stopping-device-agent", log.Fields{"deviceId": agent.deviceID, "parentId": agent.parentID})

	//	Remove the device from the KV store
	if err := agent.dbProxy.Remove(ctx, agent.deviceID); err != nil {
		return err
	}

	close(agent.exitChannel)

	agent.stopped = true

	logger.Infow("device-agent-stopped", log.Fields{"device-id": agent.deviceID, "parent-id": agent.parentID})

	return nil
}

// Load the most recent state from the KVStore for the device.
func (agent *Agent) reconcileWithKVStore(ctx context.Context) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Warnw("request-aborted", log.Fields{"device-id": agent.deviceID, "error": err})
		return
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debug("reconciling-device-agent-devicetype")
	// TODO: context timeout
	device := &voltha.Device{}
	if have, err := agent.dbProxy.Get(ctx, agent.deviceID, device); err != nil {
		logger.Errorw("kv-get-failed", log.Fields{"device-id": agent.deviceID, "error": err})
		return
	} else if !have {
		return // not found in kv
	}

	agent.deviceType = device.Adapter
	agent.device = device
	agent.flowLoader.Load(ctx)
	agent.groupLoader.Load(ctx)
	logger.Debugw("reconciled-device-agent-devicetype", log.Fields{"device-id": agent.deviceID, "type": agent.deviceType})
}

// onSuccess is a common callback for scenarios where we receive a nil response following a request to an adapter
// and the only action required is to publish a successful result on kafka
func (agent *Agent) onSuccess(rpc string, response interface{}, reqArgs ...interface{}) {
	logger.Debugw("response successful", log.Fields{"rpc": rpc, "device-id": agent.deviceID})
	// TODO: Post success message onto kafka
}

// onFailure is a common callback for scenarios where we receive an error response following a request to an adapter
// and the only action required is to publish the failed result on kafka
func (agent *Agent) onFailure(rpc string, response interface{}, reqArgs ...interface{}) {
	if res, ok := response.(error); ok {
		logger.Errorw("rpc-failed", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": res, "args": reqArgs})
	} else {
		logger.Errorw("rpc-failed-invalid-error", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "args": reqArgs})
	}
	// TODO: Post failure message onto kafka
}

func (agent *Agent) waitForAdapterResponse(ctx context.Context, cancel context.CancelFunc, rpc string, ch chan *kafka.RpcResponse,
	onSuccess coreutils.ResponseCallback, onFailure coreutils.ResponseCallback, reqArgs ...interface{}) {
	defer cancel()
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			onFailure(rpc, status.Errorf(codes.Aborted, "channel-closed"), reqArgs)
		} else if rpcResponse.Err != nil {
			onFailure(rpc, rpcResponse.Err, reqArgs)
		} else {
			onSuccess(rpc, rpcResponse.Reply, reqArgs)
		}
	case <-ctx.Done():
		onFailure(rpc, ctx.Err(), reqArgs)
	}
}

// getDevice returns the device data from cache
func (agent *Agent) getDevice(ctx context.Context) (*voltha.Device, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	return proto.Clone(agent.device).(*voltha.Device), nil
}

// getDeviceWithoutLock is a helper function to be used ONLY by any device agent function AFTER it has acquired the device lock.
func (agent *Agent) getDeviceWithoutLock() *voltha.Device {
	return agent.device
}

// enableDevice activates a preprovisioned or a disable device
func (agent *Agent) enableDevice(ctx context.Context) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw("enableDevice", log.Fields{"device-id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	// First figure out which adapter will handle this device type.  We do it at this stage as allow devices to be
	// pre-provisioned with the required adapter not registered.   At this stage, since we need to communicate
	// with the adapter then we need to know the adapter that will handle this request
	adapterName, err := agent.adapterMgr.GetAdapterType(cloned.Type)
	if err != nil {
		return err
	}
	cloned.Adapter = adapterName

	if cloned.AdminState == voltha.AdminState_ENABLED {
		logger.Warnw("device-already-enabled", log.Fields{"device-id": agent.deviceID})
		err = status.Error(codes.FailedPrecondition, fmt.Sprintf("cannot-enable-an-already-enabled-device: %s ", cloned.Id))
		return err
	}

	if cloned.AdminState == voltha.AdminState_DELETED {
		// This is a temporary state when a device is deleted before it gets removed from the model.
		err = status.Error(codes.FailedPrecondition, fmt.Sprintf("cannot-enable-a-deleted-device: %s ", cloned.Id))
		return err
	}

	previousAdminState := cloned.AdminState

	// Update the Admin State and set the operational state to activating before sending the request to the
	// Adapters
	if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_ENABLED, cloned.ConnectStatus, voltha.OperStatus_ACTIVATING); err != nil {
		return err
	}

	// Adopt the device if it was in pre-provision state.  In all other cases, try to re-enable it.
	device := proto.Clone(cloned).(*voltha.Device)
	var ch chan *kafka.RpcResponse
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	if previousAdminState == voltha.AdminState_PREPROVISIONED {
		ch, err = agent.adapterProxy.AdoptDevice(subCtx, device)
	} else {
		ch, err = agent.adapterProxy.ReEnableDevice(subCtx, device)
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
		logger.Warnw("no-adapter-response", log.Fields{"device-id": agent.deviceID, "result": errs})
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
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("disableDevice", log.Fields{"device-id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	if cloned.AdminState == voltha.AdminState_DISABLED {
		logger.Debugw("device-already-disabled", log.Fields{"id": agent.deviceID})
		return nil
	}
	if cloned.AdminState == voltha.AdminState_PREPROVISIONED ||
		cloned.AdminState == voltha.AdminState_DELETED {
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, invalid-admin-state:%s", agent.deviceID, cloned.AdminState)
	}

	// Update the Admin State and operational state before sending the request out
	if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_DISABLED, cloned.ConnectStatus, voltha.OperStatus_UNKNOWN); err != nil {
		return err
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.DisableDevice(subCtx, proto.Clone(cloned).(*voltha.Device))
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
	logger.Debugw("rebootDevice", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceWithoutLock()
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.RebootDevice(subCtx, device)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "rebootDevice", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) deleteDevice(ctx context.Context) error {
	logger.Debugw("deleteDevice", log.Fields{"device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	cloned := agent.getDeviceWithoutLock()

	previousState := cloned.AdminState

	// No check is required when deleting a device.  Changing the state to DELETE will trigger the removal of this
	// device by the state machine
	if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_DELETED, cloned.ConnectStatus, cloned.OperStatus); err != nil {
		return err
	}

	// If the device was in pre-prov state (only parent device are in that state) then do not send the request to the
	// adapter
	if previousState != ic.AdminState_PREPROVISIONED {
		subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
		ch, err := agent.adapterProxy.DeleteDevice(subCtx, cloned)
		if err != nil {
			cancel()
			return err
		}
		go agent.waitForAdapterResponse(subCtx, cancel, "deleteDevice", ch, agent.onSuccess, agent.onFailure)
	}
	return nil
}

func (agent *Agent) setParentID(ctx context.Context, device *voltha.Device, parentID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw("setParentId", log.Fields{"device-id": device.Id, "parent-id": parentID})

	cloned := agent.getDeviceWithoutLock()
	cloned.ParentId = parentID
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}

	return nil
}

// getSwitchCapability retrieves the switch capability of a parent device
func (agent *Agent) getSwitchCapability(ctx context.Context) (*ic.SwitchCapability, error) {
	logger.Debugw("getSwitchCapability", log.Fields{"device-id": agent.deviceID})

	cloned, err := agent.getDevice(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := agent.adapterProxy.GetOfpDeviceInfo(ctx, cloned)
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

func (agent *Agent) onPacketFailure(rpc string, response interface{}, args ...interface{}) {
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
	logger.Warnw("packet-out-error", log.Fields{
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
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.PacketOut(subCtx, agent.deviceType, agent.deviceID, outPort, packet)
	if err != nil {
		cancel()
		return nil
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "packetOut", ch, agent.onSuccess, agent.onPacketFailure, packet.Data)
	return nil
}

// updatePartialDeviceData updates a subset of a device that an Adapter can update.
// TODO:  May need a specific proto to handle only a subset of a device that can be changed by an adapter
func (agent *Agent) mergeDeviceInfoFromAdapter(device *voltha.Device) (*voltha.Device, error) {
	cloned := agent.getDeviceWithoutLock()
	cloned.Root = device.Root
	cloned.Vendor = device.Vendor
	cloned.Model = device.Model
	cloned.SerialNumber = device.SerialNumber
	cloned.MacAddress = device.MacAddress
	cloned.Vlan = device.Vlan
	cloned.Reason = device.Reason
	return cloned, nil
}

func (agent *Agent) updateDeviceUsingAdapterData(ctx context.Context, device *voltha.Device) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("updateDeviceUsingAdapterData", log.Fields{"device-id": device.Id})

	updatedDevice, err := agent.mergeDeviceInfoFromAdapter(device)
	if err != nil {
		return status.Errorf(codes.Internal, "%s", err.Error())
	}
	cloned := proto.Clone(updatedDevice).(*voltha.Device)
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) updateDeviceStatus(ctx context.Context, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	cloned := agent.getDeviceWithoutLock()

	newConnStatus, newOperStatus := cloned.ConnectStatus, cloned.OperStatus
	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if s, ok := voltha.ConnectStatus_Types_value[connStatus.String()]; ok {
		logger.Debugw("updateDeviceStatus-conn", log.Fields{"ok": ok, "val": s})
		newConnStatus = connStatus
	}
	if s, ok := voltha.OperStatus_Types_value[operStatus.String()]; ok {
		logger.Debugw("updateDeviceStatus-oper", log.Fields{"ok": ok, "val": s})
		newOperStatus = operStatus
	}
	logger.Debugw("updateDeviceStatus", log.Fields{"deviceId": cloned.Id, "operStatus": cloned.OperStatus, "connectStatus": cloned.ConnectStatus})
	// Store the device
	return agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, cloned.AdminState, newConnStatus, newOperStatus)
}

// TODO: A generic device update by attribute
func (agent *Agent) updateDeviceAttribute(ctx context.Context, name string, value interface{}) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Warnw("request-aborted", log.Fields{"device-id": agent.deviceID, "name": name, "error": err})
		return
	}
	defer agent.requestQueue.RequestComplete()
	if value == nil {
		return
	}

	cloned := agent.getDeviceWithoutLock()
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
	logger.Debugw("update-field-status", log.Fields{"deviceId": cloned.Id, "name": name, "updated": updated})
	//	Save the data

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		logger.Warnw("attribute-update-failed", log.Fields{"attribute": name, "value": value})
	}
}

func (agent *Agent) simulateAlarm(ctx context.Context, simulateReq *voltha.SimulateAlarmRequest) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("simulateAlarm", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.SimulateAlarm(subCtx, cloned, simulateReq)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "simulateAlarm", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) updateDeviceStateInStoreWithoutLock(
	ctx context.Context,
	device *voltha.Device,
	adminState voltha.AdminState_Types,
	connectStatus voltha.ConnectStatus_Types,
	operStatus voltha.OperStatus_Types,
) error {
	previousState := getDeviceStates(device)
	device.AdminState, device.ConnectStatus, device.OperStatus = adminState, connectStatus, operStatus

	if err := agent.updateDeviceInStoreWithoutLock(ctx, device, false, ""); err != nil {
		return err
	}

	// process state transition in its own thread
	go func() {
		if err := agent.deviceMgr.processTransition(context.Background(), device, previousState); err != nil {
			log.Errorw("failed-process-transition", log.Fields{"deviceId": device.Id, "previousAdminState": previousState.Admin, "currentAdminState": device.AdminState})
		}
	}()
	return nil
}

//This is an update operation to model without Lock.This function must never be invoked by another function unless the latter holds a lock on the device.
// It is an internal helper function.
func (agent *Agent) updateDeviceInStoreWithoutLock(ctx context.Context, device *voltha.Device, strict bool, txid string) error {
	if agent.stopped {
		return errors.New("device agent stopped")
	}

	if err := agent.dbProxy.Set(ctx, agent.deviceID, device); err != nil {
		return status.Errorf(codes.Internal, "failed-update-device:%s: %s", agent.deviceID, err)
	}
	logger.Debugw("updated-device-in-store", log.Fields{"deviceId: ": agent.deviceID})

	agent.device = device
	return nil
}

func (agent *Agent) updateDeviceReason(ctx context.Context, reason string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	cloned := agent.getDeviceWithoutLock()
	cloned.Reason = reason
	logger.Debugw("updateDeviceReason", log.Fields{"deviceId": cloned.Id, "reason": cloned.Reason})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) ChildDeviceLost(ctx context.Context, device *voltha.Device) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw("childDeviceLost", log.Fields{"child-device-id": device.Id, "parent-device-ud": agent.deviceID})

	//Remove the associated peer ports on the parent device
	parentDevice := agent.getDeviceWithoutLock()
	var updatedPeers []*voltha.Port_PeerPort
	for _, port := range parentDevice.Ports {
		updatedPeers = make([]*voltha.Port_PeerPort, 0)
		for _, peerPort := range port.Peers {
			if peerPort.DeviceId != device.Id {
				updatedPeers = append(updatedPeers, peerPort)
			}
		}
		port.Peers = updatedPeers
	}
	if err := agent.updateDeviceInStoreWithoutLock(ctx, parentDevice, false, ""); err != nil {
		return err
	}

	//send request to adapter
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
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

	device := agent.getDeviceWithoutLock()

	if device.Adapter == "" {
		adapterName, err := agent.adapterMgr.GetAdapterType(device.Type)
		if err != nil {
			agent.requestQueue.RequestComplete()
			return nil, err
		}
		device.Adapter = adapterName
	}

	// Send request to the adapter
	ch, err := agent.adapterProxy.StartOmciTest(ctx, device, omcitestrequest)
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
	logger.Debugw("Omci_test_Request-Success-device-agent", log.Fields{"testResp": testResp})
	return testResp, nil
}

func (agent *Agent) getExtValue(ctx context.Context, pdevice *voltha.Device, cdevice *voltha.Device, valueparam *voltha.ValueSpecifier) (*voltha.ReturnValues, error) {
	log.Debugw("getExtValue", log.Fields{"device-id": agent.deviceID, "onuid": valueparam.Id, "valuetype": valueparam.Value})
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
	logger.Debugw("getExtValue-Success-device-agent", log.Fields{"Resp": Resp})
	return Resp, nil
}
