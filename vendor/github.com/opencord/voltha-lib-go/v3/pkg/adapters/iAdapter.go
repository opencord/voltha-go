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
	"context"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

//IAdapter represents the set of APIs a voltha adapter has to support.
type IAdapter interface {
	Adapter_descriptor(ctx context.Context) error
	Device_types(ctx context.Context) (*voltha.DeviceTypes, error)
	Health(ctx context.Context) (*voltha.HealthStatus, error)
	Adopt_device(ctx context.Context, device *voltha.Device) error
	Reconcile_device(ctx context.Context, device *voltha.Device) error
	Abandon_device(ctx context.Context, device *voltha.Device) error
	Disable_device(ctx context.Context, device *voltha.Device) error
	Reenable_device(ctx context.Context, device *voltha.Device) error
	Reboot_device(ctx context.Context, device *voltha.Device) error
	Self_test_device(ctx context.Context, device *voltha.Device) error
	Delete_device(ctx context.Context, device *voltha.Device) error
	Get_device_details(ctx context.Context, device *voltha.Device) error
	Update_flows_bulk(ctx context.Context, device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) error
	Update_flows_incrementally(ctx context.Context, device *voltha.Device, flows *openflow_13.FlowChanges, groups *openflow_13.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error
	Update_pm_config(ctx context.Context, device *voltha.Device, pm_configs *voltha.PmConfigs) error
	Receive_packet_out(ctx context.Context, deviceId string, egress_port_no int, msg *openflow_13.OfpPacketOut) error
	Suppress_event(ctx context.Context, filter *voltha.EventFilter) error
	Unsuppress_event(ctx context.Context, filter *voltha.EventFilter) error
	Get_ofp_device_info(ctx context.Context, device *voltha.Device) (*ic.SwitchCapability, error)
	Process_inter_adapter_message(ctx context.Context, msg *ic.InterAdapterMessage) error
	Download_image(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Get_image_download_status(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Cancel_image_download(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Activate_image_update(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Revert_image_update(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error)
	Enable_port(ctx context.Context, deviceId string, port *voltha.Port) error
	Disable_port(ctx context.Context, deviceId string, port *voltha.Port) error
	Child_device_lost(ctx context.Context, parentDeviceId string, parentPortNo uint32, onuID uint32) error
	Start_omci_test(ctx context.Context, device *voltha.Device, request *voltha.OmciTestRequest) (*voltha.TestResponse, error)
	Get_ext_value(ctx context.Context, deviceId string, device *voltha.Device, valueflag voltha.ValueType_Type) (*voltha.ReturnValues, error)
}
