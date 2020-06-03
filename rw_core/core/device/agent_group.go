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
	"strconv"

	"github.com/gogo/protobuf/proto"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listDeviceGroups returns logical device flow groups
func (agent *Agent) listDeviceGroups() map[uint32]*ofp.OfpGroupEntry {
	groupIDs := agent.groupLoader.List()
	groups := make(map[uint32]*ofp.OfpGroupEntry, len(groupIDs))
	for groupID := range groupIDs {
		if groupHandle, have := agent.groupLoader.Lock(groupID); have {
			groups[groupID] = groupHandle.GetReadOnly()
			groupHandle.Unlock()
		}
	}
	return groups
}

func (agent *Agent) addGroupsToAdapter(ctx context.Context, newGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw("add-groups-to-adapters", log.Fields{"device-id": agent.deviceID, "groups": newGroups, "flow-metadata": flowMetadata})

	if (len(newGroups)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	device := agent.getDeviceWithoutLock()
	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}
	updatedAllGroups := make([]*ofp.OfpGroupEntry, 0)
	if !dType.AcceptsAddRemoveFlowUpdates {
		groupIDs := agent.groupLoader.List()
		for groupID := range groupIDs {
			if grpHandle, have := agent.groupLoader.Lock(groupID); have {
				updatedAllGroups = append(updatedAllGroups, grpHandle.GetReadOnly())
				grpHandle.Unlock()
			}
		}
	}

	groupsToAdd := make([]*ofp.OfpGroupEntry, 0)
	groupsToDelete := make([]*ofp.OfpGroupEntry, 0)
	for _, group := range newGroups {

		groupHandle, created, err := agent.groupLoader.LockOrCreate(ctx, group)
		if err != nil {
			return coreutils.DoneResponse(), err
		}

		if created {
			groupsToAdd = append(groupsToAdd, group)
			updatedAllGroups = append(updatedAllGroups, group)
		} else {
			groupToChange := groupHandle.GetReadOnly()
			if !proto.Equal(groupToChange, group) {
				//Group needs to be updated.
				if err := groupHandle.Update(ctx, group); err != nil {
					groupHandle.Unlock()
					return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-group-%s-to-device-%s", strconv.Itoa(int(group.Desc.GroupId)), agent.deviceID)
				}
				groupsToDelete = append(groupsToDelete, groupToChange)
				groupsToAdd = append(groupsToAdd, group)
				updatedAllGroups = replaceGroupInList(updatedAllGroups, groupToChange, group)
			} else {
				//No need to change the group. It is already exist.
				logger.Debugw("No-need-to-change-already-existing-group", log.Fields{"device-id": agent.deviceID, "group": newGroups, "flow-metadata": flowMetadata})
			}
		}

		groupHandle.Unlock()
	}
	// Sanity check
	if (len(groupsToAdd)) == 0 {
		logger.Debugw("no-groups-to-update", log.Fields{"device-id": agent.deviceID, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	// Send update to adapters
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	response := coreutils.NewResponse()
	if !dType.AcceptsAddRemoveFlowUpdates {
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, &voltha.Flows{Items: []*ofp.OfpFlowStats{}}, &voltha.FlowGroups{Items: updatedAllGroups}, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &voltha.FlowGroups{Items: groupsToAdd},
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

//replaceGroupInList removes the old group from list and adds the new one.
func replaceGroupInList(groupList []*ofp.OfpGroupEntry, oldGroup *ofp.OfpGroupEntry, newGroup *ofp.OfpGroupEntry) []*ofp.OfpGroupEntry {
	if idx := fu.FindGroup(groupList, oldGroup.Desc.GroupId); idx != -1 {
		groupList = deleteGroupWithoutPreservingOrder(groupList, idx)
	}
	groupList = append(groupList, newGroup)
	return groupList
}

//deleteGroupWithoutPreservingOrder removes a group specified by index from the groups slice.  This function will
//panic if the index is out of range.
func deleteGroupWithoutPreservingOrder(groups []*ofp.OfpGroupEntry, index int) []*ofp.OfpGroupEntry {
	groups[index] = groups[len(groups)-1]
	groups[len(groups)-1] = nil
	return groups[:len(groups)-1]
}

func (agent *Agent) deleteGroupsFromAdapter(ctx context.Context, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw("delete-groups-from-adapter", log.Fields{"device-id": agent.deviceID, "groups": groupsToDel})

	if (len(groupsToDel)) == 0 {
		logger.Debugw("nothing-to-delete", log.Fields{"device-id": agent.deviceID})
		return coreutils.DoneResponse(), nil
	}
	device := agent.getDeviceWithoutLock()
	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}
	updatedAllGroups := make([]*ofp.OfpGroupEntry, 0)
	if !dType.AcceptsAddRemoveFlowUpdates {
		groupIDs := agent.groupLoader.List()
		for groupID := range groupIDs {
			if grpHandle, have := agent.groupLoader.Lock(groupID); have {
				updatedAllGroups = append(updatedAllGroups, grpHandle.GetReadOnly())
				grpHandle.Unlock()
			}
		}
	}

	for _, group := range groupsToDel {
		if groupHandle, have := agent.groupLoader.Lock(group.Desc.GroupId); have {
			// Update the store and cache
			if err := groupHandle.Delete(ctx); err != nil {
				groupHandle.Unlock()
				return coreutils.DoneResponse(), err
			}
			if idx := fu.FindGroup(updatedAllGroups, group.Desc.GroupId); idx != -1 {
				updatedAllGroups = deleteGroupWithoutPreservingOrder(updatedAllGroups, idx)
			}
			groupHandle.Unlock()
		}
	}

	// Send update to adapters
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	response := coreutils.NewResponse()
	if !dType.AcceptsAddRemoveFlowUpdates {
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, &voltha.Flows{Items: []*ofp.OfpFlowStats{}}, &voltha.FlowGroups{Items: updatedAllGroups}, flowMetadata)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
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

func (agent *Agent) updateGroupsToAdapter(ctx context.Context, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw("updateGroupsToAdapter", log.Fields{"device-id": agent.deviceID, "groups": updatedGroups})

	if (len(updatedGroups)) == 0 {
		logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "groups": updatedGroups})
		return coreutils.DoneResponse(), nil
	}

	device := agent.getDeviceWithoutLock()
	if device.OperStatus != voltha.OperStatus_ACTIVE || device.ConnectStatus != voltha.ConnectStatus_REACHABLE || device.AdminState != voltha.AdminState_ENABLED {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "invalid device states-oper-%s-connect-%s-admin-%s", device.OperStatus, device.ConnectStatus, device.AdminState)
	}
	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}
	updatedAllGroups := make([]*ofp.OfpGroupEntry, 0)
	if !dType.AcceptsAddRemoveFlowUpdates {
		groupIDs := agent.groupLoader.List()
		for groupID := range groupIDs {
			if grpHandle, have := agent.groupLoader.Lock(groupID); have {
				updatedAllGroups = append(updatedAllGroups, grpHandle.GetReadOnly())
				grpHandle.Unlock()
			}
		}
	}
	groupsToUpdate := make([]*ofp.OfpGroupEntry, 0)

	for _, group := range updatedGroups {
		if groupHandle, have := agent.groupLoader.Lock(group.Desc.GroupId); have {
			// Update the store and cache
			if err := groupHandle.Update(ctx, group); err != nil {
				groupHandle.Unlock()
				return coreutils.DoneResponse(), err
			}
			groupsToUpdate = append(groupsToUpdate, group)
			updatedAllGroups = replaceGroupInList(updatedAllGroups, groupHandle.GetReadOnly(), group)
			groupHandle.Unlock()
		}
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	response := coreutils.NewResponse()
	// Process bulk flow update differently than incremental update
	if !dType.AcceptsAddRemoveFlowUpdates {
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, &voltha.Flows{Items: []*ofp.OfpFlowStats{}}, &voltha.FlowGroups{Items: updatedAllGroups}, nil)
		if err != nil {
			cancel()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, rpcResponse, response)
	} else {
		logger.Debugw("updating-groups",
			log.Fields{
				"device-id":        agent.deviceID,
				"groups-to-update": groupsToUpdate,
			})

		// Sanity check
		if (len(groupsToUpdate)) == 0 {
			logger.Debugw("nothing-to-update", log.Fields{"device-id": agent.deviceID, "groups": groupsToUpdate})
			cancel()
			return coreutils.DoneResponse(), nil
		}

		flowChanges := &ofp.FlowChanges{
			ToAdd:    &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &voltha.Flows{Items: []*ofp.OfpFlowStats{}},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &voltha.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &voltha.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToUpdate: &voltha.FlowGroups{Items: groupsToUpdate},
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
