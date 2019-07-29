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

package flow_decomposition

import (
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/rw_core/coreIf"
	"github.com/opencord/voltha-go/rw_core/graph"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
)

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

type FlowDecomposer struct {
	deviceMgr coreIf.DeviceManager
}

func NewFlowDecomposer(deviceMgr coreIf.DeviceManager) *FlowDecomposer {
	var decomposer FlowDecomposer
	decomposer.deviceMgr = deviceMgr
	return &decomposer
}

//DecomposeRules decomposes per-device flows and flow-groups from the flows and groups defined on a logical device
func (fd *FlowDecomposer) DecomposeRules(agent coreIf.LogicalDeviceAgent, flows ofp.Flows, groups ofp.FlowGroups) *fu.DeviceRules {
	deviceRules := *fu.NewDeviceRules()
	devicesToUpdate := make(map[string]string)

	groupMap := make(map[uint32]*ofp.OfpGroupEntry)
	for _, groupEntry := range groups.Items {
		groupMap[groupEntry.Desc.GroupId] = groupEntry
	}

	var decomposedRules *fu.DeviceRules
	for _, flow := range flows.Items {
		decomposedRules = fd.decomposeFlow(agent, flow, groupMap)
		for deviceId, flowAndGroups := range decomposedRules.Rules {
			deviceRules.CreateEntryIfNotExist(deviceId)
			deviceRules.Rules[deviceId].AddFrom(flowAndGroups)
			devicesToUpdate[deviceId] = deviceId
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
	for deviceId, fg := range dr.GetRules() {
		if root, _ := fd.deviceMgr.IsRootDevice(deviceId); root {
			newDeviceRules.ClearFlows(deviceId)
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
				newDeviceRules.AddFlow(deviceId, (proto.Clone(f)).(*ofp.OfpFlowStats))
			}
		}
	}

	return newDeviceRules
}

//processControllerBoundFlow decomposes trap flows
func (fd *FlowDecomposer) processControllerBoundFlow(agent coreIf.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("trap-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "flow": flow})
	deviceRules := fu.NewDeviceRules()
	meterId := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)

	egressHop := route[1]

	fg := fu.NewFlowsAndGroups()
	if agent.GetDeviceGraph().IsRootPort(inPortNo) {
		log.Debug("trap-nni")
		// no decomposition required - it is already an OLT flow from NNI
		fg.AddFlow(flow)
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
			var fa *fu.FlowArgs
			// Upstream flow
			fa = &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterId), "write_metadata": metadataFromwriteMetadata},
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
			fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)
			fg.AddFlow(fu.MkFlowStat(fa))
		}
	}
	deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)

	return deviceRules
}

