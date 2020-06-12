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
package flows

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"hash"
	"sort"

	"github.com/cevaris/ordered_map"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
)

var (
	// Instructions shortcut
	APPLY_ACTIONS  = ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS
	WRITE_METADATA = ofp.OfpInstructionType_OFPIT_WRITE_METADATA
	METER_ACTION   = ofp.OfpInstructionType_OFPIT_METER

	//OFPAT_* shortcuts
	OUTPUT       = ofp.OfpActionType_OFPAT_OUTPUT
	COPY_TTL_OUT = ofp.OfpActionType_OFPAT_COPY_TTL_OUT
	COPY_TTL_IN  = ofp.OfpActionType_OFPAT_COPY_TTL_IN
	SET_MPLS_TTL = ofp.OfpActionType_OFPAT_SET_MPLS_TTL
	DEC_MPLS_TTL = ofp.OfpActionType_OFPAT_DEC_MPLS_TTL
	PUSH_VLAN    = ofp.OfpActionType_OFPAT_PUSH_VLAN
	POP_VLAN     = ofp.OfpActionType_OFPAT_POP_VLAN
	PUSH_MPLS    = ofp.OfpActionType_OFPAT_PUSH_MPLS
	POP_MPLS     = ofp.OfpActionType_OFPAT_POP_MPLS
	SET_QUEUE    = ofp.OfpActionType_OFPAT_SET_QUEUE
	GROUP        = ofp.OfpActionType_OFPAT_GROUP
	SET_NW_TTL   = ofp.OfpActionType_OFPAT_SET_NW_TTL
	NW_TTL       = ofp.OfpActionType_OFPAT_DEC_NW_TTL
	SET_FIELD    = ofp.OfpActionType_OFPAT_SET_FIELD
	PUSH_PBB     = ofp.OfpActionType_OFPAT_PUSH_PBB
	POP_PBB      = ofp.OfpActionType_OFPAT_POP_PBB
	EXPERIMENTER = ofp.OfpActionType_OFPAT_EXPERIMENTER

	//OFPXMT_OFB_* shortcuts (incomplete)
	IN_PORT         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IN_PORT
	IN_PHY_PORT     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IN_PHY_PORT
	METADATA        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_METADATA
	ETH_DST         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ETH_DST
	ETH_SRC         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ETH_SRC
	ETH_TYPE        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ETH_TYPE
	VLAN_VID        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_VLAN_VID
	VLAN_PCP        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_VLAN_PCP
	IP_DSCP         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IP_DSCP
	IP_ECN          = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IP_ECN
	IP_PROTO        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IP_PROTO
	IPV4_SRC        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV4_SRC
	IPV4_DST        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV4_DST
	TCP_SRC         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_TCP_SRC
	TCP_DST         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_TCP_DST
	UDP_SRC         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_UDP_SRC
	UDP_DST         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_UDP_DST
	SCTP_SRC        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_SCTP_SRC
	SCTP_DST        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_SCTP_DST
	ICMPV4_TYPE     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ICMPV4_TYPE
	ICMPV4_CODE     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ICMPV4_CODE
	ARP_OP          = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ARP_OP
	ARP_SPA         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ARP_SPA
	ARP_TPA         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ARP_TPA
	ARP_SHA         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ARP_SHA
	ARP_THA         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ARP_THA
	IPV6_SRC        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_SRC
	IPV6_DST        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_DST
	IPV6_FLABEL     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_FLABEL
	ICMPV6_TYPE     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ICMPV6_TYPE
	ICMPV6_CODE     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_ICMPV6_CODE
	IPV6_ND_TARGET  = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_ND_TARGET
	OFB_IPV6_ND_SLL = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_ND_SLL
	IPV6_ND_TLL     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_ND_TLL
	MPLS_LABEL      = ofp.OxmOfbFieldTypes_OFPXMT_OFB_MPLS_LABEL
	MPLS_TC         = ofp.OxmOfbFieldTypes_OFPXMT_OFB_MPLS_TC
	MPLS_BOS        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_MPLS_BOS
	PBB_ISID        = ofp.OxmOfbFieldTypes_OFPXMT_OFB_PBB_ISID
	TUNNEL_ID       = ofp.OxmOfbFieldTypes_OFPXMT_OFB_TUNNEL_ID
	IPV6_EXTHDR     = ofp.OxmOfbFieldTypes_OFPXMT_OFB_IPV6_EXTHDR
)

//ofp_action_* shortcuts

func Output(port uint32, maxLen ...ofp.OfpControllerMaxLen) *ofp.OfpAction {
	maxLength := ofp.OfpControllerMaxLen_OFPCML_MAX
	if len(maxLen) > 0 {
		maxLength = maxLen[0]
	}
	return &ofp.OfpAction{Type: OUTPUT, Action: &ofp.OfpAction_Output{Output: &ofp.OfpActionOutput{Port: port, MaxLen: uint32(maxLength)}}}
}

func MplsTtl(ttl uint32) *ofp.OfpAction {
	return &ofp.OfpAction{Type: SET_MPLS_TTL, Action: &ofp.OfpAction_MplsTtl{MplsTtl: &ofp.OfpActionMplsTtl{MplsTtl: ttl}}}
}

func PushVlan(ethType uint32) *ofp.OfpAction {
	return &ofp.OfpAction{Type: PUSH_VLAN, Action: &ofp.OfpAction_Push{Push: &ofp.OfpActionPush{Ethertype: ethType}}}
}

func PopVlan() *ofp.OfpAction {
	return &ofp.OfpAction{Type: POP_VLAN}
}

func PopMpls(ethType uint32) *ofp.OfpAction {
	return &ofp.OfpAction{Type: POP_MPLS, Action: &ofp.OfpAction_PopMpls{PopMpls: &ofp.OfpActionPopMpls{Ethertype: ethType}}}
}

func Group(groupId uint32) *ofp.OfpAction {
	return &ofp.OfpAction{Type: GROUP, Action: &ofp.OfpAction_Group{Group: &ofp.OfpActionGroup{GroupId: groupId}}}
}

func NwTtl(nwTtl uint32) *ofp.OfpAction {
	return &ofp.OfpAction{Type: NW_TTL, Action: &ofp.OfpAction_NwTtl{NwTtl: &ofp.OfpActionNwTtl{NwTtl: nwTtl}}}
}

func SetField(field *ofp.OfpOxmOfbField) *ofp.OfpAction {
	actionSetField := &ofp.OfpOxmField{OxmClass: ofp.OfpOxmClass_OFPXMC_OPENFLOW_BASIC, Field: &ofp.OfpOxmField_OfbField{OfbField: field}}
	return &ofp.OfpAction{Type: SET_FIELD, Action: &ofp.OfpAction_SetField{SetField: &ofp.OfpActionSetField{Field: actionSetField}}}
}

func Experimenter(experimenter uint32, data []byte) *ofp.OfpAction {
	return &ofp.OfpAction{Type: EXPERIMENTER, Action: &ofp.OfpAction_Experimenter{Experimenter: &ofp.OfpActionExperimenter{Experimenter: experimenter, Data: data}}}
}

//ofb_field generators (incomplete set)

func InPort(inPort uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IN_PORT, Value: &ofp.OfpOxmOfbField_Port{Port: inPort}}
}

func InPhyPort(inPhyPort uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IN_PHY_PORT, Value: &ofp.OfpOxmOfbField_Port{Port: inPhyPort}}
}

