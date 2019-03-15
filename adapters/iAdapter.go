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
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
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
	Gelete_device(device *voltha.Device) error
	Get_device_details(device *voltha.Device) error
	Update_flows_bulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups) error
	Update_flows_incrementally(device *voltha.Device, flows *openflow_13.FlowChanges, groups *openflow_13.FlowGroupChanges) error
	Update_pm_config(device *voltha.Device, pm_configs *voltha.PmConfigs) error
	Receive_packet_out(device *voltha.Device, egress_port_no int, msg openflow_13.PacketOut) error
	Suppress_alarm(filter *voltha.AlarmFilter) error
	Unsuppress_alarm(filter *voltha.AlarmFilter) error
	Get_ofp_device_info(device *voltha.Device) (*ic.SwitchCapability, error)
	Get_ofp_port_info(device *voltha.Device, port_no int64) (*ic.PortCapability, error)
	Process_inter_adapter_message(msg *ic.InterAdapterMessage) error
	Download_image(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Get_image_download_status(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Cancel_image_download(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Activate_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Revert_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
}
