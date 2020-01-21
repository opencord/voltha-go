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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/config"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
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

type NBTest struct {
	etcdServer     *lm.EtcdServer
	core           *Core
	kClient        kafka.Client
	kvClientPort   int
	numONUPerOLT   int
	oltAdapterName string
	onuAdapterName string
	coreInstanceID string
	defaultTimeout time.Duration
	maxTimeout     time.Duration
}

func newNBTest() *NBTest {
	test := &NBTest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = startEmbeddedEtcdServer("voltha.rwcore.nb.test", "voltha.rwcore.nb.etcd", "error")
	if err != nil {
		log.Fatal(err)
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
	cfg.DefaultRequestTimeout = nb.defaultTimeout.Nanoseconds() / 1000000 //TODO: change when Core changes to Duration
	cfg.DefaultCoreTimeout = nb.defaultTimeout.Nanoseconds() / 1000000
	cfg.KVStorePort = nb.kvClientPort
	cfg.InCompetingMode = inCompeteMode
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		log.Fatal("Cannot get a freeport for grpc")
	}
	cfg.GrpcPort = grpcPort
	cfg.GrpcHost = "127.0.0.1"
	setCoreCompeteMode(inCompeteMode)
	client := setupKVClient(cfg, nb.coreInstanceID)
	nb.core = NewCore(ctx, nb.coreInstanceID, cfg, client, nb.kClient)
	err = nb.core.Start(context.Background())
	if err != nil {
		log.Fatal("Cannot start core")
	}
}

