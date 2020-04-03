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

package remote

import (
	"context"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// AdapterProxy represents adapter proxy attributes
type AdapterProxy struct {
	deviceTopicRegistered bool
	corePairTopic         string
	kafkaICProxy          kafka.InterContainerProxy
}

// NewAdapterProxy will return adapter proxy instance
func NewAdapterProxy(kafkaProxy kafka.InterContainerProxy, corePairTopic string) *AdapterProxy {
	return &AdapterProxy{
		kafkaICProxy:          kafkaProxy,
		corePairTopic:         corePairTopic,
		deviceTopicRegistered: false,
	}
}

func (ap *AdapterProxy) getCoreTopic() kafka.Topic {
	return kafka.Topic{Name: ap.corePairTopic}
}

func (ap *AdapterProxy) getAdapterTopic(adapterName string) kafka.Topic {
	return kafka.Topic{Name: adapterName}
}

func (ap *AdapterProxy) sendRPC(ctx context.Context, rpc string, toTopic *kafka.Topic, replyToTopic *kafka.Topic,
	waitForResponse bool, deviceID string, kvArgs ...*kafka.KVArg) (chan *kafka.RpcResponse, error) {

	// Sent the request to kafka
	respChnl := ap.kafkaICProxy.InvokeAsyncRPC(ctx, rpc, toTopic, replyToTopic, waitForResponse, deviceID, kvArgs...)

	// Wait for first response which would indicate whether the request was successfully sent to kafka.
	firstResponse, ok := <-respChnl
	if !ok || firstResponse.MType != kafka.RpcSent {
		logger.Errorw("failure to request to kafka", log.Fields{"rpc": rpc, "device-id": deviceID, "error": firstResponse.Err})
		return nil, firstResponse.Err
	}
	// return the kafka channel for the caller to wait for the response of the RPC call
	return respChnl, nil
}

// AdoptDevice invokes adopt device rpc
func (ap *AdapterProxy) AdoptDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("AdoptDevice", log.Fields{"device-id": device.Id})
	rpc := "adopt_device"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	ap.deviceTopicRegistered = true
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// DisableDevice invokes disable device rpc
func (ap *AdapterProxy) DisableDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("DisableDevice", log.Fields{"device-id": device.Id})
	rpc := "disable_device"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// ReEnableDevice invokes reenable device rpc
func (ap *AdapterProxy) ReEnableDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("ReEnableDevice", log.Fields{"device-id": device.Id})
	rpc := "reenable_device"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// RebootDevice invokes reboot device rpc
func (ap *AdapterProxy) RebootDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("RebootDevice", log.Fields{"device-id": device.Id})
	rpc := "reboot_device"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// DeleteDevice invokes delete device rpc
func (ap *AdapterProxy) DeleteDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("DeleteDevice", log.Fields{"device-id": device.Id})
	rpc := "delete_device"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// GetOfpDeviceInfo invokes get ofp device info rpc
func (ap *AdapterProxy) GetOfpDeviceInfo(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("GetOfpDeviceInfo", log.Fields{"device-id": device.Id})
	rpc := "get_ofp_device_info"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// GetOfpPortInfo invokes get ofp port info rpc
func (ap *AdapterProxy) GetOfpPortInfo(ctx context.Context, device *voltha.Device, portNo uint32) (chan *kafka.RpcResponse, error) {
	logger.Debugw("GetOfpPortInfo", log.Fields{"device-id": device.Id, "port-no": portNo})
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "port_no", Value: &ic.IntType{Val: int64(portNo)}},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, "get_ofp_port_info", &toTopic, &replyToTopic, true, device.Id, args...)
}

// ReconcileDevice invokes reconcile device rpc
func (ap *AdapterProxy) ReconcileDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("ReconcileDevice", log.Fields{"device-id": device.Id})
	rpc := "reconcile_device"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// DownloadImage invokes download image rpc
