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
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/remote"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"reflect"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Agent represents device agent attributes
type Agent struct {
	deviceID         string
	parentID         string
	deviceType       string
	isRootdevice     bool
	adapterProxy     *remote.AdapterProxy
	adapterMgr       *adapter.Manager
	deviceMgr        *Manager
	clusterDataProxy *model.Proxy
	exitChannel      chan int
	device           *voltha.Device
	requestQueue     *coreutils.RequestQueue
	defaultTimeout   time.Duration
	startOnce        sync.Once
	stopOnce         sync.Once
	stopped          bool
}

//newAgent creates a new device agent. The device will be initialized when start() is called.
func newAgent(ap *remote.AdapterProxy, device *voltha.Device, deviceMgr *Manager, cdProxy *model.Proxy, timeout time.Duration) *Agent {
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
	agent.clusterDataProxy = cdProxy
	agent.defaultTimeout = timeout
	agent.device = proto.Clone(device).(*voltha.Device)
	agent.requestQueue = coreutils.NewRequestQueue()
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
		have, err := agent.clusterDataProxy.Get(ctx, "devices/"+agent.deviceID, device)
		if err != nil {
			return nil, err
		} else if !have {
			return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
		}

		agent.deviceType = device.Adapter
		agent.device = proto.Clone(device).(*voltha.Device)

		logger.Infow("device-loaded-from-dB", log.Fields{"device-id": agent.deviceID})
	} else {
		// Create a new device
		// Assumption is that AdminState, FlowGroups, and Flows are unitialized since this
		// is a new device, so populate them here before passing the device to clusterDataProxy.AddWithId.
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
		if err := agent.clusterDataProxy.AddWithID(ctx, "devices", agent.deviceID, device); err != nil {
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
	if err := agent.clusterDataProxy.Remove(ctx, "devices/"+agent.deviceID); err != nil {
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
	if have, err := agent.clusterDataProxy.Get(ctx, "devices/"+agent.deviceID, device); err != nil {
		logger.Errorw("kv-get-failed", log.Fields{"device-id": agent.deviceID, "error": err})
		return
	} else if !have {
		return // not found in kv
	}

	agent.deviceType = device.Adapter
	agent.device = device
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
	return proto.Clone(agent.device).(*voltha.Device)
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

//deleteFlowWithoutPreservingOrder removes a flow specified by index from the flows slice.  This function will
//panic if the index is out of range.
func deleteFlowWithoutPreservingOrder(flows []*ofp.OfpFlowStats, index int) []*ofp.OfpFlowStats {
	flows[index] = flows[len(flows)-1]
	flows[len(flows)-1] = nil
	return flows[:len(flows)-1]
}

//deleteGroupWithoutPreservingOrder removes a group specified by index from the groups slice.  This function will
//panic if the index is out of range.
func deleteGroupWithoutPreservingOrder(groups []*ofp.OfpGroupEntry, index int) []*ofp.OfpGroupEntry {
	groups[index] = groups[len(groups)-1]
	groups[len(groups)-1] = nil
	return groups[:len(groups)-1]
}

func flowsToUpdateToDelete(newFlows, existingFlows []*ofp.OfpFlowStats) (updatedNewFlows, flowsToDelete, updatedAllFlows []*ofp.OfpFlowStats) {
	// Process flows
	for _, flow := range existingFlows {
		if idx := fu.FindFlows(newFlows, flow); idx == -1 {
			updatedAllFlows = append(updatedAllFlows, flow)
		} else {
			// We have a matching flow (i.e. the following field matches: "TableId", "Priority", "Flags", "Cookie",
			// "Match".  If this is an exact match (i.e. all other fields matches as well) then this flow will be
			// ignored.  Otherwise, the previous flow will be deleted and the new one added
			if proto.Equal(newFlows[idx], flow) {
				// Flow already exist, remove it from the new flows but keep it in the updated flows slice
				newFlows = deleteFlowWithoutPreservingOrder(newFlows, idx)
				updatedAllFlows = append(updatedAllFlows, flow)
			} else {
				// Minor change to flow, delete old and add new one
				flowsToDelete = append(flowsToDelete, flow)
			}
		}
	}
	updatedAllFlows = append(updatedAllFlows, newFlows...)
	return newFlows, flowsToDelete, updatedAllFlows
}

func groupsToUpdateToDelete(newGroups, existingGroups []*ofp.OfpGroupEntry) (updatedNewGroups, groupsToDelete, updatedAllGroups []*ofp.OfpGroupEntry) {
	for _, group := range existingGroups {
		if idx := fu.FindGroup(newGroups, group.Desc.GroupId); idx == -1 { // does not exist now
			updatedAllGroups = append(updatedAllGroups, group)
		} else {
			// Follow same logic as flows
			if proto.Equal(newGroups[idx], group) {
				// Group already exist, remove it from the new groups
				newGroups = deleteGroupWithoutPreservingOrder(newGroups, idx)
				updatedAllGroups = append(updatedAllGroups, group)
			} else {
				// Minor change to group, delete old and add new one
				groupsToDelete = append(groupsToDelete, group)
			}
		}
	}
	updatedAllGroups = append(updatedAllGroups, newGroups...)
	return newGroups, groupsToDelete, updatedAllGroups
}

func (agent *Agent) addFlowsAndGroupsToAdapter(ctx context.Context, newFlows []*ofp.OfpFlowStats, newGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw("add-flows-groups-to-adapters", log.Fields{"device-id": agent.deviceID, "flows": newFlows, "groups": newGroups, "flow-metadata": flowMetadata})

	if (len(newFlows) | len(newGroups)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": newFlows, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return coreutils.DoneResponse(), err
	}
	defer agent.requestQueue.RequestComplete()

	device := agent.getDeviceWithoutLock()
	dType := agent.adapterMgr.GetDeviceType(device.Type)
	if dType == nil {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}

	existingFlows := proto.Clone(device.Flows).(*voltha.Flows)
	existingGroups := proto.Clone(device.FlowGroups).(*ofp.FlowGroups)

	// Process flows
	newFlows, flowsToDelete, updatedAllFlows := flowsToUpdateToDelete(newFlows, existingFlows.Items)

	// Process groups
	newGroups, groupsToDelete, updatedAllGroups := groupsToUpdateToDelete(newGroups, existingGroups.Items)

	// Sanity check
	if (len(updatedAllFlows) | len(flowsToDelete) | len(updatedAllGroups) | len(groupsToDelete)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": newFlows, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	// store the changed data
	device.Flows = &voltha.Flows{Items: updatedAllFlows}
	device.FlowGroups = &voltha.FlowGroups{Items: updatedAllGroups}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-device-%s", agent.deviceID)
	}

	// Send update to adapters
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	response := coreutils.NewResponse()
	if !dType.AcceptsAddRemoveFlowUpdates {
		if len(updatedAllGroups) != 0 && reflect.DeepEqual(existingGroups.Items, updatedAllGroups) && len(updatedAllFlows) != 0 && reflect.DeepEqual(existingFlows.Items, updatedAllFlows) {
			logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": newFlows, "groups": newGroups})
			cancel()
			return coreutils.DoneResponse(), nil
		}
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, &voltha.Flows{Items: updatedAllFlows}, &voltha.FlowGroups{Items: updatedAllGroups}, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &voltha.Flows{Items: newFlows},
			ToRemove: &voltha.Flows{Items: flowsToDelete},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &voltha.FlowGroups{Items: newGroups},
			ToRemove: &voltha.FlowGroups{Items: groupsToDelete},
			ToUpdate: &voltha.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		rpcResponse, err := agent.adapterProxy.UpdateFlowsIncremental(subCtx, device, flowChanges, groupChanges, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	}
	return response, nil
}

//addFlowsAndGroups adds the "newFlows" and "newGroups" from the existing flows/groups and sends the update to the
//adapters
func (agent *Agent) addFlowsAndGroups(ctx context.Context, newFlows []*ofp.OfpFlowStats, newGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	response, err := agent.addFlowsAndGroupsToAdapter(ctx, newFlows, newGroups, flowMetadata)
	if err != nil {
		return err
	}
	if errs := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); errs != nil {
		logger.Warnw("no-adapter-response", log.Fields{"device-id": agent.deviceID, "result": errs})
		return status.Errorf(codes.Aborted, "flow-failure-device-%s", agent.deviceID)
	}
	return nil
}

func (agent *Agent) deleteFlowsAndGroupsFromAdapter(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw("delete-flows-groups-from-adapter", log.Fields{"device-id": agent.deviceID, "flows": flowsToDel, "groups": groupsToDel})

	if (len(flowsToDel) | len(groupsToDel)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": flowsToDel, "groups": groupsToDel})
		return coreutils.DoneResponse(), nil
	}

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return coreutils.DoneResponse(), err
	}
	defer agent.requestQueue.RequestComplete()

	device := agent.getDeviceWithoutLock()
	dType := agent.adapterMgr.GetDeviceType(device.Type)
	if dType == nil {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}

	existingFlows := proto.Clone(device.Flows).(*voltha.Flows)
	existingGroups := proto.Clone(device.FlowGroups).(*ofp.FlowGroups)

	var flowsToKeep []*ofp.OfpFlowStats
	var groupsToKeep []*ofp.OfpGroupEntry

	// Process flows
	for _, flow := range existingFlows.Items {
		if idx := fu.FindFlows(flowsToDel, flow); idx == -1 {
			flowsToKeep = append(flowsToKeep, flow)
		}
	}

	// Process groups
	for _, group := range existingGroups.Items {
		if fu.FindGroup(groupsToDel, group.Desc.GroupId) == -1 { // does not exist now
			groupsToKeep = append(groupsToKeep, group)
		}
	}

	logger.Debugw("deleteFlowsAndGroups",
		log.Fields{
			"device-id":      agent.deviceID,
			"flows-to-del":   len(flowsToDel),
			"flows-to-keep":  len(flowsToKeep),
			"groups-to-del":  len(groupsToDel),
			"groups-to-keep": len(groupsToKeep),
		})

	// Sanity check
	if (len(flowsToKeep) | len(flowsToDel) | len(groupsToKeep) | len(groupsToDel)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows-to-del": flowsToDel, "groups-to-del": groupsToDel})
		return coreutils.DoneResponse(), nil
	}

	// store the changed data
	device.Flows = &voltha.Flows{Items: flowsToKeep}
	device.FlowGroups = &voltha.FlowGroups{Items: groupsToKeep}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-%s", agent.deviceID)
	}

	// Send update to adapters
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	response := coreutils.NewResponse()
	if !dType.AcceptsAddRemoveFlowUpdates {
		if len(groupsToKeep) != 0 && reflect.DeepEqual(existingGroups.Items, groupsToKeep) && len(flowsToKeep) != 0 && reflect.DeepEqual(existingFlows.Items, flowsToKeep) {
			logger.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flowsToDel": flowsToDel, "groupsToDel": groupsToDel})
			cancel()
			return coreutils.DoneResponse(), nil
		}
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, &voltha.Flows{Items: flowsToKeep}, &voltha.FlowGroups{Items: groupsToKeep}, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &voltha.Flows{Items: flowsToDel},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &voltha.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &voltha.FlowGroups{Items: groupsToDel},
			ToUpdate: &voltha.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		rpcResponse, err := agent.adapterProxy.UpdateFlowsIncremental(subCtx, device, flowChanges, groupChanges, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	}
	return response, nil
}

//deleteFlowsAndGroups removes the "flowsToDel" and "groupsToDel" from the existing flows/groups and sends the update to the
//adapters
func (agent *Agent) deleteFlowsAndGroups(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	response, err := agent.deleteFlowsAndGroupsFromAdapter(ctx, flowsToDel, groupsToDel, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//filterOutFlows removes flows from a device using the uni-port as filter
func (agent *Agent) filterOutFlows(ctx context.Context, uniPort uint32, flowMetadata *voltha.FlowMetadata) error {
	device, err := agent.getDevice(ctx)
	if err != nil {
		return err
	}
	existingFlows := proto.Clone(device.Flows).(*voltha.Flows)
	var flowsToDelete []*ofp.OfpFlowStats

	// If an existing flow has the uniPort as an InPort or OutPort or as a Tunnel ID then it needs to be removed
	for _, flow := range existingFlows.Items {
		if fu.GetInPort(flow) == uniPort || fu.GetOutPort(flow) == uniPort || fu.GetTunnelId(flow) == uint64(uniPort) {
			flowsToDelete = append(flowsToDelete, flow)
		}
	}
	logger.Debugw("flows-to-delete", log.Fields{"device-id": agent.deviceID, "uni-port": uniPort, "flows": flowsToDelete})
	if len(flowsToDelete) == 0 {
		return nil
	}

	response, err := agent.deleteFlowsAndGroupsFromAdapter(ctx, flowsToDelete, []*ofp.OfpGroupEntry{}, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

func (agent *Agent) updateFlowsAndGroupsToAdapter(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw("updateFlowsAndGroups", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})

	if (len(updatedFlows) | len(updatedGroups)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})
		return coreutils.DoneResponse(), nil
	}

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return coreutils.DoneResponse(), err
	}
	defer agent.requestQueue.RequestComplete()

	device := agent.getDeviceWithoutLock()
	if device.OperStatus != voltha.OperStatus_ACTIVE || device.ConnectStatus != voltha.ConnectStatus_REACHABLE || device.AdminState != voltha.AdminState_ENABLED {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "invalid device states")
	}
	dType := agent.adapterMgr.GetDeviceType(device.Type)
	if dType == nil {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}

	existingFlows := proto.Clone(device.Flows).(*voltha.Flows)
	existingGroups := proto.Clone(device.FlowGroups).(*ofp.FlowGroups)

	if len(updatedGroups) != 0 && reflect.DeepEqual(existingGroups.Items, updatedGroups) && len(updatedFlows) != 0 && reflect.DeepEqual(existingFlows.Items, updatedFlows) {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})
		return coreutils.DoneResponse(), nil
	}

	logger.Debugw("updating-flows-and-groups",
		log.Fields{
			"device-id":      agent.deviceID,
			"updated-flows":  updatedFlows,
			"updated-groups": updatedGroups,
		})

	// store the updated data
	device.Flows = &voltha.Flows{Items: updatedFlows}
	device.FlowGroups = &voltha.FlowGroups{Items: updatedGroups}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-%s", agent.deviceID)
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	response := coreutils.NewResponse()
	// Process bulk flow update differently than incremental update
	if !dType.AcceptsAddRemoveFlowUpdates {
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, &voltha.Flows{Items: updatedFlows}, &voltha.FlowGroups{Items: updatedGroups}, nil)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	} else {
		var flowsToAdd []*ofp.OfpFlowStats
		var flowsToDelete []*ofp.OfpFlowStats
		var groupsToAdd []*ofp.OfpGroupEntry
		var groupsToDelete []*ofp.OfpGroupEntry

		// Process flows
		for _, flow := range updatedFlows {
			if idx := fu.FindFlows(existingFlows.Items, flow); idx == -1 {
				flowsToAdd = append(flowsToAdd, flow)
			}
		}
		for _, flow := range existingFlows.Items {
			if idx := fu.FindFlows(updatedFlows, flow); idx != -1 {
				flowsToDelete = append(flowsToDelete, flow)
			}
		}

		// Process groups
		for _, g := range updatedGroups {
			if fu.FindGroup(existingGroups.Items, g.Desc.GroupId) == -1 { // does not exist now
				groupsToAdd = append(groupsToAdd, g)
			}
		}
		for _, group := range existingGroups.Items {
			if fu.FindGroup(updatedGroups, group.Desc.GroupId) != -1 { // does not exist now
				groupsToDelete = append(groupsToDelete, group)
			}
		}

		logger.Debugw("updating-flows-and-groups",
			log.Fields{
				"device-id":        agent.deviceID,
				"flows-to-add":     flowsToAdd,
				"flows-to-delete":  flowsToDelete,
				"groups-to-add":    groupsToAdd,
				"groups-to-delete": groupsToDelete,
			})

		// Sanity check
		if (len(flowsToAdd) | len(flowsToDelete) | len(groupsToAdd) | len(groupsToDelete) | len(updatedGroups)) == 0 {
			logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})
			cancel()
			return coreutils.DoneResponse(), nil
		}

		flowChanges := &ofp.FlowChanges{
			ToAdd:    &voltha.Flows{Items: flowsToAdd},
			ToRemove: &voltha.Flows{Items: flowsToDelete},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &voltha.FlowGroups{Items: groupsToAdd},
			ToRemove: &voltha.FlowGroups{Items: groupsToDelete},
			ToUpdate: &voltha.FlowGroups{Items: updatedGroups},
		}
		rpcResponse, err := agent.adapterProxy.UpdateFlowsIncremental(subCtx, device, flowChanges, groupChanges, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	}

	return response, nil
}

