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
	com "github.com/opencord/voltha-lib-go/v2/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	lm "github.com/opencord/voltha-lib-go/v2/pkg/mocks"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

type DATest struct {
	etcdServer     *lm.EtcdServer
	core           *Core
	kClient        kafka.Client
	kvClientPort   int
	oltAdapterName string
	onuAdapterName string
	coreInstanceID string
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	device         *voltha.Device
	done           chan int
}

func newDATest() *DATest {
	test := &DATest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = startEmbeddedEtcdServer("voltha.rwcore.da.test", "voltha.rwcore.da.etcd", "error")
	if err != nil {
		log.Fatal(err)
	}
	// Create the kafka client
	test.kClient = lm.NewKafkaClient()
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
	cfg.DefaultRequestTimeout = dat.defaultTimeout.Nanoseconds() / 1000000 //TODO: change when Core changes to Duration
	cfg.KVStorePort = dat.kvClientPort
	cfg.InCompetingMode = inCompeteMode
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		log.Fatal("Cannot get a freeport for grpc")
	}
	cfg.GrpcPort = grpcPort
	cfg.GrpcHost = "127.0.0.1"
	setCoreCompeteMode(inCompeteMode)
	client := setupKVClient(cfg, dat.coreInstanceID)
	dat.core = NewCore(context.Background(), dat.coreInstanceID, cfg, client, dat.kClient)
	dat.core.Start(context.Background())
}

func (dat *DATest) stopAll() {
	if dat.kClient != nil {
		dat.kClient.Stop()
	}
	if dat.core != nil {
		dat.core.Stop(context.Background())
	}
	if dat.etcdServer != nil {
		stopEmbeddedEtcdServer(dat.etcdServer)
	}
}

func (dat *DATest) createDeviceAgent(t *testing.T) *DeviceAgent {
	deviceMgr := dat.core.deviceMgr
	clonedDevice := proto.Clone(dat.device).(*voltha.Device)
	deviceAgent := newDeviceAgent(deviceMgr.adapterProxy, clonedDevice, deviceMgr, deviceMgr.clusterDataProxy, deviceMgr.defaultTimeout)
	d, err := deviceAgent.start(context.TODO(), clonedDevice)
	assert.Nil(t, err)
	assert.NotNil(t, d)
	deviceMgr.addDeviceAgentToMap(deviceAgent)
	return deviceAgent
}

func (dat *DATest) updateDeviceConcurrently(t *testing.T, da *DeviceAgent, globalWG *sync.WaitGroup) {
	originalDevice := da.getDevice()
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
		err := da.updateDeviceUsingAdapterData(deviceToUpdate)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Update the device status routine
	localWG.Add(1)
	go func() {
		err := da.updateDeviceStatus(voltha.OperStatus_ACTIVE, voltha.ConnectStatus_REACHABLE)
		assert.Nil(t, err)
		localWG.Done()
	}()

	// Add a port routine
	localWG.Add(1)
	go func() {
		err := da.addPort(portToAdd)
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

	updatedDevice := da.getDevice()
	assert.NotNil(t, updatedDevice)
	assert.True(t, proto.Equal(expectedChange, updatedDevice))

	globalWG.Done()
}

func TestConcurrentDevices(t *testing.T) {
	da := newDATest()
	assert.NotNil(t, da)
	defer da.stopAll()

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
