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
	"errors"
	"github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-go/rw_core/route"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"testing"
)

func init() {
	// Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, 0, log.Fields{"instanceId": 1}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers (provisioned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": 1}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers to log level specified as input parameter
	log.SetAllLogLevel(0)
}

type testDeviceManager struct {
	mocks.DeviceManager
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

func (tdm *testDeviceManager) GetDevice(ctx context.Context, deviceID string) (*voltha.Device, error) {
	if d, ok := tdm.devices[deviceID]; ok {
		return d, nil
	}
	return nil, errors.New("ABSENT")
}
func (tdm *testDeviceManager) IsRootDevice(deviceID string) (bool, error) {
	if d, ok := tdm.devices[deviceID]; ok {
		return d.Root, nil
	}
	return false, errors.New("ABSENT")
}

type testFlowDecomposer struct {
	dMgr           *testDeviceManager
	logicalPorts   map[uint32]*voltha.LogicalPort
	routes         map[route.OFPortLink][]route.Hop
	defaultRules   *fu.DeviceRules
	deviceRoutes   *route.DeviceRoutes
	fd             *FlowDecomposer
	logicalPortsNo map[uint32]bool
}

func newTestFlowDecomposer(t *testing.T, deviceMgr *testDeviceManager) *testFlowDecomposer {
	var tfd testFlowDecomposer
	tfd.dMgr = deviceMgr

	tfd.logicalPorts = make(map[uint32]*voltha.LogicalPort)
	tfd.logicalPortsNo = make(map[uint32]bool)
	// Go protobuf interpreted absence of a port as 0, so we can't use port #0 as an openflow
	// port
	tfd.logicalPorts[10] = &voltha.LogicalPort{Id: "10", DeviceId: "olt", DevicePortNo: 2}
	tfd.logicalPorts[65536] = &voltha.LogicalPort{Id: "65536", DeviceId: "olt", DevicePortNo: 65536}
	tfd.logicalPorts[1] = &voltha.LogicalPort{Id: "1", DeviceId: "onu1", DevicePortNo: 2}
	tfd.logicalPorts[2] = &voltha.LogicalPort{Id: "2", DeviceId: "onu2", DevicePortNo: 2}
	tfd.logicalPorts[3] = &voltha.LogicalPort{Id: "3", DeviceId: "onu3", DevicePortNo: 2}
	tfd.logicalPorts[4] = &voltha.LogicalPort{Id: "4", DeviceId: "onu4", DevicePortNo: 2}

	tfd.logicalPortsNo[10] = false
	tfd.logicalPortsNo[65536] = true // nni

	tfd.routes = make(map[route.OFPortLink][]route.Hop)

	//DOWNSTREAM ROUTES

	tfd.routes[route.OFPortLink{Ingress: 10, Egress: 1}] = []route.Hop{
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

	tfd.routes[route.OFPortLink{Ingress: 10, Egress: 2}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 10, Egress: 3}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 10, Egress: 4}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 10, Egress: 10}] = []route.Hop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[1].PortNo,
		},
	}

	//UPSTREAM DATA PLANE

	tfd.routes[route.OFPortLink{Ingress: 1, Egress: 10}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 2, Egress: 10}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 3, Egress: 10}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 4, Egress: 10}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 1, Egress: 0}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 2, Egress: 0}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 3, Egress: 0}] = []route.Hop{
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
	tfd.routes[route.OFPortLink{Ingress: 4, Egress: 0}] = []route.Hop{
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

	tfd.routes[route.OFPortLink{Ingress: 10, Egress: 0}] = []route.Hop{
		{
			DeviceID: "olt",
			Ingress:  tfd.dMgr.devices["olt"].Ports[1].PortNo,
			Egress:   tfd.dMgr.devices["olt"].Ports[0].PortNo,
		},
		{}, // 2nd hop is not known yet
	}

	tfd.routes[route.OFPortLink{Ingress: 0, Egress: 10}] = []route.Hop{
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
	fa := &fu.FlowArgs{
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(1),
		},
	}
	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(fs)
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
	fs, err = fu.MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(fs)
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
	fs, err = fu.MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(fs)
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
	fs, err = fu.MkFlowStat(fa)
	assert.Nil(t, err)
	fg.AddFlow(fs)
	tfd.defaultRules.AddFlowsAndGroup("onu4", fg)

	//Set up the device graph - flow decomposer uses it only to verify whether a port is a root port.
	tfd.deviceRoutes = route.NewDeviceRoutes("ldid", tfd.getDeviceHelper)
	tfd.deviceRoutes.RootPorts = make(map[uint32]uint32)
	tfd.deviceRoutes.RootPorts[10] = 10

	tfd.fd = NewFlowDecomposer(tfd.dMgr)

	return &tfd
}

func (tfd *testFlowDecomposer) getDeviceHelper(ctx context.Context, deviceID string) (*voltha.Device, error) {
	return tfd.dMgr.GetDevice(ctx, deviceID)
}

func (tfd *testFlowDecomposer) GetDeviceLogicalID() string {
	return ""
}

func (tfd *testFlowDecomposer) GetLogicalDevice(ctx context.Context) (*voltha.LogicalDevice, error) {
	return nil, nil
}

func (tfd *testFlowDecomposer) GetDeviceRoutes() *route.DeviceRoutes {
	return tfd.deviceRoutes
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

func (tfd *testFlowDecomposer) GetRoute(ctx context.Context, ingressPortNo uint32, egressPortNo uint32) ([]route.Hop, error) {
	var portLink route.OFPortLink
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
			return val, nil
		}
	}
	return nil, status.Errorf(codes.FailedPrecondition, "no route from:%d to:%d", ingressPortNo, egressPortNo)
}