//updateFlowsAndGroups replaces the existing flows and groups with "updatedFlows" and "updatedGroups" respectively. It
//also sends the updates to the adapters
func (agent *Agent) updateFlowsAndGroups(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	response, err := agent.updateFlowsAndGroupsToAdapter(ctx, updatedFlows, updatedGroups, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//deleteAllFlows deletes all flows in the device table
func (agent *Agent) deleteAllFlows(ctx context.Context) error {
	logger.Debugw("deleteAllFlows", log.Fields{"deviceId": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	device := agent.getDeviceWithoutLock()
	// purge all flows on the device by setting it to nil
	device.Flows = &ofp.Flows{Items: nil}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		// The caller logs the error
		return err
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

func (agent *Agent) updatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("updatePmConfigs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.getDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.UpdatePmConfigs(subCtx, cloned, pmConfigs)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "updatePmConfigs", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) initPmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("initPmConfigs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.getDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	updateCtx := context.WithValue(ctx, model.RequestTimestamp, time.Now().UnixNano())
	return agent.updateDeviceInStoreWithoutLock(updateCtx, cloned, false, "")
}

func (agent *Agent) listPmConfigs(ctx context.Context) (*voltha.PmConfigs, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("listPmConfigs", log.Fields{"device-id": agent.deviceID})

	return agent.getDeviceWithoutLock().PmConfigs, nil
}

func (agent *Agent) downloadImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw("downloadImage", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceWithoutLock()

	if device.AdminState != voltha.AdminState_ENABLED {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, expected-admin-state:%s", agent.deviceID, voltha.AdminState_ENABLED)
	}
	// Save the image
	clonedImg := proto.Clone(img).(*voltha.ImageDownload)
	clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
	cloned := proto.Clone(device).(*voltha.Device)
	if cloned.ImageDownloads == nil {
		cloned.ImageDownloads = []*voltha.ImageDownload{clonedImg}
	} else {
		if device.AdminState != voltha.AdminState_ENABLED {
			logger.Debugw("device-not-enabled", log.Fields{"id": agent.deviceID})
			return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, expected-admin-state:%s", agent.deviceID, voltha.AdminState_ENABLED)
		}
		// Save the image
		clonedImg := proto.Clone(img).(*voltha.ImageDownload)
		clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
		if device.ImageDownloads == nil {
			device.ImageDownloads = []*voltha.ImageDownload{clonedImg}
		} else {
			device.ImageDownloads = append(device.ImageDownloads, clonedImg)
		}
		if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_DOWNLOADING_IMAGE, device.ConnectStatus, device.OperStatus); err != nil {
			return nil, err
		}

		// Send the request to the adapter
		subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
		ch, err := agent.adapterProxy.DownloadImage(ctx, cloned, clonedImg)
		if err != nil {
			cancel()
			return nil, err
		}
		go agent.waitForAdapterResponse(subCtx, cancel, "downloadImage", ch, agent.onSuccess, agent.onFailure)
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

// isImageRegistered is a helper method to figure out if an image is already registered
func isImageRegistered(img *voltha.ImageDownload, device *voltha.Device) bool {
	for _, image := range device.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return true
		}
	}
	return false
}

func (agent *Agent) cancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw("cancelImageDownload", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, device) {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	// Update image download state
	for _, image := range device.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.DownloadState = voltha.ImageDownload_DOWNLOAD_CANCELLED
		}
	}

	if device.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		// Set the device to Enabled
		if err := agent.updateDeviceStateInStoreWithoutLock(ctx, device, voltha.AdminState_ENABLED, device.ConnectStatus, device.OperStatus); err != nil {
			return nil, err
		}
		subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
		ch, err := agent.adapterProxy.CancelImageDownload(subCtx, device, img)
		if err != nil {
			cancel()
			return nil, err
		}
		go agent.waitForAdapterResponse(subCtx, cancel, "cancelImageDownload", ch, agent.onSuccess, agent.onFailure)
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) activateImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("activateImage", log.Fields{"device-id": agent.deviceID})
	cloned := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, cloned) {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	if cloned.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, device-in-downloading-state:%s", agent.deviceID, img.Name)
	}
	// Update image download state
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.ImageState = voltha.ImageDownload_IMAGE_ACTIVATING
		}
	}
	// Set the device to downloading_image
	if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_DOWNLOADING_IMAGE, cloned.ConnectStatus, cloned.OperStatus); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.ActivateImageUpdate(subCtx, proto.Clone(cloned).(*voltha.Device), img)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "activateImageUpdate", ch, agent.onSuccess, agent.onFailure)

	// The status of the AdminState will be changed following the update_download_status response from the adapter
	// The image name will also be removed from the device list
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) revertImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("revertImage", log.Fields{"device-id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, cloned) {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	if cloned.AdminState != voltha.AdminState_ENABLED {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, device-not-enabled-state:%s", agent.deviceID, img.Name)
	}
	// Update image download state
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.ImageState = voltha.ImageDownload_IMAGE_REVERTING
		}
	}

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.RevertImageUpdate(subCtx, proto.Clone(cloned).(*voltha.Device), img)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "revertImageUpdate", ch, agent.onSuccess, agent.onFailure)

	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) getImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw("getImageDownloadStatus", log.Fields{"device-id": agent.deviceID})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	device := agent.getDeviceWithoutLock()
	ch, err := agent.adapterProxy.GetImageDownloadStatus(ctx, device, img)
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
	// Successful response
	imgDownload := &voltha.ImageDownload{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, imgDownload); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}
	return imgDownload, nil
}

