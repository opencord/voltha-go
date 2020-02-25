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
	"fmt"
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

// DeviceAgent represents device agent attributes
type DeviceAgent struct {
	deviceID         string
	parentID         string
	deviceType       string
	isRootdevice     bool
	adapterProxy     *AdapterProxy
	adapterMgr       *AdapterManager
	deviceMgr        *DeviceManager
	clusterDataProxy *model.Proxy
	deviceProxy      *model.Proxy
	exitChannel      chan int
	lockDevice       sync.RWMutex
	device           *voltha.Device
	defaultTimeout   int64
}

//newDeviceAgent creates a new device agent. The device will be initialized when start() is called.
func newDeviceAgent(ap *AdapterProxy, device *voltha.Device, deviceMgr *DeviceManager, cdProxy *model.Proxy, timeout int64) *DeviceAgent {
	var agent DeviceAgent
	agent.adapterProxy = ap
	if device.Id == "" {
		agent.deviceID = CreateDeviceID()
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
	agent.lockDevice = sync.RWMutex{}
	agent.defaultTimeout = timeout
	agent.device = proto.Clone(device).(*voltha.Device)
	return &agent
}

// start()
// save the device to the data model and registers for callbacks on that device if deviceToCreate!=nil.  Otherwise,
// it will load the data from the dB and setup the necessary callbacks and proxies. Returns the device that
// was started.
func (agent *DeviceAgent) start(ctx context.Context, deviceToCreate *voltha.Device) (*voltha.Device, error) {
	var device *voltha.Device

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("starting-device-agent", log.Fields{"deviceId": agent.deviceID})
	if deviceToCreate == nil {
		// Load the existing device
		loadedDevice, err := agent.clusterDataProxy.Get(ctx, "/devices/"+agent.deviceID, 1, true, "")
		if err != nil {
			log.Errorw("failed-to-get-from-cluster-data-proxy", log.Fields{"error": err})
			return nil, err
		}
		if loadedDevice != nil {
			var ok bool
			if device, ok = loadedDevice.(*voltha.Device); ok {
				agent.deviceType = device.Adapter
				agent.device = proto.Clone(device).(*voltha.Device)
			} else {
				log.Errorw("failed-to-convert-device", log.Fields{"deviceId": agent.deviceID})
				return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
			}
		} else {
			log.Errorw("failed-to-load-device", log.Fields{"deviceId": agent.deviceID})
			return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
		}
		log.Debugw("device-loaded-from-dB", log.Fields{"deviceId": agent.deviceID})
	} else {
		// Create a new device
		// Assumption is that AdminState, FlowGroups, and Flows are unitialized since this
		// is a new device, so populate them here before passing the device to clusterDataProxy.AddWithId.
		// agent.deviceId will also have been set during newDeviceAgent().
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
		added, err := agent.clusterDataProxy.AddWithID(ctx, "/devices", agent.deviceID, device, "")
		if err != nil {
			log.Errorw("failed-to-save-devices-to-cluster-proxy", log.Fields{"error": err})
			return nil, err
		}
		if added == nil {
			log.Errorw("failed-to-add-device", log.Fields{"deviceId": agent.deviceID})
			return nil, status.Errorf(codes.Aborted, "failed-adding-device-%s", agent.deviceID)
		}
		agent.device = proto.Clone(device).(*voltha.Device)
	}
	var err error
	if agent.deviceProxy, err = agent.clusterDataProxy.CreateProxy(ctx, "/devices/"+agent.deviceID, false); err != nil {
		log.Errorw("failed-to-add-devices-to-cluster-proxy", log.Fields{"error": err})
		return nil, err
	}
	agent.deviceProxy.RegisterCallback(model.PostUpdate, agent.processUpdate)

	log.Debugw("device-agent-started", log.Fields{"deviceId": agent.deviceID})
	return device, nil
}

// stop stops the device agent.  Not much to do for now
func (agent *DeviceAgent) stop(ctx context.Context) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	log.Debugw("stopping-device-agent", log.Fields{"deviceId": agent.deviceID, "parentId": agent.parentID})

	// First unregister any callbacks
	agent.deviceProxy.UnregisterCallback(model.PostUpdate, agent.processUpdate)

	//	Remove the device from the KV store
	removed, err := agent.clusterDataProxy.Remove(ctx, "/devices/"+agent.deviceID, "")
	if err != nil {
		log.Errorw("Failed-to-remove-device-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	if removed == nil {
		log.Debugw("device-already-removed", log.Fields{"id": agent.deviceID})
	}
	agent.exitChannel <- 1
	log.Debugw("device-agent-stopped", log.Fields{"deviceId": agent.deviceID, "parentId": agent.parentID})
}

// Load the most recent state from the KVStore for the device.
func (agent *DeviceAgent) reconcileWithKVStore(ctx context.Context) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debug("reconciling-device-agent-devicetype")
	// TODO: context timeout
	device, err := agent.clusterDataProxy.Get(ctx, "/devices/"+agent.deviceID, 1, true, "")
	if err != nil {
		log.Errorw("Failed to get device info from cluster data proxy", log.Fields{"error": err})
		return
	}
	if device != nil {
		if d, ok := device.(*voltha.Device); ok {
			agent.deviceType = d.Adapter
			agent.device = proto.Clone(d).(*voltha.Device)
			log.Debugw("reconciled-device-agent-devicetype", log.Fields{"Id": agent.deviceID, "type": agent.deviceType})
		}
	}
}