func Metadata_ofp(tableMetadata uint64) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: METADATA, Value: &ofp.OfpOxmOfbField_TableMetadata{TableMetadata: tableMetadata}}
}

// should Metadata_ofp used here ?????
func EthDst(ethDst uint64) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ETH_DST, Value: &ofp.OfpOxmOfbField_TableMetadata{TableMetadata: ethDst}}
}

// should Metadata_ofp used here ?????
func EthSrc(ethSrc uint64) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ETH_SRC, Value: &ofp.OfpOxmOfbField_TableMetadata{TableMetadata: ethSrc}}
}

func EthType(ethType uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ETH_TYPE, Value: &ofp.OfpOxmOfbField_EthType{EthType: ethType}}
}

func VlanVid(vlanVid uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: VLAN_VID, Value: &ofp.OfpOxmOfbField_VlanVid{VlanVid: vlanVid}}
}

func VlanPcp(vlanPcp uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: VLAN_PCP, Value: &ofp.OfpOxmOfbField_VlanPcp{VlanPcp: vlanPcp}}
}

func IpDscp(ipDscp uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IP_DSCP, Value: &ofp.OfpOxmOfbField_IpDscp{IpDscp: ipDscp}}
}

func IpEcn(ipEcn uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IP_ECN, Value: &ofp.OfpOxmOfbField_IpEcn{IpEcn: ipEcn}}
}

func IpProto(ipProto uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IP_PROTO, Value: &ofp.OfpOxmOfbField_IpProto{IpProto: ipProto}}
}

func Ipv4Src(ipv4Src uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV4_SRC, Value: &ofp.OfpOxmOfbField_Ipv4Src{Ipv4Src: ipv4Src}}
}

func Ipv4Dst(ipv4Dst uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV4_DST, Value: &ofp.OfpOxmOfbField_Ipv4Dst{Ipv4Dst: ipv4Dst}}
}

func TcpSrc(tcpSrc uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: TCP_SRC, Value: &ofp.OfpOxmOfbField_TcpSrc{TcpSrc: tcpSrc}}
}

func TcpDst(tcpDst uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: TCP_DST, Value: &ofp.OfpOxmOfbField_TcpDst{TcpDst: tcpDst}}
}

func UdpSrc(udpSrc uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: UDP_SRC, Value: &ofp.OfpOxmOfbField_UdpSrc{UdpSrc: udpSrc}}
}

func UdpDst(udpDst uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: UDP_DST, Value: &ofp.OfpOxmOfbField_UdpDst{UdpDst: udpDst}}
}

func SctpSrc(sctpSrc uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: SCTP_SRC, Value: &ofp.OfpOxmOfbField_SctpSrc{SctpSrc: sctpSrc}}
}

func SctpDst(sctpDst uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: SCTP_DST, Value: &ofp.OfpOxmOfbField_SctpDst{SctpDst: sctpDst}}
}

func Icmpv4Type(icmpv4Type uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ICMPV4_TYPE, Value: &ofp.OfpOxmOfbField_Icmpv4Type{Icmpv4Type: icmpv4Type}}
}

func Icmpv4Code(icmpv4Code uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ICMPV4_CODE, Value: &ofp.OfpOxmOfbField_Icmpv4Code{Icmpv4Code: icmpv4Code}}
}

func ArpOp(arpOp uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ARP_OP, Value: &ofp.OfpOxmOfbField_ArpOp{ArpOp: arpOp}}
}

func ArpSpa(arpSpa uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ARP_SPA, Value: &ofp.OfpOxmOfbField_ArpSpa{ArpSpa: arpSpa}}
}

func ArpTpa(arpTpa uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ARP_TPA, Value: &ofp.OfpOxmOfbField_ArpTpa{ArpTpa: arpTpa}}
}

func ArpSha(arpSha []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ARP_SHA, Value: &ofp.OfpOxmOfbField_ArpSha{ArpSha: arpSha}}
}

func ArpTha(arpTha []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ARP_THA, Value: &ofp.OfpOxmOfbField_ArpTha{ArpTha: arpTha}}
}

func Ipv6Src(ipv6Src []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV6_SRC, Value: &ofp.OfpOxmOfbField_Ipv6Src{Ipv6Src: ipv6Src}}
}

func Ipv6Dst(ipv6Dst []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV6_DST, Value: &ofp.OfpOxmOfbField_Ipv6Dst{Ipv6Dst: ipv6Dst}}
}

func Ipv6Flabel(ipv6Flabel uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV6_FLABEL, Value: &ofp.OfpOxmOfbField_Ipv6Flabel{Ipv6Flabel: ipv6Flabel}}
}

func Icmpv6Type(icmpv6Type uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ICMPV6_TYPE, Value: &ofp.OfpOxmOfbField_Icmpv6Type{Icmpv6Type: icmpv6Type}}
}

func Icmpv6Code(icmpv6Code uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: ICMPV6_CODE, Value: &ofp.OfpOxmOfbField_Icmpv6Code{Icmpv6Code: icmpv6Code}}
}

func Ipv6NdTarget(ipv6NdTarget []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV6_ND_TARGET, Value: &ofp.OfpOxmOfbField_Ipv6NdTarget{Ipv6NdTarget: ipv6NdTarget}}
}

func OfbIpv6NdSll(ofbIpv6NdSll []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: OFB_IPV6_ND_SLL, Value: &ofp.OfpOxmOfbField_Ipv6NdSsl{Ipv6NdSsl: ofbIpv6NdSll}}
}

func Ipv6NdTll(ipv6NdTll []byte) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV6_ND_TLL, Value: &ofp.OfpOxmOfbField_Ipv6NdTll{Ipv6NdTll: ipv6NdTll}}
}

func MplsLabel(mplsLabel uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: MPLS_LABEL, Value: &ofp.OfpOxmOfbField_MplsLabel{MplsLabel: mplsLabel}}
}

func MplsTc(mplsTc uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: MPLS_TC, Value: &ofp.OfpOxmOfbField_MplsTc{MplsTc: mplsTc}}
}

func MplsBos(mplsBos uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: MPLS_BOS, Value: &ofp.OfpOxmOfbField_MplsBos{MplsBos: mplsBos}}
}

func PbbIsid(pbbIsid uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: PBB_ISID, Value: &ofp.OfpOxmOfbField_PbbIsid{PbbIsid: pbbIsid}}
}

func TunnelId(tunnelId uint64) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: TUNNEL_ID, Value: &ofp.OfpOxmOfbField_TunnelId{TunnelId: tunnelId}}
}

func Ipv6Exthdr(ipv6Exthdr uint32) *ofp.OfpOxmOfbField {
	return &ofp.OfpOxmOfbField{Type: IPV6_EXTHDR, Value: &ofp.OfpOxmOfbField_Ipv6Exthdr{Ipv6Exthdr: ipv6Exthdr}}
}

//frequently used extractors

func excludeAction(action *ofp.OfpAction, exclude ...ofp.OfpActionType) bool {
	for _, actionToExclude := range exclude {
		if action.Type == actionToExclude {
			return true
		}
	}
	return false
}

