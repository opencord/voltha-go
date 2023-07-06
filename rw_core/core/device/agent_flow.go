/*
 * Copyright 2018-2023 Open Networking Foundation (ONF) and the ONF Contributors

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

	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"

	"github.com/gogo/protobuf/proto"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v7/pkg/flows"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listDeviceFlows returns device flows
func (agent *Agent) listDeviceFlows() map[uint64]*ofp.OfpFlowStats {
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

func (agent *Agent) addFlowsToAdapter(ctx context.Context, newFlows []*ofp.OfpFlowStats, flowMetadata *ofp.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "add-flows-to-adapters", log.Fields{"device-id": agent.deviceID, "flows": newFlows, "flow-metadata": flowMetadata})

	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if (len(newFlows)) == 0 {
		logger.Debugw(ctx, "nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": newFlows})
		return coreutils.DoneResponse(), nil
	}
	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return coreutils.DoneResponse(), status.Errorf(codes.Aborted, "%s", err)
	}

	if !agent.proceedWithRequest(device) {
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return coreutils.DoneResponse(), err
	}

	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	flowsToAdd := make([]*ofp.OfpFlowStats, 0)
	flowsToDelete := make([]*ofp.OfpFlowStats, 0)
	for _, flow := range newFlows {
		flowHandle, created, err := agent.flowCache.LockOrCreate(ctx, flow)
		if err != nil {
			desc = err.Error()
			return coreutils.DoneResponse(), err
		}
		if created {
			flowsToAdd = append(flowsToAdd, flow)
		} else {
			flowToReplace := flowHandle.GetReadOnly()
			if !proto.Equal(flowToReplace, flow) {
				//Flow needs to be updated.
				if err := flowHandle.Update(ctx, flow); err != nil {
					flowHandle.Unlock()
					return coreutils.DoneResponse(), err
				}
				flowsToDelete = append(flowsToDelete, flowToReplace)
				flowsToAdd = append(flowsToAdd, flow)
			} else {
				//No need to change the flow. It is already exist.
				logger.Debugw(ctx, "no-need-to-change-already-existing-flow", log.Fields{"device-id": agent.deviceID, "flows": newFlows, "flow-metadata": flowMetadata})
			}
		}
		flowHandle.Unlock()
	}

	// Sanity check
	if (len(flowsToAdd)) == 0 {
		logger.Debugw(ctx, "no-flows-to-update", log.Fields{"device-id": agent.deviceID, "flows": newFlows})
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return coreutils.DoneResponse(), nil
	}

	// Send update to adapters
	response := coreutils.NewResponse()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllFlows := agent.listDeviceFlows()
		ctr, flowSlice := 0, make([]*ofp.OfpFlowStats, len(updatedAllFlows))
		for _, flow := range updatedAllFlows {
			flowSlice[ctr] = flow
			ctr++
		}
		go agent.sendBulkFlows(subCtx, device, &ofp.Flows{Items: flowSlice}, nil, flowMetadata, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &ofp.Flows{Items: flowsToAdd},
			ToRemove: &ofp.Flows{Items: flowsToDelete},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToUpdate: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		go agent.sendIncrementalFlows(subCtx, device, flowChanges, groupChanges, flowMetadata, response)
	}
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}

func (agent *Agent) sendBulkFlows(
	ctx context.Context,
	device *voltha.Device,
	flows *ofp.Flows,
	groups *ofp.FlowGroups,
	flowMetadata *ofp.FlowMetadata,
	response coreutils.Response,
) {
	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	// Get a grpc client
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		response.Error(err)
		return
	}
	subCtx, cancel := context.WithTimeout(ctx, agent.flowTimeout)
	defer cancel()

	if _, err = client.UpdateFlowsBulk(subCtx, &ca.BulkFlows{
		Device:       device,
		Flows:        flows,
		Groups:       groups,
		FlowMetadata: flowMetadata,
	}); err != nil {
		response.Error(err)
	} else {
		response.Done()
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
}

func (agent *Agent) sendIncrementalFlows(
	ctx context.Context,
	device *voltha.Device,
	flowChanges *ofp.FlowChanges,
	groupChanges *ofp.FlowGroupChanges,
	flowMetadata *ofp.FlowMetadata,
	response coreutils.Response,
) {
	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	// Get a grpc client
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		response.Error(err)
		return
	}
	subCtx, cancel := context.WithTimeout(ctx, agent.flowTimeout)
	defer cancel()
	if _, err = client.UpdateFlowsIncrementally(subCtx, &ca.IncrementalFlows{
		Device:       device,
		Flows:        flowChanges,
		Groups:       groupChanges,
		FlowMetadata: flowMetadata,
	}); err != nil {
		response.Error(err)
	} else {
		response.Done()
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
}

func (agent *Agent) deleteFlowsFromAdapter(ctx context.Context, flowsToDel []*ofp.OfpFlowStats, flowMetadata *ofp.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "delete-flows-from-adapter", log.Fields{"device-id": agent.deviceID, "flows": flowsToDel})

	var desc string
	var err error
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if (len(flowsToDel)) == 0 {
		logger.Debugw(ctx, "nothing-to-delete", log.Fields{"device-id": agent.deviceID, "flows": flowsToDel})
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

	for _, flow := range flowsToDel {
		if flowHandle, have := agent.flowCache.Lock(flow.Id); have {
			// Update the store and cache
			if err = flowHandle.Delete(ctx); err != nil {
				flowHandle.Unlock()
				desc = err.Error()
				return coreutils.DoneResponse(), err
			}
			flowHandle.Unlock()
		}
	}

	// Send update to adapters
	response := coreutils.NewResponse()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllFlows := agent.listDeviceFlows()
		ctr, flowSlice := 0, make([]*ofp.OfpFlowStats, len(updatedAllFlows))
		for _, flow := range updatedAllFlows {
			flowSlice[ctr] = flow
			ctr++
		}
		go agent.sendBulkFlows(subCtx, device, &ofp.Flows{Items: flowSlice}, nil, flowMetadata, response)
	} else {
		flowChanges := &ofp.FlowChanges{
			ToAdd:    &ofp.Flows{Items: []*ofp.OfpFlowStats{}},
			ToRemove: &ofp.Flows{Items: flowsToDel},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToUpdate: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		go agent.sendIncrementalFlows(subCtx, device, flowChanges, groupChanges, flowMetadata, response)
	}
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}

func (agent *Agent) updateFlowsToAdapter(ctx context.Context, updatedFlows []*ofp.OfpFlowStats, flowMetadata *ofp.FlowMetadata) (coreutils.Response, error) {
	logger.Debugw(ctx, "update-flows-to-adapter", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows})

	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if (len(updatedFlows)) == 0 {
		logger.Debugw(ctx, "nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows})
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
		err = status.Errorf(codes.FailedPrecondition, "invalid device states")
		return coreutils.DoneResponse(), err
	}

	dType, err := agent.adapterMgr.GetDeviceType(ctx, &voltha.ID{Id: device.Type})
	if err != nil {
		return coreutils.DoneResponse(), err
	}

	flowsToAdd := make([]*ofp.OfpFlowStats, 0, len(updatedFlows))
	flowsToDelete := make([]*ofp.OfpFlowStats, 0, len(updatedFlows))
	for _, flow := range updatedFlows {
		if flowHandle, have := agent.flowCache.Lock(flow.Id); have {
			flowToDelete := flowHandle.GetReadOnly()
			// Update the store and cache
			if err = flowHandle.Update(ctx, flow); err != nil {
				flowHandle.Unlock()
				return coreutils.DoneResponse(), err
			}

			flowsToDelete = append(flowsToDelete, flowToDelete)
			flowsToAdd = append(flowsToAdd, flow)
			flowHandle.Unlock()
		}
	}

	response := coreutils.NewResponse()
	subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
	// Process bulk flow update differently than incremental update
	if !dType.AcceptsAddRemoveFlowUpdates {
		updatedAllFlows := agent.listDeviceFlows()
		ctr, flowSlice := 0, make([]*ofp.OfpFlowStats, len(updatedAllFlows))
		for _, flow := range updatedAllFlows {
			flowSlice[ctr] = flow
			ctr++
		}
		go agent.sendBulkFlows(subCtx, device, &ofp.Flows{Items: flowSlice}, nil, flowMetadata, response)
	} else {
		logger.Debugw(ctx, "updating-flows-and-groups",
			log.Fields{
				"device-id":       agent.deviceID,
				"flows-to-add":    flowsToAdd,
				"flows-to-delete": flowsToDelete,
			})
		// Sanity check
		if (len(flowsToAdd) | len(flowsToDelete)) == 0 {
			logger.Debugw(ctx, "nothing-to-update", log.Fields{"device-id": agent.deviceID, "flows": updatedFlows})
			operStatus.Code = common.OperationResp_OPERATION_SUCCESS
			return coreutils.DoneResponse(), nil
		}

		flowChanges := &ofp.FlowChanges{
			ToAdd:    &ofp.Flows{Items: flowsToAdd},
			ToRemove: &ofp.Flows{Items: flowsToDelete},
		}
		groupChanges := &ofp.FlowGroupChanges{
			ToAdd:    &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToRemove: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
			ToUpdate: &ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{}},
		}
		go agent.sendIncrementalFlows(subCtx, device, flowChanges, groupChanges, flowMetadata, response)
	}
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	return response, nil
}

// filterOutFlows removes flows from a device using the uni-port as filter
func (agent *Agent) filterOutFlows(ctx context.Context, uniPort uint32, flowMetadata *ofp.FlowMetadata) error {
	var flowsToDelete []*ofp.OfpFlowStats
	// If an existing flow has the uniPort as an InPort or OutPort or as a Tunnel ID then it needs to be removed
	for flowID := range agent.flowCache.ListIDs() {
		if flowHandle, have := agent.flowCache.Lock(flowID); have {
			flow := flowHandle.GetReadOnly()
			if flow != nil && (fu.GetInPort(flow) == uniPort || fu.GetOutPort(flow) == uniPort || fu.GetTunnelId(flow) == uint64(uniPort)) {
				flowsToDelete = append(flowsToDelete, flow)
			}
			flowHandle.Unlock()
		}
	}

	logger.Debugw(ctx, "flows-to-delete", log.Fields{"device-id": agent.deviceID, "uni-port": uniPort, "flows": flowsToDelete})
	if len(flowsToDelete) == 0 {
		return nil
	}

	response, err := agent.deleteFlowsFromAdapter(ctx, flowsToDelete, flowMetadata)
	if err != nil {
		return err
	}
	if res := coreutils.WaitForNilOrErrorResponses(agent.flowTimeout, response); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

// deleteAllFlows deletes all flows in the device table
func (agent *Agent) deleteAllFlows(ctx context.Context) error {
	logger.Debugw(ctx, "deleteAllFlows", log.Fields{"device-id": agent.deviceID})

	var err error
	var errFlows string
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	for flowID := range agent.flowCache.ListIDs() {
		if flowHandle, have := agent.flowCache.Lock(flowID); have {
			// Update the store and cache
			if err = flowHandle.Delete(ctx); err != nil {
				flowHandle.Unlock()
				errFlows += fmt.Sprintf("%v ", flowID)
				logger.Errorw(ctx, "unable-to-delete-flow", log.Fields{"device-id": agent.deviceID, "flowID": flowID, "error": err})
				continue
			}
			flowHandle.Unlock()
		}
	}

	if errFlows != "" {
		err = fmt.Errorf("unable to delete flows : %s", errFlows)
	} else {
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
	}
	return err
}
