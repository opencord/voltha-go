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
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/stretchr/testify/assert"
)

var (
	BenchmarkProxyLogger           log.CLogger
	TestProxyRootLogicalDevice     *Proxy
	TestProxyRootDevice            *Proxy
	TestProxyRootAdapter           *Proxy
	TestProxyDeviceID              string
	TestProxyAdapterID             string
	TestProxyLogicalDeviceID       string
	TestProxyTargetDeviceID        string
	TestProxyTargetLogicalDeviceID string
	TestProxyStats                 *openflow_13.OfpFlowStats
	TestProxyDevice                *voltha.Device
	TestProxyLogicalDevice         *voltha.LogicalDevice
	TestProxyAdapter               *voltha.Adapter
)

func init() {
	BenchmarkProxyLogger, _ = log.RegisterPackage(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"})
	ctx := context.Background()
	//log.UpdateAllLoggers(log.Fields{"instanceId": "PROXY_LOAD_TEST"})
	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"}); err != nil {
		BenchmarkProxyLogger.With(log.Fields{"error": err}).Fatal(ctx, "Cannot setup logging")
	}

	// Update all loggers (provisioned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": "PLT"}); err != nil {
		BenchmarkProxyLogger.With(log.Fields{"error": err}).Fatal(ctx, "Cannot setup logging")
	}
	log.SetPackageLogLevel("github.com/opencord/voltha-go/db/model", log.DebugLevel)

	proxy := NewDBPath(mockBackend)
	TestProxyRootLogicalDevice = proxy.Proxy("logical_devices")
	TestProxyRootDevice = proxy.Proxy("device")
	TestProxyRootAdapter = proxy.Proxy("adapters")

	TestProxyStats = &openflow_13.OfpFlowStats{
		Id: 1111,
	}
	TestProxyDevice = &voltha.Device{
		Id:         TestProxyDeviceID,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
	}

	TestProxyLogicalDevice = &voltha.LogicalDevice{
		Id:         TestProxyDeviceID,
		DatapathId: 0,
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
	ctx := context.Background()
	devIDBin, _ := uuid.New().MarshalBinary()
	TestProxyDeviceID = "0001" + hex.EncodeToString(devIDBin)[:12]
	TestProxyDevice.Id = TestProxyDeviceID

	if err := TestProxyRootDevice.Set(context.Background(), TestProxyDeviceID, TestProxyDevice); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to add test proxy device due to error: %v", err)
		t.Errorf("failed to add device: %s", err)
	}
	t.Logf("Added device : %+v", TestProxyDevice.Id)

	// Verify that the added device can now be retrieved
	d := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), TestProxyDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed get device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_1_2_Add_ExistingDevice(t *testing.T) {
	ctx := context.Background()
	TestProxyDevice.Id = TestProxyDeviceID

	if err := TestProxyRootDevice.Set(context.Background(), "devices", TestProxyDevice); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to add device to test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}

	d := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), TestProxyDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed get device info from test proxy due to error: %v", err)
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
	ctx := context.Background()
	TestProxyAdapterID = "test-adapter"
	TestProxyAdapter.Id = TestProxyAdapterID

	// Add the adapter
	if err := TestProxyRootAdapter.Set(context.Background(), TestProxyAdapterID, TestProxyAdapter); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to add adapter to test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else {
		t.Logf("Added adapter : %+v", TestProxyAdapter.Id)
	}

	// Verify that the added device can now be retrieved
	d := &voltha.Device{}
	if have, err := TestProxyRootAdapter.Get(context.Background(), TestProxyAdapterID, d); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to retrieve device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added adapter")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found adapter: %s", string(djson))
	}

}