func (tfd *testFlowDecomposer) GetNNIPorts() []uint32 {
	nniPorts := make([]uint32, 0)
	for portNo, nni := range tfd.logicalPortsNo {
		if nni {
			nniPorts = append(nniPorts, portNo)
		}
	}
	return nniPorts
}

func TestEapolReRouteRuleVlanDecomposition(t *testing.T) {

	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(50),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	faParent := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.TunnelId(uint64(1)),
			fu.VlanVid(50),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	expectedOltFlow, err := fu.MkFlowStat(faParent)
	assert.Nil(t, err)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	faChild := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.PushVlan(0x8100),
			fu.SetField(fu.VlanVid(50)),
			fu.Output(1),
		},
	}
	expectedOnuFlow, err := fu.MkFlowStat(faChild)
	assert.Nil(t, err)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnuFlow.String(), derivedFlow.String())
}

func TestEapolReRouteRuleZeroVlanDecomposition(t *testing.T) {

	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(0),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	faParent := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.TunnelId(uint64(1)),
			fu.VlanVid(0),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	expectedOltFlow, err := fu.MkFlowStat(faParent)
	assert.Nil(t, err)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	faChild := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.PushVlan(0x8100),
			fu.SetField(fu.VlanVid(0)),
			fu.Output(1),
		},
	}
	expectedOnuFlow, err := fu.MkFlowStat(faChild)
	assert.Nil(t, err)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnuFlow.String(), derivedFlow.String())
}

func TestEapolReRouteRuleNoVlanDecomposition(t *testing.T) {

	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	faParent := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	expectedOltFlow, err := fu.MkFlowStat(faParent)
	assert.Nil(t, err)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	faChild := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(1),
		},
	}
	expectedOnuFlow, err := fu.MkFlowStat(faChild)
	assert.Nil(t, err)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnuFlow.String(), derivedFlow.String())
}

func TestDhcpReRouteRuleDecomposition(t *testing.T) {

	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
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

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 0, onu1FlowAndGroup.Groups.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	faParent := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.TunnelId(uint64(1)),
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
	expectedOltFlow, err := fu.MkFlowStat(faParent)
	assert.Nil(t, err)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())

	faChild := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.TunnelId(uint64(1)),
			fu.EthType(0x0800),
			fu.Ipv4Dst(0xffffffff),
			fu.IpProto(17),
			fu.UdpSrc(68),
			fu.UdpDst(67),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(1),
		},
	}
	expectedOnuFlow, err := fu.MkFlowStat(faChild)
	assert.Nil(t, err)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnuFlow.String(), derivedFlow.String())
}

func TestLldpReRouteRuleDecomposition(t *testing.T) {
	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(10),
			fu.EthType(0x88CC),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs}}
	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())
	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Nil(t, onu1FlowAndGroup)
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 1000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.EthType(0x88CC),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	expectedOltFlow, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())
}

