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

	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//updateGroupTable updates the group table of that logical device
func (agent *LogicalAgent) updateGroupTable(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("updateGroupTable")
	if groupMod == nil {
		return nil
	}

	if err := agent.generateDeviceRoutesIfNeeded(ctx); err != nil {
		return err
	}

	switch groupMod.GetCommand() {
	case ofp.OfpGroupModCommand_OFPGC_ADD:
		return agent.groupAdd(ctx, groupMod)
	case ofp.OfpGroupModCommand_OFPGC_DELETE:
		return agent.groupDelete(ctx, groupMod)
	case ofp.OfpGroupModCommand_OFPGC_MODIFY:
		return agent.groupModify(ctx, groupMod)
	}
	return status.Errorf(codes.Internal, "unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, groupMod.GetCommand())
}

func (agent *LogicalAgent) groupAdd(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	if groupMod == nil {
		return nil
	}
	logger.Debugw("groupAdd", log.Fields{"GroupId": groupMod.GroupId})
	agent.groupLock.Lock()
	_, ok := agent.groups[groupMod.GroupId]
	if ok {
		agent.groupLock.Unlock()
		return fmt.Errorf("Group %d already exists", groupMod.GroupId)
	}

	groupEntry := fu.GroupEntryFromGroupMod(ctx, groupMod)
	groupChunk := GroupChunk{
		group: groupEntry,
	}
	//add to map
	agent.groups[groupMod.GroupId] = &groupChunk
	groupChunk.lock.Lock()
	defer groupChunk.lock.Unlock()
	agent.groupLock.Unlock()
	//add to the kv store
	path := fmt.Sprintf("groups/%s", agent.logicalDeviceID)
	groupID := strconv.Itoa(int(groupMod.GroupId))
	if err := agent.clusterDataProxy.AddWithID(ctx, path, groupID, groupEntry); err != nil {
		logger.Errorw("failed-adding-group", log.Fields{"deviceID": agent.logicalDeviceID, "groupID": groupID, "err": err})
		agent.groupLock.Lock()
		delete(agent.groups, groupMod.GroupId)
		agent.groupLock.Unlock()
		return err
	}
	deviceRules := fu.NewDeviceRules(ctx)
	deviceRules.CreateEntryIfNotExist(ctx, agent.rootDeviceID)
	fg := fu.NewFlowsAndGroups(ctx)
	fg.AddGroup(ctx, fu.GroupEntryFromGroupMod(ctx, groupMod))
	deviceRules.AddFlowsAndGroup(ctx, agent.rootDeviceID, fg)

	logger.Debugw("rules", log.Fields{"rules for group-add": deviceRules.String(ctx)})

	// Update the devices
	respChnls := agent.addFlowsAndGroupsToDevices(ctx, deviceRules, &voltha.FlowMetadata{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(ctx, agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}

func (agent *LogicalAgent) groupDelete(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupDelete")
	if groupMod == nil {
		return nil
	}
	affectedFlows := make([]*ofp.OfpFlowStats, 0)
	affectedGroups := make([]*ofp.OfpGroupEntry, 0)
	var groupsChanged bool
	groupID := groupMod.GroupId
	var err error
	if groupID == uint32(ofp.OfpGroup_OFPG_ALL) {
		if err := func() error {
			agent.groupLock.Lock()
			defer agent.groupLock.Unlock()
			for key, groupChunk := range agent.groups {
				//Remove from store and cache. Do this in a one time lock allocation.
				path := fmt.Sprintf("groups/%s/%d", agent.logicalDeviceID, key)
				if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
					return fmt.Errorf("couldnt-deleted-group-from-store-%s", path)
				}
				delete(agent.groups, groupID)
				var flows []*ofp.OfpFlowStats
				if flows, err = agent.deleteFlowsOfGroup(ctx, key); err != nil {
					logger.Errorw("cannot-update-flow-for-group-delete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "groupID": key})
					return err
				}
				affectedFlows = append(affectedFlows, flows...)
				affectedGroups = append(affectedGroups, groupChunk.group)
			}
			return nil
		}(); err != nil {
			return err
		}
		groupsChanged = true
	} else {
		agent.groupLock.RLock()
		groupChunk, ok := agent.groups[groupID]
		agent.groupLock.RUnlock()
		if !ok {
			logger.Warnw("group-not-found", log.Fields{"groupID": groupID})
			return nil
		}
		groupChunk.lock.Lock()
		defer groupChunk.lock.Unlock()
		var flows []*ofp.OfpFlowStats
		if flows, err = agent.deleteFlowsOfGroup(ctx, groupID); err != nil {
			logger.Errorw("cannot-update-flow-for-group-delete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "groupID": groupID})
			return err
		}
		//remove from store
		if err := agent.removeLogicalDeviceFlowGroup(ctx, groupID); err != nil {
			return err
		}
		affectedFlows = append(affectedFlows, flows...)
		affectedGroups = append(affectedGroups, groupChunk.group)
		groupsChanged = true

	}

	if err != nil || groupsChanged {
		var deviceRules *fu.DeviceRules
		deviceRules, err = agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: affectedFlows}, ofp.FlowGroups{Items: affectedGroups})
		if err != nil {
			return err
		}
		logger.Debugw("rules", log.Fields{"rules": deviceRules.String(ctx)})

		// Update the devices
		respChnls := agent.updateFlowsAndGroupsOfDevice(ctx, deviceRules, nil)

		// Wait for completion
		go func() {
			if res := coreutils.WaitForNilOrErrorResponses(ctx, agent.defaultTimeout, respChnls...); res != nil {
				logger.Warnw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
				//TODO: Revert flow changes
			}
		}()
	}
	return nil
}

func (agent *LogicalAgent) groupModify(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupModify")
	if groupMod == nil {
		return nil
	}

	groupID := groupMod.GroupId
	agent.groupLock.RLock()
	groupChunk, ok := agent.groups[groupID]
	agent.groupLock.RUnlock()
	if !ok {
		return fmt.Errorf("group-absent:%d", groupID)
	}
	//Don't let any other thread to make modifications to this group till all done here.
	groupChunk.lock.Lock()
	defer groupChunk.lock.Unlock()
	//replace existing group entry with new group definition
	groupEntry := fu.GroupEntryFromGroupMod(ctx, groupMod)
	deviceRules := fu.NewDeviceRules(ctx)
	deviceRules.CreateEntryIfNotExist(ctx, agent.rootDeviceID)
	fg := fu.NewFlowsAndGroups(ctx)
	fg.AddGroup(ctx, fu.GroupEntryFromGroupMod(ctx, groupMod))
	deviceRules.AddFlowsAndGroup(ctx, agent.rootDeviceID, fg)

	logger.Debugw("rules", log.Fields{"rules-for-group-modify": deviceRules.String(ctx)})
	//update KV
	if err := agent.updateLogicalDeviceFlowGroup(ctx, groupEntry, groupChunk); err != nil {
		logger.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return err
	}

	// Update the devices
	respChnls := agent.updateFlowsAndGroupsOfDevice(ctx, deviceRules, &voltha.FlowMetadata{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(ctx, agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}
