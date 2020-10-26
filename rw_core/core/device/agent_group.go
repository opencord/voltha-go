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
	"fmt"
	"strconv"

	"github.com/gogo/protobuf/proto"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/common"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listDeviceGroups returns logical device flow groups
func (agent *Agent) listDeviceGroups() map[uint32]*ofp.OfpGroupEntry {
	groupIDs := agent.groupLoader.ListIDs()
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
	logger.Debugw(ctx, "add-groups-to-adapters", log.Fields{"device-id": agent.deviceID, "groups": newGroups, "flow-metadata": flowMetadata})

	update := agent.newDeviceUpdate(ctx, "addGroupsToAdapter", "NB")

	if (len(newGroups)) == 0 {
		logger.Debugw(ctx, "nothing-to-update", log.Fields{"device-id": agent.deviceID, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		update.Description = err.Error()
		agent.deviceUpdate(ctx, update)
		return coreutils.DoneResponse(), status.Errorf(codes.Aborted, "%s", err)
	}
	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		update.Description = fmt.Sprintf("non-existent-device-type-%s", device.Type)
		agent.deviceUpdate(ctx, update)
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}

	groupsToAdd := make([]*ofp.OfpGroupEntry, 0)
	groupsToDelete := make([]*ofp.OfpGroupEntry, 0)
	for _, group := range newGroups {
		groupHandle, created, err := agent.groupLoader.LockOrCreate(ctx, group)
		if err != nil {
			update.Description = err.Error()
			agent.deviceUpdate(ctx, update)
			return coreutils.DoneResponse(), err
		}

		if created {
			groupsToAdd = append(groupsToAdd, group)
		} else {
			groupToChange := groupHandle.GetReadOnly()
			if !proto.Equal(groupToChange, group) {
				//Group needs to be updated.
				if err := groupHandle.Update(ctx, group); err != nil {
					groupHandle.Unlock()
					update.Description = fmt.Sprintf("failure-updating-group-%s-to-device-%s", strconv.Itoa(int(group.Desc.GroupId)), agent.deviceID)
					agent.deviceUpdate(ctx, update)
					return coreutils.DoneResponse(), status.Errorf(codes.Internal, "failure-updating-group-%s-to-device-%s", strconv.Itoa(int(group.Desc.GroupId)), agent.deviceID)
				}
				groupsToDelete = append(groupsToDelete, groupToChange)
				groupsToAdd = append(groupsToAdd, group)
			} else {
				//No need to change the group. It is already exist.
				logger.Debugw(ctx, "No-need-to-change-already-existing-group", log.Fields{"device-id": agent.deviceID, "group": newGroups, "flow-metadata": flowMetadata})
			}
		}

		groupHandle.Unlock()
	}
	// Sanity check
	if (len(groupsToAdd)) == 0 {
		logger.Debugw(ctx, "no-groups-to-update", log.Fields{"device-id": agent.deviceID, "groups": newGroups})
		return coreutils.DoneResponse(), nil
	}

	// Send update to adapters
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	response := coreutils.NewResponse()
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllGroups := agent.listDeviceGroups()
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, nil, updatedAllGroups, flowMetadata)
		if err != nil {
			cancel()
			update.Description = err.Error()
			agent.deviceUpdate(ctx, update)
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, "addGroupsToAdapter", rpcResponse, response)
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
			update.Description = err.Error()
			agent.deviceUpdate(ctx, update)
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, "addGroupsToAdapter", rpcResponse, response)
	}
	update.Status.Code = common.OperationResp_OPERATION_IN_PROGRESS
	agent.deviceUpdate(ctx, update)
	return response, nil
}

