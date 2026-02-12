/*
 * Copyright 2018-2024 Open Networking Foundation (ONF) and the ONF Contributors

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

	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// listDeviceGroups returns logical device flow groups
func (agent *Agent) listDeviceGroups() map[uint32]*ofp.OfpGroupEntry {
	groupIDs := agent.groupCache.ListIDs()
	groups := make(map[uint32]*ofp.OfpGroupEntry, len(groupIDs))
	for groupID := range groupIDs {
		if groupHandle, have := agent.groupCache.Lock(groupID); have {
			groups[groupID] = groupHandle.GetReadOnly()
			groupHandle.Unlock()
		}
	}
	return groups
}

func (agent *Agent) addGroupsToAdapter(ctx context.Context, newGroups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "add-groups-to-adapters", log.Fields{"device-id": agent.deviceID, "groups": newGroups, "flow-metadata": flowMetadata})

	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if (len(newGroups)) == 0 {
		desc = "no new groups"
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return coreutils.DoneResponse(), nil
	}

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	if !agent.proceedWithRequest(device) {
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return coreutils.DoneResponse(), err
	}

	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	groupsToAdd := make([]*ofp.OfpGroupEntry, 0)
	groupsToDelete := make([]*ofp.OfpGroupEntry, 0)
	for _, group := range newGroups {
		groupHandle, created, err := agent.groupCache.LockOrCreate(ctx, group)
		if err != nil {
			return coreutils.DoneResponse(), err
		}

		if created {
			groupsToAdd = append(groupsToAdd, group)
		} else {
			groupToChange := groupHandle.GetReadOnly()
			if !proto.Equal(groupToChange, group) {
				// Group needs to be updated.
				if err = groupHandle.Update(ctx, group); err != nil {
					groupHandle.Unlock()
					return coreutils.DoneResponse(), err
				}
				groupsToDelete = append(groupsToDelete, groupToChange)
				groupsToAdd = append(groupsToAdd, group)
			} else {
				// No need to change the group. It is already exist.
				logger.Debugw(ctx, "no-need-to-change-already-existing-group", log.Fields{"device-id": agent.deviceID, "group": newGroups, "flow-metadata": flowMetadata})
			}
		}

		groupHandle.Unlock()
	}
	// Sanity check
	if (len(groupsToAdd)) == 0 {
		desc = "no group to update"
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return coreutils.DoneResponse(), nil
	}

	// Send update to adapters
	response := coreutils.NewResponse()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllGroups := agent.listDeviceGroups()
		ctr, groupSlice := 0, make([]*ofp.OfpGroupEntry, len(updatedAllGroups))
		for _, group := range updatedAllGroups {
			groupSlice[ctr] = group
			ctr++
		}
		go agent.sendBulkFlows(subCtx, device, nil, &ofp.FlowGroups{Items: groupSlice}, flowMetadata, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &ofp.FlowGroups{Items: groupsToAdd},
			ToRemove: &ofp.FlowGroups{Items: groupsToDelete},
			ToUpdate: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		go agent.sendIncrementalFlows(subCtx, device, flowChanges, groupChanges, flowMetadata, response)
	}
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}

func (agent *Agent) deleteGroupsFromAdapter(ctx context.Context, groupsToDel []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "delete-groups-from-adapter", log.Fields{"device-id": agent.deviceID, "groups": groupsToDel})

	var desc string
	var err error
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if (len(groupsToDel)) == 0 {
		desc = "nothing to delete"
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return coreutils.DoneResponse(), nil
	}

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	if !agent.proceedWithRequest(device) {
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return coreutils.DoneResponse(), err
	}

	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	for _, group := range groupsToDel {
		if groupHandle, have := agent.groupCache.Lock(group.Desc.GroupId); have {
			// Update the store and cache
			if err = groupHandle.Delete(ctx); err != nil {
				groupHandle.Unlock()
				return coreutils.DoneResponse(), err
			}
			groupHandle.Unlock()
		}
	}

	// Send update to adapters
	response := coreutils.NewResponse()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllGroups := agent.listDeviceGroups()
		ctr, groupSlice := 0, make([]*ofp.OfpGroupEntry, len(updatedAllGroups))
		for _, group := range updatedAllGroups {
			groupSlice[ctr] = group
			ctr++
		}
		go agent.sendBulkFlows(subCtx, device, nil, &ofp.FlowGroups{Items: groupSlice}, flowMetadata, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &ofp.FlowGroups{Items: groupsToDel},
			ToUpdate: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		go agent.sendIncrementalFlows(subCtx, device, flowChanges, groupChanges, flowMetadata, response)
	}
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}

func (agent *Agent) updateGroupsToAdapter(ctx context.Context, updatedGroups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "update-groups-to-adapter", log.Fields{"device-id": agent.deviceID, "groups": updatedGroups})

	var desc string
	var err error
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if (len(updatedGroups)) == 0 {
		desc = "no groups to update"
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return coreutils.DoneResponse(), nil
	}

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	if !agent.proceedWithRequest(device) {
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return coreutils.DoneResponse(), err
	}

	if device.OperStatus != voltha.OperStatus_ACTIVE || device.ConnectStatus != voltha.ConnectStatus_REACHABLE || device.AdminState != voltha.AdminState_ENABLED {
		err = status.Errorf(codes.FailedPrecondition, "invalid device states-oper-%s-connect-%s-admin-%s", device.OperStatus, device.ConnectStatus, device.AdminState)
		return coreutils.DoneResponse(), err
	}

	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		err = status.Errorf(codes.FailedPrecondition, "non-existent-device-type-%s", device.Type)
		return coreutils.DoneResponse(), err
	}

	groupsToUpdate := make([]*ofp.OfpGroupEntry, 0)
	for _, group := range updatedGroups {
		if groupHandle, have := agent.groupCache.Lock(group.Desc.GroupId); have {
			// Update the store and cache
			if err = groupHandle.Update(ctx, group); err != nil {
				groupHandle.Unlock()
				return coreutils.DoneResponse(), err
			}
			groupsToUpdate = append(groupsToUpdate, group)
			groupHandle.Unlock()
		}
	}

	response := coreutils.NewResponse()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
	// Process bulk flow update differently than incremental update
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllGroups := agent.listDeviceGroups()
		ctr, groupSlice := 0, make([]*ofp.OfpGroupEntry, len(updatedAllGroups))
		for _, group := range updatedAllGroups {
			groupSlice[ctr] = group
			ctr++
		}
		go agent.sendBulkFlows(subCtx, device, nil, &ofp.FlowGroups{Items: groupSlice}, flowMetadata, response)
	} else {
		logger.Debugw(ctx, "updating-groups",
			log.Fields{
				"device-id":        agent.deviceID,
				"groups-to-update": groupsToUpdate,
			})

		// Sanity check
		if (len(groupsToUpdate)) == 0 {
			desc = "nothing to update"
			operStatus.Code = common.OperationResp_OPERATION_SUCCESS
			return coreutils.DoneResponse(), nil
		}

		flowChanges := &ofp.FlowChanges{
			ToAdd:    &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToUpdate: &ofp.FlowGroups{Items: groupsToUpdate},
		}
		go agent.sendIncrementalFlows(subCtx, device, flowChanges, groupChanges, flowMetadata, response)
	}

	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}