func (nb *NBTest) createAndregisterAdapters(t *testing.T) {
	// Setup the mock OLT adapter
	oltAdapter, err := createMockAdapter(OltAdapter, nb.kClient, nb.coreInstanceID, coreName, nb.oltAdapterName)
	if err != nil {
		log.Fatalw("setting-mock-olt-adapter-failed", log.Fields{"error": err})
	}
	if adapter, ok := (oltAdapter).(*cm.OLTAdapter); ok {
		nb.numONUPerOLT = adapter.GetNumONUPerOLT()
	}
	//	Register the adapter
	registrationData := &voltha.Adapter{
		Id:      nb.oltAdapterName,
		Vendor:  "Voltha-olt",
		Version: version.VersionInfo.Version,
	}
	types := []*voltha.DeviceType{{Id: nb.oltAdapterName, Adapter: nb.oltAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes := &voltha.DeviceTypes{Items: types}
	if _, err := nb.core.adapterMgr.registerAdapter(registrationData, deviceTypes); err != nil {
		log.Errorw("failed-to-register-adapter", log.Fields{"error": err})
		assert.NotNil(t, err)
	}

	// Setup the mock ONU adapter
	if _, err := createMockAdapter(OnuAdapter, nb.kClient, nb.coreInstanceID, coreName, nb.onuAdapterName); err != nil {
		log.Fatalw("setting-mock-onu-adapter-failed", log.Fields{"error": err})
	}
	//	Register the adapter
	registrationData = &voltha.Adapter{
		Id:      nb.onuAdapterName,
		Vendor:  "Voltha-onu",
		Version: version.VersionInfo.Version,
	}
	types = []*voltha.DeviceType{{Id: nb.onuAdapterName, Adapter: nb.onuAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes = &voltha.DeviceTypes{Items: types}
	if _, err := nb.core.adapterMgr.registerAdapter(registrationData, deviceTypes); err != nil {
		log.Errorw("failed-to-register-adapter", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
}

func (nb *NBTest) stopAll() {
	if nb.kClient != nil {
		nb.kClient.Stop()
	}
	if nb.core != nil {
		nb.core.Stop(context.Background())
	}
	if nb.etcdServer != nil {
		stopEmbeddedEtcdServer(nb.etcdServer)
	}
}

func (nb *NBTest) verifyLogicalDevices(t *testing.T, oltDevice *voltha.Device, nbi *APIHandler) {
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

func (nb *NBTest) verifyDevices(t *testing.T, nbi *APIHandler) {
	// Get the latest set of devices
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)

	// Wait until devices are in the correct states
	var vFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED && device.OperStatus == voltha.OperStatus_ACTIVE
	}
	for _, d := range devices.Items {
		err = waitUntilDeviceReadiness(d.Id, nb.maxTimeout, vFunction, nbi)
		assert.Nil(t, err)
		assert.NotNil(t, d)
	}
	// Get the latest device updates as they may have changed since last list devices
	updatedDevices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)
	for _, d := range updatedDevices.Items {
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
	}
}

func (nb *NBTest) getADevice(rootDevice bool, nbi *APIHandler) (*voltha.Device, error) {
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

func (nb *NBTest) testCoreWithoutData(t *testing.T, nbi *APIHandler) {
	lds, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, lds)
	assert.Equal(t, 0, len(lds.Items))
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)
	assert.Equal(t, 0, len(devices.Items))
	adapters, err := nbi.ListAdapters(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, adapters)
	assert.Equal(t, 0, len(adapters.Items))
}

func (nb *NBTest) testAdapterRegistration(t *testing.T, nbi *APIHandler) {
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
			log.Fatal("unregistered-adapter", a.Id)
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
			log.Fatal("invalid-device-type", dt.Id)
		}
	}
}

func (nb *NBTest) testCreateDevice(t *testing.T, nbi *APIHandler) {
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
	err = waitUntilConditionForDevices(5*time.Second, nbi, vFunction)
	assert.Nil(t, err)
}

func (nb *NBTest) testEnableDevice(t *testing.T, nbi *APIHandler) {
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
	err = waitUntilConditionForDevices(5*time.Second, nbi, vdFunction)
	assert.Nil(t, err)

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
}

func (nb *NBTest) testDisableAndReEnableRootDevice(t *testing.T, nbi *APIHandler) {
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
	onuDevices, err := nb.core.deviceMgr.getAllChildDevices(oltDevice.Id)
	assert.Nil(t, err)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Wait for the logical device to satisfy the expected condition
	var vlFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
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
	onuDevices, err = nb.core.deviceMgr.getAllChildDevices(oltDevice.Id)
	assert.Nil(t, err)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Wait for the logical device to satisfy the expected condition
	vlFunction = func(ld *voltha.LogicalDevice) bool {
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

func (nb *NBTest) testDisableAndDeleteAllDevice(t *testing.T, nbi *APIHandler) {
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
	onuDevices, err := nb.core.deviceMgr.getAllChildDevices(oltDevice.Id)
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
func (nb *NBTest) testDisableAndEnablePort(t *testing.T, nbi *APIHandler) {
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

func TestSuite1(t *testing.T) {
	nb := newNBTest()
	assert.NotNil(t, nb)

	defer nb.stopAll()

	// Start the Core
	nb.startCore(false)

	// Set the grpc API interface - no grpc server is running in unit test
	nbi := NewAPIHandler(nb.core)

	// 1. Basic test with no data in Core
	nb.testCoreWithoutData(t, nbi)

	// Create/register the adapters
	nb.createAndregisterAdapters(t)

	// 2. Test adapter registration
	nb.testAdapterRegistration(t, nbi)

	numberOfDeviceTestRuns := 2
	for i := 1; i <= numberOfDeviceTestRuns; i++ {
		// 3. Test create device
		nb.testCreateDevice(t, nbi)

		// 4. Test Enable a device
		nb.testEnableDevice(t, nbi)

		// 5. Test disable and ReEnable a root device
		nb.testDisableAndReEnableRootDevice(t, nbi)
		// 6. Test disable and Enable pon port of OLT device
		nb.testDisableAndEnablePort(t, nbi)

		// 6. Test disable and delete all devices
		nb.testDisableAndDeleteAllDevice(t, nbi)
	}

	//x. TODO - More tests to come
}
