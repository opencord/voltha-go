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
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/opencord/voltha-protos/go/common"
	"github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"
)

var (
	TestProxy_Root                  *root
	TestProxy_Root_LogicalDevice    *Proxy
	TestProxy_Root_Device           *Proxy
	TestProxy_Root_Adapter          *Proxy
	TestProxy_DeviceId              string
	TestProxy_AdapterId             string
	TestProxy_LogicalDeviceId       string
	TestProxy_TargetDeviceId        string
	TestProxy_TargetLogicalDeviceId string
	TestProxy_LogicalPorts          []*voltha.LogicalPort
	TestProxy_Ports                 []*voltha.Port
	TestProxy_Stats                 *openflow_13.OfpFlowStats
	TestProxy_Flows                 *openflow_13.Flows
	TestProxy_Device                *voltha.Device
	TestProxy_LogicalDevice         *voltha.LogicalDevice
	TestProxy_Adapter               *voltha.Adapter
)

func init() {
	//log.AddPackage(log.JSON, log.InfoLevel, log.Fields{"instanceId": "DB_MODEL"})
	//log.UpdateAllLoggers(log.Fields{"instanceId": "PROXY_LOAD_TEST"})
	TestProxy_Root = NewRoot(&voltha.Voltha{}, nil)
	TestProxy_Root_LogicalDevice = TestProxy_Root.CreateProxy(context.Background(), "/", false)
	TestProxy_Root_Device = TestProxy_Root.CreateProxy(context.Background(), "/", false)
	TestProxy_Root_Adapter = TestProxy_Root.CreateProxy(context.Background(), "/", false)

	TestProxy_LogicalPorts = []*voltha.LogicalPort{
		{
			Id:           "123",
			DeviceId:     "logicalport-0-device-id",
			DevicePortNo: 123,
			RootPort:     false,
		},
	}
	TestProxy_Ports = []*voltha.Port{
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

	TestProxy_Stats = &openflow_13.OfpFlowStats{
		Id: 1111,
	}
	TestProxy_Flows = &openflow_13.Flows{
		Items: []*openflow_13.OfpFlowStats{TestProxy_Stats},
	}
	TestProxy_Device = &voltha.Device{
		Id:         TestProxy_DeviceId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Flows:      TestProxy_Flows,
		Ports:      TestProxy_Ports,
	}

	TestProxy_LogicalDevice = &voltha.LogicalDevice{
		Id:         TestProxy_DeviceId,
		DatapathId: 0,
		Ports:      TestProxy_LogicalPorts,
		Flows:      TestProxy_Flows,
	}

	TestProxy_Adapter = &voltha.Adapter{
		Id:      TestProxy_AdapterId,
		Vendor:  "test-adapter-vendor",
		Version: "test-adapter-version",
	}
}

func TestProxy_1_1_1_Add_NewDevice(t *testing.T) {
	devIDBin, _ := uuid.New().MarshalBinary()
	TestProxy_DeviceId = "0001" + hex.EncodeToString(devIDBin)[:12]
	TestProxy_Device.Id = TestProxy_DeviceId

	preAddExecuted := make(chan struct{})
	postAddExecuted := make(chan struct{})
	preAddExecutedPtr, postAddExecutedPtr := preAddExecuted, postAddExecuted

	devicesProxy := TestProxy_Root.node.CreateProxy(context.Background(), "/devices", false)
	devicesProxy.RegisterCallback(PRE_ADD, commonCallback2, "PRE_ADD Device container changes")
	devicesProxy.RegisterCallback(POST_ADD, commonCallback2, "POST_ADD Device container changes")

	// Register ADD instructions callbacks
	TestProxy_Root_Device.RegisterCallback(PRE_ADD, commonChanCallback, "PRE_ADD instructions", &preAddExecutedPtr)
	TestProxy_Root_Device.RegisterCallback(POST_ADD, commonChanCallback, "POST_ADD instructions", &postAddExecutedPtr)

	if added := TestProxy_Root_Device.Add(context.Background(), "/devices", TestProxy_Device, ""); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}

	if !verifyGotResponse(preAddExecuted) {
		t.Error("PRE_ADD callback was not executed")
	}
	if !verifyGotResponse(postAddExecuted) {
		t.Error("POST_ADD callback was not executed")
	}

	// Verify that the added device can now be retrieved
	if d := TestProxy_Root_Device.Get(context.Background(), "/devices/"+TestProxy_DeviceId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}
}

