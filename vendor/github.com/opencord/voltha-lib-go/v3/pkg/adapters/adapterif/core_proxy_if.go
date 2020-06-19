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

package adapterif

import (
	"context"

	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// CoreProxy interface for voltha-go coreproxy.
type CoreProxy interface {
	UpdateCoreReference(deviceID string, coreReference string)
	DeleteCoreReference(deviceID string)
	RegisterAdapter(ctx context.Context, adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) error
	DeviceUpdate(ctx context.Context, device *voltha.Device) error
	PortCreated(ctx context.Context, deviceID string, port *voltha.Port) error
	PortsStateUpdate(ctx context.Context, deviceID string, portTypeFilter uint32, operStatus voltha.OperStatus_Types) error
	DeleteAllPorts(ctx context.Context, deviceID string) error
	GetDevicePort(ctx context.Context, deviceID string, portNo uint32) (*voltha.Port, error)
	ListDevicePorts(ctx context.Context, deviceID string) ([]*voltha.Port, error)
	DeviceStateUpdate(ctx context.Context, deviceID string,
		connStatus voltha.ConnectStatus_Types, operStatus voltha.OperStatus_Types) error

	DevicePMConfigUpdate(ctx context.Context, pmConfigs *voltha.PmConfigs) error
	ChildDeviceDetected(ctx context.Context, parentDeviceID string, parentPortNo int,
		childDeviceType string, channelID int, vendorID string, serialNumber string, onuID int64) (*voltha.Device, error)

	ChildDevicesLost(ctx context.Context, parentDeviceID string) error
	ChildDevicesDetected(ctx context.Context, parentDeviceID string) error
	GetDevice(ctx context.Context, parentDeviceID string, deviceID string) (*voltha.Device, error)
	GetChildDevice(ctx context.Context, parentDeviceID string, kwargs map[string]interface{}) (*voltha.Device, error)
	GetChildDevices(ctx context.Context, parentDeviceID string) (*voltha.Devices, error)
	SendPacketIn(ctx context.Context, deviceID string, port uint32, pktPayload []byte) error
	DeviceReasonUpdate(ctx context.Context, deviceID string, deviceReason string) error
	PortStateUpdate(ctx context.Context, deviceID string, pType voltha.Port_PortType, portNo uint32,
		operStatus voltha.OperStatus_Types) error
}
