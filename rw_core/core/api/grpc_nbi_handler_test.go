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
package api

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	lm "github.com/opencord/voltha-lib-go/v3/pkg/mocks"
	"github.com/opencord/voltha-lib-go/v3/pkg/version"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	coreName = "rw_core"
)

type NBTest struct {
	etcdServer        *lm.EtcdServer
	deviceMgr         *device.Manager
	logicalDeviceMgr  *device.LogicalManager
	adapterMgr        *adapter.Manager
	kmp               kafka.InterContainerProxy
	kClient           kafka.Client
	kvClientPort      int
	numONUPerOLT      int
	startingUNIPortNo int
	oltAdapter        *cm.OLTAdapter
	onuAdapter        *cm.ONUAdapter
	oltAdapterName    string
	onuAdapterName    string
	coreInstanceID    string
	defaultTimeout    time.Duration
	maxTimeout        time.Duration
}

func newNBTest() *NBTest {
	test := &NBTest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = startEmbeddedEtcdServer("voltha.rwcore.nb.test", "voltha.rwcore.nb.etcd", "error")
	if err != nil {
		logger.Fatal(err)
	}
	// Create the kafka client
	test.kClient = lm.NewKafkaClient()
	test.oltAdapterName = "olt_adapter_mock"
	test.onuAdapterName = "onu_adapter_mock"
	test.coreInstanceID = "rw-nbi-test"
	test.defaultTimeout = 10 * time.Second
	test.maxTimeout = 20 * time.Second
	return test
}

func (nb *NBTest) startCore(inCompeteMode bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	cfg := config.NewRWCoreFlags()
	cfg.CorePairTopic = "rw_core"
	cfg.DefaultRequestTimeout = nb.defaultTimeout
	cfg.DefaultCoreTimeout = nb.defaultTimeout
	cfg.KVStorePort = nb.kvClientPort
	cfg.InCompetingMode = inCompeteMode
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal("Cannot get a freeport for grpc")
	}
	cfg.GrpcPort = grpcPort
	cfg.GrpcHost = "127.0.0.1"
	setCoreCompeteMode(inCompeteMode)
	client := setupKVClient(cfg, nb.coreInstanceID)
	backend := &db.Backend{
		Client:                  client,
		StoreType:               cfg.KVStoreType,
		Host:                    cfg.KVStoreHost,
		Port:                    cfg.KVStorePort,
		Timeout:                 cfg.KVStoreTimeout,
		LivenessChannelInterval: cfg.LiveProbeInterval / 2,
		PathPrefix:              cfg.KVStoreDataPrefix}
	nb.kmp = kafka.NewInterContainerProxy(
		kafka.InterContainerHost(cfg.KafkaAdapterHost),
		kafka.InterContainerPort(cfg.KafkaAdapterPort),
		kafka.MsgClient(nb.kClient),
		kafka.DefaultTopic(&kafka.Topic{Name: cfg.CoreTopic}),
		kafka.DeviceDiscoveryTopic(&kafka.Topic{Name: cfg.AffinityRouterTopic}))

	proxy := model.NewProxy(backend, "/")
	nb.adapterMgr = adapter.NewAdapterManager(proxy, nb.coreInstanceID, nb.kClient)
	nb.deviceMgr, nb.logicalDeviceMgr = device.NewDeviceManagers(proxy, nb.adapterMgr, nb.kmp, cfg.CorePairTopic, nb.coreInstanceID, cfg.DefaultCoreTimeout)
	if err = nb.adapterMgr.Start(ctx); err != nil {
		logger.Fatalf("Cannot start adapterMgr: %s", err)
	}
	nb.deviceMgr.Start(ctx)
	nb.logicalDeviceMgr.Start(ctx)

	if err = nb.kmp.Start(); err != nil {
		logger.Fatalf("Cannot start InterContainerProxy: %s", err)
	}
	requestProxy := NewAdapterRequestHandlerProxy(nb.coreInstanceID, nb.deviceMgr, nb.adapterMgr, proxy, proxy, cfg.LongRunningRequestTimeout, cfg.DefaultRequestTimeout)
	if err := nb.kmp.SubscribeWithRequestHandlerInterface(kafka.Topic{Name: cfg.CoreTopic}, requestProxy); err != nil {
		logger.Fatalf("Cannot add request handler: %s", err)
	}
	if err := nb.kmp.SubscribeWithDefaultRequestHandler(kafka.Topic{Name: cfg.CorePairTopic}, kafka.OffsetNewest); err != nil {
		logger.Fatalf("Cannot add default request handler: %s", err)
	}
}

