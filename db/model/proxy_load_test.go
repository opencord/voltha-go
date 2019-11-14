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
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

var (
	BenchmarkProxyRoot        *root
	BenchmarkProxyDeviceProxy *Proxy
	BenchmarkProxyPLT         *proxyLoadTest
	BenchmarkProxyLogger      log.Logger
)

type proxyLoadChanges struct {
	ID     string
	Before interface{}
	After  interface{}
}
type proxyLoadTest struct {
	mutex sync.RWMutex

	addMutex     sync.RWMutex
	addedDevices []string

	firmwareMutex    sync.RWMutex
	updatedFirmwares []proxyLoadChanges
	flowMutex        sync.RWMutex
	updatedFlows     []proxyLoadChanges

	preAddExecuted     bool
	postAddExecuted    bool
	preUpdateExecuted  bool
	postUpdateExecuted bool
}

func (plt *proxyLoadTest) SetPreAddExecuted(status bool) {
	plt.mutex.Lock()
	defer plt.mutex.Unlock()
	plt.preAddExecuted = status
}
func (plt *proxyLoadTest) SetPostAddExecuted(status bool) {
	plt.mutex.Lock()
	defer plt.mutex.Unlock()
	plt.postAddExecuted = status
}
func (plt *proxyLoadTest) SetPreUpdateExecuted(status bool) {
	plt.mutex.Lock()
	defer plt.mutex.Unlock()
	plt.preUpdateExecuted = status
}
func (plt *proxyLoadTest) SetPostUpdateExecuted(status bool) {
	plt.mutex.Lock()
	defer plt.mutex.Unlock()
	plt.postUpdateExecuted = status
}

func init() {
	BenchmarkProxyRoot = NewRoot(&voltha.Voltha{}, nil)

	BenchmarkProxyLogger, _ = log.AddPackage(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"})
	//log.UpdateAllLoggers(log.Fields{"instanceId": "PROXY_LOAD_TEST"})
	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers (provisioned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": "PLT"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	log.SetPackageLogLevel("github.com/opencord/voltha-go/db/model", log.DebugLevel)

	BenchmarkProxyDeviceProxy = BenchmarkProxyRoot.node.CreateProxy(context.Background(), "/", false)
	// Register ADD instructions callbacks
	BenchmarkProxyPLT = &proxyLoadTest{}

	BenchmarkProxyDeviceProxy.RegisterCallback(PreAdd, commonCallbackFunc, "PreAdd", BenchmarkProxyPLT.SetPreAddExecuted)
	BenchmarkProxyDeviceProxy.RegisterCallback(PostAdd, commonCallbackFunc, "PostAdd", BenchmarkProxyPLT.SetPostAddExecuted)

	//// Register UPDATE instructions callbacks
	BenchmarkProxyDeviceProxy.RegisterCallback(PreUpdate, commonCallbackFunc, "PreUpdate", BenchmarkProxyPLT.SetPreUpdateExecuted)
	BenchmarkProxyDeviceProxy.RegisterCallback(PostUpdate, commonCallbackFunc, "PostUpdate", BenchmarkProxyPLT.SetPostUpdateExecuted)

}

func BenchmarkProxy_AddDevice(b *testing.B) {
	defer getProfiling().Report()
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

			BenchmarkProxyPLT.SetPreAddExecuted(false)
			BenchmarkProxyPLT.SetPostAddExecuted(false)

			var added interface{}
			// Add the device
			if added = BenchmarkProxyDeviceProxy.AddWithID(context.Background(), "/devices", ltDevID, ltDevice, ""); added == nil {
				BenchmarkProxyLogger.Errorf("Failed to add device: %+v", ltDevice)
				continue
			} else {
				BenchmarkProxyLogger.Infof("Device was added 1: %+v", added)
			}

			BenchmarkProxyPLT.addMutex.Lock()
			BenchmarkProxyPLT.addedDevices = append(BenchmarkProxyPLT.addedDevices, added.(*voltha.Device).Id)
			BenchmarkProxyPLT.addMutex.Unlock()
		}
	})

	BenchmarkProxyLogger.Infof("Number of added devices : %d", len(BenchmarkProxyPLT.addedDevices))
}

