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
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
)

var (
	TestTransactionRoot           Root
	TestTransactionRootProxy      *Proxy
	TestTransactionTargetDeviceID string
	TestTransactionDeviceID       string
)

func init() {
	var err error
	TestTransactionRoot = NewRoot(&voltha.Voltha{}, nil)
	if TestTransactionRootProxy, err = TestTransactionRoot.CreateProxy(context.Background(), "/", false); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create proxy")
	}
}

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

	added, err := addTx.Add(context.Background(), "/devices", device)
	if err != nil {
		log.Errorf("Failed to add device due to error %v", err)
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add device")
	} else {
		TestTransactionTargetDeviceID = added.(*voltha.Device).Id
		t.Logf("Added device : %+v", added)
	}
	ctx, cancel := context.WithTimeout(context.Background(), kvstore.GetDuration(1))
	defer cancel()
	addTx.Commit(ctx)
}

func TestTransaction_3_GetDevice_PostAdd(t *testing.T) {

	basePath := "/devices/" + TestTransactionDeviceID

	getDevWithPortsTx := TestTransactionRootProxy.OpenTransaction()
	device1, err := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	if err != nil {
		log.Errorf("Failed to get device with ports due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device with ports: %+v", device1)
	ctx, cancel := context.WithTimeout(context.Background(), kvstore.GetDuration(1))
	defer cancel()
	getDevWithPortsTx.Commit(ctx)

	getDevTx := TestTransactionRootProxy.OpenTransaction()
	device2, err := getDevTx.Get(context.Background(), basePath, 0, false)
	if err != nil {
		log.Errorf("Failed to open transaction due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit(ctx)
}

func TestTransaction_4_UpdateDevice(t *testing.T) {
	updateTx := TestTransactionRootProxy.OpenTransaction()
	if retrieved, err := updateTx.Get(context.Background(), "/devices/"+TestTransactionTargetDeviceID, 1, false); err != nil {
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
		afterUpdate, err := updateTx.Update(context.Background(), "/devices/"+TestTransactionTargetDeviceID, retrieved, false)
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
	ctx, cancel := context.WithTimeout(context.Background(), kvstore.GetDuration(1))
	defer cancel()
	updateTx.Commit(ctx)
}

func TestTransaction_5_GetDevice_PostUpdate(t *testing.T) {

	basePath := "/devices/" + TestTransactionDeviceID

	getDevWithPortsTx := TestTransactionRootProxy.OpenTransaction()
	device1, err := getDevWithPortsTx.Get(context.Background(), basePath+"/ports", 1, false)
	if err != nil {
		log.Errorf("Failed to device with ports info due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device with ports: %+v", device1)
	ctx, cancel := context.WithTimeout(context.Background(), kvstore.GetDuration(1))
	defer cancel()
	getDevWithPortsTx.Commit(ctx)

	getDevTx := TestTransactionRootProxy.OpenTransaction()
	device2, err := getDevTx.Get(context.Background(), basePath, 0, false)
	if err != nil {
		log.Errorf("Failed to  get device info due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit(ctx)
}

func TestTransaction_6_RemoveDevice(t *testing.T) {
	removeTx := TestTransactionRootProxy.OpenTransaction()
	removed, err := removeTx.Remove(context.Background(), "/devices/"+TestTransactionDeviceID)
	if err != nil {
		log.Errorf("Failed to remove device due to error %v", err)
		assert.NotNil(t, err)
	}
	if removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), kvstore.GetDuration(1))
	defer cancel()
	removeTx.Commit(ctx)
}

func TestTransaction_7_GetDevice_PostRemove(t *testing.T) {

	basePath := "/devices/" + TestTransactionDeviceID

	getDevTx := TestTransactionRootProxy.OpenTransaction()
	device, err := TestTransactionRootProxy.Get(context.Background(), basePath, 0, false, "")
	if err != nil {
		log.Errorf("Failed to get device info post remove due to error %v", err)
		assert.NotNil(t, err)
	}
	t.Logf("retrieved device: %+v", device)

	ctx, cancel := context.WithTimeout(context.Background(), kvstore.GetDuration(1))
	defer cancel()
	getDevTx.Commit(ctx)
}
