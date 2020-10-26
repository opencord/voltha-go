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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	tst "github.com/opencord/voltha-go/rw_core/test"
	com "github.com/opencord/voltha-lib-go/v4/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	fu "github.com/opencord/voltha-lib-go/v4/pkg/flows"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	mock_etcd "github.com/opencord/voltha-lib-go/v4/pkg/mocks/etcd"
	mock_kafka "github.com/opencord/voltha-lib-go/v4/pkg/mocks/kafka"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

type LDATest struct {
	etcdServer       *mock_etcd.EtcdServer
	deviceMgr        *Manager
	kmp              kafka.InterContainerProxy
	logicalDeviceMgr *LogicalManager
	kClient          kafka.Client
	kvClientPort     int
	oltAdapterName   string
	onuAdapterName   string
	coreInstanceID   string
	defaultTimeout   time.Duration
	maxTimeout       time.Duration
	logicalDevice    *voltha.LogicalDevice
	logicalPorts     map[uint32]*voltha.LogicalPort
	deviceIds        []string
	done             chan int
}

func newLDATest(ctx context.Context) *LDATest {
	test := &LDATest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = tst.StartEmbeddedEtcdServer(ctx, "voltha.rwcore.lda.test", "voltha.rwcore.lda.etcd", "error")
	if err != nil {
		logger.Fatal(ctx, err)
	}
	// Create the kafka client
	test.kClient = mock_kafka.NewKafkaClient()
	test.oltAdapterName = "olt_adapter_mock"
	test.onuAdapterName = "onu_adapter_mock"
	test.coreInstanceID = "rw-da-test"
	test.defaultTimeout = 5 * time.Second
	test.maxTimeout = 20 * time.Second
	test.done = make(chan int)
	test.deviceIds = []string{com.GetRandomString(10), com.GetRandomString(10), com.GetRandomString(10)}
	test.logicalDevice = &voltha.LogicalDevice{
		Desc: &ofp.OfpDesc{
			HwDesc:    "olt_adapter_mock",
			SwDesc:    "olt_adapter_mock",
			SerialNum: com.GetRandomSerialNumber(),
		},
		SwitchFeatures: &ofp.OfpSwitchFeatures{
			NBuffers: 256,
			NTables:  2,
			Capabilities: uint32(ofp.OfpCapabilities_OFPC_FLOW_STATS |
				ofp.OfpCapabilities_OFPC_TABLE_STATS |
				ofp.OfpCapabilities_OFPC_PORT_STATS |
				ofp.OfpCapabilities_OFPC_GROUP_STATS),
		},
		RootDeviceId: test.deviceIds[0],
	}
	test.logicalPorts = map[uint32]*voltha.LogicalPort{
		1: {
			Id:           "1001",
			DeviceId:     test.deviceIds[0],
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 4,
				State:  4,
			},
		},
		2: {
			Id:           "1002",
			DeviceId:     test.deviceIds[1],
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 4,
				State:  4,
			},
		},
		3: {
			Id:           "1003",
			DeviceId:     test.deviceIds[2],
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 4,
				State:  4,
			},
		},
	}
	return test
}

func (lda *LDATest) startCore(ctx context.Context, inCompeteMode bool) {
	cfg := config.NewRWCoreFlags()
	cfg.CoreTopic = "rw_core"
	cfg.DefaultRequestTimeout = lda.defaultTimeout
	cfg.KVStoreAddress = "127.0.0.1" + ":" + strconv.Itoa(lda.kvClientPort)
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for grpc")
	}
	cfg.GrpcAddress = "127.0.0.1" + ":" + strconv.Itoa(grpcPort)
	client := tst.SetupKVClient(ctx, cfg, lda.coreInstanceID)
	backend := &db.Backend{
		Client:                  client,
		StoreType:               cfg.KVStoreType,
		Address:                 cfg.KVStoreAddress,
		Timeout:                 cfg.KVStoreTimeout,
		LivenessChannelInterval: cfg.LiveProbeInterval / 2}
	lda.kmp = kafka.NewInterContainerProxy(
		kafka.InterContainerAddress(cfg.KafkaAdapterAddress),
		kafka.MsgClient(lda.kClient),
		kafka.DefaultTopic(&kafka.Topic{Name: cfg.CoreTopic}))

	endpointMgr := kafka.NewEndpointManager(backend)
	proxy := model.NewDBPath(backend)
	adapterMgr := adapter.NewAdapterManager(ctx, proxy, lda.coreInstanceID, lda.kClient)

	lda.deviceMgr, lda.logicalDeviceMgr = NewManagers(proxy, adapterMgr, lda.kmp, endpointMgr, cfg.CoreTopic, lda.coreInstanceID, cfg.DefaultCoreTimeout)
	if err = lda.kmp.Start(ctx); err != nil {
		logger.Fatal(ctx, "Cannot start InterContainerProxy")
	}
	adapterMgr.Start(context.Background())
}

func (lda *LDATest) stopAll(ctx context.Context) {
	if lda.kClient != nil {
		lda.kClient.Stop(ctx)
	}
	if lda.kmp != nil {
		lda.kmp.Stop(ctx)
	}
	if lda.etcdServer != nil {
		tst.StopEmbeddedEtcdServer(ctx, lda.etcdServer)
	}
}

