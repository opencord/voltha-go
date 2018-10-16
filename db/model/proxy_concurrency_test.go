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
	"github.com/opencord/voltha-go/protos/voltha"
	"reflect"
	"strconv"
	"testing"
)

/*

1. Add device
2. Do parallel updates of that device
3. Do parallel gets of that device
4. Remove device

 */

var (
	//pctTargetDeviceId string
	target *voltha.Device
)

func Test_ConcurrentProxy_1_1_Add_NewDevice(t *testing.T) {
	devIdBin, _ := uuid.New().MarshalBinary()
	devId = "0001" + hex.EncodeToString(devIdBin)[:12]

	preAddExecuted := false
	postAddExecuted := false

	modelTestConfig.RootProxy.RegisterCallback(PRE_ADD, commonCallback, "PRE_ADD instructions", &preAddExecuted)
	modelTestConfig.RootProxy.RegisterCallback(POST_ADD, commonCallback, "POST_ADD instructions", &postAddExecuted)

	device.Id = devId
	if added := modelTestConfig.RootProxy.Add("/devices", device, ""); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}

	if d := modelTestConfig.RootProxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

	//if !preAddExecuted {
	//	t.Error("PRE_ADD callback was not executed")
	//}
	//if !postAddExecuted {
	//	t.Error("POST_ADD callback was not executed")
	//}
}

func Test_ConcurrentProxy_1_Add_ExistingDevice(t *testing.T) {
	t.Parallel()

	device.Id = devId
	if added := modelTestConfig.RootProxy.Add("/devices", device, ""); added == nil {
		t.Logf("Successfully detected that the device already exists: %s", devId)
	} else {
		t.Errorf("A new device should not have been created : %+v", added)
	}

}

func Test_ConcurrentProxy_Get_AllDevices(t *testing.T) {
	t.Parallel()

	devices := modelTestConfig.RootProxy.Get("/devices", 1, false, "")

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		t.Logf("retrieved all devices: %+v", devices)
	}
}

func Test_ConcurrentProxy_Get_Update_DeviceAdminState(t *testing.T) {
	t.Parallel()
	if retrieved := modelTestConfig.RootProxy.Get("/devices/"+devId, 1, false, ""); retrieved == nil {
		t.Error("Failed to get device")
	} else {
		retrieved.(*voltha.Device).AdminState = voltha.AdminState_DISABLED

		preUpdateExecuted := false
		postUpdateExecuted := false

		modelTestConfig.RootProxy.RegisterCallback(
			PRE_UPDATE,
			commonCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecuted,
		)
		modelTestConfig.RootProxy.RegisterCallback(
			POST_UPDATE,
			commonCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecuted,
		)

		if afterUpdate := modelTestConfig.RootProxy.Update("/devices/"+devId, retrieved, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate.(Revision).GetData())
		}
		if d := modelTestConfig.RootProxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s", string(djson))
		}

		//if !preUpdateExecuted {
		//	t.Error("PRE_UPDATE callback was not executed")
		//}
		//if !postUpdateExecuted {
		//	t.Error("POST_UPDATE callback was not executed")
		//}
	}
}

func Test_ConcurrentProxy_Get_SingleDevice(t *testing.T) {
	//t.Parallel()

	if d := modelTestConfig.RootProxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", devId)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

}

func Test_ConcurrentProxy_Get_SingleDeviceFlows(t *testing.T) {
	t.Parallel()

	if d := modelTestConfig.RootProxy.Get("/devices/"+devId+"/flows", 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", devId)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

}

func Test_ConcurrentProxy_Get_SingleDevicePorts(t *testing.T) {
	t.Parallel()
	if d := modelTestConfig.RootProxy.Get("/devices/"+devId+"/ports", 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", devId)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

}

func Test_ConcurrentProxy_Get_Update_DeviceFirmware(t *testing.T) {
	t.Parallel()

	if retrieved := modelTestConfig.RootProxy.Get("/devices/"+devId, 1, false, ""); retrieved == nil {
		t.Error("Failed to get device")
	} else {
		var fwVersion int
		if retrieved.(*voltha.Device).FirmwareVersion == "n/a" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.Device).FirmwareVersion)
			fwVersion += 1
		}

		preUpdateExecuted := false
		postUpdateExecuted := false

		modelTestConfig.RootProxy.RegisterCallback(
			PRE_UPDATE,
			commonCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecuted,
		)
		modelTestConfig.RootProxy.RegisterCallback(
			POST_UPDATE,
			commonCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecuted,
		)

		retrieved.(*voltha.Device).FirmwareVersion = strconv.Itoa(fwVersion)

		if afterUpdate := modelTestConfig.RootProxy.Update("/devices/"+devId, retrieved, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate.(Revision).GetData())
		}
		if d := modelTestConfig.RootProxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s", string(djson))
		}

		//if !preUpdateExecuted {
		//	t.Error("PRE_UPDATE callback was not executed")
		//}
		//if !postUpdateExecuted {
		//	t.Error("POST_UPDATE callback was not executed")
		//}
	}
}

