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
	"github.com/opencord/voltha-go/common/log"
	"testing"
	"github.com/google/uuid"
	"encoding/hex"
	"strconv"
	"reflect"
)

type transactionTest struct {
	Root        *Root
	Backend     *Backend
	Proxy       *Proxy
	DbPrefix    string
	DbType      string
	DbHost      string
	DbPort      int
	DbTimeout   int
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
	txDevId string
)

func init() {
	if _, err := log.SetLogger(log.CONSOLE, 0, log.Fields{"instanceId": "transaction_test"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("cannot setup logging")
	}
	defer log.CleanUp()

	tx.Backend = NewBackend(tx.DbType, tx.DbHost, tx.DbPort, tx.DbTimeout, tx.DbPrefix)

	msgClass := &voltha.Voltha{}
	root := NewRoot(msgClass, tx.Backend, nil)
	tx.Root = root.Load(msgClass)

	GetProfiling().Report()

	tx.Proxy = tx.Root.Node.GetProxy("/", false)

}

func Test_Transaction_1_GetDevices(t *testing.T) {
	getTx := tx.Proxy.openTransaction()

	devices := getTx.Get("/devices", 1, false)

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		txTargetDevId = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved devices: %+v", devices)
	}

	tx.Proxy.commitTransaction(getTx.txid)
}

func Test_Transaction_2_GetDevice(t *testing.T) {

	basePath := "/devices/" + txTargetDevId

	getDevWithPortsTx := tx.Proxy.openTransaction()
	device1 := getDevWithPortsTx.Get(basePath+"/ports", 1, false)
	t.Logf("retrieved device with ports: %+v", device1)
	tx.Proxy.commitTransaction(getDevWithPortsTx.txid)

	getDevTx := tx.Proxy.openTransaction()
	device2 := getDevTx.Get(basePath, 0, false)
	t.Logf("retrieved device: %+v", device2)
	tx.Proxy.commitTransaction(getDevTx.txid)
}

func Test_Transaction_3_AddDevice(t *testing.T) {
	devIdBin, _ := uuid.New().MarshalBinary()
	txDevId = "0001" + hex.EncodeToString(devIdBin)[:12]

	device := &voltha.Device{
		Id:         txDevId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
	}

	addTx := tx.Proxy.openTransaction()

	if added := addTx.Add("/devices", device); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}
	tx.Proxy.commitTransaction(addTx.txid)
}

func Test_Transaction_4_UpdateDevice(t *testing.T) {
	updateTx := tx.Proxy.openTransaction()
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
	tx.Proxy.commitTransaction(updateTx.txid)
}

func Test_Transaction_5_RemoveDevice(t *testing.T) {
	removeTx := tx.Proxy.openTransaction()
	if removed := removeTx.Remove("/devices/"+txDevId); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	tx.Proxy.commitTransaction(removeTx.txid)
}