func (nb *NBTest) createAndregisterAdapters(t *testing.T) {
	// Setup the mock OLT adapter
	oltAdapter, err := createMockAdapter(OltAdapter, nb.kClient, nb.coreInstanceID, coreName, nb.oltAdapterName)
	if err != nil {
		logger.Fatalw("setting-mock-olt-adapter-failed", log.Fields{"error": err})
	}
	nb.oltAdapter = (oltAdapter).(*cm.OLTAdapter)
	nb.numONUPerOLT = nb.oltAdapter.GetNumONUPerOLT()
	nb.startingUNIPortNo = nb.oltAdapter.GetStartingUNIPortNo()

	//	Register the adapter
	registrationData := &voltha.Adapter{
		Id:      nb.oltAdapterName,
		Vendor:  "Voltha-olt",
		Version: version.VersionInfo.Version,
	}
	types := []*voltha.DeviceType{{Id: nb.oltAdapterName, Adapter: nb.oltAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes := &voltha.DeviceTypes{Items: types}
	if _, err := nb.adapterMgr.RegisterAdapter(registrationData, deviceTypes); err != nil {
		logger.Errorw("failed-to-register-adapter", log.Fields{"error": err})
		assert.NotNil(t, err)
	}

	// Setup the mock ONU adapter
	onuAdapter, err := createMockAdapter(OnuAdapter, nb.kClient, nb.coreInstanceID, coreName, nb.onuAdapterName)
	if err != nil {
		logger.Fatalw("setting-mock-onu-adapter-failed", log.Fields{"error": err})
	}
	nb.onuAdapter = (onuAdapter).(*cm.ONUAdapter)

	//	Register the adapter
	registrationData = &voltha.Adapter{
		Id:      nb.onuAdapterName,
		Vendor:  "Voltha-onu",
		Version: version.VersionInfo.Version,
	}
	types = []*voltha.DeviceType{{Id: nb.onuAdapterName, Adapter: nb.onuAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes = &voltha.DeviceTypes{Items: types}
	if _, err := nb.adapterMgr.RegisterAdapter(registrationData, deviceTypes); err != nil {
		logger.Errorw("failed-to-register-adapter", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
}

func (nb *NBTest) stopAll() {
	if nb.kClient != nil {
		nb.kClient.Stop()
	}
	if nb.logicalDeviceMgr != nil {
		nb.logicalDeviceMgr.Stop(context.Background())
	}
	if nb.deviceMgr != nil {
		nb.deviceMgr.Stop(context.Background())
	}
	if nb.kmp != nil {
		nb.kmp.Stop()
	}
	if nb.etcdServer != nil {
		stopEmbeddedEtcdServer(nb.etcdServer)
	}
}

func (nb *NBTest) verifyLogicalDevices(t *testing.T, oltDevice *voltha.Device, nbi *NBIHandler) {
	// Get the latest set of logical devices
	logicalDevices, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, logicalDevices)
	assert.Equal(t, 1, len(logicalDevices.Items))

	ld := logicalDevices.Items[0]
	assert.NotEqual(t, "", ld.Id)
	assert.NotEqual(t, uint64(0), ld.DatapathId)
	assert.Equal(t, "olt_adapter_mock", ld.Desc.HwDesc)
	assert.Equal(t, "olt_adapter_mock", ld.Desc.SwDesc)
	assert.NotEqual(t, "", ld.RootDeviceId)
	assert.NotEqual(t, "", ld.Desc.SerialNum)
	assert.Equal(t, uint32(256), ld.SwitchFeatures.NBuffers)
	assert.Equal(t, uint32(2), ld.SwitchFeatures.NTables)
	assert.Equal(t, uint32(15), ld.SwitchFeatures.Capabilities)
	assert.Equal(t, 1+nb.numONUPerOLT, len(ld.Ports))
	assert.Equal(t, oltDevice.ParentId, ld.Id)
	//Expected port no
	expectedPortNo := make(map[uint32]bool)
	expectedPortNo[uint32(2)] = false
	for i := 0; i < nb.numONUPerOLT; i++ {
		expectedPortNo[uint32(i+100)] = false
	}
	for _, p := range ld.Ports {
		assert.Equal(t, p.OfpPort.PortNo, p.DevicePortNo)
		assert.Equal(t, uint32(4), p.OfpPort.State)
		expectedPortNo[p.OfpPort.PortNo] = true
		if strings.HasPrefix(p.Id, "nni") {
			assert.Equal(t, true, p.RootPort)
			//assert.Equal(t, uint32(2), p.OfpPort.PortNo)
			assert.Equal(t, p.Id, fmt.Sprintf("nni-%d", p.DevicePortNo))
		} else {
			assert.Equal(t, p.Id, fmt.Sprintf("uni-%d", p.DevicePortNo))
			assert.Equal(t, false, p.RootPort)
		}
	}
}

func (nb *NBTest) verifyDevices(t *testing.T, nbi *NBIHandler) {
	// Get the latest set of devices
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)

	// A device is ready to be examined when its ADMIN state is ENABLED and OPERATIONAL state is ACTIVE
	var vFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED && device.OperStatus == voltha.OperStatus_ACTIVE
	}

	var wg sync.WaitGroup
	for _, device := range devices.Items {
		wg.Add(1)
		go func(wg *sync.WaitGroup, device *voltha.Device) {
			// Wait until the device is in the right state
			err := waitUntilDeviceReadiness(device.Id, nb.maxTimeout, vFunction, nbi)
			assert.Nil(t, err)

			// Now, verify the details of the device.  First get the latest update
			d, err := nbi.GetDevice(getContext(), &voltha.ID{Id: device.Id})
			assert.Nil(t, err)
			assert.Equal(t, voltha.AdminState_ENABLED, d.AdminState)
			assert.Equal(t, voltha.ConnectStatus_REACHABLE, d.ConnectStatus)
			assert.Equal(t, voltha.OperStatus_ACTIVE, d.OperStatus)
			assert.Equal(t, d.Type, d.Adapter)
			assert.NotEqual(t, "", d.MacAddress)
			assert.NotEqual(t, "", d.SerialNumber)

			if d.Type == "olt_adapter_mock" {
				assert.Equal(t, true, d.Root)
				assert.NotEqual(t, "", d.Id)
				assert.NotEqual(t, "", d.ParentId)
				assert.Nil(t, d.ProxyAddress)
			} else if d.Type == "onu_adapter_mock" {
				assert.Equal(t, false, d.Root)
				assert.NotEqual(t, uint32(0), d.Vlan)
				assert.NotEqual(t, "", d.Id)
				assert.NotEqual(t, "", d.ParentId)
				assert.NotEqual(t, "", d.ProxyAddress.DeviceId)
				assert.Equal(t, "olt_adapter_mock", d.ProxyAddress.DeviceType)
			} else {
				assert.Error(t, errors.New("invalid-device-type"))
			}
			assert.Equal(t, 2, len(d.Ports))
			for _, p := range d.Ports {
				assert.Equal(t, voltha.AdminState_ENABLED, p.AdminState)
				assert.Equal(t, voltha.OperStatus_ACTIVE, p.OperStatus)
				if p.Type == voltha.Port_ETHERNET_NNI || p.Type == voltha.Port_ETHERNET_UNI {
					assert.Equal(t, 0, len(p.Peers))
				} else if p.Type == voltha.Port_PON_OLT {
					assert.Equal(t, nb.numONUPerOLT, len(p.Peers))
					assert.Equal(t, uint32(1), p.PortNo)
				} else if p.Type == voltha.Port_PON_ONU {
					assert.Equal(t, 1, len(p.Peers))
					assert.Equal(t, uint32(1), p.PortNo)
				} else {
					assert.Error(t, errors.New("invalid-port"))
				}
			}
			wg.Done()
		}(&wg, device)
	}
	wg.Wait()
}

func (nb *NBTest) getADevice(rootDevice bool, nbi *NBIHandler) (*voltha.Device, error) {
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	if err != nil {
		return nil, err
	}
	for _, d := range devices.Items {
		if d.Root == rootDevice {
			return d, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "%v device not found", rootDevice)
}

func (nb *NBTest) testCoreWithoutData(t *testing.T, nbi *NBIHandler) {
	lds, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, lds)
	assert.Equal(t, 0, len(lds.Items))
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)
	assert.Equal(t, 0, len(devices.Items))
	adapters, err := nbi.ListAdapters(getContext(), &empty.Empty{})
	assert.Equal(t, 0, len(adapters.Items))
	assert.Nil(t, err)
	assert.NotNil(t, adapters)
}

func (nb *NBTest) testAdapterRegistration(t *testing.T, nbi *NBIHandler) {
	adapters, err := nbi.ListAdapters(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, adapters)
	assert.Equal(t, 2, len(adapters.Items))
	for _, a := range adapters.Items {
		switch a.Id {
		case nb.oltAdapterName:
			assert.Equal(t, "Voltha-olt", a.Vendor)
		case nb.onuAdapterName:
			assert.Equal(t, "Voltha-onu", a.Vendor)
		default:
			logger.Fatal("unregistered-adapter", a.Id)
		}
	}
	deviceTypes, err := nbi.ListDeviceTypes(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, deviceTypes)
	assert.Equal(t, 2, len(deviceTypes.Items))
	for _, dt := range deviceTypes.Items {
		switch dt.Id {
		case nb.oltAdapterName:
			assert.Equal(t, nb.oltAdapterName, dt.Adapter)
			assert.Equal(t, false, dt.AcceptsBulkFlowUpdate)
			assert.Equal(t, true, dt.AcceptsAddRemoveFlowUpdates)
		case nb.onuAdapterName:
			assert.Equal(t, nb.onuAdapterName, dt.Adapter)
			assert.Equal(t, false, dt.AcceptsBulkFlowUpdate)
			assert.Equal(t, true, dt.AcceptsAddRemoveFlowUpdates)
		default:
			logger.Fatal("invalid-device-type", dt.Id)
		}
	}
}

func (nb *NBTest) testCreateDevice(t *testing.T, nbi *NBIHandler) {
	//	Create a valid device
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: nb.oltAdapterName, MacAddress: "aa:bb:cc:cc:ee:ee"})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)
	device, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, oltDevice.String(), device.String())

	// Try to create the same device
	_, err = nbi.CreateDevice(getContext(), &voltha.Device{Type: nb.oltAdapterName, MacAddress: "aa:bb:cc:cc:ee:ee"})
	assert.NotNil(t, err)
	assert.Equal(t, "Device is already pre-provisioned", err.Error())

	// Try to create a device with invalid data
	_, err = nbi.CreateDevice(getContext(), &voltha.Device{Type: nb.oltAdapterName})
	assert.NotNil(t, err)
	assert.Equal(t, "no-device-info-present; MAC or HOSTIP&PORT", err.Error())

	// Ensure we only have 1 device in the Core
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)
	assert.Equal(t, 1, len(devices.Items))
	assert.Equal(t, oltDevice.String(), devices.Items[0].String())

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	//Ensure there are no devices in the Core now - wait until condition satisfied or timeout
	var vFunction isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		return devices != nil && len(devices.Items) == 0
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)
}

