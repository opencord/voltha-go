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
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/coreif"
	"github.com/opencord/voltha-go/rw_core/graph"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

func init() {
	_, err := log.AddPackage(log.JSON, log.DebugLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

// FlowDecomposer represent flow decomposer attribute
type FlowDecomposer struct {
	deviceMgr coreif.DeviceManager
}

// NewFlowDecomposer creates flow decomposer instance
func NewFlowDecomposer(deviceMgr coreif.DeviceManager) *FlowDecomposer {
	var decomposer FlowDecomposer
	decomposer.deviceMgr = deviceMgr
	return &decomposer
}

//DecomposeRules decomposes per-device flows and flow-groups from the flows and groups defined on a logical device
func (fd *FlowDecomposer) DecomposeRules(ctx context.Context, agent coreif.LogicalDeviceAgent, flows ofp.Flows, groups ofp.FlowGroups) *fu.DeviceRules {
	deviceRules := *fu.NewDeviceRules()
	devicesToUpdate := make(map[string]string)

	groupMap := make(map[uint32]*ofp.OfpGroupEntry)
	for _, groupEntry := range groups.Items {
		groupMap[groupEntry.Desc.GroupId] = groupEntry
	}

	var decomposedRules *fu.DeviceRules
	for _, flow := range flows.Items {
		decomposedRules = fd.decomposeFlow(ctx, agent, flow, groupMap)
		for deviceID, flowAndGroups := range decomposedRules.Rules {
			deviceRules.CreateEntryIfNotExist(deviceID)
			deviceRules.Rules[deviceID].AddFrom(flowAndGroups)
			devicesToUpdate[deviceID] = deviceID
		}
	}
	return deviceRules.FilterRules(devicesToUpdate)
}

// Handles special case of any controller-bound flow for a parent device
func (fd *FlowDecomposer) updateOutputPortForControllerBoundFlowForParentDevide(flow *ofp.OfpFlowStats,
	dr *fu.DeviceRules) *fu.DeviceRules {
	EAPOL := fu.EthType(0x888e)
	IGMP := fu.IpProto(2)
	UDP := fu.IpProto(17)

	newDeviceRules := dr.Copy()
	//	Check whether we are dealing with a parent device
	for deviceID, fg := range dr.GetRules() {
		if root, _ := fd.deviceMgr.IsRootDevice(deviceID); root {
			newDeviceRules.ClearFlows(deviceID)
			for i := 0; i < fg.Flows.Len(); i++ {
				f := fg.GetFlow(i)
				UpdateOutPortNo := false
				for _, field := range fu.GetOfbFields(f) {
					UpdateOutPortNo = (field.String() == EAPOL.String())
					UpdateOutPortNo = UpdateOutPortNo || (field.String() == IGMP.String())
					UpdateOutPortNo = UpdateOutPortNo || (field.String() == UDP.String())
					if UpdateOutPortNo {
						break
					}
				}
				if UpdateOutPortNo {
					f = fu.UpdateOutputPortByActionType(f, uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS),
						uint32(ofp.OfpPortNo_OFPP_CONTROLLER))
				}
				// Update flow Id as a change in the instruction field will result in a new flow ID
				f.Id = fu.HashFlowStats(f)
				newDeviceRules.AddFlow(deviceID, (proto.Clone(f)).(*ofp.OfpFlowStats))
			}
		}
	}

	return newDeviceRules
}

//processControllerBoundFlow decomposes trap flows
func (fd *FlowDecomposer) processControllerBoundFlow(agent coreif.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("trap-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "flow": flow})
	deviceRules := fu.NewDeviceRules()
	meterID := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)

	ingressHop := route[0]
	egressHop := route[1]

	//case of packet_in from NNI port rule
	if agent.GetDeviceGraph().IsRootPort(inPortNo) {
		// Trap flow for NNI port
		log.Debug("trap-nni")

		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(egressHop.Egress),
			},
			Actions: fu.GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fg := fu.NewFlowsAndGroups()
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	} else {
		// Trap flow for UNI port
		log.Debug("trap-uni")

		//inPortNo is 0 for wildcard input case, do not include upstream port for 4000 flow in input
		var inPorts []uint32
		if inPortNo == 0 {
			inPorts = agent.GetWildcardInputPorts(egressHop.Egress) // exclude egress_hop.egress_port.port_no
		} else {
			inPorts = []uint32{inPortNo}
		}
		for _, inputPort := range inPorts {
			// Upstream flow on parent (olt) device
			faParent := &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
				MatchFields: []*ofp.OfpOxmOfbField{
					fu.InPort(egressHop.Ingress),
					fu.TunnelId(uint64(inputPort)),
				},
				Actions: []*ofp.OfpAction{
					fu.PushVlan(0x8100),
					fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000)),
					fu.Output(egressHop.Egress),
				},
			}
			// Augment the matchfields with the ofpfields from the flow
			faParent.MatchFields = append(faParent.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)
			fgParent := fu.NewFlowsAndGroups()
			fgParent.AddFlow(fu.MkFlowStat(faParent))
			deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fgParent)
			log.Debugw("parent-trap-flow-set", log.Fields{"flow": faParent})

			// Upstream flow on child (onu) device
			var actions []*ofp.OfpAction
			setvid := fu.GetVlanVid(flow)
			if setvid != nil {
				// have this child push the vlan the parent is matching/trapping on above
				actions = []*ofp.OfpAction{
					fu.PushVlan(0x8100),
					fu.SetField(fu.VlanVid(*setvid)),
					fu.Output(ingressHop.Egress),
				}
			} else {
				// otherwise just set the egress port
				actions = []*ofp.OfpAction{
					fu.Output(ingressHop.Egress),
				}
			}
			faChild := &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
				MatchFields: []*ofp.OfpOxmOfbField{
					fu.InPort(ingressHop.Ingress),
					fu.TunnelId(uint64(inputPort)),
				},
				Actions: actions,
			}
			// Augment the matchfields with the ofpfields from the flow.
			// If the parent has a match vid and the child is setting that match vid exclude the the match vlan
			// for the child given it will be setting that vlan and the parent will be matching on it
			if setvid != nil {
				faChild.MatchFields = append(faChild.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT, fu.VLAN_VID)...)
			} else {
				faChild.MatchFields = append(faChild.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)
			}
			fgChild := fu.NewFlowsAndGroups()
			fgChild.AddFlow(fu.MkFlowStat(faChild))
			deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fgChild)
			log.Debugw("child-trap-flow-set", log.Fields{"flow": faChild})
		}
	}

	return deviceRules
}

