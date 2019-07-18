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
	"github.com/google/uuid"
	"github.com/opencord/voltha-protos/go/common"
	"github.com/opencord/voltha-protos/go/voltha"
	"strconv"
	"testing"
)

var (
	TestTransaction_Root           *root
	TestTransaction_RootProxy      *Proxy
	TestTransaction_TargetDeviceId string
	TestTransaction_DeviceId       string
)

func init() {
	TestTransaction_Root = NewRoot(&voltha.Voltha{}, nil)
	TestTransaction_RootProxy = TestTransaction_Root.node.CreateProxy(context.Background(), "/", false)
}

//func TestTransaction_1_GetDevices(t *testing.T) {
//	getTx := TestTransaction_RootProxy.OpenTransaction()
//
//	devices := getTx.Get("/devices", 1, false)
//
//	if len(devices.([]interface{})) == 0 {
//		t.Error("there are no available devices to retrieve")
//	} else {
//		// Save the target device id for later tests
//		TestTransaction_TargetDeviceId = devices.([]interface{})[0].(*voltha.Device).Id
//		t.Logf("retrieved devices: %+v", devices)
//	}
//
//	getTx.Commit()
//}

func TestTransaction_2_AddDevice(t *testing.T) {
	devIDBin, _ := uuid.New().MarshalBinary()
	TestTransaction_DeviceId = "0001" + hex.EncodeToString(devIDBin)[:12]

	ports := []*voltha.Port{
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

	device := &voltha.Device{
		Id:         TestTransaction_DeviceId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Ports:      ports,
	}

	addTx := TestTransaction_RootProxy.OpenTransaction()

	if added := addTx.Add(context.Background(), "/devices", device); added == nil {
		t.Error("Failed to add device")
	} else {
		TestTransaction_TargetDeviceId = added.(*voltha.Device).Id
		t.Logf("Added device : %+v", added)
	}
	addTx.Commit()
}

func TestTransaction_3_GetDevice_PostAdd(t *testing.T) {

	basePath := "/devices/" + TestTransaction_DeviceId

	getDevWithPortsTx := TestTransaction_RootProxy.OpenTransaction()
	device1 := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := TestTransaction_RootProxy.OpenTransaction()
	device2 := getDevTx.Get(context.Background(), basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func TestTransaction_4_UpdateDevice(t *testing.T) {
	updateTx := TestTransaction_RootProxy.OpenTransaction()
	if retrieved := updateTx.Get(context.Background(), "/devices/"+TestTransaction_TargetDeviceId, 1, false); retrieved == nil {
		t.Error("Failed to get device")
	} else {
		var fwVersion int
		if retrieved.(*voltha.Device).FirmwareVersion == "n/a" {
			fwVersion = 0
		} else {
			fwVersion, _ = strconv.Atoi(retrieved.(*voltha.Device).FirmwareVersion)
			fwVersion++
		}

		//cloned := reflect.ValueOf(retrieved).Elem().Interface().(voltha.Device)
		retrieved.(*voltha.Device).FirmwareVersion = strconv.Itoa(fwVersion)
		t.Logf("Before update : %+v", retrieved)

		// FIXME: The makeBranch passed in function is nil or not being executed properly!!!!!
		if afterUpdate := updateTx.Update(context.Background(), "/devices/"+TestTransaction_TargetDeviceId, retrieved, false); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}
	}
	updateTx.Commit()
}

func TestTransaction_5_GetDevice_PostUpdate(t *testing.T) {

	basePath := "/devices/" + TestTransaction_DeviceId

	getDevWithPortsTx := TestTransaction_RootProxy.OpenTransaction()
	device1 := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := TestTransaction_RootProxy.OpenTransaction()
	device2 := getDevTx.Get(context.Background(), basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func TestTransaction_6_RemoveDevice(t *testing.T) {
	removeTx := TestTransaction_RootProxy.OpenTransaction()
	if removed := removeTx.Remove(context.Background(), "/devices/"+TestTransaction_DeviceId); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	removeTx.Commit()
}

func TestTransaction_7_GetDevice_PostRemove(t *testing.T) {

	basePath := "/devices/" + TestTransaction_DeviceId

	getDevTx := TestTransaction_RootProxy.OpenTransaction()
	device := TestTransaction_RootProxy.Get(context.Background(), basePath, 0, false, "")
	t.Logf("retrieved device: %+v", device)

	getDevTx.Commit()
}
