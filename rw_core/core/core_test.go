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
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/config"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	lm "github.com/opencord/voltha-lib-go/v2/pkg/mocks"
	"github.com/opencord/voltha-lib-go/v2/pkg/version"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type CoreTest struct {
	etcdServer     *lm.EtcdServer
	core           *rwCoreUnderTest
	kClient        kafka.Client
	kvClientPort   int
	numONUPerOLT   int
	oltAdapterName string
	onuAdapterName string
	coreInstanceId string
	defaultTimeout time.Duration
	maxTimeout     time.Duration
}

func newCoreTest() *CoreTest {
	test := &CoreTest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = startEmbeddedEtcdServer("voltha.rwcore.core.test", "voltha.rwcore.core.etcd", "error")
	if err != nil {
		log.Fatal(err)
	}
	// Create the kafka client
	test.kClient = lm.NewKafkaClient()
	test.oltAdapterName = "olt_adapter_mock"
	test.onuAdapterName = "onu_adapter_mock"
	test.coreInstanceId = "rw-core-test"
	test.defaultTimeout = 5 * time.Second
	test.maxTimeout = 20 * time.Second
	return test
}

func (ct *CoreTest) startCore(inCompeteMode bool) {
	cfg := config.NewRWCoreFlags()
	cfg.CorePairTopic = "rw_core"
	cfg.DefaultRequestTimeout = ct.defaultTimeout.Nanoseconds() / 1000000 //TODO: change when Core changes to Duration
	cfg.KVStorePort = ct.kvClientPort
	cfg.InCompetingMode = inCompeteMode
	client := setupKVClient(cfg, ct.coreInstanceId)
	ct.core = newRWCoreUnderTest(ct.coreInstanceId, cfg, client, ct.kClient)
	ct.core.Start(context.Background())
}

func (ct *CoreTest) createAndregisterAdapters() {
	// Setup the mock OLT adapter
	oltAdapter, err := createMockAdapter(OltAdapter, ct.kClient, ct.coreInstanceId, coreName, ct.oltAdapterName)
	if err != nil {
		log.Fatalw("setting-mock-olt-adapter-failed", log.Fields{"error": err})
	}
	if adapter, ok := (oltAdapter).(*cm.OLTAdapter); ok {
		ct.numONUPerOLT = adapter.GetNumONUPerOLT()
	}
	//	Register the adapter
	registrationData := &voltha.Adapter{
		Id:      ct.oltAdapterName,
		Vendor:  "Voltha-olt",
		Version: version.VersionInfo.Version,
	}
	types := []*voltha.DeviceType{{Id: ct.oltAdapterName, Adapter: ct.oltAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes := &voltha.DeviceTypes{Items: types}
	ct.core.GetAdapterManager().RegisterAdapter(registrationData, deviceTypes)

	// Setup the mock ONU adapter
	if _, err := createMockAdapter(OnuAdapter, ct.kClient, ct.coreInstanceId, coreName, ct.onuAdapterName); err != nil {
		log.Fatalw("setting-mock-onu-adapter-failed", log.Fields{"error": err})
	}
	//	Register the adapter
	registrationData = &voltha.Adapter{
		Id:      ct.onuAdapterName,
		Vendor:  "Voltha-onu",
		Version: version.VersionInfo.Version,
	}
	types = []*voltha.DeviceType{{Id: ct.onuAdapterName, Adapter: ct.onuAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes = &voltha.DeviceTypes{Items: types}
	ct.core.GetAdapterManager().RegisterAdapter(registrationData, deviceTypes)
}

func (ct *CoreTest) stopCore() {
	if ct.kClient != nil {
		ct.kClient.Stop()
	}
	if ct.core != nil {
		ct.core.Stop(context.Background())
	}
	ct.core = nil
}

func (ct *CoreTest) stopAll() {
	if ct.kClient != nil {
		ct.kClient.Stop()
	}
	if ct.core != nil {
		ct.core.Stop(context.Background())
	}
	if ct.etcdServer != nil {
		stopEmbeddedEtcdServer(ct.etcdServer)
	}
}

func (ct *CoreTest) createAndEnable(t *testing.T, nbi *APIHandler) {
	//	Create the oltDevice
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: ct.oltAdapterName, MacAddress: "aa:bb:cc:cc:ee:ee"})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Verify oltDevice exist in the core
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(devices.Items))
	assert.Equal(t, 1, len(devices.Items))
	assert.Equal(t, oltDevice.Id, devices.Items[0].Id)

	// Enable the oltDevice
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the logical device to be in the ready state
	var vFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
		return ld != nil && len(ld.Ports) == ct.numONUPerOLT+1
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, ct.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)

	// The tests in grpc_nbi_api_handler_test.go covers the validation of device and logical device
}

func testNoKVConnection(t *testing.T) {
	ct := newCoreTest()
	assert.NotNil(t, ct)
	defer ct.stopAll()

	cfg := config.NewRWCoreFlags()
	// Get the KC client to try and connect to a fake port
	cfg.KVStorePort = ct.kvClientPort + 10
	cfg.InCompetingMode = false
	client := setupKVClient(cfg, ct.coreInstanceId)
	ct.core = newRWCoreUnderTest(ct.coreInstanceId, cfg, client, ct.kClient)
	ct.core.Start(context.Background())

	//	Ensure we get an error back
	err := ct.core.waitUntilKVStoreReachableOrMaxTries(context.Background(), 3, 2*time.Second)
	assert.NotNil(t, err)

	//	Update the KV client with the correct port
	cfg.KVStorePort = ct.kvClientPort
	ct.core.kvClient = setupKVClient(cfg, ct.coreInstanceId)
	//	Verify the connection is up
	err = ct.core.waitUntilKVStoreReachableOrMaxTries(context.Background(), 3, 2*time.Second)
	assert.Nil(t, err)
}

func testCoreRestart(t *testing.T) {
	ct := newCoreTest()
	assert.NotNil(t, ct)
	defer ct.stopAll()

	// Start the Core
	ct.startCore(false)

	// Set the grpc API interface - no grpc server is running in unit test
	nbi := NewAPIHandler(ct.core)

	// Create/register the adapters
	ct.createAndregisterAdapters()

	//Create/Enable a device and verify.
	ct.createAndEnable(t, nbi)

	//Keep a list of existing devices
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	dMap := make(map[string]*voltha.Device)
	for _, d := range devices.Items {
		dMap[d.Id] = d
	}

	ldsBefore, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ldsBefore.Items))

	//3. Mock a Core restart - stop and recreate a Core (cTCoreUT)
	ct.stopCore()
	ct.startCore(false)
	nbi = NewAPIHandler(ct.core)

	// Verify that the devices are still present
	devices, err = nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)
	assert.Equal(t, 1+ct.numONUPerOLT, len(devices.Items))
	for _, d := range devices.Items {
		assert.Equal(t, dMap[d.Id].String(), d.String())
	}

	// Verify that the logical device is still present
	ldsAfter, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, ldsAfter)
	assert.Equal(t, 1, len(ldsAfter.Items))
	assert.Equal(t, ldsBefore.Items[0].String(), ldsAfter.Items[0].String())
}

func TestCore(t *testing.T) {
	// This is just a very limited set of test just to show some of the tests that are feasible

	//1. test core with no connection to Etcd
	testNoKVConnection(t)

	//2. test a core restart
	testCoreRestart(t)

	//x. TODO - Add more tests
}
