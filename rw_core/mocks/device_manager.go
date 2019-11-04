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

	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// DeviceManager represents a mock of a device manager that implements the coreif/DeviceManager interface.  It provides
// default behaviors. For non-default behavior, another implementation of the coreif/DeviceManager interface must be
// used.
type DeviceManager struct {
}

// GetDevice -
func (dm *DeviceManager) GetDevice(ctx context.Context, deviceID string) (*voltha.Device, error) {
	return nil, nil
}

// IsRootDevice -
func (dm *DeviceManager) IsRootDevice(deviceID string) (bool, error) {
	return false, nil
}

// NotifyInvalidTransition -
func (dm *DeviceManager) NotifyInvalidTransition(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// CreateLogicalDevice -
func (dm *DeviceManager) CreateLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// SetupUNILogicalPorts -
func (dm *DeviceManager) SetupUNILogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DisableAllChildDevices -
func (dm *DeviceManager) DisableAllChildDevices(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DeleteLogicalDevice -
func (dm *DeviceManager) DeleteLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DeleteLogicalPorts -
func (dm *DeviceManager) DeleteLogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DeleteAllChildDevices -
func (dm *DeviceManager) DeleteAllChildDevices(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DeleteAllUNILogicalPorts -
func (dm *DeviceManager) DeleteAllUNILogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DeleteAllLogicalPorts -
func (dm *DeviceManager) DeleteAllLogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// DeleteAllDeviceFlows -
func (dm *DeviceManager) DeleteAllDeviceFlows(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// RunPostDeviceDelete -
func (dm *DeviceManager) RunPostDeviceDelete(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}

// childDeviceLost -
func (dm *DeviceManager) ChildDeviceLost(ctx context.Context, cDevice *voltha.Device) error {
	return nil
}