func GetActions(flow *ofp.OfpFlowStats, exclude ...ofp.OfpActionType) []*ofp.OfpAction {
	if flow == nil {
		return nil
	}
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS) {
			instActions := instruction.GetActions()
			if instActions == nil {
				return nil
			}
			if len(exclude) == 0 {
				return instActions.Actions
			} else {
				filteredAction := make([]*ofp.OfpAction, 0)
				for _, action := range instActions.Actions {
					if !excludeAction(action, exclude...) {
						filteredAction = append(filteredAction, action)
					}
				}
				return filteredAction
			}
		}
	}
	return nil
}

func UpdateOutputPortByActionType(flow *ofp.OfpFlowStats, actionType uint32, toPort uint32) *ofp.OfpFlowStats {
	if flow == nil {
		return nil
	}
	nFlow := (proto.Clone(flow)).(*ofp.OfpFlowStats)
	nFlow.Instructions = nil
	nInsts := make([]*ofp.OfpInstruction, 0)
	for _, instruction := range flow.Instructions {
		if instruction.Type == actionType {
			instActions := instruction.GetActions()
			if instActions == nil {
				return nil
			}
			nActions := make([]*ofp.OfpAction, 0)
			for _, action := range instActions.Actions {
				if action.GetOutput() != nil {
					nActions = append(nActions, Output(toPort))
				} else {
					nActions = append(nActions, action)
				}
			}
			instructionAction := ofp.OfpInstruction_Actions{Actions: &ofp.OfpInstructionActions{Actions: nActions}}
			nInsts = append(nInsts, &ofp.OfpInstruction{Type: uint32(APPLY_ACTIONS), Data: &instructionAction})
		} else {
			nInsts = append(nInsts, instruction)
		}
	}
	nFlow.Instructions = nInsts
	return nFlow
}

func excludeOxmOfbField(field *ofp.OfpOxmOfbField, exclude ...ofp.OxmOfbFieldTypes) bool {
	for _, fieldToExclude := range exclude {
		if field.Type == fieldToExclude {
			return true
		}
	}
	return false
}

func GetOfbFields(flow *ofp.OfpFlowStats, exclude ...ofp.OxmOfbFieldTypes) []*ofp.OfpOxmOfbField {
	if flow == nil || flow.Match == nil || flow.Match.Type != ofp.OfpMatchType_OFPMT_OXM {
		return nil
	}
	ofbFields := make([]*ofp.OfpOxmOfbField, 0)
	for _, field := range flow.Match.OxmFields {
		if field.OxmClass == ofp.OfpOxmClass_OFPXMC_OPENFLOW_BASIC {
			ofbFields = append(ofbFields, field.GetOfbField())
		}
	}
	if len(exclude) == 0 {
		return ofbFields
	} else {
		filteredFields := make([]*ofp.OfpOxmOfbField, 0)
		for _, ofbField := range ofbFields {
			if !excludeOxmOfbField(ofbField, exclude...) {
				filteredFields = append(filteredFields, ofbField)
			}
		}
		return filteredFields
	}
}

func GetPacketOutPort(packet *ofp.OfpPacketOut) uint32 {
	if packet == nil {
		return 0
	}
	for _, action := range packet.GetActions() {
		if action.Type == OUTPUT {
			return action.GetOutput().Port
		}
	}
	return 0
}

func GetOutPort(flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, action := range GetActions(flow) {
		if action.Type == OUTPUT {
			out := action.GetOutput()
			if out == nil {
				return 0
			}
			return out.GetPort()
		}
	}
	return 0
}

func GetInPort(flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == IN_PORT {
			return field.GetPort()
		}
	}
	return 0
}

func GetGotoTableId(flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_GOTO_TABLE) {
			gotoTable := instruction.GetGotoTable()
			if gotoTable == nil {
				return 0
			}
			return gotoTable.GetTableId()
		}
	}
	return 0
}

func GetMeterId(flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_METER) {
			MeterInstruction := instruction.GetMeter()
			if MeterInstruction == nil {
				return 0
			}
			return MeterInstruction.GetMeterId()
		}
	}
	return 0
}

func GetVlanVid(flow *ofp.OfpFlowStats) *uint32 {
	if flow == nil {
		return nil
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == VLAN_VID {
			ret := field.GetVlanVid()
			return &ret
		}
	}
	// Dont return 0 if the field is missing as vlan id value 0 has meaning and cannot be overloaded as "not found"
	return nil
}

func GetTunnelId(flow *ofp.OfpFlowStats) uint64 {
	if flow == nil {
		return 0
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == TUNNEL_ID {
			return field.GetTunnelId()
		}
	}
	return 0
}

//GetMetaData - legacy get method (only want lower 32 bits)
func GetMetaData(ctx context.Context, flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == METADATA {
			return uint32(field.GetTableMetadata() & 0xFFFFFFFF)
		}
	}
	logger.Debug(ctx, "No-metadata-present")
	return 0
}

func GetMetaData64Bit(ctx context.Context, flow *ofp.OfpFlowStats) uint64 {
	if flow == nil {
		return 0
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == METADATA {
			return field.GetTableMetadata()
		}
	}
	logger.Debug(ctx, "No-metadata-present")
	return 0
}

// function returns write metadata value from write_metadata action field
func GetMetadataFromWriteMetadataAction(ctx context.Context, flow *ofp.OfpFlowStats) uint64 {
	if flow != nil {
		for _, instruction := range flow.Instructions {
			if instruction.Type == uint32(WRITE_METADATA) {
				if writeMetadata := instruction.GetWriteMetadata(); writeMetadata != nil {
					return writeMetadata.GetMetadata()
				}
			}
		}
	}
	logger.Debugw(ctx, "No-write-metadata-present", log.Fields{"flow": flow})
	return 0
}

func GetTechProfileIDFromWriteMetaData(ctx context.Context, metadata uint64) uint16 {
	/*
	   Write metadata instruction value (metadata) is 8 bytes:
	   MS 2 bytes: C Tag
	   Next 2 bytes: Technology Profile Id
	   Next 4 bytes: Port number (uni or nni)

	   This is set in the ONOS OltPipeline as a write metadata instruction
	*/
	var tpId uint16 = 0
	logger.Debugw(ctx, "Write metadata value for Techprofile ID", log.Fields{"metadata": metadata})
	if metadata != 0 {
		tpId = uint16((metadata >> 32) & 0xFFFF)
		logger.Debugw(ctx, "Found techprofile ID from write metadata action", log.Fields{"tpid": tpId})
	}
	return tpId
}

func GetEgressPortNumberFromWriteMetadata(ctx context.Context, flow *ofp.OfpFlowStats) uint32 {
	/*
			  Write metadata instruction value (metadata) is 8 bytes:
		    	MS 2 bytes: C Tag
		    	Next 2 bytes: Technology Profile Id
		    	Next 4 bytes: Port number (uni or nni)
		    	This is set in the ONOS OltPipeline as a write metadata instruction
	*/
	var uniPort uint32 = 0
	md := GetMetadataFromWriteMetadataAction(ctx, flow)
	logger.Debugw(ctx, "Metadata found for egress/uni port ", log.Fields{"metadata": md})
	if md != 0 {
		uniPort = uint32(md & 0xFFFFFFFF)
		logger.Debugw(ctx, "Found EgressPort from write metadata action", log.Fields{"egress_port": uniPort})
	}
	return uniPort

}