func TestUnicastUpstreamRuleDecomposition(t *testing.T) {
	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 5000, "table_id": 0},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(1),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
		},
	}

	fa2 := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500, "table_id": 1},
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

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	fs2, err := fu.MkFlowStat(fa2)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs, fs2}}
	flows.Items[0].Instructions = []*ofp.OfpInstruction{{
		Type: uint32(ofp.OfpInstructionType_OFPIT_GOTO_TABLE),
		Data: &ofp.OfpInstruction_GotoTable{
			GotoTable: &ofp.OfpInstructionGotoTable{
				TableId: 1,
			},
		}}}

	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	onu1FlowAndGroup := deviceRules.Rules["onu1"]
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.NotNil(t, onu1FlowAndGroup)
	assert.NotNil(t, onu1FlowAndGroup.Flows)
	assert.Equal(t, 1, onu1FlowAndGroup.Flows.Len())
	assert.Equal(t, 0, onu1FlowAndGroup.Groups.Len())
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 5000},
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

	derivedFlow := onu1FlowAndGroup.GetFlow(0)
	// Form the expected flow
	expectedOnu1Flow, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	expectedOnu1Flow.Instructions = []*ofp.OfpInstruction{{
		Type: uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS),
		Data: &ofp.OfpInstruction_Actions{
			Actions: &ofp.OfpInstructionActions{
				Actions: []*ofp.OfpAction{{
					Type: 0,
					Action: &ofp.OfpAction_Output{
						Output: &ofp.OfpActionOutput{
							Port:   1,
							MaxLen: 65509,
						},
					}}}}}}}

	expectedOnu1Flow.Id = derivedFlow.Id //  Assign same flow ID as derived flowID to match completely
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
	expectedOltFlow, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	derivedFlow = oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())
}

func TestUnicastDownstreamRuleDecomposition(t *testing.T) {
	logger.Debugf("Starting Test Unicast Downstream")
	fa1 := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500, "table_id": 0},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(10),
			fu.Metadata_ofp((1000 << 32) | 1),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
		},
	}

	fa2 := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500, "table_id": 1},
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

	fs1, err := fu.MkFlowStat(fa1)
	assert.Nil(t, err)
	fs2, err := fu.MkFlowStat(fa2)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs1, fs2}}
	flows.Items[0].Instructions = []*ofp.OfpInstruction{{
		Type: uint32(ofp.OfpInstructionType_OFPIT_GOTO_TABLE),
		Data: &ofp.OfpInstruction_GotoTable{
			GotoTable: &ofp.OfpInstructionGotoTable{
				TableId: 1,
			},
		}}}

	groups := ofp.FlowGroups{}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
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
			fu.TunnelId(uint64(10)),
			fu.Metadata_ofp(4294967296001),
			fu.VlanPcp(0),
		},
		Actions: []*ofp.OfpAction{
			fu.PopVlan(),
			fu.Output(1),
		},
	}

	derivedFlow := oltFlowAndGroup.GetFlow(0)
	expectedOltFlow, err := fu.MkFlowStat(fa1)
	assert.Nil(t, err)
	expectedOltFlow.Instructions = []*ofp.OfpInstruction{{
		Type: uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS),
		Data: &ofp.OfpInstruction_Actions{
			Actions: &ofp.OfpInstructionActions{
				Actions: []*ofp.OfpAction{{
					Type: 0,
					Action: &ofp.OfpAction_Output{
						Output: &ofp.OfpActionOutput{
							Port:   1,
							MaxLen: 65509,
						},
					}}}}}}}
	expectedOltFlow.Id = derivedFlow.Id
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
	expectedOnu1Flow, err := fu.MkFlowStat(fa1)
	assert.Nil(t, err)
	derivedFlow = onu1FlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOnu1Flow.String(), derivedFlow.String())
}

func TestMulticastDownstreamRuleDecomposition(t *testing.T) {
	fa := &fu.FlowArgs{
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

	ga := &fu.GroupArgs{
		GroupId: 10,
		Buckets: []*ofp.OfpBucket{
			{Actions: []*ofp.OfpAction{
				fu.PopVlan(),
				fu.Output(1),
			},
			},
		},
	}

	fs, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	flows := ofp.Flows{Items: []*ofp.OfpFlowStats{fs}}
	groups := ofp.FlowGroups{Items: []*ofp.OfpGroupEntry{fu.MkGroupStat(ga)}}
	tfd := newTestFlowDecomposer(t, newTestDeviceManager())

	deviceRules, err := tfd.fd.DecomposeRules(context.Background(), tfd, flows, groups)
	assert.Nil(t, err)
	oltFlowAndGroup := deviceRules.Rules["olt"]
	assert.Equal(t, 1, oltFlowAndGroup.Flows.Len())
	assert.Equal(t, 0, oltFlowAndGroup.Groups.Len())

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
	expectedOltFlow, err := fu.MkFlowStat(fa)
	assert.Nil(t, err)
	derivedFlow := oltFlowAndGroup.GetFlow(0)
	assert.Equal(t, expectedOltFlow.String(), derivedFlow.String())
}
