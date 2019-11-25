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
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

// DeviceManager represents a generic device manager
type DeviceManager interface {
	ReconcileDevices(context.Context, *voltha.IDs, chan interface{})
	CreateDevice(context.Context, *voltha.Device, chan interface{})
	EnableDevice(context.Context, *voltha.ID, chan interface{})
	DisableDevice(context.Context, *voltha.ID, chan interface{})
	RebootDevice(context.Context, *voltha.ID, chan interface{})
	DeleteDevice(context.Context, *voltha.ID, chan interface{})
	DownloadImage(context.Context, *voltha.ImageDownload, chan interface{})
	CancelImageDownload(context.Context, *voltha.ImageDownload, chan interface{})
	ActivateImage(context.Context, *voltha.ImageDownload, chan interface{})
	RevertImage(context.Context, *voltha.ImageDownload, chan interface{})
	GetImageDownloadStatus(context.Context, *voltha.ImageDownload, chan interface{})
	UpdateImageDownload(string, *voltha.ImageDownload) error
	GetImageDownload(context.Context, *voltha.ImageDownload) (*voltha.ImageDownload, error)
	ListImageDownloads(context.Context, string) (*voltha.ImageDownloads, error)
	UpdatePmConfigs(context.Context, *voltha.PmConfigs, chan interface{})
	ListPmConfigs(context.Context, string) (*voltha.PmConfigs, error)
	SimulateAlarm(context.Context, *voltha.SimulateAlarmRequest, chan interface{})
	GetDevice(context.Context, string) (*voltha.Device, error)
	ListDevices(context.Context) (*voltha.Devices, error)
	ListDeviceIds(context.Context) (*voltha.IDs, error)
	StopManagingDevice(string)
	IsRootDevice(string) (bool, error)
	NotifyInvalidTransition(*voltha.Device) error
	SetAdminStateToEnable(*voltha.Device) error
	CreateLogicalDevice(*voltha.Device) error
	SetupUNILogicalPorts(*voltha.Device) error
	DisableAllChildDevices(*voltha.Device) error
	DeleteLogicalDevice(*voltha.Device) error
	DeleteLogicalPorts(*voltha.Device) error
	DeleteAllChildDevices(*voltha.Device) error
	RunPostDeviceDelete(*voltha.Device) error
	DeletePeerPorts(string, string) error
	ProcessTransition(*voltha.Device, *voltha.Device) error
}
