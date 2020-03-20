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

// ONUAdapter represent ONU adapter attributes
type ONUAdapter struct {
	flows map[uint64]*voltha.OfpFlowStats
	lock  sync.Mutex
	Adapter
}

// NewONUAdapter creates ONU adapter
func NewONUAdapter(cp adapterif.CoreProxy) *ONUAdapter {
	return &ONUAdapter{
		flows: map[uint64]*voltha.OfpFlowStats{},
		Adapter: Adapter{
			coreProxy: cp,
		},
	}
}

// Adopt_device creates new handler for added device
func (onuA *ONUAdapter) Adopt_device(device *voltha.Device) error { // nolint
	go func() {
		d := proto.Clone(device).(*voltha.Device)
		d.Root = false
		d.Vendor = "onu_adapter_mock"
		d.Model = "go-mock"
		d.SerialNumber = com.GetRandomSerialNumber()
		d.MacAddress = strings.ToUpper(com.GetRandomMacAddress())
		onuA.storeDevice(d)
		if res := onuA.coreProxy.DeviceUpdate(context.TODO(), d); res != nil {
			logger.Fatalf("deviceUpdate-failed-%s", res)
		}

		d.ConnectStatus = voltha.ConnectStatus_REACHABLE
		d.OperStatus = voltha.OperStatus_DISCOVERED

		if err := onuA.coreProxy.DeviceStateUpdate(context.TODO(), d.Id, d.ConnectStatus, d.OperStatus); err != nil {
			logger.Fatalf("device-state-update-failed-%s", err)
		}

		uniPortNo := uint32(2)
		if device.ProxyAddress != nil {
			if device.ProxyAddress.ChannelId != 0 {
				uniPortNo = device.ProxyAddress.ChannelId
			}
		}

		uniPort := &voltha.Port{
			PortNo:     uniPortNo,
			Label:      fmt.Sprintf("uni-%d", uniPortNo),
			Type:       voltha.Port_ETHERNET_UNI,
			OperStatus: voltha.OperStatus_ACTIVE,
		}
		var err error
		if err = onuA.coreProxy.PortCreated(context.TODO(), d.Id, uniPort); err != nil {
			logger.Fatalf("PortCreated-failed-%s", err)
		}

		ponPortNo := uint32(1)
		if device.ParentPortNo != 0 {
			ponPortNo = device.ParentPortNo
		}

		ponPort := &voltha.Port{
			PortNo:     ponPortNo,
			Label:      fmt.Sprintf("pon-%d", ponPortNo),
			Type:       voltha.Port_PON_ONU,
			OperStatus: voltha.OperStatus_ACTIVE,
			Peers: []*voltha.Port_PeerPort{{DeviceId: d.ParentId, // Peer device  is OLT
				PortNo: device.ParentPortNo}}, // Peer port is parent's port number
		}
		if err = onuA.coreProxy.PortCreated(context.TODO(), d.Id, ponPort); err != nil {
			logger.Fatalf("PortCreated-failed-%s", err)
		}

		d.ConnectStatus = voltha.ConnectStatus_REACHABLE
		d.OperStatus = voltha.OperStatus_ACTIVE

		if err = onuA.coreProxy.DeviceStateUpdate(context.TODO(), d.Id, d.ConnectStatus, d.OperStatus); err != nil {
			logger.Fatalf("device-state-update-failed-%s", err)
		}
		//Get the latest device data from the Core
		if d, err = onuA.coreProxy.GetDevice(context.TODO(), d.Id, d.Id); err != nil {
			logger.Fatalf("getting-device-failed-%s", err)
		}

		if err = onuA.updateDevice(d); err != nil {
			logger.Fatalf("saving-device-failed-%s", err)
		}
	}()
	return nil
}

// Get_ofp_port_info returns ofp device info
func (onuA *ONUAdapter) Get_ofp_port_info(device *voltha.Device, portNo int64) (*ic.PortCapability, error) { // nolint
	if d := onuA.getDevice(device.Id); d == nil {
		logger.Fatalf("device-not-found-%s", device.Id)
	}
	capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
	return &ic.PortCapability{
		Port: &voltha.LogicalPort{
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array("12:12:12:12:12:12"),
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

// Disable_device disables device
func (onuA *ONUAdapter) Disable_device(device *voltha.Device) error { // nolint
	go func() {
		if d := onuA.getDevice(device.Id); d == nil {
			logger.Fatalf("device-not-found-%s", device.Id)
		}
		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to disable
		if err := onuA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, voltha.OperStatus_UNKNOWN); err != nil {
			// Device may also have been deleted in the Core
			logger.Warnw("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
			return
		}
		//Update the device state
		cloned.ConnectStatus = voltha.ConnectStatus_UNREACHABLE
		cloned.OperStatus = voltha.OperStatus_UNKNOWN

		if err := onuA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			logger.Warnw("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
			return
		}
		if err := onuA.updateDevice(cloned); err != nil {
			logger.Fatalf("saving-device-failed-%s", err)
		}
	}()
	return nil
}

// Reenable_device reenables device
func (onuA *ONUAdapter) Reenable_device(device *voltha.Device) error { // nolint
	go func() {
		if d := onuA.getDevice(device.Id); d == nil {
			logger.Fatalf("device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to enable
		if err := onuA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, voltha.OperStatus_ACTIVE); err != nil {
			logger.Fatalf("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		//Update the device state
		cloned.ConnectStatus = voltha.ConnectStatus_REACHABLE
		cloned.OperStatus = voltha.OperStatus_ACTIVE

		if err := onuA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			logger.Fatalf("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
		}
		if err := onuA.updateDevice(cloned); err != nil {
			logger.Fatalf("saving-device-failed-%s", err)
		}
	}()
	return nil
}

// Update_flows_incrementally mocks the incremental flow update
func (onuA *ONUAdapter) Update_flows_incrementally(device *voltha.Device, flows *of.FlowChanges, groups *of.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error { // nolint
	onuA.lock.Lock()
	defer onuA.lock.Unlock()

	if flows.ToAdd != nil {
		for _, f := range flows.ToAdd.Items {
			onuA.flows[f.Id] = f
		}
	}
	if flows.ToRemove != nil {
		for _, f := range flows.ToRemove.Items {
			delete(onuA.flows, f.Id)
		}
	}
	return nil
}

// GetFlowCount returns the total number of flows presently under this adapter
func (onuA *ONUAdapter) GetFlowCount() int {
	onuA.lock.Lock()
	defer onuA.lock.Unlock()

	return len(onuA.flows)
}

// ClearFlows removes all flows in this adapter
func (onuA *ONUAdapter) ClearFlows() {
	onuA.lock.Lock()
	defer onuA.lock.Unlock()

	onuA.flows = map[uint64]*voltha.OfpFlowStats{}
}
