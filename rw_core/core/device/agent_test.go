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

package device

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	tst "github.com/opencord/voltha-go/rw_core/test"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	mock_etcd "github.com/opencord/voltha-lib-go/v3/pkg/mocks/etcd"
	mock_kafka "github.com/opencord/voltha-lib-go/v3/pkg/mocks/kafka"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

type DATest struct {
	etcdServer       *mock_etcd.EtcdServer
	deviceMgr        *Manager
	logicalDeviceMgr *LogicalManager
	adapterMgr       *adapter.Manager
	kmp              kafka.InterContainerProxy
	kClient          kafka.Client
	kvClientPort     int
	oltAdapter       *cm.OLTAdapter
	onuAdapter       *cm.ONUAdapter
	oltAdapterName   string
	onuAdapterName   string
	coreInstanceID   string
	defaultTimeout   time.Duration
	maxTimeout       time.Duration
	device           *voltha.Device
	done             chan int
}

func newDATest() *DATest {
	test := &DATest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = tst.StartEmbeddedEtcdServer("voltha.rwcore.da.test", "voltha.rwcore.da.etcd", "error")
	if err != nil {
		logger.Fatal(err)
	}
	// Create the kafka client
	test.kClient = mock_kafka.NewKafkaClient()
	test.oltAdapterName = "olt_adapter_mock"
	test.onuAdapterName = "onu_adapter_mock"
	test.coreInstanceID = "rw-da-test"
	test.defaultTimeout = 5 * time.Second
	test.maxTimeout = 20 * time.Second
	test.done = make(chan int)
	parentID := com.GetRandomString(10)
	test.device = &voltha.Device{
		Type:         "onu_adapter_mock",
		ParentId:     parentID,
		ParentPortNo: 1,
		VendorId:     "onu_adapter_mock",
		Adapter:      "onu_adapter_mock",
		Vlan:         100,
		Address:      nil,
		ProxyAddress: &voltha.Device_ProxyAddress{
			DeviceId:           parentID,
			DeviceType:         "olt_adapter_mock",
			ChannelId:          100,
			ChannelGroupId:     0,
			ChannelTermination: "",
			OnuId:              2,
		},
		AdminState:    voltha.AdminState_PREPROVISIONED,
		OperStatus:    voltha.OperStatus_UNKNOWN,
		Reason:        "All good",
		ConnectStatus: voltha.ConnectStatus_UNKNOWN,
		Custom:        nil,
		Ports: []*voltha.Port{
			{PortNo: 1, Label: "pon-1", Type: voltha.Port_PON_ONU, AdminState: voltha.AdminState_ENABLED,
				OperStatus: voltha.OperStatus_ACTIVE, Peers: []*voltha.Port_PeerPort{{DeviceId: parentID, PortNo: 1}}},
			{PortNo: 100, Label: "uni-100", Type: voltha.Port_ETHERNET_UNI, AdminState: voltha.AdminState_ENABLED,
				OperStatus: voltha.OperStatus_ACTIVE},
		},
	}
	return test
}

func (dat *DATest) startCore(inCompeteMode bool) {
	cfg := config.NewRWCoreFlags()
	cfg.CorePairTopic = "rw_core"
	cfg.DefaultRequestTimeout = dat.defaultTimeout
	cfg.KVStorePort = dat.kvClientPort
	cfg.InCompetingMode = inCompeteMode
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal("Cannot get a freeport for grpc")
	}
	cfg.GrpcPort = grpcPort
	cfg.GrpcHost = "127.0.0.1"
	client := tst.SetupKVClient(cfg, dat.coreInstanceID)
	backend := &db.Backend{
		Client:                  client,
		StoreType:               cfg.KVStoreType,
		Host:                    cfg.KVStoreHost,
		Port:                    cfg.KVStorePort,
		Timeout:                 cfg.KVStoreTimeout,
		LivenessChannelInterval: cfg.LiveProbeInterval / 2,
		PathPrefix:              cfg.KVStoreDataPrefix}
	dat.kmp = kafka.NewInterContainerProxy(
		kafka.InterContainerHost(cfg.KafkaAdapterHost),
		kafka.InterContainerPort(cfg.KafkaAdapterPort),
		kafka.MsgClient(dat.kClient),
		kafka.DefaultTopic(&kafka.Topic{Name: cfg.CoreTopic}),
		kafka.DeviceDiscoveryTopic(&kafka.Topic{Name: cfg.AffinityRouterTopic}))

	endpointMgr := kafka.NewEndpointManager(backend)
	proxy := model.NewDBPath(backend)
	dat.adapterMgr = adapter.NewAdapterManager(proxy, dat.coreInstanceID, dat.kClient)

	dat.deviceMgr, dat.logicalDeviceMgr = NewManagers(proxy, dat.adapterMgr, dat.kmp, endpointMgr, cfg.CorePairTopic, dat.coreInstanceID, cfg.DefaultCoreTimeout)
	dat.adapterMgr.Start(context.Background())
	if err = dat.kmp.Start(); err != nil {
		logger.Fatal("Cannot start InterContainerProxy")
	}

	if err := dat.kmp.SubscribeWithDefaultRequestHandler(kafka.Topic{Name: cfg.CorePairTopic}, kafka.OffsetNewest); err != nil {
		logger.Fatalf("Cannot add default request handler: %s", err)
	}

}