func (agent *Agent) updateImageDownload(ctx context.Context, img *voltha.ImageDownload) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("updating-image-download", log.Fields{"device-id": agent.deviceID, "img": img})

	cloned := agent.getDeviceWithoutLock()

	// Update the image as well as remove it if the download was cancelled
	clonedImages := make([]*voltha.ImageDownload, len(cloned.ImageDownloads))
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			if image.DownloadState != voltha.ImageDownload_DOWNLOAD_CANCELLED {
				clonedImages = append(clonedImages, img)
			}
		}
	}
	cloned.ImageDownloads = clonedImages
	// Set the Admin state to enabled if required
	if (img.DownloadState != voltha.ImageDownload_DOWNLOAD_REQUESTED &&
		img.DownloadState != voltha.ImageDownload_DOWNLOAD_STARTED) ||
		(img.ImageState != voltha.ImageDownload_IMAGE_ACTIVATING) {
		return agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_ENABLED, cloned.ConnectStatus, cloned.OperStatus)
	}
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) getImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("getImageDownload", log.Fields{"device-id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return image, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "image-not-found:%s", img.Name)
}

func (agent *Agent) listImageDownloads(ctx context.Context, deviceID string) (*voltha.ImageDownloads, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("listImageDownloads", log.Fields{"device-id": agent.deviceID})

	return &voltha.ImageDownloads{Items: agent.getDeviceWithoutLock().ImageDownloads}, nil
}

// getPorts retrieves the ports information of the device based on the port type.
func (agent *Agent) getPorts(ctx context.Context, portType voltha.Port_PortType) *voltha.Ports {
	logger.Debugw("getPorts", log.Fields{"device-id": agent.deviceID, "port-type": portType})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.GetDevice(ctx, agent.deviceID); device != nil {
		for _, port := range device.Ports {
			if port.Type == portType {
				ports.Items = append(ports.Items, port)
			}
		}
	}
	return ports
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

// getPortCapability retrieves the port capability of a device
func (agent *Agent) getPortCapability(ctx context.Context, portNo uint32) (*ic.PortCapability, error) {
	logger.Debugw("getPortCapability", log.Fields{"device-id": agent.deviceID})
	device, err := agent.getDevice(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := agent.adapterProxy.GetOfpPortInfo(ctx, device, portNo)
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
	portCap := &ic.PortCapability{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, portCap); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}
	return portCap, nil
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

func (agent *Agent) updateDeviceWithoutLock(ctx context.Context, device *voltha.Device) error {
	logger.Debugw("updateDevice", log.Fields{"deviceId": device.Id})
	//cloned := proto.Clone(device).(*voltha.Device)
	cloned := device
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

func (agent *Agent) updatePortsOperState(ctx context.Context, operStatus voltha.OperStatus_Types) error {
	logger.Debugw("updatePortsOperState", log.Fields{"device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	cloned := agent.getDeviceWithoutLock()
	for _, port := range cloned.Ports {
		port.OperStatus = operStatus
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) updatePortState(ctx context.Context, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_Types) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Work only on latest data
	// TODO: Get list of ports from device directly instead of the entire device
	cloned := agent.getDeviceWithoutLock()

	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if _, ok := voltha.Port_PortType_value[portType.String()]; !ok {
		return status.Errorf(codes.InvalidArgument, "%s", portType)
	}
	for _, port := range cloned.Ports {
		if port.Type == portType && port.PortNo == portNo {
			port.OperStatus = operStatus
		}
	}
	logger.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) deleteAllPorts(ctx context.Context) error {
	logger.Debugw("deleteAllPorts", log.Fields{"deviceId": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	cloned := agent.getDeviceWithoutLock()

	if cloned.AdminState != voltha.AdminState_DISABLED && cloned.AdminState != voltha.AdminState_DELETED {
		err := status.Error(codes.FailedPrecondition, fmt.Sprintf("invalid-state-%v", cloned.AdminState))
		logger.Warnw("invalid-state-removing-ports", log.Fields{"state": cloned.AdminState, "error": err})
		return err
	}
	if len(cloned.Ports) == 0 {
		logger.Debugw("no-ports-present", log.Fields{"deviceId": agent.deviceID})
		return nil
	}

	cloned.Ports = []*voltha.Port{}
	logger.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) addPort(ctx context.Context, port *voltha.Port) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("addPort", log.Fields{"deviceId": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	updatePort := false
	if cloned.Ports == nil {
		//	First port
		logger.Debugw("addPort-first-port-to-add", log.Fields{"deviceId": agent.deviceID})
		cloned.Ports = make([]*voltha.Port, 0)
	} else {
		for _, p := range cloned.Ports {
			if p.Type == port.Type && p.PortNo == port.PortNo {
				if p.Label == "" && p.Type == voltha.Port_PON_OLT {
					//Creation of OLT PON port is being processed after a default PON port was created.  Just update it.
					logger.Infow("update-pon-port-created-by-default", log.Fields{"default-port": p, "port-to-add": port})
					p.Label = port.Label
					p.OperStatus = port.OperStatus
					updatePort = true
					break
				}
				logger.Debugw("port already exists", log.Fields{"port": port})
				return nil
			}
		}
	}
	if !updatePort {
		cp := proto.Clone(port).(*voltha.Port)
		// Set the admin state of the port to ENABLE
		cp.AdminState = voltha.AdminState_ENABLED
		cloned.Ports = append(cloned.Ports, cp)
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) addPeerPort(ctx context.Context, peerPort *voltha.Port_PeerPort) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("adding-peer-peerPort", log.Fields{"device-id": agent.deviceID, "peer-peerPort": peerPort})

	cloned := agent.getDeviceWithoutLock()

	// Get the peer port on the device based on the peerPort no
	found := false
	for _, port := range cloned.Ports {
		if port.PortNo == peerPort.PortNo { // found peerPort
			cp := proto.Clone(peerPort).(*voltha.Port_PeerPort)
			port.Peers = append(port.Peers, cp)
			logger.Debugw("found-peer", log.Fields{"device-id": agent.deviceID, "portNo": peerPort.PortNo, "deviceId": agent.deviceID})
			found = true
			break
		}
	}
	if !found && agent.isRootdevice {
		// An ONU PON port has been created before the corresponding creation of the OLT PON port.  Create the OLT PON port
		// with default values which will be updated once the OLT PON port creation is processed.
		ponPort := &voltha.Port{
			PortNo:     peerPort.PortNo,
			Type:       voltha.Port_PON_OLT,
			AdminState: voltha.AdminState_ENABLED,
			DeviceId:   agent.deviceID,
			Peers:      []*voltha.Port_PeerPort{proto.Clone(peerPort).(*voltha.Port_PeerPort)},
		}
		cloned.Ports = append(cloned.Ports, ponPort)
		logger.Infow("adding-default-pon-port", log.Fields{"device-id": agent.deviceID, "peer": peerPort, "pon-port": ponPort})
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
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

func (agent *Agent) simulateAlarm(ctx context.Context, simulatereq *voltha.SimulateAlarmRequest) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("simulateAlarm", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.SimulateAlarm(subCtx, cloned, simulatereq)
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

	updateCtx := context.WithValue(ctx, model.RequestTimestamp, time.Now().UnixNano())
	if err := agent.clusterDataProxy.Update(updateCtx, "devices/"+agent.deviceID, device); err != nil {
		return status.Errorf(codes.Internal, "failed-update-device:%s: %s", agent.deviceID, err)
	}
	logger.Debugw("updated-device-in-store", log.Fields{"deviceId: ": agent.deviceID})

	agent.device = proto.Clone(device).(*voltha.Device)
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

func (agent *Agent) disablePort(ctx context.Context, Port *voltha.Port) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("disablePort", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo})
	var cp *voltha.Port
	// Get the most up to date the device info
	device := agent.getDeviceWithoutLock()
	for _, port := range device.Ports {
		if port.PortNo == Port.PortNo {
			port.AdminState = voltha.AdminState_DISABLED
			cp = proto.Clone(port).(*voltha.Port)
			break

		}
	}
	if cp == nil {
		return status.Errorf(codes.InvalidArgument, "%v", Port.PortNo)
	}

	if cp.Type != voltha.Port_PON_OLT {
		return status.Errorf(codes.InvalidArgument, "Disabling of Port Type %v unimplemented", cp.Type)
	}
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, device, false, ""); err != nil {
		logger.Debugw("updateDeviceInStoreWithoutLock error ", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}

	//send request to adapter
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.DisablePort(ctx, device, cp)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "disablePort", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) enablePort(ctx context.Context, Port *voltha.Port) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("enablePort", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo})

	var cp *voltha.Port
	// Get the most up to date the device info
	device := agent.getDeviceWithoutLock()
	for _, port := range device.Ports {
		if port.PortNo == Port.PortNo {
			port.AdminState = voltha.AdminState_ENABLED
			cp = proto.Clone(port).(*voltha.Port)
			break
		}
	}

	if cp == nil {
		return status.Errorf(codes.InvalidArgument, "%v", Port.PortNo)
	}

	if cp.Type != voltha.Port_PON_OLT {
		return status.Errorf(codes.InvalidArgument, "Enabling of Port Type %v unimplemented", cp.Type)
	}
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, device, false, ""); err != nil {
		logger.Debugw("updateDeviceInStoreWithoutLock error ", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}
	//send request to adapter
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.EnablePort(ctx, device, cp)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "enablePort", ch, agent.onSuccess, agent.onFailure)
	return nil
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
