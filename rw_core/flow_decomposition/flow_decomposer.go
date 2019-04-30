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
	"bytes"
	"crypto/md5"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/rw_core/coreIf"
	"github.com/opencord/voltha-go/rw_core/graph"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"math/big"
)

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

var (
	// Instructions shortcut
	APPLY_ACTIONS = ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS

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
func GetMetaData(flow *ofp.OfpFlowStats) uint32 {
	if flow == nil {
		return 0
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == METADATA {
			return uint32(field.GetTableMetadata() & 0xffffffff)
		}
	}
	return 0
}

func GetMetaData64Bit(flow *ofp.OfpFlowStats) uint64 {
	if flow == nil {
		return 0
	}
	for _, field := range GetOfbFields(flow) {
		if field.Type == METADATA {
			return field.GetTableMetadata()
		}
	}
	return 0
}

// GetPortNumberFromMetadata retrieves the port number from the Metadata_ofp. The port number (UNI on ONU) is in the
// lower 32-bits of Metadata_ofp and the inner_tag is in the upper 32-bits. This is set in the ONOS OltPipeline as
// a Metadata_ofp field
func GetPortNumberFromMetadata(flow *ofp.OfpFlowStats) uint64 {
	md := GetMetaData64Bit(flow)
	if md == 0 {
		return 0
	}
	if md <= 0xffffffff {
		log.Debugw("onos-upgrade-suggested", log.Fields{"Metadata_ofp": md, "message": "Legacy MetaData detected form OltPipeline"})
		return md
	}
	return md & 0xffffffff
}

//GetInnerTagFromMetaData retrieves the inner tag from the Metadata_ofp. The port number (UNI on ONU) is in the
// lower 32-bits of Metadata_ofp and the inner_tag is in the upper 32-bits. This is set in the ONOS OltPipeline as
//// a Metadata_ofp field
func GetInnerTagFromMetaData(flow *ofp.OfpFlowStats) uint64 {
	md := GetMetaData64Bit(flow)
	if md == 0 {
		return 0
	}
	if md <= 0xffffffff {
		log.Debugw("onos-upgrade-suggested", log.Fields{"Metadata_ofp": md, "message": "Legacy MetaData detected form OltPipeline"})
		return md
	}
	return (md >> 32) & 0xffffffff
}

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
func GetNextTableId(kw fu.OfpFlowModArgs) *uint32 {
	if val, exist := kw["table_id"]; exist {
		ret := uint32(val)
		return &ret
	}
	return nil
}

// Return unique 64-bit integer hash for flow covering the following attributes:
// 'table_id', 'priority', 'flags', 'cookie', 'match', '_instruction_string'
func hashFlowStats(flow *ofp.OfpFlowStats) uint64 {
	if flow == nil { // Should never happen
		return 0
	}
	// Create string with the instructions field first
	var instructionString bytes.Buffer
	for _, instruction := range flow.Instructions {
		instructionString.WriteString(instruction.String())
	}
	var flowString = fmt.Sprintf("%d%d%d%d%s%s", flow.TableId, flow.Priority, flow.Flags, flow.Cookie, flow.Match.String(), instructionString.String())
	h := md5.New()
	h.Write([]byte(flowString))
	hash := big.NewInt(0)
	hash.SetBytes(h.Sum(nil))
	return hash.Uint64()
}

// flowStatsEntryFromFlowModMessage maps an ofp_flow_mod message to an ofp_flow_stats message
func FlowStatsEntryFromFlowModMessage(mod *ofp.OfpFlowMod) *ofp.OfpFlowStats {
	flow := &ofp.OfpFlowStats{}
	if mod == nil {
		return flow
	}
	flow.TableId = mod.TableId
	flow.Priority = mod.Priority
	flow.IdleTimeout = mod.IdleTimeout
	flow.HardTimeout = mod.HardTimeout
	flow.Flags = mod.Flags
	flow.Cookie = mod.Cookie
	flow.Match = mod.Match
	flow.Instructions = mod.Instructions
	flow.Id = hashFlowStats(flow)
	return flow
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
func MkSimpleFlowMod(matchFields []*ofp.OfpOxmField, actions []*ofp.OfpAction, command *ofp.OfpFlowModCommand, kw fu.OfpFlowModArgs) *ofp.OfpFlowMod {

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
func setVariadicModAttributes(mod *ofp.OfpFlowMod, args fu.OfpFlowModArgs) *ofp.OfpFlowMod {
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
func MkFlowStat(fa *fu.FlowArgs) *ofp.OfpFlowStats {
	//Build the matchfields
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return FlowStatsEntryFromFlowModMessage(MkSimpleFlowMod(matchFields, fa.Actions, fa.Command, fa.KV))
}

func MkGroupStat(ga *fu.GroupArgs) *ofp.OfpGroupEntry {
	return GroupEntryFromGroupMod(MkMulticastGroupMod(ga.GroupId, ga.Buckets, ga.Command))
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
func (fd *FlowDecomposer) DecomposeRules(agent coreIf.LogicalDeviceAgent, flows ofp.Flows, groups ofp.FlowGroups, includeDefaultFlows bool) *fu.DeviceRules {
	rules := agent.GetAllDefaultRules()
	deviceRules := rules.Copy()
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
	if includeDefaultFlows {
		return deviceRules
	}
	updatedDeviceRules := deviceRules.FilterRules(devicesToUpdate)

	return updatedDeviceRules
}

// Handles special case of any controller-bound flow for a parent device
func (fd *FlowDecomposer) updateOutputPortForControllerBoundFlowForParentDevide(flow *ofp.OfpFlowStats,
	dr *fu.DeviceRules) *fu.DeviceRules {
	EAPOL := EthType(0x888e)
	IGMP := IpProto(2)
	UDP := IpProto(17)

	newDeviceRules := dr.Copy()
	//	Check whether we are dealing with a parent device
	for deviceId, fg := range dr.GetRules() {
		if root, _ := fd.deviceMgr.IsRootDevice(deviceId); root {
			newDeviceRules.ClearFlows(deviceId)
			for i := 0; i < fg.Flows.Len(); i++ {
				f := fg.GetFlow(i)
				UpdateOutPortNo := false
				for _, field := range GetOfbFields(f) {
					UpdateOutPortNo = (field.String() == EAPOL.String())
					UpdateOutPortNo = UpdateOutPortNo || (field.String() == IGMP.String())
					UpdateOutPortNo = UpdateOutPortNo || (field.String() == UDP.String())
					if UpdateOutPortNo {
						break
					}
				}
				if UpdateOutPortNo {
					f = UpdateOutputPortByActionType(f, uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS),
						uint32(ofp.OfpPortNo_OFPP_CONTROLLER))
				}
				// Update flow Id as a change in the instruction field will result in a new flow ID
				f.Id = hashFlowStats(f)
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
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
				MatchFields: []*ofp.OfpOxmOfbField{
					InPort(egressHop.Ingress),
					VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | inputPort),
					TunnelId(uint64(inputPort)),
				},
				Actions: []*ofp.OfpAction{
					PushVlan(0x8100),
					SetField(VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000)),
					Output(egressHop.Egress),
				},
			}
			// Augment the matchfields with the ofpfields from the flow
			fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT, VLAN_VID)...)
			fg.AddFlow(MkFlowStat(fa))

			// Downstream flow
			fa = &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority)},
				MatchFields: []*ofp.OfpOxmOfbField{
					InPort(egressHop.Egress),
					VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000),
					VlanPcp(0),
					Metadata_ofp(uint64(inputPort)),
					TunnelId(uint64(inputPort)),
				},
				Actions: []*ofp.OfpAction{
					PopVlan(),
					Output(egressHop.Ingress),
				},
			}
			fg.AddFlow(MkFlowStat(fa))
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

	ingressHop := route[0]
	egressHop := route[1]

	if HasNextTable(flow) {
		log.Debugw("has-next-table", log.Fields{"table_id": flow.TableId})
		if outPortNo != 0 {
			log.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
		}
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(ingressHop.Ingress),
				TunnelId(uint64(inPortNo)),
			},
			Actions: GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, Output(ingressHop.Egress))

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)
	} else {
		var actions []ofp.OfpActionType
		var isOutputTypeInActions bool
		for _, action := range GetActions(flow) {
			actions = append(actions, action.Type)
			if !isOutputTypeInActions && action.Type == OUTPUT {
				isOutputTypeInActions = true
			}
		}
		if len(actions) == 1 && isOutputTypeInActions {
			var fa *fu.FlowArgs
			// child device flow
			fa = &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
				MatchFields: []*ofp.OfpOxmOfbField{
					InPort(ingressHop.Ingress),
				},
				Actions: []*ofp.OfpAction{
					Output(ingressHop.Egress),
				},
			}
			// Augment the matchfields with the ofpfields from the flow
			fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)
			fg := fu.NewFlowsAndGroups()
			fg.AddFlow(MkFlowStat(fa))
			deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)

			// parent device flow
			fa = &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
				MatchFields: []*ofp.OfpOxmOfbField{
					InPort(egressHop.Ingress), //egress_hop.ingress_port.port_no
					TunnelId(uint64(inPortNo)),
				},
				Actions: []*ofp.OfpAction{
					Output(egressHop.Egress),
				},
			}
			// Augment the matchfields with the ofpfields from the flow
			fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)
			fg = fu.NewFlowsAndGroups()
			fg.AddFlow(MkFlowStat(fa))
			deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
		} else {
			if outPortNo == 0 {
				log.Warnw("outPort-should-be-specified", log.Fields{"outPortNo": outPortNo})
			}
			var fa *fu.FlowArgs
			fa = &fu.FlowArgs{
				KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
				MatchFields: []*ofp.OfpOxmOfbField{
					InPort(egressHop.Ingress),
					TunnelId(uint64(inPortNo)),
				},
			}
			// Augment the matchfields with the ofpfields from the flow
			fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

			//Augment the actions
			filteredAction := GetActions(flow, OUTPUT)
			filteredAction = append(filteredAction, Output(egressHop.Egress))
			fa.Actions = filteredAction

			fg := fu.NewFlowsAndGroups()
			fg.AddFlow(MkFlowStat(fa))
			deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
		}
	}
	return deviceRules
}

