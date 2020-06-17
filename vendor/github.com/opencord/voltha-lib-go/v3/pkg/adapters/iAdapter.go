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
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

//IAdapter represents the set of APIs a voltha adapter has to support.
type IAdapter interface {
	Adapter_descriptor() error
	Device_types() (*voltha.DeviceTypes, error)
	Health() (*voltha.HealthStatus, error)
	Adopt_device(device *voltha.Device) error
	Reconcile_device(device *voltha.Device) error
	Abandon_device(device *voltha.Device) error
	Disable_device(device *voltha.Device) error
	Reenable_device(device *voltha.Device) error
	Reboot_device(device *voltha.Device) error
	Self_test_device(device *voltha.Device) error
	Delete_device(device *voltha.Device) error
	Get_device_details(device *voltha.Device) error
	Update_flows_bulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) error
	Update_flows_incrementally(device *voltha.Device, flows *openflow_13.FlowChanges, groups *openflow_13.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error
	Update_pm_config(device *voltha.Device, pm_configs *voltha.PmConfigs) error
	Receive_packet_out(deviceId string, egress_port_no int, msg *openflow_13.OfpPacketOut) error
	Suppress_event(filter *voltha.EventFilter) error
	Unsuppress_event(filter *voltha.EventFilter) error
	Get_ofp_device_info(device *voltha.Device) (*ic.SwitchCapability, error)
	Process_inter_adapter_message(msg *ic.InterAdapterMessage) error
	Download_image(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Get_image_download_status(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Cancel_image_download(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Activate_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Revert_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Enable_port(deviceId string, port *voltha.Port) error
	Disable_port(deviceId string, port *voltha.Port) error
	Child_device_lost(parentDeviceId string, parentPortNo uint32, onuID uint32) error
	Start_omci_test(device *voltha.Device, request *voltha.OmciTestRequest) (*voltha.TestResponse, error)
	Get_ext_value(deviceId string, device *voltha.Device, valueflag voltha.ValueType_Type) (*voltha.ReturnValues, error)
}
