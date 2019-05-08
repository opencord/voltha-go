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
package adaptercore

import (
	"context"
	"fmt"
	"github.com/gogo/protobuf/proto"
	com "github.com/opencord/voltha-go/adapters/common"
	"github.com/opencord/voltha-go/common/log"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	of "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"strconv"
	"strings"
	"sync"
)

//DeviceHandler follows the same patterns as ponsim_olt.  The only difference is that it does not
// interact with an OLT device.
type DeviceHandler struct {
	deviceId     string
	deviceType   string
	device       *voltha.Device
	coreProxy    *com.CoreProxy
	simulatedOLT *SimulatedOLT
	nniPort      *voltha.Port
	ponPort      *voltha.Port
	exitChannel  chan int
	lockDevice   sync.RWMutex
}

//NewDeviceHandler creates a new device handler
func NewDeviceHandler(cp *com.CoreProxy, device *voltha.Device, adapter *SimulatedOLT) *DeviceHandler {
	var dh DeviceHandler
	dh.coreProxy = cp
	cloned := (proto.Clone(device)).(*voltha.Device)
	dh.deviceId = cloned.Id
	dh.deviceType = cloned.Type
	dh.device = cloned
	dh.simulatedOLT = adapter
	dh.exitChannel = make(chan int, 1)
	dh.lockDevice = sync.RWMutex{}
	return &dh
}

// start save the device to the data model
func (dh *DeviceHandler) start(ctx context.Context) {
	dh.lockDevice.Lock()
	defer dh.lockDevice.Unlock()
	log.Debugw("starting-device-agent", log.Fields{"device": dh.device})
	// Add the initial device to the local model
	log.Debug("device-agent-started")
}

// stop stops the device dh.  Not much to do for now
func (dh *DeviceHandler) stop(ctx context.Context) {
	dh.lockDevice.Lock()
	defer dh.lockDevice.Unlock()
	log.Debug("stopping-device-agent")
	dh.exitChannel <- 1
	log.Debug("device-agent-stopped")
}

func macAddressToUint32Array(mac string) []uint32 {
	slist := strings.Split(mac, ":")
	result := make([]uint32, len(slist))
	var err error
	var tmp int64
	for index, val := range slist {
		if tmp, err = strconv.ParseInt(val, 16, 32); err != nil {
			return []uint32{1, 2, 3, 4, 5, 6}
		}
		result[index] = uint32(tmp)
	}
	return result
}

func (dh *DeviceHandler) AdoptDevice(device *voltha.Device) {
	log.Debugw("AdoptDevice", log.Fields{"deviceId": device.Id})

	//	Update the device info
	cloned := proto.Clone(device).(*voltha.Device)
	cloned.Root = true
	cloned.Vendor = "simulators"
	cloned.Model = "go-simulators"
	cloned.SerialNumber = com.GetRandomSerialNumber()
	cloned.MacAddress = strings.ToUpper(com.GetRandomMacAddress())

	// Synchronous call to update device - this method is run in its own go routine
	if err := dh.coreProxy.DeviceUpdate(nil, cloned); err != nil {
		log.Errorw("error-updating-device", log.Fields{"deviceId": device.Id, "error": err})
	}

	//	Now create the NNI Port
	dh.nniPort = &voltha.Port{
		PortNo:     2,
		Label:      fmt.Sprintf("nni-%d", 2),
		Type:       voltha.Port_ETHERNET_NNI,
		OperStatus: voltha.OperStatus_ACTIVE,
	}

	// Synchronous call to update device - this method is run in its own go routine
	if err := dh.coreProxy.PortCreated(nil, cloned.Id, dh.nniPort); err != nil {
		log.Errorw("error-creating-nni-port", log.Fields{"deviceId": device.Id, "error": err})
	}

	//	Now create the PON Port
	dh.ponPort = &voltha.Port{
		PortNo:     1,
		Label:      fmt.Sprintf("pon-%d", 1),
		Type:       voltha.Port_PON_OLT,
		OperStatus: voltha.OperStatus_ACTIVE,
	}

	// Synchronous call to update device - this method is run in its own go routine
	if err := dh.coreProxy.PortCreated(nil, cloned.Id, dh.ponPort); err != nil {
		log.Errorw("error-creating-nni-port", log.Fields{"deviceId": device.Id, "error": err})
	}

	cloned.ConnectStatus = voltha.ConnectStatus_REACHABLE
	cloned.OperStatus = voltha.OperStatus_ACTIVE

	dh.device = cloned
	//dh.device.SerialNumber = cloned.SerialNumber

	//	Update the device state
	if err := dh.coreProxy.DeviceStateUpdate(nil, cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
		log.Errorw("error-creating-nni-port", log.Fields{"deviceId": device.Id, "error": err})
	}

	//	Register Child device
	initialUniPortNo := 100
	log.Debugw("registering-onus", log.Fields{"total": dh.simulatedOLT.numOnus})
	for i := 0; i < dh.simulatedOLT.numOnus; i++ {
		go dh.coreProxy.ChildDeviceDetected(
			nil,
			cloned.Id,
			1,
			"simulated_onu",
			initialUniPortNo+i,
			"simulated_onu",
			com.GetRandomSerialNumber(),
			int64(i))
	}
}

