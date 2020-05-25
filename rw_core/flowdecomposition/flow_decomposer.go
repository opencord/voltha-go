/*
 * Copyright 2018-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package flowdecomposition

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/coreif"
	"github.com/opencord/voltha-go/rw_core/route"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FlowDecomposer represent flow decomposer attribute
type FlowDecomposer struct {
	deviceMgr coreif.DeviceManager
}

// NewFlowDecomposer creates flow decomposer instance
func NewFlowDecomposer(ctx context.Context, deviceMgr coreif.DeviceManager) *FlowDecomposer {
	var decomposer FlowDecomposer
	decomposer.deviceMgr = deviceMgr
	return &decomposer
}

//DecomposeRules decomposes per-device flows and flow-groups from the flows and groups defined on a logical device
func (fd *FlowDecomposer) DecomposeRules(ctx context.Context, agent coreif.LogicalDeviceAgent, flows ofp.Flows, groups ofp.FlowGroups) (*fu.DeviceRules, error) {
	deviceRules := *fu.NewDeviceRules(ctx)
	devicesToUpdate := make(map[string]string)

	groupMap := make(map[uint32]*ofp.OfpGroupEntry)
	for _, groupEntry := range groups.Items {
		groupMap[groupEntry.Desc.GroupId] = groupEntry
	}

	for _, flow := range flows.Items {
		decomposedRules, err := fd.decomposeFlow(ctx, agent, flow, groupMap)
		if err != nil {
			return nil, err
		}
		for deviceID, flowAndGroups := range decomposedRules.Rules {
			deviceRules.CreateEntryIfNotExist(ctx, deviceID)
			deviceRules.Rules[deviceID].AddFrom(ctx, flowAndGroups)
			devicesToUpdate[deviceID] = deviceID
		}
	}
	return deviceRules.FilterRules(ctx, devicesToUpdate), nil
}

// Handles special case of any controller-bound flow for a parent device
func (fd *FlowDecomposer) updateOutputPortForControllerBoundFlowForParentDevide(ctx context.Context, flow *ofp.OfpFlowStats,
	dr *fu.DeviceRules) (*fu.DeviceRules, error) {
	EAPOL := fu.EthType(ctx, 0x888e)
	IGMP := fu.IpProto(ctx, 2)
	UDP := fu.IpProto(ctx, 17)

	newDeviceRules := dr.Copy(ctx)
	//	Check whether we are dealing with a parent device
	for deviceID, fg := range dr.GetRules(ctx) {
		if root, _ := fd.deviceMgr.IsRootDevice(ctx, deviceID); root {
			newDeviceRules.ClearFlows(ctx, deviceID)
			for i := 0; i < fg.Flows.Len(); i++ {
				f := fg.GetFlow(ctx, i)
				UpdateOutPortNo := false
				for _, field := range fu.GetOfbFields(ctx, f) {
					UpdateOutPortNo = (field.String() == EAPOL.String())
					UpdateOutPortNo = UpdateOutPortNo || (field.String() == IGMP.String())
					UpdateOutPortNo = UpdateOutPortNo || (field.String() == UDP.String())
					if UpdateOutPortNo {
						break
					}
				}
				if UpdateOutPortNo {
					f = fu.UpdateOutputPortByActionType(ctx, f, uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS),
						uint32(ofp.OfpPortNo_OFPP_CONTROLLER))
				}
				// Update flow Id as a change in the instruction field will result in a new flow ID
				//var err error
				//if f.Id, err = fu.HashFlowStats(f); err != nil {
				//return nil, err
				//}
				newDeviceRules.AddFlow(ctx, deviceID, (proto.Clone(f)).(*ofp.OfpFlowStats))
			}
		}
	}

	return newDeviceRules, nil
}

//processControllerBoundFlow decomposes trap flows
func (fd *FlowDecomposer) processControllerBoundFlow(ctx context.Context, agent coreif.LogicalDeviceAgent, path []route.Hop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) (*fu.DeviceRules, error) {

	logger.Debugw("trap-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "flow": flow})
	deviceRules := fu.NewDeviceRules(ctx)
	meterID := fu.GetMeterIdFromFlow(ctx, flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(ctx, flow)

	ingressHop := path[0]
	egressHop := path[1]

	//case of packet_in from NNI port rule
	if agent.GetDeviceRoutes(ctx).IsRootPort(ctx, inPortNo) {
		// Trap flow for NNI port
		logger.Debug("trap-nni")

		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ctx, egressHop.Egress),
			},
			Actions: fu.GetActions(ctx, flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fg := fu.NewFlowsAndGroups(ctx)
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)
		fs, err := fu.MkFlowStat(ctx, fa)
		if err != nil {
			return nil, err
		}
		fg.AddFlow(ctx, fs)
		deviceRules.AddFlowsAndGroup(ctx, egressHop.DeviceID, fg)
	} else {
		// Trap flow for UNI port
		logger.Debug("trap-uni")

		//inPortNo is 0 for wildcard input case, do not include upstream port for controller bound flow in input
		var inPorts []uint32
		if inPortNo == 0 {
			inPorts = agent.GetWildcardInputPorts(ctx, egressHop.Egress) // exclude egress_hop.egress_port.port_no
		} else {
			inPorts = []uint32{inPortNo}
		}
		for _, inputPort := range inPorts {
			// Upstream flow on parent (olt) device
			faParent := &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
				MatchFields: []*ofp.OfpOxmOfbField{
					fu.InPort(ctx, egressHop.Ingress),
					fu.TunnelId(ctx, uint64(inputPort)),
				},
				Actions: []*ofp.OfpAction{
					fu.Output(ctx, egressHop.Egress),
				},
			}
			// Augment the matchfields with the ofpfields from the flow
			faParent.MatchFields = append(faParent.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)
			fgParent := fu.NewFlowsAndGroups(ctx)
			fs, err := fu.MkFlowStat(ctx, faParent)
			if err != nil {
				return nil, err
			}
			fgParent.AddFlow(ctx, fs)
			deviceRules.AddFlowsAndGroup(ctx, egressHop.DeviceID, fgParent)
			logger.Debugw("parent-trap-flow-set", log.Fields{"flow": faParent})

			// Upstream flow on child (onu) device
			var actions []*ofp.OfpAction
			setvid := fu.GetVlanVid(ctx, flow)
			if setvid != nil {
				// have this child push the vlan the parent is matching/trapping on above
				actions = []*ofp.OfpAction{
					fu.PushVlan(ctx, 0x8100),
					fu.SetField(ctx, fu.VlanVid(ctx, *setvid)),
					fu.Output(ctx, ingressHop.Egress),
				}
			} else {
				// otherwise just set the egress port
				actions = []*ofp.OfpAction{
					fu.Output(ctx, ingressHop.Egress),
				}
			}
			faChild := &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
				MatchFields: []*ofp.OfpOxmOfbField{
					fu.InPort(ctx, ingressHop.Ingress),
					fu.TunnelId(ctx, uint64(inputPort)),
				},
				Actions: actions,
			}
			// Augment the matchfields with the ofpfields from the flow.
			// If the parent has a match vid and the child is setting that match vid exclude the the match vlan
			// for the child given it will be setting that vlan and the parent will be matching on it
			if setvid != nil {
				faChild.MatchFields = append(faChild.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT, fu.VLAN_VID)...)
			} else {
				faChild.MatchFields = append(faChild.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)
			}
			fgChild := fu.NewFlowsAndGroups(ctx)
			fs, err = fu.MkFlowStat(ctx, faChild)
			if err != nil {
				return nil, err
			}
			fgChild.AddFlow(ctx, fs)
			deviceRules.AddFlowsAndGroup(ctx, ingressHop.DeviceID, fgChild)
			logger.Debugw("child-trap-flow-set", log.Fields{"flow": faChild})
		}
	}

	return deviceRules, nil
}

// processUpstreamNonControllerBoundFlow processes non-controller bound flow. We assume that anything that is
// upstream needs to get Q-in-Q treatment and that this is expressed via two flow rules, the first using the
// goto-statement. We also assume that the inner tag is applied at the ONU, while the outer tag is
// applied at the OLT
func (fd *FlowDecomposer) processUpstreamNonControllerBoundFlow(ctx context.Context,
	path []route.Hop, inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) (*fu.DeviceRules, error) {

	logger.Debugw("upstream-non-controller-bound-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules(ctx)

	meterID := fu.GetMeterIdFromFlow(ctx, flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(ctx, flow)

	ingressHop := path[0]
	egressHop := path[1]

	if flow.TableId == 0 && fu.HasNextTable(ctx, flow) {
		logger.Debugw("decomposing-onu-flow-in-upstream-has-next-table", log.Fields{"table_id": flow.TableId})
		if outPortNo != 0 {
			logger.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
			return deviceRules, nil
		}
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ctx, ingressHop.Ingress),
				fu.TunnelId(ctx, uint64(inPortNo)),
			},
			Actions: fu.GetActions(ctx, flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, fu.Output(ctx, ingressHop.Egress))

		fg := fu.NewFlowsAndGroups(ctx)
		fs, err := fu.MkFlowStat(ctx, fa)
		if err != nil {
			return nil, err
		}
		fg.AddFlow(ctx, fs)
		deviceRules.AddFlowsAndGroup(ctx, ingressHop.DeviceID, fg)
	} else if flow.TableId == 1 && outPortNo != 0 {
		logger.Debugw("decomposing-olt-flow-in-upstream-has-next-table", log.Fields{"table_id": flow.TableId})
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ctx, egressHop.Ingress),
				fu.TunnelId(ctx, uint64(inPortNo)),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)

		//Augment the actions
		filteredAction := fu.GetActions(ctx, flow, fu.OUTPUT)
		filteredAction = append(filteredAction, fu.Output(ctx, egressHop.Egress))
		fa.Actions = filteredAction

		fg := fu.NewFlowsAndGroups(ctx)
		fs, err := fu.MkFlowStat(ctx, fa)
		if err != nil {
			return nil, err
		}
		fg.AddFlow(ctx, fs)
		deviceRules.AddFlowsAndGroup(ctx, egressHop.DeviceID, fg)
	}
	return deviceRules, nil
}

// processDownstreamFlowWithNextTable decomposes downstream flows containing next table ID instructions
func (fd *FlowDecomposer) processDownstreamFlowWithNextTable(ctx context.Context, agent coreif.LogicalDeviceAgent, path []route.Hop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) (*fu.DeviceRules, error) {
	logger.Debugw("decomposing-olt-flow-in-downstream-flow-with-next-table", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules(ctx)
	meterID := fu.GetMeterIdFromFlow(ctx, flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(ctx, flow)

	if outPortNo != 0 {
		logger.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
		return deviceRules, nil
	}

	if flow.TableId != 0 {
		logger.Warnw("This is not olt pipeline table, so skipping", log.Fields{"tableId": flow.TableId})
		return deviceRules, nil
	}

	ingressHop := path[0]
	egressHop := path[1]
	if metadataFromwriteMetadata != 0 {
		logger.Debugw("creating-metadata-flow", log.Fields{"flow": flow})
		portNumber := fu.GetEgressPortNumberFromWriteMetadata(ctx, flow)
		if portNumber != 0 {
			recalculatedRoute, err := agent.GetRoute(ctx, inPortNo, portNumber)
			if err != nil {
				logger.Errorw("no-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "metadata": metadataFromwriteMetadata, "error": err})
				return deviceRules, nil
			}
			switch len(recalculatedRoute) {
			case 0:
				logger.Errorw("no-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": portNumber, "comment": "deleting-flow", "metadata": metadataFromwriteMetadata})
				//TODO: Delete flow
				return deviceRules, nil
			case 2:
				logger.Debugw("route-found", log.Fields{"ingressHop": ingressHop, "egressHop": egressHop})
			default:
				logger.Errorw("invalid-route-length", log.Fields{"routeLen": len(path)})
				return deviceRules, nil
			}
			ingressHop = recalculatedRoute[0]
		}
		innerTag := fu.GetInnerTagFromMetaData(ctx, flow)
		if innerTag == 0 {
			logger.Errorw("no-inner-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": portNumber, "comment": "deleting-flow", "metadata": metadataFromwriteMetadata})
			//TODO: Delete flow
			return deviceRules, nil
		}
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ctx, ingressHop.Ingress),
				fu.Metadata_ofp(ctx, uint64(innerTag)),
				fu.TunnelId(ctx, uint64(portNumber)),
			},
			Actions: fu.GetActions(ctx, flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT, fu.METADATA)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, fu.Output(ctx, ingressHop.Egress))

		fg := fu.NewFlowsAndGroups(ctx)
		fs, err := fu.MkFlowStat(ctx, fa)
		if err != nil {
			return nil, err
		}
		fg.AddFlow(ctx, fs)
		deviceRules.AddFlowsAndGroup(ctx, ingressHop.DeviceID, fg)
	} else { // Create standard flow
		logger.Debugw("creating-standard-flow", log.Fields{"flow": flow})
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ctx, ingressHop.Ingress),
				fu.TunnelId(ctx, uint64(inPortNo)),
			},
			Actions: fu.GetActions(ctx, flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, fu.Output(ctx, ingressHop.Egress))

		fg := fu.NewFlowsAndGroups(ctx)
		fs, err := fu.MkFlowStat(ctx, fa)
		if err != nil {
			return nil, err
		}
		fg.AddFlow(ctx, fs)
		deviceRules.AddFlowsAndGroup(ctx, ingressHop.DeviceID, fg)
	}

	return deviceRules, nil
}

// processUnicastFlow decomposes unicast flows
func (fd *FlowDecomposer) processUnicastFlow(ctx context.Context, path []route.Hop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) (*fu.DeviceRules, error) {

	logger.Debugw("decomposing-onu-flow-in-downstream-unicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules(ctx)

	egressHop := path[1]

	meterID := fu.GetMeterIdFromFlow(ctx, flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(ctx, flow)
	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(ctx, egressHop.Ingress),
		},
	}
	// Augment the matchfields with the ofpfields from the flow
	fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(ctx, flow, fu.IN_PORT)...)

	// Augment the Actions
	filteredAction := fu.GetActions(ctx, flow, fu.OUTPUT)
	filteredAction = append(filteredAction, fu.Output(ctx, egressHop.Egress))
	fa.Actions = filteredAction

	fg := fu.NewFlowsAndGroups(ctx)
	fs, err := fu.MkFlowStat(ctx, fa)
	if err != nil {
		return nil, err
	}
	fg.AddFlow(ctx, fs)
	deviceRules.AddFlowsAndGroup(ctx, egressHop.DeviceID, fg)
	return deviceRules, nil
}

// processMulticastFlow decompose multicast flows
func (fd *FlowDecomposer) processMulticastFlow(ctx context.Context, path []route.Hop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats, grpID uint32,
	groupMap map[uint32]*ofp.OfpGroupEntry) *fu.DeviceRules {

	logger.Debugw("multicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules(ctx)

	//having no Group yet is the same as having a Group with no buckets
	var grp *ofp.OfpGroupEntry
	var ok bool
	if grp, ok = groupMap[grpID]; !ok {
		logger.Warnw("Group-id-not-present-in-map", log.Fields{"grpId": grpID, "groupMap": groupMap})
		return deviceRules
	}
	if grp == nil || grp.Desc == nil {
		logger.Warnw("Group-or-desc-nil", log.Fields{"grpId": grpID, "grp": grp})
		return deviceRules
	}

	deviceRules.CreateEntryIfNotExist(ctx, path[0].DeviceID)
	fg := fu.NewFlowsAndGroups(ctx)
	fg.AddFlow(ctx, flow)
	//return the multicast flow without decomposing it
	deviceRules.AddFlowsAndGroup(ctx, path[0].DeviceID, fg)
	return deviceRules
}

// decomposeFlow decomposes a flow for a logical device into flows for each physical device
func (fd *FlowDecomposer) decomposeFlow(ctx context.Context, agent coreif.LogicalDeviceAgent, flow *ofp.OfpFlowStats,
	groupMap map[uint32]*ofp.OfpGroupEntry) (*fu.DeviceRules, error) {

	inPortNo := fu.GetInPort(ctx, flow)
	if fu.HasGroup(ctx, flow) && inPortNo == 0 {
		//if no in-port specified for a multicast flow, put NNI port as in-port
		//so that a valid path can be found for the flow
		nniPorts := agent.GetNNIPorts(ctx)
		if len(nniPorts) > 0 {
			inPortNo = nniPorts[0]
			logger.Debugw("assigning-nni-port-as-in-port-for-multicast-flow", log.Fields{"nni": nniPorts[0], "flow:": flow})
		}
	}
	outPortNo := fu.GetOutPort(ctx, flow)
	deviceRules := fu.NewDeviceRules(ctx)
	path, err := agent.GetRoute(ctx, inPortNo, outPortNo)
	if err != nil {
		return deviceRules, err
	}

	switch len(path) {
	case 0:
		return deviceRules, fmt.Errorf("no route from:%d to:%d :%w", inPortNo, outPortNo, route.ErrNoRoute)
	case 2:
		logger.Debugw("route-found", log.Fields{"ingressHop": path[0], "egressHop": path[1]})
	default:
		return deviceRules, fmt.Errorf("invalid route length %d :%w", len(path), route.ErrNoRoute)
	}

	// Process controller bound flow
	if outPortNo != 0 && (outPortNo&0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER) {
		deviceRules, err = fd.processControllerBoundFlow(ctx, agent, path, inPortNo, outPortNo, flow)
		if err != nil {
			return nil, err
		}
	} else {
		var ingressDevice *voltha.Device
		var err error
		if ingressDevice, err = fd.deviceMgr.GetDevice(ctx, &voltha.ID{Id: path[0].DeviceID}); err != nil {
			// This can happen in a race condition where a device is deleted right after we obtain a
			// route involving the device (GetRoute() above).  Handle it as a no route event as well.
			return deviceRules, fmt.Errorf("get-device-error :%v :%w", err, route.ErrNoRoute)
		}
		isUpstream := !ingressDevice.Root
		if isUpstream { // Unicast OLT and ONU UL
			logger.Debug("process-olt-nd-onu-upstream-noncontrollerbound-unicast-flows", log.Fields{"flows": flow})
			deviceRules, err = fd.processUpstreamNonControllerBoundFlow(ctx, path, inPortNo, outPortNo, flow)
			if err != nil {
				return nil, err
			}
		} else if fu.HasNextTable(ctx, flow) && flow.TableId == 0 { // Unicast OLT flow DL
			logger.Debugw("process-olt-downstream-noncontrollerbound-flow-with-nexttable", log.Fields{"flows": flow})
			deviceRules, err = fd.processDownstreamFlowWithNextTable(ctx, agent, path, inPortNo, outPortNo, flow)
			if err != nil {
				return nil, err
			}
		} else if flow.TableId == 1 && outPortNo != 0 { // Unicast ONU flow DL
			logger.Debugw("process-onu-downstream-unicast-flow", log.Fields{"flows": flow})
			deviceRules, err = fd.processUnicastFlow(ctx, path, inPortNo, outPortNo, flow)
			if err != nil {
				return nil, err
			}
		} else if grpID := fu.GetGroup(ctx, flow); grpID != 0 && flow.TableId == 0 { //Multicast
			logger.Debugw("process-multicast-flow", log.Fields{"flows": flow})
			deviceRules = fd.processMulticastFlow(ctx, path, inPortNo, outPortNo, flow, grpID, groupMap)
		} else {
			return deviceRules, status.Errorf(codes.Aborted, "unknown downstream flow %v", *flow)
		}
	}
	deviceRules, err = fd.updateOutputPortForControllerBoundFlowForParentDevide(ctx, flow, deviceRules)
	return deviceRules, err
}
