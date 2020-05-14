package device

import (
	"context"
	"fmt"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
)

//GroupChunk keeps a group entry and its lock. The lock in the struct is used to syncronize the
//modifications for the related group.
type GroupChunk struct {
	group *ofp.OfpGroupEntry
	lock  sync.Mutex
}

func (agent *LogicalAgent) loadGroups(ctx context.Context) {
	agent.groupLock.Lock()
	defer agent.groupLock.Unlock()

	var groups []*ofp.OfpGroupEntry
	if err := agent.clusterDataProxy.List(ctx, "groups/"+agent.logicalDeviceID, &groups); err != nil {
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
	logger.Infow("Groups-are-loaded-into-the-cache-from-store", log.Fields{"logicalDeviceID": agent.logicalDeviceID})
}

//updateLogicalDeviceFlowGroup updates the flow groups in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) updateLogicalDeviceFlowGroup(ctx context.Context, groupEntry *ofp.OfpGroupEntry, groupChunk *GroupChunk) error {
	path := fmt.Sprintf("groups/%s/%d", agent.logicalDeviceID, groupEntry.Desc.GroupId)
	if err := agent.clusterDataProxy.Update(ctx, path, groupEntry); err != nil {
		logger.Errorw("error-updating-logical-device-with-group", log.Fields{"error": err})
		return err
	}
	groupChunk.group = groupEntry
	return nil
}

//removeLogicalDeviceFlowGroup removes the flow groups in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) removeLogicalDeviceFlowGroup(ctx context.Context, groupID uint32) error {
	path := fmt.Sprintf("groups/%s/%d", agent.logicalDeviceID, groupID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-group-from-store-%s", path)
	}
	agent.groupLock.Lock()
	defer agent.groupLock.Unlock()
	delete(agent.groups, groupID)
	return nil
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (agent *LogicalAgent) ListLogicalDeviceFlowGroups(ctx context.Context) (*ofp.FlowGroups, error) {
	logger.Debug("ListLogicalDeviceFlowGroups")

	var groupEntries []*ofp.OfpGroupEntry
	agent.groupLock.RLock()
	defer agent.groupLock.RUnlock()
	for _, value := range agent.groups {
		groupEntries = append(groupEntries, (proto.Clone(value.group)).(*ofp.OfpGroupEntry))
	}
	return &ofp.FlowGroups{Items: groupEntries}, nil
}
