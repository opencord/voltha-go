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
	"errors"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/rw_core/graph"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"github.com/stretchr/testify/assert"

	"testing"
)

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
	log.UpdateAllLoggers(log.Fields{"instanceId": "flow-decomposition"})
	log.SetAllLogLevel(log.WarnLevel)
}

type testDeviceManager struct {
	devices map[string]*voltha.Device
}

func newTestDeviceManager() *testDeviceManager {
	var tdm testDeviceManager
	tdm.devices = make(map[string]*voltha.Device)
	tdm.devices["olt"] = &voltha.Device{
		Id:       "olt",
		Root:     true,
		ParentId: "logical_device",
		Ports: []*voltha.Port{
			{PortNo: 1, Label: "pon"},
			{PortNo: 2, Label: "nni"},
		},
	}
	tdm.devices["onu1"] = &voltha.Device{
		Id:       "onu1",
		Root:     false,
		ParentId: "olt",
		Ports: []*voltha.Port{
			{PortNo: 1, Label: "pon"},
			{PortNo: 2, Label: "uni"},
		},
	}
	tdm.devices["onu2"] = &voltha.Device{
		Id:       "onu2",
		Root:     false,
		ParentId: "olt",
		Ports: []*voltha.Port{
			{PortNo: 1, Label: "pon"},
			{PortNo: 2, Label: "uni"},
		},
	}
	tdm.devices["onu3"] = &voltha.Device{
		Id:       "onu3",
		Root:     false,
		ParentId: "olt",
		Ports: []*voltha.Port{
			{PortNo: 1, Label: "pon"},
			{PortNo: 2, Label: "uni"},
		},
	}
	tdm.devices["onu4"] = &voltha.Device{
		Id:       "onu4",
		Root:     false,
		ParentId: "olt",
		Ports: []*voltha.Port{
			{PortNo: 1, Label: "pon"},
			{PortNo: 2, Label: "uni"},
		},
	}
	return &tdm
}

func (tdm *testDeviceManager) GetDevice(deviceId string) (*voltha.Device, error) {
	if d, ok := tdm.devices[deviceId]; ok {
		return d, nil
	}
	return nil, errors.New("ABSENT.")
}
func (tdm *testDeviceManager) IsRootDevice(deviceId string) (bool, error) {
	if d, ok := tdm.devices[deviceId]; ok {
		return d.Root, nil
	}
	return false, errors.New("ABSENT.")
}

