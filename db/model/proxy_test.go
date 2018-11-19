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
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

var (
	ldevProxy *Proxy
	devProxy *Proxy
	flowProxy *Proxy
)

func init() {
	ldevProxy = modelTestConfig.Root.node.CreateProxy("/", false)
	devProxy = modelTestConfig.Root.node.CreateProxy("/", false)
}

func Test_Proxy_1_1_1_Add_NewDevice(t *testing.T) {
	devIDBin, _ := uuid.New().MarshalBinary()
	devID = "0001" + hex.EncodeToString(devIDBin)[:12]
	device.Id = devID

	preAddExecuted := false
	postAddExecuted := false

	// Register ADD instructions callbacks
	devProxy.RegisterCallback(PRE_ADD, commonCallback, "PRE_ADD instructions", &preAddExecuted)
	devProxy.RegisterCallback(POST_ADD, commonCallback, "POST_ADD instructions", &postAddExecuted)

	// Add the device
	if added := devProxy.Add("/devices", device, ""); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}

	// Verify that the added device can now be retrieved
	if d := devProxy.Get("/devices/"+devID, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

	if !preAddExecuted {
		t.Error("PRE_ADD callback was not executed")
	}
	if !postAddExecuted {
		t.Error("POST_ADD callback was not executed")
	}
}

func Test_Proxy_1_1_2_Add_ExistingDevice(t *testing.T) {
	device.Id = devID

	if added := devProxy.Add("/devices", device, ""); added == nil {
		t.Logf("Successfully detected that the device already exists: %s", devID)
	} else {
		t.Errorf("A new device should not have been created : %+v", added)
	}

}

func Test_Proxy_1_2_1_Get_AllDevices(t *testing.T) {
	devices := devProxy.Get("/devices", 1, false, "")

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		targetDevID = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved all devices: %+v", devices)
	}
}