func (lda *LDATest) createLogicalDeviceAgent(t *testing.T) *LogicalAgent {
	lDeviceMgr := lda.logicalDeviceMgr
	deviceMgr := lda.deviceMgr
	clonedLD := proto.Clone(lda.logicalDevice).(*voltha.LogicalDevice)
	clonedLD.Id = com.GetRandomString(10)
	clonedLD.DatapathId = rand.Uint64()
	lDeviceAgent := newLogicalAgent(context.Background(), clonedLD.Id, clonedLD.Id, clonedLD.RootDeviceId, lDeviceMgr, deviceMgr, lDeviceMgr.dbPath, lDeviceMgr.ldProxy, lDeviceMgr.defaultTimeout)
	lDeviceAgent.logicalDevice = clonedLD
	for _, port := range lda.logicalPorts {
		clonedPort := proto.Clone(port).(*voltha.LogicalPort)
		handle, created, err := lDeviceAgent.portLoader.LockOrCreate(context.Background(), clonedPort)
		if err != nil {
			panic(err)
		}
		handle.Unlock()
		if !created {
			t.Errorf("port %d already exists", clonedPort.OfpPort.PortNo)
		}
	}
	err := lDeviceAgent.ldProxy.Set(context.Background(), clonedLD.Id, clonedLD)
	assert.Nil(t, err)
	lDeviceMgr.addLogicalDeviceAgentToMap(lDeviceAgent)
	return lDeviceAgent
}

func (lda *LDATest) updateLogicalDeviceConcurrently(t *testing.T, ldAgent *LogicalAgent, globalWG *sync.WaitGroup) {
	originalLogicalPorts := ldAgent.listLogicalDevicePorts(context.Background())
	assert.NotNil(t, originalLogicalPorts)
	var localWG sync.WaitGroup

	// Change the state of the first port to FAILED
	localWG.Add(1)
	go func() {
		err := ldAgent.updatePortState(context.Background(), 1, voltha.OperStatus_FAILED)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Change the state of the second port to TESTING
	localWG.Add(1)
	go func() {
		err := ldAgent.updatePortState(context.Background(), 2, voltha.OperStatus_TESTING)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Change the state of the third port to UNKNOWN and then back to ACTIVE
	localWG.Add(1)
	go func() {
		err := ldAgent.updatePortState(context.Background(), 3, voltha.OperStatus_UNKNOWN)
		assert.Nil(t, err)
		err = ldAgent.updatePortState(context.Background(), 3, voltha.OperStatus_ACTIVE)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Add a meter to the logical device
	meterMod := &ofp.OfpMeterMod{
		Command: ofp.OfpMeterModCommand_OFPMC_ADD,
		Flags:   rand.Uint32(),
		MeterId: rand.Uint32(),
		Bands: []*ofp.OfpMeterBandHeader{
			{Type: ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER,
				Rate:      rand.Uint32(),
				BurstSize: rand.Uint32(),
				Data:      nil,
			},
		},
	}
	localWG.Add(1)
	ctx := context.Background()
	go func() {
		err := ldAgent.meterAdd(ctx, meterMod)
		assert.Nil(t, err)
		localWG.Done()
	}()
	// wait for go routines to be done
	localWG.Wait()
	meterEntry := fu.MeterEntryFromMeterMod(ctx, meterMod)

	meterHandle, have := ldAgent.meterLoader.Lock(meterMod.MeterId)
	assert.Equal(t, have, true)
	if have {
		assert.True(t, proto.Equal(meterEntry, meterHandle.GetReadOnly()))
		meterHandle.Unlock()
	}

	expectedLogicalPorts := make(map[uint32]*voltha.LogicalPort)
	for _, port := range originalLogicalPorts {
		clonedPort := proto.Clone(port).(*voltha.LogicalPort)
		switch clonedPort.OfpPort.PortNo {
		case 1:
			clonedPort.OfpPort.Config = originalLogicalPorts[1].OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
			clonedPort.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
		case 2:
			clonedPort.OfpPort.Config = originalLogicalPorts[1].OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
			clonedPort.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
		case 3:
			clonedPort.OfpPort.Config = originalLogicalPorts[1].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
			clonedPort.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
		}
		expectedLogicalPorts[clonedPort.OfpPort.PortNo] = clonedPort
	}

	updatedLogicalDevicePorts := ldAgent.listLogicalDevicePorts(ctx)
	assert.Equal(t, len(expectedLogicalPorts), len(updatedLogicalDevicePorts))
	for _, p := range updatedLogicalDevicePorts {
		assert.True(t, proto.Equal(p, expectedLogicalPorts[p.OfpPort.PortNo]))
	}
	globalWG.Done()
}

func TestConcurrentLogicalDeviceUpdate(t *testing.T) {
	ctx := context.Background()
	lda := newLDATest(ctx)
	assert.NotNil(t, lda)
	defer lda.stopAll(ctx)

	// Start the Core
	lda.startCore(ctx, false)

	var wg sync.WaitGroup
	numConCurrentLogicalDeviceAgents := 3
	for i := 0; i < numConCurrentLogicalDeviceAgents; i++ {
		wg.Add(1)
		a := lda.createLogicalDeviceAgent(t)
		go lda.updateLogicalDeviceConcurrently(t, a, &wg)
	}

	wg.Wait()
}
