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
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/route"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
)

//updateFlowTable updates the flow table of that logical device
func (agent *LogicalAgent) updateFlowTable(ctx context.Context, flow *ofp.OfpFlowMod) error {
	logger.Debug("UpdateFlowTable")
	if flow == nil {
		return nil
	}

	switch flow.GetCommand() {
	case ofp.OfpFlowModCommand_OFPFC_ADD:
		return agent.flowAdd(ctx, flow)
	case ofp.OfpFlowModCommand_OFPFC_DELETE:
		return agent.flowDelete(ctx, flow)
	case ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT:
		return agent.flowDeleteStrict(ctx, flow)
	case ofp.OfpFlowModCommand_OFPFC_MODIFY:
		return agent.flowModify(flow)
	case ofp.OfpFlowModCommand_OFPFC_MODIFY_STRICT:
		return agent.flowModifyStrict(flow)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, flow.GetCommand())
}

//flowAdd adds a flow to the flow table of that logical device
func (agent *LogicalAgent) flowAdd(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debugw("flowAdd", log.Fields{"flow": mod})
	if mod == nil {
		return nil
	}
	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		logger.Errorw("flowAdd-failed", log.Fields{"flowMod": mod, "err": err})
		return err
	}
	var updated bool
	var changed bool
	if changed, updated, err = agent.decomposeAndAdd(ctx, flow, mod); err != nil {
		logger.Errorw("flow-decompose-and-add-failed ", log.Fields{"flowMod": mod, "err": err})
		return err
	}
	if changed && !updated {
		if dbupdated := agent.updateFlowCountOfMeterStats(ctx, mod, flow, false); !dbupdated {
			return fmt.Errorf("couldnt-updated-flow-stats-%s", strconv.FormatUint(flow.Id, 10))
		}
	}
	return nil

}

func (agent *LogicalAgent) decomposeAndAdd(ctx context.Context, flow *ofp.OfpFlowStats, mod *ofp.OfpFlowMod) (bool, bool, error) {
	changed := false
	updated := false
	alreadyExist := true
	var flowToReplace *ofp.OfpFlowStats

	//if flow is not found in the map, create a new entry, otherwise get the existing one.
	agent.flowLock.Lock()
	flowChunk, ok := agent.flows[flow.Id]
	if !ok {
		flowChunk = &FlowChunk{
			flow: flow,
		}
		agent.flows[flow.Id] = flowChunk
		alreadyExist = false
		flowChunk.lock.Lock() //acquire chunk lock before releasing map lock
		defer flowChunk.lock.Unlock()
		agent.flowLock.Unlock()
	} else {
		agent.flowLock.Unlock() //release map lock before acquiring chunk lock
		flowChunk.lock.Lock()
		defer flowChunk.lock.Unlock()
	}

	if !alreadyExist {
		flowID := strconv.FormatUint(flow.Id, 10)
		if err := agent.clusterDataProxy.AddWithID(ctx, "logical_flows/"+agent.logicalDeviceID, flowID, flow); err != nil {
			logger.Errorw("failed-adding-flow-to-db", log.Fields{"deviceID": agent.logicalDeviceID, "flowID": flowID, "err": err})
			//Revert the map
			//TODO: Solve the condition:If we have two flow Adds of the same flow (at least same priority and match) in quick succession
			//then if the first one fails while the second one was waiting on the flowchunk, we will end up with an instance of flowChunk that is no longer in the map.
			agent.flowLock.Lock()
			delete(agent.flows, flow.Id)
			agent.flowLock.Unlock()
			return changed, updated, err
		}
	}
	flows := make([]*ofp.OfpFlowStats, 0)
	updatedFlows := make([]*ofp.OfpFlowStats, 0)
	checkOverlap := (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_CHECK_OVERLAP)) != 0
	if checkOverlap {
		if overlapped := fu.FindOverlappingFlows(flows, mod); len(overlapped) != 0 {
			//	TODO:  should this error be notified other than being logged?
			logger.Warnw("overlapped-flows", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		} else {
			//	Add flow
			changed = true
		}
	} else {
		if alreadyExist {
			flowToReplace = flowChunk.flow
			if (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_RESET_COUNTS)) != 0 {
				flow.ByteCount = flowToReplace.ByteCount
				flow.PacketCount = flowToReplace.PacketCount
			}
			if !proto.Equal(flowToReplace, flow) {
				changed = true
				updated = true
			}
		} else {
			changed = true
		}
	}
	logger.Debugw("flowAdd-changed", log.Fields{"changed": changed, "updated": updated})
	if changed {
		updatedFlows = append(updatedFlows, flow)
		var flowMetadata voltha.FlowMetadata
		lMeters, _ := agent.ListLogicalDeviceMeters(ctx)
		if err := agent.GetMeterConfig(updatedFlows, lMeters.Items, &flowMetadata); err != nil {
			logger.Error("Meter-referred-in-flow-not-present")
			return changed, updated, err
		}
		flowGroups, _ := agent.ListLogicalDeviceFlowGroups(ctx)
		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: updatedFlows}, *flowGroups)
		if err != nil {
			return changed, updated, err
		}

		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})
		//	Update store and cache
		if updated {
			if err := agent.updateLogicalDeviceFlow(ctx, flow, flowChunk); err != nil {
				return changed, updated, err
			}
		}

		respChannels := agent.addFlowsAndGroupsToDevices(ctx, deviceRules, &flowMetadata)
		// Create the go routines to wait
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChannels...); res != nil {
				logger.Infow("failed-to-add-flows-will-attempt-deletion", log.Fields{"errors": res, "logical-device-id": agent.logicalDeviceID})
				// Revert added flows
				if err := agent.revertAddedFlows(context.Background(), mod, flow, flowToReplace, deviceRules, &flowMetadata); err != nil {
					logger.Errorw("failure-to-delete-flows-after-failed-addition", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
				}
			}
		}()
	}
	return changed, updated, nil
}

