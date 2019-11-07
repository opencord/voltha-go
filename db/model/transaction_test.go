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
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/stretchr/testify/assert"
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
	var err error
	TestTransaction_Root = NewRoot(&voltha.Voltha{}, nil)
	if TestTransaction_RootProxy, err = TestTransaction_Root.node.CreateProxy(context.Background(), "/", false); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create proxy")
	}
}

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

	added, err := addTx.Add(context.Background(), "/devices", device)
	if err != nil {
		log.Errorf("Failed to add device due to error %v", err)
		assert.NotNil(t, err)
	}
	if added == nil {
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
	device1, err := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	if err != nil {
		log.Errorf("Failed to get device with ports due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := TestTransaction_RootProxy.OpenTransaction()
	device2, err := getDevTx.Get(context.Background(), basePath, 0, false)
	if err != nil {
		log.Errorf("Failed to open transaction due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func TestTransaction_4_UpdateDevice(t *testing.T) {
	updateTx := TestTransaction_RootProxy.OpenTransaction()
	if retrieved, err := updateTx.Get(context.Background(), "/devices/"+TestTransaction_TargetDeviceId, 1, false); err != nil {
		log.Errorf("Failed to retrieve device info due to error %v", err)
		assert.NotNil(t, err)
	} else if retrieved == nil {
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
		afterUpdate, err := updateTx.Update(context.Background(), "/devices/"+TestTransaction_TargetDeviceId, retrieved, false)
		if err != nil {
			log.Errorf("Failed to update device info due to error %v", err)
			assert.NotNil(t, err)
		}
		if afterUpdate == nil {
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
	device1, err := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	if err != nil {
		log.Errorf("Failed to device with ports info due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := TestTransaction_RootProxy.OpenTransaction()
	device2, err := getDevTx.Get(context.Background(), basePath, 0, false)
	if err != nil {
		log.Errorf("Failed to  get device info due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func TestTransaction_6_RemoveDevice(t *testing.T) {
	removeTx := TestTransaction_RootProxy.OpenTransaction()
	removed, err := removeTx.Remove(context.Background(), "/devices/"+TestTransaction_DeviceId)
	if err != nil {
		log.Errorf("Failed to remove device due to error %v", err)
		assert.NotNil(t, err)
	}
	if removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	removeTx.Commit()
}

func TestTransaction_7_GetDevice_PostRemove(t *testing.T) {

	basePath := "/devices/" + TestTransaction_DeviceId

	getDevTx := TestTransaction_RootProxy.OpenTransaction()
	device, err := TestTransaction_RootProxy.Get(context.Background(), basePath, 0, false, "")
	if err != nil {
		log.Errorf("Failed to get device info post remove due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device: %+v", device)

	getDevTx.Commit()
}
