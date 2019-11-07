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
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

var (
	BenchmarkProxy_Root        *root
	BenchmarkProxy_DeviceProxy *Proxy
	BenchmarkProxy_PLT         *proxyLoadTest
	BenchmarkProxy_Logger      log.Logger
	err                        error
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
	BenchmarkProxy_Root = NewRoot(&voltha.Voltha{}, nil)

	BenchmarkProxy_Logger, _ = log.AddPackage(log.JSON, log.DebugLevel, log.Fields{"instanceId": "PLT"})
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

	BenchmarkProxy_DeviceProxy, err = BenchmarkProxy_Root.node.CreateProxy(context.Background(), "/", false)
	if err != nil {
		log.Errorf("error %v", err)
	}
	// Register ADD instructions callbacks
	BenchmarkProxy_PLT = &proxyLoadTest{}

	BenchmarkProxy_DeviceProxy.RegisterCallback(PRE_ADD, commonCallbackFunc, "PRE_ADD", BenchmarkProxy_PLT.SetPreAddExecuted)
	BenchmarkProxy_DeviceProxy.RegisterCallback(POST_ADD, commonCallbackFunc, "POST_ADD", BenchmarkProxy_PLT.SetPostAddExecuted)

	//// Register UPDATE instructions callbacks
	BenchmarkProxy_DeviceProxy.RegisterCallback(PRE_UPDATE, commonCallbackFunc, "PRE_UPDATE", BenchmarkProxy_PLT.SetPreUpdateExecuted)
	BenchmarkProxy_DeviceProxy.RegisterCallback(POST_UPDATE, commonCallbackFunc, "POST_UPDATE", BenchmarkProxy_PLT.SetPostUpdateExecuted)

}

func BenchmarkProxy_AddDevice(b *testing.B) {
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

			BenchmarkProxy_PLT.SetPreAddExecuted(false)
			BenchmarkProxy_PLT.SetPostAddExecuted(false)

			var added interface{}
			// Add the device
			if added, _ = BenchmarkProxy_DeviceProxy.AddWithID(context.Background(), "/devices", ltDevID, ltDevice, ""); added == nil {
				BenchmarkProxy_Logger.Errorf("Failed to add device: %+v", ltDevice)
				continue
			} else {
				BenchmarkProxy_Logger.Infof("Device was added 1: %+v", added)
			}

			BenchmarkProxy_PLT.addMutex.Lock()
			BenchmarkProxy_PLT.addedDevices = append(BenchmarkProxy_PLT.addedDevices, added.(*voltha.Device).Id)
			BenchmarkProxy_PLT.addMutex.Unlock()
		}
	})

	BenchmarkProxy_Logger.Infof("Number of added devices : %d", len(BenchmarkProxy_PLT.addedDevices))
}

