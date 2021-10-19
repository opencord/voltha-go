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
	"strconv"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/core/device/flow"
	"github.com/opencord/voltha-go/rw_core/route"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v7/pkg/flows"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listLogicalDeviceFlows returns logical device flows
func (agent *LogicalAgent) listLogicalDeviceFlows() map[uint64]*ofp.OfpFlowStats {
	flowIDs := agent.flowCache.ListIDs()
	flows := make(map[uint64]*ofp.OfpFlowStats, len(flowIDs))
	for flowID := range flowIDs {
		if flowHandle, have := agent.flowCache.Lock(flowID); have {
			flows[flowID] = flowHandle.GetReadOnly()
			flowHandle.Unlock()
		}
	}
	return flows
}

//updateFlowTable updates the flow table of that logical device
func (agent *LogicalAgent) updateFlowTable(ctx context.Context, flow *ofp.FlowTableUpdate) error {
	logger.Debug(ctx, "update-flow-table")
	if flow == nil {
		return nil
	}

	switch flow.FlowMod.GetCommand() {
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
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, flow.FlowMod.GetCommand())
}

//flowAdd adds a flow to the flow table of that logical device
func (agent *LogicalAgent) flowAdd(ctx context.Context, flowUpdate *ofp.FlowTableUpdate) error {
	mod := flowUpdate.FlowMod
	logger.Debugw(ctx, "flow-add", log.Fields{"flow": mod})
	if mod == nil {
		return nil
	}
	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		logger.Errorw(ctx, "flow-add-failed", log.Fields{"flow-mod": mod, "err": err})
		return err
	}
	var updated bool
	var changed bool
	if changed, updated, err = agent.decomposeAndAdd(ctx, flow, flowUpdate); err != nil {
		logger.Errorw(ctx, "flow-decompose-and-add-failed ", log.Fields{"flow-mod": mod, "err": err})
		return err
	}
	if changed && !updated {
		if dbupdated := agent.updateFlowCountOfMeterStats(ctx, mod, flow, false); !dbupdated {
			return fmt.Errorf("couldnt-updated-flow-stats-%s", strconv.FormatUint(flow.Id, 10))
		}
	}
	return nil

}