// processUpstreamNonControllerBoundFlow processes non-controller bound flow. We assume that anything that is
// upstream needs to get Q-in-Q treatment and that this is expressed via two flow rules, the first using the
// goto-statement. We also assume that the inner tag is applied at the ONU, while the outer tag is
// applied at the OLT
func (fd *FlowDecomposer) processUpstreamNonControllerBoundFlow(agent coreif.LogicalDeviceAgent,
	route []graph.RouteHop, inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("upstream-non-controller-bound-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	meterID := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)

	ingressHop := route[0]
	egressHop := route[1]

	if flow.TableId == 0 && fu.HasNextTable(flow) {
		log.Debugw("decomposing-onu-flow-in-upstream-has-next-table", log.Fields{"table_id": flow.TableId})
		if outPortNo != 0 {
			log.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
			return deviceRules
		}
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ingressHop.Ingress),
				fu.TunnelId(uint64(inPortNo)),
			},
			Actions: fu.GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, fu.Output(ingressHop.Egress))

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)
	} else if flow.TableId == 1 && outPortNo != 0 {
		log.Debugw("decomposing-olt-flow-in-upstream-has-next-table", log.Fields{"table_id": flow.TableId})
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(egressHop.Ingress),
				fu.TunnelId(uint64(inPortNo)),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)

		//Augment the actions
		filteredAction := fu.GetActions(flow, fu.OUTPUT)
		filteredAction = append(filteredAction, fu.Output(egressHop.Egress))
		fa.Actions = filteredAction

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	}
	return deviceRules
}