func (nb *NBTest) testEnableDevice(t *testing.T, nbi *NBIHandler) {
	// Create a device that has no adapter registered
	oltDeviceNoAdapter, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: "noAdapterRegistered", MacAddress: "aa:bb:cc:cc:ee:ff"})
	assert.Nil(t, err)
	assert.NotNil(t, oltDeviceNoAdapter)

	// Try to enable the oltDevice and check the error message
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDeviceNoAdapter.Id})
	assert.NotNil(t, err)
	assert.Equal(t, "Adapter-not-registered-for-device-type noAdapterRegistered", err.Error())

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDeviceNoAdapter.Id})
	assert.Nil(t, err)

	//Ensure there are no devices in the Core now - wait until condition satisfied or timeout
	var vdFunction isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		return devices != nil && len(devices.Items) == 0
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vdFunction)
	assert.Nil(t, err)

	// Create a logical device monitor will automatically send trap and eapol flows to the devices being enables
	var wg sync.WaitGroup
	wg.Add(1)
	go nb.monitorLogicalDevice(t, nbi, 1, nb.numONUPerOLT, &wg)

	//	Create the device with valid data
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: nb.oltAdapterName, MacAddress: "aa:bb:cc:cc:ee:ee"})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Verify oltDevice exist in the core
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(devices.Items))
	assert.Equal(t, oltDevice.Id, devices.Items[0].Id)

	// Enable the oltDevice
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the logical device to be in the ready state
	var vldFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
		return ld != nil && len(ld.Ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	// Verify that the devices have been setup correctly
	nb.verifyDevices(t, nbi)

	// Get latest oltDevice data
	oltDevice, err = nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Verify that the logical device has been setup correctly
	nb.verifyLogicalDevices(t, oltDevice, nbi)

	// Wait until all flows has been sent to the devices successfully
	wg.Wait()
}