func (tdm *testDeviceManager) NotifyInvalidTransition(pcDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) SetAdminStateToEnable(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) CreateLogicalDevice(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) SetupUNILogicalPorts(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DisableAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DeleteLogicalDevice(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DeleteLogicalPorts(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DeleteAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) RunPostDeviceDelete(cDevice *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) UpdateAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

type testFlowDecomposer struct {
	dMgr         *testDeviceManager
	logicalPorts map[uint32]*voltha.LogicalPort
	routes       map[graph.OFPortLink][]graph.RouteHop
	defaultRules *fu.DeviceRules
	deviceGraph  *graph.DeviceGraph
	fd           *FlowDecomposer
}

func newTestFlowDecomposer(deviceMgr *testDeviceManager) *testFlowDecomposer {
	var tfd testFlowDecomposer
	tfd.dMgr = deviceMgr

	tfd.logicalPorts = make(map[uint32]*voltha.LogicalPort)
	// Go protobuf interpreted absence of a port as 0, so we can't use port #0 as an openflow
	// port
	tfd.logicalPorts[10] = &voltha.LogicalPort{Id: "10", DeviceId: "olt", DevicePortNo: 2}
	tfd.logicalPorts[1] = &voltha.LogicalPort{Id: "1", DeviceId: "onu1", DevicePortNo: 2}
	tfd.logicalPorts[2] = &voltha.LogicalPort{Id: "2", DeviceId: "onu2", DevicePortNo: 2}
	tfd.logicalPorts[3] = &voltha.LogicalPort{Id: "3", DeviceId: "onu3", DevicePortNo: 2}
	tfd.logicalPorts[4] = &voltha.LogicalPort{Id: "4", DeviceId: "onu4", DevicePortNo: 2}

	tfd.routes = make(map[graph.OFPortLink][]graph.RouteHop)

	//DOWNSTREAM ROUTES

	tfd.routes[graph.OFPortLink{Ingress: 10, Egress: 1}] = []graph.RouteHop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[0].PortNo,
		},
		{
			DeviceID: "onu1",
			Ingress:  tfd.dMgr.devices["onu1"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["onu1"].Ports[1].PortNo,
		},
	}

	tfd.routes[graph.OFPortLink{Ingress: 10, Egress: 2}] = []graph.RouteHop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[0].PortNo,
		},
		{
			DeviceID: "onu2",
			Ingress:  tfd.dMgr.devices["onu2"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["onu2"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 10, Egress: 3}] = []graph.RouteHop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[0].PortNo,
		},
		{
			DeviceID: "onu3",
			Ingress:  tfd.dMgr.devices["onu3"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["onu3"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 10, Egress: 4}] = []graph.RouteHop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[0].PortNo,
		},
		{
			DeviceID: "onu4",
			Ingress:  tfd.dMgr.devices["onu4"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["onu4"].Ports[1].PortNo,
		},
	}

	//UPSTREAM DATA PLANE

	tfd.routes[graph.OFPortLink{Ingress: 1, Egress: 10}] = []graph.RouteHop{
		{
			DeviceID: "onu1",
			Ingress:  tfd.dMgr.devices["onu1"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu1"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 2, Egress: 10}] = []graph.RouteHop{
		{
			DeviceID: "onu2",
			Ingress:  tfd.dMgr.devices["onu2"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu2"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 3, Egress: 10}] = []graph.RouteHop{
		{
			DeviceID: "onu3",
			Ingress:  tfd.dMgr.devices["onu3"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu3"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 4, Egress: 10}] = []graph.RouteHop{
		{
			DeviceID: "onu4",
			Ingress:  tfd.dMgr.devices["onu4"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu4"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}

	//UPSTREAM NEXT TABLE BASED

	// openflow port 0 means absence of a port - go/protobuf interpretation
	tfd.routes[graph.OFPortLink{Ingress: 1, Egress: 0}] = []graph.RouteHop{
		{
			DeviceID: "onu1",
			Ingress:  tfd.dMgr.devices["onu1"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu1"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 2, Egress: 0}] = []graph.RouteHop{
		{
			DeviceID: "onu2",
			Ingress:  tfd.dMgr.devices["onu2"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu2"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 3, Egress: 0}] = []graph.RouteHop{
		{
			DeviceID: "onu3",
			Ingress:  tfd.dMgr.devices["onu3"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu3"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}
	tfd.routes[graph.OFPortLink{Ingress: 4, Egress: 0}] = []graph.RouteHop{
		{
			DeviceID: "onu4",
			Ingress:  tfd.dMgr.devices["onu4"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["onu4"].Ports[0].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}

	// DOWNSTREAM NEXT TABLE BASED

	tfd.routes[graph.OFPortLink{Ingress: 10, Egress: 0}] = []graph.RouteHop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[0].PortNo,
		},
		{}, // 2nd hop is not known yet
	}

	tfd.routes[graph.OFPortLink{Ingress: 0, Egress: 10}] = []graph.RouteHop{
		{}, // 1st hop is wildcard
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[0].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}

	// DEFAULT RULES

	tfd.defaultRules = fu.NewDeviceRules()
	fg := fu.NewFlowsAndGroups()
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(1),
		},
	}
	fg.AddFlow(fu.MkFlowStat(fa))
	tfd.defaultRules.AddFlowsAndGroup("onu1", fg)

	fg = fu.NewFlowsAndGroups()
	fa = &fu.FlowArgs{
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 102)),
			fu.Output(1),
		},
	}
	fg.AddFlow(fu.MkFlowStat(fa))
	tfd.defaultRules.AddFlowsAndGroup("onu2", fg)

	fg = fu.NewFlowsAndGroups()
	fa = &fu.FlowArgs{
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 103)),
			fu.Output(1),
		},
	}
	fg.AddFlow(fu.MkFlowStat(fa))
	tfd.defaultRules.AddFlowsAndGroup("onu3", fg)

	fg = fu.NewFlowsAndGroups()
	fa = &fu.FlowArgs{
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 104)),
			fu.Output(1),
		},
	}
	fg.AddFlow(fu.MkFlowStat(fa))
	tfd.defaultRules.AddFlowsAndGroup("onu4", fg)

	//Set up the device graph - flow decomposer uses it only to verify whether a port is a root port.
	tfd.deviceGraph = graph.NewDeviceGraph("ldid", tfd.getDeviceHelper)
	tfd.deviceGraph.RootPorts = make(map[uint32]uint32)
	tfd.deviceGraph.RootPorts[10] = 10

	tfd.fd = NewFlowDecomposer(tfd.dMgr)

	return &tfd
}

func (tfd *testFlowDecomposer) getDeviceHelper(deviceId string) (*voltha.Device, error) {
	return tfd.dMgr.GetDevice(deviceId)
}

func (tfd *testFlowDecomposer) GetDeviceLogicalId() string {
	return ""
}

func (tfd *testFlowDecomposer) GetLogicalDevice() (*voltha.LogicalDevice, error) {
	return nil, nil
}

func (tfd *testFlowDecomposer) GetDeviceGraph() *graph.DeviceGraph {
	return tfd.deviceGraph
}

func (tfd *testFlowDecomposer) GetAllDefaultRules() *fu.DeviceRules {
	return tfd.defaultRules
}

func (tfd *testFlowDecomposer) GetWildcardInputPorts(excludePort ...uint32) []uint32 {
	lPorts := make([]uint32, 0)
	var exclPort uint32
	if len(excludePort) == 1 {
		exclPort = excludePort[0]
	}
	for portno := range tfd.logicalPorts {
		if portno != exclPort {
			lPorts = append(lPorts, portno)
		}
	}
	return lPorts
}

func (tfd *testFlowDecomposer) GetRoute(ingressPortNo uint32, egressPortNo uint32) []graph.RouteHop {
	var portLink graph.OFPortLink
	if egressPortNo == 0 {
		portLink.Egress = 0
	} else if egressPortNo&0x7fffffff == uint32(ofp.OfpPortNo_OFPP_CONTROLLER) {
		portLink.Egress = 10
	} else {
		portLink.Egress = egressPortNo
	}
	if ingressPortNo == 0 {
		portLink.Ingress = 0
	} else {
		portLink.Ingress = ingressPortNo
	}
	for key, val := range tfd.routes {
		if key.Ingress == portLink.Ingress && key.Egress == portLink.Egress {
			return val
		}
	}
	return nil
}

func TestEapolReRouteRuleDecomposition(t *testing.T) {

	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}

	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fu.MkFlowStat(fa)}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(newTestDeviceManager())

	deviceRules := tfd.fd.DecomposeRules(tfd, flows, groups)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Nil(t, onu1FlowAndGroup)
	assert.Equal(t, 2, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 1),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.PushVlan(0x8100),
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000)),
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	expectedOltFlow := fu.MkFlowStat(fa)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000),
			fu.VlanPcp(0),
			fu.Metadata_ofp(1),
			fu.TunnelId(uint64(1)),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
			fu.Output(1),
		},
	}
	expectedOltFlow = fu.MkFlowStat(fa)
	derivedFlow = oltFlowAndGroup.GetFlow(1)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())
}