// getDevice returns the device data from cache
func (agent *DeviceAgent) getDevice() *voltha.Device {
	agent.lockDevice.RLock()
	defer agent.lockDevice.RUnlock()
	return proto.Clone(agent.device).(*voltha.Device)
}

// getDeviceWithoutLock is a helper function to be used ONLY by any device agent function AFTER it has acquired the device lock.
func (agent *DeviceAgent) getDeviceWithoutLock() *voltha.Device {
	return proto.Clone(agent.device).(*voltha.Device)
}

// enableDevice activates a preprovisioned or a disable device
func (agent *DeviceAgent) enableDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("enableDevice", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	// First figure out which adapter will handle this device type.  We do it at this stage as allow devices to be
	// pre-provisionned with the required adapter not registered.   At this stage, since we need to communicate
	// with the adapter then we need to know the adapter that will handle this request
	adapterName, err := agent.adapterMgr.getAdapterName(cloned.Type)
	if err != nil {
		log.Warnw("no-adapter-registered-for-device-type", log.Fields{"deviceType": cloned.Type, "deviceAdapter": cloned.Adapter})
		return err
	}
	cloned.Adapter = adapterName

	if cloned.AdminState == voltha.AdminState_ENABLED {
		log.Debugw("device-already-enabled", log.Fields{"id": agent.deviceID})
		return nil
	}

	if cloned.AdminState == voltha.AdminState_DELETED {
		// This is a temporary state when a device is deleted before it gets removed from the model.
		err = status.Error(codes.FailedPrecondition, fmt.Sprintf("cannot-enable-a-deleted-device: %s ", cloned.Id))
		log.Warnw("invalid-state", log.Fields{"id": agent.deviceID, "state": cloned.AdminState, "error": err})
		return err
	}

	previousAdminState := cloned.AdminState

	// Update the Admin State and set the operational state to activating before sending the request to the
	// Adapters
	cloned.AdminState = voltha.AdminState_ENABLED
	cloned.OperStatus = voltha.OperStatus_ACTIVATING

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}

	// Adopt the device if it was in preprovision state.  In all other cases, try to reenable it.
	device := proto.Clone(cloned).(*voltha.Device)
	if previousAdminState == voltha.AdminState_PREPROVISIONED {
		if err := agent.adapterProxy.AdoptDevice(ctx, device); err != nil {
			log.Debugw("adoptDevice-error", log.Fields{"id": agent.deviceID, "error": err})
			return err
		}
	} else {
		if err := agent.adapterProxy.ReEnableDevice(ctx, device); err != nil {
			log.Debugw("renableDevice-error", log.Fields{"id": agent.deviceID, "error": err})
			return err
		}
	}
	return nil
}

func (agent *DeviceAgent) sendBulkFlowsToAdapters(ctx context.Context, device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata, response coreutils.Response) {
	if err := agent.adapterProxy.UpdateFlowsBulk(ctx, device, flows, groups, flowMetadata); err != nil {
		log.Debugw("update-flow-bulk-error", log.Fields{"id": agent.deviceID, "error": err})
		response.Error(err)
	}
	response.Done()
}