func (nb *NBTest) testDisableAndReEnableRootDevice(t *testing.T, nbi *NBIHandler) {
	//Get an OLT device
	oltDevice, err := nb.getADevice(true, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Disable the oltDevice
	_, err = nbi.DisableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the old device to be disabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_DISABLED && device.OperStatus == voltha.OperStatus_UNKNOWN
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Verify that all onu devices are disabled as well
	onuDevices, err := nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Wait for the logical device to satisfy the expected condition
	var vlFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
		if ld == nil {
			return false
		}
		for _, lp := range ld.Ports {
			if (lp.OfpPort.Config&uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LINK_DOWN) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	// Reenable the oltDevice
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the old device to be enabled
	vdFunction = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED && device.OperStatus == voltha.OperStatus_ACTIVE
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Verify that all onu devices are enabled as well
	onuDevices, err = nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Wait for the logical device to satisfy the expected condition
	vlFunction = func(ld *voltha.LogicalDevice) bool {
		if ld == nil {
			return false
		}
		for _, lp := range ld.Ports {
			if (lp.OfpPort.Config&^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LIVE) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)
}

func (nb *NBTest) testDisableAndDeleteAllDevice(t *testing.T, nbi *NBIHandler) {
	//Get an OLT device
	oltDevice, err := nb.getADevice(true, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Disable the oltDevice
	_, err = nbi.DisableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the olt device to be disabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_DISABLED && device.OperStatus == voltha.OperStatus_UNKNOWN
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Verify that all onu devices are disabled as well
	onuDevices, err := nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Delete the oltDevice
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	var vFunction isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		return devices != nil && len(devices.Items) == 0
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)

	// Wait for absence of logical device
	var vlFunction isLogicalDevicesConditionSatisfied = func(lds *voltha.LogicalDevices) bool {
		return lds != nil && len(lds.Items) == 0
	}

	err = waitUntilConditionForLogicalDevices(nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)
}
func (nb *NBTest) testEnableAndDeleteAllDevice(t *testing.T, nbi *NBIHandler) {
	//Create the device with valid data
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: nb.oltAdapterName, MacAddress: "aa:bb:cc:cc:ee:ee"})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	//Get an OLT device
	oltDevice, err = nb.getADevice(true, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Enable the oltDevice
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the logical device to be in the ready state
	var vldFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
		return ld != nil && len(ld.Ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	//Get all child devices
	onuDevices, err := nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)

	// Wait for the all onu devices to be enabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED
	}
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}
	// Wait for each onu device to get deleted
	var vdFunc isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device == nil
	}

	// Delete the onuDevice
	for _, onu := range onuDevices.Items {
		_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: onu.Id})
		assert.Nil(t, err)
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunc, nbi)
		assert.Nil(t, err)
	}

	// Disable the oltDevice
	_, err = nbi.DisableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the olt device to be disabled
	var vFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_DISABLED && device.OperStatus == voltha.OperStatus_UNKNOWN
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vFunction, nbi)
	assert.Nil(t, err)

	// Delete the oltDevice
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	var vFunc isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		return devices != nil && len(devices.Items) == 0
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunc)
	assert.Nil(t, err)
}
func (nb *NBTest) testDisableAndEnablePort(t *testing.T, nbi *NBIHandler) {
	//Get an OLT device
	var cp *voltha.Port
	oltDevice, err := nb.getADevice(true, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	for _, cp = range oltDevice.Ports {
		if cp.Type == voltha.Port_PON_OLT {
			break
		}

	}
	assert.NotNil(t, cp)
	cp.DeviceId = oltDevice.Id

	// Disable the NW Port of oltDevice
	_, err = nbi.DisablePort(getContext(), cp)
	assert.Nil(t, err)
	// Wait for the olt device Port  to be disabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		for _, port := range device.Ports {
			if port.PortNo == cp.PortNo {
				return port.AdminState == voltha.AdminState_DISABLED
			}
		}
		return false
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)
	// Wait for the logical device to satisfy the expected condition
	var vlFunction = func(ld *voltha.LogicalDevice) bool {
		if ld == nil {
			return false
		}
		for _, lp := range ld.Ports {
			if (lp.OfpPort.Config&^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LIVE) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	// Enable the NW Port of oltDevice
	_, err = nbi.EnablePort(getContext(), cp)
	assert.Nil(t, err)

	// Wait for the olt device Port to be enabled
	vdFunction = func(device *voltha.Device) bool {
		for _, port := range device.Ports {
			if port.PortNo == cp.PortNo {
				return port.AdminState == voltha.AdminState_ENABLED
			}
		}
		return false
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)
	// Wait for the logical device to satisfy the expected condition
	vlFunction = func(ld *voltha.LogicalDevice) bool {
		if ld == nil {
			return false
		}
		for _, lp := range ld.Ports {
			if (lp.OfpPort.Config&^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LIVE) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	// Disable a non-PON port
	for _, cp = range oltDevice.Ports {
		if cp.Type != voltha.Port_PON_OLT {
			break
		}

	}
	assert.NotNil(t, cp)
	cp.DeviceId = oltDevice.Id

	// Disable the NW Port of oltDevice
	_, err = nbi.DisablePort(getContext(), cp)
	assert.NotNil(t, err)

}

func (nb *NBTest) testDeviceRebootWhenOltIsEnabled(t *testing.T, nbi *NBIHandler) {
	//Get an OLT device
	oltDevice, err := nb.getADevice(true, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)
	assert.Equal(t, oltDevice.ConnectStatus, voltha.ConnectStatus_REACHABLE)
	assert.Equal(t, oltDevice.AdminState, voltha.AdminState_ENABLED)

	// Verify that we have one or more ONUs to start with
	onuDevices, err := nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Greater(t, len(onuDevices.Items), 0)

	// Reboot the OLT and very that Connection Status goes to UNREACHABLE and operation status to UNKNOWN
	_, err = nbi.RebootDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	var vlFunction0 = func(d *voltha.Device) bool {
		return d.ConnectStatus == voltha.ConnectStatus_UNREACHABLE && d.OperStatus == voltha.OperStatus_UNKNOWN
	}

	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vlFunction0, nbi)
	assert.Nil(t, err)

	// Wait for the logical device to satisfy the expected condition
	var vlFunction1 = func(ld *voltha.LogicalDevice) bool {
		return ld == nil
	}

	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction1)
	assert.Nil(t, err)

	// Wait for the device to satisfy the expected condition (device does not have flows)
	var vlFunction2 = func(d *voltha.Device) bool {
		var deviceFlows *ofp.Flows
		var err error
		if deviceFlows, err = nbi.ListDeviceFlows(getContext(), &voltha.ID{Id: d.Id}); err != nil {
			return false
		}
		return len(deviceFlows.Items) == 0
	}

	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vlFunction2, nbi)
	assert.Nil(t, err)

	// Wait for the device to satisfy the expected condition (there are no child devices)
	var vlFunction3 = func(d *voltha.Device) bool {
		var devices *voltha.Devices
		var err error
		if devices, err = nbi.ListDevices(getContext(), nil); err != nil {
			return false
		}
		for _, device := range devices.Items {
			if device.ParentId == d.Id {
				// We have a child device still left
				return false
			}
		}
		return true
	}

	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vlFunction3, nbi)
	assert.Nil(t, err)

	// Update the OLT Connection Status to REACHABLE and operation status to ACTIVE
	// Normally, in a real adapter this happens after connection regain via a heartbeat mechanism with real hardware
	err = nbi.deviceMgr.UpdateDeviceStatus(getContext(), oltDevice.Id, voltha.OperStatus_ACTIVE, voltha.ConnectStatus_REACHABLE)
	assert.Nil(t, err)

	// Verify the device connection and operation states
	oltDevice, err = nb.getADevice(true, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)
	assert.Equal(t, oltDevice.ConnectStatus, voltha.ConnectStatus_REACHABLE)
	assert.Equal(t, oltDevice.AdminState, voltha.AdminState_ENABLED)

	// Wait for the logical device to satisfy the expected condition
	var vlFunction4 = func(ld *voltha.LogicalDevice) bool {
		return ld != nil
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction4)
	assert.Nil(t, err)

	// Verify that logical device is created again
	logicalDevices, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, logicalDevices)
	assert.Equal(t, 1, len(logicalDevices.Items))

	// Verify that we have no ONUs left
	onuDevices, err = nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Equal(t, 0, len(onuDevices.Items))
}