// processUpstreamNonControllerBoundFlow processes non-controller bound flow. We assume that anything that is
// upstream needs to get Q-in-Q treatment and that this is expressed via two flow rules, the first using the
// goto-statement. We also assume that the inner tag is applied at the ONU, while the outer tag is
// applied at the OLT
func (fd *FlowDecomposer) processUpstreamNonControllerBoundFlow(agent coreIf.LogicalDeviceAgent,
	route []graph.RouteHop, inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("upstream-non-controller-bound-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	meterId := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)

	ingressHop := route[0]
	egressHop := route[1]

	if flow.TableId == 0 && fu.HasNextTable(flow) {
		log.Debugw("decomposing-onu-flow-in-upstream-has-next-table", log.Fields{"table_id": flow.TableId})
		if outPortNo != 0 {
			log.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
			return deviceRules
		}
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterId), "write_metadata": metadataFromwriteMetadata},
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
		if outPortNo == 0 {
			log.Warnw("outPort-should-be-specified", log.Fields{"outPortNo": outPortNo})
		}
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterId), "write_metadata": metadataFromwriteMetadata},
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
func (fd *FlowDecomposer) processDownstreamFlowWithNextTable(agent coreIf.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {
	log.Debugw("decomposing-olt-flow-in-downstream-flow-with-next-table", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()
	meterId := fu.GetMeterIdFromFlow(flow)
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
				break
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
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterId), "write_metadata": metadataFromwriteMetadata},
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
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterId), "write_metadata": metadataFromwriteMetadata},
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
func (fd *FlowDecomposer) processUnicastFlow(agent coreIf.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("decomposing-onu-flow-in-downstream-unicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	egressHop := route[1]

	meterId := fu.GetMeterIdFromFlow(flow)
	metadataFromwriteMetadata := fu.GetMetadataFromWriteMetadataAction(flow)
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie, "meter_id": uint64(meterId), "write_metadata": metadataFromwriteMetadata},
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
func (fd *FlowDecomposer) processMulticastFlow(agent coreIf.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats, grpId uint32,
	groupMap map[uint32]*ofp.OfpGroupEntry) *fu.DeviceRules {

	log.Debugw("multicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	//having no Group yet is the same as having a Group with no buckets
	var grp *ofp.OfpGroupEntry
	var ok bool
	if grp, ok = groupMap[grpId]; !ok {
		log.Warnw("Group-id-not-present-in-map", log.Fields{"grpId": grpId, "groupMap": groupMap})
		return deviceRules
	}
	if grp == nil || grp.Desc == nil {
		log.Warnw("Group-or-desc-nil", log.Fields{"grpId": grpId, "grp": grp})
		return deviceRules
	}
	for _, bucket := range grp.Desc.Buckets {
		otherActions := make([]*ofp.OfpAction, 0)
		for _, action := range bucket.Actions {
			if action.Type == fu.OUTPUT {
				outPortNo = action.GetOutput().Port
			} else if action.Type != fu.POP_VLAN {
				otherActions = append(otherActions, action)
			}
		}

		route2 := agent.GetRoute(inPortNo, outPortNo)
		switch len(route2) {
		case 0:
			log.Errorw("mc-no-route", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "comment": "deleting flow"})
			//	TODO: Delete flow
			return deviceRules
		case 2:
			log.Debugw("route-found", log.Fields{"ingressHop": route2[0], "egressHop": route2[1]})
			break
		default:
			log.Errorw("invalid-route-length", log.Fields{"routeLen": len(route)})
			return deviceRules
		}

		ingressHop := route[0]
		ingressHop2 := route2[0]
		egressHop := route2[1]

		if ingressHop.Ingress != ingressHop2.Ingress {
			log.Errorw("mc-ingress-hop-hop2-mismatch", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo, "comment": "ignoring flow"})
			return deviceRules
		}
		// Set the parent device flow
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(ingressHop.Ingress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT)...)

		// Augment the Actions
		filteredAction := fu.GetActions(flow, fu.GROUP)
		filteredAction = append(filteredAction, fu.PopVlan())
		filteredAction = append(filteredAction, fu.Output(route2[1].Ingress))
		fa.Actions = filteredAction

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)

		// Set the child device flow
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(egressHop.Ingress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, fu.GetOfbFields(flow, fu.IN_PORT, fu.VLAN_VID, fu.VLAN_PCP)...)

		// Augment the Actions
		otherActions = append(otherActions, fu.Output(egressHop.Egress))
		fa.Actions = otherActions

		fg = fu.NewFlowsAndGroups()
		fg.AddFlow(fu.MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	}
	return deviceRules
}

// decomposeFlow decomposes a flow for a logical device into flows for each physical device
func (fd *FlowDecomposer) decomposeFlow(agent coreIf.LogicalDeviceAgent, flow *ofp.OfpFlowStats,
	groupMap map[uint32]*ofp.OfpGroupEntry) *fu.DeviceRules {

	inPortNo := fu.GetInPort(flow)
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
		break
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
		if ingressDevice, err = fd.deviceMgr.GetDevice(route[0].DeviceID); err != nil {
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
		} else if grpId := fu.GetGroup(flow); grpId != 0 && flow.TableId == 0 { //Multicast
			log.Debugw("processMulticastFlow", log.Fields{"flows": flow})
			deviceRules = fd.processMulticastFlow(agent, route, inPortNo, outPortNo, flow, grpId, groupMap)
		} else {
			log.Errorw("unknown-downstream-flow", log.Fields{"flow": *flow})
		}
	}
	deviceRules = fd.updateOutputPortForControllerBoundFlowForParentDevide(flow, deviceRules)
	return deviceRules
}
