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
	"errors"
	"fmt"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v4/pkg/adapters"
	"github.com/opencord/voltha-lib-go/v4/pkg/adapters/adapterif"
	com "github.com/opencord/voltha-lib-go/v4/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	of "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

const (
	numONUPerOLT      = 4
	startingUNIPortNo = 100
)

// static implementation check
var _ adapters.IAdapter = &OLTAdapter{}

// OLTAdapter represent OLT adapter
type OLTAdapter struct {
	*Adapter
}

// NewOLTAdapter - creates OLT adapter instance
func NewOLTAdapter(ctx context.Context, cp adapterif.CoreProxy) *OLTAdapter {
	return &OLTAdapter{
		Adapter: NewAdapter(cp),
	}
}

// Adopt_device creates new handler for added device
func (oltA *OLTAdapter) Adopt_device(ctx context.Context, device *voltha.Device) error { // nolint
	go func() {
		d := proto.Clone(device).(*voltha.Device)
		d.Root = true
		d.Vendor = "olt_adapter_mock"
		d.Model = "go-mock"
		d.SerialNumber = com.GetRandomSerialNumber()
		d.MacAddress = strings.ToUpper(com.GetRandomMacAddress())
		oltA.storeDevice(d)
		if res := oltA.coreProxy.DeviceUpdate(context.TODO(), d); res != nil {
			logger.Fatalf(ctx, "deviceUpdate-failed-%s", res)
		}
		capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
		nniPort := &voltha.Port{
			PortNo:     2,
			Label:      fmt.Sprintf("nni-%d", 2),
			Type:       voltha.Port_ETHERNET_NNI,
			OperStatus: voltha.OperStatus_ACTIVE,
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
		}
		var err error
		if err = oltA.coreProxy.PortCreated(context.TODO(), d.Id, nniPort); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		ponPort := &voltha.Port{
			PortNo:     1,
			Label:      fmt.Sprintf("pon-%d", 1),
			Type:       voltha.Port_PON_OLT,
			OperStatus: voltha.OperStatus_ACTIVE,
		}
		if err = oltA.coreProxy.PortCreated(context.TODO(), d.Id, ponPort); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		d.ConnectStatus = voltha.ConnectStatus_REACHABLE
		d.OperStatus = voltha.OperStatus_ACTIVE

		if err = oltA.coreProxy.DeviceStateUpdate(context.TODO(), d.Id, d.ConnectStatus, d.OperStatus); err != nil {
			logger.Fatalf(ctx, "Device-state-update-failed-%s", err)
		}

		//Get the latest device data from the Core
		if d, err = oltA.coreProxy.GetDevice(context.TODO(), d.Id, d.Id); err != nil {
			logger.Fatalf(ctx, "getting-device-failed-%s", err)
		}

		oltA.updateDevice(d)

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
					logger.Fatalf(ctx, "failure-sending-child-device-%s", err)
				}
			}(i)
		}
	}()
	return nil
}

