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
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
)

var (
	TestProxyRoot                  Root
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
	//log.AddPackage(log.JSON, log.InfoLevel, log.Fields{"instanceId": "DB_MODEL"})
	//log.UpdateAllLoggers(log.Fields{"instanceId": "PROXY_LOAD_TEST"})
	var err error
	TestProxyRoot = NewRoot(&voltha.Voltha{}, nil)
	if TestProxyRootLogicalDevice, err = TestProxyRoot.CreateProxy(context.Background(), "/", false); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create logical device proxy")
	}
	if TestProxyRootDevice, err = TestProxyRoot.CreateProxy(context.Background(), "/", false); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create device proxy")
	}
	if TestProxyRootAdapter, err = TestProxyRoot.CreateProxy(context.Background(), "/", false); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create adapter proxy")
	}

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

func TestProxy_1_1_1_Add_NewDevice(t *testing.T) {
	devIDBin, _ := uuid.New().MarshalBinary()
	TestProxyDeviceID = "0001" + hex.EncodeToString(devIDBin)[:12]
	TestProxyDevice.Id = TestProxyDeviceID

	preAddExecuted := make(chan struct{})
	postAddExecuted := make(chan struct{})
	preAddExecutedPtr, postAddExecutedPtr := preAddExecuted, postAddExecuted

	devicesProxy, err := TestProxyRoot.CreateProxy(context.Background(), "/devices", false)
	if err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create devices proxy")
	}
	devicesProxy.RegisterCallback(PreAdd, commonCallback2, "PRE_ADD Device container changes")
	devicesProxy.RegisterCallback(PostAdd, commonCallback2, "POST_ADD Device container changes")

	// Register ADD instructions callbacks
	TestProxyRootDevice.RegisterCallback(PreAdd, commonChanCallback, "PreAdd instructions", &preAddExecutedPtr)
	TestProxyRootDevice.RegisterCallback(PostAdd, commonChanCallback, "PostAdd instructions", &postAddExecutedPtr)

	added, err := TestProxyRootDevice.Add(context.Background(), "/devices", TestProxyDevice, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add test proxy device due to error: %v", err)
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}

	if !verifyGotResponse(preAddExecuted) {
		t.Error("PreAdd callback was not executed")
	}
	if !verifyGotResponse(postAddExecuted) {
		t.Error("PostAdd callback was not executed")
	}

	// Verify that the added device can now be retrieved
	d, err := TestProxyRootDevice.Get(context.Background(), "/devices/"+TestProxyDeviceID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed get device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_1_2_Add_ExistingDevice(t *testing.T) {
	TestProxyDevice.Id = TestProxyDeviceID

	added, err := TestProxyRootDevice.Add(context.Background(), "/devices", TestProxyDevice, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add device to test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if added.(proto.Message).String() != reflect.ValueOf(TestProxyDevice).Interface().(proto.Message).String() {
		t.Errorf("Devices don't match - existing: %+v returned: %+v", TestProxyLogicalDevice, added)
	}
}

func verifyGotResponse(callbackIndicator <-chan struct{}) bool {
	timeout := time.After(1 * time.Second)
	// Wait until the channel closes, or we time out
	select {
	case <-callbackIndicator:
		// Received response successfully
		return true

	case <-timeout:
		// Got a timeout! fail with a timeout error
		return false
	}
}

func TestProxy_1_1_3_Add_NewAdapter(t *testing.T) {
	TestProxyAdapterID = "test-adapter"
	TestProxyAdapter.Id = TestProxyAdapterID
	preAddExecuted := make(chan struct{})
	postAddExecuted := make(chan struct{})
	preAddExecutedPtr, postAddExecutedPtr := preAddExecuted, postAddExecuted

	// Register ADD instructions callbacks
	TestProxyRootAdapter.RegisterCallback(PreAdd, commonChanCallback, "PreAdd instructions for adapters", &preAddExecutedPtr)
	TestProxyRootAdapter.RegisterCallback(PostAdd, commonChanCallback, "PostAdd instructions for adapters", &postAddExecutedPtr)

	// Add the adapter
	added, err := TestProxyRootAdapter.Add(context.Background(), "/adapters", TestProxyAdapter, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add adapter to test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add adapter")
	} else {
		t.Logf("Added adapter : %+v", added)
	}

	verifyGotResponse(postAddExecuted)

	// Verify that the added device can now be retrieved
	d, err := TestProxyRootAdapter.Get(context.Background(), "/adapters/"+TestProxyAdapterID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to retrieve device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added adapter")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found adapter: %s", string(djson))
	}

	if !verifyGotResponse(preAddExecuted) {
		t.Error("PreAdd callback was not executed")
	}
	if !verifyGotResponse(postAddExecuted) {
		t.Error("PostAdd callback was not executed")
	}
}

func TestProxy_1_2_1_Get_AllDevices(t *testing.T) {
	devices, err := TestProxyRootDevice.Get(context.Background(), "/devices", 1, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get all devices info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		TestProxyTargetDeviceID = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved all devices: %+v", devices)
	}
}

func TestProxy_1_2_2_Get_SingleDevice(t *testing.T) {
	d, err := TestProxyRootDevice.Get(context.Background(), "/devices/"+TestProxyTargetDeviceID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get single device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", TestProxyTargetDeviceID)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_3_1_Update_Device(t *testing.T) {
	var fwVersion int

	preUpdateExecuted := make(chan struct{})
	postUpdateExecuted := make(chan struct{})
	preUpdateExecutedPtr, postUpdateExecutedPtr := preUpdateExecuted, postUpdateExecuted

	retrieved, err := TestProxyRootDevice.Get(context.Background(), "/devices/"+TestProxyTargetDeviceID, 1, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get device info from test proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if retrieved == nil {
		t.Error("Failed to get device")
	} else {
		t.Logf("Found raw device (root proxy): %+v", retrieved)

		if retrieved.(*voltha.Device).FirmwareVersion == "n/a" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.Device).FirmwareVersion)
			fwVersion++
		}

		retrieved.(*voltha.Device).FirmwareVersion = strconv.Itoa(fwVersion)

		TestProxyRootDevice.RegisterCallback(
			PreUpdate,
			commonChanCallback,
			"PreUpdate instructions (root proxy)", &preUpdateExecutedPtr,
		)
		TestProxyRootDevice.RegisterCallback(
			PostUpdate,
			commonChanCallback,
			"PostUpdate instructions (root proxy)", &postUpdateExecutedPtr,
		)

		afterUpdate, err := TestProxyRootDevice.Update(context.Background(), "/devices/"+TestProxyTargetDeviceID, retrieved, false, "")
		if err != nil {
			BenchmarkProxyLogger.Errorf("Failed to update device info test proxy due to error: %v", err)
			assert.NotNil(t, err)
		}
		if afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}

		if !verifyGotResponse(preUpdateExecuted) {
			t.Error("PreUpdate callback was not executed")
		}
		if !verifyGotResponse(postUpdateExecuted) {
			t.Error("PostUpdate callback was not executed")
		}

		d, err := TestProxyRootDevice.Get(context.Background(), "/devices/"+TestProxyTargetDeviceID, 1, false, "")
		if err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get device info from test proxy due to error: %v", err)
			assert.NotNil(t, err)
		}
		if !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_1_3_2_Update_DeviceFlows(t *testing.T) {
	// Get a device proxy and update a specific port
	devFlowsProxy, err := TestProxyRoot.CreateProxy(context.Background(), "/devices/"+TestProxyDeviceID+"/flows", false)
	if err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create device flows proxy")
	}
	flows, err := devFlowsProxy.Get(context.Background(), "/", 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get flows from device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	flows.(*openflow_13.Flows).Items[0].TableId = 2244

	preUpdateExecuted := make(chan struct{})
	postUpdateExecuted := make(chan struct{})
	preUpdateExecutedPtr, postUpdateExecutedPtr := preUpdateExecuted, postUpdateExecuted

	devFlowsProxy.RegisterCallback(
		PreUpdate,
		commonChanCallback,
		"PreUpdate instructions (flows proxy)", &preUpdateExecutedPtr,
	)
	devFlowsProxy.RegisterCallback(
		PostUpdate,
		commonChanCallback,
		"PostUpdate instructions (flows proxy)", &postUpdateExecutedPtr,
	)

	kvFlows, err := devFlowsProxy.Get(context.Background(), "/", 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get flows from device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}

	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	updated, err := devFlowsProxy.Update(context.Background(), "/", flows.(*openflow_13.Flows), false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to update flows in device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if updated == nil {
		t.Error("Failed to update flow")
	} else {
		t.Logf("Updated flows : %+v", updated)
	}

	if !verifyGotResponse(preUpdateExecuted) {
		t.Error("PreUpdate callback was not executed")
	}
	if !verifyGotResponse(postUpdateExecuted) {
		t.Error("PostUpdate callback was not executed")
	}

	d, err := devFlowsProxy.Get(context.Background(), "/", 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get flows in device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if d == nil {
		t.Error("Failed to find updated flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	d, err = TestProxyRootDevice.Get(context.Background(), "/devices/"+TestProxyDeviceID+"/flows", 1, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get flows from device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find updated flows (root proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (root proxy): %s", string(djson))
	}
}

func TestProxy_1_3_3_Update_Adapter(t *testing.T) {
	preUpdateExecuted := make(chan struct{})
	postUpdateExecuted := make(chan struct{})
	preUpdateExecutedPtr, postUpdateExecutedPtr := preUpdateExecuted, postUpdateExecuted

	adaptersProxy, err := TestProxyRoot.CreateProxy(context.Background(), "/adapters", false)
	if err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create adapters proxy")
	}
	retrieved, err := TestProxyRootAdapter.Get(context.Background(), "/adapters/"+TestProxyAdapterID, 1, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to retrieve adapter info from adapters proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if retrieved == nil {
		t.Error("Failed to get adapter")
	} else {
		t.Logf("Found raw adapter (root proxy): %+v", retrieved)

		retrieved.(*voltha.Adapter).Version = "test-adapter-version-2"

		adaptersProxy.RegisterCallback(
			PreUpdate,
			commonChanCallback,
			"PreUpdate instructions for adapters", &preUpdateExecutedPtr,
		)
		adaptersProxy.RegisterCallback(
			PostUpdate,
			commonChanCallback,
			"PostUpdate instructions for adapters", &postUpdateExecutedPtr,
		)

		afterUpdate, err := adaptersProxy.Update(context.Background(), "/"+TestProxyAdapterID, retrieved, false, "")
		if err != nil {
			BenchmarkProxyLogger.Errorf("Failed to update adapter info in adapters proxy due to error: %v", err)
			assert.NotNil(t, err)
		}
		if afterUpdate == nil {
			t.Error("Failed to update adapter")
		} else {
			t.Logf("Updated adapter : %+v", afterUpdate)
		}

		if !verifyGotResponse(preUpdateExecuted) {
			t.Error("PreUpdate callback for adapter was not executed")
		}
		if !verifyGotResponse(postUpdateExecuted) {
			t.Error("PostUpdate callback for adapter was not executed")
		}

		d, err := TestProxyRootAdapter.Get(context.Background(), "/adapters/"+TestProxyAdapterID, 1, false, "")
		if err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get updated adapter info from adapters proxy due to error: %v", err)
			assert.NotNil(t, err)
		}
		if !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated adapter (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found adapter (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_1_4_1_Remove_Device(t *testing.T) {
	preRemoveExecuted := make(chan struct{})
	postRemoveExecuted := make(chan struct{})
	preRemoveExecutedPtr, postRemoveExecutedPtr := preRemoveExecuted, postRemoveExecuted

	TestProxyRootDevice.RegisterCallback(
		PreRemove,
		commonChanCallback,
		"PreRemove instructions (root proxy)", &preRemoveExecutedPtr,
	)
	TestProxyRootDevice.RegisterCallback(
		PostRemove,
		commonChanCallback,
		"PostRemove instructions (root proxy)", &postRemoveExecutedPtr,
	)

	removed, err := TestProxyRootDevice.Remove(context.Background(), "/devices/"+TestProxyDeviceID, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to remove device from devices proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}

	if !verifyGotResponse(preRemoveExecuted) {
		t.Error("PreRemove callback was not executed")
	}
	if !verifyGotResponse(postRemoveExecuted) {
		t.Error("PostRemove callback was not executed")
	}

	d, err := TestProxyRootDevice.Get(context.Background(), "/devices/"+TestProxyDeviceID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get device info from devices proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if reflect.ValueOf(d).IsValid() {
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

	preAddExecuted := make(chan struct{})
	postAddExecuted := make(chan struct{})
	preAddExecutedPtr, postAddExecutedPtr := preAddExecuted, postAddExecuted

	// Register
	TestProxyRootLogicalDevice.RegisterCallback(PreAdd, commonChanCallback, "PreAdd instructions", &preAddExecutedPtr)
	TestProxyRootLogicalDevice.RegisterCallback(PostAdd, commonChanCallback, "PostAdd instructions", &postAddExecutedPtr)

	added, err := TestProxyRootLogicalDevice.Add(context.Background(), "/logical_devices", TestProxyLogicalDevice, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add new logical device into proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	} else {
		t.Logf("Added logical device : %+v", added)
	}

	verifyGotResponse(postAddExecuted)

	ld, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxyLogicalDeviceID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical device info from logical device proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(ld).IsValid() {
		t.Error("Failed to find added logical device")
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

	if !verifyGotResponse(preAddExecuted) {
		t.Error("PreAdd callback was not executed")
	}
	if !verifyGotResponse(postAddExecuted) {
		t.Error("PostAdd callback was not executed")
	}
}

func TestProxy_2_1_2_Add_ExistingLogicalDevice(t *testing.T) {
	TestProxyLogicalDevice.Id = TestProxyLogicalDeviceID

	added, err := TestProxyRootLogicalDevice.Add(context.Background(), "/logical_devices", TestProxyLogicalDevice, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to add logical device due to error: %v", err)
		assert.NotNil(t, err)
	}
	if added.(proto.Message).String() != reflect.ValueOf(TestProxyLogicalDevice).Interface().(proto.Message).String() {
		t.Errorf("Logical devices don't match - existing: %+v returned: %+v", TestProxyLogicalDevice, added)
	}
}

func TestProxy_2_2_1_Get_AllLogicalDevices(t *testing.T) {
	logicalDevices, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices", 1, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get all logical devices from proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if len(logicalDevices.([]interface{})) == 0 {
		t.Error("there are no available logical devices to retrieve")
	} else {
		// Save the target device id for later tests
		TestProxyTargetLogicalDeviceID = logicalDevices.([]interface{})[0].(*voltha.LogicalDevice).Id
		t.Logf("retrieved all logical devices: %+v", logicalDevices)
	}
}

func TestProxy_2_2_2_Get_SingleLogicalDevice(t *testing.T) {
	ld, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxyTargetLogicalDeviceID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get single logical device from proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(ld).IsValid() {
		t.Errorf("Failed to find logical device : %s", TestProxyTargetLogicalDeviceID)
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

}

func TestProxy_2_3_1_Update_LogicalDevice(t *testing.T) {
	var fwVersion int
	preUpdateExecuted := make(chan struct{})
	postUpdateExecuted := make(chan struct{})
	preUpdateExecutedPtr, postUpdateExecutedPtr := preUpdateExecuted, postUpdateExecuted

	retrieved, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxyTargetLogicalDeviceID, 1, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical devices due to error: %v", err)
		assert.NotNil(t, err)
	}
	if retrieved == nil {
		t.Error("Failed to get logical device")
	} else {
		t.Logf("Found raw logical device (root proxy): %+v", retrieved)

		if retrieved.(*voltha.LogicalDevice).RootDeviceId == "" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.LogicalDevice).RootDeviceId)
			fwVersion++
		}

		TestProxyRootLogicalDevice.RegisterCallback(
			PreUpdate,
			commonChanCallback,
			"PreUpdate instructions (root proxy)", &preUpdateExecutedPtr,
		)
		TestProxyRootLogicalDevice.RegisterCallback(
			PostUpdate,
			commonChanCallback,
			"PostUpdate instructions (root proxy)", &postUpdateExecutedPtr,
		)

		retrieved.(*voltha.LogicalDevice).RootDeviceId = strconv.Itoa(fwVersion)

		afterUpdate, err := TestProxyRootLogicalDevice.Update(context.Background(), "/logical_devices/"+TestProxyTargetLogicalDeviceID, retrieved, false,
			"")
		if err != nil {
			BenchmarkProxyLogger.Errorf("Faield to update logical device info due to error: %v", err)
			assert.NotNil(t, err)
		}
		if afterUpdate == nil {
			t.Error("Failed to update logical device")
		} else {
			t.Logf("Updated logical device : %+v", afterUpdate)
		}

		if !verifyGotResponse(preUpdateExecuted) {
			t.Error("PreUpdate callback was not executed")
		}
		if !verifyGotResponse(postUpdateExecuted) {
			t.Error("PostUpdate callback was not executed")
		}

		d, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxyTargetLogicalDeviceID, 1, false, "")
		if err != nil {
			BenchmarkProxyLogger.Errorf("Failed to get logical device info due to error: %v", err)
			assert.NotNil(t, err)
		}
		if !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated logical device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)

			t.Logf("Found logical device (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_2_3_2_Update_LogicalDeviceFlows(t *testing.T) {
	// Get a device proxy and update a specific port
	ldFlowsProxy, err := TestProxyRoot.CreateProxy(context.Background(), "/logical_devices/"+TestProxyLogicalDeviceID+"/flows", false)
	if err != nil {
		log.With(log.Fields{"error": err}).Fatal("Failed to create logical device flows proxy")
	}
	flows, err := ldFlowsProxy.Get(context.Background(), "/", 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get flows from logical device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	flows.(*openflow_13.Flows).Items[0].TableId = rand.Uint32()
	t.Logf("before updated flows: %+v", flows)

	ldFlowsProxy.RegisterCallback(
		PreUpdate,
		commonCallback2,
	)
	ldFlowsProxy.RegisterCallback(
		PostUpdate,
		commonCallback2,
	)

	kvFlows, err := ldFlowsProxy.Get(context.Background(), "/", 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Faield to get flows from logical device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	updated, err := ldFlowsProxy.Update(context.Background(), "/", flows.(*openflow_13.Flows), false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to update flows in logical device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if updated == nil {
		t.Error("Failed to update logical device flows")
	} else {
		t.Logf("Updated logical device flows : %+v", updated)
	}

	if d, _ := ldFlowsProxy.Get(context.Background(), "/", 0, false, ""); d == nil {
		t.Error("Failed to find updated logical device flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	d, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxyLogicalDeviceID+"/flows", 0, false,
		"")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get flows from logical device flows proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find updated logical device flows (root proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found logical device flows (root proxy): %s", string(djson))
	}
}

func TestProxy_2_4_1_Remove_Device(t *testing.T) {
	preRemoveExecuted := make(chan struct{})
	postRemoveExecuted := make(chan struct{})
	preRemoveExecutedPtr, postRemoveExecutedPtr := preRemoveExecuted, postRemoveExecuted

	TestProxyRootLogicalDevice.RegisterCallback(
		PreRemove,
		commonChanCallback,
		"PreRemove instructions (root proxy)", &preRemoveExecutedPtr,
	)
	TestProxyRootLogicalDevice.RegisterCallback(
		PostRemove,
		commonChanCallback,
		"PostRemove instructions (root proxy)", &postRemoveExecutedPtr,
	)

	removed, err := TestProxyRootLogicalDevice.Remove(context.Background(), "/logical_devices/"+TestProxyLogicalDeviceID, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to remove device from logical devices proxy due to error: %v", err)
		assert.NotNil(t, err)
	}
	if removed == nil {
		t.Error("Failed to remove logical device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}

	if !verifyGotResponse(preRemoveExecuted) {
		t.Error("PreRemove callback was not executed")
	}
	if !verifyGotResponse(postRemoveExecuted) {
		t.Error("PostRemove callback was not executed")
	}

	d, err := TestProxyRootLogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxyLogicalDeviceID, 0, false, "")
	if err != nil {
		BenchmarkProxyLogger.Errorf("Failed to get logical device info due to error: %v", err)
		assert.NotNil(t, err)
	}
	if reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxyLogicalDeviceID)
	}
}

// -----------------------------
// Callback tests
// -----------------------------

func TestProxy_Callbacks_1_Register(t *testing.T) {
	TestProxyRootDevice.RegisterCallback(PreAdd, firstCallback, "abcde", "12345")

	m := make(map[string]string)
	m["name"] = "fghij"
	TestProxyRootDevice.RegisterCallback(PreAdd, secondCallback, m, 1.2345)

	d := &voltha.Device{Id: "12345"}
	TestProxyRootDevice.RegisterCallback(PreAdd, thirdCallback, "klmno", d)
}

func TestProxy_Callbacks_2_Invoke_WithNoInterruption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	TestProxyRootDevice.InvokeCallbacks(ctx, PreAdd, false, nil)
}

func TestProxy_Callbacks_3_Invoke_WithInterruption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	TestProxyRootDevice.InvokeCallbacks(ctx, PreAdd, true, nil)
}

func TestProxy_Callbacks_4_Unregister(t *testing.T) {
	TestProxyRootDevice.UnregisterCallback(PreAdd, firstCallback)
	TestProxyRootDevice.UnregisterCallback(PreAdd, secondCallback)
	TestProxyRootDevice.UnregisterCallback(PreAdd, thirdCallback)
}

//func TestProxy_Callbacks_5_Add(t *testing.T) {
//	TestProxyRootDevice.Root.AddCallback(TestProxyRootDevice.InvokeCallbacks, PostUpdate, false, "some data", "some new data")
//}
//
//func TestProxy_Callbacks_6_Execute(t *testing.T) {
//	TestProxyRootDevice.Root.ExecuteCallbacks()
//}