func Test_Proxy_1_2_2_Get_SingleDevice(t *testing.T) {
	if d := devProxy.Get("/devices/"+targetDevID, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", targetDevID)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

}

func Test_Proxy_1_3_1_Update_Device(t *testing.T) {
	var fwVersion int
	preUpdateExecuted := false
	postUpdateExecuted := false

	if retrieved := devProxy.Get("/devices/"+targetDevID, 1, false, ""); retrieved == nil {
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

		devProxy.RegisterCallback(
			PRE_UPDATE,
			commonCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecuted,
		)
		devProxy.RegisterCallback(
			POST_UPDATE,
			commonCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecuted,
		)

		if afterUpdate := devProxy.Update("/devices/"+targetDevID, retrieved, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}

		if d := devProxy.Get("/devices/"+targetDevID, 1, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s raw: %+v", string(djson), d)
		}

		if !preUpdateExecuted {
			t.Error("PRE_UPDATE callback was not executed")
		}
		if !postUpdateExecuted {
			t.Error("POST_UPDATE callback was not executed")
		}
	}
}

func Test_Proxy_1_3_2_Update_DeviceFlows(t *testing.T) {
	// Get a device proxy and update a specific port
	flowProxy = modelTestConfig.Root.node.CreateProxy("/devices/"+devID+"/flows", false)
	flows := flowProxy.Get("/", 0, false, "")
	flows.(*openflow_13.Flows).Items[0].TableId = 2244

	preUpdateExecuted := false
	postUpdateExecuted := false

	flowProxy.RegisterCallback(
		PRE_UPDATE,
		commonCallback,
		"PRE_UPDATE instructions (flows proxy)", &preUpdateExecuted,
	)
	flowProxy.RegisterCallback(
		POST_UPDATE,
		commonCallback,
		"POST_UPDATE instructions (flows proxy)", &postUpdateExecuted,
	)

	kvFlows := flowProxy.Get("/", 0, false, "")

	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	if updated := flowProxy.Update("/", flows.(*openflow_13.Flows), false, ""); updated == nil {
		t.Error("Failed to update flow")
	} else {
		t.Logf("Updated flows : %+v", updated)
	}

	if d := flowProxy.Get("/", 0, false, ""); d == nil {
		t.Error("Failed to find updated flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	if d := devProxy.Get("/devices/"+devID+"/flows", 1, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find updated flows (root proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (root proxy): %s", string(djson))
	}

	if !preUpdateExecuted {
		t.Error("PRE_UPDATE callback was not executed")
	}
	if !postUpdateExecuted {
		t.Error("POST_UPDATE callback was not executed")
	}
}

func Test_Proxy_1_4_1_Remove_Device(t *testing.T) {
	preRemoveExecuted := false
	postRemoveExecuted := false

	modelTestConfig.RootProxy.RegisterCallback(
		PRE_REMOVE,
		commonCallback,
		"PRE_REMOVE instructions (root proxy)", &preRemoveExecuted,
	)
	modelTestConfig.RootProxy.RegisterCallback(
		POST_REMOVE,
		commonCallback,
		"POST_REMOVE instructions (root proxy)", &postRemoveExecuted,
	)

	if removed := modelTestConfig.RootProxy.Remove("/devices/"+devID, ""); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	if d := modelTestConfig.RootProxy.Get("/devices/"+devID, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", devID)
	}

	if !preRemoveExecuted {
		t.Error("PRE_UPDATE callback was not executed")
	}
	if !postRemoveExecuted {
		t.Error("POST_UPDATE callback was not executed")
	}
}

func Test_Proxy_2_1_1_Add_NewLogicalDevice(t *testing.T) {

	ldIDBin, _ := uuid.New().MarshalBinary()
	ldevID = "0001" + hex.EncodeToString(ldIDBin)[:12]
	logicalDevice.Id = ldevID

	preAddExecuted := false
	postAddExecuted := false

	// Register
	ldevProxy.RegisterCallback(PRE_ADD, commonCallback, "PRE_ADD instructions", &preAddExecuted)
	ldevProxy.RegisterCallback(POST_ADD, commonCallback, "POST_ADD instructions", &postAddExecuted)

	if added := ldevProxy.Add("/logical_devices", logicalDevice, ""); added == nil {
		t.Error("Failed to add logical device")
	} else {
		t.Logf("Added logical device : %+v", added)
	}

	if ld := ldevProxy.Get("/logical_devices/"+ldevID, 0, false, ""); !reflect.ValueOf(ld).IsValid() {
		t.Error("Failed to find added logical device")
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

	if !preAddExecuted {
		t.Error("PRE_ADD callback was not executed")
	}
	if !postAddExecuted {
		t.Error("POST_ADD callback was not executed")
	}
}

func Test_Proxy_2_1_2_Add_ExistingLogicalDevice(t *testing.T) {
	logicalDevice.Id = ldevID
	if added := ldevProxy.Add("/logical_devices", logicalDevice, ""); added == nil {
		t.Logf("Successfully detected that the logical device already exists: %s", ldevID)
	} else {
		t.Errorf("A new logical device should not have been created : %+v", added)
	}

}

func Test_Proxy_2_2_1_Get_AllLogicalDevices(t *testing.T) {
	logicalDevices := ldevProxy.Get("/logical_devices", 1, false, "")

	if len(logicalDevices.([]interface{})) == 0 {
		t.Error("there are no available logical devices to retrieve")
	} else {
		// Save the target device id for later tests
		targetLogDevID = logicalDevices.([]interface{})[0].(*voltha.LogicalDevice).Id
		t.Logf("retrieved all logical devices: %+v", logicalDevices)
	}
}

func Test_Proxy_2_2_2_Get_SingleLogicalDevice(t *testing.T) {
	if ld := ldevProxy.Get("/logical_devices/"+targetLogDevID, 0, false, ""); !reflect.ValueOf(ld).IsValid() {
		t.Errorf("Failed to find logical device : %s", targetLogDevID)
	} else {
		ldJSON, _ := json.Marshal(ld)
		t.Logf("Found logical device: %s", string(ldJSON))
	}

}

func Test_Proxy_2_3_1_Update_LogicalDevice(t *testing.T) {
	var fwVersion int
	preUpdateExecuted := false
	postUpdateExecuted := false

	if retrieved := ldevProxy.Get("/logical_devices/"+targetLogDevID, 1, false, ""); retrieved == nil {
		t.Error("Failed to get logical device")
	} else {
		t.Logf("Found raw logical device (root proxy): %+v", retrieved)

		if retrieved.(*voltha.LogicalDevice).RootDeviceId == "" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.LogicalDevice).RootDeviceId)
			fwVersion++
		}

		ldevProxy.RegisterCallback(
			PRE_UPDATE,
			commonCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecuted,
		)
		ldevProxy.RegisterCallback(
			POST_UPDATE,
			commonCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecuted,
		)

		retrieved.(*voltha.LogicalDevice).RootDeviceId = strconv.Itoa(fwVersion)

		if afterUpdate := ldevProxy.Update("/logical_devices/"+targetLogDevID, retrieved, false,
			""); afterUpdate == nil {
			t.Error("Failed to update logical device")
		} else {
			t.Logf("Updated logical device : %+v", afterUpdate)
		}
		if d := ldevProxy.Get("/logical_devices/"+targetLogDevID, 1, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated logical device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)

			t.Logf("Found logical device (root proxy): %s raw: %+v", string(djson), d)
		}

		if !preUpdateExecuted {
			t.Error("PRE_UPDATE callback was not executed")
		}
		if !postUpdateExecuted {
			t.Error("POST_UPDATE callback was not executed")
		}
	}
}