func (agent *DeviceAgent) sendIncrementalFlowsToAdapters(ctx context.Context, device *voltha.Device, flows *ofp.FlowChanges, groups *ofp.FlowGroupChanges, flowMetadata *voltha.FlowMetadata, response coreutils.Response) {
	if err := agent.adapterProxy.UpdateFlowsIncremental(ctx, device, flows, groups, flowMetadata); err != nil {
		log.Debugw("update-flow-incremental-error", log.Fields{"id": agent.deviceID, "error": err})
		response.Error(err)
	}
	response.Done()
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

func (agent *DeviceAgent) addFlowsAndGroupsToAdapter(ctx context.Context, newFlows []*ofp.OfpFlowStats, newGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	log.Debugw("addFlowsAndGroups", log.Fields{"deviceId": agent.deviceID, "flows": newFlows, "groups": newGroups, "flowMetadata": flowMetadata})

	if (len(newFlows) | len(newGroups)) == 0 {
		log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": newFlows, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	device := agent.getDeviceWithoutLock()
	existingFlows := proto.Clone(device.Flows).(*voltha.Flows)
	existingGroups := proto.Clone(device.FlowGroups).(*ofp.FlowGroups)

	// Process flows
	newFlows, flowsToDelete, updatedAllFlows := flowsToUpdateToDelete(newFlows, existingFlows.Items)

	// Process groups
	newGroups, groupsToDelete, updatedAllGroups := groupsToUpdateToDelete(newGroups, existingGroups.Items)

	// Sanity check
	if (len(updatedAllFlows) | len(flowsToDelete) | len(updatedAllGroups) | len(groupsToDelete)) == 0 {
		log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": newFlows, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	// Send update to adapters
	// Create two channels to receive responses from the dB and from the adapters.
	// Do not close these channels as this function may exit on timeout before the dB or adapters get a chance
	// to send their responses.  These channels will be garbage collected once all the responses are
	// received
	response := coreutils.NewResponse()
	dType := agent.adapterMgr.getDeviceType(device.Type)
	if dType == nil {
		log.Errorw("non-existent device type", log.Fields{"deviceType": device.Type})
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent device type %s", device.Type)
	}
	if !dType.AcceptsAddRemoveFlowUpdates {

		if len(updatedAllGroups) != 0 && reflect.DeepEqual(existingGroups.Items, updatedAllGroups) && len(updatedAllFlows) != 0 && reflect.DeepEqual(existingFlows.Items, updatedAllFlows) {
			log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": newFlows, "groups": newGroups})
			return coreutils.DoneResponse(), nil
		}
		go agent.sendBulkFlowsToAdapters(ctx, device, &voltha.Flows{Items: updatedAllFlows}, &voltha.FlowGroups{Items: updatedAllGroups}, flowMetadata, response)

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
		go agent.sendIncrementalFlowsToAdapters(ctx, device, flowChanges, groupChanges, flowMetadata, response)
	}

	// store the changed data
	device.Flows = &voltha.Flows{Items: updatedAllFlows}
	device.FlowGroups = &voltha.FlowGroups{Items: updatedAllGroups}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-%s", agent.deviceID)
	}

	return response, nil
}

//addFlowsAndGroups adds the "newFlows" and "newGroups" from the existing flows/groups and sends the update to the
//adapters
func (agent *DeviceAgent) addFlowsAndGroups(ctx context.Context, newFlows []*ofp.OfpFlowStats, newGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	response, err := agent.addFlowsAndGroupsToAdapter(ctx, newFlows, newGroups, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); res != nil {
		log.Debugw("Failed to get response from adapter[or] DB", log.Fields{"result": res})
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

func (agent *DeviceAgent) deleteFlowsAndGroupsFromAdapter(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	log.Debugw("deleteFlowsAndGroups", log.Fields{"deviceId": agent.deviceID, "flows": flowsToDel, "groups": groupsToDel})

	if (len(flowsToDel) | len(groupsToDel)) == 0 {
		log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": flowsToDel, "groups": groupsToDel})
		return coreutils.DoneResponse(), nil
	}

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	device := agent.getDeviceWithoutLock()

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

	log.Debugw("deleteFlowsAndGroups",
		log.Fields{
			"deviceId":     agent.deviceID,
			"flowsToDel":   len(flowsToDel),
			"flowsToKeep":  len(flowsToKeep),
			"groupsToDel":  len(groupsToDel),
			"groupsToKeep": len(groupsToKeep),
		})

	// Sanity check
	if (len(flowsToKeep) | len(flowsToDel) | len(groupsToKeep) | len(groupsToDel)) == 0 {
		log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flowsToDel": flowsToDel, "groupsToDel": groupsToDel})
		return coreutils.DoneResponse(), nil
	}

	// Send update to adapters
	response := coreutils.NewResponse()
	dType := agent.adapterMgr.getDeviceType(device.Type)
	if dType == nil {
		log.Errorw("non-existent device type", log.Fields{"deviceType": device.Type})
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent device type %s", device.Type)
	}
	if !dType.AcceptsAddRemoveFlowUpdates {
		if len(groupsToKeep) != 0 && reflect.DeepEqual(existingGroups.Items, groupsToKeep) && len(flowsToKeep) != 0 && reflect.DeepEqual(existingFlows.Items, flowsToKeep) {
			log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flowsToDel": flowsToDel, "groupsToDel": groupsToDel})
			return coreutils.DoneResponse(), nil
		}
		go agent.sendBulkFlowsToAdapters(ctx, device, &voltha.Flows{Items: flowsToKeep}, &voltha.FlowGroups{Items: groupsToKeep}, flowMetadata, response)
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
		go agent.sendIncrementalFlowsToAdapters(ctx, device, flowChanges, groupChanges, flowMetadata, response)
	}

	// store the changed data
	device.Flows = &voltha.Flows{Items: flowsToKeep}
	device.FlowGroups = &voltha.FlowGroups{Items: groupsToKeep}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-%s", agent.deviceID)
	}

	return response, nil

}

//deleteFlowsAndGroups removes the "flowsToDel" and "groupsToDel" from the existing flows/groups and sends the update to the
//adapters
func (agent *DeviceAgent) deleteFlowsAndGroups(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	response, err := agent.deleteFlowsAndGroupsFromAdapter(ctx, flowsToDel, groupsToDel, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

func (agent *DeviceAgent) updateFlowsAndGroupsToAdapter(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	log.Debugw("updateFlowsAndGroups", log.Fields{"deviceId": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})

	if (len(updatedFlows) | len(updatedGroups)) == 0 {
		log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})
		return coreutils.DoneResponse(), nil
	}

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	device := agent.getDeviceWithoutLock()

	existingFlows := proto.Clone(device.Flows).(*voltha.Flows)
	existingGroups := proto.Clone(device.FlowGroups).(*ofp.FlowGroups)

	if len(updatedGroups) != 0 && reflect.DeepEqual(existingGroups.Items, updatedGroups) && len(updatedFlows) != 0 && reflect.DeepEqual(existingFlows.Items, updatedFlows) {
		log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})
		return coreutils.DoneResponse(), nil
	}

	log.Debugw("updating-flows-and-groups",
		log.Fields{
			"deviceId":      agent.deviceID,
			"updatedFlows":  updatedFlows,
			"updatedGroups": updatedGroups,
		})

	response := coreutils.NewResponse()
	dType := agent.adapterMgr.getDeviceType(device.Type)
	if dType == nil {
		log.Errorw("non-existent device type", log.Fields{"deviceType": device.Type})
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent device type %s", device.Type)
	}

	// Process bulk flow update differently than incremental update
	if !dType.AcceptsAddRemoveFlowUpdates {
		go agent.sendBulkFlowsToAdapters(ctx, device, &voltha.Flows{Items: updatedFlows}, &voltha.FlowGroups{Items: updatedGroups}, nil, response)
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

		log.Debugw("updating-flows-and-groups",
			log.Fields{
				"deviceId":       agent.deviceID,
				"flowsToAdd":     flowsToAdd,
				"flowsToDelete":  flowsToDelete,
				"groupsToAdd":    groupsToAdd,
				"groupsToDelete": groupsToDelete,
			})

		// Sanity check
		if (len(flowsToAdd) | len(flowsToDelete) | len(groupsToAdd) | len(groupsToDelete) | len(updatedGroups)) == 0 {
			log.Debugw("nothing-to-update", log.Fields{"deviceId": agent.deviceID, "flows": updatedFlows, "groups": updatedGroups})
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
		go agent.sendIncrementalFlowsToAdapters(ctx, device, flowChanges, groupChanges, flowMetadata, response)
	}

	// store the updated data
	device.Flows = &voltha.Flows{Items: updatedFlows}
	device.FlowGroups = &voltha.FlowGroups{Items: updatedGroups}
	if err := agent.updateDeviceWithoutLock(ctx, device); err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-%s", agent.deviceID)
	}

	return response, nil
}

//updateFlowsAndGroups replaces the existing flows and groups with "updatedFlows" and "updatedGroups" respectively. It
//also sends the updates to the adapters
func (agent *DeviceAgent) updateFlowsAndGroups(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	response, err := agent.updateFlowsAndGroupsToAdapter(ctx, updatedFlows, updatedGroups, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, response); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

//disableDevice disable a device
func (agent *DeviceAgent) disableDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("disableDevice", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	if cloned.AdminState == voltha.AdminState_DISABLED {
		log.Debugw("device-already-disabled", log.Fields{"id": agent.deviceID})
		return nil
	}
	if cloned.AdminState == voltha.AdminState_PREPROVISIONED ||
		cloned.AdminState == voltha.AdminState_DELETED {
		log.Debugw("device-not-enabled", log.Fields{"id": agent.deviceID})
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, invalid-admin-state:%s", agent.deviceID, cloned.AdminState)
	}

	// Update the Admin State and operational state before sending the request out
	cloned.AdminState = voltha.AdminState_DISABLED
	cloned.OperStatus = voltha.OperStatus_UNKNOWN
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	if err := agent.adapterProxy.DisableDevice(ctx, proto.Clone(cloned).(*voltha.Device)); err != nil {
		log.Debugw("disableDevice-error", log.Fields{"id": agent.deviceID, "error": err})
		return err
	}
	return nil
}

func (agent *DeviceAgent) updateAdminState(ctx context.Context, adminState voltha.AdminState_Types) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("updateAdminState", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	if cloned.AdminState == adminState {
		log.Debugw("no-change-needed", log.Fields{"id": agent.deviceID, "state": adminState})
		return nil
	}
	// Received an Ack (no error found above).  Now update the device in the model to the expected state
	cloned.AdminState = adminState
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	return nil
}

func (agent *DeviceAgent) rebootDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("rebootDevice", log.Fields{"id": agent.deviceID})

	device := agent.getDeviceWithoutLock()
	if err := agent.adapterProxy.RebootDevice(ctx, device); err != nil {
		log.Debugw("rebootDevice-error", log.Fields{"id": agent.deviceID, "error": err})
		return err
	}
	return nil
}

func (agent *DeviceAgent) deleteDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("deleteDevice", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	if cloned.AdminState == voltha.AdminState_DELETED {
		log.Debugw("device-already-in-deleted-state", log.Fields{"id": agent.deviceID})
		return nil
	}
	if cloned.AdminState != voltha.AdminState_PREPROVISIONED {
		if err := agent.adapterProxy.DeleteDevice(ctx, cloned); err != nil {
			log.Debugw("deleteDevice-error", log.Fields{"id": agent.deviceID, "error": err})
			return err
		}
	}
	//Set the state to deleted - this will trigger some background process to invoke parent adapter to delete child
	//device and clean up the device as well as its association with the logical device
	cloned.AdminState = voltha.AdminState_DELETED

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	return nil
}

func (agent *DeviceAgent) setParentID(ctx context.Context, device *voltha.Device, parentID string) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("setParentId", log.Fields{"deviceId": device.Id, "parentId": parentID})

	cloned := agent.getDeviceWithoutLock()
	cloned.ParentId = parentID
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	return nil
}