func (agent *LogicalAgent) decomposeAndAdd(ctx context.Context, flow *ofp.OfpFlowStats, flowUpdate *ofp.FlowTableUpdate) (bool, bool, error) {
	changed := false
	updated := false
	mod := flowUpdate.FlowMod
	var flowToReplace *ofp.OfpFlowStats

	//if flow is not found in the map, create a new entry, otherwise get the existing one.
	flowHandle, flowCreated, err := agent.flowCache.LockOrCreate(ctx, flow)
	if err != nil {
		return changed, updated, err
	}
	defer flowHandle.Unlock()

	flows := make([]*ofp.OfpFlowStats, 0)
	checkOverlap := (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_CHECK_OVERLAP)) != 0
	if checkOverlap {
		// TODO: this currently does nothing
		if overlapped := fu.FindOverlappingFlows(flows, mod); len(overlapped) != 0 {
			// TODO: should this error be notified other than being logged?
			logger.Warnw(ctx, "overlapped-flows", log.Fields{"logical-device-id": agent.logicalDeviceID})
		} else {
			//	Add flow
			changed = true
		}
	} else {
		if !flowCreated {
			flowToReplace = flowHandle.GetReadOnly()
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
	logger.Debugw(ctx, "flowAdd-changed", log.Fields{"changed": changed, "updated": updated})
	if changed {
		updatedFlows := map[uint64]*ofp.OfpFlowStats{flow.Id: flow}

		groupIDs := agent.groupCache.ListIDs()
		groups := make(map[uint32]*ofp.OfpGroupEntry, len(groupIDs))
		for groupID := range groupIDs {
			if groupHandle, have := agent.groupCache.Lock(groupID); have {
				groups[groupID] = groupHandle.GetReadOnly()
				groupHandle.Unlock()
			}
		}

		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, updatedFlows, groups)
		if err != nil {
			if flowCreated {
				if er := flowHandle.Delete(ctx); er != nil {
					logger.Errorw(ctx, "deleting-flow-from-cache-failed", log.Fields{"error": er, "flow-id": flow.Id})
				}
			}
			return changed, updated, err
		}

		// Verify whether the flow request can proceed, usually to multiple adapters
		// This is an optimization to address the case where a decomposed set of flows need to
		// be sent to multiple adapters.  One or more adapters may not be ready at this time.
		// If one adapter is not ready this will result in flows being reverted from the
		// other adapters, at times continuously as the OF controller will keep sending the
		// flows until they are successfully added.
		if err := agent.deviceMgr.canMultipleAdapterRequestProceed(ctx, deviceRules.Keys()); err != nil {
			logger.Warnw(ctx, "adapters-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "flow-id": flow.Id, "error": err})
			if flowCreated {
				if er := flowHandle.Delete(ctx); er != nil {
					logger.Errorw(ctx, "deleting-flow-from-cache-failed", log.Fields{"error": er, "flow-id": flow.Id})
				}
			}
			return false, false, err
		}

		logger.Debugw(ctx, "rules", log.Fields{"rules": deviceRules.String()})
		//	Update store and cache
		if updated {
			if err := flowHandle.Update(ctx, flow); err != nil {
				return changed, updated, err
			}
		}
		respChannels := agent.addFlowsAndGroupsToDevices(ctx, deviceRules)

		// Create the go routines to wait
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.internalTimeout, respChannels...); res != nil {
				logger.Errorw(ctx, "failed-to-add-flow-will-attempt-deletion", log.Fields{
					"errors":            res,
					"logical-device-id": agent.logicalDeviceID,
					"flow":              flow,
					"groups":            groups,
				})
				subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)

				// Revert added flows
				if err := agent.revertAddedFlows(subCtx, mod, flow, flowToReplace, deviceRules); err != nil {
					logger.Errorw(ctx, "failure-to-delete-flow-after-failed-addition", log.Fields{
						"error":             err,
						"logical-device-id": agent.logicalDeviceID,
						"flow":              flow,
						"groups":            groups,
					})
				}
				// send event
				agent.ldeviceMgr.SendFlowChangeEvent(ctx, agent.logicalDeviceID, res, flowUpdate.Xid, flowUpdate.FlowMod.Cookie)
				context := make(map[string]string)
				context["rpc"] = coreutils.GetRPCMetadataFromContext(ctx)
				context["flow-id"] = fmt.Sprintf("%v", flow.Id)
				context["flow-cookie"] = fmt.Sprintf("%v", flowUpdate.FlowMod.Cookie)
				context["logical-device-id"] = agent.logicalDeviceID
				if deviceRules != nil {
					context["device-rules"] = deviceRules.String()
				}
				agent.ldeviceMgr.SendRPCEvent(ctx,
					agent.logicalDeviceID, "failed-to-add-flow", context, "RPC_ERROR_RAISE_EVENT",
					voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())
			}
		}()
	}
	return changed, updated, nil
}

// revertAddedFlows reverts flows after the flowAdd request has failed.  All flows corresponding to that flowAdd request
// will be reverted, both from the logical devices and the devices.
func (agent *LogicalAgent) revertAddedFlows(ctx context.Context, mod *ofp.OfpFlowMod, addedFlow *ofp.OfpFlowStats, replacedFlow *ofp.OfpFlowStats, deviceRules *fu.DeviceRules) error {
	logger.Debugw(ctx, "revert-flow-add", log.Fields{"added-flow": addedFlow, "replaced-flow": replacedFlow, "device-rules": deviceRules})

	flowHandle, have := agent.flowCache.Lock(addedFlow.Id)
	if !have {
		// Not found - do nothing
		logger.Debugw(ctx, "flow-not-found", log.Fields{"added-flow": addedFlow})
		return nil
	}
	defer flowHandle.Unlock()

	if replacedFlow != nil {
		if err := flowHandle.Update(ctx, replacedFlow); err != nil {
			return err
		}
	} else {
		if err := flowHandle.Delete(ctx); err != nil {
			return err
		}
	}

	// Revert meters
	if changedMeterStats := agent.updateFlowCountOfMeterStats(ctx, mod, addedFlow, true); !changedMeterStats {
		return fmt.Errorf("Unable-to-revert-meterstats-for-flow-%s", strconv.FormatUint(addedFlow.Id, 10))
	}

	// Update the devices
	respChnls := agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, mod)

	// Wait for the responses
	go func() {
		// Since this action is taken following an add failure, we may also receive a failure for the revert
		if res := coreutils.WaitForNilOrErrorResponses(agent.internalTimeout, respChnls...); res != nil {
			logger.Warnw(ctx, "failure-reverting-added-flows", log.Fields{
				"logical-device-id": agent.logicalDeviceID,
				"flow-cookie":       mod.Cookie,
				"errors":            res,
			})
		}
	}()

	return nil
}

