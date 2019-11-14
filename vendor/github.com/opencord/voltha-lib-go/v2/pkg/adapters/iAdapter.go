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
package adapters

import (
	ic "github.com/opencord/voltha-protos/v2/go/inter_container"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

//IAdapter represents the set of APIs a voltha adapter has to support.
type IAdapter interface {
	AdapterDescriptor() error
	DeviceTypes() (*voltha.DeviceTypes, error)
	Health() (*voltha.HealthStatus, error)
	AdoptDevice(device *voltha.Device) error
	ReconcileDevice(device *voltha.Device) error
	AbandonDevice(device *voltha.Device) error
	DisableDevice(device *voltha.Device) error
	ReenableDevice(device *voltha.Device) error
	RebootDevice(device *voltha.Device) error
	SelfTestDevice(device *voltha.Device) error
	DeleteDevice(device *voltha.Device) error
	GetDeviceDetails(device *voltha.Device) error
	UpdateFlowsBulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) error
	UpdateFlowsIncrementally(device *voltha.Device, flows *openflow_13.FlowChanges, groups *openflow_13.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error
	UpdatePmConfig(device *voltha.Device, pm_configs *voltha.PmConfigs) error
	ReceivePacketOut(deviceId string, egress_port_no int, msg *openflow_13.OfpPacketOut) error
	SuppressAlarm(filter *voltha.AlarmFilter) error
	UnsuppressAlarm(filter *voltha.AlarmFilter) error
	GetOfpDeviceInfo(device *voltha.Device) (*ic.SwitchCapability, error)
	GetOfpPortInfo(device *voltha.Device, port_no int64) (*ic.PortCapability, error)
	ProcessInterAdapterMessage(msg *ic.InterAdapterMessage) error
	DownloadImage(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	GetImageDownloadStatus(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	CancelImageDownload(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	ActivateImageUpdate(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	RevertImageUpdate(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
}