func (agent *Agent) deleteGroupsFromAdapter(ctx context.Context, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "delete-groups-from-adapter", log.Fields{"device-id": agent.deviceID, "groups": groupsToDel})

	if (len(groupsToDel)) == 0 {
		logger.Debugw(ctx, "nothing-to-delete", log.Fields{"device-id": agent.deviceID})
		return coreutils.DoneResponse(), nil
	}

	update := agent.newDeviceUpdate(ctx, "deleteGroupsFromAdapter", "NB")
	defer agent.deviceUpdate(ctx, update)

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		update.Description = err.Error()
		return coreutils.DoneResponse(), status.Errorf(codes.Aborted, "%s", err)
	}
	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		update.Description = fmt.Sprintf("non-existent-device-type-%s", device.Type)
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}

	for _, group := range groupsToDel {
		if groupHandle, have := agent.groupLoader.Lock(group.Desc.GroupId); have {
			// Update the store and cache
			if err := groupHandle.Delete(ctx); err != nil {
				groupHandle.Unlock()
				update.Description = err.Error()
				return coreutils.DoneResponse(), err
			}
			groupHandle.Unlock()
		}
	}

	// Send update to adapters
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	response := coreutils.NewResponse()
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllGroups := agent.listDeviceGroups()
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, nil, updatedAllGroups, flowMetadata)
		if err != nil {
			cancel()
			update.Description = err.Error()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, "deleteGroupsFromAdapter", rpcResponse, response)
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
			update.Description = err.Error()
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, "deleteGroupsFromAdapter", rpcResponse, response)
	}
	update.Status.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}

func (agent *Agent) updateGroupsToAdapter(ctx context.Context, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "updateGroupsToAdapter", log.Fields{"device-id": agent.deviceID, "groups": updatedGroups})

	update := agent.newDeviceUpdate(ctx, "updateGroupsToAdapter", "NB")

	if (len(updatedGroups)) == 0 {
		logger.Debugw(ctx, "nothing-to-update", log.Fields{"device-id": agent.deviceID, "groups": updatedGroups})
		return coreutils.DoneResponse(), nil
	}

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		update.Description = err.Error()
		update := agent.newDeviceUpdate(ctx, "deleteGroupsFromAdapter", "NB")
		agent.deviceUpdate(ctx, update)
		return coreutils.DoneResponse(), status.Errorf(codes.Aborted, "%s", err)
	}
	if device.OperStatus != voltha.OperStatus_ACTIVE || device.ConnectStatus != voltha.ConnectStatus_REACHABLE || device.AdminState != voltha.AdminState_ENABLED {
		update.Description = fmt.Sprintf("invalid device states-oper-%s-connect-%s-admin-%s", device.OperStatus, device.ConnectStatus, device.AdminState)
		agent.deviceUpdate(ctx, update)
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "invalid device states-oper-%s-connect-%s-admin-%s", device.OperStatus, device.ConnectStatus, device.AdminState)
	}
	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		update.Description = fmt.Sprintf("non-existent-device-type-%s", device.Type)
		agent.deviceUpdate(ctx, update)
		return coreutils.DoneResponse(), status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
	}

	groupsToUpdate := make([]*ofp.OfpGroupEntry, 0)
	for _, group := range updatedGroups {
		if groupHandle, have := agent.groupLoader.Lock(group.Desc.GroupId); have {
			// Update the store and cache
			if err := groupHandle.Update(ctx, group); err != nil {
				groupHandle.Unlock()
				update.Description = err.Error()
				agent.deviceUpdate(ctx, update)
				return coreutils.DoneResponse(), err
			}
			groupsToUpdate = append(groupsToUpdate, group)
			groupHandle.Unlock()
		}
	}

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	response := coreutils.NewResponse()
	// Process bulk flow update differently than incremental update
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllGroups := agent.listDeviceGroups()
		rpcResponse, err := agent.adapterProxy.UpdateFlowsBulk(subCtx, device, nil, updatedAllGroups, nil)
		if err != nil {
			cancel()
			update.Description = err.Error()
			agent.deviceUpdate(ctx, update)
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, "updateGroupsToAdapter", rpcResponse, response)
	} else {
		logger.Debugw(ctx, "updating-groups",
			log.Fields{
				"device-id":        agent.deviceID,
				"groups-to-update": groupsToUpdate,
			})

		// Sanity check
		if (len(groupsToUpdate)) == 0 {
			logger.Debugw(ctx, "nothing-to-update", log.Fields{"device-id": agent.deviceID, "groups": groupsToUpdate})
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
			update.Description = err.Error()
			agent.deviceUpdate(ctx, update)
			return coreutils.DoneResponse(), err
		}
		go agent.waitForAdapterFlowResponse(subCtx, cancel, "updateGroupsToAdapter", rpcResponse, response)
	}

	update.Status.Code = common.OperationResp_OPERATION_IN_PROGRESS
	agent.deviceUpdate(ctx, update)
	return response, nil
}