func GetInnerTagFromMetaData(ctx context.Context, flow *ofp.OfpFlowStats) uint16 {
	/*
			  Write metadata instruction value (metadata) is 8 bytes:
		    	MS 2 bytes: C Tag
		    	Next 2 bytes: Technology Profile Id
		    	Next 4 bytes: Port number (uni or nni)
		    	This is set in the ONOS OltPipeline as a write metadata instruction
	*/
	var innerTag uint16 = 0
	md := GetMetadataFromWriteMetadataAction(ctx, flow)
	if md != 0 {
		innerTag = uint16((md >> 48) & 0xFFFF)
		logger.Debugw(ctx, "Found  CVLAN from write metadate action", log.Fields{"c_vlan": innerTag})
	}
	return innerTag
}

//GetInnerTagFromMetaData retrieves the inner tag from the Metadata_ofp. The port number (UNI on ONU) is in the
// lower 32-bits of Metadata_ofp and the inner_tag is in the upper 32-bits. This is set in the ONOS OltPipeline as
//// a Metadata_ofp field
/*func GetInnerTagFromMetaData(flow *ofp.OfpFlowStats) uint64 {
	md := GetMetaData64Bit(flow)
	if md == 0 {
		return 0
	}
	if md <= 0xffffffff {
		logger.Debugw(ctx, "onos-upgrade-suggested", logger.Fields{"Metadata_ofp": md, "message": "Legacy MetaData detected form OltPipeline"})
		return md
	}
	return (md >> 32) & 0xffffffff
}*/

// Extract the child device port from a flow that contains the parent device peer port.  Typically the UNI port of an
// ONU child device.  Per TST agreement this will be the lower 32 bits of tunnel id reserving upper 32 bits for later
// use
func GetChildPortFromTunnelId(flow *ofp.OfpFlowStats) uint32 {
	tid := GetTunnelId(flow)
	if tid == 0 {
		return 0
	}
	// Per TST agreement we are keeping any child port id (uni port id) in the lower 32 bits
	return uint32(tid & 0xffffffff)
}

func HasNextTable(flow *ofp.OfpFlowStats) bool {
	if flow == nil {
		return false
	}
	return GetGotoTableId(flow) != 0
}

func GetGroup(flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, action := range GetActions(flow) {
		if action.Type == GROUP {
			grp := action.GetGroup()
			if grp == nil {
				return 0
			}
			return grp.GetGroupId()
		}
	}
	return 0
}

func HasGroup(flow *ofp.OfpFlowStats) bool {
	return GetGroup(flow) != 0
}

// GetNextTableId returns the next table ID if the "table_id" is present in the map, otherwise return nil
func GetNextTableId(kw OfpFlowModArgs) *uint32 {
	if val, exist := kw["table_id"]; exist {
		ret := uint32(val)
		return &ret
	}
	return nil
}

// GetMeterIdFlowModArgs returns the meterId if the "meter_id" is present in the map, otherwise return 0
func GetMeterIdFlowModArgs(kw OfpFlowModArgs) uint32 {
	if val, exist := kw["meter_id"]; exist {
		return uint32(val)
	}
	return 0
}

// Function returns the metadata if the "write_metadata" is present in the map, otherwise return nil
func GetMetadataFlowModArgs(kw OfpFlowModArgs) uint64 {
	if val, exist := kw["write_metadata"]; exist {
		ret := uint64(val)
		return ret
	}
	return 0
}

// HashFlowStats returns a unique 64-bit integer hash of 'table_id', 'priority', and 'match'
// The OF spec states that:
// A flow table entry is identified by its match fields and priority: the match fields
// and priority taken together identify a unique flow entry in the flow table.
func HashFlowStats(flow *ofp.OfpFlowStats) (uint64, error) {
	// first we need to make sure the oxm fields are in a predictable order (the specific order doesn't matter)
	sort.Slice(flow.Match.OxmFields, func(a, b int) bool {
		fieldsA, fieldsB := flow.Match.OxmFields[a], flow.Match.OxmFields[b]
		if fieldsA.OxmClass < fieldsB.OxmClass {
			return true
		}
		switch fieldA := fieldsA.Field.(type) {
		case *ofp.OfpOxmField_OfbField:
			switch fieldB := fieldsB.Field.(type) {
			case *ofp.OfpOxmField_ExperimenterField:
				return true // ofp < experimenter
			case *ofp.OfpOxmField_OfbField:
				return fieldA.OfbField.Type < fieldB.OfbField.Type
			}
		case *ofp.OfpOxmField_ExperimenterField:
			switch fieldB := fieldsB.Field.(type) {
			case *ofp.OfpOxmField_OfbField:
				return false // ofp < experimenter
			case *ofp.OfpOxmField_ExperimenterField:
				eFieldA, eFieldB := fieldA.ExperimenterField, fieldB.ExperimenterField
				if eFieldA.Experimenter != eFieldB.Experimenter {
					return eFieldA.Experimenter < eFieldB.Experimenter
				}
				return eFieldA.OxmHeader < eFieldB.OxmHeader
			}
		}
		return false
	})

	md5Hash := md5.New() // note that write errors will never occur with md5 hashing
	var tmp [12]byte

	binary.BigEndian.PutUint32(tmp[0:4], flow.TableId)             // tableId
	binary.BigEndian.PutUint32(tmp[4:8], flow.Priority)            // priority
	binary.BigEndian.PutUint32(tmp[8:12], uint32(flow.Match.Type)) // match type
	_, _ = md5Hash.Write(tmp[:12])

	for _, field := range flow.Match.OxmFields { // for all match fields
		binary.BigEndian.PutUint32(tmp[:4], uint32(field.OxmClass)) // match class
		_, _ = md5Hash.Write(tmp[:4])

		switch oxmField := field.Field.(type) {
		case *ofp.OfpOxmField_ExperimenterField:
			binary.BigEndian.PutUint32(tmp[0:4], oxmField.ExperimenterField.Experimenter)
			binary.BigEndian.PutUint32(tmp[4:8], oxmField.ExperimenterField.OxmHeader)
			_, _ = md5Hash.Write(tmp[:8])

		case *ofp.OfpOxmField_OfbField:
			if err := hashWriteOfbField(md5Hash, oxmField.OfbField); err != nil {
				return 0, err
			}

		default:
			return 0, fmt.Errorf("unknown OfpOxmField type: %T", field.Field)
		}
	}

	ret := md5Hash.Sum(nil)
	return binary.BigEndian.Uint64(ret[0:8]), nil
}