// revertAddedFlows reverts flows after the flowAdd request has failed.  All flows corresponding to that flowAdd request
// will be reverted, both from the logical devices and the devices.
func (agent *LogicalAgent) revertAddedFlows(ctx context.Context, mod *ofp.OfpFlowMod, addedFlow *ofp.OfpFlowStats, replacedFlow *ofp.OfpFlowStats, deviceRules *fu.DeviceRules, metadata *voltha.FlowMetadata) error {
	logger.Debugw("revertFlowAdd", log.Fields{"added-flow": addedFlow, "replaced-flow": replacedFlow, "device-rules": deviceRules, "metadata": metadata})

	agent.flowLock.RLock()
	flowChunk, ok := agent.flows[addedFlow.Id]
	agent.flowLock.RUnlock()
	if !ok {
		// Not found - do nothing
		log.Debugw("flow-not-found", log.Fields{"added-flow": addedFlow})
		return nil
	}
	//Leave the map lock and syncronize per flow
	flowChunk.lock.Lock()
	defer flowChunk.lock.Unlock()

	if replacedFlow != nil {
		if err := agent.updateLogicalDeviceFlow(ctx, replacedFlow, flowChunk); err != nil {
			return err
		}
	} else {
		if err := agent.removeLogicalDeviceFlow(ctx, addedFlow.Id); err != nil {
			return err
		}
	}
	// Revert meters
	if changedMeterStats := agent.updateFlowCountOfMeterStats(ctx, mod, addedFlow, true); !changedMeterStats {
		return fmt.Errorf("Unable-to-revert-meterstats-for-flow-%s", strconv.FormatUint(addedFlow.Id, 10))
	}

	// Update the devices
	respChnls := agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, metadata)

	// Wait for the responses
	go func() {
		// Since this action is taken following an add failure, we may also receive a failure for the revert
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-reverting-added-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
		}
	}()

	return nil
}

