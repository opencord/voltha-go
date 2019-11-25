/*
 * Copyright 2018-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
Defines a DeviceManager Interface - Used for unit testing of the flow decomposer only at this
time.
*/
package coreIf

import (
	"context"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

type LogicalDeviceManager interface {
	CreateLogicalDevice(context.Context, *voltha.Device) (*string, error)
	GetLogicalPort(context.Context, *voltha.LogicalPortId) (*voltha.LogicalPort, error)
	EnableLogicalPort(context.Context, *voltha.LogicalPortId, chan interface{})
	DisableLogicalPort(context.Context, *voltha.LogicalPortId, chan interface{})
	UpdateFlowTable(context.Context, string, *openflow_13.OfpFlowMod, chan interface{})
	UpdateMeterTable(context.Context, string, *openflow_13.OfpMeterMod, chan interface{})
	UpdateGroupTable(context.Context, string, *openflow_13.OfpGroupMod, chan interface{})
	GetLogicalDevice(context.Context, string) (*voltha.LogicalDevice, error)
	ListManagedLogicalDevices() (*voltha.LogicalDevices, error)
	ListLogicalDevices(context.Context) (*voltha.LogicalDevices, error)
	ListLogicalDeviceFlows(context.Context, string) (*openflow_13.Flows, error)
	ListLogicalDeviceFlowGroups(context.Context, string) (*openflow_13.FlowGroups, error)
	ListLogicalDevicePorts(context.Context, string) (*voltha.LogicalPorts, error)
	ListLogicalDeviceMeters(context.Context, string) (*openflow_13.Meters, error)
	PacketOut(*openflow_13.PacketOut) error
}