func BenchmarkProxy_UpdateFirmware(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			//for i:=0; i < b.N; i++ {

			if len(BenchmarkProxy_PLT.addedDevices) > 0 {
				var target interface{}
				randomID := BenchmarkProxy_PLT.addedDevices[rand.Intn(len(BenchmarkProxy_PLT.addedDevices))]
				firmProxy, err := BenchmarkProxy_Root.node.CreateProxy(context.Background(), "/", false)
				if err != nil {
					log.Errorf("error %v", err)
				}
				if target, err = firmProxy.Get(context.Background(), "/devices/"+randomID, 0, false,
					""); err != nil {
					log.Errorf("error %v", err)
				} else if !reflect.ValueOf(target).IsValid() {
					BenchmarkProxy_Logger.Errorf("Failed to find device: %s %+v", randomID, target)
					continue
				}

				BenchmarkProxy_PLT.SetPreUpdateExecuted(false)
				BenchmarkProxy_PLT.SetPostUpdateExecuted(false)
				firmProxy.RegisterCallback(PRE_UPDATE, commonCallbackFunc, "PRE_UPDATE", BenchmarkProxy_PLT.SetPreUpdateExecuted)
				firmProxy.RegisterCallback(POST_UPDATE, commonCallbackFunc, "POST_UPDATE", BenchmarkProxy_PLT.SetPostUpdateExecuted)

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
				if updated, err = firmProxy.Update(context.Background(), "/devices/"+randomID, target.(*voltha.Device), false,
					""); err != nil {
					log.Errorf("error %v", err)
				} else if updated == nil {
					BenchmarkProxy_Logger.Errorf("Failed to update device: %+v", target)
					continue
				} else {
					BenchmarkProxy_Logger.Infof("Device was updated : %+v", updated)

				}

				if d, err := firmProxy.Get(context.Background(), "/devices/"+randomID, 0, false,
					""); err != nil {
					log.Errorf("error %v", err)
				} else if !reflect.ValueOf(d).IsValid() {
					BenchmarkProxy_Logger.Errorf("Failed to get device: %s", randomID)
					continue
				} else if d.(*voltha.Device).FirmwareVersion == after {
					BenchmarkProxy_Logger.Infof("Imm Device was updated with new value: %s %+v", randomID, d)
				} else if d.(*voltha.Device).FirmwareVersion == before {
					BenchmarkProxy_Logger.Errorf("Imm Device kept old value: %s %+v %+v", randomID, d, target)
				} else {
					BenchmarkProxy_Logger.Errorf("Imm Device has unknown value: %s %+v %+v", randomID, d, target)
				}

				BenchmarkProxy_PLT.firmwareMutex.Lock()

				BenchmarkProxy_PLT.updatedFirmwares = append(
					BenchmarkProxy_PLT.updatedFirmwares,
					proxyLoadChanges{ID: randomID, Before: before, After: after},
				)
				BenchmarkProxy_PLT.firmwareMutex.Unlock()
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

	BenchmarkProxy_Logger.Debugf("%sRevision: %s %+v", prefix, revision.GetHash(), revision.GetData())

	//for brIdx, brRev := range revision.GetBranch().Revisions {
	//	BenchmarkProxy_Logger.Debugf("%sbranchIndex: %s", prefix, brIdx)
	//	traverseBranches(brRev, depth+1)
	//}
	for childrenI, children := range revision.GetAllChildren() {
		BenchmarkProxy_Logger.Debugf("%schildrenIndex: %s, length: %d", prefix, childrenI, len(children))

		for _, subrev := range children {
			//subrev.GetBranch().Latest
			traverseBranches(subrev, depth+1)
		}
	}

}
func BenchmarkProxy_UpdateFlows(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if len(BenchmarkProxy_PLT.addedDevices) > 0 {
				randomID := BenchmarkProxy_PLT.addedDevices[rand.Intn(len(BenchmarkProxy_PLT.addedDevices))]

				flowsProxy, err := BenchmarkProxy_Root.node.CreateProxy(context.Background(), "/devices/"+randomID+"/flows", false)
				if err != nil {
					log.Errorf("error %v", err)
				}
				flows, err := flowsProxy.Get(context.Background(), "/", 0, false, "")
				if err != nil {
					log.Errorf("error %v", err)
				}

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
				if updated, err = flowsProxy.Update(context.Background(), "/", flows.(*openflow_13.Flows), false, ""); err != nil {
					log.Errorf("error %v", err)
				} else if updated == nil {
					b.Errorf("Failed to update flows for device: %+v", flows)
				} else {
					BenchmarkProxy_Logger.Infof("Flows were updated : %+v", updated)
				}
				BenchmarkProxy_PLT.flowMutex.Lock()
				BenchmarkProxy_PLT.updatedFlows = append(
					BenchmarkProxy_PLT.updatedFlows,
					proxyLoadChanges{ID: randomID, Before: before, After: after},
				)
				BenchmarkProxy_PLT.flowMutex.Unlock()
			}
		}
	})
}

func BenchmarkProxy_GetDevices(b *testing.B) {
	//traverseBranches(BenchmarkProxy_DeviceProxy.Root.node.Branches[NONE].GetLatest(), 0)

	for i := 0; i < len(BenchmarkProxy_PLT.addedDevices); i++ {
		devToGet := BenchmarkProxy_PLT.addedDevices[i]
		// Verify that the added device can now be retrieved
		if d, err := BenchmarkProxy_DeviceProxy.Get(context.Background(), "/devices/"+devToGet, 0, false,
			""); err != nil {
			log.Errorf("error %v", err)
		} else if !reflect.ValueOf(d).IsValid() {
			BenchmarkProxy_Logger.Errorf("Failed to get device: %s", devToGet)
			continue
		} else {
			BenchmarkProxy_Logger.Infof("Got device: %s %+v", devToGet, d)
		}
	}
}

func BenchmarkProxy_GetUpdatedFirmware(b *testing.B) {
	for i := 0; i < len(BenchmarkProxy_PLT.updatedFirmwares); i++ {
		devToGet := BenchmarkProxy_PLT.updatedFirmwares[i].ID
		// Verify that the updated device can be retrieved and that the updates were actually applied
		if d, err := BenchmarkProxy_DeviceProxy.Get(context.Background(), "/devices/"+devToGet, 0, false,
			""); err != nil {
			log.Errorf("error %v", err)
		} else if !reflect.ValueOf(d).IsValid() {
			BenchmarkProxy_Logger.Errorf("Failed to get device: %s", devToGet)
			continue
		} else if d.(*voltha.Device).FirmwareVersion == BenchmarkProxy_PLT.updatedFirmwares[i].After.(string) {
			BenchmarkProxy_Logger.Infof("Device was updated with new value: %s %+v", devToGet, d)
		} else if d.(*voltha.Device).FirmwareVersion == BenchmarkProxy_PLT.updatedFirmwares[i].Before.(string) {
			BenchmarkProxy_Logger.Errorf("Device kept old value: %s %+v %+v", devToGet, d, BenchmarkProxy_PLT.updatedFirmwares[i])
		} else {
			BenchmarkProxy_Logger.Errorf("Device has unknown value: %s %+v %+v", devToGet, d, BenchmarkProxy_PLT.updatedFirmwares[i])
		}
	}
}