//func Test_ConcurrentProxy_Get_Update_DeviceFlows(t *testing.T) {
//	t.Parallel()
//
//	// Get a device proxy and update a specific port
//	//devflowsProxy := modelTestConfig.Root.GetProxy("/devices/"+devId+"/flows", false)
//	flows := modelTestConfig.RootProxy.Get("/", 0, false, "")
//	flows.([]interface{})[0].(*openflow_13.Flows).Items[0].TableId = 2244
//
//	preUpdateExecuted := false
//	postUpdateExecuted := false
//
//	modelTestConfig.RootProxy.RegisterCallback(
//		PRE_UPDATE,
//		commonCallback,
//		"PRE_UPDATE instructions (flows proxy)", &preUpdateExecuted,
//	)
//	modelTestConfig.RootProxy.RegisterCallback(
//		POST_UPDATE,
//		commonCallback,
//		"POST_UPDATE instructions (flows proxy)", &postUpdateExecuted,
//	)
//
//	kvFlows := modelTestConfig.RootProxy.Get("/devices/"+devId+"/flows", 0, false, "")
//
//	if reflect.DeepEqual(flows, kvFlows) {
//		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
//	}
//
//	if updated := modelTestConfig.RootProxy.Update("/devices/"+devId+"/flows", flows.([]interface{})[0], false, ""); updated == nil {
//		t.Error("Failed to update flow")
//	} else {
//		t.Logf("Updated flows : %+v", updated)
//	}
//
//	if d := modelTestConfig.RootProxy.Get("/devices/"+devId+"/flows", 0, false, ""); d == nil {
//		t.Error("Failed to find updated flows (flows proxy)")
//	} else {
//		djson, _ := json.Marshal(d)
//		t.Logf("Found flows (flows proxy): %s", string(djson))
//	}
//
//	if d := modelTestConfig.RootProxy.Get("/devices/"+devId+"/flows", 0, false, ""); !reflect.ValueOf(d).IsValid() {
//		t.Error("Failed to find updated flows (root proxy)")
//	} else {
//		djson, _ := json.Marshal(d)
//		t.Logf("Found flows (root proxy): %s", string(djson))
//	}
//
//	//if !preUpdateExecuted {
//	//	t.Error("PRE_UPDATE callback was not executed")
//	//}
//	//if !postUpdateExecuted {
//	//	t.Error("POST_UPDATE callback was not executed")
//	//}
//}

//func Test_ConcurrentProxy_4_1_Remove_Device(t *testing.T) {
//	preRemoveExecuted := false
//	postRemoveExecuted := false
//
//	modelTestConfig.RootProxy.RegisterCallback(
//		PRE_REMOVE,
//		commonCallback,
//		"PRE_REMOVE instructions (root proxy)", &preRemoveExecuted,
//	)
//	modelTestConfig.RootProxy.RegisterCallback(
//		POST_REMOVE,
//		commonCallback,
//		"POST_REMOVE instructions (root proxy)", &postRemoveExecuted,
//	)
//
//	if removed := modelTestConfig.RootProxy.Remove("/devices/"+devId, ""); removed == nil {
//		t.Error("Failed to remove device")
//	} else {
//		t.Logf("Removed device : %+v", removed)
//	}
//	if d := modelTestConfig.RootProxy.Get("/devices/"+devId, 0, false, ""); reflect.ValueOf(d).IsValid() {
//		djson, _ := json.Marshal(d)
//		t.Errorf("Device was not removed - %s", djson)
//	} else {
//		t.Logf("Device was removed: %s", devId)
//	}
//
//	if !preRemoveExecuted {
//		t.Error("PRE_UPDATE callback was not executed")
//	}
//	if !postRemoveExecuted {
//		t.Error("POST_UPDATE callback was not executed")
//	}
//}