//flowDelete deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowDelete(ctx context.Context, flowUpdate *ofp.FlowTableUpdate) error {
	logger.Debug(ctx, "flow-delete")
	mod := flowUpdate.FlowMod
	if mod == nil {
		return nil
	}

	//build a list of what to delete
	toDelete := make(map[uint64]*ofp.OfpFlowStats)

	// add perfectly matching entry if exists
	fs, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}
	if handle, have := agent.flowCache.Lock(fs.Id); have {
		toDelete[fs.Id] = handle.GetReadOnly()
		handle.Unlock()
	}

	// search through all the flows
	for flowID := range agent.flowCache.ListIDs() {
		if flowHandle, have := agent.flowCache.Lock(flowID); have {
			if flow := flowHandle.GetReadOnly(); fu.FlowMatchesMod(flow, mod) {
				toDelete[flow.Id] = flow
			}
			flowHandle.Unlock()
		}
	}

	//Delete the matched flows
	if len(toDelete) > 0 {
		logger.Debugw(ctx, "flow-delete", log.Fields{"logical-device-id": agent.logicalDeviceID, "to-delete": len(toDelete)})

		for _, flow := range toDelete {
			if flowHandle, have := agent.flowCache.Lock(flow.Id); have {
				// TODO: Flow should only be updated if meter is updated, and meter should only be updated if flow is updated
				//       currently an error while performing the second operation will leave an inconsistent state in kv.
				//       This should be a single atomic operation down to the kv.
				if changedMeter := agent.updateFlowCountOfMeterStats(ctx, mod, flowHandle.GetReadOnly(), false); !changedMeter {
					flowHandle.Unlock()
					return fmt.Errorf("cannot-delete-flow-%d. Meter-update-failed", flow.Id)
				}
				// Update store and cache
				if err := flowHandle.Delete(ctx); err != nil {
					flowHandle.Unlock()
					return fmt.Errorf("cannot-delete-flows-%d. Delete-from-store-failed", flow.Id)
				}
				flowHandle.Unlock()
				// TODO: since this is executed in a loop without also updating meter stats, and error part way through this
				//       operation will leave inconsistent state in the meter stats & flows on the devices.
				//       This & related meter updates should be a single atomic operation down to the kv.
			}
		}

		groups := make(map[uint32]*ofp.OfpGroupEntry)
		for groupID := range agent.groupCache.ListIDs() {
			if groupHandle, have := agent.groupCache.Lock(groupID); have {
				groups[groupID] = groupHandle.GetReadOnly()
				groupHandle.Unlock()
			}
		}

		var respChnls []coreutils.Response
		var partialRoute bool
		var deviceRules *fu.DeviceRules
		deviceRules, err = agent.flowDecomposer.DecomposeRules(ctx, agent, toDelete, groups)
		if err != nil {
			// A no route error means no route exists between the ports specified in the flow. This can happen when the
			// child device is deleted and a request to delete flows from the parent device is received
			if !errors.Is(err, route.ErrNoRoute) {
				logger.Errorw(ctx, "unexpected-error-received", log.Fields{"flows-to-delete": toDelete, "error": err})
				return err
			}
			partialRoute = true
		}

		var devicesInFlows []string
		if deviceRules != nil {
			devicesInFlows = deviceRules.Keys()
		} else {
			devicesInFlows = []string{agent.rootDeviceID}
		}

		if err := agent.deviceMgr.canMultipleAdapterRequestProceed(ctx, devicesInFlows); err != nil {
			logger.Warnw(ctx, "adapters-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "flow": toDelete, "error": err})
			return err
		}

		// Update the devices
		if partialRoute {
			respChnls = agent.deleteFlowsFromParentDevice(ctx, toDelete, mod)
		} else {
			respChnls = agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, mod)
		}

		// Wait for the responses
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.internalTimeout, respChnls...); res != nil {
				logger.Errorw(ctx, "failure-updating-device-flows", log.Fields{"logical-device-id": agent.logicalDeviceID, "errors": res})
				context := make(map[string]string)
				context["rpc"] = coreutils.GetRPCMetadataFromContext(ctx)
				context["logical-device-id"] = agent.logicalDeviceID
				context["flow-id"] = fmt.Sprintf("%v", fs.Id)
				context["flow-cookie"] = fmt.Sprintf("%v", flowUpdate.FlowMod.Cookie)
				if deviceRules != nil {
					context["device-rules"] = deviceRules.String()
				}

				agent.ldeviceMgr.SendRPCEvent(ctx,
					agent.logicalDeviceID, "failed-to-update-device-flows", context, "RPC_ERROR_RAISE_EVENT",
					voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())
				// TODO: Revert the flow deletion
				// send event, and allow any queued events to be sent as well
				agent.ldeviceMgr.SendFlowChangeEvent(ctx, agent.logicalDeviceID, res, flowUpdate.Xid, flowUpdate.FlowMod.Cookie)
			}
		}()
	}
	//TODO: send announcement on delete
	return nil
}

