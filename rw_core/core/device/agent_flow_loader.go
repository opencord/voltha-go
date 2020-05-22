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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	flowRootPath = "flows/"
	flowFullPath = "flows/%s/%d"
)

func (agent *Agent) loadFlows(ctx context.Context) {
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()

	var flowList []*ofp.OfpFlowStats
	if err := agent.clusterDataProxy.List(ctx, flowRootPath+agent.deviceID, &flowList); err != nil {
		logger.Errorw("Failed-to-list-flows-from-cluster-data-proxy", log.Fields{"error": err})
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

//updateDeviceFlow updates flow in the store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *Agent) updateDeviceFlow(ctx context.Context, flow *ofp.OfpFlowStats, flowChunk *FlowChunk) error {
	path := fmt.Sprintf(flowFullPath, agent.deviceID, flow.Id)
	if err := agent.clusterDataProxy.Update(ctx, path, flow); err != nil {
		return status.Errorf(codes.Internal, "failed-update-flow:%s:%d %s", agent.deviceID, flow.Id, err)
	}
	flowChunk.flow = flow
	return nil
}

//removeDeviceFlow deletes the flow from store and cache.
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *Agent) removeDeviceFlow(ctx context.Context, flowID uint64) error {
	path := fmt.Sprintf(flowFullPath, agent.deviceID, flowID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-flow-from-the-store-%s", path)
	}
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	delete(agent.flows, flowID)
	return nil
}

// ListDeviceFlows returns logical device flows
func (agent *Agent) ListDeviceFlows(ctx context.Context) (*ofp.Flows, error) {
	logger.Debug("ListDeviceFlows")
	var flowStats []*ofp.OfpFlowStats
	agent.flowLock.RLock()
	defer agent.flowLock.RUnlock()
	for _, flowChunk := range agent.flows {
		flowStats = append(flowStats, (proto.Clone(flowChunk.flow)).(*ofp.OfpFlowStats))
	}
	return &ofp.Flows{Items: flowStats}, nil
}