func TestDhcpReRouteRuleDecomposition(t *testing.T) {

	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			fu.EthType(0x0800),
			fu.Ipv4Dst(0xffffffff),
			fu.IpProto(17),
			fu.UdpSrc(68),
			fu.UdpDst(67),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}

	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fu.MkFlowStat(fa)}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(newTestDeviceManager())

	deviceRules := tfd.fd.DecomposeRules(tfd, flows, groups)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Nil(t, onu1FlowAndGroup)
	assert.Equal(t, 2, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 1),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x0800),
			fu.Ipv4Dst(0xffffffff),
			fu.IpProto(17),
			fu.UdpSrc(68),
			fu.UdpDst(67),
		},
		Actions: []*ofp.OfpAction{
			fu.PushVlan(0x8100),
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000)),
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	expectedOltFlow := fu.MkFlowStat(fa)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000),
			fu.VlanPcp(0),
			fu.Metadata_ofp(1),
			fu.TunnelId(uint64(1)),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
			fu.Output(1),
		},
	}
	expectedOltFlow = fu.MkFlowStat(fa)
	derivedFlow = oltFlowAndGroup.GetFlow(1)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())
}

func TestUnicastUpstreamRuleDecomposition(t *testing.T) {

	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500, "table_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
		},
	}

	var fa2 *fu.FlowArgs
	fa2 = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.PushVlan(0x8100),
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 1000)),
			fu.SetField(fu.VlanPcp(0)),
			fu.Output(10),
		},
	}

	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fu.MkFlowStat(fa), fu.MkFlowStat(fa2)}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(newTestDeviceManager())

	deviceRules := tfd.fd.DecomposeRules(tfd, flows, groups)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 0, onu1FlowAndGroup.Groups.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.TunnelId(uint64(1)),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(1),
		},
	}
	expectedOnu1Flow := fu.MkFlowStat(fa)
	derivedFlow := onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnu1Flow.String(), derivedFlow.String())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.TunnelId(uint64(1)),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.PushVlan(0x8100),
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 1000)),
			fu.SetField(fu.VlanPcp(0)),
			fu.Output(2),
		},
	}
	expectedOltFlow := fu.MkFlowStat(fa)
	derivedFlow = oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())
}

