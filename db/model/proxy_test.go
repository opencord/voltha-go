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
	"fmt"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/voltha"
	"reflect"
	"strconv"
	"testing"
)

type proxyTest struct {
	Root      *root
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
	devId          string
	targetDeviceId string
)

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
	log.UpdateAllLoggers(log.Fields{"instanceId": "proxy_test"})

	defer log.CleanUp()

	pt.Backend = NewBackend(pt.DbType, pt.DbHost, pt.DbPort, pt.DbTimeout, pt.DbPrefix)

	msgClass := &voltha.Voltha{}
	root := NewRoot(msgClass, pt.Backend)
	pt.Root = root.Load(msgClass)

	GetProfiling().Report()

	pt.Proxy = pt.Root.GetProxy("/", false)
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

func Test_Proxy_2_AddDevice(t *testing.T) {
	devIdBin, _ := uuid.New().MarshalBinary()
	devId = "0001" + hex.EncodeToString(devIdBin)[:12]

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
		Id:         devId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Ports:      ports,
	}

	if added := pt.Proxy.Add("/devices", device, ""); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}
}

func Test_Proxy_3_GetDevice_PostAdd(t *testing.T) {
	if d := pt.Proxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find added device")
	} else {
		djson, _ := json.Marshal(d)

		t.Logf("Found device: count: %s", djson)
	}
}
func Test_Proxy_3_1_RegisterProxy(t *testing.T) {
	// Get a device proxy and update a specific port
	devProxy := pt.Root.GetProxy("/devices/"+devId, false)
	port123 := devProxy.Get("/ports/123", 0, false, "")
	t.Logf("got ports: %+v", port123)

	devProxy.RegisterCallback(POST_UPDATE, deviceCallback, nil)

	port123.(*voltha.Port).OperStatus = common.OperStatus_DISCOVERED

	devProxy.Update("/ports/123", port123, false, "")
	updated := devProxy.Get("/ports", 0, false, "")
	t.Logf("got updated ports: %+v", updated)

	//
	// Get a device proxy and update all its ports
	//

	//devProxy := pt.Root.GetProxy("/devices/"+devId, false)
	//ports := devProxy.Get("/ports", 0, false, "")
	//t.Logf("got ports: %+v", ports)
	//devProxy.RegisterCallback(POST_UPDATE, deviceCallback, nil)
	//
	//ports.([]interface{})[0].(*voltha.Port).OperStatus = common.OperStatus_DISCOVERED
	//
	//devProxy.Update("/ports", ports, false, "")
	//updated := devProxy.Get("/ports", 0, false, "")
	//t.Logf("got updated ports: %+v", updated)


	//
	// Get a device proxy, retrieve all the ports and update a specific one
	//

	//devProxy := pt.Root.GetProxy("/devices/"+devId, false)
	//ports := devProxy.Get("/ports", 0, false, "")
	//t.Logf("got ports: %+v", ports)
	//devProxy.RegisterCallback(POST_UPDATE, deviceCallback, nil)
	//
	//ports.([]interface{})[0].(*voltha.Port).OperStatus = common.OperStatus_DISCOVERED
	//
	//devProxy.Update("/ports/123", ports.([]interface{})[0], false, "")
	//updated := devProxy.Get("/ports", 0, false, "")
	//t.Logf("got updated ports: %+v", updated)
}

func Test_Proxy_3_2_GetDevice_PostRegister(t *testing.T) {
	if d := pt.Proxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Error("Failed to find updated registered device")
	} else {
		djson, _ := json.Marshal(d)

		t.Logf("Found device: count: %s", djson)
	}
}

func Test_Proxy_4_UpdateDevice(t *testing.T) {
	if retrieved := pt.Proxy.Get("/devices/"+targetDeviceId, 1, false, ""); retrieved == nil {
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

		if afterUpdate := pt.Proxy.Update("/devices/"+targetDeviceId, &cloned, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate.(Revision).GetData())
		}
	}
}

func Test_Proxy_5_GetDevice_PostUpdate(t *testing.T) {
	device := pt.Proxy.Get("/devices/"+targetDeviceId, 0, false, "")

	t.Logf("content of updated device: %+v", device)
}

func Test_Proxy_6_RemoveDevice(t *testing.T) {
	if removed := pt.Proxy.Remove("/devices/"+devId, ""); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
}

func Test_Proxy_7_GetDevice_PostRemove(t *testing.T) {
	if d := pt.Proxy.Get("/devices/"+devId, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", devId)
	}
}

// -----------------------------
// Callback tests
// -----------------------------

func firstCallback(args ...interface{}) interface{} {
	name := args[0]
	id := args[1]
	fmt.Printf("Running first callback - name: %s, id: %s\n", name, id)
	return nil
}
func deviceCallback(args ...interface{}) interface{} {
	fmt.Printf("Running device callback\n")
	return nil
}
func secondCallback(args ...interface{}) interface{} {
	name := args[0].(map[string]string)
	id := args[1]
	fmt.Printf("Running second callback - name: %s, id: %f\n", name["name"], id)
	// FIXME: the panic call seem to interfere with the logging mechanism
	//panic("Generating a panic in second callback")
	return nil
}
func thirdCallback(args ...interface{}) interface{} {
	name := args[0]
	id := args[1].(*voltha.Device)
	fmt.Printf("Running third callback - name: %+v, id: %s\n", name, id.Id)
	return nil
}

func Test_Proxy_Callbacks_1_Register(t *testing.T) {
	pt.Proxy.RegisterCallback(PRE_ADD, firstCallback, "abcde", "12345")

	m := make(map[string]string)
	m["name"] = "fghij"
	pt.Proxy.RegisterCallback(PRE_ADD, secondCallback, m, 1.2345)

	d := &voltha.Device{Id: "12345"}
	pt.Proxy.RegisterCallback(PRE_ADD, thirdCallback, "klmno", d)
}

func Test_Proxy_Callbacks_2_Invoke_WithNoInterruption(t *testing.T) {
	pt.Proxy.InvokeCallbacks(PRE_ADD, nil, true)
}
func Test_Proxy_Callbacks_3_Invoke_WithInterruption(t *testing.T) {
	pt.Proxy.InvokeCallbacks(PRE_ADD, nil, false)
}

func Test_Proxy_Callbacks_4_Unregister(t *testing.T) {
	pt.Proxy.UnregisterCallback(PRE_ADD, firstCallback)
	pt.Proxy.UnregisterCallback(PRE_ADD, secondCallback)
	pt.Proxy.UnregisterCallback(PRE_ADD, thirdCallback)
}
