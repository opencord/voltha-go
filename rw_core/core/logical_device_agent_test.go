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
package core

import (
	"context"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/config"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	lm "github.com/opencord/voltha-lib-go/v3/pkg/mocks"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestLogicalDeviceAgent_diff_nochange_1(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{}
	updatedLogicalPorts := []*voltha.LogicalPort{}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
}

func TestLogicalDeviceAgent_diff_nochange_2(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
}

func TestLogicalDeviceAgent_diff_add(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 2, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
	assert.Equal(t, updatedLogicalPorts[0], newPorts[0])
	assert.Equal(t, updatedLogicalPorts[1], newPorts[1])
}

func TestLogicalDeviceAgent_diff_delete(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 1, len(deletedPorts))
	assert.Equal(t, currentLogicalPorts[0], deletedPorts[0])
}

func TestLogicalDeviceAgent_diff_changed(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 2, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
	assert.Equal(t, updatedLogicalPorts[0], changedPorts[0])
	assert.Equal(t, updatedLogicalPorts[1], changedPorts[1])
}

func TestLogicalDeviceAgent_diff_mix(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1234",
			DeviceId:     "d1234",
			DevicePortNo: 4,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 4,
				Name:   "port4",
				Config: 4,
				State:  4,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 1, len(newPorts))
	assert.Equal(t, 2, len(changedPorts))
	assert.Equal(t, 1, len(deletedPorts))
	assert.Equal(t, updatedLogicalPorts[0], changedPorts[0])
	assert.Equal(t, updatedLogicalPorts[1], changedPorts[1])
	assert.Equal(t, currentLogicalPorts[2], deletedPorts[0])
}

type LDATest struct {
	etcdServer     *lm.EtcdServer
	core           *Core
	kClient        kafka.Client
	kvClientPort   int
	oltAdapterName string
	onuAdapterName string
	coreInstanceID string
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	logicalDevice  *voltha.LogicalDevice
	deviceIds      []string
	done           chan int
}

func newLDATest() *LDATest {
	test := &LDATest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = startEmbeddedEtcdServer("voltha.rwcore.lda.test", "voltha.rwcore.lda.etcd", "error")
	if err != nil {
		logger.Fatal(err)
	}
	// Create the kafka client
	test.kClient = lm.NewKafkaClient()
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
		Ports: []*voltha.LogicalPort{
			{
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
			{
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
			{
				Id:           "1003",
				DeviceId:     test.deviceIds[2],
				DevicePortNo: 3,
				RootPort:     false,
				OfpPort: &ofp.OfpPort{
					PortNo: 4,
					Name:   "port3",
					Config: 4,
					State:  4,
				},
			},
		},
	}
	return test
}

func (lda *LDATest) startCore(inCompeteMode bool) {
	ctx := context.Background()
	cfg := config.NewRWCoreFlags()
	cfg.CorePairTopic = "rw_core"
	cfg.DefaultRequestTimeout = lda.defaultTimeout
	cfg.KVStorePort = lda.kvClientPort
	cfg.InCompetingMode = inCompeteMode
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal("Cannot get a freeport for grpc")
	}
	cfg.GrpcPort = grpcPort
	cfg.GrpcHost = "127.0.0.1"
	setCoreCompeteMode(inCompeteMode)
	client := setupKVClient(cfg, lda.coreInstanceID)
	lda.core = NewCore(ctx, lda.coreInstanceID, cfg, client, lda.kClient)
	err = lda.core.Start(ctx)
	if err != nil {
		logger.Fatal("Cannot start core")
	}
}

func (lda *LDATest) stopAll() {
	if lda.kClient != nil {
		lda.kClient.Stop()
	}
	if lda.core != nil {
		lda.core.Stop(context.Background())
	}
	if lda.etcdServer != nil {
		stopEmbeddedEtcdServer(lda.etcdServer)
	}
}