func TestProxy_1_2_1_Get_AllDevices(t *testing.T) {
	ctx := context.Background()
	var devices []*voltha.Device
	if err := TestProxyRootDevice.List(context.Background(), &devices); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get all devices info from test proxy due to error: %v", err)
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
	ctx := context.Background()
	d := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), TestProxyTargetDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get single device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Errorf("Failed to find device : %s", TestProxyTargetDeviceID)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_3_1_Update_Device(t *testing.T) {
	ctx := context.Background()
	var fwVersion int

	retrieved := &voltha.Device{}
	if have, err := TestProxyRootDevice.Get(context.Background(), TestProxyTargetDeviceID, retrieved); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get device info from test proxy due to error: %v", err)
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

		if err := TestProxyRootDevice.Set(context.Background(), TestProxyTargetDeviceID, retrieved); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Failed to update device info test proxy due to error: %v", err)
			assert.NotNil(t, err)
		}
		afterUpdate := &voltha.Device{}
		if have, err := TestProxyRootDevice.Get(context.Background(), TestProxyTargetDeviceID, afterUpdate); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Failed to get device info from test proxy due to error: %v", err)
		} else if !have {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}

		d := &voltha.Device{}
		if have, err := TestProxyRootDevice.Get(context.Background(), TestProxyTargetDeviceID, d); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Failed to get device info from test proxy due to error: %v", err)
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
	ctx := context.Background()

	adaptersProxy := NewDBPath(mockBackend).Proxy("adapters")

	retrieved := &voltha.Adapter{}
	if have, err := TestProxyRootAdapter.Get(context.Background(), TestProxyAdapterID, retrieved); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to retrieve adapter info from adapters proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to get adapter")
	} else {
		t.Logf("Found raw adapter (root proxy): %+v", retrieved)

		retrieved.Version = "test-adapter-version-2"

		if err := adaptersProxy.Set(context.Background(), TestProxyAdapterID, retrieved); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Failed to update adapter info in adapters proxy due to error: %v", err)
			assert.NotNil(t, err)
		} else {
			t.Logf("Updated adapter : %s", retrieved.Id)
		}

		d := &voltha.Adapter{}
		if have, err := TestProxyRootAdapter.Get(context.Background(), TestProxyAdapterID, d); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Failed to get updated adapter info from adapters proxy due to error: %v", err)
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
	ctx := context.Background()
	if err := TestProxyRootDevice.Remove(context.Background(), TestProxyDeviceID); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to remove device from devices proxy due to error: %v", err)
		t.Errorf("failed to remove device: %s", err)
	} else {
		t.Logf("Removed device : %+v", TestProxyDeviceID)
	}

	d := &voltha.Device{}
	have, err := TestProxyRootDevice.Get(context.Background(), TestProxyDeviceID, d)
	if err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get device info from devices proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if have {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxyDeviceID)
	}
}

func TestProxy_2_1_1_Add_NewLogicalDevice(t *testing.T) {
	ctx := context.Background()

	ldIDBin, _ := uuid.New().MarshalBinary()
	TestProxyLogicalDeviceID = "0001" + hex.EncodeToString(ldIDBin)[:12]
	TestProxyLogicalDevice.Id = TestProxyLogicalDeviceID

	if err := TestProxyRootLogicalDevice.Set(context.Background(), TestProxyLogicalDeviceID, TestProxyLogicalDevice); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to add new logical device into proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else {
		t.Logf("Added logical device : %s", TestProxyLogicalDevice.Id)
	}

	ld := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), TestProxyLogicalDeviceID, ld); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get logical device info from logical device proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Error("Failed to find added logical device")
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}
}

func TestProxy_2_1_2_Add_ExistingLogicalDevice(t *testing.T) {
	ctx := context.Background()
	TestProxyLogicalDevice.Id = TestProxyLogicalDeviceID

	if err := TestProxyRootLogicalDevice.Set(context.Background(), "logical_devices", TestProxyLogicalDevice); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to add logical device due to error: %v", err)
		assert.NotNil(t, err)
	}

	device := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices", device); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get logical device info from logical device proxy due to error: %v", err)
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
	ctx := context.Background()
	var logicalDevices []*voltha.LogicalDevice
	if err := TestProxyRootLogicalDevice.List(context.Background(), &logicalDevices); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get all logical devices from proxy due to error: %v", err)
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
	ctx := context.Background()
	ld := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), TestProxyTargetLogicalDeviceID, ld); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get single logical device from proxy due to error: %v", err)
		assert.NotNil(t, err)
	} else if !have {
		t.Errorf("Failed to find logical device : %s", TestProxyTargetLogicalDeviceID)
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

}

func TestProxy_2_3_1_Update_LogicalDevice(t *testing.T) {
	ctx := context.Background()
	var fwVersion int

	retrieved := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), TestProxyTargetLogicalDeviceID, retrieved); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get logical devices due to error: %v", err)
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

		if err := TestProxyRootLogicalDevice.Set(context.Background(), TestProxyTargetLogicalDeviceID, retrieved); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Faield to update logical device info due to error: %v", err)
			assert.NotNil(t, err)
		} else {
			t.Log("Updated logical device")
		}

		d := &voltha.LogicalDevice{}
		if have, err := TestProxyRootLogicalDevice.Get(context.Background(), TestProxyTargetLogicalDeviceID, d); err != nil {
			BenchmarkProxyLogger.Errorf(ctx, "Failed to get logical device info due to error: %v", err)
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
	ctx := context.Background()
	if err := TestProxyRootLogicalDevice.Remove(context.Background(), "logical_devices/"+TestProxyLogicalDeviceID); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to remove device from logical devices proxy due to error: %v", err)
		t.Errorf("Failed to remove logical device: %s", err)
	} else {
		t.Logf("Removed device : %+v", TestProxyLogicalDeviceID)
	}

	d := &voltha.LogicalDevice{}
	if have, err := TestProxyRootLogicalDevice.Get(context.Background(), "logical_devices/"+TestProxyLogicalDeviceID, d); err != nil {
		BenchmarkProxyLogger.Errorf(ctx, "Failed to get logical device info due to error: %v", err)
		assert.NotNil(t, err)
	} else if have {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxyLogicalDeviceID)
	}
}
