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

import "github.com/opencord/voltha-protos/go/voltha"

// DeviceManager represents a generic device manager
type DeviceManager interface {
	GetDevice(string) (*voltha.Device, error)
	IsRootDevice(string) (bool, error)
	NotifyInvalidTransition(*voltha.Device) error
	SetAdminStateToEnable(*voltha.Device) error
	CreateLogicalDevice(*voltha.Device) error
	SetupUNILogicalPorts(*voltha.Device) error
	DisableAllChildDevices(cDevice *voltha.Device) error
	DeleteLogicalDevice(cDevice *voltha.Device) error
	DeleteLogicalPorts(cDevice *voltha.Device) error
	DeleteAllChildDevices(cDevice *voltha.Device) error
	RunPostDeviceDelete(cDevice *voltha.Device) error
}
