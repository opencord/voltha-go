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

package coreif

import (
	"context"

	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

// LogicalDeviceManager represent logical device manager related methods
type LogicalDeviceManager interface {
	GetLogicalPort(lPortID *voltha.LogicalPortId) (*voltha.LogicalPort, error)
	EnableLogicalPort(ctx context.Context, id *voltha.LogicalPortId, ch chan interface{})
	DisableLogicalPort(ctx context.Context, id *voltha.LogicalPortId, ch chan interface{})
	UpdateFlowTable(ctx context.Context, id string, flow *openflow_13.OfpFlowMod, ch chan interface{})
	UpdateMeterTable(ctx context.Context, id string, meter *openflow_13.OfpMeterMod, ch chan interface{})
	UpdateGroupTable(ctx context.Context, id string, groupMod *openflow_13.OfpGroupMod, ch chan interface{})
	GetLogicalDevice(id string) (*voltha.LogicalDevice, error)
	ListManagedLogicalDevices() (*voltha.LogicalDevices, error)
	ListLogicalDevices() (*voltha.LogicalDevices, error)
	ListLogicalDeviceFlows(ctx context.Context, id string) (*openflow_13.Flows, error)
	ListLogicalDeviceFlowGroups(ctx context.Context, id string) (*openflow_13.FlowGroups, error)
	ListLogicalDevicePorts(ctx context.Context, id string) (*voltha.LogicalPorts, error)
	ListLogicalDeviceMeters(ctx context.Context, id string) (*openflow_13.Meters, error)
	PacketOut(packet *openflow_13.PacketOut) error
}