// processDownstreamFlowWithNextTable decomposes downstream flows containing next table ID instructions
func (fd *FlowDecomposer) processDownstreamFlowWithNextTable(agent coreIf.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("downstream-flow-with-next-table", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	if outPortNo != 0 {
		log.Warnw("outPort-should-not-be-specified", log.Fields{"outPortNo": outPortNo})
	}
	ingressHop := route[0]
	egressHop := route[1]

	if GetMetaData(flow) != 0 {
		log.Debugw("creating-metadata-flow", log.Fields{"flow": flow})
		portNumber := uint32(GetPortNumberFromMetadata(flow))
		if portNumber != 0 {
			recalculatedRoute := agent.GetRoute(inPortNo, portNumber)
			switch len(recalculatedRoute) {
			case 0:
				log.Errorw("no-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": portNumber, "comment": "deleting-flow", "metadata": GetMetaData64Bit(flow)})
				//	TODO: Delete flow
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
		innerTag := GetInnerTagFromMetaData(flow)
		if innerTag == 0 {
			log.Errorw("no-inner-route-double-tag", log.Fields{"inPortNo": inPortNo, "outPortNo": portNumber, "comment": "deleting-flow", "metadata": GetMetaData64Bit(flow)})
			//	TODO: Delete flow
			return deviceRules
		}
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(ingressHop.Ingress),
				Metadata_ofp(innerTag),
				TunnelId(uint64(portNumber)),
			},
			Actions: GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT, METADATA)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, Output(ingressHop.Egress))

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)
	} else { // Create standard flow
		log.Debugw("creating-standard-flow", log.Fields{"flow": flow})
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(ingressHop.Ingress),
				TunnelId(uint64(inPortNo)),
			},
			Actions: GetActions(flow),
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

		// Augment the Actions
		fa.Actions = append(fa.Actions, Output(ingressHop.Egress))

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)
	}

	return deviceRules
}