func (dat *DATest) stopAll() {
	if dat.kClient != nil {
		dat.kClient.Stop()
	}
	if dat.kmp != nil {
		dat.kmp.Stop()
	}
	if dat.etcdServer != nil {
		tst.StopEmbeddedEtcdServer(dat.etcdServer)
	}
}

func (dat *DATest) createDeviceAgent(t *testing.T) *Agent {
	deviceMgr := dat.deviceMgr
	clonedDevice := proto.Clone(dat.device).(*voltha.Device)
	deviceAgent := newAgent(deviceMgr.adapterProxy, clonedDevice, deviceMgr, deviceMgr.dbProxy, deviceMgr.dProxy, deviceMgr.defaultTimeout)
	d, err := deviceAgent.start(context.TODO(), clonedDevice)
	assert.Nil(t, err)
	assert.NotNil(t, d)
	deviceMgr.addDeviceAgentToMap(deviceAgent)
	return deviceAgent
}

func (dat *DATest) updateDeviceConcurrently(t *testing.T, da *Agent, globalWG *sync.WaitGroup) {
	originalDevice, err := da.getDevice(context.Background())
	assert.Nil(t, err)
	assert.NotNil(t, originalDevice)
	var localWG sync.WaitGroup

	// Update device routine
	var (
		root         = false
		vendor       = "onu_adapter_mock"
		model        = "go-mock"
		serialNumber = com.GetRandomSerialNumber()
		macAddress   = strings.ToUpper(com.GetRandomMacAddress())
		vlan         = rand.Uint32()
		reason       = "testing concurrent device update"
		portToAdd    = &voltha.Port{PortNo: 101, Label: "uni-101", Type: voltha.Port_ETHERNET_UNI, AdminState: voltha.AdminState_ENABLED,
			OperStatus: voltha.OperStatus_ACTIVE}
	)
	localWG.Add(1)
	go func() {
		deviceToUpdate := proto.Clone(originalDevice).(*voltha.Device)
		deviceToUpdate.Root = root
		deviceToUpdate.Vendor = vendor
		deviceToUpdate.Model = model
		deviceToUpdate.SerialNumber = serialNumber
		deviceToUpdate.MacAddress = macAddress
		deviceToUpdate.Vlan = vlan
		deviceToUpdate.Reason = reason
		err := da.updateDeviceUsingAdapterData(context.Background(), deviceToUpdate)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Update the device status routine
	localWG.Add(1)
	go func() {
		err := da.updateDeviceStatus(context.Background(), voltha.OperStatus_ACTIVE, voltha.ConnectStatus_REACHABLE)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Add a port routine
	localWG.Add(1)
	go func() {
		err := da.addPort(context.Background(), portToAdd)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// wait for go routines to be done
	localWG.Wait()

	expectedChange := proto.Clone(originalDevice).(*voltha.Device)
	expectedChange.OperStatus = voltha.OperStatus_ACTIVE
	expectedChange.ConnectStatus = voltha.ConnectStatus_REACHABLE
	expectedChange.Ports = append(expectedChange.Ports, portToAdd)
	expectedChange.Root = root
	expectedChange.Vendor = vendor
	expectedChange.Model = model
	expectedChange.SerialNumber = serialNumber
	expectedChange.MacAddress = macAddress
	expectedChange.Vlan = vlan
	expectedChange.Reason = reason

	updatedDevice, _ := da.getDevice(context.Background())
	assert.NotNil(t, updatedDevice)
	assert.True(t, proto.Equal(expectedChange, updatedDevice))

	globalWG.Done()
}

func TestConcurrentDevices(t *testing.T) {
	for i := 0; i < 2; i++ {
		da := newDATest()
		assert.NotNil(t, da)
		defer da.stopAll()
		log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)
		// Start the Core
		da.startCore(false)

		var wg sync.WaitGroup
		numConCurrentDeviceAgents := 20
		for i := 0; i < numConCurrentDeviceAgents; i++ {
			wg.Add(1)
			a := da.createDeviceAgent(t)
			go da.updateDeviceConcurrently(t, a, &wg)
		}

		wg.Wait()
	}
}
func TestFlowUpdates(t *testing.T) {
	da := newDATest()
	assert.NotNil(t, da)
	defer da.stopAll()

	log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)
	// Start the Core
	da.startCore(false)
	da.oltAdapter, da.onuAdapter = tst.CreateAndregisterAdapters(t, da.kClient, da.coreInstanceID, da.oltAdapterName, da.onuAdapterName, da.adapterMgr)

	a := da.createDeviceAgent(t)
	cloned := a.getDeviceWithoutLock()
	err := a.updateDeviceStateInStoreWithoutLock(context.Background(), cloned, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	assert.Nil(t, err)
	da.testFlowAddDeletes(t, a)
}

func TestGroupUpdates(t *testing.T) {
	da := newDATest()
	assert.NotNil(t, da)
	defer da.stopAll()

	// Start the Core
	da.startCore(false)
	da.oltAdapter, da.onuAdapter = tst.CreateAndregisterAdapters(t, da.kClient, da.coreInstanceID, da.oltAdapterName, da.onuAdapterName, da.adapterMgr)
	a := da.createDeviceAgent(t)
	cloned := a.getDeviceWithoutLock()
	err := a.updateDeviceStateInStoreWithoutLock(context.Background(), cloned, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	assert.Nil(t, err)
	da.testGroupAddDeletes(t, a)
}

func isFlowSliceEqual(a, b []*ofp.OfpFlowStats) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Slice(a, func(i, j int) bool {
		return a[i].Id < a[j].Id
	})
	sort.Slice(b, func(i, j int) bool {
		return b[i].Id < b[j].Id
	})
	for idx := range a {
		if !proto.Equal(a[idx], b[idx]) {
			return false
		}
	}
	return true
}

func isGroupSliceEqual(a, b []*ofp.OfpGroupEntry) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Slice(a, func(i, j int) bool {
		return a[i].Desc.GroupId < a[j].Desc.GroupId
	})
	sort.Slice(b, func(i, j int) bool {
		return b[i].Desc.GroupId < b[j].Desc.GroupId
	})
	for idx := range a {
		if !proto.Equal(a[idx], b[idx]) {
			return false
		}
	}
	return true
}
func changeToFlowList(flowList map[uint64]*ofp.OfpFlowStats) []*ofp.OfpFlowStats {
	flows := make([]*ofp.OfpFlowStats, 0)
	for _, flow := range flowList {
		flows = append(flows, flow)
	}
	return flows
}
func changeToGroupList(groupList map[uint32]*ofp.OfpGroupEntry) []*ofp.OfpGroupEntry {
	groups := make([]*ofp.OfpGroupEntry, 0)
	for _, group := range groupList {
		groups = append(groups, group)
	}
	return groups
}
func (dat *DATest) testFlowAddDeletes(t *testing.T, da *Agent) {
	//Add new Flows on empty list
	newFlows := []*ofp.OfpFlowStats{
		{Id: 123, TableId: 1230, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1230000, PacketCount: 0},
		{Id: 124, TableId: 1240, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1240000, PacketCount: 0},
		{Id: 125, TableId: 1250, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1250000, PacketCount: 0},
	}
	err := da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daFlows := changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(newFlows, daFlows))

	//Add new Flows on existing ones
	newFlows = []*ofp.OfpFlowStats{
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270000, PacketCount: 0},
	}

	expectedFlows := []*ofp.OfpFlowStats{
		{Id: 123, TableId: 1230, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1230000, PacketCount: 0},
		{Id: 124, TableId: 1240, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1240000, PacketCount: 0},
		{Id: 125, TableId: 1250, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1250000, PacketCount: 0},
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270000, PacketCount: 0},
	}

	err = da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daFlows = changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(expectedFlows, daFlows))

	//Add existing Flows again with a new flow
	newFlows = []*ofp.OfpFlowStats{
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270001, PacketCount: 0},
		{Id: 128, TableId: 1280, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1280000, PacketCount: 0},
	}

	expectedFlows = []*ofp.OfpFlowStats{
		{Id: 123, TableId: 1230, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1230000, PacketCount: 0},
		{Id: 124, TableId: 1240, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1240000, PacketCount: 0},
		{Id: 125, TableId: 1250, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1250000, PacketCount: 0},
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270001, PacketCount: 0},
		{Id: 128, TableId: 1280, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1280000, PacketCount: 0},
	}

	err = da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daFlows = changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(expectedFlows, daFlows))

	//Add already existing flows again
	newFlows = []*ofp.OfpFlowStats{
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270001, PacketCount: 0},
		{Id: 128, TableId: 1280, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1280000, PacketCount: 0},
	}

	expectedFlows = []*ofp.OfpFlowStats{
		{Id: 123, TableId: 1230, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1230000, PacketCount: 0},
		{Id: 124, TableId: 1240, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1240000, PacketCount: 0},
		{Id: 125, TableId: 1250, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1250000, PacketCount: 0},
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270001, PacketCount: 0},
		{Id: 128, TableId: 1280, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1280000, PacketCount: 0},
	}

	err = da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daFlows = changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(expectedFlows, daFlows))

	//Delete flows
	flowsToDelete := []*ofp.OfpFlowStats{
		{Id: 126, TableId: 1260, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1260000, PacketCount: 0},
		{Id: 127, TableId: 1270, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1270001, PacketCount: 0},
		{Id: 128, TableId: 1280, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1280000, PacketCount: 0},
	}

	expectedFlows = []*ofp.OfpFlowStats{
		{Id: 123, TableId: 1230, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1230000, PacketCount: 0},
		{Id: 124, TableId: 1240, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1240000, PacketCount: 0},
		{Id: 125, TableId: 1250, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1250000, PacketCount: 0},
	}

	err = da.deleteFlowsAndGroups(context.Background(), flowsToDelete, []*ofp.OfpGroupEntry{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daFlows = changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(expectedFlows, daFlows))
	//Delete flows with an unexisting one
	flowsToDelete = []*ofp.OfpFlowStats{
		{Id: 125, TableId: 1250, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1250000, PacketCount: 0},
		{Id: 129, TableId: 1290, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1290000, PacketCount: 0},
	}

	expectedFlows = []*ofp.OfpFlowStats{
		{Id: 123, TableId: 1230, Priority: 100, IdleTimeout: 0, Flags: 0, Cookie: 1230000, PacketCount: 0},
		{Id: 124, TableId: 1240, Priority: 1000, IdleTimeout: 0, Flags: 0, Cookie: 1240000, PacketCount: 0},
	}

	err = da.deleteFlowsAndGroups(context.Background(), flowsToDelete, []*ofp.OfpGroupEntry{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daFlows = changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(expectedFlows, daFlows))
}

func (dat *DATest) testGroupAddDeletes(t *testing.T, da *Agent) {
	//Add new Groups on empty list
	newGroups := []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
	}
	err := da.addFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, newGroups, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daGroups := changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(newGroups, daGroups))

	//Add new Groups on existing ones
	newGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 3, GroupId: 30, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 4, GroupId: 40, Buckets: nil}},
	}
	expectedGroups := []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 3, GroupId: 30, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 4, GroupId: 40, Buckets: nil}},
	}
	err = da.addFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, newGroups, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daGroups = changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(expectedGroups, daGroups))

	//Add new Groups on existing ones
	newGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 3, GroupId: 30, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 44, GroupId: 40, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 5, GroupId: 50, Buckets: nil}},
	}
	expectedGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 3, GroupId: 30, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 44, GroupId: 40, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 5, GroupId: 50, Buckets: nil}},
	}
	err = da.addFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, newGroups, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daGroups = changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(expectedGroups, daGroups))

	//Modify Group
	updtGroups := []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 33, GroupId: 30, Buckets: nil}},
	}
	expectedGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 33, GroupId: 30, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 44, GroupId: 40, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 5, GroupId: 50, Buckets: nil}},
	}
	err = da.updateFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, updtGroups, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daGroups = changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(expectedGroups, daGroups))

	//Delete Group
	delGroups := []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 33, GroupId: 30, Buckets: nil}},
	}
	expectedGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 44, GroupId: 40, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 5, GroupId: 50, Buckets: nil}},
	}
	err = da.deleteFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, delGroups, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daGroups = changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(expectedGroups, daGroups))

	//Delete Group
	delGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 4, GroupId: 40, Buckets: nil}},
	}
	expectedGroups = []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 5, GroupId: 50, Buckets: nil}},
	}
	err = da.deleteFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, delGroups, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	daGroups = changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(expectedGroups, daGroups))
}