func TestUnicastDownstreamRuleDecomposition(t *testing.T) {
	var fa1 *fu.FlowArgs
	fa1 = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500, "table_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(10),
			fu.Metadata_ofp((1000 << 32) | 1),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
		},
	}

	var fa2 *fu.FlowArgs
	fa2 = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(10),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0)),
			fu.Output(1),
		},
	}

	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fu.MkFlowStat(fa1), fu.MkFlowStat(fa2)}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(newTestDeviceManager())

	deviceRules := tfd.fd.DecomposeRules(tfd, flows, groups)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 0, onu1FlowAndGroup.Groups.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa1 = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.Metadata_ofp(1000),
			fu.TunnelId(uint64(1)),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
			fu.Output(1),
		},
	}
	expectedOltFlow := fu.MkFlowStat(fa1)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	fa1 = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0)),
			fu.Output(2),
		},
	}
	expectedOnu1Flow := fu.MkFlowStat(fa1)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnu1Flow.String(), derivedFlow.String())
}

func TestMulticastDownstreamRuleDecomposition(t *testing.T) {
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(10),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 170),
			fu.VlanPcp(0),
			fu.EthType(0x800),
			fu.Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			fu.Group(10),
		},
	}

	var ga *fu.GroupArgs
	ga = &fu.GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				fu.PopVlan(),
				fu.Output(1),
			},
			},
		},
	}

	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fu.MkFlowStat(fa)}}
	groups := ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{fu.MkGroupStat(ga)}}
	tfd := newTestFlowDecomposer(newTestDeviceManager())

	deviceRules := tfd.fd.DecomposeRules(tfd, flows, groups)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 0, onu1FlowAndGroup.Groups.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 170),
			fu.VlanPcp(0),
			fu.EthType(0x800),
			fu.Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
			fu.Output(1),
		},
	}
	expectedOltFlow := fu.MkFlowStat(fa)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.EthType(0x800),
			fu.Ipv4Dst(0xe00a0a0a),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(2),
		},
	}
	expectedOnu1Flow := fu.MkFlowStat(fa)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnu1Flow.String(), derivedFlow.String())
}