// processDownstreamFlowWithNextTable decomposes downstream flows containing next table ID instructions
func (fd *FlowDecomposer) processDownstreamFlowWithNextTable(agent coreif.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {
	log.Debugw("decomposing-olt-flow-in-downstream-flow-with-next-table", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()
	meterID := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)

	if outPortNo != 0 {
		log.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
		return deviceRules
	}

	if flow.TableId != 0 {
		log.Warnw("This is not olt pipeline table, so skipping", log.Fields{"tableId": flow.TableId})
		return deviceRules
	}

	ingressHop := route[0]
	egressHop := route[1]
	if metadataFromwriteMetadata != 0 {
		log.Debugw("creating-metadata-flow", log.Fields{"flow": flow})
		portNumber := fu.GetEgressPortNumberFromWriteMetadata(flow)
		if portNumber != 0 {
			recalculatedRoute := agent.GetRoute(inPortNo, portNumber)
			switch len(recalculatedRoute) {
			case 0:
				log.Errorw("no-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": portNumber, "comment": "deleting-flow", "metadata": metadataFromwriteMetadata})
				//TODO: Delete flow
				return deviceRules
			case 2:
				log.Debugw("route-found", log.Fields{"ingressHop": ingressHop, "egressHop": egressHop})
			default:
				log.Errorw("invalid-route-length", log.Fields{"routeLen": len(route)})
				return deviceRules
			}
			ingressHop = recalculatedRoute[0]
		}
		innerTag := fu.GetInnerTagFromMetaData(flow)
		if innerTag == 0 {
			log.Errorw("no-inner-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": portNumber, "comment": "deleting-flow", "metadata": metadataFromwriteMetadata})
			//TODO: Delete flow
			return deviceRules
		}
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ingressHop.Ingress),
				fu.Metadata_ofp(uint64(innerTag)),
				fu.TunnelId(uint64(portNumber)),
			},
			Actions: fu.GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT, fu.METADATA)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, fu.Output(ingressHop.Egress))

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)
	} else { // Create standard flow
		log.Debugw("creating-standard-flow", log.Fields{"flow": flow})
		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ingressHop.Ingress),
				fu.TunnelId(uint64(inPortNo)),
			},
			Actions: fu.GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, fu.Output(ingressHop.Egress))

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)
	}

	return deviceRules
}

// processUnicastFlow decomposes unicast flows
func (fd *FlowDecomposer) processUnicastFlow(agent coreif.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("decomposing-onu-flow-in-downstream-unicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	egressHop := route[1]

	meterID := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)
	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterID), "write_metadata": metadataFromwriteMetadata},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(egressHop.Ingress),
		},
	}
	// Augment the matchfields with the ofpfields from the flow
	fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)

	// Augment the Actions
	filteredAction := fu.GetActions(flow, fu.OUTPUT)
	filteredAction = append(filteredAction, fu.Output(egressHop.Egress))
	fa.Actions = filteredAction

	fg := fu.NewFlowsAndGroups()
	fg.AddFlow(fu.MkFlowStat(fa))
	deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	return deviceRules
}