func hashWriteOfbField(md5Hash hash.Hash, field *ofp.OfpOxmOfbField) error {
	var tmp [8]byte
	binary.BigEndian.PutUint32(tmp[:4], uint32(field.Type)) // type
	_, _ = md5Hash.Write(tmp[:4])

	// value
	valType, val32, val64, valSlice := uint8(0), uint32(0), uint64(0), []byte(nil)
	switch val := field.Value.(type) {
	case *ofp.OfpOxmOfbField_Port:
		valType, val32 = 4, val.Port
	case *ofp.OfpOxmOfbField_PhysicalPort:
		valType, val32 = 4, val.PhysicalPort
	case *ofp.OfpOxmOfbField_TableMetadata:
		valType, val64 = 8, val.TableMetadata
	case *ofp.OfpOxmOfbField_EthDst:
		valType, valSlice = 1, val.EthDst
	case *ofp.OfpOxmOfbField_EthSrc:
		valType, valSlice = 1, val.EthSrc
	case *ofp.OfpOxmOfbField_EthType:
		valType, val32 = 4, val.EthType
	case *ofp.OfpOxmOfbField_VlanVid:
		valType, val32 = 4, val.VlanVid
	case *ofp.OfpOxmOfbField_VlanPcp:
		valType, val32 = 4, val.VlanPcp
	case *ofp.OfpOxmOfbField_IpDscp:
		valType, val32 = 4, val.IpDscp
	case *ofp.OfpOxmOfbField_IpEcn:
		valType, val32 = 4, val.IpEcn
	case *ofp.OfpOxmOfbField_IpProto:
		valType, val32 = 4, val.IpProto
	case *ofp.OfpOxmOfbField_Ipv4Src:
		valType, val32 = 4, val.Ipv4Src
	case *ofp.OfpOxmOfbField_Ipv4Dst:
		valType, val32 = 4, val.Ipv4Dst
	case *ofp.OfpOxmOfbField_TcpSrc:
		valType, val32 = 4, val.TcpSrc
	case *ofp.OfpOxmOfbField_TcpDst:
		valType, val32 = 4, val.TcpDst
	case *ofp.OfpOxmOfbField_UdpSrc:
		valType, val32 = 4, val.UdpSrc
	case *ofp.OfpOxmOfbField_UdpDst:
		valType, val32 = 4, val.UdpDst
	case *ofp.OfpOxmOfbField_SctpSrc:
		valType, val32 = 4, val.SctpSrc
	case *ofp.OfpOxmOfbField_SctpDst:
		valType, val32 = 4, val.SctpDst
	case *ofp.OfpOxmOfbField_Icmpv4Type:
		valType, val32 = 4, val.Icmpv4Type
	case *ofp.OfpOxmOfbField_Icmpv4Code:
		valType, val32 = 4, val.Icmpv4Code
	case *ofp.OfpOxmOfbField_ArpOp:
		valType, val32 = 4, val.ArpOp
	case *ofp.OfpOxmOfbField_ArpSpa:
		valType, val32 = 4, val.ArpSpa
	case *ofp.OfpOxmOfbField_ArpTpa:
		valType, val32 = 4, val.ArpTpa
	case *ofp.OfpOxmOfbField_ArpSha:
		valType, valSlice = 1, val.ArpSha
	case *ofp.OfpOxmOfbField_ArpTha:
		valType, valSlice = 1, val.ArpTha
	case *ofp.OfpOxmOfbField_Ipv6Src:
		valType, valSlice = 1, val.Ipv6Src
	case *ofp.OfpOxmOfbField_Ipv6Dst:
		valType, valSlice = 1, val.Ipv6Dst
	case *ofp.OfpOxmOfbField_Ipv6Flabel:
		valType, val32 = 4, val.Ipv6Flabel
	case *ofp.OfpOxmOfbField_Icmpv6Type:
		valType, val32 = 4, val.Icmpv6Type
	case *ofp.OfpOxmOfbField_Icmpv6Code:
		valType, val32 = 4, val.Icmpv6Code
	case *ofp.OfpOxmOfbField_Ipv6NdTarget:
		valType, valSlice = 1, val.Ipv6NdTarget
	case *ofp.OfpOxmOfbField_Ipv6NdSsl:
		valType, valSlice = 1, val.Ipv6NdSsl
	case *ofp.OfpOxmOfbField_Ipv6NdTll:
		valType, valSlice = 1, val.Ipv6NdTll
	case *ofp.OfpOxmOfbField_MplsLabel:
		valType, val32 = 4, val.MplsLabel
	case *ofp.OfpOxmOfbField_MplsTc:
		valType, val32 = 4, val.MplsTc
	case *ofp.OfpOxmOfbField_MplsBos:
		valType, val32 = 4, val.MplsBos
	case *ofp.OfpOxmOfbField_PbbIsid:
		valType, val32 = 4, val.PbbIsid
	case *ofp.OfpOxmOfbField_TunnelId:
		valType, val64 = 8, val.TunnelId
	case *ofp.OfpOxmOfbField_Ipv6Exthdr:
		valType, val32 = 4, val.Ipv6Exthdr
	default:
		return fmt.Errorf("unknown OfpOxmField value type: %T", val)
	}
	switch valType {
	case 1: // slice
		_, _ = md5Hash.Write(valSlice)
	case 4: // uint32
		binary.BigEndian.PutUint32(tmp[:4], val32)
		_, _ = md5Hash.Write(tmp[:4])
	case 8: // uint64
		binary.BigEndian.PutUint64(tmp[:8], val64)
		_, _ = md5Hash.Write(tmp[:8])
	}

	// mask
	if !field.HasMask {
		tmp[0] = 0x00
		_, _ = md5Hash.Write(tmp[:1]) // match hasMask = false
	} else {
		tmp[0] = 0x01
		_, _ = md5Hash.Write(tmp[:1]) // match hasMask = true

		maskType, mask32, mask64, maskSlice := uint8(0), uint32(0), uint64(0), []byte(nil)
		switch mask := field.Mask.(type) {
		case *ofp.OfpOxmOfbField_TableMetadataMask:
			maskType, mask64 = 8, mask.TableMetadataMask
		case *ofp.OfpOxmOfbField_EthDstMask:
			maskType, maskSlice = 1, mask.EthDstMask
		case *ofp.OfpOxmOfbField_EthSrcMask:
			maskType, maskSlice = 1, mask.EthSrcMask
		case *ofp.OfpOxmOfbField_VlanVidMask:
			maskType, mask32 = 4, mask.VlanVidMask
		case *ofp.OfpOxmOfbField_Ipv4SrcMask:
			maskType, mask32 = 4, mask.Ipv4SrcMask
		case *ofp.OfpOxmOfbField_Ipv4DstMask:
			maskType, mask32 = 4, mask.Ipv4DstMask
		case *ofp.OfpOxmOfbField_ArpSpaMask:
			maskType, mask32 = 4, mask.ArpSpaMask
		case *ofp.OfpOxmOfbField_ArpTpaMask:
			maskType, mask32 = 4, mask.ArpTpaMask
		case *ofp.OfpOxmOfbField_Ipv6SrcMask:
			maskType, maskSlice = 1, mask.Ipv6SrcMask
		case *ofp.OfpOxmOfbField_Ipv6DstMask:
			maskType, maskSlice = 1, mask.Ipv6DstMask
		case *ofp.OfpOxmOfbField_Ipv6FlabelMask:
			maskType, mask32 = 4, mask.Ipv6FlabelMask
		case *ofp.OfpOxmOfbField_PbbIsidMask:
			maskType, mask32 = 4, mask.PbbIsidMask
		case *ofp.OfpOxmOfbField_TunnelIdMask:
			maskType, mask64 = 8, mask.TunnelIdMask
		case *ofp.OfpOxmOfbField_Ipv6ExthdrMask:
			maskType, mask32 = 4, mask.Ipv6ExthdrMask
		case nil:
			return fmt.Errorf("hasMask set to true, but no mask present")
		default:
			return fmt.Errorf("unknown OfpOxmField mask type: %T", mask)
		}
		switch maskType {
		case 1: // slice
			_, _ = md5Hash.Write(maskSlice)
		case 4: // uint32
			binary.BigEndian.PutUint32(tmp[:4], mask32)
			_, _ = md5Hash.Write(tmp[:4])
		case 8: // uint64
			binary.BigEndian.PutUint64(tmp[:8], mask64)
			_, _ = md5Hash.Write(tmp[:8])
		}
	}
	return nil
}