func (lda *LDATest) createLogicalDeviceAgent(t *testing.T) *LogicalDeviceAgent {
	lDeviceMgr := lda.core.logicalDeviceMgr
	deviceMgr := lda.core.deviceMgr
	clonedLD := proto.Clone(lda.logicalDevice).(*voltha.LogicalDevice)
	clonedLD.Id = com.GetRandomString(10)
	clonedLD.DatapathId = rand.Uint64()
	lDeviceAgent := newLogicalDeviceAgent(clonedLD.Id, clonedLD.RootDeviceId, lDeviceMgr, deviceMgr, lDeviceMgr.clusterDataProxy, lDeviceMgr.defaultTimeout)
	lDeviceAgent.logicalDevice = clonedLD
	lDeviceAgent.requestQueue.Start()
	err := lDeviceAgent.clusterDataProxy.AddWithID(context.Background(), "/logical_devices", clonedLD.Id, clonedLD)
	assert.Nil(t, err)
	lDeviceMgr.addLogicalDeviceAgentToMap(lDeviceAgent)
	return lDeviceAgent
}

func (lda *LDATest) updateLogicalDeviceConcurrently(t *testing.T, ldAgent *LogicalDeviceAgent, globalWG *sync.WaitGroup) {
	originalLogicalDevice, _ := ldAgent.GetLogicalDevice(context.Background())
	assert.NotNil(t, originalLogicalDevice)
	var localWG sync.WaitGroup

	// Change the state of the first port to FAILED
	localWG.Add(1)
	go func() {
		err := ldAgent.updatePortState(context.Background(), lda.logicalDevice.Ports[0].DeviceId, lda.logicalDevice.Ports[0].DevicePortNo, voltha.OperStatus_FAILED)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Change the state of the second port to TESTING
	localWG.Add(1)
	go func() {
		err := ldAgent.updatePortState(context.Background(), lda.logicalDevice.Ports[1].DeviceId, lda.logicalDevice.Ports[1].DevicePortNo, voltha.OperStatus_TESTING)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Change the state of the third port to UNKNOWN and then back to ACTIVE
	localWG.Add(1)
	go func() {
		err := ldAgent.updatePortState(context.Background(), lda.logicalDevice.Ports[2].DeviceId, lda.logicalDevice.Ports[2].DevicePortNo, voltha.OperStatus_UNKNOWN)
		assert.Nil(t, err)
		err = ldAgent.updatePortState(context.Background(), lda.logicalDevice.Ports[2].DeviceId, lda.logicalDevice.Ports[2].DevicePortNo, voltha.OperStatus_ACTIVE)
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
	go func() {
		err := ldAgent.meterAdd(context.Background(), meterMod)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// wait for go routines to be done
	localWG.Wait()

	expectedChange := proto.Clone(originalLogicalDevice).(*voltha.LogicalDevice)
	expectedChange.Ports[0].OfpPort.Config = originalLogicalDevice.Ports[0].OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
	expectedChange.Ports[0].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
	expectedChange.Ports[1].OfpPort.Config = originalLogicalDevice.Ports[0].OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
	expectedChange.Ports[1].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
	expectedChange.Ports[2].OfpPort.Config = originalLogicalDevice.Ports[0].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
	expectedChange.Ports[2].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
	expectedChange.Meters = &voltha.Meters{Items: nil}
	expectedChange.Meters.Items = append(expectedChange.Meters.Items, fu.MeterEntryFromMeterMod(meterMod))
	updatedLogicalDevice, _ := ldAgent.GetLogicalDevice(context.Background())
	assert.NotNil(t, updatedLogicalDevice)
	assert.True(t, proto.Equal(expectedChange, updatedLogicalDevice))
	globalWG.Done()
}

func TestConcurrentLogicalDeviceUpdate(t *testing.T) {
	lda := newLDATest()
	assert.NotNil(t, lda)
	defer lda.stopAll()

	// Start the Core
	lda.startCore(false)

	var wg sync.WaitGroup
	numConCurrentLogicalDeviceAgents := 3
	for i := 0; i < numConCurrentLogicalDeviceAgents; i++ {
		wg.Add(1)
		a := lda.createLogicalDeviceAgent(t)
		go lda.updateLogicalDeviceConcurrently(t, a, &wg)
	}

	wg.Wait()
}