//flowDelete deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowDelete(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debug("flowDelete")
	if mod == nil {
		return nil
	}

	fs, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}

	//build a list of what to delete
	toDelete := make([]*ofp.OfpFlowStats, 0)
	toDeleteChunks := make([]*FlowChunk, 0)
	//Lock the map to search the matched flows
	agent.flowLock.RLock()
	for _, f := range agent.flows {
		if fu.FlowMatch(f.flow, fs) {
			toDelete = append(toDelete, f.flow)
			toDeleteChunks = append(toDeleteChunks, f)
			continue
		}
		// Check wild card match
		if fu.FlowMatchesMod(f.flow, mod) {
			toDelete = append(toDelete, f.flow)
			toDeleteChunks = append(toDeleteChunks, f)
		}
	}
	agent.flowLock.RUnlock()
	//Delete the matched flows
	if len(toDelete) > 0 {
		logger.Debugw("flowDelete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "toDelete": len(toDelete)})
		var meters []*ofp.OfpMeterEntry
		var flowGroups []*ofp.OfpGroupEntry
		if ofpMeters, err := agent.ListLogicalDeviceMeters(ctx); err != nil {
			meters = ofpMeters.Items
		}

		if groups, err := agent.ListLogicalDeviceFlowGroups(ctx); err != nil {
			flowGroups = groups.Items
		}

		for _, fc := range toDeleteChunks {
			if err := agent.deleteFlowAndUpdateMeterStats(ctx, mod, fc); err != nil {
				return err
			}
		}
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(toDelete, meters, &flowMetadata); err != nil { // This should never happen
			logger.Error("Meter-referred-in-flows-not-present")
			return err
		}
		var respChnls []coreutils.Response
		var partialRoute bool
		var deviceRules *fu.DeviceRules
		deviceRules, err = agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: toDelete}, ofp.FlowGroups{Items: flowGroups})
		if err != nil {
			// A no route error means no route exists between the ports specified in the flow. This can happen when the
			// child device is deleted and a request to delete flows from the parent device is received
			if !errors.Is(err, route.ErrNoRoute) {
				logger.Errorw("unexpected-error-received", log.Fields{"flows-to-delete": toDelete, "error": err})
				return err
			}
			partialRoute = true
		}

		// Update the devices
		if partialRoute {
			respChnls = agent.deleteFlowsFromParentDevice(ctx, ofp.Flows{Items: toDelete}, &flowMetadata)
		} else {
			respChnls = agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &flowMetadata)
		}

		// Wait for the responses
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
				logger.Errorw("failure-updating-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
				// TODO: Revert the flow deletion
			}
		}()
	}
	//TODO: send announcement on delete
	return nil
}

//flowDeleteStrict deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowDeleteStrict(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debugw("flowDeleteStrict", log.Fields{"mod": mod})
	if mod == nil {
		return nil
	}

	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}
	logger.Debugw("flow-id-in-flow-delete-strict", log.Fields{"flowID": flow.Id})
	agent.flowLock.RLock()
	flowChunk, ok := agent.flows[flow.Id]
	agent.flowLock.RUnlock()
	if !ok {
		logger.Debugw("Skipping-flow-delete-strict-request. No-flow-found", log.Fields{"flowMod": mod})
		return nil
	}
	//Release the map lock and syncronize per flow
	flowChunk.lock.Lock()
	defer flowChunk.lock.Unlock()

	var meters []*ofp.OfpMeterEntry
	var flowGroups []*ofp.OfpGroupEntry
	if ofMeters, er := agent.ListLogicalDeviceMeters(ctx); er == nil {
		meters = ofMeters.Items
	}
	if ofGroups, er := agent.ListLogicalDeviceFlowGroups(ctx); er == nil {
		flowGroups = ofGroups.Items
	}
	if changedMeter := agent.updateFlowCountOfMeterStats(ctx, mod, flow, false); !changedMeter {
		return fmt.Errorf("Cannot delete flow - %s. Meter update failed", flow)
	}

	var flowMetadata voltha.FlowMetadata
	flowsToDelete := []*ofp.OfpFlowStats{flowChunk.flow}
	if err := agent.GetMeterConfig(flowsToDelete, meters, &flowMetadata); err != nil {
		logger.Error("meter-referred-in-flows-not-present")
		return err
	}
	var respChnls []coreutils.Response
	var partialRoute bool
	deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: flowsToDelete}, ofp.FlowGroups{Items: flowGroups})
	if err != nil {
		// A no route error means no route exists between the ports specified in the flow. This can happen when the
		// child device is deleted and a request to delete flows from the parent device is received
		if !errors.Is(err, route.ErrNoRoute) {
			logger.Errorw("unexpected-error-received", log.Fields{"flows-to-delete": flowsToDelete, "error": err})
			return err
		}
		partialRoute = true
	}

	// Update the model
	if err := agent.removeLogicalDeviceFlow(ctx, flow.Id); err != nil {
		return err
	}
	// Update the devices
	if partialRoute {
		respChnls = agent.deleteFlowsFromParentDevice(ctx, ofp.Flows{Items: flowsToDelete}, &flowMetadata)
	} else {
		respChnls = agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &flowMetadata)
	}

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-deleting-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()

	return nil
}

//flowModify modifies a flow from the flow table of that logical device
func (agent *LogicalAgent) flowModify(mod *ofp.OfpFlowMod) error {
	return errors.New("flowModify not implemented")
}

//flowModifyStrict deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowModifyStrict(mod *ofp.OfpFlowMod) error {
	return errors.New("flowModifyStrict not implemented")
}