func TestProxy_1_1_2_Add_ExistingDevice(t *testing.T) {
	TestProxy_Device.Id = TestProxy_DeviceId

	added := TestProxy_Root_Device.Add(context.Background(), "/devices", TestProxy_Device, "")
	if added.(proto.Message).String() != reflect.ValueOf(TestProxy_Device).Interface().(proto.Message).String() {
		t.Errorf("Devices don't match - existing: %+v returned: %+v", TestProxy_LogicalDevice, added)
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
	TestProxy_AdapterId = "test-adapter"
	TestProxy_Adapter.Id = TestProxy_AdapterId
	preAddExecuted := make(chan struct{})
	postAddExecuted := make(chan struct{})
	preAddExecutedPtr, postAddExecutedPtr := preAddExecuted, postAddExecuted

	// Register ADD instructions callbacks
	TestProxy_Root_Adapter.RegisterCallback(PRE_ADD, commonChanCallback, "PRE_ADD instructions for adapters", &preAddExecutedPtr)
	TestProxy_Root_Adapter.RegisterCallback(POST_ADD, commonChanCallback, "POST_ADD instructions for adapters", &postAddExecutedPtr)

	// Add the adapter
	if added := TestProxy_Root_Adapter.Add(context.Background(), "/adapters", TestProxy_Adapter, ""); added == nil {
		t.Error("Failed to add adapter")
	} else {
		t.Logf("Added adapter : %+v", added)
	}

	verifyGotResponse(postAddExecuted)

	// Verify that the added device can now be retrieved
	if d := TestProxy_Root_Adapter.Get(context.Background(), "/adapters/"+TestProxy_AdapterId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added adapter")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found adapter: %s", string(djson))
	}

	if !verifyGotResponse(preAddExecuted) {
		t.Error("PRE_ADD callback was not executed")
	}
	if !verifyGotResponse(postAddExecuted) {
		t.Error("POST_ADD callback was not executed")
	}
}

func TestProxy_1_2_1_Get_AllDevices(t *testing.T) {
	devices := TestProxy_Root_Device.Get(context.Background(), "/devices", 1, false, "")

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		TestProxy_TargetDeviceId = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved all devices: %+v", devices)
	}
}