// processUnicastFlow decomposes unicast flows
func (fd *FlowDecomposer) processUnicastFlow(agent coreIf.LogicalDeviceAgent, route []graph.RouteHop,
	inPortNo uint32, outPortNo uint32, flow *ofp.OfpFlowStats) *fu.DeviceRules {

	log.Debugw("unicast-flow", log.Fields{"inPortNo": inPortNo, "outPortNo": outPortNo})
	deviceRules := fu.NewDeviceRules()

	ingressHop := route[0]
	egressHop := route[1]

	var actions []ofp.OfpActionType
	var isOutputTypeInActions bool
	for _, action := range GetActions(flow) {
		actions = append(actions, action.Type)
		if !isOutputTypeInActions && action.Type == OUTPUT {
			isOutputTypeInActions = true
		}
	}
	if len(actions) == 1 && isOutputTypeInActions {
		var fa *fu.FlowArgs
		// Parent device flow
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(ingressHop.Ingress),
				TunnelId(uint64(inPortNo)),
			},
			Actions: []*ofp.OfpAction{
				Output(ingressHop.Egress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)

		// Child device flow
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(egressHop.Ingress),
			},
			Actions: []*ofp.OfpAction{
				Output(egressHop.Egress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

		fg = fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	} else {
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(egressHop.Ingress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

		// Augment the Actions
		filteredAction := GetActions(flow, OUTPUT)
		filteredAction = append(filteredAction, Output(egressHop.Egress))
		fa.Actions = filteredAction

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	}
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
			if action.Type == OUTPUT {
				outPortNo = action.GetOutput().Port
			} else if action.Type != POP_VLAN {
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
				InPort(ingressHop.Ingress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT)...)

		// Augment the Actions
		filteredAction := GetActions(flow, GROUP)
		filteredAction = append(filteredAction, PopVlan())
		filteredAction = append(filteredAction, Output(route2[1].Ingress))
		fa.Actions = filteredAction

		fg := fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(ingressHop.DeviceID, fg)

		// Set the child device flow
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": uint64(flow.Priority), "cookie": flow.Cookie},
			MatchFields: []*ofp.OfpOxmOfbField{
				InPort(egressHop.Ingress),
			},
		}
		// Augment the matchfields with the ofpfields from the flow
		fa.MatchFields = append(fa.MatchFields, GetOfbFields(flow, IN_PORT, VLAN_VID, VLAN_PCP)...)

		// Augment the Actions
		otherActions = append(otherActions, Output(egressHop.Egress))
		fa.Actions = otherActions

		fg = fu.NewFlowsAndGroups()
		fg.AddFlow(MkFlowStat(fa))
		deviceRules.AddFlowsAndGroup(egressHop.DeviceID, fg)
	}
	return deviceRules
}

// decomposeFlow decomposes a flow for a logical device into flows for each physical device
func (fd *FlowDecomposer) decomposeFlow(agent coreIf.LogicalDeviceAgent, flow *ofp.OfpFlowStats,
	groupMap map[uint32]*ofp.OfpGroupEntry) *fu.DeviceRules {

	inPortNo := GetInPort(flow)
	outPortNo := GetOutPort(flow)
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
		if isUpstream {
			deviceRules = fd.processUpstreamNonControllerBoundFlow(agent, route, inPortNo, outPortNo, flow)
		} else if HasNextTable(flow) {
			deviceRules = fd.processDownstreamFlowWithNextTable(agent, route, inPortNo, outPortNo, flow)
		} else if outPortNo != 0 { // Unicast
			deviceRules = fd.processUnicastFlow(agent, route, inPortNo, outPortNo, flow)
		} else if grpId := GetGroup(flow); grpId != 0 { //Multicast
			deviceRules = fd.processMulticastFlow(agent, route, inPortNo, outPortNo, flow, grpId, groupMap)
		}
	}
	deviceRules = fd.updateOutputPortForControllerBoundFlowForParentDevide(flow, deviceRules)
	return deviceRules
}
