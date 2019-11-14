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

	"github.com/opencord/voltha-lib-go/v2/pkg/adapters/adapterif"
	ic "github.com/opencord/voltha-protos/v2/go/inter_container"
	of "github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
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

// AdapterDescriptor -
func (ta *Adapter) AdapterDescriptor() error {
	return nil
}

// DeviceTypes -
func (ta *Adapter) DeviceTypes() (*voltha.DeviceTypes, error) {
	return nil, nil
}

// Health -
func (ta *Adapter) Health() (*voltha.HealthStatus, error) {
	return nil, nil
}

// AdoptDevice -
func (ta *Adapter) AdoptDevice(device *voltha.Device) error {
	return nil
}

// ReconcileDevice -
func (ta *Adapter) ReconcileDevice(device *voltha.Device) error {
	return nil
}

// AbandonDevice -
func (ta *Adapter) AbandonDevice(device *voltha.Device) error {
	return nil
}

// DisableDevice -
func (ta *Adapter) DisableDevice(device *voltha.Device) error {
	return nil
}

// ReenableDevice -
func (ta *Adapter) ReenableDevice(device *voltha.Device) error {
	return nil
}

// RebootDevice -
func (ta *Adapter) RebootDevice(device *voltha.Device) error {
	return nil
}

// SelfTestDevice -
func (ta *Adapter) SelfTestDevice(device *voltha.Device) error {
	return nil
}

// DeleteDevice -
func (ta *Adapter) DeleteDevice(device *voltha.Device) error {
	return nil
}

// GetDeviceDetails -
func (ta *Adapter) GetDeviceDetails(device *voltha.Device) error {
	return nil
}

// UpdateFlowsBulk -
func (ta *Adapter) UpdateFlowsBulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) error {
	return nil
}

// UpdateFlowsIncrementally -
func (ta *Adapter) UpdateFlowsIncrementally(device *voltha.Device, flows *of.FlowChanges, groups *of.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) error {
	return nil
}

// UpdatePmConfig -
func (ta *Adapter) UpdatePmConfig(device *voltha.Device, pmConfigs *voltha.PmConfigs) error {
	return nil
}

// ReceivePacketOut -
func (ta *Adapter) ReceivePacketOut(deviceID string, egressPortNo int, msg *of.OfpPacketOut) error {
	return nil
}

// SuppressAlarm -
func (ta *Adapter) SuppressAlarm(filter *voltha.AlarmFilter) error {
	return nil
}

// UnsuppressAlarm -
func (ta *Adapter) UnsuppressAlarm(filter *voltha.AlarmFilter) error {
	return nil
}

// GetOfpDeviceInfo -
func (ta *Adapter) GetOfpDeviceInfo(device *voltha.Device) (*ic.SwitchCapability, error) {
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

// GetOfpPortInfo -
func (ta *Adapter) GetOfpPortInfo(device *voltha.Device, portNo int64) (*ic.PortCapability, error) {
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

// ProcessInterAdapterMessage -
func (ta *Adapter) ProcessInterAdapterMessage(msg *ic.InterAdapterMessage) error {
	return nil
}

// DownloadImage -
func (ta *Adapter) DownloadImage(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}

// GetImageDownloadStatus -
func (ta *Adapter) GetImageDownloadStatus(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}

// CancelImageDownload -
func (ta *Adapter) CancelImageDownload(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}

// ActivateImageUpdate -
func (ta *Adapter) ActivateImageUpdate(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}

// RevertImageUpdate -
func (ta *Adapter) RevertImageUpdate(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, nil
}
