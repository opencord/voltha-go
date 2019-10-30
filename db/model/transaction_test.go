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
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

var (
	TestTransactionRoot           *root
	TestTransactionRootProxy      *Proxy
	TestTransactionTargetDeviceID string
	TestTransactionDeviceID       string
)

func init() {
	TestTransactionRoot = NewRoot(&voltha.Voltha{}, nil)
	TestTransactionRootProxy = TestTransactionRoot.node.CreateProxy(context.Background(), "/", false)
}

//func TestTransaction_1_GetDevices(t *testing.T) {
//	getTx := TestTransactionRootProxy.OpenTransaction()
//
//	devices := getTx.Get("/devices", 1, false)
//
//	if len(devices.([]interface{})) == 0 {
//		t.Error("there are no available devices to retrieve")
//	} else {
//		// Save the target device id for later tests
//		TestTransactionTargetDeviceID = devices.([]interface{})[0].(*voltha.Device).Id
//		t.Logf("retrieved devices: %+v", devices)
//	}
//
//	getTx.Commit()
//}

func TestTransaction_2_AddDevice(t *testing.T) {
	devIDBin, _ := uuid.New().MarshalBinary()
	TestTransactionDeviceID = "0001" + hex.EncodeToString(devIDBin)[:12]

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
		Id:         TestTransactionDeviceID,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Ports:      ports,
	}

	addTx := TestTransactionRootProxy.OpenTransaction()

	if added := addTx.Add(context.Background(), "/devices", device); added == nil {
		t.Error("Failed to add device")
	} else {
		TestTransactionTargetDeviceID = added.(*voltha.Device).Id
		t.Logf("Added device : %+v", added)
	}
	addTx.Commit()
}

func TestTransaction_3_GetDevice_PostAdd(t *testing.T) {

	basePath := "/devices/" + TestTransactionDeviceID

	getDevWithPortsTx := TestTransactionRootProxy.OpenTransaction()
	device1 := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := TestTransactionRootProxy.OpenTransaction()
	device2 := getDevTx.Get(context.Background(), basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func TestTransaction_4_UpdateDevice(t *testing.T) {
	updateTx := TestTransactionRootProxy.OpenTransaction()
	if retrieved := updateTx.Get(context.Background(), "/devices/"+TestTransactionTargetDeviceID, 1, false); retrieved == nil {
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
		if afterUpdate := updateTx.Update(context.Background(), "/devices/"+TestTransactionTargetDeviceID, retrieved, false); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate)
		}
	}
	updateTx.Commit()
}

func TestTransaction_5_GetDevice_PostUpdate(t *testing.T) {

	basePath := "/devices/" + TestTransactionDeviceID

	getDevWithPortsTx := TestTransactionRootProxy.OpenTransaction()
	device1 := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := TestTransactionRootProxy.OpenTransaction()
	device2 := getDevTx.Get(context.Background(), basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func TestTransaction_6_RemoveDevice(t *testing.T) {
	removeTx := TestTransactionRootProxy.OpenTransaction()
	if removed := removeTx.Remove(context.Background(), "/devices/"+TestTransactionDeviceID); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	removeTx.Commit()
}

func TestTransaction_7_GetDevice_PostRemove(t *testing.T) {

	basePath := "/devices/" + TestTransactionDeviceID

	getDevTx := TestTransactionRootProxy.OpenTransaction()
	device := TestTransactionRootProxy.Get(context.Background(), basePath, 0, false, "")
	t.Logf("retrieved device: %+v", device)

	getDevTx.Commit()
}