func (dh *DeviceHandler) GetOfpDeviceInfo(device *voltha.Device) (*ic.SwitchCapability, error) {
	return &ic.SwitchCapability{
		Desc: &of.OfpDesc{
			HwDesc:    "simulated_pon",
			SwDesc:    "simulated_pon",
			SerialNum: dh.device.SerialNumber,
		},
		SwitchFeatures: &of.OfpSwitchFeatures{
			NBuffers: 256,
			NTables:  2,
			Capabilities: uint32(of.OfpCapabilities_OFPC_FLOW_STATS |
				of.OfpCapabilities_OFPC_TABLE_STATS |
				of.OfpCapabilities_OFPC_PORT_STATS |
				of.OfpCapabilities_OFPC_GROUP_STATS),
		},
	}, nil
}

func (dh *DeviceHandler) GetOfpPortInfo(device *voltha.Device, portNo int64) (*ic.PortCapability, error) {
	cap := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
	return &ic.PortCapability{
		Port: &voltha.LogicalPort{
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array(dh.device.MacAddress),
				Config:     0,
				State:      uint32(of.OfpPortState_OFPPS_LIVE),
				Curr:       cap,
				Advertised: cap,
				Peer:       cap,
				CurrSpeed:  uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
				MaxSpeed:   uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
			},
			DeviceId:     dh.device.Id,
			DevicePortNo: uint32(portNo),
		},
	}, nil
}

func (dh *DeviceHandler) Process_inter_adapter_message(msg *ic.InterAdapterMessage) error {
	log.Debugw("Process_inter_adapter_message", log.Fields{"msgId": msg.Header.Id})
	return nil
}

func (dh *DeviceHandler) DisableDevice(device *voltha.Device) {
	cloned := proto.Clone(device).(*voltha.Device)
	// Update the all ports state on that device to disable
	if err := dh.coreProxy.PortsStateUpdate(nil, cloned.Id, voltha.OperStatus_UNKNOWN); err != nil {
		log.Errorw("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	//Update the device state
	cloned.ConnectStatus = voltha.ConnectStatus_UNREACHABLE
	cloned.OperStatus = voltha.OperStatus_UNKNOWN
	dh.device = cloned

	if err := dh.coreProxy.DeviceStateUpdate(nil, cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
		log.Errorw("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	// Tell the Core that all child devices have been disabled (by default it's an action already taken by the Core
	if err := dh.coreProxy.ChildDevicesLost(nil, cloned.Id); err != nil {
		log.Errorw("lost-notif-of-child-devices-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	log.Debugw("DisableDevice-end", log.Fields{"deviceId": device.Id})
}

func (dh *DeviceHandler) ReEnableDevice(device *voltha.Device) {

	cloned := proto.Clone(device).(*voltha.Device)
	// Update the all ports state on that device to enable
	if err := dh.coreProxy.PortsStateUpdate(nil, cloned.Id, voltha.OperStatus_ACTIVE); err != nil {
		log.Errorw("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	//Update the device state
	cloned.ConnectStatus = voltha.ConnectStatus_REACHABLE
	cloned.OperStatus = voltha.OperStatus_ACTIVE
	dh.device = cloned

	if err := dh.coreProxy.DeviceStateUpdate(nil, cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
		log.Errorw("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	// Tell the Core that all child devices have been enabled
	if err := dh.coreProxy.ChildDevicesDetected(nil, cloned.Id); err != nil {
		log.Errorw("detection-notif-of-child-devices-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	log.Debugw("ReEnableDevice-end", log.Fields{"deviceId": device.Id})
}

func (dh *DeviceHandler) DeleteDevice(device *voltha.Device) {
	cloned := proto.Clone(device).(*voltha.Device)
	// Update the all ports state on that device to disable
	if err := dh.coreProxy.DeleteAllPorts(nil, cloned.Id); err != nil {
		log.Errorw("delete-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		return
	}

	log.Debugw("DeleteDevice-end", log.Fields{"deviceId": device.Id})
}