//func Benchmark_ConcurrentProxy_UpdateFirmware(b *testing.B) {
//	//var target *voltha.Device
//	if target == nil {
//		devices := modelTestConfig.RootProxy.Get("/devices", 1, false, "")
//		if len(devices.([]interface{})) == 0 {
//			b.Error("there are no available devices to retrieve")
//		} else {
//			// Save the target device id for later tests
//			target = devices.([]interface{})[0].(*voltha.Device)
//			//b.Logf("retrieved all devices: %+v", devices)
//		}
//	}
//
//	for n := 0; n < b.N; n++ {
//		var fwVersion int
//
//		if target.FirmwareVersion == "n/a" {
//			fwVersion = 0
//		} else {
//			fwVersion, _ = strconv.Atoi(target.FirmwareVersion)
//			fwVersion += 1
//		}
//
//		target.FirmwareVersion = strconv.Itoa(fwVersion)
//
//		if afterUpdate := modelTestConfig.RootProxy.Update("/devices/"+target.Id, target, false,
//			""); afterUpdate == nil {
//			b.Error("Failed to update device")
//		} else {
//			if d := modelTestConfig.RootProxy.Get("/devices/"+target.Id, 0, false, ""); !reflect.ValueOf(d).IsValid() {
//				b.Errorf("Failed to find device : %s", devId)
//			} else {
//				//djson, _ := json.Marshal(d)
//				//b.Logf("Checking updated device device: %s", string(djson))
//			}
//		}
//	}
//
//}
//
//func Benchmark_ConcurrentProxy_GetDevice(b *testing.B) {
//	if target == nil {
//		//var target *voltha.Device
//		devices := modelTestConfig.RootProxy.Get("/devices", 1, false, "")
//		if len(devices.([]interface{})) == 0 {
//			b.Error("there are no available devices to retrieve")
//		} else {
//			// Save the target device id for later tests
//			target = devices.([]interface{})[0].(*voltha.Device)
//			//b.Logf("retrieved all devices: %+v", devices)
//		}
//	}
//
//	for n := 0; n < b.N; n++ {
//		if d := modelTestConfig.RootProxy.Get("/devices/"+target.Id, 0, false, ""); !reflect.ValueOf(d).IsValid() {
//			b.Errorf("Failed to find device : %s", devId)
//		} else {
//			//djson, _ := json.Marshal(d)
//			//b.Logf("Found device: %s", string(djson))
//		}
//	}
//
//}

func Benchmark_ConcurrentProxy_UpdateFirmware(b *testing.B) {
	//GetProfiling().Reset()
	defer GetProfiling().Report()
	//b.SetParallelism(100)
	if target == nil {
		devices := modelTestConfig.RootProxy.Get("/devices", 1, false, "")
		if len(devices.([]interface{})) == 0 {
			b.Error("there are no available devices to retrieve")
		} else {
			// Save the target device id for later tests
			target = devices.([]interface{})[0].(*voltha.Device)
			//b.Logf("retrieved all devices: %+v", devices)
		}
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var fwVersion int

			if target.FirmwareVersion == "n/a" {
				fwVersion = 0
			} else {
				fwVersion, _ = strconv.Atoi(target.FirmwareVersion)
				fwVersion += 1
			}

			target.FirmwareVersion = strconv.Itoa(fwVersion)

			if afterUpdate := modelTestConfig.RootProxy.Update("/devices/"+target.Id, target, false,
				""); afterUpdate == nil {
				b.Error("Failed to update device")
			} else {
				if d := modelTestConfig.RootProxy.Get("/devices/"+target.Id, 0, false, ""); !reflect.ValueOf(d).IsValid() {
					b.Errorf("Failed to find device : %s", devId)
				} else if d.(*voltha.Device).FirmwareVersion != target.FirmwareVersion {
					b.Errorf("Firmware was not uptaded - expected: %s, actual: %s",
						target.FirmwareVersion,
						d.(*voltha.Device).FirmwareVersion,
					)
				} else {
					b.Logf("Firmware is now : %s", d.(*voltha.Device).FirmwareVersion)
					//djson, _ := json.Marshal(d)
					//b.Logf("Checking updated device device: %s", string(djson))
				}
			}
		}
	})
}

func Benchmark_ConcurrentProxy_GetDevice(b *testing.B) {
	//GetProfiling().Reset()
	defer GetProfiling().Report()
	//b.SetParallelism(5)
	if target == nil {
		//var target *voltha.Device
		devices := modelTestConfig.RootProxy.Get("/devices", 1, false, "")
		if len(devices.([]interface{})) == 0 {
			b.Error("there are no available devices to retrieve")
		} else {
			// Save the target device id for later tests
			target = devices.([]interface{})[0].(*voltha.Device)
			//b.Logf("retrieved all devices: %+v", devices)
		}
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if d := modelTestConfig.RootProxy.Get("/devices/"+target.Id, 0, false, ""); !reflect.ValueOf(d).IsValid() {
				b.Errorf("Failed to find device : %s", devId)
			} else {
				//djson, _ := json.Marshal(d)
				//b.Logf("Found device: %s", string(djson))
			}
		}
	})
}