func TestProxy_1_2_2_Get_SingleDevice(t *testing.T) {
	if d := TestProxy_Root_Device.Get(context.Background(), "/devices/"+TestProxy_TargetDeviceId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", TestProxy_TargetDeviceId)
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

	if retrieved := TestProxy_Root_Device.Get(context.Background(), "/devices/"+TestProxy_TargetDeviceId, 1, false, ""); retrieved == nil {
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

		TestProxy_Root_Device.RegisterCallback(
			PRE_UPDATE,
			commonChanCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecutedPtr,
		)
		TestProxy_Root_Device.RegisterCallback(
			POST_UPDATE,
			commonChanCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecutedPtr,
		)

		if afterUpdate := TestProxy_Root_Device.Update(context.Background(), "/devices/"+TestProxy_TargetDeviceId, retrieved, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}

		if !verifyGotResponse(preUpdateExecuted) {
			t.Error("PRE_UPDATE callback was not executed")
		}
		if !verifyGotResponse(postUpdateExecuted) {
			t.Error("POST_UPDATE callback was not executed")
		}

		if d := TestProxy_Root_Device.Get(context.Background(), "/devices/"+TestProxy_TargetDeviceId, 1, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_1_3_2_Update_DeviceFlows(t *testing.T) {
	// Get a device proxy and update a specific port
	devFlowsProxy := TestProxy_Root.node.CreateProxy(context.Background(), "/devices/"+TestProxy_DeviceId+"/flows", false)
	flows := devFlowsProxy.Get(context.Background(), "/", 0, false, "")
	flows.(*openflow_13.Flows).Items[0].TableId = 2244

	preUpdateExecuted := make(chan struct{})
	postUpdateExecuted := make(chan struct{})
	preUpdateExecutedPtr, postUpdateExecutedPtr := preUpdateExecuted, postUpdateExecuted

	devFlowsProxy.RegisterCallback(
		PRE_UPDATE,
		commonChanCallback,
		"PRE_UPDATE instructions (flows proxy)", &preUpdateExecutedPtr,
	)
	devFlowsProxy.RegisterCallback(
		POST_UPDATE,
		commonChanCallback,
		"POST_UPDATE instructions (flows proxy)", &postUpdateExecutedPtr,
	)

	kvFlows := devFlowsProxy.Get(context.Background(), "/", 0, false, "")

	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	if updated := devFlowsProxy.Update(context.Background(), "/", flows.(*openflow_13.Flows), false, ""); updated == nil {
		t.Error("Failed to update flow")
	} else {
		t.Logf("Updated flows : %+v", updated)
	}

	if !verifyGotResponse(preUpdateExecuted) {
		t.Error("PRE_UPDATE callback was not executed")
	}
	if !verifyGotResponse(postUpdateExecuted) {
		t.Error("POST_UPDATE callback was not executed")
	}

	if d := devFlowsProxy.Get(context.Background(), "/", 0, false, ""); d == nil {
		t.Error("Failed to find updated flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	if d := TestProxy_Root_Device.Get(context.Background(), "/devices/"+TestProxy_DeviceId+"/flows", 1, false, ""); !reflect.ValueOf(d).IsValid() {
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

	adaptersProxy := TestProxy_Root.node.CreateProxy(context.Background(), "/adapters", false)

	if retrieved := TestProxy_Root_Adapter.Get(context.Background(), "/adapters/"+TestProxy_AdapterId, 1, false, ""); retrieved == nil {
		t.Error("Failed to get adapter")
	} else {
		t.Logf("Found raw adapter (root proxy): %+v", retrieved)

		retrieved.(*voltha.Adapter).Version = "test-adapter-version-2"

		adaptersProxy.RegisterCallback(
			PRE_UPDATE,
			commonChanCallback,
			"PRE_UPDATE instructions for adapters", &preUpdateExecutedPtr,
		)
		adaptersProxy.RegisterCallback(
			POST_UPDATE,
			commonChanCallback,
			"POST_UPDATE instructions for adapters", &postUpdateExecutedPtr,
		)

		if afterUpdate := adaptersProxy.Update(context.Background(), "/"+TestProxy_AdapterId, retrieved, false, ""); afterUpdate == nil {
			t.Error("Failed to update adapter")
		} else {
			t.Logf("Updated adapter : %+v", afterUpdate)
		}

		if !verifyGotResponse(preUpdateExecuted) {
			t.Error("PRE_UPDATE callback for adapter was not executed")
		}
		if !verifyGotResponse(postUpdateExecuted) {
			t.Error("POST_UPDATE callback for adapter was not executed")
		}

		if d := TestProxy_Root_Adapter.Get(context.Background(), "/adapters/"+TestProxy_AdapterId, 1, false, ""); !reflect.ValueOf(d).IsValid() {
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

	TestProxy_Root_Device.RegisterCallback(
		PRE_REMOVE,
		commonChanCallback,
		"PRE_REMOVE instructions (root proxy)", &preRemoveExecutedPtr,
	)
	TestProxy_Root_Device.RegisterCallback(
		POST_REMOVE,
		commonChanCallback,
		"POST_REMOVE instructions (root proxy)", &postRemoveExecutedPtr,
	)

	if removed := TestProxy_Root_Device.Remove(context.Background(), "/devices/"+TestProxy_DeviceId, ""); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}

	if !verifyGotResponse(preRemoveExecuted) {
		t.Error("PRE_REMOVE callback was not executed")
	}
	if !verifyGotResponse(postRemoveExecuted) {
		t.Error("POST_REMOVE callback was not executed")
	}

	if d := TestProxy_Root_Device.Get(context.Background(), "/devices/"+TestProxy_DeviceId, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxy_DeviceId)
	}
}

func TestProxy_2_1_1_Add_NewLogicalDevice(t *testing.T) {

	ldIDBin, _ := uuid.New().MarshalBinary()
	TestProxy_LogicalDeviceId = "0001" + hex.EncodeToString(ldIDBin)[:12]
	TestProxy_LogicalDevice.Id = TestProxy_LogicalDeviceId

	preAddExecuted := make(chan struct{})
	postAddExecuted := make(chan struct{})
	preAddExecutedPtr, postAddExecutedPtr := preAddExecuted, postAddExecuted

	// Register
	TestProxy_Root_LogicalDevice.RegisterCallback(PRE_ADD, commonChanCallback, "PRE_ADD instructions", &preAddExecutedPtr)
	TestProxy_Root_LogicalDevice.RegisterCallback(POST_ADD, commonChanCallback, "POST_ADD instructions", &postAddExecutedPtr)

	if added := TestProxy_Root_LogicalDevice.Add(context.Background(), "/logical_devices", TestProxy_LogicalDevice, ""); added == nil {
		t.Error("Failed to add logical device")
	} else {
		t.Logf("Added logical device : %+v", added)
	}

	verifyGotResponse(postAddExecuted)

	if ld := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxy_LogicalDeviceId, 0, false, ""); !reflect.ValueOf(ld).IsValid() {
		t.Error("Failed to find added logical device")
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

	if !verifyGotResponse(preAddExecuted) {
		t.Error("PRE_ADD callback was not executed")
	}
	if !verifyGotResponse(postAddExecuted) {
		t.Error("POST_ADD callback was not executed")
	}
}

func TestProxy_2_1_2_Add_ExistingLogicalDevice(t *testing.T) {
	TestProxy_LogicalDevice.Id = TestProxy_LogicalDeviceId

	added := TestProxy_Root_LogicalDevice.Add(context.Background(), "/logical_devices", TestProxy_LogicalDevice, "")
	if added.(proto.Message).String() != reflect.ValueOf(TestProxy_LogicalDevice).Interface().(proto.Message).String() {
		t.Errorf("Logical devices don't match - existing: %+v returned: %+v", TestProxy_LogicalDevice, added)
	}
}

func TestProxy_2_2_1_Get_AllLogicalDevices(t *testing.T) {
	logicalDevices := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices", 1, false, "")

	if len(logicalDevices.([]interface{})) == 0 {
		t.Error("there are no available logical devices to retrieve")
	} else {
		// Save the target device id for later tests
		TestProxy_TargetLogicalDeviceId = logicalDevices.([]interface{})[0].(*voltha.LogicalDevice).Id
		t.Logf("retrieved all logical devices: %+v", logicalDevices)
	}
}

func TestProxy_2_2_2_Get_SingleLogicalDevice(t *testing.T) {
	if ld := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxy_TargetLogicalDeviceId, 0, false, ""); !reflect.ValueOf(ld).IsValid() {
		t.Errorf("Failed to find logical device : %s", TestProxy_TargetLogicalDeviceId)
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

	if retrieved := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxy_TargetLogicalDeviceId, 1, false, ""); retrieved == nil {
		t.Error("Failed to get logical device")
	} else {
		t.Logf("Found raw logical device (root proxy): %+v", retrieved)

		if retrieved.(*voltha.LogicalDevice).RootDeviceId == "" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.LogicalDevice).RootDeviceId)
			fwVersion++
		}

		TestProxy_Root_LogicalDevice.RegisterCallback(
			PRE_UPDATE,
			commonChanCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecutedPtr,
		)
		TestProxy_Root_LogicalDevice.RegisterCallback(
			POST_UPDATE,
			commonChanCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecutedPtr,
		)

		retrieved.(*voltha.LogicalDevice).RootDeviceId = strconv.Itoa(fwVersion)

		if afterUpdate := TestProxy_Root_LogicalDevice.Update(context.Background(), "/logical_devices/"+TestProxy_TargetLogicalDeviceId, retrieved, false,
			""); afterUpdate == nil {
			t.Error("Failed to update logical device")
		} else {
			t.Logf("Updated logical device : %+v", afterUpdate)
		}

		if !verifyGotResponse(preUpdateExecuted) {
			t.Error("PRE_UPDATE callback was not executed")
		}
		if !verifyGotResponse(postUpdateExecuted) {
			t.Error("POST_UPDATE callback was not executed")
		}

		if d := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxy_TargetLogicalDeviceId, 1, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated logical device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)

			t.Logf("Found logical device (root proxy): %s raw: %+v", string(djson), d)
		}
	}
}

