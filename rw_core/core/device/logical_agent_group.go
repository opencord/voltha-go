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

	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v4/pkg/flows"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listLogicalDeviceGroups returns logical device flow groups
func (agent *LogicalAgent) listLogicalDeviceGroups() map[uint32]*ofp.OfpGroupEntry {
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

//updateGroupTable updates the group table of that logical device
func (agent *LogicalAgent) updateGroupTable(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug(ctx, "updateGroupTable")
	if groupMod == nil {
		return nil
	}

	switch groupMod.GetCommand() {
	case ofp.OfpGroupModCommand_OFPGC_ADD:
		return agent.groupAdd(ctx, groupMod)
	case ofp.OfpGroupModCommand_OFPGC_DELETE:
		return agent.groupDelete(ctx, groupMod)
	case ofp.OfpGroupModCommand_OFPGC_MODIFY:
		return agent.groupModify(ctx, groupMod)
	}
	return status.Errorf(codes.Internal, "unhandled-command: logical-device-id:%s, command:%s", agent.logicalDeviceID, groupMod.GetCommand())
}

func (agent *LogicalAgent) groupAdd(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	if groupMod == nil {
		return nil
	}
	logger.Debugw(ctx, "groupAdd", log.Fields{"GroupId": groupMod.GroupId})

	groupEntry := fu.GroupEntryFromGroupMod(groupMod)

	groupHandle, created, err := agent.groupLoader.LockOrCreate(ctx, groupEntry)
	if err != nil {
		return err
	}
	groupHandle.Unlock()

	if !created {
		return fmt.Errorf("group %d already exists", groupMod.GroupId)
	}

	fg := fu.NewFlowsAndGroups()
	fg.AddGroup(groupEntry)
	deviceRules := fu.NewDeviceRules()
	deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)

	logger.Debugw(ctx, "rules", log.Fields{"rules for group-add": deviceRules.String()})

	// Update the devices
	respChnls := agent.addFlowsAndGroupsToDevices(ctx, deviceRules, &voltha.FlowMetadata{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw(ctx, "failure-updating-device-flows-groups", log.Fields{"logical-device-id": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}

func (agent *LogicalAgent) groupDelete(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debugw(ctx, "groupDelete", log.Fields{"groupMod": groupMod})
	if groupMod == nil {
		return nil
	}

	affectedFlows := make(map[uint64]*ofp.OfpFlowStats)
	affectedGroups := make(map[uint32]*ofp.OfpGroupEntry)

	toDelete := map[uint32]struct{}{groupMod.GroupId: {}}
	if groupMod.GroupId == uint32(ofp.OfpGroup_OFPG_ALL) {
		toDelete = agent.groupLoader.ListIDs()
	}

	for groupID := range toDelete {
		if groupHandle, have := agent.groupLoader.Lock(groupID); have {
			affectedGroups[groupID] = groupHandle.GetReadOnly()
			if err := groupHandle.Delete(ctx); err != nil {
				return err
			}
			groupHandle.Unlock()

			//TODO: this is another case where ordering guarantees are not being made,
			//      group deletion does not guarantee deletion of corresponding flows.
			//      an error while deleting flows can cause inconsistent state.
			flows, err := agent.deleteFlowsHavingGroup(ctx, groupID)
			if err != nil {
				logger.Errorw(ctx, "cannot-update-flow-for-group-delete", log.Fields{"logical-device-id": agent.logicalDeviceID, "groupID": groupID})
				return err
			}
			for flowID, flow := range flows {
				affectedFlows[flowID] = flow
			}
		}
	}

	if len(affectedGroups) == 0 {
		logger.Debugw(ctx, "no-group-to-delete", log.Fields{"groupId": groupMod.GroupId})
		return nil
	}

	var deviceRules *fu.DeviceRules
	var err error

	if len(affectedFlows) != 0 {
		deviceRules, err = agent.flowDecomposer.DecomposeRules(ctx, agent, affectedFlows, affectedGroups)
		if err != nil {
			return err
		}
	} else {
		//no flow is affected, just remove the groups
		deviceRules = fu.NewDeviceRules()
		deviceRules.CreateEntryIfNotExist(agent.rootDeviceID)
	}
	//add groups to deviceRules
	for _, groupEntry := range affectedGroups {
		fg := fu.NewFlowsAndGroups()
		fg.AddGroup(groupEntry)
		deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)
	}
	logger.Debugw(ctx, "rules", log.Fields{"rules": deviceRules.String()})

	// delete groups and related flows, if any
	respChnls := agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &voltha.FlowMetadata{}, &ofp.OfpFlowMod{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw(ctx, "failure-updating-device-flows-groups", log.Fields{"logical-device-id": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}

func (agent *LogicalAgent) groupModify(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug(ctx, "groupModify")
	if groupMod == nil {
		return nil
	}

	groupID := groupMod.GroupId

	groupHandle, have := agent.groupLoader.Lock(groupID)
	if !have {
		return fmt.Errorf("group-absent:%d", groupID)
	}
	defer groupHandle.Unlock()

	//replace existing group entry with new group definition
	groupEntry := fu.GroupEntryFromGroupMod(groupMod)
	deviceRules := fu.NewDeviceRules()
	deviceRules.CreateEntryIfNotExist(agent.rootDeviceID)
	fg := fu.NewFlowsAndGroups()
	fg.AddGroup(fu.GroupEntryFromGroupMod(groupMod))
	deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)

	logger.Debugw(ctx, "rules", log.Fields{"rules-for-group-modify": deviceRules.String()})

	//update KV
	if err := groupHandle.Update(ctx, groupEntry); err != nil {
		logger.Errorw(ctx, "Cannot-update-logical-group", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return err
	}

	// Update the devices
	respChnls := agent.updateFlowsAndGroupsOfDevice(ctx, deviceRules, &voltha.FlowMetadata{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw(ctx, "failure-updating-device-flows-groups", log.Fields{"logical-device-id": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}