// flowStatsEntryFromFlowModMessage maps an ofp_flow_mod message to an ofp_flow_stats message
func FlowStatsEntryFromFlowModMessage(mod *ofp.OfpFlowMod) (*ofp.OfpFlowStats, error) {
	flow := &ofp.OfpFlowStats{}
	if mod == nil {
		return flow, nil
	}
	flow.TableId = mod.TableId
	flow.Priority = mod.Priority
	flow.IdleTimeout = mod.IdleTimeout
	flow.HardTimeout = mod.HardTimeout
	flow.Flags = mod.Flags
	flow.Cookie = mod.Cookie
	flow.Match = mod.Match
	flow.Instructions = mod.Instructions
	var err error
	if flow.Id, err = HashFlowStats(flow); err != nil {
		return nil, err
	}

	return flow, nil
}

func GroupEntryFromGroupMod(mod *ofp.OfpGroupMod) *ofp.OfpGroupEntry {
	group := &ofp.OfpGroupEntry{}
	if mod == nil {
		return group
	}
	group.Desc = &ofp.OfpGroupDesc{Type: mod.Type, GroupId: mod.GroupId, Buckets: mod.Buckets}
	group.Stats = &ofp.OfpGroupStats{GroupId: mod.GroupId}
	//TODO do we need to instantiate bucket bins?
	return group
}

// flowStatsEntryFromFlowModMessage maps an ofp_flow_mod message to an ofp_flow_stats message
func MeterEntryFromMeterMod(ctx context.Context, meterMod *ofp.OfpMeterMod) *ofp.OfpMeterEntry {
	bandStats := make([]*ofp.OfpMeterBandStats, 0)
	meter := &ofp.OfpMeterEntry{Config: &ofp.OfpMeterConfig{},
		Stats: &ofp.OfpMeterStats{BandStats: bandStats}}
	if meterMod == nil {
		logger.Error(ctx, "Invalid meter mod command")
		return meter
	}
	// config init
	meter.Config.MeterId = meterMod.MeterId
	meter.Config.Flags = meterMod.Flags
	meter.Config.Bands = meterMod.Bands
	// meter stats init
	meter.Stats.MeterId = meterMod.MeterId
	meter.Stats.FlowCount = 0
	meter.Stats.PacketInCount = 0
	meter.Stats.ByteInCount = 0
	meter.Stats.DurationSec = 0
	meter.Stats.DurationNsec = 0
	// band stats init
	for range meterMod.Bands {
		band := &ofp.OfpMeterBandStats{}
		band.PacketBandCount = 0
		band.ByteBandCount = 0
		bandStats = append(bandStats, band)
	}
	meter.Stats.BandStats = bandStats
	logger.Debugw(ctx, "Allocated meter entry", log.Fields{"meter": *meter})
	return meter

}

func GetMeterIdFromFlow(flow *ofp.OfpFlowStats) uint32 {
	if flow != nil {
		for _, instruction := range flow.Instructions {
			if instruction.Type == uint32(METER_ACTION) {
				if meterInst := instruction.GetMeter(); meterInst != nil {
					return meterInst.GetMeterId()
				}
			}
		}
	}

	return uint32(0)
}

func MkOxmFields(matchFields []ofp.OfpOxmField) []*ofp.OfpOxmField {
	oxmFields := make([]*ofp.OfpOxmField, 0)
	for _, matchField := range matchFields {
		oxmField := ofp.OfpOxmField{OxmClass: ofp.OfpOxmClass_OFPXMC_OPENFLOW_BASIC, Field: matchField.Field}
		oxmFields = append(oxmFields, &oxmField)
	}
	return oxmFields
}

func MkInstructionsFromActions(actions []*ofp.OfpAction) []*ofp.OfpInstruction {
	instructions := make([]*ofp.OfpInstruction, 0)
	instructionAction := ofp.OfpInstruction_Actions{Actions: &ofp.OfpInstructionActions{Actions: actions}}
	instruction := ofp.OfpInstruction{Type: uint32(APPLY_ACTIONS), Data: &instructionAction}
	instructions = append(instructions, &instruction)
	return instructions
}

// Convenience function to generare ofp_flow_mod message with OXM BASIC match composed from the match_fields, and
// single APPLY_ACTIONS instruction with a list if ofp_action objects.
func MkSimpleFlowMod(matchFields []*ofp.OfpOxmField, actions []*ofp.OfpAction, command *ofp.OfpFlowModCommand, kw OfpFlowModArgs) *ofp.OfpFlowMod {

	// Process actions instructions
	instructions := make([]*ofp.OfpInstruction, 0)
	instructionAction := ofp.OfpInstruction_Actions{Actions: &ofp.OfpInstructionActions{Actions: actions}}
	instruction := ofp.OfpInstruction{Type: uint32(APPLY_ACTIONS), Data: &instructionAction}
	instructions = append(instructions, &instruction)

	// Process next table
	if tableId := GetNextTableId(kw); tableId != nil {
		var instGotoTable ofp.OfpInstruction_GotoTable
		instGotoTable.GotoTable = &ofp.OfpInstructionGotoTable{TableId: *tableId}
		inst := ofp.OfpInstruction{Type: uint32(ofp.OfpInstructionType_OFPIT_GOTO_TABLE), Data: &instGotoTable}
		instructions = append(instructions, &inst)
	}
	// Process meter action
	if meterId := GetMeterIdFlowModArgs(kw); meterId != 0 {
		var instMeter ofp.OfpInstruction_Meter
		instMeter.Meter = &ofp.OfpInstructionMeter{MeterId: meterId}
		inst := ofp.OfpInstruction{Type: uint32(METER_ACTION), Data: &instMeter}
		instructions = append(instructions, &inst)
	}
	//process write_metadata action
	if metadata := GetMetadataFlowModArgs(kw); metadata != 0 {
		var instWriteMetadata ofp.OfpInstruction_WriteMetadata
		instWriteMetadata.WriteMetadata = &ofp.OfpInstructionWriteMetadata{Metadata: metadata}
		inst := ofp.OfpInstruction{Type: uint32(WRITE_METADATA), Data: &instWriteMetadata}
		instructions = append(instructions, &inst)
	}

	// Process match fields
	oxmFields := make([]*ofp.OfpOxmField, 0)
	for _, matchField := range matchFields {
		oxmField := ofp.OfpOxmField{OxmClass: ofp.OfpOxmClass_OFPXMC_OPENFLOW_BASIC, Field: matchField.Field}
		oxmFields = append(oxmFields, &oxmField)
	}
	var match ofp.OfpMatch
	match.Type = ofp.OfpMatchType_OFPMT_OXM
	match.OxmFields = oxmFields

	// Create ofp_flow_message
	msg := &ofp.OfpFlowMod{}
	if command == nil {
		msg.Command = ofp.OfpFlowModCommand_OFPFC_ADD
	} else {
		msg.Command = *command
	}
	msg.Instructions = instructions
	msg.Match = &match

	// Set the variadic argument values
	msg = setVariadicModAttributes(msg, kw)

	return msg
}

func MkMulticastGroupMod(groupId uint32, buckets []*ofp.OfpBucket, command *ofp.OfpGroupModCommand) *ofp.OfpGroupMod {
	group := &ofp.OfpGroupMod{}
	if command == nil {
		group.Command = ofp.OfpGroupModCommand_OFPGC_ADD
	} else {
		group.Command = *command
	}
	group.Type = ofp.OfpGroupType_OFPGT_ALL
	group.GroupId = groupId
	group.Buckets = buckets
	return group
}

