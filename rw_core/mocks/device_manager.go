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

type DeviceManager struct {
}

func (dm *DeviceManager) GetDevice(deviceId string) (*voltha.Device, error) {
	return nil, nil
}
func (dm *DeviceManager) IsRootDevice(deviceId string) (bool, error) {
	return false, nil
}

func (dm *DeviceManager) NotifyInvalidTransition(pcDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) SetAdminStateToEnable(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) CreateLogicalDevice(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) SetupUNILogicalPorts(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) DisableAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) DeleteLogicalDevice(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) DeleteLogicalPorts(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) DeleteAllChildDevices(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) RunPostDeviceDelete(cDevice *voltha.Device) error {
	return nil
}

func (dm *DeviceManager) ListDevices() (*voltha.Devices, error) {
	return nil, nil
}

func (dm *DeviceManager) ListDeviceIds() (*voltha.IDs, error) {
	return nil, nil
}

func (dm *DeviceManager) ReconcileDevices(ctx context.Context, ids *voltha.IDs, ch chan interface{}) {
}

func (dm *DeviceManager) CreateDevice(ctx context.Context, device *voltha.Device, ch chan interface{}) {
}

func (dm *DeviceManager) EnableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

func (dm *DeviceManager) DisableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

func (dm *DeviceManager) RebootDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

func (dm *DeviceManager) DeleteDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
}

func (dm *DeviceManager) StopManagingDevice(id string) {
}

func (dm *DeviceManager) DownloadImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

func (dm *DeviceManager) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

func (dm *DeviceManager) ActivateImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

func (dm *DeviceManager) RevertImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

func (dm *DeviceManager) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
}

func (dm *DeviceManager) UpdateImageDownload(deviceId string, img *voltha.ImageDownload) error {
	return nil
}

func (dm *DeviceManager) SimulateAlarm(ctx context.Context, simulatereq *voltha.SimulateAlarmRequest, ch chan interface{}) {
}

func (dm *DeviceManager) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}

func (dm *DeviceManager) ListImageDownloads(ctx context.Context, deviceId string) (*voltha.ImageDownloads, error) {
	return nil, nil
}

func (dm *DeviceManager) UpdatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs, ch chan interface{}) {
}

func (dm *DeviceManager) ListPmConfigs(ctx context.Context, deviceId string) (*voltha.PmConfigs, error) {
	return nil, nil
}

func (dm *DeviceManager) DeletePeerPorts(fromDeviceId string, deviceId string) error {
	return nil
}

func (dm *DeviceManager) ProcessTransition(previous *voltha.Device, current *voltha.Device) error {
	return nil
}