//flowDeleteStrict deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowDeleteStrict(ctx context.Context, flowUpdate *ofp.FlowTableUpdate) error {
	var flowHandle *flow.Handle
	var have bool

	mod := flowUpdate.FlowMod
	logger.Debugw(ctx, "flow-delete-strict", log.Fields{"mod": mod})
	if mod == nil {
		return nil
	}

	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}

	defer func() {
		if flowHandle != nil {
			flowHandle.Unlock()
		}
	}()

	logger.Debugw(ctx, "flow-id-in-flow-delete-strict", log.Fields{"flow-id": flow.Id})
	flowHandle, have = agent.flowCache.Lock(flow.Id)
	if !have {
		logger.Debugw(ctx, "flow-delete-strict-request-no-flow-found-continuing", log.Fields{"flow-mod": mod})
	}

	groups := make(map[uint32]*ofp.OfpGroupEntry)
	for groupID := range agent.groupCache.ListIDs() {
		if groupHandle, have := agent.groupCache.Lock(groupID); have {
			groups[groupID] = groupHandle.GetReadOnly()
			groupHandle.Unlock()
		}
	}

	flowsToDelete := map[uint64]*ofp.OfpFlowStats{flow.Id: flow}
	if flowHandle != nil {
		flowsToDelete = map[uint64]*ofp.OfpFlowStats{flow.Id: flowHandle.GetReadOnly()}
	}

	var respChnls []coreutils.Response
	var partialRoute bool
	deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, flowsToDelete, groups)
	if err != nil {
		// A no route error means no route exists between the ports specified in the flow. This can happen when the
		// child device is deleted and a request to delete flows from the parent device is received
		if !errors.Is(err, route.ErrNoRoute) {
			logger.Errorw(ctx, "unexpected-error-received", log.Fields{"flows-to-delete": flowsToDelete, "error": err})
			return err
		}
		partialRoute = true
	}

	var devicesInFlows []string
	if deviceRules != nil {
		devicesInFlows = deviceRules.Keys()
	} else {
		devicesInFlows = []string{agent.rootDeviceID}
	}

	if err := agent.deviceMgr.canMultipleAdapterRequestProceed(ctx, devicesInFlows); err != nil {
		logger.Warnw(ctx, "adapters-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "flow": flowsToDelete, "error": err})
		return err
	}

	// Update the devices
	if partialRoute {
		respChnls = agent.deleteFlowsFromParentDevice(ctx, flowsToDelete, mod)
	} else {
		respChnls = agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, mod)
	}

	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.internalTimeout, respChnls...); res != nil {
		logger.Warnw(ctx, "failure-deleting-device-flows", log.Fields{
			"flow-cookie":       mod.Cookie,
			"logical-device-id": agent.logicalDeviceID,
			"errors":            res,
		})
		context := make(map[string]string)
		context["rpc"] = coreutils.GetRPCMetadataFromContext(ctx)
		context["flow-id"] = fmt.Sprintf("%v", flow.Id)
		context["flow-cookie"] = fmt.Sprintf("%v", flowUpdate.FlowMod.Cookie)
		context["logical-device-id"] = agent.logicalDeviceID
		if deviceRules != nil {
			context["device-rules"] = deviceRules.String()
		}
		// Create context and send extra information as part of it.
		agent.ldeviceMgr.SendRPCEvent(ctx,
			agent.logicalDeviceID, "failed-to-delete-device-flows", context, "RPC_ERROR_RAISE_EVENT",
			voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())

		return status.Errorf(codes.Aborted, "failed deleting flows id:%d, errors:%v", flow.Id, res)
	}

	// Update meter count
	if changedMeter := agent.updateFlowCountOfMeterStats(ctx, mod, flow, false); !changedMeter {
		return fmt.Errorf("cannot delete flow - %s. Meter update failed", flow)
	}

	// Update the model
	if flowHandle != nil {
		if err := flowHandle.Delete(ctx); err != nil {
			return err
		}
	}

	return nil
}

