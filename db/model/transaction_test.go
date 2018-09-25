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
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/voltha"
	"reflect"
	"strconv"
	"testing"
)

type transactionTest struct {
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
	tx = &transactionTest{
		DbPrefix: "service/voltha/data/core/0001",
		DbType:   "etcd",
		//DbHost:    "10.102.58.0",
		DbHost:    "localhost",
		DbPort:    2379,
		DbTimeout: 5,
	}
	txTargetDevId string
	txDevId       string
)

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
	log.UpdateAllLoggers(log.Fields{"instanceId": "transaction_test"})

	defer log.CleanUp()

	tx.Backend = NewBackend(tx.DbType, tx.DbHost, tx.DbPort, tx.DbTimeout, tx.DbPrefix)

	msgClass := &voltha.Voltha{}
	root := NewRoot(msgClass, tx.Backend)

	if tx.Backend != nil {
		tx.Root = root.Load(msgClass)
	} else {
		tx.Root = root
	}

	GetProfiling().Report()

	tx.Proxy = tx.Root.Node.GetProxy("/", false)
}

func Test_Transaction_1_GetDevices(t *testing.T) {
	getTx := tx.Proxy.OpenTransaction()

	devices := getTx.Get("/devices", 1, false)

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		txTargetDevId = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved devices: %+v", devices)
	}

	getTx.Commit()
}

func Test_Transaction_2_AddDevice(t *testing.T) {
	devIdBin, _ := uuid.New().MarshalBinary()
	txDevId = "0001" + hex.EncodeToString(devIdBin)[:12]

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
		Id:         txDevId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Ports:      ports,
	}

	addTx := tx.Proxy.OpenTransaction()

	if added := addTx.Add("/devices", device); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}
	addTx.Commit()
}

func Test_Transaction_3_GetDevice_PostAdd(t *testing.T) {

	basePath := "/devices/" + txDevId

	getDevWithPortsTx := tx.Proxy.OpenTransaction()
	device1 := getDevWithPortsTx.Get(basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := tx.Proxy.OpenTransaction()
	device2 := getDevTx.Get(basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}

func Test_Transaction_4_UpdateDevice(t *testing.T) {
	updateTx := tx.Proxy.OpenTransaction()
	if retrieved := updateTx.Get("/devices/"+txTargetDevId, 1, false); retrieved == nil {
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

		// FIXME: The makeBranch passed in function is nil or not being executed properly!!!!!
		if afterUpdate := updateTx.Update("/devices/"+txTargetDevId, &cloned, false); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate.(Revision).GetData())
		}
	}
	updateTx.Commit()
}

func Test_Transaction_5_GetDevice_PostUpdate(t *testing.T) {

	basePath := "/devices/" + txDevId

	getDevWithPortsTx := tx.Proxy.OpenTransaction()
	device1 := getDevWithPortsTx.Get(basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	getDevWithPortsTx.Commit()

	getDevTx := tx.Proxy.OpenTransaction()
	device2 := getDevTx.Get(basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)

	getDevTx.Commit()
}


func Test_Transaction_6_RemoveDevice(t *testing.T) {
	removeTx := tx.Proxy.OpenTransaction()
	if removed := removeTx.Remove("/devices/" + txDevId); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	removeTx.Commit()
}

func Test_Transaction_7_GetDevice_PostRemove(t *testing.T) {

	basePath := "/devices/" + txDevId

	getDevTx := tx.Proxy.OpenTransaction()
	device := tx.Proxy.Get(basePath, 0, false, "")
	t.Logf("retrieved device: %+v", device)

	getDevTx.Commit()
}
