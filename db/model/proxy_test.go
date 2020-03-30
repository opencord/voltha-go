/*
 * Copyright 2018-present Open Networking Foundation

 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package model

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var (
	BenchmarkProxyLogger           log.Logger
	TestProxyRootLogicalDevice     *Proxy
	TestProxyRootDevice            *Proxy
	TestProxyRootAdapter           *Proxy
	TestProxyDeviceID              string
	TestProxyAdapterID             string
	TestProxyLogicalDeviceID       string
	TestProxyTargetDeviceID        string
	TestProxyTargetLogicalDeviceID string
	TestProxyLogicalPorts          []*voltha.LogicalPort
	TestProxyPorts                 []*voltha.Port
	TestProxyStats                 *openflow_13.OfpFlowStats
	TestProxyFlows                 *openflow_13.Flows
	TestProxyDevice                *voltha.Device
	TestProxyLogicalDevice         *voltha.LogicalDevice
	TestProxyAdapter               *voltha.Adapter
)

func init() {
	BenchmarkProxyLogger, _ = log.AddPackage(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"})
	//log.UpdateAllLoggers(log.Fields{"instanceId": "PROXY_LOAD_TEST"})
	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers (provisioned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": "PLT"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	log.SetPackageLogLevel("github.com/opencord/voltha-go/db/model", log.DebugLevel)

	TestProxyRootLogicalDevice = NewProxy(mockBackend, "/")
	TestProxyRootDevice = NewProxy(mockBackend, "/")
	TestProxyRootAdapter = NewProxy(mockBackend, "/")

	TestProxyLogicalPorts = []*voltha.LogicalPort{
		{
			Id:           "123",
			DeviceId:     "logicalport-0-device-id",
			DevicePortNo: 123,
			RootPort:     false,
		},
	}
	TestProxyPorts = []*voltha.Port{
		{
			PortNo:     123,
			Label:      "test-port-0",
			Type:       voltha.Port_PON_OLT,
			AdminState: common.AdminState_ENABLED,
			OperStatus: common.OperStatus_ACTIVE,
			DeviceId:   "etcd_port-0-device-id",
			Peers:      []*voltha.Port_PeerPort{},
		},
	}

	TestProxyStats = &openflow_13.OfpFlowStats{
		Id: 1111,
	}
	TestProxyFlows = &openflow_13.Flows{
		Items: []*openflow_13.OfpFlowStats{TestProxyStats},
	}
	TestProxyDevice = &voltha.Device{
		Id:         TestProxyDeviceID,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Flows:      TestProxyFlows,
		Ports:      TestProxyPorts,
	}

	TestProxyLogicalDevice = &voltha.LogicalDevice{
		Id:         TestProxyDeviceID,
		DatapathId: 0,
		Ports:      TestProxyLogicalPorts,
		Flows:      TestProxyFlows,
	}

	TestProxyAdapter = &voltha.Adapter{
		Id:      TestProxyAdapterID,
		Vendor:  "test-adapter-vendor",
		Version: "test-adapter-version",
	}
}

type mockKV struct {
	kvstore.Client // pretend we implement everything

	mutex sync.RWMutex
	data  map[string]interface{}
}

func (kv *mockKV) List(_ context.Context, key string) (map[string]*kvstore.KVPair, error) {
	kv.mutex.RLock()
	defer kv.mutex.RUnlock()

	ret := make(map[string]*kvstore.KVPair, len(kv.data))
	for k, v := range kv.data {
		if strings.HasPrefix(k, key) {
			ret[k] = &kvstore.KVPair{Key: k, Value: v}
		}
	}
	return ret, nil
}
func (kv *mockKV) Get(_ context.Context, key string) (*kvstore.KVPair, error) {
	kv.mutex.RLock()
	defer kv.mutex.RUnlock()

	if val, have := kv.data[key]; have {
		return &kvstore.KVPair{Key: key, Value: val}, nil
	}
	return nil, nil
}
func (kv *mockKV) Put(_ context.Context, key string, value interface{}) error {
	kv.mutex.Lock()
	defer kv.mutex.Unlock()

	kv.data[key] = value
	return nil
}
func (kv *mockKV) Delete(_ context.Context, key string) error {
	kv.mutex.Lock()
	defer kv.mutex.Unlock()

	delete(kv.data, key)
	return nil
}

var mockBackend = &db.Backend{Client: &mockKV{data: make(map[string]interface{})}}

func TestProxy_1_1_1_Add_NewDevice(t *testing.T) {
	devIDBin, _ := uuid.New().MarshalBinary()
	TestProxyDeviceID = "0001" + hex.EncodeToString(devIDBin)[:12]
	TestProxyDevice.Id = TestProxyDeviceID

	if err := TestProxyRootDevice.AddWithID(context.Background(), "devices", TestProxyDeviceID, TestProxyDevice); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add test proxy device due to error: %v", err)
		t.Errorf("failed to add device: %s", err)
	}
	t.Logf("Added device : %+v", TestProxyDevice.Id)

	// Verify that the added device can now be retrieved
	d := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf("Failed get device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_1_2_Add_ExistingDevice(t *testing.T) {
	TestProxyDevice.Id = TestProxyDeviceID

	if err := TestProxyRootDevice.add(context.Background(), "devices", TestProxyDevice); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add device to test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}

	d := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf("Failed get device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added device")
	} else {
		if d.String() != TestProxyDevice.String() {
			t.Errorf("Devices don't match - existing: %+v returned: %+v", TestProxyLogicalDevice, d)
		}
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

}

func TestProxy_1_1_3_Add_NewAdapter(t *testing.T) {
	TestProxyAdapterID = "test-adapter"
	TestProxyAdapter.Id = TestProxyAdapterID

	// Add the adapter
	if err := TestProxyRootAdapter.AddWithID(context.Background(), "adapters", TestProxyAdapterID, TestProxyAdapter); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add adapter to test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else {
		t.Logf("Added adapter : %+v", TestProxyAdapter.Id)
	}

	// Verify that the added device can now be retrieved
	d := &voltha.Device{}
	if have, err := TestProxyRootAdapter.Get(context.Background(), "adapters/"+TestProxyAdapterID, d); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to retrieve device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added adapter")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found adapter: %s", string(djson))
	}

}

func TestProxy_1_2_1_Get_AllDevices(t *testing.T) {
	var devices []*voltha.Device
	if err := TestProxyRootDevice.List(context.Background(), "devices", &devices); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get all devices info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if len(devices) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		TestProxyTargetDeviceID = devices[0].Id
		t.Logf("retrieved all devices: %+v", devices)
	}
}

func TestProxy_1_2_2_Get_SingleDevice(t *testing.T) {
	d := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyTargetDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get single device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Errorf("Failed to find device : %s", TestProxyTargetDeviceID)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_3_1_Update_Device(t *testing.T) {
	var fwVersion int

	retrieved := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyTargetDeviceID, retrieved); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to get device")
	} else {
		t.Logf("Found raw device (root proxy): %+v", retrieved)

		if retrieved.FirmwareVersion == "n/a" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.FirmwareVersion)
			fwVersion++
		}

		retrieved.FirmwareVersion = strconv.Itoa(fwVersion)

		if err := TestProxyRootDevice.Update(context.Background(), "devices/"+TestProxyTargetDeviceID, retrieved); err != nil {
			BenchmarkProxyLogger.Errorf("Failed to update device info test proxy due to error: %v", err)
			assert.NotNil(t, err)
		}
		afterUpdate := &voltha.Device{}
		if have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyTargetDeviceID, afterUpdate); err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get device info from test proxy due to error: %v", err)
		} else if !have {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}

		d := &voltha.Device{}
		if have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyTargetDeviceID, d); err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get device info from test proxy due to error: %v", err)
			assert.NotNil(t, err)
		} else if !have {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_1_3_3_Update_Adapter(t *testing.T) {

	adaptersProxy := NewProxy(mockBackend, "/adapters")

	retrieved := &voltha.Adapter{}
	if have, err := TestProxyRootAdapter.Get(context.Background(), "adapters/"+TestProxyAdapterID, retrieved); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to retrieve adapter info from adapters proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to get adapter")
	} else {
		t.Logf("Found raw adapter (root proxy): %+v", retrieved)

		retrieved.Version = "test-adapter-version-2"

		if err := adaptersProxy.Update(context.Background(), TestProxyAdapterID, retrieved); err != nil {
			BenchmarkProxyLogger.Errorf("Failed to update adapter info in adapters proxy due to error: %v", err)
			assert.NotNil(t, err)
		} else {
			t.Logf("Updated adapter : %s", retrieved.Id)
		}

		d := &voltha.Adapter{}
		if have, err := TestProxyRootAdapter.Get(context.Background(), "adapters/"+TestProxyAdapterID, d); err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get updated adapter info from adapters proxy due to error: %v", err)
			assert.NotNil(t, err)
		} else if !have {
			t.Error("Failed to find updated adapter (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found adapter (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_1_4_1_Remove_Device(t *testing.T) {
	if err := TestProxyRootDevice.Remove(context.Background(), "devices/"+TestProxyDeviceID); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to remove device from devices proxy due to error: %v", err)
		t.Errorf("failed to remove device: %s", err)
	} else {
		t.Logf("Removed device : %+v", TestProxyDeviceID)
	}

	d := &voltha.Device{}
	have, err := TestProxyRootDevice.Get(context.Background(), "devices/"+TestProxyDeviceID, d)
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get device info from devices proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if have {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxyDeviceID)
	}
}

func TestProxy_2_1_1_Add_NewLogicalDevice(t *testing.T) {

	ldIDBin, _ := uuid.New().MarshalBinary()
	TestProxyLogicalDeviceID = "0001" + hex.EncodeToString(ldIDBin)[:12]
	TestProxyLogicalDevice.Id = TestProxyLogicalDeviceID

	if err := TestProxyRootLogicalDevice.AddWithID(context.Background(), "logical_devices", TestProxyLogicalDeviceID, TestProxyLogicalDevice); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add new logical device into proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else {
		t.Logf("Added logical device : %s", TestProxyLogicalDevice.Id)
	}

	ld := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices/"+TestProxyLogicalDeviceID, ld); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical device info from logical device proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added logical device")
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}
}

func TestProxy_2_1_2_Add_ExistingLogicalDevice(t *testing.T) {
	TestProxyLogicalDevice.Id = TestProxyLogicalDeviceID

	if err := TestProxyRootLogicalDevice.add(context.Background(), "logical_devices", TestProxyLogicalDevice); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add logical device due to error: %v", err)
		assert.NotNil(t, err)
	}

	device := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices", device); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical device info from logical device proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added logical device")
	} else {
		if device.String() != TestProxyLogicalDevice.String() {
			t.Errorf("Logical devices don't match - existing: %+v returned: %+v", TestProxyLogicalDevice, device)
		}
	}
}

func TestProxy_2_2_1_Get_AllLogicalDevices(t *testing.T) {
	var logicalDevices []*voltha.LogicalDevice
	if err := TestProxyRootLogicalDevice.List(context.Background(), "logical_devices", &logicalDevices); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get all logical devices from proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if len(logicalDevices) == 0 {
		t.Error("there are no available logical devices to retrieve")
	} else {
		// Save the target device id for later tests
		TestProxyTargetLogicalDeviceID = logicalDevices[0].Id
		t.Logf("retrieved all logical devices: %+v", logicalDevices)
	}
}

func TestProxy_2_2_2_Get_SingleLogicalDevice(t *testing.T) {
	ld := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices/"+TestProxyTargetLogicalDeviceID, ld); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get single logical device from proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Errorf("Failed to find logical device : %s", TestProxyTargetLogicalDeviceID)
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

}

func TestProxy_2_3_1_Update_LogicalDevice(t *testing.T) {
	var fwVersion int

	retrieved := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices/"+TestProxyTargetLogicalDeviceID, retrieved); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical devices due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to get logical device")
	} else {
		t.Logf("Found raw logical device (root proxy): %+v", retrieved)

		if retrieved.RootDeviceId == "" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.RootDeviceId)
			fwVersion++
		}

		retrieved.RootDeviceId = strconv.Itoa(fwVersion)

		if err := TestProxyRootLogicalDevice.Update(context.Background(), "logical_devices/"+TestProxyTargetLogicalDeviceID, retrieved); err != nil {
			BenchmarkProxyLogger.Errorf("Faield to update logical device info due to error: %v", err)
			assert.NotNil(t, err)
		} else {
			t.Log("Updated logical device")
		}

		d := &voltha.LogicalDevice{}
		if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices/"+TestProxyTargetLogicalDeviceID, d); err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get logical device info due to error: %v", err)
			assert.NotNil(t, err)
		} else if !have {
			t.Error("Failed to find updated logical device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found logical device (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_2_4_1_Remove_Device(t *testing.T) {
	if err := TestProxyRootLogicalDevice.Remove(context.Background(), "logical_devices/"+TestProxyLogicalDeviceID); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to remove device from logical devices proxy due to error: %v", err)
		t.Errorf("Failed to remove logical device: %s", err)
	} else {
		t.Logf("Removed device : %+v", TestProxyLogicalDeviceID)
	}

	d := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices/"+TestProxyLogicalDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical device info due to error: %v", err)
		assert.NotNil(t, err)
	} else if have {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxyLogicalDeviceID)
	}
}
