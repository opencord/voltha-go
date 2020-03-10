/*
 * Copyright 2019-present Open Networking Foundation

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

package mocks

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/adapters/adapterif"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	of "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

const (
	numONUPerOLT      = 4
	startingUNIPortNo = 100
)

// OLTAdapter represent OLT adapter
type OLTAdapter struct {
	flows map[uint64]*voltha.OfpFlowStats
	lock  sync.Mutex
	Adapter
}

// NewOLTAdapter - creates OLT adapter instance
func NewOLTAdapter(cp adapterif.CoreProxy) *OLTAdapter {
	return &OLTAdapter{
		flows: map[uint64]*voltha.OfpFlowStats{},
		Adapter: Adapter{
			coreProxy: cp,
		},
	}
}

// Adopt_device creates new handler for added device
func (oltA *OLTAdapter) Adopt_device(device *voltha.Device) error { // nolint
	go func() {
		d := proto.Clone(device).(*voltha.Device)
		d.Root = true
		d.Vendor = "olt_adapter_mock"
		d.Model = "go-mock"
		d.SerialNumber = com.GetRandomSerialNumber()
		d.MacAddress = strings.ToUpper(com.GetRandomMacAddress())
		oltA.storeDevice(d)
		if res := oltA.coreProxy.DeviceUpdate(context.TODO(), d); res != nil {
			log.Fatalf("deviceUpdate-failed-%s", res)
		}
		nniPort := &voltha.Port{
			PortNo:     2,
			Label:      fmt.Sprintf("nni-%d", 2),
			Type:       voltha.Port_ETHERNET_NNI,
			OperStatus: voltha.OperStatus_ACTIVE,
		}
		var err error
		if err = oltA.coreProxy.PortCreated(context.TODO(), d.Id, nniPort); err != nil {
			log.Fatalf("PortCreated-failed-%s", err)
		}

		ponPort := &voltha.Port{
			PortNo:     1,
			Label:      fmt.Sprintf("pon-%d", 1),
			Type:       voltha.Port_PON_OLT,
			OperStatus: voltha.OperStatus_ACTIVE,
		}
		if err = oltA.coreProxy.PortCreated(context.TODO(), d.Id, ponPort); err != nil {
			log.Fatalf("PortCreated-failed-%s", err)
		}

		d.ConnectStatus = voltha.ConnectStatus_REACHABLE
		d.OperStatus = voltha.OperStatus_ACTIVE

		if err = oltA.coreProxy.DeviceStateUpdate(context.TODO(), d.Id, d.ConnectStatus, d.OperStatus); err != nil {
			log.Fatalf("Device-state-update-failed-%s", err)
		}

		//Get the latest device data from the Core
		if d, err = oltA.coreProxy.GetDevice(context.TODO(), d.Id, d.Id); err != nil {
			log.Fatalf("getting-device-failed-%s", err)
		}

		if err = oltA.updateDevice(d); err != nil {
			log.Fatalf("saving-device-failed-%s", err)
		}

		// Register Child devices
		initialUniPortNo := startingUNIPortNo
		for i := 0; i < numONUPerOLT; i++ {
			go func(seqNo int) {
				if _, err := oltA.coreProxy.ChildDeviceDetected(
					context.TODO(),
					d.Id,
					1,
					"onu_adapter_mock",
					initialUniPortNo+seqNo,
					"onu_adapter_mock",
					com.GetRandomSerialNumber(),
					int64(seqNo)); err != nil {
					log.Fatalf("failure-sending-child-device-%s", err)
				}
			}(i)
		}
	}()
	return nil
}

// Get_ofp_device_info returns ofp device info
func (oltA *OLTAdapter) Get_ofp_device_info(device *voltha.Device) (*ic.SwitchCapability, error) { // nolint
	if d := oltA.getDevice(device.Id); d == nil {
		log.Fatalf("device-not-found-%s", device.Id)
	}
	return &ic.SwitchCapability{
		Desc: &of.OfpDesc{
			HwDesc:    "olt_adapter_mock",
			SwDesc:    "olt_adapter_mock",
			SerialNum: "12345678",
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

// Get_ofp_port_info returns ofp port info
func (oltA *OLTAdapter) Get_ofp_port_info(device *voltha.Device, portNo int64) (*ic.PortCapability, error) { // nolint
	if d := oltA.getDevice(device.Id); d == nil {
		log.Fatalf("device-not-found-%s", device.Id)
	}
	capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
	return &ic.PortCapability{
		Port: &voltha.LogicalPort{
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array("11:22:33:44:55:66"),
				Config:     0,
				State:      uint32(of.OfpPortState_OFPPS_LIVE),
				Curr:       capability,
				Advertised: capability,
				Peer:       capability,
				CurrSpeed:  uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
				MaxSpeed:   uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
			},
			DeviceId:     device.Id,
			DevicePortNo: uint32(portNo),
		},
	}, nil
}

// GetNumONUPerOLT returns number of ONUs per OLT
func (oltA *OLTAdapter) GetNumONUPerOLT() int {
	return numONUPerOLT
}

// Returns the starting UNI port number
func (oltA *OLTAdapter) GetStartingUNIPortNo() int {
	return startingUNIPortNo
}

// Disable_device disables device
func (oltA *OLTAdapter) Disable_device(device *voltha.Device) error { // nolint
	go func() {
		if d := oltA.getDevice(device.Id); d == nil {
			log.Fatalf("device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to disable
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, voltha.OperStatus_UNKNOWN); err != nil {
			log.Fatalf("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		//Update the device state
		cloned.ConnectStatus = voltha.ConnectStatus_UNREACHABLE
		cloned.OperStatus = voltha.OperStatus_UNKNOWN

		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			log.Fatalf("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		if err := oltA.updateDevice(cloned); err != nil {
			log.Fatalf("saving-device-failed-%s", err)
		}

		// Tell the Core that all child devices have been disabled (by default it's an action already taken by the Core
		if err := oltA.coreProxy.ChildDevicesLost(context.TODO(), cloned.Id); err != nil {
			// Device may already have been deleted in the core
			log.Warnw("lost-notif-of-child-devices-failed", log.Fields{"deviceId": device.Id, "error": err})
		}
	}()
	return nil
}

// Reenable_device reenables device
func (oltA *OLTAdapter) Reenable_device(device *voltha.Device) error { // nolint
	go func() {
		if d := oltA.getDevice(device.Id); d == nil {
			log.Fatalf("device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to enable
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, voltha.OperStatus_ACTIVE); err != nil {
			log.Fatalf("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		//Update the device state
		cloned.ConnectStatus = voltha.ConnectStatus_REACHABLE
		cloned.OperStatus = voltha.OperStatus_ACTIVE

		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			log.Fatalf("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		// Tell the Core that all child devices have been enabled
		if err := oltA.coreProxy.ChildDevicesDetected(context.TODO(), cloned.Id); err != nil {
			log.Fatalf("detection-notif-of-child-devices-failed", log.Fields{"deviceId": device.Id, "error": err})
		}
	}()
	return nil
}

// Enable_port -
func (oltA *OLTAdapter) Enable_port(deviceId string, Port *voltha.Port) error { //nolint
	go func() {

		if Port.Type == voltha.Port_PON_OLT {
			if err := oltA.coreProxy.PortStateUpdate(context.TODO(), deviceId, voltha.Port_PON_OLT, Port.PortNo, voltha.OperStatus_ACTIVE); err != nil {
				log.Fatalf("updating-ports-failed", log.Fields{"device-id": deviceId, "error": err})
			}
		}

	}()
	return nil
}

// Disable_port -
func (oltA *OLTAdapter) Disable_port(deviceId string, Port *voltha.Port) error { //nolint
	go func() {

		if Port.Type == voltha.Port_PON_OLT {
			if err := oltA.coreProxy.PortStateUpdate(context.TODO(), deviceId, voltha.Port_PON_OLT, Port.PortNo, voltha.OperStatus_DISCOVERED); err != nil {
				log.Fatalf("updating-ports-failed", log.Fields{"device-id": deviceId, "error": err})
			}
		}
	}()
	return nil
}

// Child_device_lost deletes ONU and its references
func (oltA *OLTAdapter) Child_device_lost(deviceID string, pPortNo uint32, onuID uint32) error { // nolint
	return nil
}

// Update_flows_incrementally mocks the incremental flow update
func (oltA *OLTAdapter) Update_flows_incrementally(device *voltha.Device, flows *of.FlowChanges, groups *of.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error { // nolint
	oltA.lock.Lock()
	defer oltA.lock.Unlock()

	if flows.ToAdd != nil {
		for _, f := range flows.ToAdd.Items {
			oltA.flows[f.Id] = f
		}
	}
	if flows.ToRemove != nil {
		for _, f := range flows.ToRemove.Items {
			delete(oltA.flows, f.Id)
		}
	}
	return nil
}

// GetFlowCount returns the total number of flows presently under this adapter
func (oltA *OLTAdapter) GetFlowCount() int {
	oltA.lock.Lock()
	defer oltA.lock.Unlock()

	return len(oltA.flows)
}

// ClearFlows removes all flows in this adapter
func (oltA *OLTAdapter) ClearFlows() {
	oltA.lock.Lock()
	defer oltA.lock.Unlock()

	oltA.flows = map[uint64]*voltha.OfpFlowStats{}
}
