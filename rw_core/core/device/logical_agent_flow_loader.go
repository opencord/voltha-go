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
	"sync"

	"github.com/gogo/protobuf/proto"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//FlowChunk keeps a flow and the lock for this flow. The lock in the struct is used to syncronize the
//modifications for the related flow.
type FlowChunk struct {
	flow *ofp.OfpFlowStats
	lock sync.Mutex
}

func (agent *LogicalAgent) loadFlows(ctx context.Context) {
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()

	var flowList []*ofp.OfpFlowStats
	if err := agent.clusterDataProxy.List(ctx, "logical_flows/"+agent.logicalDeviceID, &flowList); err != nil {
		logger.Errorw("Failed-to-list-logicalflows-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, flow := range flowList {
		if flow != nil {
			flowsChunk := FlowChunk{
				flow: flow,
			}
			agent.flows[flow.Id] = &flowsChunk
		}
	}
}

//updateLogicalDeviceFlow updates flow in the store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) updateLogicalDeviceFlow(ctx context.Context, flow *ofp.OfpFlowStats, flowChunk *FlowChunk) error {
	path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flow.Id)
	if err := agent.clusterDataProxy.Update(ctx, path, flow); err != nil {
		return status.Errorf(codes.Internal, "failed-update-flow:%s:%d %s", agent.logicalDeviceID, flow.Id, err)
	}
	flowChunk.flow = flow
	return nil
}

//removeLogicalDeviceFlow deletes the flow from store and cache.
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) removeLogicalDeviceFlow(ctx context.Context, flowID uint64) error {
	path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flowID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-flow-from-the-store-%s", path)
	}
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	delete(agent.flows, flowID)
	return nil
}

// ListLogicalDeviceFlows returns logical device flows
func (agent *LogicalAgent) ListLogicalDeviceFlows(ctx context.Context) (*ofp.Flows, error) {
	logger.Debug("ListLogicalDeviceFlows")
	var flowStats []*ofp.OfpFlowStats
	agent.flowLock.RLock()
	defer agent.flowLock.RUnlock()
	for _, flowChunk := range agent.flows {
		flowStats = append(flowStats, (proto.Clone(flowChunk.flow)).(*ofp.OfpFlowStats))
	}
	return &ofp.Flows{Items: flowStats}, nil
}

func (agent *LogicalAgent) deleteFlowsOfMeter(ctx context.Context, meterID uint32) error {
	logger.Infow("Delete-flows-matching-meter", log.Fields{"meter": meterID})
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	for flowID, flowChunk := range agent.flows {
		if mID := fu.GetMeterIdFromFlow(ctx, flowChunk.flow); mID != 0 && mID == meterID {
			logger.Debugw("Flow-to-be- deleted", log.Fields{"flow": flowChunk.flow})
			path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flowID)
			if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
				//TODO: Think on carrying on and deleting the remaining flows, instead of returning.
				//Anyways this returns an error to controller which possibly results with a re-deletion.
				//Then how can we handle the new deletion request(Same for group deletion)?
				return fmt.Errorf("couldnt-deleted-flow-from-store-%s", path)
			}
			delete(agent.flows, flowID)
		}
	}
	return nil
}

func (agent *LogicalAgent) deleteFlowsOfGroup(ctx context.Context, groupID uint32) ([]*ofp.OfpFlowStats, error) {
	logger.Infow("Delete-flows-matching-group", log.Fields{"groupID": groupID})
	var flowsRemoved []*ofp.OfpFlowStats
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	for flowID, flowChunk := range agent.flows {
		if fu.FlowHasOutGroup(ctx, flowChunk.flow, groupID) {
			path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flowID)
			if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
				return nil, fmt.Errorf("couldnt-delete-flow-from-store-%s", path)
			}
			delete(agent.flows, flowID)
			flowsRemoved = append(flowsRemoved, flowChunk.flow)
		}
	}
	return flowsRemoved, nil
}