func (agent *DeviceAgent) updatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("updatePmConfigs", log.Fields{"id": pmConfigs.Id})

	cloned := agent.getDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	// Send the request to the adapter
	if err := agent.adapterProxy.UpdatePmConfigs(ctx, cloned, pmConfigs); err != nil {
		log.Errorw("update-pm-configs-error", log.Fields{"id": agent.deviceID, "error": err})
		return err
	}
	return nil
}

func (agent *DeviceAgent) initPmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("initPmConfigs", log.Fields{"id": pmConfigs.Id})

	cloned := agent.getDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	// Store the device
	updateCtx := context.WithValue(ctx, model.RequestTimestamp, time.Now().UnixNano())
	afterUpdate, err := agent.clusterDataProxy.Update(updateCtx, "/devices/"+agent.deviceID, cloned, false, "")
	if err != nil {
		return status.Errorf(codes.Internal, "%s", agent.deviceID)
	}
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "%s", agent.deviceID)
	}
	return nil
}

func (agent *DeviceAgent) listPmConfigs(ctx context.Context) (*voltha.PmConfigs, error) {
	agent.lockDevice.RLock()
	defer agent.lockDevice.RUnlock()
	log.Debugw("listPmConfigs", log.Fields{"id": agent.deviceID})

	return agent.getDeviceWithoutLock().PmConfigs, nil
}

