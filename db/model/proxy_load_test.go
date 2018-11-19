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
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

var (
	ltDevProxy *Proxy
	plt        *proxyLoadTest
	tlog       log.Logger
)

type proxyLoadChanges struct {
	ID     string
	Before interface{}
	After  interface{}
}
type proxyLoadTest struct {
	sync.RWMutex
	addedDevices       [] string
	updatedFirmwares   []proxyLoadChanges
	updatedFlows       []proxyLoadChanges
	preAddExecuted     bool
	postAddExecuted    bool
	preUpdateExecuted  bool
	postUpdateExecuted bool
}

func (plt *proxyLoadTest) SetPreAddExecuted(status bool) {
	plt.Lock()
	defer plt.Unlock()
	plt.preAddExecuted = status
}
func (plt *proxyLoadTest) SetPostAddExecuted(status bool) {
	plt.Lock()
	defer plt.Unlock()
	plt.postAddExecuted = status
}
func (plt *proxyLoadTest) SetPreUpdateExecuted(status bool) {
	plt.Lock()
	defer plt.Unlock()
	plt.preUpdateExecuted = status
}
func (plt *proxyLoadTest) SetPostUpdateExecuted(status bool) {
	plt.Lock()
	defer plt.Unlock()
	plt.postUpdateExecuted = status
}
func init() {
	tlog, _ = log.AddPackage(log.JSON, log.DebugLevel, nil)
	log.UpdateAllLoggers(log.Fields{"instanceId": "PROXY_LOAD_TEST"})
	defer log.CleanUp()

	ltDevProxy = modelTestConfig.Root.node.CreateProxy("/", false)
	// Register ADD instructions callbacks
	plt = &proxyLoadTest{}

	ltDevProxy.RegisterCallback(PRE_ADD, commonCallbackFunc, "PRE_ADD", plt.SetPreAddExecuted)
	ltDevProxy.RegisterCallback(POST_ADD, commonCallbackFunc, "POST_ADD", plt.SetPostAddExecuted)

	//// Register UPDATE instructions callbacks
	ltDevProxy.RegisterCallback(PRE_UPDATE, commonCallbackFunc, "PRE_UPDATE", plt.SetPreUpdateExecuted)
	ltDevProxy.RegisterCallback(POST_UPDATE, commonCallbackFunc, "POST_UPDATE", plt.SetPostUpdateExecuted)

}

func Benchmark_ProxyLoad_AddDevice(b *testing.B) {
	defer GetProfiling().Report()
	b.RunParallel(func(pb *testing.PB) {
		b.Log("Started adding devices")
		for pb.Next() {
			ltPorts := []*voltha.Port{
				{
					PortNo:     123,
					Label:      "lt-port-0",
					Type:       voltha.Port_PON_OLT,
					AdminState: common.AdminState_ENABLED,
					OperStatus: common.OperStatus_ACTIVE,
					DeviceId:   "lt-port-0-device-id",
					Peers:      []*voltha.Port_PeerPort{},
				},
			}

			ltStats := &openflow_13.OfpFlowStats{
				Id: 1000,
			}
			ltFlows := &openflow_13.Flows{
				Items: []*openflow_13.OfpFlowStats{ltStats},
			}
			ltDevice := &voltha.Device{
				Id:         "",
				Type:       "simulated_olt",
				Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
				AdminState: voltha.AdminState_PREPROVISIONED,
				Flows:      ltFlows,
				Ports:      ltPorts,
			}

			ltDevIDBin, _ := uuid.New().MarshalBinary()
			ltDevID := "0001" + hex.EncodeToString(ltDevIDBin)[:12]
			ltDevice.Id = ltDevID

			plt.SetPreAddExecuted(false)
			plt.SetPostAddExecuted(false)

			var added interface{}
			// Add the device
			if added = ltDevProxy.AddWithID("/devices", ltDevID, ltDevice, ""); added == nil {
				tlog.Errorf("Failed to add device: %+v", ltDevice)
				continue
			} else {
				tlog.Infof("Device was added 1: %+v", added)
			}

			plt.Lock()
			plt.addedDevices = append(plt.addedDevices, added.(*voltha.Device).Id)
			plt.Unlock()
		}
	})

	tlog.Infof("Number of added devices : %d", len(plt.addedDevices))
}