//SetVariadicModAttributes sets only uint64 or uint32 fields of the ofp_flow_mod message
func setVariadicModAttributes(mod *ofp.OfpFlowMod, args OfpFlowModArgs) *ofp.OfpFlowMod {
	if args == nil {
		return mod
	}
	for key, val := range args {
		switch key {
		case "cookie":
			mod.Cookie = val
		case "cookie_mask":
			mod.CookieMask = val
		case "table_id":
			mod.TableId = uint32(val)
		case "idle_timeout":
			mod.IdleTimeout = uint32(val)
		case "hard_timeout":
			mod.HardTimeout = uint32(val)
		case "priority":
			mod.Priority = uint32(val)
		case "buffer_id":
			mod.BufferId = uint32(val)
		case "out_port":
			mod.OutPort = uint32(val)
		case "out_group":
			mod.OutGroup = uint32(val)
		case "flags":
			mod.Flags = uint32(val)
		}
	}
	return mod
}

func MkPacketIn(port uint32, packet []byte) *ofp.OfpPacketIn {
	packetIn := &ofp.OfpPacketIn{
		Reason: ofp.OfpPacketInReason_OFPR_ACTION,
		Match: &ofp.OfpMatch{
			Type: ofp.OfpMatchType_OFPMT_OXM,
			OxmFields: []*ofp.OfpOxmField{
				{
					OxmClass: ofp.OfpOxmClass_OFPXMC_OPENFLOW_BASIC,
					Field: &ofp.OfpOxmField_OfbField{
						OfbField: InPort(port)},
				},
			},
		},
		Data: packet,
	}
	return packetIn
}

// MkFlowStat is a helper method to build flows
func MkFlowStat(fa *FlowArgs) (*ofp.OfpFlowStats, error) {
	//Build the match-fields
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return FlowStatsEntryFromFlowModMessage(MkSimpleFlowMod(matchFields, fa.Actions, fa.Command, fa.KV))
}

func MkGroupStat(ga *GroupArgs) *ofp.OfpGroupEntry {
	return GroupEntryFromGroupMod(MkMulticastGroupMod(ga.GroupId, ga.Buckets, ga.Command))
}

type OfpFlowModArgs map[string]uint64

type FlowArgs struct {
	MatchFields []*ofp.OfpOxmOfbField
	Actions     []*ofp.OfpAction
	Command     *ofp.OfpFlowModCommand
	Priority    uint32
	KV          OfpFlowModArgs
}

type GroupArgs struct {
	GroupId uint32
	Buckets []*ofp.OfpBucket
	Command *ofp.OfpGroupModCommand
}

type FlowsAndGroups struct {
	Flows  *ordered_map.OrderedMap
	Groups *ordered_map.OrderedMap
}

func NewFlowsAndGroups() *FlowsAndGroups {
	var fg FlowsAndGroups
	fg.Flows = ordered_map.NewOrderedMap()
	fg.Groups = ordered_map.NewOrderedMap()
	return &fg
}

func (fg *FlowsAndGroups) Copy() *FlowsAndGroups {
	copyFG := NewFlowsAndGroups()
	iter := fg.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			copyFG.Flows.Set(kv.Key, proto.Clone(protoMsg))
		}
	}
	iter = fg.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			copyFG.Groups.Set(kv.Key, proto.Clone(protoMsg))
		}
	}
	return copyFG
}

func (fg *FlowsAndGroups) GetFlow(index int) *ofp.OfpFlowStats {
	iter := fg.Flows.IterFunc()
	pos := 0
	for kv, ok := iter(); ok; kv, ok = iter() {
		if pos == index {
			if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
				return protoMsg
			}
			return nil
		}
		pos += 1
	}
	return nil
}

func (fg *FlowsAndGroups) ListFlows() []*ofp.OfpFlowStats {
	flows := make([]*ofp.OfpFlowStats, 0)
	iter := fg.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			flows = append(flows, protoMsg)
		}
	}
	return flows
}

func (fg *FlowsAndGroups) ListGroups() []*ofp.OfpGroupEntry {
	groups := make([]*ofp.OfpGroupEntry, 0)
	iter := fg.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			groups = append(groups, protoMsg)
		}
	}
	return groups
}

func (fg *FlowsAndGroups) String() string {
	var buffer bytes.Buffer
	iter := fg.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			buffer.WriteString("\nFlow:\n")
			buffer.WriteString(proto.MarshalTextString(protoMsg))
			buffer.WriteString("\n")
		}
	}
	iter = fg.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			buffer.WriteString("\nGroup:\n")
			buffer.WriteString(proto.MarshalTextString(protoMsg))
			buffer.WriteString("\n")
		}
	}
	return buffer.String()
}

func (fg *FlowsAndGroups) AddFlow(flow *ofp.OfpFlowStats) {
	if flow == nil {
		return
	}

	if fg.Flows == nil {
		fg.Flows = ordered_map.NewOrderedMap()
	}
	if fg.Groups == nil {
		fg.Groups = ordered_map.NewOrderedMap()
	}
	//Add flow only if absent
	if _, exist := fg.Flows.Get(flow.Id); !exist {
		fg.Flows.Set(flow.Id, flow)
	}
}

func (fg *FlowsAndGroups) AddGroup(group *ofp.OfpGroupEntry) {
	if group == nil {
		return
	}

	if fg.Flows == nil {
		fg.Flows = ordered_map.NewOrderedMap()
	}
	if fg.Groups == nil {
		fg.Groups = ordered_map.NewOrderedMap()
	}
	//Add group only if absent
	if _, exist := fg.Groups.Get(group.Desc.GroupId); !exist {
		fg.Groups.Set(group.Desc.GroupId, group)
	}
}

//AddFrom add flows and groups from the argument into this structure only if they do not already exist
func (fg *FlowsAndGroups) AddFrom(from *FlowsAndGroups) {
	iter := from.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			if _, exist := fg.Flows.Get(protoMsg.Id); !exist {
				fg.Flows.Set(protoMsg.Id, protoMsg)
			}
		}
	}
	iter = from.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			if _, exist := fg.Groups.Get(protoMsg.Stats.GroupId); !exist {
				fg.Groups.Set(protoMsg.Stats.GroupId, protoMsg)
			}
		}
	}
}

type DeviceRules struct {
	Rules map[string]*FlowsAndGroups
}

func NewDeviceRules() *DeviceRules {
	var dr DeviceRules
	dr.Rules = make(map[string]*FlowsAndGroups)
	return &dr
}

func (dr *DeviceRules) Copy() *DeviceRules {
	copyDR := NewDeviceRules()
	if dr != nil {
		for key, val := range dr.Rules {
			if val != nil {
				copyDR.Rules[key] = val.Copy()
			}
		}
	}
	return copyDR
}

func (dr *DeviceRules) ClearFlows(deviceId string) {
	if _, exist := dr.Rules[deviceId]; exist {
		dr.Rules[deviceId].Flows = ordered_map.NewOrderedMap()
	}
}

func (dr *DeviceRules) FilterRules(deviceIds map[string]string) *DeviceRules {
	filteredDR := NewDeviceRules()
	for key, val := range dr.Rules {
		if _, exist := deviceIds[key]; exist {
			filteredDR.Rules[key] = val.Copy()
		}
	}
	return filteredDR
}