func (agent *DeviceAgent) downloadImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("downloadImage", log.Fields{"id": agent.deviceID})

	device := agent.getDeviceWithoutLock()

	if device.AdminState != voltha.AdminState_ENABLED {
		log.Debugw("device-not-enabled", log.Fields{"id": agent.deviceID})
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, expected-admin-state:%s", agent.deviceID, voltha.AdminState_ENABLED)
	}
	// Save the image
	clonedImg := proto.Clone(img).(*voltha.ImageDownload)
	clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
	cloned := proto.Clone(device).(*voltha.Device)
	if cloned.ImageDownloads == nil {
		cloned.ImageDownloads = []*voltha.ImageDownload{clonedImg}
	} else {
		if device.AdminState != voltha.AdminState_ENABLED {
			log.Debugw("device-not-enabled", log.Fields{"id": agent.deviceID})
			return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, expected-admin-state:%s", agent.deviceID, voltha.AdminState_ENABLED)
		}
		// Save the image
		clonedImg := proto.Clone(img).(*voltha.ImageDownload)
		clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
		cloned := proto.Clone(device).(*voltha.Device)
		if cloned.ImageDownloads == nil {
			cloned.ImageDownloads = []*voltha.ImageDownload{clonedImg}
		} else {
			cloned.ImageDownloads = append(cloned.ImageDownloads, clonedImg)
		}
		cloned.AdminState = voltha.AdminState_DOWNLOADING_IMAGE
		if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
			return nil, err
		}
		// Send the request to the adapter
		if err := agent.adapterProxy.DownloadImage(ctx, cloned, clonedImg); err != nil {
			log.Debugw("downloadImage-error", log.Fields{"id": agent.deviceID, "error": err, "image": img.Name})
			return nil, err
		}
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