// Get_ofp_device_info returns ofp device info
func (oltA *OLTAdapter) Get_ofp_device_info(ctx context.Context, device *voltha.Device) (*ic.SwitchCapability, error) { // nolint
	if d := oltA.getDevice(device.Id); d == nil {
		logger.Fatalf(ctx, "device-not-found-%s", device.Id)
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

// GetNumONUPerOLT returns number of ONUs per OLT
func (oltA *OLTAdapter) GetNumONUPerOLT() int {
	return numONUPerOLT
}

// Returns the starting UNI port number
func (oltA *OLTAdapter) GetStartingUNIPortNo() int {
	return startingUNIPortNo
}

// Disable_device disables device
func (oltA *OLTAdapter) Disable_device(ctx context.Context, device *voltha.Device) error { // nolint
	go func() {
		if d := oltA.getDevice(device.Id); d == nil {
			logger.Fatalf(ctx, "device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to disable
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, 0, voltha.OperStatus_UNKNOWN); err != nil {
			logger.Warnw(ctx, "updating-ports-failed", log.Fields{"device-id": device.Id, "error": err})
		}

		//Update the device operational state
		cloned.OperStatus = voltha.OperStatus_UNKNOWN
		// The device is still reachable after it has been disabled, so the connection status should not be changed.

		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
			return
		}

		oltA.updateDevice(cloned)

		// Tell the Core that all child devices have been disabled (by default it's an action already taken by the Core
		if err := oltA.coreProxy.ChildDevicesLost(context.TODO(), cloned.Id); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw(ctx, "lost-notif-of-child-devices-failed", log.Fields{"device-id": device.Id, "error": err})
		}
	}()
	return nil
}

// Reenable_device reenables device
func (oltA *OLTAdapter) Reenable_device(ctx context.Context, device *voltha.Device) error { // nolint
	go func() {
		if d := oltA.getDevice(device.Id); d == nil {
			logger.Fatalf(ctx, "device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to enable
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, 0, voltha.OperStatus_ACTIVE); err != nil {
			logger.Fatalf(ctx, "updating-ports-failed", log.Fields{"device-id": device.Id, "error": err})
		}

		//Update the device state
		cloned.OperStatus = voltha.OperStatus_ACTIVE

		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			logger.Fatalf(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
		}

		// Tell the Core that all child devices have been enabled
		if err := oltA.coreProxy.ChildDevicesDetected(context.TODO(), cloned.Id); err != nil {
			logger.Fatalf(ctx, "detection-notif-of-child-devices-failed", log.Fields{"device-id": device.Id, "error": err})
		}
	}()
	return nil
}

// Enable_port -
func (oltA *OLTAdapter) Enable_port(ctx context.Context, deviceId string, Port *voltha.Port) error { //nolint
	go func() {

		if Port.Type == voltha.Port_PON_OLT {
			if err := oltA.coreProxy.PortStateUpdate(context.TODO(), deviceId, voltha.Port_PON_OLT, Port.PortNo, voltha.OperStatus_ACTIVE); err != nil {
				logger.Fatalf(ctx, "updating-ports-failed", log.Fields{"device-id": deviceId, "error": err})
			}
		}

	}()
	return nil
}

// Disable_port -
func (oltA *OLTAdapter) Disable_port(ctx context.Context, deviceId string, Port *voltha.Port) error { //nolint
	go func() {

		if Port.Type == voltha.Port_PON_OLT {
			if err := oltA.coreProxy.PortStateUpdate(context.TODO(), deviceId, voltha.Port_PON_OLT, Port.PortNo, voltha.OperStatus_DISCOVERED); err != nil {
				// Corresponding device may have been deleted
				logger.Warnw(ctx, "updating-ports-failed", log.Fields{"device-id": deviceId, "error": err})
			}
		}
	}()
	return nil
}

// Child_device_lost deletes ONU and its references
func (oltA *OLTAdapter) Child_device_lost(ctx context.Context, deviceID string, pPortNo uint32, onuID uint32) error { // nolint
	return nil
}

// Reboot_device -
func (oltA *OLTAdapter) Reboot_device(ctx context.Context, device *voltha.Device) error { // nolint
	logger.Infow(ctx, "reboot-device", log.Fields{"device-id": device.Id})

	go func() {
		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), device.Id, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN); err != nil {
			logger.Fatalf(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
		}
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), device.Id, 0, voltha.OperStatus_UNKNOWN); err != nil {
			// Not an error as the previous command will start the process of clearing the OLT
			logger.Infow(ctx, "port-update-failed", log.Fields{"device-id": device.Id, "error": err})
		}
	}()
	return nil
}

// TODO: REMOVE Start_omci_test begins an omci self-test
func (oltA *OLTAdapter) Start_omci_test(ctx context.Context, device *voltha.Device, request *voltha.OmciTestRequest) (*ic.TestResponse, error) { // nolint
	_ = device
	return nil, errors.New("start-omci-test-not-implemented")
}

func (oltA *OLTAdapter) Get_ext_value(ctx context.Context, deviceId string, device *voltha.Device, valueflag voltha.ValueType_Type) (*voltha.ReturnValues, error) { // nolint
	_ = deviceId
	_ = device
	_ = valueflag
	return nil, errors.New("get-ext-value-not-implemented")
}