func (ap *AdapterProxy) DownloadImage(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("DownloadImage", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "download_image"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// GetImageDownloadStatus invokes get image download status rpc
func (ap *AdapterProxy) GetImageDownloadStatus(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("GetImageDownloadStatus", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "get_image_download_status"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// CancelImageDownload invokes cancel image download rpc
func (ap *AdapterProxy) CancelImageDownload(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("CancelImageDownload", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "cancel_image_download"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// ActivateImageUpdate invokes activate image update rpc
func (ap *AdapterProxy) ActivateImageUpdate(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("ActivateImageUpdate", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "activate_image_update"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// RevertImageUpdate invokes revert image update rpc
func (ap *AdapterProxy) RevertImageUpdate(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("RevertImageUpdate", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "revert_image_update"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

func (ap *AdapterProxy) PacketOut(ctx context.Context, deviceType string, deviceID string, outPort uint32, packet *openflow_13.OfpPacketOut) (chan *kafka.RpcResponse, error) {
	logger.Debugw("PacketOut", log.Fields{"device-id": deviceID, "device-type": deviceType, "out-port": outPort})
	toTopic := ap.getAdapterTopic(deviceType)
	rpc := "receive_packet_out"
	args := []*kafka.KVArg{
		{Key: "deviceId", Value: &ic.StrType{Val: deviceID}},
		{Key: "outPort", Value: &ic.IntType{Val: int64(outPort)}},
		{Key: "packet", Value: packet},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, deviceID, args...)
}

// UpdateFlowsBulk invokes update flows bulk rpc
func (ap *AdapterProxy) UpdateFlowsBulk(ctx context.Context, device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) (chan *kafka.RpcResponse, error) {
	logger.Debugw("UpdateFlowsBulk", log.Fields{"device-id": device.Id, "flow-count": len(flows.Items), "group-count": len(groups.Items), "flow-metadata": flowMetadata})
	toTopic := ap.getAdapterTopic(device.Adapter)
	rpc := "update_flows_bulk"
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "flows", Value: flows},
		{Key: "groups", Value: groups},
		{Key: "flow_metadata", Value: flowMetadata},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(context.TODO(), rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// UpdateFlowsIncremental invokes update flows incremental rpc
func (ap *AdapterProxy) UpdateFlowsIncremental(ctx context.Context, device *voltha.Device, flowChanges *openflow_13.FlowChanges, groupChanges *openflow_13.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) (chan *kafka.RpcResponse, error) {
	logger.Debugw("UpdateFlowsIncremental",
		log.Fields{
			"device-id":             device.Id,
			"flow-to-add-count":     len(flowChanges.ToAdd.Items),
			"flow-to-delete-count":  len(flowChanges.ToRemove.Items),
			"group-to-add-count":    len(groupChanges.ToAdd.Items),
			"group-to-delete-count": len(groupChanges.ToRemove.Items),
			"group-to-update-count": len(groupChanges.ToUpdate.Items),
		})
	toTopic := ap.getAdapterTopic(device.Adapter)
	rpc := "update_flows_incrementally"
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "flow_changes", Value: flowChanges},
		{Key: "group_changes", Value: groupChanges},
		{Key: "flow_metadata", Value: flowMetadata},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(context.TODO(), rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// UpdatePmConfigs invokes update pm configs rpc
func (ap *AdapterProxy) UpdatePmConfigs(ctx context.Context, device *voltha.Device, pmConfigs *voltha.PmConfigs) (chan *kafka.RpcResponse, error) {
	logger.Debugw("UpdatePmConfigs", log.Fields{"device-id": device.Id, "pm-configs-id": pmConfigs.Id})
	toTopic := ap.getAdapterTopic(device.Adapter)
	rpc := "Update_pm_config"
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "pm_configs", Value: pmConfigs},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// SimulateAlarm invokes simulate alarm rpc
func (ap *AdapterProxy) SimulateAlarm(ctx context.Context, device *voltha.Device, simulateReq *voltha.SimulateAlarmRequest) (chan *kafka.RpcResponse, error) {
	logger.Debugw("SimulateAlarm", log.Fields{"device-id": device.Id, "simulate-req-id": simulateReq.Id})
	rpc := "simulate_alarm"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: simulateReq},
	}
	replyToTopic := ap.getCoreTopic()
	ap.deviceTopicRegistered = true
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

func (ap *AdapterProxy) DisablePort(ctx context.Context, device *voltha.Device, port *voltha.Port) (chan *kafka.RpcResponse, error) {
	logger.Debugw("DisablePort", log.Fields{"device-id": device.Id, "port-no": port.PortNo})
	rpc := "disable_port"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "deviceId", Value: &ic.StrType{Val: device.Id}},
		{Key: "port", Value: port},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

func (ap *AdapterProxy) EnablePort(ctx context.Context, device *voltha.Device, port *voltha.Port) (chan *kafka.RpcResponse, error) {
	logger.Debugw("EnablePort", log.Fields{"device-id": device.Id, "port-no": port.PortNo})
	rpc := "enable_port"
	toTopic := ap.getAdapterTopic(device.Adapter)
	args := []*kafka.KVArg{
		{Key: "deviceId", Value: &ic.StrType{Val: device.Id}},
		{Key: "port", Value: port},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
}

// ChildDeviceLost invokes child device_lost rpc
func (ap *AdapterProxy) ChildDeviceLost(ctx context.Context, deviceType string, pDeviceID string, pPortNo uint32, onuID uint32) (chan *kafka.RpcResponse, error) {
	logger.Debugw("ChildDeviceLost", log.Fields{"parent-device-id": pDeviceID, "parent-port-no": pPortNo, "onu-id": onuID})
	rpc := "child_device_lost"
	toTopic := ap.getAdapterTopic(deviceType)
	args := []*kafka.KVArg{
		{Key: "pDeviceId", Value: &ic.StrType{Val: pDeviceID}},
		{Key: "pPortNo", Value: &ic.IntType{Val: int64(pPortNo)}},
		{Key: "onuID", Value: &ic.IntType{Val: int64(onuID)}},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, pDeviceID, args...)
}

func (ap *AdapterProxy) StartOmciTest(ctx context.Context, device *voltha.Device, omcitestrequest *voltha.OmciTestRequest) (chan *kafka.RpcResponse, error) {
	logger.Debugw("Omci_test_Request_adapter_proxy", log.Fields{"device": device, "omciTestRequest": omcitestrequest})
	rpc := "start_omci_test"
	toTopic := ap.getAdapterTopic(device.Adapter)
	// Use a device specific topic as we are the only core handling requests for this device
	replyToTopic := ap.getCoreTopic()
	// TODO: Perhaps this should have used omcitestrequest.uuid as the second argument rather
	//   than including the whole request, which is (deviceid, uuid)
	return ap.sendRPC(ctx, rpc, &toTopic, &replyToTopic, true, device.Id,
		&kafka.KVArg{Key: "device", Value: device},
		&kafka.KVArg{Key: "omcitestrequest", Value: omcitestrequest})
}
