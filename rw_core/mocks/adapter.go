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
	"strconv"
	"strings"
	"sync"

	"github.com/opencord/voltha-lib-go/v3/pkg/adapters/adapterif"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	of "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func macAddressToUint32Array(mac string) []uint32 {
	slist := strings.Split(mac, ":")
	result := make([]uint32, len(slist))
	var err error
	var tmp int64
	for index, val := range slist {
		if tmp, err = strconv.ParseInt(val, 16, 32); err != nil {
			return []uint32{1, 2, 3, 4, 5, 6}
		}
		result[index] = uint32(tmp)
	}
	return result
}

// Adapter represents adapter attributes
type Adapter struct {
	coreProxy adapterif.CoreProxy
	devices   sync.Map
}

// NewAdapter creates adapter instance
func NewAdapter(cp adapterif.CoreProxy) *Adapter {
	return &Adapter{
		coreProxy: cp,
	}
}

func (ta *Adapter) storeDevice(d *voltha.Device) {
	if d != nil {
		ta.devices.Store(d.Id, d)
	}
}

func (ta *Adapter) getDevice(id string) *voltha.Device {
	if val, ok := ta.devices.Load(id); ok && val != nil {
		if device, ok := val.(*voltha.Device); ok {
			return device
		}
	}
	return nil
}

func (ta *Adapter) updateDevice(d *voltha.Device) error {
	if d != nil {
		if _, ok := ta.devices.LoadOrStore(d.Id, d); !ok {
			return status.Errorf(codes.Internal, "error updating device %s", d.Id)
		}
	}
	return nil
}

// Adapter_descriptor -
func (ta *Adapter) Adapter_descriptor() error { // nolint
	return nil
}

// Device_types -
func (ta *Adapter) Device_types() (*voltha.DeviceTypes, error) { // nolint
	return nil, nil
}

// Health -
func (ta *Adapter) Health() (*voltha.HealthStatus, error) {
	return nil, nil
}

// Adopt_device -
func (ta *Adapter) Adopt_device(device *voltha.Device) error { // nolint
	return nil
}

// Reconcile_device -
func (ta *Adapter) Reconcile_device(device *voltha.Device) error { // nolint
	return nil
}

// Abandon_device -
func (ta *Adapter) Abandon_device(device *voltha.Device) error { // nolint
	return nil
}

// Disable_device -
func (ta *Adapter) Disable_device(device *voltha.Device) error { // nolint
	return nil
}

// Reenable_device -
func (ta *Adapter) Reenable_device(device *voltha.Device) error { // nolint
	return nil
}

// Reboot_device -
func (ta *Adapter) Reboot_device(device *voltha.Device) error { // nolint
	return nil
}

// Self_test_device -
func (ta *Adapter) Self_test_device(device *voltha.Device) error { // nolint
	return nil
}

// Delete_device -
func (ta *Adapter) Delete_device(device *voltha.Device) error { // nolint
	return nil
}

// Get_device_details -
func (ta *Adapter) Get_device_details(device *voltha.Device) error { // nolint
	return nil
}

// Update_flows_bulk -
func (ta *Adapter) Update_flows_bulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) error { // nolint
	return nil
}

// Update_flows_incrementally -
func (ta *Adapter) Update_flows_incrementally(device *voltha.Device, flows *of.FlowChanges, groups *of.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error { // nolint
	return nil
}

// Update_pm_config -
func (ta *Adapter) Update_pm_config(device *voltha.Device, pmConfigs *voltha.PmConfigs) error { // nolint
	return nil
}

// Receive_packet_out -
func (ta *Adapter) Receive_packet_out(deviceID string, egressPortNo int, msg *of.OfpPacketOut) error { // nolint
	return nil
}

// Suppress_event -
func (ta *Adapter) Suppress_event(filter *voltha.EventFilter) error { // nolint
	return nil
}

// Unsuppress_event -
func (ta *Adapter) Unsuppress_event(filter *voltha.EventFilter) error { // nolint
	return nil
}

// Get_ofp_device_info -
func (ta *Adapter) Get_ofp_device_info(device *voltha.Device) (*ic.SwitchCapability, error) { // nolint
	return &ic.SwitchCapability{
		Desc: &of.OfpDesc{
			HwDesc:    "adapter_mock",
			SwDesc:    "adapter_mock",
			SerialNum: "000000000",
		},
		SwitchFeatures: &of.OfpSwitchFeatures{
			NBuffers: 256,
			NTables:  2,
			Capabilities: uint32(of.OfpCapabilities_OFPC_FLOW_STATS |
				of.OfpCapabilities_OFPC_TABLE_STATS |
				of.OfpCapabilities_OFPC_PORT_STATS |
				of.OfpCapabilities_OFPC_GROUP_STATS),
		},
	}, nil
}

// Get_ofp_port_info -
func (ta *Adapter) Get_ofp_port_info(device *voltha.Device, portNo int64) (*ic.PortCapability, error) { // nolint
	capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
	return &ic.PortCapability{
		Port: &voltha.LogicalPort{
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array("11:11:33:44:55:66"),
				Config:     0,
				State:      uint32(of.OfpPortState_OFPPS_LIVE),
				Curr:       capability,
				Advertised: capability,
				Peer:       capability,
				CurrSpeed:  uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
				MaxSpeed:   uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
			},
			DeviceId:     device.Id,
			DevicePortNo: uint32(portNo),
		},
	}, nil
}

// Process_inter_adapter_message -
func (ta *Adapter) Process_inter_adapter_message(msg *ic.InterAdapterMessage) error { // nolint
	return nil
}

// Download_image -
func (ta *Adapter) Download_image(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Get_image_download_status -
func (ta *Adapter) Get_image_download_status(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Cancel_image_download -
func (ta *Adapter) Cancel_image_download(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Activate_image_update -
func (ta *Adapter) Activate_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Revert_image_update -
func (ta *Adapter) Revert_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Enable_port -
func (ta *Adapter) Enable_port(deviceId string, port *voltha.Port) error { //nolint
	return nil
}

// Disable_port -
func (ta *Adapter) Disable_port(deviceId string, port *voltha.Port) error { //nolint
	return nil
}

func (ta *Adapter) Child_device_lost(pDeviceID string, pPortNo uint32, onuID uint32) error {
	return nil
}
