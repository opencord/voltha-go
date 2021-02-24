/*
 * Copyright 2019-present Open Networking Foundation
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
	"strings"
	"testing"

	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	timeoutError     error
	taskFailureError error
)

func init() {
	timeoutError = status.Errorf(codes.Aborted, "timeout")
	taskFailureError = status.Error(codes.Internal, "test failure task")
	timeoutError = status.Errorf(codes.Aborted, "timeout")
}

func TestFlowsAndGroups_AddFlow(t *testing.T) {
	fg := NewFlowsAndGroups()
	allFlows := fg.ListFlows()
	assert.Equal(t, 0, len(allFlows))
	fg.AddFlow(nil)
	allFlows = fg.ListFlows()
	assert.Equal(t, 0, len(allFlows))

	fa := &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(1),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 1),
			TunnelId(uint64(1)),
			EthType(0x0800),
			Ipv4Dst(0xffffffff),
			IpProto(17),
			UdpSrc(68),
			UdpDst(67),
		},
		Actions: []*ofp.OfpAction{
			PushVlan(0x8100),
			SetField(VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000)),
			Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(flow)

	allFlows = fg.ListFlows()
	assert.Equal(t, 1, len(allFlows))
	assert.True(t, FlowMatch(flow, allFlows[0]))
}

func TestFlowsAndGroups_AddGroup(t *testing.T) {
	var ga *GroupArgs

	fg := NewFlowsAndGroups()
	allGroups := fg.ListGroups()
	assert.Equal(t, 0, len(allGroups))
	fg.AddGroup(nil)
	allGroups = fg.ListGroups()
	assert.Equal(t, 0, len(allGroups))

	ga = &GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				PopVlan(),
				Output(1),
			},
			},
		},
	}
	group := MkGroupStat(ga)
	fg.AddGroup(group)

	allGroups = fg.ListGroups()
	assert.Equal(t, 1, len(allGroups))
	assert.Equal(t, ga.GroupId, allGroups[0].Desc.GroupId)
}

func TestFlowsAndGroups_Copy(t *testing.T) {
	fg := NewFlowsAndGroups()
	var fa *FlowArgs
	var ga *GroupArgs

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			SetField(VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 10)),
			Output(1),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(flow)

	ga = &GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				PopVlan(),
				Output(1),
			},
			},
		},
	}
	group := MkGroupStat(ga)
	fg.AddGroup(group)

	fgCopy := fg.Copy()

	allFlows := fgCopy.ListFlows()
	assert.Equal(t, 1, len(allFlows))
	assert.True(t, FlowMatch(flow, allFlows[0]))

	allGroups := fgCopy.ListGroups()
	assert.Equal(t, 1, len(allGroups))
	assert.Equal(t, ga.GroupId, allGroups[0].Desc.GroupId)

	fg = NewFlowsAndGroups()
	fgCopy = fg.Copy()
	allFlows = fgCopy.ListFlows()
	allGroups = fgCopy.ListGroups()
	assert.Equal(t, 0, len(allFlows))
	assert.Equal(t, 0, len(allGroups))
}

func TestFlowsAndGroups_GetFlow(t *testing.T) {
	fg := NewFlowsAndGroups()
	var fa1 *FlowArgs
	var fa2 *FlowArgs
	var ga *GroupArgs

	fa1 = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			Metadata_ofp((1000 << 32) | 1),
			VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
		},
	}
	flow1, err := MkFlowStat(fa1)
	assert.Nil(t, err)

	fa2 = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 1500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(5),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			PushVlan(0x8100),
			SetField(VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 1000)),
			SetField(VlanPcp(0)),
			Output(2),
		},
	}
	flow2, err := MkFlowStat(fa2)
	assert.Nil(t, err)

	fg.AddFlow(flow1)
	fg.AddFlow(flow2)

	ga = &GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				PopVlan(),
				Output(1),
			},
			},
		},
	}
	group := MkGroupStat(ga)
	fg.AddGroup(group)

	gf1 := fg.GetFlow(0)
	assert.True(t, FlowMatch(flow1, gf1))

	gf2 := fg.GetFlow(1)
	assert.True(t, FlowMatch(flow2, gf2))

	gf3 := fg.GetFlow(2)
	assert.Nil(t, gf3)

	allFlows := fg.ListFlows()
	assert.True(t, FlowMatch(flow1, allFlows[0]))
	assert.True(t, FlowMatch(flow2, allFlows[1]))
}

func TestFlowsAndGroups_String(t *testing.T) {
	fg := NewFlowsAndGroups()
	var fa *FlowArgs
	var ga *GroupArgs

	str := fg.String()
	assert.True(t, str == "")

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Group(10),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(flow)

	ga = &GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				PopVlan(),
				Output(1),
			},
			},
		},
	}
	group := MkGroupStat(ga)
	fg.AddGroup(group)

	str = fg.String()
	assert.True(t, strings.Contains(str, "id: 11819684229970388353"))
	assert.True(t, strings.Contains(str, "group_id: 10"))
	assert.True(t, strings.Contains(str, "oxm_class: OFPXMC_OPENFLOW_BASICOFPXMC_OPENFLOW_BASIC"))
	assert.True(t, strings.Contains(str, "type: OFPXMT_OFB_VLAN_VIDOFPXMT_OFB_VLAN_VID"))
	assert.True(t, strings.Contains(str, "vlan_vid: 4096"))
	assert.True(t, strings.Contains(str, "buckets:"))
}

func TestFlowsAndGroups_AddFrom(t *testing.T) {
	fg := NewFlowsAndGroups()
	var fa *FlowArgs
	var ga *GroupArgs

	str := fg.String()
	assert.True(t, str == "")

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			Metadata_ofp(1000),
			TunnelId(uint64(1)),
			VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
			Output(1),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(flow)

	ga = &GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				PopVlan(),
				Output(1),
			},
			},
		},
	}
	group := MkGroupStat(ga)
	fg.AddGroup(group)

	fg1 := NewFlowsAndGroups()
	fg1.AddFrom(fg)

	allFlows := fg1.ListFlows()
	allGroups := fg1.ListGroups()
	assert.Equal(t, 1, len(allFlows))
	assert.Equal(t, 1, len(allGroups))
	assert.True(t, FlowMatch(flow, allFlows[0]))
	assert.Equal(t, group.Desc.GroupId, allGroups[0].Desc.GroupId)
}

func TestDeviceRules_AddFlow(t *testing.T) {
	dr := NewDeviceRules()
	rules := dr.GetRules()
	assert.True(t, len(rules) == 0)

	dr.AddFlow("123456", nil)
	rules = dr.GetRules()
	assert.True(t, len(rules) == 1)
	val, ok := rules["123456"]
	assert.True(t, ok)
	assert.Equal(t, 0, len(val.ListFlows()))
	assert.Equal(t, 0, len(val.ListGroups()))

	fa := &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			Metadata_ofp(1000),
			TunnelId(uint64(1)),
			VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
			Output(1),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	dr.AddFlow("123456", flow)
	rules = dr.GetRules()
	assert.True(t, len(rules) == 1)
	val, ok = rules["123456"]
	assert.True(t, ok)
	assert.Equal(t, 1, len(val.ListFlows()))
	assert.True(t, FlowMatch(flow, val.ListFlows()[0]))
	assert.Equal(t, 0, len(val.ListGroups()))
}

func TestDeviceRules_AddFlowsAndGroup(t *testing.T) {
	fg := NewFlowsAndGroups()
	var fa *FlowArgs
	var ga *GroupArgs

	str := fg.String()
	assert.True(t, str == "")

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 2000},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			Metadata_ofp(1000),
			TunnelId(uint64(1)),
			VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
			Output(1),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(flow)

	ga = &GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				PopVlan(),
				Output(1),
			},
			},
		},
	}
	group := MkGroupStat(ga)
	fg.AddGroup(group)

	dr := NewDeviceRules()
	dr.AddFlowsAndGroup("123456", fg)
	rules := dr.GetRules()
	assert.True(t, len(rules) == 1)
	val, ok := rules["123456"]
	assert.True(t, ok)
	assert.Equal(t, 1, len(val.ListFlows()))
	assert.Equal(t, 1, len(val.ListGroups()))
	assert.True(t, FlowMatch(flow, val.ListFlows()[0]))
	assert.Equal(t, 10, int(val.ListGroups()[0].Desc.GroupId))
}

func TestFlowHasOutPort(t *testing.T) {
	var flow *ofp.OfpFlowStats
	assert.False(t, FlowHasOutPort(flow, 1))

	fa := &FlowArgs{
		KV: OfpFlowModArgs{"priority": 2000},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			Metadata_ofp(1000),
			TunnelId(uint64(1)),
			VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
			Output(1),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	assert.True(t, FlowHasOutPort(flow, 1))
	assert.False(t, FlowHasOutPort(flow, 2))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 2000},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
	}
	flow, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowHasOutPort(flow, 1))
}

func TestFlowHasOutGroup(t *testing.T) {
	var flow *ofp.OfpFlowStats
	assert.False(t, FlowHasOutGroup(flow, 10))

	fa := &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Group(10),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	assert.True(t, FlowHasOutGroup(flow, 10))
	assert.False(t, FlowHasOutGroup(flow, 11))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Output(1),
		},
	}
	flow, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowHasOutGroup(flow, 1))
}

func TestMatchFlow(t *testing.T) {
	assert.False(t, FlowMatch(nil, nil))
	fa := &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Group(10),
		},
	}
	flow1, err := MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatch(flow1, nil))

	// different table_id, cookie, flags
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Group(10),
		},
	}
	flow2, err := MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatch(flow1, flow2))
	assert.False(t, FlowMatch(nil, flow2))

	// no difference
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.True(t, FlowMatch(flow1, flow2))

	// different priority
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 501, "table_id": 1, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Group(10),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatch(flow1, flow2))

	// different table id
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 2, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatch(flow1, flow2))

	// different cookie
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268467, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.True(t, FlowMatch(flow1, flow2))

	// different flags
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268468, "flags": 14},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.True(t, FlowMatch(flow1, flow2))

	// different match InPort
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(4),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatch(flow1, flow2))

	// different match Ipv4Dst
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatch(flow1, flow2))

	// different actions
	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1, "cookie": 38268468, "flags": 12},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
			Output(1),
		},
	}
	flow2, err = MkFlowStat(fa)
	assert.Nil(t, err)
	assert.True(t, FlowMatch(flow1, flow2))
}

func TestFlowMatchesMod(t *testing.T) {
	assert.False(t, FlowMatchesMod(nil, nil))
	fa := &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			Output(1),
			Group(10),
		},
	}
	flow, err := MkFlowStat(fa)
	assert.Nil(t, err)
	assert.False(t, FlowMatchesMod(flow, nil))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"priority": 500, "table_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			InPort(2),
			VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			VlanPcp(0),
			EthType(0x800),
			Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			PopVlan(),
			Output(1),
		},
	}
	flowMod := MkSimpleFlowMod(ToOfpOxmField(fa.MatchFields), fa.Actions, fa.Command, fa.KV)
	assert.False(t, FlowMatchesMod(nil, flowMod))
	assert.False(t, FlowMatchesMod(flow, flowMod))
	entry, err := FlowStatsEntryFromFlowModMessage(flowMod)
	assert.Nil(t, err)
	assert.True(t, FlowMatch(flow, entry))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"table_id": uint64(ofp.OfpTable_OFPTT_ALL),
			"cookie_mask": 0,
			"out_port":    uint64(ofp.OfpPortNo_OFPP_ANY),
			"out_group":   uint64(ofp.OfpGroup_OFPG_ANY),
		},
	}
	flowMod = MkSimpleFlowMod(ToOfpOxmField(fa.MatchFields), fa.Actions, fa.Command, fa.KV)
	assert.True(t, FlowMatchesMod(flow, flowMod))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"table_id": 1,
			"cookie_mask": 0,
			"out_port":    uint64(ofp.OfpPortNo_OFPP_ANY),
			"out_group":   uint64(ofp.OfpGroup_OFPG_ANY),
		},
	}
	flowMod = MkSimpleFlowMod(ToOfpOxmField(fa.MatchFields), fa.Actions, fa.Command, fa.KV)
	assert.True(t, FlowMatchesMod(flow, flowMod))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"table_id": 1,
			"cookie_mask": 0,
			"out_port":    1,
			"out_group":   uint64(ofp.OfpGroup_OFPG_ANY),
		},
	}
	flowMod = MkSimpleFlowMod(ToOfpOxmField(fa.MatchFields), fa.Actions, fa.Command, fa.KV)
	assert.True(t, FlowMatchesMod(flow, flowMod))

	fa = &FlowArgs{
		KV: OfpFlowModArgs{"table_id": 1,
			"cookie_mask": 0,
			"out_port":    1,
			"out_group":   10,
		},
	}
	flowMod = MkSimpleFlowMod(ToOfpOxmField(fa.MatchFields), fa.Actions, fa.Command, fa.KV)
	assert.True(t, FlowMatchesMod(flow, flowMod))
}

func TestIsMulticastIpAddress(t *testing.T) {
	isMcastIp := IsMulticastIp(3776315393) //225.22.0.1
	assert.True(t, isMcastIp)
	isMcastIp = IsMulticastIp(3232243777) //192.168.32.65
	assert.True(t, !isMcastIp)
}

func TestConvertToMulticastMac(t *testing.T) {
	mcastIp := uint32(4001431809)                   //238.129.1.1
	expectedMacInBytes := []byte{1, 0, 94, 1, 1, 1} //01:00:5e:01:01:01
	macInBytes := ConvertToMulticastMacBytes(mcastIp)
	assert.True(t, bytes.Equal(macInBytes, expectedMacInBytes))
}
