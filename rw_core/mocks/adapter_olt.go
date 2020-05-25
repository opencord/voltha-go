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
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/adapters/adapterif"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	of "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"strings"
)

const (
	numONUPerOLT      = 4
	startingUNIPortNo = 100
)

// OLTAdapter represent OLT adapter
type OLTAdapter struct {
	*Adapter
}

// NewOLTAdapter - creates OLT adapter instance
func NewOLTAdapter(ctx context.Context, cp adapterif.CoreProxy) *OLTAdapter {
	return &OLTAdapter{
		Adapter: NewAdapter(ctx, cp),
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
		oltA.storeDevice(ctx, d)
		if res := oltA.coreProxy.DeviceUpdate(ctx, d); res != nil {
			logger.Fatalf("deviceUpdate-failed-%s", res)
		}
		nniPort := &voltha.Port{
			PortNo:     2,
			Label:      fmt.Sprintf("nni-%d", 2),
			Type:       voltha.Port_ETHERNET_NNI,
			OperStatus: voltha.OperStatus_ACTIVE,
		}
		var err error
		if err = oltA.coreProxy.PortCreated(ctx, d.Id, nniPort); err != nil {
			logger.Fatalf("PortCreated-failed-%s", err)
		}

		ponPort := &voltha.Port{
			PortNo:     1,
			Label:      fmt.Sprintf("pon-%d", 1),
			Type:       voltha.Port_PON_OLT,
			OperStatus: voltha.OperStatus_ACTIVE,
		}
		if err = oltA.coreProxy.PortCreated(ctx, d.Id, ponPort); err != nil {
			logger.Fatalf("PortCreated-failed-%s", err)
		}

		d.ConnectStatus = voltha.ConnectStatus_REACHABLE
		d.OperStatus = voltha.OperStatus_ACTIVE

		if err = oltA.coreProxy.DeviceStateUpdate(ctx, d.Id, d.ConnectStatus, d.OperStatus); err != nil {
			logger.Fatalf("Device-state-update-failed-%s", err)
		}

		//Get the latest device data from the Core
		if d, err = oltA.coreProxy.GetDevice(ctx, d.Id, d.Id); err != nil {
			logger.Fatalf("getting-device-failed-%s", err)
		}

		oltA.updateDevice(ctx, d)

		// Register Child devices
		initialUniPortNo := startingUNIPortNo
		for i := 0; i < numONUPerOLT; i++ {
			go func(seqNo int) {
				if _, err := oltA.coreProxy.ChildDeviceDetected(
					ctx,
					d.Id,
					1,
					"onu_adapter_mock",
					initialUniPortNo+seqNo,
					"onu_adapter_mock",
					com.GetRandomSerialNumber(),
					int64(seqNo)); err != nil {
					logger.Fatalf("failure-sending-child-device-%s", err)
				}
			}(i)
		}
	}()
	return nil
}

// Get_ofp_device_info returns ofp device info
func (oltA *OLTAdapter) Get_ofp_device_info(ctx context.Context, device *voltha.Device) (*ic.SwitchCapability, error) { // nolint
	if d := oltA.getDevice(ctx, device.Id); d == nil {
		logger.Fatalf("device-not-found-%s", device.Id)
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
func (oltA *OLTAdapter) Get_ofp_port_info(ctx context.Context, device *voltha.Device, portNo int64) (*ic.PortCapability, error) { // nolint
	if d := oltA.getDevice(ctx, device.Id); d == nil {
		logger.Fatalf("device-not-found-%s", device.Id)
	}
	capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
	return &ic.PortCapability{
		Port: &voltha.LogicalPort{
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array(ctx, "11:22:33:44:55:66"),
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
func (oltA *OLTAdapter) GetNumONUPerOLT(ctx context.Context) int {
	return numONUPerOLT
}

// Returns the starting UNI port number
func (oltA *OLTAdapter) GetStartingUNIPortNo(ctx context.Context) int {
	return startingUNIPortNo
}

// Disable_device disables device
func (oltA *OLTAdapter) Disable_device(ctx context.Context, device *voltha.Device) error { // nolint
	go func() {
		if d := oltA.getDevice(ctx, device.Id); d == nil {
			logger.Fatalf("device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to disable
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, voltha.OperStatus_UNKNOWN); err != nil {
			logger.Warnw("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		//Update the device operational state
		cloned.OperStatus = voltha.OperStatus_UNKNOWN
		// The device is still reachable after it has been disabled, so the connection status should not be changed.

		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
			return
		}

		oltA.updateDevice(ctx, cloned)

		// Tell the Core that all child devices have been disabled (by default it's an action already taken by the Core
		if err := oltA.coreProxy.ChildDevicesLost(context.TODO(), cloned.Id); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw("lost-notif-of-child-devices-failed", log.Fields{"deviceId": device.Id, "error": err})
		}
	}()
	return nil
}

// Reenable_device reenables device
func (oltA *OLTAdapter) Reenable_device(ctx context.Context, device *voltha.Device) error { // nolint
	go func() {
		if d := oltA.getDevice(ctx, device.Id); d == nil {
			logger.Fatalf("device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to enable
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), cloned.Id, voltha.OperStatus_ACTIVE); err != nil {
			logger.Fatalf("updating-ports-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		//Update the device state
		cloned.OperStatus = voltha.OperStatus_ACTIVE

		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), cloned.Id, cloned.ConnectStatus, cloned.OperStatus); err != nil {
			logger.Fatalf("device-state-update-failed", log.Fields{"deviceId": device.Id, "error": err})
		}

		// Tell the Core that all child devices have been enabled
		if err := oltA.coreProxy.ChildDevicesDetected(context.TODO(), cloned.Id); err != nil {
			logger.Fatalf("detection-notif-of-child-devices-failed", log.Fields{"deviceId": device.Id, "error": err})
		}
	}()
	return nil
}

// Enable_port -
func (oltA *OLTAdapter) Enable_port(ctx context.Context, deviceId string, Port *voltha.Port) error { //nolint
	go func() {

		if Port.Type == voltha.Port_PON_OLT {
			if err := oltA.coreProxy.PortStateUpdate(context.TODO(), deviceId, voltha.Port_PON_OLT, Port.PortNo, voltha.OperStatus_ACTIVE); err != nil {
				logger.Fatalf("updating-ports-failed", log.Fields{"device-id": deviceId, "error": err})
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
				logger.Warnw("updating-ports-failed", log.Fields{"device-id": deviceId, "error": err})
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
	logger.Infow("reboot-device", log.Fields{"deviceId": device.Id})

	go func() {
		if err := oltA.coreProxy.DeviceStateUpdate(context.TODO(), device.Id, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN); err != nil {
			logger.Fatalf("device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
		}
		if err := oltA.coreProxy.PortsStateUpdate(context.TODO(), device.Id, voltha.OperStatus_UNKNOWN); err != nil {
			// Not an error as the previous command will start the process of clearing the OLT
			logger.Infow("port-update-failed", log.Fields{"device-id": device.Id, "error": err})
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