func (agent *DeviceAgent) cancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("cancelImageDownload", log.Fields{"id": agent.deviceID})

	device := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, device) {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	// Update image download state
	cloned := proto.Clone(device).(*voltha.Device)
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.DownloadState = voltha.ImageDownload_DOWNLOAD_CANCELLED
		}
	}

	if device.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		// Set the device to Enabled
		cloned.AdminState = voltha.AdminState_ENABLED
		if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
			return nil, err
		}
		// Send the request to the adapter
		if err := agent.adapterProxy.CancelImageDownload(ctx, device, img); err != nil {
			log.Debugw("cancelImageDownload-error", log.Fields{"id": agent.deviceID, "error": err, "image": img.Name})
			return nil, err
		}
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *DeviceAgent) activateImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("activateImage", log.Fields{"id": agent.deviceID})
	cloned := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, cloned) {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	if cloned.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, device-in-downloading-state:%s", agent.deviceID, img.Name)
	}
	// Update image download state
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.ImageState = voltha.ImageDownload_IMAGE_ACTIVATING
		}
	}
	// Set the device to downloading_image
	cloned.AdminState = voltha.AdminState_DOWNLOADING_IMAGE
	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return nil, err
	}

	if err := agent.adapterProxy.ActivateImageUpdate(ctx, proto.Clone(cloned).(*voltha.Device), img); err != nil {
		log.Debugw("activateImage-error", log.Fields{"id": agent.deviceID, "error": err, "image": img.Name})
		return nil, err
	}
	// The status of the AdminState will be changed following the update_download_status response from the adapter
	// The image name will also be removed from the device list
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *DeviceAgent) revertImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("revertImage", log.Fields{"id": agent.deviceID})

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

	if err := agent.adapterProxy.RevertImageUpdate(ctx, proto.Clone(cloned).(*voltha.Device), img); err != nil {
		log.Debugw("revertImage-error", log.Fields{"id": agent.deviceID, "error": err, "image": img.Name})
		return nil, err
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *DeviceAgent) getImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("getImageDownloadStatus", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	resp, err := agent.adapterProxy.GetImageDownloadStatus(ctx, cloned, img)
	if err != nil {
		log.Debugw("getImageDownloadStatus-error", log.Fields{"id": agent.deviceID, "error": err, "image": img.Name})
		return nil, err
	}
	return resp, nil
}

func (agent *DeviceAgent) updateImageDownload(ctx context.Context, img *voltha.ImageDownload) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("updateImageDownload", log.Fields{"id": agent.deviceID})

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
		cloned.AdminState = voltha.AdminState_ENABLED
	}

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return err
	}
	return nil
}

func (agent *DeviceAgent) getImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	agent.lockDevice.RLock()
	defer agent.lockDevice.RUnlock()
	log.Debugw("getImageDownload", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return image, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "image-not-found:%s", img.Name)
}

func (agent *DeviceAgent) listImageDownloads(ctx context.Context, deviceID string) (*voltha.ImageDownloads, error) {
	agent.lockDevice.RLock()
	defer agent.lockDevice.RUnlock()
	log.Debugw("listImageDownloads", log.Fields{"id": agent.deviceID})

	return &voltha.ImageDownloads{Items: agent.getDeviceWithoutLock().ImageDownloads}, nil
}