func (nb *NBTest) testStartOmciTestAction(t *testing.T, nbi *NBIHandler) {
	// -----------------------------------------------------------------------
	// SubTest 1: Omci test action should fail due to nonexistent device id

	request := &voltha.OmciTestRequest{Id: "123", Uuid: "456"}
	_, err := nbi.StartOmciTestAction(getContext(), request)
	assert.NotNil(t, err)
	assert.Equal(t, "rpc error: code = NotFound desc = 123", err.Error())

	// -----------------------------------------------------------------------
	// SubTest 2: Error should be returned for device with no adapter registered

	// Create a device that has no adapter registered
	deviceNoAdapter, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: "noAdapterRegisteredOmciTest", MacAddress: "aa:bb:cc:cc:ee:01"})
	assert.Nil(t, err)
	assert.NotNil(t, deviceNoAdapter)

	// Omci test action should fail due to nonexistent adapter
	request = &voltha.OmciTestRequest{Id: deviceNoAdapter.Id, Uuid: "456"}
	_, err = nbi.StartOmciTestAction(getContext(), request)
	assert.NotNil(t, err)
	assert.Equal(t, "Adapter-not-registered-for-device-type noAdapterRegisteredOmciTest", err.Error())

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: deviceNoAdapter.Id})
	assert.Nil(t, err)

	//Ensure there are no devices in the Core now - wait until condition satisfied or timeout
	var vFunction isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		return devices != nil && len(devices.Items) == 0
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)

	// -----------------------------------------------------------------------
	// SubTest 3: Omci test action should succeed on valid ONU

	//	Create the device with valid data
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: nb.oltAdapterName, MacAddress: "aa:bb:cc:cc:ee:ee"})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Verify oltDevice exist in the core
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(devices.Items))
	assert.Equal(t, oltDevice.Id, devices.Items[0].Id)

	// Enable the oltDevice
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the logical device to be in the ready state
	var vldFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
		return ld != nil && len(ld.Ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	// Wait for the olt device to be enabled
	vdFunction := func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED && device.OperStatus == voltha.OperStatus_ACTIVE
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	onuDevices, err := nb.deviceMgr.GetAllChildDevices(getContext(), oltDevice.Id)
	assert.Nil(t, err)
	assert.Greater(t, len(onuDevices.Items), 0)

	onuDevice := onuDevices.Items[0]

	// Omci test action should succeed
	request = &voltha.OmciTestRequest{Id: onuDevice.Id, Uuid: "456"}
	resp, err := nbi.StartOmciTestAction(getContext(), request)
	assert.Nil(t, err)
	assert.Equal(t, resp.Result, voltha.TestResponse_SUCCESS)

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	//Ensure there are no devices in the Core now - wait until condition satisfied or timeout
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)
}

