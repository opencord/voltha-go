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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/openflow_13"
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
	ports = []*voltha.Port{
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

	stats = &openflow_13.OfpFlowStats{
		Id: 1111,
	}
	flows = &openflow_13.Flows{
		Items: []*openflow_13.OfpFlowStats{stats},
	}
	device = &voltha.Device{
		Id:         devId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Flows:      flows,
		Ports:      ports,
	}
	devId          string
	targetDeviceId string

	preAddExecuted      = false
	postAddExecuted     = false
	preUpdateExecuted   = false
	postUpdateExecuted  = false
	preRemoveExecuted   = false
	postRemoveExecuted = false
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

func commonCallback(args ...interface{}) interface{} {
	log.Infof("Running common callback - arg count: %s", len(args))

	for i := 0; i < len(args); i++ {
		log.Infof("ARG %d : %+v", i, args[i])
	}
	execStatus := args[1].(*bool)

	// Inform the caller that the callback was executed
	*execStatus = true

	return nil
}

func Test_Proxy_1_1_Add_NewDevice(t *testing.T) {
	devIdBin, _ := uuid.New().MarshalBinary()
	devId = "0001" + hex.EncodeToString(devIdBin)[:12]

	pt.Proxy.RegisterCallback(PRE_ADD, commonCallback, "PRE_ADD instructions", &preAddExecuted)
	pt.Proxy.RegisterCallback(POST_ADD, commonCallback, "POST_ADD instructions", &postAddExecuted)

	device.Id = devId
	if added := pt.Proxy.Add("/devices", device, ""); added == nil {
		t.Error("Failed to add device")
	} else {
		t.Logf("Added device : %+v", added)
	}

	if d := pt.Proxy.Get("/devices/"+devId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
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

func Test_Proxy_1_2_Add_ExistingDevice(t *testing.T) {
	device.Id = devId
	if added := pt.Proxy.Add("/devices", device, ""); added == nil {
		t.Logf("Successfully detected that the device already exists: %s", devId)
	} else {
		t.Errorf("A new device should not have been created : %+v", added)
	}

}

func Test_Proxy_2_1_Get_AllDevices(t *testing.T) {
	devices := pt.Proxy.Get("/devices", 1, false, "")

	if len(devices.([]interface{})) == 0 {
		t.Error("there are no available devices to retrieve")
	} else {
		// Save the target device id for later tests
		targetDeviceId = devices.([]interface{})[0].(*voltha.Device).Id
		t.Logf("retrieved all devices: %+v", devices)
	}
}

func Test_Proxy_2_2_Get_SingleDevice(t *testing.T) {
	if d := pt.Proxy.Get("/devices/"+targetDeviceId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
		t.Errorf("Failed to find device : %s", targetDeviceId)
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found device: %s", string(djson))
	}

}

func Test_Proxy_3_1_Update_Device_WithRootProxy(t *testing.T) {
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

		preUpdateExecuted = false
		postUpdateExecuted = false

		pt.Proxy.RegisterCallback(
			PRE_UPDATE,
			commonCallback,
			"PRE_UPDATE instructions (root proxy)", &preUpdateExecuted,
		)
		pt.Proxy.RegisterCallback(
			POST_UPDATE,
			commonCallback,
			"POST_UPDATE instructions (root proxy)", &postUpdateExecuted,
		)

		//cloned := reflect.ValueOf(retrieved).Elem().Interface().(voltha.Device)
		//cloned.FirmwareVersion = strconv.Itoa(fwVersion)
		retrieved.(*voltha.Device).FirmwareVersion = strconv.Itoa(fwVersion)
		//t.Logf("Before update : %+v", cloned)

		if afterUpdate := pt.Proxy.Update("/devices/"+targetDeviceId, retrieved, false, ""); afterUpdate == nil {
			t.Error("Failed to update device")
		} else {
			t.Logf("Updated device : %+v", afterUpdate.(Revision).GetData())
		}
		if d := pt.Proxy.Get("/devices/"+targetDeviceId, 0, false, ""); !reflect.ValueOf(d).IsValid() {
			t.Error("Failed to find updated device (root proxy)")
		} else {
			djson, _ := json.Marshal(d)
			t.Logf("Found device (root proxy): %s", string(djson))
		}

		if !preUpdateExecuted {
			t.Error("PRE_UPDATE callback was not executed")
		}
		if !postUpdateExecuted {
			t.Error("POST_UPDATE callback was not executed")
		}
	}
}

func Test_Proxy_3_2_Update_Flow_WithSubProxy(t *testing.T) {
	// Get a device proxy and update a specific port
	devflowsProxy := pt.Root.GetProxy("/devices/"+devId+"/flows", false)
	flows := devflowsProxy.Get("/", 0, false, "")
	//flows.([]interface{})[0].(*openflow_13.Flows).Items[0].TableId = 2222
	flows.([]interface{})[0].(*openflow_13.Flows).Items[0].TableId = 2244
	//flows.(*openflow_13.Flows).Items[0].TableId = 2244
	t.Logf("before updated flows: %+v", flows)

	//devPortsProxy := pt.Root.node.GetProxy("/devices/"+devId+"/ports", false)
	//port123 := devPortsProxy.Get("/123", 0, false, "")
	//t.Logf("got ports: %+v", port123)
	//port123.(*voltha.Port).OperStatus = common.OperStatus_DISCOVERED

	preUpdateExecuted = false
	postUpdateExecuted = false

	devflowsProxy.RegisterCallback(
		PRE_UPDATE,
		commonCallback,
		"PRE_UPDATE instructions (flows proxy)", &preUpdateExecuted,
	)
	devflowsProxy.RegisterCallback(
		POST_UPDATE,
		commonCallback,
		"POST_UPDATE instructions (flows proxy)", &postUpdateExecuted,
	)

	kvFlows := devflowsProxy.Get("/", 0, false, "")

	if reflect.DeepEqual(flows, kvFlows) {
		t.Errorf("Local changes have changed the KV store contents -  local:%+v, kv: %+v", flows, kvFlows)
	}

	if updated := devflowsProxy.Update("/", flows.([]interface{})[0], false, ""); updated == nil {
		t.Error("Failed to update flow")
	} else {
		t.Logf("Updated flows : %+v", updated)
	}

	if d := devflowsProxy.Get("/", 0, false, ""); d == nil {
		t.Error("Failed to find updated flows (flows proxy)")
	} else {
		djson, _ := json.Marshal(d)
		t.Logf("Found flows (flows proxy): %s", string(djson))
	}

	if d := pt.Proxy.Get("/devices/"+devId+"/flows", 0, false, ""); !reflect.ValueOf(d).IsValid() {
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
	//
	// Get a device proxy and update all its ports
	//

	//devProxy := pt.Root.GetProxy("/devices/"+devId, false)
	//ports := devProxy.Get("/ports", 0, false, "")
	//t.Logf("got ports: %+v", ports)
	//devProxy.RegisterCallback(POST_UPDATE, commonCallback, nil)
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
	//devProxy.RegisterCallback(POST_UPDATE, commonCallback, nil)
	//
	//ports.([]interface{})[0].(*voltha.Port).OperStatus = common.OperStatus_DISCOVERED
	//
	//devProxy.Update("/ports/123", ports.([]interface{})[0], false, "")
	//updated := devProxy.Get("/ports", 0, false, "")
	//t.Logf("got updated ports: %+v", updated)
}

func Test_Proxy_4_1_Remove_Device(t *testing.T) {
	preRemoveExecuted = false
	postRemoveExecuted = false

	pt.Proxy.RegisterCallback(
		PRE_REMOVE,
		commonCallback,
		"PRE_REMOVE instructions (root proxy)", &preRemoveExecuted,
	)
	pt.Proxy.RegisterCallback(
		POST_REMOVE,
		commonCallback,
		"POST_REMOVE instructions (root proxy)", &postRemoveExecuted,
	)

	if removed := pt.Proxy.Remove("/devices/"+devId, ""); removed == nil {
		t.Error("Failed to remove device")
	} else {
		t.Logf("Removed device : %+v", removed)
	}
	if d := pt.Proxy.Get("/devices/"+devId, 0, false, ""); reflect.ValueOf(d).IsValid() {
		djson, _ := json.Marshal(d)
		t.Errorf("Device was not removed - %s", djson)
	} else {
		t.Logf("Device was removed: %s", devId)
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

func firstCallback(args ...interface{}) interface{} {
	name := args[0]
	id := args[1]
	log.Infof("Running first callback - name: %s, id: %s\n", name, id)
	return nil
}
func secondCallback(args ...interface{}) interface{} {
	name := args[0].(map[string]string)
	id := args[1]
	log.Infof("Running second callback - name: %s, id: %f\n", name["name"], id)
	// FIXME: the panic call seem to interfere with the logging mechanism
	//panic("Generating a panic in second callback")
	return nil
}
func thirdCallback(args ...interface{}) interface{} {
	name := args[0]
	id := args[1].(*voltha.Device)
	log.Infof("Running third callback - name: %+v, id: %s\n", name, id.Id)
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
	pt.Proxy.InvokeCallbacks(PRE_ADD, false, nil)
}

func Test_Proxy_Callbacks_3_Invoke_WithInterruption(t *testing.T) {
	pt.Proxy.InvokeCallbacks(PRE_ADD, true, nil)
}

func Test_Proxy_Callbacks_4_Unregister(t *testing.T) {
	pt.Proxy.UnregisterCallback(PRE_ADD, firstCallback)
	pt.Proxy.UnregisterCallback(PRE_ADD, secondCallback)
	pt.Proxy.UnregisterCallback(PRE_ADD, thirdCallback)
}

//func Test_Proxy_Callbacks_5_Add(t *testing.T) {
//	pt.Proxy.Root.AddCallback(pt.Proxy.InvokeCallbacks, POST_UPDATE, false, "some data", "some new data")
//}
//
//func Test_Proxy_Callbacks_6_Execute(t *testing.T) {
//	pt.Proxy.Root.ExecuteCallbacks()
//}