func Benchmark_ProxyLoad_UpdateFirmware(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			//for i:=0; i < b.N; i++ {

			if len(plt.addedDevices) > 0 {
				var target interface{}
				randomID := plt.addedDevices[rand.Intn(len(plt.addedDevices))]
				firmProxy := modelTestConfig.Root.node.CreateProxy("/", false)
				if target = firmProxy.Get("/devices/"+randomID, 0, false,
					""); !reflect.ValueOf(target).IsValid() {
					tlog.Errorf("Failed to find device: %s %+v", randomID, target)
					continue
				}

				plt.SetPreUpdateExecuted(false)
				plt.SetPostUpdateExecuted(false)
				firmProxy.RegisterCallback(PRE_UPDATE, commonCallbackFunc, "PRE_UPDATE", plt.SetPreUpdateExecuted)
				firmProxy.RegisterCallback(POST_UPDATE, commonCallbackFunc, "POST_UPDATE", plt.SetPostUpdateExecuted)

				var fwVersion int

				before := target.(*voltha.Device).FirmwareVersion
				if target.(*voltha.Device).FirmwareVersion == "n/a" {
					fwVersion = 0
				} else {
					fwVersion, _ = strconv.Atoi(target.(*voltha.Device).FirmwareVersion)
					fwVersion++
				}

				target.(*voltha.Device).FirmwareVersion = strconv.Itoa(fwVersion)
				after := target.(*voltha.Device).FirmwareVersion

				var updated interface{}
				if updated = firmProxy.Update("/devices/"+randomID, target.(*voltha.Device), false,
					""); updated == nil {
					tlog.Errorf("Failed to update device: %+v", target)
					continue
				} else {
					tlog.Infof("Device was updated : %+v", updated)

				}

				if d := firmProxy.Get("/devices/"+randomID, 0, false,
					""); !reflect.ValueOf(d).IsValid() {
					tlog.Errorf("Failed to get device: %s", randomID)
					continue
				} else if d.(*voltha.Device).FirmwareVersion == after {
					tlog.Infof("Imm Device was updated with new value: %s %+v", randomID, d)
				} else if d.(*voltha.Device).FirmwareVersion == before {
					tlog.Errorf("Imm Device kept old value: %s %+v %+v", randomID, d, target)
				} else {
					tlog.Errorf("Imm Device has unknown value: %s %+v %+v", randomID, d, target)
				}

				plt.Lock()
				plt.updatedFirmwares = append(
					plt.updatedFirmwares,
					proxyLoadChanges{ID: randomID, Before: before, After: after},
				)
				plt.Unlock()
			}
		}
	})
}

func traverseBranches(revision Revision, depth int) {
	if revision == nil {
		return
	}
	prefix := strconv.Itoa(depth) + " ~~~~ "
	for i := 0; i < depth; i++ {
		prefix += "  "
	}

	tlog.Debugf("%sRevision: %s %+v", prefix, revision.GetHash(), revision.GetData())

	//for brIdx, brRev := range revision.GetBranch().Revisions {
	//	tlog.Debugf("%sbranchIndex: %s", prefix, brIdx)
	//	traverseBranches(brRev, depth+1)
	//}
	for childrenI, children := range revision.GetChildren() {
		tlog.Debugf("%schildrenIndex: %s, length: %d", prefix, childrenI, len(children))

		for _, subrev := range children {
			//subrev.GetBranch().Latest
			traverseBranches(subrev, depth+1)
		}
	}

}
func Benchmark_ProxyLoad_UpdateFlows(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if len(plt.addedDevices) > 0 {
				randomID := plt.addedDevices[rand.Intn(len(plt.addedDevices))]

				flowsProxy := modelTestConfig.Root.node.CreateProxy("/devices/"+randomID+"/flows", false)
				flows := flowsProxy.Get("/", 0, false, "")

				before := flows.(*openflow_13.Flows).Items[0].TableId
				flows.(*openflow_13.Flows).Items[0].TableId = uint32(rand.Intn(3000))
				after := flows.(*openflow_13.Flows).Items[0].TableId

				flowsProxy.RegisterCallback(
					PRE_UPDATE,
					commonCallback2,
				)
				flowsProxy.RegisterCallback(
					POST_UPDATE,
					commonCallback2,
				)

				var updated interface{}
				if updated = flowsProxy.Update("/", flows.(*openflow_13.Flows), false, ""); updated == nil {
					b.Errorf("Failed to update flows for device: %+v", flows)
				} else {
					tlog.Infof("Flows were updated : %+v", updated)
				}
				plt.Lock()
				plt.updatedFlows = append(
					plt.updatedFlows,
					proxyLoadChanges{ID: randomID, Before: before, After: after},
				)
				plt.Unlock()
			}
		}
	})
}