// processMulticastFlow decompose multicast flows
func (fd *FlowDecomposer) processMulticastFlow(agent coreif.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats, grpID uint32,
	groupMap map[uint32]*ofp.OfpGroupEntry) *fu.DeviceRules {

	log.Debugw("multicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	//having no Group yet is the same as having a Group with no buckets
	var grp *ofp.OfpGroupEntry
	var ok bool
	if grp, ok = groupMap[grpID]; !ok {
		log.Warnw("Group-id-not-present-in-map", log.Fields{"grpId": grpID, "groupMap": groupMap})
		return deviceRules
	}
	if grp == nil || grp.Desc == nil {
		log.Warnw("Group-or-desc-nil", log.Fields{"grpId": grpID, "grp": grp})
		return deviceRules
	}

	deviceRules.CreateEntryIfNotExist(route[0].DeviceID)
	fg := fu.NewFlowsAndGroups()
	fg.AddFlow(flow)
	//return the multicast flow without decomposing it
	deviceRules.AddFlowsAndGroup(route[0].DeviceID, fg)
	return deviceRules
}

// decomposeFlow decomposes a flow for a logical device into flows for each physical device
func (fd *FlowDecomposer) decomposeFlow(ctx context.Context, agent coreif.LogicalDeviceAgent, flow *ofp.OfpFlowStats,
	groupMap map[uint32]*ofp.OfpGroupEntry) *fu.DeviceRules {

	inPortNo := fu.GetInPort(flow)
	if fu.HasGroup(flow) && inPortNo == 0 {
		//if no in-port specified for a multicast flow, put NNI port as in-port
		//so that a valid route can be found for the flow
		nniPorts := agent.GetNNIPorts()
		if len(nniPorts) > 0 {
			inPortNo = nniPorts[0]
			log.Debugw("Assigning NNI port as in-port for the multicast flow", log.Fields{"nni": nniPorts[0], "flow:": flow})
		}
	}
	outPortNo := fu.GetOutPort(flow)
	deviceRules := fu.NewDeviceRules()
	route := agent.GetRoute(inPortNo, outPortNo)

	switch len(route) {
	case 0:
		log.Errorw("no-route", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "comment": "deleting-flow"})
		//	TODO: Delete flow
		return deviceRules
	case 2:
		log.Debugw("route-found", log.Fields{"ingressHop": route[0], "egressHop": route[1]})
	default:
		log.Errorw("invalid-route-length", log.Fields{"routeLen": len(route)})
		return deviceRules
	}

	// Process controller bound flow
	if outPortNo != 0 && (outPortNo&0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER) {
		deviceRules = fd.processControllerBoundFlow(agent, route, inPortNo, outPortNo, flow)
	} else {
		var ingressDevice *voltha.Device
		var err error
		if ingressDevice, err = fd.deviceMgr.GetDevice(ctx, route[0].DeviceID); err != nil {
			log.Errorw("ingress-device-not-found", log.Fields{"deviceId": route[0].DeviceID, "flow": flow})
			return deviceRules
		}
		isUpstream := !ingressDevice.Root
		if isUpstream { // Unicast OLT and ONU UL
			log.Info("processOltAndOnuUpstreamNonControllerBoundUnicastFlows", log.Fields{"flows": flow})
			deviceRules = fd.processUpstreamNonControllerBoundFlow(agent, route, inPortNo, outPortNo, flow)
		} else if fu.HasNextTable(flow) && flow.TableId == 0 { // Unicast OLT flow DL
			log.Debugw("processOltDownstreamNonControllerBoundFlowWithNextTable", log.Fields{"flows": flow})
			deviceRules = fd.processDownstreamFlowWithNextTable(agent, route, inPortNo, outPortNo, flow)
		} else if flow.TableId == 1 && outPortNo != 0 { // Unicast ONU flow DL
			log.Debugw("processOnuDownstreamUnicastFlow", log.Fields{"flows": flow})
			deviceRules = fd.processUnicastFlow(agent, route, inPortNo, outPortNo, flow)
		} else if grpID := fu.GetGroup(flow); grpID != 0 && flow.TableId == 0 { //Multicast
			log.Debugw("processMulticastFlow", log.Fields{"flows": flow})
			deviceRules = fd.processMulticastFlow(agent, route, inPortNo, outPortNo, flow, grpID, groupMap)
		} else {
			log.Errorw("unknown-downstream-flow", log.Fields{"flow": *flow})
		}
	}
	deviceRules = fd.updateOutputPortForControllerBoundFlowForParentDevide(flow, deviceRules)
	return deviceRules
}