//flowModify modifies a flow from the flow table of that logical device
func (agent *LogicalAgent) flowModify(flowUpdate *ofp.FlowTableUpdate) error {
	return errors.New("flowModify not implemented")
}

//flowModifyStrict deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowModifyStrict(flowUpdate *ofp.FlowTableUpdate) error {
	return errors.New("flowModifyStrict not implemented")
}

// TODO: Remove this helper, just pass the map through to functions directly
func toMetadata(meters map[uint32]*ofp.OfpMeterConfig) *ofp.FlowMetadata {
	ctr, ret := 0, make([]*ofp.OfpMeterConfig, len(meters))
	for _, meter := range meters {
		ret[ctr] = meter
		ctr++
	}
	return &ofp.FlowMetadata{Meters: ret}
}

func (agent *LogicalAgent) deleteFlowsHavingMeter(ctx context.Context, meterID uint32) error {
	logger.Infow(ctx, "delete-flows-matching-meter", log.Fields{"meter": meterID})
	for flowID := range agent.flowCache.ListIDs() {
		if flowHandle, have := agent.flowCache.Lock(flowID); have {
			if flowMeterID := fu.GetMeterIdFromFlow(flowHandle.GetReadOnly()); flowMeterID != 0 && flowMeterID == meterID {
				if err := flowHandle.Delete(ctx); err != nil {
					//TODO: Think on carrying on and deleting the remaining flows, instead of returning.
					//Anyways this returns an error to controller which possibly results with a re-deletion.
					//Then how can we handle the new deletion request(Same for group deletion)?
					return err
				}
			}
			flowHandle.Unlock()
		}
	}
	return nil
}

func (agent *LogicalAgent) deleteFlowsHavingGroup(ctx context.Context, groupID uint32) (map[uint64]*ofp.OfpFlowStats, error) {
	logger.Infow(ctx, "delete-flows-matching-group", log.Fields{"group-id": groupID})
	flowsRemoved := make(map[uint64]*ofp.OfpFlowStats)
	for flowID := range agent.flowCache.ListIDs() {
		if flowHandle, have := agent.flowCache.Lock(flowID); have {
			if flow := flowHandle.GetReadOnly(); fu.FlowHasOutGroup(flow, groupID) {
				if err := flowHandle.Delete(ctx); err != nil {
					return nil, err
				}
				flowsRemoved[flowID] = flow
			}
			flowHandle.Unlock()
		}
	}
	return flowsRemoved, nil
}