func Benchmark_ProxyLoad_GetDevices(b *testing.B) {
	traverseBranches(ltDevProxy.Root.node.Branches[NONE].GetLatest(), 0)

	for i := 0; i < len(plt.addedDevices); i++ {
		devToGet := plt.addedDevices[i]
		// Verify that the added device can now be retrieved
		if d := ltDevProxy.Get("/devices/"+devToGet, 0, false,
			""); !reflect.ValueOf(d).IsValid() {
			tlog.Errorf("Failed to get device: %s", devToGet)
			continue
		} else {
			tlog.Infof("Got device: %s %+v", devToGet, d)
		}
	}
}

func Benchmark_ProxyLoad_GetUpdatedFirmware(b *testing.B) {
	for i := 0; i < len(plt.updatedFirmwares); i++ {
		devToGet := plt.updatedFirmwares[i].ID
		// Verify that the updated device can be retrieved and that the updates were actually applied
		if d := ltDevProxy.Get("/devices/"+devToGet, 0, false,
			""); !reflect.ValueOf(d).IsValid() {
			tlog.Errorf("Failed to get device: %s", devToGet)
			continue
		} else if d.(*voltha.Device).FirmwareVersion == plt.updatedFirmwares[i].After.(string) {
			tlog.Infof("Device was updated with new value: %s %+v", devToGet, d)
		} else if d.(*voltha.Device).FirmwareVersion == plt.updatedFirmwares[i].Before.(string) {
			tlog.Errorf("Device kept old value: %s %+v %+v", devToGet, d, plt.updatedFirmwares[i])
		} else {
			tlog.Errorf("Device has unknown value: %s %+v %+v", devToGet, d, plt.updatedFirmwares[i])
		}
	}
}

func Benchmark_ProxyLoad_GetUpdatedFlows(b *testing.B) {
	var d interface{}
	for i := 0; i < len(plt.updatedFlows); i++ {
		devToGet := plt.updatedFlows[i].ID
		// Verify that the updated device can be retrieved and that the updates were actually applied
		flowsProxy := modelTestConfig.Root.node.CreateProxy("/devices/"+devToGet+"/flows", false)
		if d = flowsProxy.Get("/", 0, false,
			""); !reflect.ValueOf(d).IsValid() {
			tlog.Errorf("Failed to get device flows: %s", devToGet)
			continue
		} else if d.(*openflow_13.Flows).Items[0].TableId == plt.updatedFlows[i].After.(uint32) {
			tlog.Infof("Device was updated with new flow value: %s %+v", devToGet, d)
		} else if d.(*openflow_13.Flows).Items[0].TableId == plt.updatedFlows[i].Before.(uint32) {
			tlog.Errorf("Device kept old flow value: %s %+v %+v", devToGet, d, plt.updatedFlows[i])
		} else {
			tlog.Errorf("Device has unknown flow value: %s %+v %+v", devToGet, d, plt.updatedFlows[i])
		}
		//if d = ltDevProxy.Get("/devices/"+devToGet, 0, false,
		//	""); !reflect.ValueOf(d).IsValid() {
		//	tlog.Errorf("Failed to get device: %s", devToGet)
		//	continue
		//} else if d.(*voltha.Device).Flows.Items[0].TableId == plt.updatedFlows[i].After.(uint32) {
		//	tlog.Infof("Device was updated with new flow value: %s %+v", devToGet, d)
		//} else if d.(*voltha.Device).Flows.Items[0].TableId == plt.updatedFlows[i].Before.(uint32) {
		//	tlog.Errorf("Device kept old flow value: %s %+v %+v", devToGet, d, plt.updatedFlows[i])
		//} else {
		//	tlog.Errorf("Device has unknown flow value: %s %+v %+v", devToGet, d, plt.updatedFlows[i])
		//}
	}
}
