/*
 * Copyright 2019-2024 Open Networking Foundation (ONF) and the ONF Contributors
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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ver "github.com/opencord/voltha-lib-go/v7/pkg/version"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	tst "github.com/opencord/voltha-go/rw_core/test"
	com "github.com/opencord/voltha-lib-go/v7/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	"github.com/opencord/voltha-lib-go/v7/pkg/events"
	"github.com/opencord/voltha-lib-go/v7/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	mock_etcd "github.com/opencord/voltha-lib-go/v7/pkg/mocks/etcd"
	mock_kafka "github.com/opencord/voltha-lib-go/v7/pkg/mocks/kafka"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

type DATest struct {
	etcdServer       *mock_etcd.EtcdServer
	deviceMgr        *Manager
	logicalDeviceMgr *LogicalManager
	adapterMgr       *adapter.Manager
	kClient          kafka.Client
	kEventClient     kafka.Client
	kvClientPort     int
	oltAdapterName   string
	onuAdapterName   string
	coreInstanceID   string
	internalTimeout  time.Duration
	device           *voltha.Device
	devicePorts      map[uint32]*voltha.Port
	done             chan int
}

func newDATest(ctx context.Context) *DATest {
	test := &DATest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = tst.StartEmbeddedEtcdServer(ctx, "voltha.rwcore.da.test", "voltha.rwcore.da.etcd", "error")
	if err != nil {
		logger.Fatal(ctx, err)
	}
	// Create the kafka client
	test.kClient = mock_kafka.NewKafkaClient()
	test.kEventClient = mock_kafka.NewKafkaClient()
	test.oltAdapterName = "olt-mock-adapter"
	test.onuAdapterName = "onu-mock-adapter"
	test.coreInstanceID = "rw-da-test"
	test.internalTimeout = 5 * time.Second
	test.done = make(chan int)
	parentID := com.GetRandomString(10)
	test.device = &voltha.Device{
		Type:         "onu-mock-device-type",
		ParentId:     parentID,
		ParentPortNo: 1,
		VendorId:     "onu-mock-vendor",
		Vlan:         100,
		Address:      nil,
		ProxyAddress: &voltha.Device_ProxyAddress{
			DeviceId:           parentID,
			DeviceType:         "olt-mock-device-type",
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
	}
	test.devicePorts = map[uint32]*voltha.Port{
		1: {PortNo: 1, Label: "pon-1", Type: voltha.Port_PON_ONU, AdminState: voltha.AdminState_ENABLED,
			OperStatus: voltha.OperStatus_ACTIVE, Peers: []*voltha.Port_PeerPort{{DeviceId: parentID, PortNo: 1}}},
		100: {PortNo: 100, Label: "uni-100", Type: voltha.Port_ETHERNET_UNI, AdminState: voltha.AdminState_ENABLED,
			OperStatus: voltha.OperStatus_ACTIVE},
	}
	return test
}

func (dat *DATest) startCore(ctx context.Context) {
	cfg := &config.RWCoreFlags{}
	cfg.ParseCommandArguments([]string{})
	cfg.EventTopic = "voltha.events"
	cfg.InternalTimeout = dat.internalTimeout
	cfg.KVStoreAddress = "127.0.0.1" + ":" + strconv.Itoa(dat.kvClientPort)
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for grpc")
	}
	cfg.GrpcNBIAddress = "127.0.0.1" + ":" + strconv.Itoa(grpcPort)
	client := tst.SetupKVClient(ctx, cfg, dat.coreInstanceID)
	backend := &db.Backend{
		Client:                  client,
		StoreType:               cfg.KVStoreType,
		Address:                 cfg.KVStoreAddress,
		Timeout:                 cfg.KVStoreTimeout,
		LivenessChannelInterval: cfg.LiveProbeInterval / 2}

	proxy := model.NewDBPath(backend)
	dat.adapterMgr = adapter.NewAdapterManager("test-endpoint", proxy, dat.coreInstanceID, backend, 5, 6, 5)
	eventProxy := events.NewEventProxy(events.MsgClient(dat.kEventClient), events.MsgTopic(kafka.Topic{Name: cfg.EventTopic}))
	dat.deviceMgr, dat.logicalDeviceMgr = NewManagers(proxy, dat.adapterMgr, cfg, dat.coreInstanceID, eventProxy)
	dat.adapterMgr.Start(context.Background(), "agent-test")
	dat.registerAdapters(context.Background())
	log.SetAllLogLevel(log.FatalLevel)
}

func (dat *DATest) stopAll(ctx context.Context) {
	if dat.kClient != nil {
		dat.kClient.Stop(ctx)
	}
	if dat.etcdServer != nil {
		tst.StopEmbeddedEtcdServer(ctx, dat.etcdServer)
	}
	if dat.kEventClient != nil {
		dat.kEventClient.Stop(ctx)
	}
}

func (dat *DATest) createDeviceAgent(t *testing.T) *Agent {
	deviceMgr := dat.deviceMgr
	clonedDevice := proto.Clone(dat.device).(*voltha.Device)
	deviceAgent := newAgent(clonedDevice, deviceMgr, deviceMgr.dbPath, deviceMgr.dProxy, deviceMgr.internalTimeout, deviceMgr.rpcTimeout, deviceMgr.flowTimeout)
	d, err := deviceAgent.start(context.TODO(), false, clonedDevice)
	assert.Nil(t, err)
	assert.NotNil(t, d)
	for _, port := range dat.devicePorts {
		err := deviceAgent.addPort(context.TODO(), port)
		assert.Nil(t, err)
	}
	deviceMgr.addDeviceAgentToMap(deviceAgent)
	return deviceAgent
}

func (dat *DATest) updateDeviceConcurrently(t *testing.T, da *Agent, globalWG *sync.WaitGroup) {
	originalDevice, err := da.getDeviceReadOnly(context.Background())
	originalDevicePorts := da.listDevicePorts()
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
		deviceToUpdate.OperStatus = voltha.OperStatus_ACTIVE
		deviceToUpdate.ConnectStatus = voltha.ConnectStatus_REACHABLE
		err := da.updateDeviceUsingAdapterData(context.Background(), deviceToUpdate)
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
	expectedChange.Root = root
	expectedChange.Vendor = vendor
	expectedChange.Model = model
	expectedChange.SerialNumber = serialNumber
	expectedChange.MacAddress = macAddress
	expectedChange.Vlan = vlan
	expectedChange.Reason = reason

	updatedDevice, _ := da.getDeviceReadOnly(context.Background())
	updatedDevicePorts := da.listDevicePorts()
	assert.NotNil(t, updatedDevice)
	assert.True(t, proto.Equal(expectedChange, updatedDevice))
	assert.Equal(t, len(originalDevicePorts)+1, len(updatedDevicePorts))
	assert.True(t, proto.Equal(updatedDevicePorts[portToAdd.PortNo], portToAdd))

	globalWG.Done()
}

func TestConcurrentDevices(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		da := newDATest(ctx)
		assert.NotNil(t, da)
		defer da.stopAll(ctx)

		// Start the Core
		da.startCore(ctx)

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
	ctx := context.Background()
	da := newDATest(ctx)
	assert.NotNil(t, da)
	defer da.stopAll(ctx)

	log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)
	// Start the Core
	da.startCore(ctx)
	a := da.createDeviceAgent(t)
	err1 := a.requestQueue.WaitForGreenLight(ctx)
	assert.Nil(t, err1)
	cloned := a.cloneDeviceWithoutLock()
	cloned.AdminState, cloned.ConnectStatus, cloned.OperStatus = voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE
	err2 := a.updateDeviceAndReleaseLock(ctx, cloned)
	assert.Nil(t, err2)
	da.testFlowAddDeletes(t, a)
}

func TestGroupUpdates(t *testing.T) {
	ctx := context.Background()
	da := newDATest(ctx)
	assert.NotNil(t, da)
	defer da.stopAll(ctx)

	// Start the Core
	da.startCore(ctx)
	a := da.createDeviceAgent(t)
	err1 := a.requestQueue.WaitForGreenLight(ctx)
	assert.Nil(t, err1)
	cloned := a.cloneDeviceWithoutLock()
	cloned.AdminState, cloned.ConnectStatus, cloned.OperStatus = voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE
	err2 := a.updateDeviceAndReleaseLock(ctx, cloned)
	assert.Nil(t, err2)
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
	err := da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &ofp.FlowMetadata{})
	// Expect specific error as adapter communication, for unit tests, are not set
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "flow-failure-device-"))
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

	err = da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "flow-failure-device-"))
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

	err = da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
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

	err = da.addFlowsAndGroups(context.Background(), newFlows, []*ofp.OfpGroupEntry{}, &ofp.FlowMetadata{})
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

	err = da.deleteFlowsAndGroups(context.Background(), flowsToDelete, []*ofp.OfpGroupEntry{}, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
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

	err = da.deleteFlowsAndGroups(context.Background(), flowsToDelete, []*ofp.OfpGroupEntry{}, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
	daFlows = changeToFlowList(da.listDeviceFlows())
	assert.True(t, isFlowSliceEqual(expectedFlows, daFlows))
}

func (dat *DATest) testGroupAddDeletes(t *testing.T, da *Agent) {
	//Add new Groups on empty list
	newGroups := []*ofp.OfpGroupEntry{
		{Desc: &ofp.OfpGroupDesc{Type: 1, GroupId: 10, Buckets: nil}},
		{Desc: &ofp.OfpGroupDesc{Type: 2, GroupId: 20, Buckets: nil}},
	}
	err := da.addFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, newGroups, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "flow-failure-device-"))
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
	err = da.addFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, newGroups, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
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
	err = da.addFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, newGroups, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
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
	err = da.updateFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, updtGroups, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
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
	err = da.deleteFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, delGroups, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
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
	err = da.deleteFlowsAndGroups(context.Background(), []*ofp.OfpFlowStats{}, delGroups, &ofp.FlowMetadata{})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "Aborted"))
	daGroups = changeToGroupList(da.listDeviceGroups())
	assert.True(t, isGroupSliceEqual(expectedGroups, daGroups))
}

// registerAdapters registers the ONU and OLT adapters
func (dat *DATest) registerAdapters(ctx context.Context) {
	oltAdapter := &voltha.Adapter{
		Id:             "olt-mock-adapter-1",
		Vendor:         "olt-mock-vendor",
		Version:        ver.VersionInfo.Version,
		Type:           "olt-mock-adapter-type",
		CurrentReplica: 1,
		TotalReplicas:  1,
		Endpoint:       "mock-olt-endpoint",
	}
	types := []*voltha.DeviceType{{Id: "olt-mock-device-type", AdapterType: "olt-mock-adapter-type", AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes := &voltha.DeviceTypes{Items: types}
	_, err := dat.adapterMgr.RegisterAdapter(ctx, &ca.AdapterRegistration{
		Adapter: oltAdapter,
		DTypes:  deviceTypes,
	})
	if err != nil {
		logger.Fatalw(ctx, "olt registration failed", log.Fields{"error": err})
	}

	onuAdapter := &voltha.Adapter{
		Id:             "onu-mock-adapter-1",
		Vendor:         "onu-mock-vendor",
		Version:        ver.VersionInfo.Version,
		Type:           "onu-mock-adapter-type",
		CurrentReplica: 1,
		TotalReplicas:  1,
		Endpoint:       "mock-onu-endpoint",
	}
	types = []*voltha.DeviceType{{Id: "onu-mock-device-type", AdapterType: "onu-mock-adapter-type", AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes = &voltha.DeviceTypes{Items: types}
	_, err = dat.adapterMgr.RegisterAdapter(ctx, &ca.AdapterRegistration{
		Adapter: onuAdapter,
		DTypes:  deviceTypes,
	})
	if err != nil {
		logger.Fatalw(ctx, "onu registration failed", log.Fields{"error": err})
	}
}