func Test_Proxy_2_3_2_Update_LogicalDeviceFlows(t *testing.T) {
	// Get a device proxy and update a specific port
	ldFlowsProxy := modelTestConfig.Root.node.CreateProxy("/logical_devices/"+ldevID+"/flows", false)
	flows := ldFlowsProxy.Get("/", 0, false, "")
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

	kvFlows := ldFlowsProxy.Get("/", 0, false, "")

	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	if updated := ldFlowsProxy.Update("/", flows.(*openflow_13.Flows), false, ""); updated == nil {
		t.Error("Failed to update logical device flows")
	} else {
		t.Logf("Updated logical device flows : %+v", updated)
	}

	if d := ldFlowsProxy.Get("/", 0, false, ""); d == nil {
		t.Error("Failed to find updated logical device flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	if d := modelTestConfig.RootProxy.Get("/logical_devices/"+ldevID+"/flows", 0, false,
		""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find updated logical device flows (root proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found logical device flows (root proxy): %s", string(djson))
	}
}

func Test_Proxy_2_4_1_Remove_Device(t *testing.T) {
	preRemoveExecuted := false
	postRemoveExecuted := false

	ldevProxy.RegisterCallback(
		PRE_REMOVE,
		commonCallback,
		"PRE_REMOVE instructions (root proxy)", &preRemoveExecuted,
	)
	ldevProxy.RegisterCallback(
		POST_REMOVE,
		commonCallback,
		"POST_REMOVE instructions (root proxy)", &postRemoveExecuted,
	)

	if removed := ldevProxy.Remove("/logical_devices/"+ldevID, ""); removed == nil {
		t.Error("Failed to remove logical device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	if d := ldevProxy.Get("/logical_devices/"+ldevID, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", ldevID)
	}

	if !preRemoveExecuted {
		t.Error("PRE_UPDATE callback was not executed")
	}
	if !postRemoveExecuted {
		t.Error("POST_UPDATE callback was not executed")
	}
}

// -----------------------------
// Callback tests
// -----------------------------

func Test_Proxy_Callbacks_1_Register(t *testing.T) {
	modelTestConfig.RootProxy.RegisterCallback(PRE_ADD, firstCallback, "abcde", "12345")

	m := make(map[string]string)
	m["name"] = "fghij"
	modelTestConfig.RootProxy.RegisterCallback(PRE_ADD, secondCallback, m, 1.2345)

	d := &voltha.Device{Id: "12345"}
	modelTestConfig.RootProxy.RegisterCallback(PRE_ADD, thirdCallback, "klmno", d)
}

func Test_Proxy_Callbacks_2_Invoke_WithNoInterruption(t *testing.T) {
	modelTestConfig.RootProxy.InvokeCallbacks(PRE_ADD, false, nil)
}

func Test_Proxy_Callbacks_3_Invoke_WithInterruption(t *testing.T) {
	modelTestConfig.RootProxy.InvokeCallbacks(PRE_ADD, true, nil)
}

func Test_Proxy_Callbacks_4_Unregister(t *testing.T) {
	modelTestConfig.RootProxy.UnregisterCallback(PRE_ADD, firstCallback)
	modelTestConfig.RootProxy.UnregisterCallback(PRE_ADD, secondCallback)
	modelTestConfig.RootProxy.UnregisterCallback(PRE_ADD, thirdCallback)
}

//func Test_Proxy_Callbacks_5_Add(t *testing.T) {
//	modelTestConfig.RootProxy.Root.AddCallback(modelTestConfig.RootProxy.InvokeCallbacks, POST_UPDATE, false, "some data", "some new data")
//}
//
//func Test_Proxy_Callbacks_6_Execute(t *testing.T) {
//	modelTestConfig.RootProxy.Root.ExecuteCallbacks()
//}
