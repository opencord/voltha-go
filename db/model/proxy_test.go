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
	"github.com/opencord/voltha-go/protos/voltha"
	"testing"
	"github.com/opencord/voltha-go/common/log"
	"strconv"
	"reflect"
	"github.com/google/uuid"
	"encoding/hex"
	"encoding/json"
)

type proxyTest struct {
	Root      *Root
	Backend   *Backend
	Proxy     *Proxy
	DbPrefix  string
	DbType    string
	DbHost    string
	DbPort    int
	DbTimeout int
}

var (
	pt = &proxyTest{
		DbPrefix: "service/voltha/data/core/0001",
		DbType:   "etcd",
		//DbHost:    "10.102.58.0",
		DbHost:    "localhost",
		DbPort:    2379,
		DbTimeout: 5,
	}
	devId string
	targetDeviceId string
)

func init() {
	if _, err := log.SetLogger(log.CONSOLE, 0, log.Fields{"instanceId": "proxy_test"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("cannot setup logging")
	}
	defer log.CleanUp()

}
func Test_Proxy_0_GetRootProxy(t *testing.T) {
	pt.Backend = NewBackend(pt.DbType, pt.DbHost, pt.DbPort, pt.DbTimeout, pt.DbPrefix)

	msgClass := &voltha.Voltha{}
	root := NewRoot(msgClass, pt.Backend, nil)
	pt.Root = root.Load(msgClass)

	GetProfiling().Report()

	pt.Proxy = pt.Root.Node.GetProxy("/", false)
}

func Test_Proxy_1_GetDevices(t *testing.T) {
	devices := pt.Proxy.Get("/devices", 1, false, "")

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		targetDeviceId = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved devices: %+v", devices)
	}
}

func Test_Proxy_2_GetDevice(t *testing.T) {
	basePath := "/devices/" + targetDeviceId
	device1 := pt.Proxy.Get(basePath + "/ports", 1, false, "")
	t.Logf("retrieved device with ports: %+v", device1)

	device2 := pt.Proxy.Get(basePath, 0, false, "")

	t.Logf("retrieved device: %+v", device2)
}

//func Test_Proxy_3_AddDevice(t *testing.T) {
//	//ports := []*voltha.Port{
//	//	{
//	//		PortNo:     123,
//	//		Label:      "test-port-0",
//	//		Type:       voltha.Port_PON_OLT,
//	//		AdminState: common.AdminState_ENABLED,
//	//		OperStatus: common.OperStatus_ACTIVE,
//	//		DeviceId:   "etcd_port-0-device-id",
//	//		Peers:      []*voltha.Port_PeerPort{},
//	//	},
//	//}
//	devIdBin, _ := uuid.New().MarshalBinary()
//	devId := hex.EncodeToString(devIdBin)[:12]
//
//	device := &voltha.Device{
//		Id:                  devId,
//		Type:                "simulated_olt",
//		//Root:                true,
//		//ParentId:            "",
//		//ParentPortNo:        0,
//		//Vendor:              "voltha-test",
//		//Model:               "latest-voltha-simulated-olt",
//		//HardwareVersion:     "1.0.0",
//		//FirmwareVersion:     "1.0.0",
//		//Images:              &voltha.Images{},
//		//SerialNumber:        "abcdef-123456",
//		//VendorId:            "DEADBEEF-INC",
//		//Adapter:             "simulated_olt",
//		//Vlan:                1234,
//		Address:             &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
//		//ExtraArgs:           "",
//		//ProxyAddress:        &voltha.Device_ProxyAddress{},
//		AdminState:          voltha.AdminState_PREPROVISIONED,
//		//OperStatus:          common.OperStatus_ACTIVE,
//		//Reason:              "",
//		//ConnectStatus:       common.ConnectStatus_REACHABLE,
//		//Custom:              &any.Any{},
//		//Ports:               ports,
//		//Flows:               &openflow_13.Flows{},
//		//FlowGroups:          &openflow_13.FlowGroups{},
//		//PmConfigs:           &voltha.PmConfigs{},
//		//ImageDownloads:      []*voltha.ImageDownload{},
//	}
//
//	//if retrieved := pt.Proxy.Get("/devices/00019b09a90bbe17", 0, false, ""); retrieved == nil {
//	//	t.Error("Failed to get device")
//	//} else {
//	//	devIdBin, _ := uuid.New().MarshalBinary()
//	//	devId = "0001" + hex.EncodeToString(devIdBin)[:12]
//	//	newDevice := Clone(de\).(*voltha.Device)
//	//	newDevice.Id = devId
//
//		if added := pt.Proxy.Add("/devices", device, ""); added == nil {
//			t.Error("Failed to add device")
//		} else {
//			t.Logf("Added device : %+v", added)
//		}
//	//}
//
//}
func Test_Proxy_3_AddDevice(t *testing.T) {
	devIdBin, _ := uuid.New().MarshalBinary()
	devId = "0001" + hex.EncodeToString(devIdBin)[:12]

	device := &voltha.Device{
		Id:                  devId,
		Type:                "simulated_olt",
		Address:             &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState:          voltha.AdminState_PREPROVISIONED,
	}

	if added := pt.Proxy.Add("/devices", device, ""); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}
}

func Test_Proxy_4_CheckAddedDevice(t *testing.T) {
	if d := pt.Proxy.Get("/devices/" + devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)

		t.Logf("Found device: count: %s", djson)
	}
}

func Test_Proxy_5_UpdateDevice(t *testing.T) {
	if retrieved := pt.Proxy.Get("/devices/" + targetDeviceId, 1, false, ""); retrieved == nil {
		t.Error("Failed to get device")
	} else {
		var fwVersion int
		if retrieved.(*voltha.Device).FirmwareVersion == "n/a" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.Device).FirmwareVersion)
			fwVersion += 1
		}

		cloned := reflect.ValueOf(retrieved).Elem().Interface().(voltha.Device)
		cloned.FirmwareVersion = strconv.Itoa(fwVersion)
		t.Logf("Before update : %+v", cloned)

		if afterUpdate := pt.Proxy.Update("/devices/" + targetDeviceId, &cloned, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate.(Revision).GetData())
		}
	}
}

func Test_Proxy_6_CheckUpdatedDevice(t *testing.T) {
	device := pt.Proxy.Get("/devices/" + targetDeviceId, 0, false, "")

	t.Logf("content of updated device: %+v", device)
}

func Test_Proxy_7_RemoveDevice(t *testing.T) {
	if removed := pt.Proxy.Remove("/devices/" + devId, ""); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
}

func Test_Proxy_8_CheckRemovedDevice(t *testing.T) {
	if d := pt.Proxy.Get("/devices/" + devId, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", devId)
	}
}
