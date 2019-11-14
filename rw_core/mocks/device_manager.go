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

	"github.com/opencord/voltha-protos/v2/go/voltha"
)

// DeviceManager -
type DeviceManager struct {
}

// GetDevice -
func (dm *DeviceManager) GetDevice(deviceID string) (*voltha.Device, error) {
	return nil, nil
}

// IsRootDevice -
func (dm *DeviceManager) IsRootDevice(deviceID string) (bool, error) {
	return false, nil
}

// NotifyInvalidTransition -
func (dm *DeviceManager) NotifyInvalidTransition(pcDevice *voltha.Device) error {
	return nil
}

// SetAdminStateToEnable -
func (dm *DeviceManager) SetAdminStateToEnable(cDevice *voltha.Device) error {
	return nil
}

// CreateLogicalDevice -
func (dm *DeviceManager) CreateLogicalDevice(cDevice *voltha.Device) error {
	return nil
}

// SetupUNILogicalPorts -
func (dm *DeviceManager) SetupUNILogicalPorts(cDevice *voltha.Device) error {
	return nil
}

// DisableAllChildDevices -
func (dm *DeviceManager) DisableAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

// DeleteLogicalDevice -
func (dm *DeviceManager) DeleteLogicalDevice(cDevice *voltha.Device) error {
	return nil
}

// DeleteLogicalPorts -
func (dm *DeviceManager) DeleteLogicalPorts(cDevice *voltha.Device) error {
	return nil
}

// DeleteAllChildDevices -
func (dm *DeviceManager) DeleteAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

// RunPostDeviceDelete -
func (dm *DeviceManager) RunPostDeviceDelete(cDevice *voltha.Device) error {
	return nil
}

// ListDevices -
func (dm *DeviceManager) ListDevices() (*voltha.Devices, error) {
	return nil, nil
}

// ListDeviceIds -
func (dm *DeviceManager) ListDeviceIds() (*voltha.IDs, error) {
	return nil, nil
}

// ReconcileDevices -
func (dm *DeviceManager) ReconcileDevices(ctx context.Context, ids *voltha.IDs, ch chan interface{}) {
}

// CreateDevice -
func (dm *DeviceManager) CreateDevice(ctx context.Context, device *voltha.Device, ch chan interface{}) {
}

// EnableDevice -
func (dm *DeviceManager) EnableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

// DisableDevice -
func (dm *DeviceManager) DisableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

// RebootDevice -
func (dm *DeviceManager) RebootDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

// DeleteDevice -
func (dm *DeviceManager) DeleteDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

// StopManagingDevice -
func (dm *DeviceManager) StopManagingDevice(id string) {
}

// DownloadImage -
func (dm *DeviceManager) DownloadImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

// CancelImageDownload -
func (dm *DeviceManager) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

// ActivateImage -
func (dm *DeviceManager) ActivateImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

// RevertImage -
func (dm *DeviceManager) RevertImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

// GetImageDownloadStatus -
func (dm *DeviceManager) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

// UpdateImageDownload -
func (dm *DeviceManager) UpdateImageDownload(deviceID string, img *voltha.ImageDownload) error {
	return nil
}

// SimulateAlarm -
func (dm *DeviceManager) SimulateAlarm(ctx context.Context, simulatereq *voltha.SimulateAlarmRequest, ch chan interface{}) {
}

// GetImageDownload -
func (dm *DeviceManager) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}

// ListImageDownloads -
func (dm *DeviceManager) ListImageDownloads(ctx context.Context, deviceID string) (*voltha.ImageDownloads, error) {
	return nil, nil
}

// UpdatePmConfigs -
func (dm *DeviceManager) UpdatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs, ch chan interface{}) {
}

// ListPmConfigs -
func (dm *DeviceManager) ListPmConfigs(ctx context.Context, deviceID string) (*voltha.PmConfigs, error) {
	return nil, nil
}

// DeletePeerPorts -
func (dm *DeviceManager) DeletePeerPorts(fromDeviceID string, deviceID string) error {
	return nil
}

// ProcessTransition -
func (dm *DeviceManager) ProcessTransition(previous *voltha.Device, current *voltha.Device) error {
	return nil
}