func makeSimpleFlowMod(fa *flows.FlowArgs) *ofp.OfpFlowMod {
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return flows.MkSimpleFlowMod(matchFields, fa.Actions, fa.Command, fa.KV)
}

func createMetadata(cTag int, techProfile int, port int) uint64 {
	md := 0
	md = (md | (cTag & 0xFFFF)) << 16
	md = (md | (techProfile & 0xFFFF)) << 32
	return uint64(md | (port & 0xFFFFFFFF))
}

func (nb *NBTest) verifyLogicalDeviceFlowCount(t *testing.T, nbi *NBIHandler, numNNIPorts int, numUNIPorts int) {
	expectedNumFlows := numNNIPorts*3 + numNNIPorts*numUNIPorts
	// Wait for logical device to have all the flows
	var vlFunction isLogicalDevicesConditionSatisfied = func(lds *voltha.LogicalDevices) bool {
		return lds != nil && len(lds.Items) == 1 && len(lds.Items[0].Flows.Items) == expectedNumFlows
	}
	// No timeout implies a success
	err := waitUntilConditionForLogicalDevices(nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)
}

func (nb *NBTest) sendTrapFlows(t *testing.T, nbi *NBIHandler, logicalDevice *voltha.LogicalDevice, meterID uint64, startingVlan int) (numNNIPorts, numUNIPorts int) {
	// Send flows for the parent device
	var nniPorts []*voltha.LogicalPort
	var uniPorts []*voltha.LogicalPort
	for _, p := range logicalDevice.Ports {
		if p.RootPort {
			nniPorts = append(nniPorts, p)
		} else {
			uniPorts = append(uniPorts, p)
		}
	}
	assert.Equal(t, 1, len(nniPorts))
	//assert.Greater(t, len(uniPorts), 1 )
	nniPort := nniPorts[0].OfpPort.PortNo
	maxInt32 := uint64(0xFFFFFFFF)
	controllerPortMask := uint32(4294967293) // will result in 4294967293&0x7fffffff => 2147483645 which is the actual controller port
	var fa *flows.FlowArgs
	fa = &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(nniPort),
			flows.EthType(35020),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowLLDP := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDevice.Id}
	_, err := nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowLLDP)
	assert.Nil(t, err)

	fa = &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(nniPort),
			flows.EthType(2048),
			flows.IpProto(17),
			flows.UdpSrc(67),
			flows.UdpDst(68),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowIPV4 := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDevice.Id}
	_, err = nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowIPV4)
	assert.Nil(t, err)

	fa = &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(nniPort),
			flows.EthType(34525),
			flows.IpProto(17),
			flows.UdpSrc(546),
			flows.UdpDst(547),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowIPV6 := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDevice.Id}
	_, err = nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowIPV6)
	assert.Nil(t, err)

	return len(nniPorts), len(uniPorts)
}