// getPorts retrieves the ports information of the device based on the port type.
func (agent *DeviceAgent) getPorts(ctx context.Context, portType voltha.Port_PortType) *voltha.Ports {
	log.Debugw("getPorts", log.Fields{"id": agent.deviceID, "portType": portType})
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

// getSwitchCapability is a helper method that a logical device agent uses to retrieve the switch capability of a
// parent device
func (agent *DeviceAgent) getSwitchCapability(ctx context.Context) (*ic.SwitchCapability, error) {
	log.Debugw("getSwitchCapability", log.Fields{"deviceId": agent.deviceID})
	device, err := agent.deviceMgr.GetDevice(ctx, agent.deviceID)
	if device == nil {
		return nil, err
	}
	var switchCap *ic.SwitchCapability
	if switchCap, err = agent.adapterProxy.GetOfpDeviceInfo(ctx, device); err != nil {
		log.Debugw("getSwitchCapability-error", log.Fields{"id": device.Id, "error": err})
		return nil, err
	}
	return switchCap, nil
}

// getPortCapability is a helper method that a logical device agent uses to retrieve the port capability of a
// device
func (agent *DeviceAgent) getPortCapability(ctx context.Context, portNo uint32) (*ic.PortCapability, error) {
	log.Debugw("getPortCapability", log.Fields{"deviceId": agent.deviceID})
	device, err := agent.deviceMgr.GetDevice(ctx, agent.deviceID)
	if device == nil {
		return nil, err
	}
	var portCap *ic.PortCapability
	if portCap, err = agent.adapterProxy.GetOfpPortInfo(ctx, device, portNo); err != nil {
		log.Debugw("getPortCapability-error", log.Fields{"id": device.Id, "error": err})
		return nil, err
	}
	return portCap, nil
}

func (agent *DeviceAgent) packetOut(ctx context.Context, outPort uint32, packet *ofp.OfpPacketOut) error {
	// If deviceType=="" then we must have taken ownership of this device.
	// Fixes VOL-2226 where a core would take ownership and have stale data
	if agent.deviceType == "" {
		agent.reconcileWithKVStore(ctx)
	}
	//	Send packet to adapter
	if err := agent.adapterProxy.packetOut(ctx, agent.deviceType, agent.deviceID, outPort, packet); err != nil {
		log.Debugw("packet-out-error", log.Fields{
			"id":     agent.deviceID,
			"error":  err,
			"packet": hex.EncodeToString(packet.Data),
		})
		return err
	}
	return nil
}

// processUpdate is a callback invoked whenever there is a change on the device manages by this device agent
func (agent *DeviceAgent) processUpdate(ctx context.Context, args ...interface{}) interface{} {
	//// Run this callback in its own go routine
	go func(args ...interface{}) interface{} {
		var previous *voltha.Device
		var current *voltha.Device
		var ok bool
		if len(args) == 2 {
			if previous, ok = args[0].(*voltha.Device); !ok {
				log.Errorw("invalid-callback-type", log.Fields{"data": args[0]})
				return nil
			}
			if current, ok = args[1].(*voltha.Device); !ok {
				log.Errorw("invalid-callback-type", log.Fields{"data": args[1]})
				return nil
			}
		} else {
			log.Errorw("too-many-args-in-callback", log.Fields{"len": len(args)})
			return nil
		}
		// Perform the state transition in it's own go routine (since the caller doesn't wait for this, use a background context)
		if err := agent.deviceMgr.processTransition(context.Background(), previous, current); err != nil {
			log.Errorw("failed-process-transition", log.Fields{"deviceId": previous.Id,
				"previousAdminState": previous.AdminState, "currentAdminState": current.AdminState})
		}
		return nil
	}(args...)

	return nil
}

// updatePartialDeviceData updates a subset of a device that an Adapter can update.
// TODO:  May need a specific proto to handle only a subset of a device that can be changed by an adapter
func (agent *DeviceAgent) mergeDeviceInfoFromAdapter(device *voltha.Device) (*voltha.Device, error) {
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
func (agent *DeviceAgent) updateDeviceUsingAdapterData(ctx context.Context, device *voltha.Device) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("updateDeviceUsingAdapterData", log.Fields{"deviceId": device.Id})
	updatedDevice, err := agent.mergeDeviceInfoFromAdapter(device)
	if err != nil {
		log.Errorw("failed to update device ", log.Fields{"deviceId": device.Id})
		return status.Errorf(codes.Internal, "%s", err.Error())
	}
	cloned := proto.Clone(updatedDevice).(*voltha.Device)
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) updateDeviceWithoutLock(ctx context.Context, device *voltha.Device) error {
	log.Debugw("updateDevice", log.Fields{"deviceId": device.Id})
	cloned := proto.Clone(device).(*voltha.Device)
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) updateDeviceStatus(ctx context.Context, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	cloned := agent.getDeviceWithoutLock()

	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if s, ok := voltha.ConnectStatus_Types_value[connStatus.String()]; ok {
		log.Debugw("updateDeviceStatus-conn", log.Fields{"ok": ok, "val": s})
		cloned.ConnectStatus = connStatus
	}
	if s, ok := voltha.OperStatus_Types_value[operStatus.String()]; ok {
		log.Debugw("updateDeviceStatus-oper", log.Fields{"ok": ok, "val": s})
		cloned.OperStatus = operStatus
	}
	log.Debugw("updateDeviceStatus", log.Fields{"deviceId": cloned.Id, "operStatus": cloned.OperStatus, "connectStatus": cloned.ConnectStatus})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) updatePortsOperState(ctx context.Context, operStatus voltha.OperStatus_Types) error {
	log.Debugw("updatePortsOperState", log.Fields{"device-id": agent.deviceID})
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	cloned := agent.getDeviceWithoutLock()
	for _, port := range cloned.Ports {
		port.OperStatus = operStatus
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) updatePortState(ctx context.Context, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_Types) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
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
	log.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) deleteAllPorts(ctx context.Context) error {
	log.Debugw("deleteAllPorts", log.Fields{"deviceId": agent.deviceID})
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	cloned := agent.getDeviceWithoutLock()

	if cloned.AdminState != voltha.AdminState_DISABLED && cloned.AdminState != voltha.AdminState_DELETED {
		err := status.Error(codes.FailedPrecondition, fmt.Sprintf("invalid-state-%v", cloned.AdminState))
		log.Warnw("invalid-state-removing-ports", log.Fields{"state": cloned.AdminState, "error": err})
		return err
	}
	if len(cloned.Ports) == 0 {
		log.Debugw("no-ports-present", log.Fields{"deviceId": agent.deviceID})
		return nil
	}

	cloned.Ports = []*voltha.Port{}
	log.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) addPort(ctx context.Context, port *voltha.Port) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("addPort", log.Fields{"deviceId": agent.deviceID, "port": port})

	cloned := agent.getDeviceWithoutLock()
	updatePort := false
	if cloned.Ports == nil {
		//	First port
		log.Debugw("addPort-first-port-to-add", log.Fields{"deviceId": agent.deviceID})
		cloned.Ports = make([]*voltha.Port, 0)
	} else {
		for _, p := range cloned.Ports {
			if p.Type == port.Type && p.PortNo == port.PortNo {
				if p.Label == "" && p.Type == voltha.Port_PON_OLT {
					//Creation of OLT PON port is being processed after a default PON port was created.  Just update it.
					log.Infow("update-pon-port-created-by-default", log.Fields{"default-port": p, "port-to-add": port})
					p.Label = port.Label
					p.OperStatus = port.OperStatus
					updatePort = true
					break
				}
				log.Debugw("port already exists", log.Fields{"port": port})
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

func (agent *DeviceAgent) addPeerPort(ctx context.Context, peerPort *voltha.Port_PeerPort) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("adding-peer-peerPort", log.Fields{"device-id": agent.deviceID, "peer-peerPort": peerPort})

	cloned := agent.getDeviceWithoutLock()

	// Get the peer port on the device based on the peerPort no
	found := false
	for _, port := range cloned.Ports {
		if port.PortNo == peerPort.PortNo { // found peerPort
			cp := proto.Clone(peerPort).(*voltha.Port_PeerPort)
			port.Peers = append(port.Peers, cp)
			log.Debugw("found-peer", log.Fields{"device-id": agent.deviceID, "portNo": peerPort.PortNo, "deviceId": agent.deviceID})
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
		log.Infow("adding-default-pon-port", log.Fields{"device-id": agent.deviceID, "peer": peerPort, "pon-port": ponPort})
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) deletePeerPorts(ctx context.Context, deviceID string) error {
	log.Debug("deletePeerPorts")

	cloned := agent.getDeviceWithoutLock()

	var updatedPeers []*voltha.Port_PeerPort
	for _, port := range cloned.Ports {
		updatedPeers = make([]*voltha.Port_PeerPort, 0)
		for _, peerPort := range port.Peers {
			if peerPort.DeviceId != deviceID {
				updatedPeers = append(updatedPeers, peerPort)
			}
		}
		port.Peers = updatedPeers
	}

	// Store the device with updated peer ports
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

// TODO: A generic device update by attribute
func (agent *DeviceAgent) updateDeviceAttribute(ctx context.Context, name string, value interface{}) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
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
	log.Debugw("update-field-status", log.Fields{"deviceId": cloned.Id, "name": name, "updated": updated})
	//	Save the data

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		log.Warnw("attribute-update-failed", log.Fields{"attribute": name, "value": value})
	}
}

func (agent *DeviceAgent) simulateAlarm(ctx context.Context, simulatereq *voltha.SimulateAlarmRequest) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("simulateAlarm", log.Fields{"id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	// First send the request to an Adapter and wait for a response
	if err := agent.adapterProxy.SimulateAlarm(ctx, cloned, simulatereq); err != nil {
		log.Debugw("simulateAlarm-error", log.Fields{"id": agent.deviceID, "error": err})
		return err
	}
	return nil
}

//This is an update operation to model without Lock.This function must never be invoked by another function unless the latter holds a lock on the device.
// It is an internal helper function.
func (agent *DeviceAgent) updateDeviceInStoreWithoutLock(ctx context.Context, device *voltha.Device, strict bool, txid string) error {
	updateCtx := context.WithValue(ctx, model.RequestTimestamp, time.Now().UnixNano())
	afterUpdate, err := agent.clusterDataProxy.Update(updateCtx, "/devices/"+agent.deviceID, device, strict, txid)
	if err != nil {
		return status.Errorf(codes.Internal, "failed-update-device:%s", agent.deviceID)
	}
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-update-device:%s", agent.deviceID)
	}
	log.Debugw("updated-device-in-store", log.Fields{"deviceId: ": agent.deviceID})

	agent.device = proto.Clone(device).(*voltha.Device)

	return nil
}

func (agent *DeviceAgent) updateDeviceReason(ctx context.Context, reason string) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	cloned := agent.getDeviceWithoutLock()
	cloned.Reason = reason
	log.Debugw("updateDeviceReason", log.Fields{"deviceId": cloned.Id, "reason": cloned.Reason})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *DeviceAgent) disablePort(ctx context.Context, Port *voltha.Port) error {
	var cp *voltha.Port
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("disablePort", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo})
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
		log.Debugw("updateDeviceInStoreWithoutLock error ", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}
	//send request to adapter
	if err := agent.adapterProxy.disablePort(ctx, device, cp); err != nil {
		log.Debugw("DisablePort-error", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}
	return nil
}

func (agent *DeviceAgent) enablePort(ctx context.Context, Port *voltha.Port) error {
	var cp *voltha.Port
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("enablePort", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo})
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
		log.Debugw("updateDeviceInStoreWithoutLock error ", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}
	//send request to adapter
	if err := agent.adapterProxy.enablePort(ctx, device, cp); err != nil {
		log.Debugw("EnablePort-error", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}
	return nil
}

func (agent *DeviceAgent) ChildDeviceLost(ctx context.Context, device *voltha.Device) error {

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("ChildDeviceLost", log.Fields{"id": device.Id})

	//Remove the associated peer ports on the parent device
	if err := agent.deviceMgr.deletePeerPorts(ctx, device.ParentId, device.Id); err != nil {
		return err
	}

	if err := agent.adapterProxy.ChildDeviceLost(ctx, agent.deviceType, agent.deviceID, device.ParentPortNo, device.ProxyAddress.OnuId); err != nil {
		log.Warnw("ChildDeviceLost-error", log.Fields{"error": err})
	}
	return nil

}
