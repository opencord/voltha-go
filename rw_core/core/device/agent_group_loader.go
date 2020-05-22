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

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
)

const (
	groupRootPath = "groups/"
	groupFullPath = "groups/%s/%d"
)

func (agent *Agent) loadGroups(ctx context.Context) {
	agent.groupLock.Lock()
	defer agent.groupLock.Unlock()

	var groups []*ofp.OfpGroupEntry
	if err := agent.clusterDataProxy.List(ctx, groupRootPath+agent.deviceID, &groups); err != nil {
		logger.Errorw("Failed-to-list-groups-from-proxy", log.Fields{"error": err})
		return
	}
	for _, group := range groups {
		if group.Desc != nil {
			groupChunk := GroupChunk{
				group: group,
			}
			agent.groups[group.Desc.GroupId] = &groupChunk
		}
	}
	logger.Infow("Groups-are-loaded-into-the-cache-from-store", log.Fields{"deviceID": agent.deviceID})
}

//updateDeviceFlowGroup updates the flow groups in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *Agent) updateDeviceFlowGroup(ctx context.Context, groupEntry *ofp.OfpGroupEntry, groupChunk *GroupChunk) error {
	path := fmt.Sprintf(groupFullPath, agent.deviceID, groupEntry.Desc.GroupId)
	if err := agent.clusterDataProxy.Update(ctx, path, groupEntry); err != nil {
		logger.Errorw("error-updating-logical-device-with-group", log.Fields{"error": err})
		return err
	}
	groupChunk.group = groupEntry
	return nil
}

//removeDeviceFlowGroup removes the flow groups in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *Agent) removeDeviceFlowGroup(ctx context.Context, groupID uint32) error {
	path := fmt.Sprintf(groupFullPath, agent.deviceID, groupID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-group-from-store-%s", path)
	}
	agent.groupLock.Lock()
	defer agent.groupLock.Unlock()
	delete(agent.groups, groupID)
	return nil
}

// ListDeviceFlowGroups returns logical device flow groups
func (agent *Agent) ListDeviceFlowGroups(ctx context.Context) (*ofp.FlowGroups, error) {
	logger.Debug("ListDeviceFlowGroups")

	var groupEntries []*ofp.OfpGroupEntry
	agent.groupLock.RLock()
	defer agent.groupLock.RUnlock()
	for _, value := range agent.groups {
		groupEntries = append(groupEntries, (proto.Clone(value.group)).(*ofp.OfpGroupEntry))
	}
	return &ofp.FlowGroups{Items: groupEntries}, nil
}