func (nb *NBTest) sendEAPFlows(t *testing.T, nbi *NBIHandler, logicalDeviceID string, port *ofp.OfpPort, vlan int, meterID uint64) {
	maxInt32 := uint64(0xFFFFFFFF)
	controllerPortMask := uint32(4294967293) // will result in 4294967293&0x7fffffff => 2147483645 which is the actual controller port
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1, "write_metadata": createMetadata(vlan, 64, 0), "meter_id": meterID},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(port.PortNo),
			flows.EthType(34958),
			flows.VlanVid(8187),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowEAP := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDeviceID}
	_, err := nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowEAP)
	assert.Nil(t, err)
}

func (nb *NBTest) monitorLogicalDevice(t *testing.T, nbi *NBIHandler, numNNIPorts int, numUNIPorts int, wg *sync.WaitGroup) {
	defer wg.Done()
	nb.logicalDeviceMgr.SetEventCallbacks(nbi)

	// Clear any existing flows on the adapters
	nb.oltAdapter.ClearFlows()
	nb.onuAdapter.ClearFlows()

	// Wait until a logical device is ready
	var vlFunction isLogicalDevicesConditionSatisfied = func(lds *voltha.LogicalDevices) bool {
		if lds == nil || len(lds.Items) != 1 {
			return false
		}
		// Ensure there are both NNI ports and at least one UNI port on the logical device
		ld := lds.Items[0]
		nniPort := false
		uniPort := false
		for _, p := range ld.Ports {
			nniPort = nniPort || p.RootPort == true
			uniPort = uniPort || p.RootPort == false
			if nniPort && uniPort {
				return true
			}
		}
		return false
	}
	err := waitUntilConditionForLogicalDevices(nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	logicalDevices, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, logicalDevices)
	assert.Equal(t, 1, len(logicalDevices.Items))

	logicalDevice := logicalDevices.Items[0]
	meterID := rand.Uint32()

	// Add a meter to the logical device
	meterMod := &ofp.OfpMeterMod{
		Command: ofp.OfpMeterModCommand_OFPMC_ADD,
		Flags:   rand.Uint32(),
		MeterId: meterID,
		Bands: []*ofp.OfpMeterBandHeader{
			{Type: ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER,
				Rate:      rand.Uint32(),
				BurstSize: rand.Uint32(),
				Data:      nil,
			},
		},
	}
	_, err = nbi.UpdateLogicalDeviceMeterTable(getContext(), &ofp.MeterModUpdate{Id: logicalDevice.Id, MeterMod: meterMod})
	assert.Nil(t, err)

	// Send initial set of Trap flows
	startingVlan := 4091
	nb.sendTrapFlows(t, nbi, logicalDevice, uint64(meterID), startingVlan)

	// Listen for port events
	start := time.Now()
	processedNniLogicalPorts := 0
	processedUniLogicalPorts := 0

	for event := range nbi.changeEventQueue {
		startingVlan++
		if portStatus, ok := (event.Event).(*ofp.ChangeEvent_PortStatus); ok {
			ps := portStatus.PortStatus
			if ps.Reason == ofp.OfpPortReason_OFPPR_ADD {
				if ps.Desc.PortNo >= uint32(nb.startingUNIPortNo) {
					processedUniLogicalPorts++
					nb.sendEAPFlows(t, nbi, logicalDevice.Id, ps.Desc, startingVlan, uint64(meterID))
				} else {
					processedNniLogicalPorts++
				}
			}
		}

		if processedNniLogicalPorts >= numNNIPorts && processedUniLogicalPorts >= numUNIPorts {
			fmt.Println("Total time to send all flows:", time.Since(start))
			break
		}
	}
	//Verify the flow count on the logical device
	nb.verifyLogicalDeviceFlowCount(t, nbi, numNNIPorts, numUNIPorts)

	// Wait until all flows have been sent to the OLT adapters
	var oltVFunc isConditionSatisfied = func() bool {
		return nb.oltAdapter.GetFlowCount() >= (numNNIPorts*3)+numNNIPorts*numUNIPorts
	}
	err = waitUntilCondition(nb.maxTimeout, nbi, oltVFunc)
	assert.Nil(t, err)

	// Wait until all flows have been sent to the ONU adapters
	var onuVFunc isConditionSatisfied = func() bool {
		return nb.onuAdapter.GetFlowCount() == numUNIPorts
	}
	err = waitUntilCondition(nb.maxTimeout, nbi, onuVFunc)
	assert.Nil(t, err)
}

