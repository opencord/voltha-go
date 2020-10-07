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
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/opencord/voltha-lib-go/v4/pkg/adapters/adapterif"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	of "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
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
	coreProxy        adapterif.CoreProxy
	flows            map[uint64]*voltha.OfpFlowStats
	flowLock         sync.RWMutex
	devices          map[string]*voltha.Device
	deviceLock       sync.RWMutex
	failFlowAdd      bool
	failFlowDelete   bool
	failDeleteDevice bool
}

// NewAdapter creates adapter instance
func NewAdapter(cp adapterif.CoreProxy) *Adapter {
	return &Adapter{
		flows:     map[uint64]*voltha.OfpFlowStats{},
		devices:   map[string]*voltha.Device{},
		coreProxy: cp,
	}
}

func (ta *Adapter) storeDevice(d *voltha.Device) {
	ta.deviceLock.Lock()
	defer ta.deviceLock.Unlock()
	if d != nil {
		ta.devices[d.Id] = d
	}
}

func (ta *Adapter) getDevice(id string) *voltha.Device {
	ta.deviceLock.RLock()
	defer ta.deviceLock.RUnlock()
	return ta.devices[id]
}

func (ta *Adapter) updateDevice(d *voltha.Device) {
	ta.storeDevice(d)
}

// Adapter_descriptor -
func (ta *Adapter) Adapter_descriptor(ctx context.Context) error { // nolint
	return nil
}

// Device_types -
func (ta *Adapter) Device_types(ctx context.Context) (*voltha.DeviceTypes, error) { // nolint
	return nil, nil
}

// Health -
func (ta *Adapter) Health(ctx context.Context) (*voltha.HealthStatus, error) {
	return nil, nil
}

// Adopt_device -
func (ta *Adapter) Adopt_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Reconcile_device -
func (ta *Adapter) Reconcile_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Abandon_device -
func (ta *Adapter) Abandon_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Disable_device -
func (ta *Adapter) Disable_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Reenable_device -
func (ta *Adapter) Reenable_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Reboot_device -
func (ta *Adapter) Reboot_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Self_test_device -
func (ta *Adapter) Self_test_device(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Delete_device -
func (ta *Adapter) Delete_device(ctx context.Context, device *voltha.Device) error { // nolint
	if ta.failDeleteDevice {
		return fmt.Errorf("delete-device-failure")
	}
	return nil
}

// Get_device_details -
func (ta *Adapter) Get_device_details(ctx context.Context, device *voltha.Device) error { // nolint
	return nil
}

// Update_flows_bulk -
func (ta *Adapter) Update_flows_bulk(ctx context.Context, device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) error { // nolint
	return nil
}

// Update_flows_incrementally mocks the incremental flow update
func (ta *Adapter) Update_flows_incrementally(ctx context.Context, device *voltha.Device, flows *of.FlowChanges, groups *of.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error { // nolint
	ta.flowLock.Lock()
	defer ta.flowLock.Unlock()

	if flows.ToAdd != nil && len(flows.ToAdd.Items) > 0 {
		if ta.failFlowAdd {
			return fmt.Errorf("flow-add-error")
		}
		for _, f := range flows.ToAdd.Items {
			ta.flows[f.Id] = f
		}
	}
	if flows.ToRemove != nil && len(flows.ToRemove.Items) > 0 {
		if ta.failFlowDelete {
			return fmt.Errorf("flow-delete-error")
		}
		for _, f := range flows.ToRemove.Items {
			delete(ta.flows, f.Id)
		}
	}
	return nil
}

// Update_pm_config -
func (ta *Adapter) Update_pm_config(ctx context.Context, device *voltha.Device, pmConfigs *voltha.PmConfigs) error { // nolint
	return nil
}

// Receive_packet_out -
func (ta *Adapter) Receive_packet_out(ctx context.Context, deviceID string, egressPortNo int, msg *of.OfpPacketOut) error { // nolint
	return nil
}

// Suppress_event -
func (ta *Adapter) Suppress_event(ctx context.Context, filter *voltha.EventFilter) error { // nolint
	return nil
}

// Unsuppress_event -
func (ta *Adapter) Unsuppress_event(ctx context.Context, filter *voltha.EventFilter) error { // nolint
	return nil
}

// Get_ofp_device_info -
func (ta *Adapter) Get_ofp_device_info(ctx context.Context, device *voltha.Device) (*ic.SwitchCapability, error) { // nolint
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

// Process_inter_adapter_message -
func (ta *Adapter) Process_inter_adapter_message(ctx context.Context, msg *ic.InterAdapterMessage) error { // nolint
	return nil
}

// Download_image -
func (ta *Adapter) Download_image(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Get_image_download_status -
func (ta *Adapter) Get_image_download_status(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Cancel_image_download -
func (ta *Adapter) Cancel_image_download(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Activate_image_update -
func (ta *Adapter) Activate_image_update(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Revert_image_update -
func (ta *Adapter) Revert_image_update(ctx context.Context, device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) { // nolint
	return nil, nil
}

// Enable_port -
func (ta *Adapter) Enable_port(ctx context.Context, deviceId string, port *voltha.Port) error { //nolint
	return nil
}

// Disable_port -
func (ta *Adapter) Disable_port(ctx context.Context, deviceId string, port *voltha.Port) error { //nolint
	return nil
}

// Child_device_lost -
func (ta *Adapter) Child_device_lost(ctx context.Context, pDeviceID string, pPortNo uint32, onuID uint32) error { //nolint
	return nil
}

// Start_omci_test
func (ta *Adapter) Start_omci_test(ctx context.Context, device *voltha.Device, request *voltha.OmciTestRequest) (*voltha.TestResponse, error) { //nolint
	return nil, nil
}

func (ta *Adapter) Get_ext_value(ctx context.Context, deviceId string, device *voltha.Device, valueflag voltha.ValueType_Type) (*voltha.ReturnValues, error) { //nolint
	return nil, nil
}

// GetFlowCount returns the total number of flows presently under this adapter
func (ta *Adapter) GetFlowCount() int {
	ta.flowLock.RLock()
	defer ta.flowLock.RUnlock()

	return len(ta.flows)
}

// ClearFlows removes all flows in this adapter
func (ta *Adapter) ClearFlows() {
	ta.flowLock.Lock()
	defer ta.flowLock.Unlock()

	ta.flows = map[uint64]*voltha.OfpFlowStats{}
}

// SetFlowAction sets the adapter action on addition and deletion of flows
func (ta *Adapter) SetFlowAction(failFlowAdd, failFlowDelete bool) {
	ta.failFlowAdd = failFlowAdd
	ta.failFlowDelete = failFlowDelete
}

// SetDeleteAction sets the adapter action on delete device
func (ta *Adapter) SetDeleteAction(failDeleteDevice bool) {
	ta.failDeleteDevice = failDeleteDevice
}