func TestProxy_2_3_2_Update_LogicalDeviceFlows(t *testing.T) {
	// Get a device proxy and update a specific port
	ldFlowsProxy := TestProxy_Root.node.CreateProxy(context.Background(), "/logical_devices/"+TestProxy_LogicalDeviceId+"/flows", false)
	flows := ldFlowsProxy.Get(context.Background(), "/", 0, false, "")
	flows.(*openflow_13.Flows).Items[0].TableId = rand.Uint32()
	t.Logf("before updated flows: %+v", flows)

	ldFlowsProxy.RegisterCallback(
		PRE_UPDATE,
		commonCallback2,
	)
	ldFlowsProxy.RegisterCallback(
		POST_UPDATE,
		commonCallback2,
	)

	kvFlows := ldFlowsProxy.Get(context.Background(), "/", 0, false, "")

	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	if updated := ldFlowsProxy.Update(context.Background(), "/", flows.(*openflow_13.Flows), false, ""); updated == nil {
		t.Error("Failed to update logical device flows")
	} else {
		t.Logf("Updated logical device flows : %+v", updated)
	}

	if d := ldFlowsProxy.Get(context.Background(), "/", 0, false, ""); d == nil {
		t.Error("Failed to find updated logical device flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	if d := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxy_LogicalDeviceId+"/flows", 0, false,
		""); !reflect.ValueOf(d).IsValid() {
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

	TestProxy_Root_LogicalDevice.RegisterCallback(
		PRE_REMOVE,
		commonChanCallback,
		"PRE_REMOVE instructions (root proxy)", &preRemoveExecutedPtr,
	)
	TestProxy_Root_LogicalDevice.RegisterCallback(
		POST_REMOVE,
		commonChanCallback,
		"POST_REMOVE instructions (root proxy)", &postRemoveExecutedPtr,
	)

	if removed := TestProxy_Root_LogicalDevice.Remove(context.Background(), "/logical_devices/"+TestProxy_LogicalDeviceId, ""); removed == nil {
		t.Error("Failed to remove logical device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}

	if !verifyGotResponse(preRemoveExecuted) {
		t.Error("PRE_REMOVE callback was not executed")
	}
	if !verifyGotResponse(postRemoveExecuted) {
		t.Error("POST_REMOVE callback was not executed")
	}

	if d := TestProxy_Root_LogicalDevice.Get(context.Background(), "/logical_devices/"+TestProxy_LogicalDeviceId, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", TestProxy_LogicalDeviceId)
	}
}

// -----------------------------
// Callback tests
// -----------------------------

func TestProxy_Callbacks_1_Register(t *testing.T) {
	TestProxy_Root_Device.RegisterCallback(PRE_ADD, firstCallback, "abcde", "12345")

	m := make(map[string]string)
	m["name"] = "fghij"
	TestProxy_Root_Device.RegisterCallback(PRE_ADD, secondCallback, m, 1.2345)

	d := &voltha.Device{Id: "12345"}
	TestProxy_Root_Device.RegisterCallback(PRE_ADD, thirdCallback, "klmno", d)
}

func TestProxy_Callbacks_2_Invoke_WithNoInterruption(t *testing.T) {
	TestProxy_Root_Device.InvokeCallbacks(PRE_ADD, false, nil)
}

func TestProxy_Callbacks_3_Invoke_WithInterruption(t *testing.T) {
	TestProxy_Root_Device.InvokeCallbacks(PRE_ADD, true, nil)
}

func TestProxy_Callbacks_4_Unregister(t *testing.T) {
	TestProxy_Root_Device.UnregisterCallback(PRE_ADD, firstCallback)
	TestProxy_Root_Device.UnregisterCallback(PRE_ADD, secondCallback)
	TestProxy_Root_Device.UnregisterCallback(PRE_ADD, thirdCallback)
}

//func TestProxy_Callbacks_5_Add(t *testing.T) {
//	TestProxy_Root_Device.Root.AddCallback(TestProxy_Root_Device.InvokeCallbacks, POST_UPDATE, false, "some data", "some new data")
//}
//
//func TestProxy_Callbacks_6_Execute(t *testing.T) {
//	TestProxy_Root_Device.Root.ExecuteCallbacks()
//}