func (dr *DeviceRules) AddFlow(deviceId string, flow *ofp.OfpFlowStats) {
	if _, exist := dr.Rules[deviceId]; !exist {
		dr.Rules[deviceId] = NewFlowsAndGroups()
	}
	dr.Rules[deviceId].AddFlow(flow)
}

func (dr *DeviceRules) GetRules() map[string]*FlowsAndGroups {
	return dr.Rules
}

func (dr *DeviceRules) String() string {
	var buffer bytes.Buffer
	for key, value := range dr.Rules {
		buffer.WriteString("DeviceId:")
		buffer.WriteString(key)
		buffer.WriteString(value.String())
		buffer.WriteString("\n\n")
	}
	return buffer.String()
}

func (dr *DeviceRules) AddFlowsAndGroup(deviceId string, fg *FlowsAndGroups) {
	if _, ok := dr.Rules[deviceId]; !ok {
		dr.Rules[deviceId] = NewFlowsAndGroups()
	}
	dr.Rules[deviceId] = fg
}

// CreateEntryIfNotExist creates a new deviceId in the Map if it does not exist and assigns an
// empty FlowsAndGroups to it.  Otherwise, it does nothing.
func (dr *DeviceRules) CreateEntryIfNotExist(deviceId string) {
	if _, ok := dr.Rules[deviceId]; !ok {
		dr.Rules[deviceId] = NewFlowsAndGroups()
	}
}

/*
 *  Common flow routines
 */

//FindOverlappingFlows return a list of overlapping flow(s) where mod is the flow request
func FindOverlappingFlows(flows []*ofp.OfpFlowStats, mod *ofp.OfpFlowMod) []*ofp.OfpFlowStats {
	return nil //TODO - complete implementation
}

// FindFlowById returns the index of the flow in the flows array if present. Otherwise, it returns -1
func FindFlowById(flows []*ofp.OfpFlowStats, flow *ofp.OfpFlowStats) int {
	for idx, f := range flows {
		if flow.Id == f.Id {
			return idx
		}
	}
	return -1
}

// FindFlows returns the index in flows where flow if present.  Otherwise, it returns -1
func FindFlows(flows []*ofp.OfpFlowStats, flow *ofp.OfpFlowStats) int {
	for idx, f := range flows {
		if f.Id == flow.Id {
			return idx
		}
	}
	return -1
}

//FlowMatch returns true if two flows matches on the following flow attributes:
//TableId, Priority, Flags, Cookie, Match
func FlowMatch(f1 *ofp.OfpFlowStats, f2 *ofp.OfpFlowStats) bool {
	return f1 != nil && f2 != nil && f1.Id == f2.Id
}

//FlowMatchesMod returns True if given flow is "covered" by the wildcard flow_mod, taking into consideration of
//both exact matches as well as masks-based match fields if any. Otherwise return False
func FlowMatchesMod(flow *ofp.OfpFlowStats, mod *ofp.OfpFlowMod) bool {
	if flow == nil || mod == nil {
		return false
	}
	//Check if flow.cookie is covered by mod.cookie and mod.cookie_mask
	if (flow.Cookie & mod.CookieMask) != (mod.Cookie & mod.CookieMask) {
		return false
	}

	//Check if flow.table_id is covered by flow_mod.table_id
	if mod.TableId != uint32(ofp.OfpTable_OFPTT_ALL) && flow.TableId != mod.TableId {
		return false
	}

	//Check out_port
	if (mod.OutPort&0x7fffffff) != uint32(ofp.OfpPortNo_OFPP_ANY) && !FlowHasOutPort(flow, mod.OutPort) {
		return false
	}

	//	Check out_group
	if (mod.OutGroup&0x7fffffff) != uint32(ofp.OfpGroup_OFPG_ANY) && !FlowHasOutGroup(flow, mod.OutGroup) {
		return false
	}

	//Priority is ignored

	//Check match condition
	//If the flow_mod match field is empty, that is a special case and indicates the flow entry matches
	if (mod.Match == nil) || (mod.Match.OxmFields == nil) || (len(mod.Match.OxmFields) == 0) {
		//If we got this far and the match is empty in the flow spec, than the flow matches
		return true
	} // TODO : implement the flow match analysis
	return false

}

//FlowHasOutPort returns True if flow has a output command with the given out_port
func FlowHasOutPort(flow *ofp.OfpFlowStats, outPort uint32) bool {
	if flow == nil {
		return false
	}
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS) {
			if instruction.GetActions() == nil {
				return false
			}
			for _, action := range instruction.GetActions().Actions {
				if action.Type == ofp.OfpActionType_OFPAT_OUTPUT {
					if (action.GetOutput() != nil) && (action.GetOutput().Port == outPort) {
						return true
					}
				}

			}
		}
	}
	return false
}

//FlowHasOutGroup return True if flow has a output command with the given out_group
func FlowHasOutGroup(flow *ofp.OfpFlowStats, groupID uint32) bool {
	if flow == nil {
		return false
	}
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS) {
			if instruction.GetActions() == nil {
				return false
			}
			for _, action := range instruction.GetActions().Actions {
				if action.Type == ofp.OfpActionType_OFPAT_GROUP {
					if (action.GetGroup() != nil) && (action.GetGroup().GroupId == groupID) {
						return true
					}
				}

			}
		}
	}
	return false
}

//FindGroup returns index of group if found, else returns -1
func FindGroup(groups []*ofp.OfpGroupEntry, groupId uint32) int {
	for idx, group := range groups {
		if group.Desc.GroupId == groupId {
			return idx
		}
	}
	return -1
}

func FlowsDeleteByGroupId(flows []*ofp.OfpFlowStats, groupId uint32) (bool, []*ofp.OfpFlowStats) {
	toKeep := make([]*ofp.OfpFlowStats, 0)

	for _, f := range flows {
		if !FlowHasOutGroup(f, groupId) {
			toKeep = append(toKeep, f)
		}
	}
	return len(toKeep) < len(flows), toKeep
}

func ToOfpOxmField(from []*ofp.OfpOxmOfbField) []*ofp.OfpOxmField {
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range from {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return matchFields
}

//IsMulticastIp returns true if the ip starts with the byte sequence of 1110;
//false otherwise.
func IsMulticastIp(ip uint32) bool {
	return ip>>28 == 14
}

//ConvertToMulticastMacInt returns equivalent mac address of the given multicast ip address
func ConvertToMulticastMacInt(ip uint32) uint64 {
	//get last 23 bits of ip address by ip & 00000000011111111111111111111111
	theLast23BitsOfIp := ip & 8388607
	// perform OR with 0x1005E000000 to build mcast mac address
	return 1101088686080 | uint64(theLast23BitsOfIp)
}

//ConvertToMulticastMacBytes returns equivalent mac address of the given multicast ip address
func ConvertToMulticastMacBytes(ip uint32) []byte {
	mac := ConvertToMulticastMacInt(ip)
	var b bytes.Buffer
	// catalyze (48 bits) in binary:111111110000000000000000000000000000000000000000
	catalyze := uint64(280375465082880)
	//convert each octet to decimal
	for i := 0; i < 6; i++ {
		if i != 0 {
			catalyze = catalyze >> 8
		}
		octet := mac & catalyze
		octetDecimal := octet >> uint8(40-i*8)
		b.WriteByte(byte(octetDecimal))
	}
	return b.Bytes()
}