func BenchmarkProxy_UpdateFirmware(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			//for i:=0; i < b.N; i++ {

			if len(BenchmarkProxyPLT.addedDevices) > 0 {
				var target interface{}
				randomID := BenchmarkProxyPLT.addedDevices[rand.Intn(len(BenchmarkProxyPLT.addedDevices))]
				firmProxy := BenchmarkProxyRoot.node.CreateProxy(context.Background(), "/", false)
				if target = firmProxy.Get(context.Background(), "/devices/"+randomID, 0, false,
					""); !reflect.ValueOf(target).IsValid() {
					BenchmarkProxyLogger.Errorf("Failed to find device: %s %+v", randomID, target)
					continue
				}

				BenchmarkProxyPLT.SetPreUpdateExecuted(false)
				BenchmarkProxyPLT.SetPostUpdateExecuted(false)
				firmProxy.RegisterCallback(PreUpdate, commonCallbackFunc, "PreUpdate", BenchmarkProxyPLT.SetPreUpdateExecuted)
				firmProxy.RegisterCallback(PostUpdate, commonCallbackFunc, "PostUpdate", BenchmarkProxyPLT.SetPostUpdateExecuted)

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
				if updated = firmProxy.Update(context.Background(), "/devices/"+randomID, target.(*voltha.Device), false,
					""); updated == nil {
					BenchmarkProxyLogger.Errorf("Failed to update device: %+v", target)
					continue
				} else {
					BenchmarkProxyLogger.Infof("Device was updated : %+v", updated)

				}

				if d := firmProxy.Get(context.Background(), "/devices/"+randomID, 0, false,
					""); !reflect.ValueOf(d).IsValid() {
					BenchmarkProxyLogger.Errorf("Failed to get device: %s", randomID)
					continue
				} else if d.(*voltha.Device).FirmwareVersion == after {
					BenchmarkProxyLogger.Infof("Imm Device was updated with new value: %s %+v", randomID, d)
				} else if d.(*voltha.Device).FirmwareVersion == before {
					BenchmarkProxyLogger.Errorf("Imm Device kept old value: %s %+v %+v", randomID, d, target)
				} else {
					BenchmarkProxyLogger.Errorf("Imm Device has unknown value: %s %+v %+v", randomID, d, target)
				}

				BenchmarkProxyPLT.firmwareMutex.Lock()

				BenchmarkProxyPLT.updatedFirmwares = append(
					BenchmarkProxyPLT.updatedFirmwares,
					proxyLoadChanges{ID: randomID, Before: before, After: after},
				)
				BenchmarkProxyPLT.firmwareMutex.Unlock()
			}
		}
	})
}

func BenchmarkProxy_UpdateFlows(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if len(BenchmarkProxyPLT.addedDevices) > 0 {
				randomID := BenchmarkProxyPLT.addedDevices[rand.Intn(len(BenchmarkProxyPLT.addedDevices))]

				flowsProxy := BenchmarkProxyRoot.node.CreateProxy(context.Background(), "/devices/"+randomID+"/flows", false)
				flows := flowsProxy.Get(context.Background(), "/", 0, false, "")

				before := flows.(*openflow_13.Flows).Items[0].TableId
				flows.(*openflow_13.Flows).Items[0].TableId = uint32(rand.Intn(3000))
				after := flows.(*openflow_13.Flows).Items[0].TableId

				flowsProxy.RegisterCallback(
					PreUpdate,
					commonCallback2,
				)
				flowsProxy.RegisterCallback(
					PostUpdate,
					commonCallback2,
				)

				var updated interface{}
				if updated = flowsProxy.Update(context.Background(), "/", flows.(*openflow_13.Flows), false, ""); updated == nil {
					b.Errorf("Failed to update flows for device: %+v", flows)
				} else {
					BenchmarkProxyLogger.Infof("Flows were updated : %+v", updated)
				}
				BenchmarkProxyPLT.flowMutex.Lock()
				BenchmarkProxyPLT.updatedFlows = append(
					BenchmarkProxyPLT.updatedFlows,
					proxyLoadChanges{ID: randomID, Before: before, After: after},
				)
				BenchmarkProxyPLT.flowMutex.Unlock()
			}
		}
	})
}

func BenchmarkProxy_GetDevices(b *testing.B) {
	//traverseBranches(BenchmarkProxy_DeviceProxy.Root.node.Branches[NONE].GetLatest(), 0)

	for i := 0; i < len(BenchmarkProxyPLT.addedDevices); i++ {
		devToGet := BenchmarkProxyPLT.addedDevices[i]
		// Verify that the added device can now be retrieved
		if d := BenchmarkProxyDeviceProxy.Get(context.Background(), "/devices/"+devToGet, 0, false,
			""); !reflect.ValueOf(d).IsValid() {
			BenchmarkProxyLogger.Errorf("Failed to get device: %s", devToGet)
			continue
		} else {
			BenchmarkProxyLogger.Infof("Got device: %s %+v", devToGet, d)
		}
	}
}

func BenchmarkProxy_GetUpdatedFirmware(b *testing.B) {
	for i := 0; i < len(BenchmarkProxyPLT.updatedFirmwares); i++ {
		devToGet := BenchmarkProxyPLT.updatedFirmwares[i].ID
		// Verify that the updated device can be retrieved and that the updates were actually applied
		if d := BenchmarkProxyDeviceProxy.Get(context.Background(), "/devices/"+devToGet, 0, false,
			""); !reflect.ValueOf(d).IsValid() {
			BenchmarkProxyLogger.Errorf("Failed to get device: %s", devToGet)
			continue
		} else if d.(*voltha.Device).FirmwareVersion == BenchmarkProxyPLT.updatedFirmwares[i].After.(string) {
			BenchmarkProxyLogger.Infof("Device was updated with new value: %s %+v", devToGet, d)
		} else if d.(*voltha.Device).FirmwareVersion == BenchmarkProxyPLT.updatedFirmwares[i].Before.(string) {
			BenchmarkProxyLogger.Errorf("Device kept old value: %s %+v %+v", devToGet, d, BenchmarkProxyPLT.updatedFirmwares[i])
		} else {
			BenchmarkProxyLogger.Errorf("Device has unknown value: %s %+v %+v", devToGet, d, BenchmarkProxyPLT.updatedFirmwares[i])
		}
	}
}