func TestSuite1(t *testing.T) {
	f, err := os.Create("../../../tests/results/profile.cpu")
	if err != nil {
		logger.Fatalf("could not create CPU profile: %v\n ", err)
	}
	defer f.Close()
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(-1)
	if err := pprof.StartCPUProfile(f); err != nil {
		logger.Fatalf("could not start CPU profile: %v\n", err)
	}
	defer pprof.StopCPUProfile()

	//log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)

	nb := newNBTest()
	assert.NotNil(t, nb)

	defer nb.stopAll()

	// Start the Core
	nb.startCore(false)

	// Set the grpc API interface - no grpc server is running in unit test
	nbi := NewAPIHandler(nb.deviceMgr, nb.logicalDeviceMgr, nb.adapterMgr)

	// 1. Basic test with no data in Core
	nb.testCoreWithoutData(t, nbi)

	// Create/register the adapters
	nb.createAndregisterAdapters(t)

	// 2. Test adapter registration
	nb.testAdapterRegistration(t, nbi)

	numberOfDeviceTestRuns := 2
	for i := 1; i <= numberOfDeviceTestRuns; i++ {
		//3. Test create device
		nb.testCreateDevice(t, nbi)

		// 4. Test Enable a device
		nb.testEnableDevice(t, nbi)

		// 5. Test disable and ReEnable a root device
		nb.testDisableAndReEnableRootDevice(t, nbi)

		// 6. Test disable and Enable pon port of OLT device
		nb.testDisableAndEnablePort(t, nbi)

		// 7.Test Device unreachable when OLT is enabled
		nb.testDeviceRebootWhenOltIsEnabled(t, nbi)

		// 8. Test disable and delete all devices
		nb.testDisableAndDeleteAllDevice(t, nbi)

		// 9. Test enable and delete all devices
		nb.testEnableAndDeleteAllDevice(t, nbi)

		// 10. Test omci test
		nb.testStartOmciTestAction(t, nbi)
	}

	//x. TODO - More tests to come
}
