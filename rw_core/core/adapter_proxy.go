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

package core

import (
	"context"
	"errors"
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
	endpointManager       kafka.EndpointManager
}

// NewAdapterProxy will return adapter proxy instance
func NewAdapterProxy(kafkaProxy kafka.InterContainerProxy, corePairTopic string, endpointManager kafka.EndpointManager) *AdapterProxy {
	return &AdapterProxy{
		kafkaICProxy:          kafkaProxy,
		corePairTopic:         corePairTopic,
		deviceTopicRegistered: false,
		endpointManager:       endpointManager,
	}
}

func (ap *AdapterProxy) getCoreTopic() kafka.Topic {
	return kafka.Topic{Name: ap.corePairTopic}
}

func (ap *AdapterProxy) getAdapterTopic(deviceID string, adapterType string) (*kafka.Topic, error) {

	endpoint, err := ap.endpointManager.GetEndpoint(deviceID, adapterType)
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		logger.Errorw("cannot-find-topic-for-device", log.Fields{
			"deviceID":    deviceID,
			"adapterType": adapterType,
		})
		return nil, errors.New("cannot-find-topic-for-device")
	}

	return &kafka.Topic{Name: string(endpoint)}, nil
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

// adoptDevice invokes adopt device rpc
func (ap *AdapterProxy) adoptDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("adoptDevice", log.Fields{"device-id": device.Id})
	rpc := "adopt_device"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	ap.deviceTopicRegistered = true
	logger.Debugw("adoptDevice-send-request", log.Fields{"device-id": device.Id, "deviceType": device.Type, "serialNumber": device.SerialNumber})
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// disableDevice invokes disable device rpc
func (ap *AdapterProxy) disableDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("disableDevice", log.Fields{"device-id": device.Id})
	rpc := "disable_device"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// reEnableDevice invokes reenable device rpc
func (ap *AdapterProxy) reEnableDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("reEnableDevice", log.Fields{"device-id": device.Id})
	rpc := "reenable_device"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// rebootDevice invokes reboot device rpc
func (ap *AdapterProxy) rebootDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("rebootDevice", log.Fields{"device-id": device.Id})
	rpc := "reboot_device"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// deleteDevice invokes delete device rpc
func (ap *AdapterProxy) deleteDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("deleteDevice", log.Fields{"device-id": device.Id})
	rpc := "delete_device"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// getOfpDeviceInfo invokes get ofp device info rpc
func (ap *AdapterProxy) getOfpDeviceInfo(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("getOfpDeviceInfo", log.Fields{"device-id": device.Id})
	rpc := "get_ofp_device_info"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// getOfpPortInfo invokes get ofp port info rpc
func (ap *AdapterProxy) getOfpPortInfo(ctx context.Context, device *voltha.Device, portNo uint32) (chan *kafka.RpcResponse, error) {
	logger.Debugw("getOfpPortInfo", log.Fields{"device-id": device.Id, "port-no": portNo})
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "port_no", Value: &ic.IntType{Val: int64(portNo)}},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, "get_ofp_port_info", toTopic, &replyToTopic, true, device.Id, args...)
}

// reconcileDevice invokes reconcile device rpc
func (ap *AdapterProxy) reconcileDevice(ctx context.Context, device *voltha.Device) (chan *kafka.RpcResponse, error) {
	logger.Debugw("reconcileDevice", log.Fields{"device-id": device.Id})
	rpc := "reconcile_device"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// downloadImage invokes download image rpc
func (ap *AdapterProxy) downloadImage(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("downloadImage", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "download_image"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// getImageDownloadStatus invokes get image download status rpc
func (ap *AdapterProxy) getImageDownloadStatus(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("getImageDownloadStatus", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "get_image_download_status"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// cancelImageDownload invokes cancel image download rpc
func (ap *AdapterProxy) cancelImageDownload(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("cancelImageDownload", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "cancel_image_download"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// activateImageUpdate invokes activate image update rpc
func (ap *AdapterProxy) activateImageUpdate(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("activateImageUpdate", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "activate_image_update"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// revertImageUpdate invokes revert image update rpc
func (ap *AdapterProxy) revertImageUpdate(ctx context.Context, device *voltha.Device, download *voltha.ImageDownload) (chan *kafka.RpcResponse, error) {
	logger.Debugw("revertImageUpdate", log.Fields{"device-id": device.Id, "image": download.Name})
	rpc := "revert_image_update"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: download},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

func (ap *AdapterProxy) packetOut(ctx context.Context, deviceType string, deviceID string, outPort uint32, packet *openflow_13.OfpPacketOut) (chan *kafka.RpcResponse, error) {
	logger.Debugw("packetOut", log.Fields{"device-id": deviceID, "device-type": deviceType, "out-port": outPort})
	toTopic, err := ap.getAdapterTopic(deviceID, deviceType)
	if err != nil {
		return nil, err
	}
	rpc := "receive_packet_out"
	args := []*kafka.KVArg{
		{Key: "deviceId", Value: &ic.StrType{Val: deviceID}},
		{Key: "outPort", Value: &ic.IntType{Val: int64(outPort)}},
		{Key: "packet", Value: packet},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, deviceID, args...)
}

// updateFlowsBulk invokes update flows bulk rpc
func (ap *AdapterProxy) updateFlowsBulk(ctx context.Context, device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, flowMetadata *voltha.FlowMetadata) (chan *kafka.RpcResponse, error) {
	logger.Debugw("updateFlowsBulk", log.Fields{"device-id": device.Id, "flow-count": len(flows.Items), "group-count": len(groups.Items), "flow-metadata": flowMetadata})
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	rpc := "update_flows_bulk"
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "flows", Value: flows},
		{Key: "groups", Value: groups},
		{Key: "flow_metadata", Value: flowMetadata},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(context.TODO(), rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// updateFlowsIncremental invokes update flows incremental rpc
func (ap *AdapterProxy) updateFlowsIncremental(ctx context.Context, device *voltha.Device, flowChanges *openflow_13.FlowChanges, groupChanges *openflow_13.FlowGroupChanges, flowMetadata *voltha.FlowMetadata) (chan *kafka.RpcResponse, error) {
	logger.Debugw("updateFlowsIncremental",
		log.Fields{
			"device-id":             device.Id,
			"flow-to-add-count":     len(flowChanges.ToAdd.Items),
			"flow-to-delete-count":  len(flowChanges.ToRemove.Items),
			"group-to-add-count":    len(groupChanges.ToAdd.Items),
			"group-to-delete-count": len(groupChanges.ToRemove.Items),
			"group-to-update-count": len(groupChanges.ToUpdate.Items),
		})
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	rpc := "update_flows_incrementally"
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "flow_changes", Value: flowChanges},
		{Key: "group_changes", Value: groupChanges},
		{Key: "flow_metadata", Value: flowMetadata},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(context.TODO(), rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// updatePmConfigs invokes update pm configs rpc
func (ap *AdapterProxy) updatePmConfigs(ctx context.Context, device *voltha.Device, pmConfigs *voltha.PmConfigs) (chan *kafka.RpcResponse, error) {
	logger.Debugw("updatePmConfigs", log.Fields{"device-id": device.Id, "pm-configs-id": pmConfigs.Id})
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	rpc := "Update_pm_config"
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "pm_configs", Value: pmConfigs},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// simulateAlarm invokes simulate alarm rpc
func (ap *AdapterProxy) simulateAlarm(ctx context.Context, device *voltha.Device, simulateReq *voltha.SimulateAlarmRequest) (chan *kafka.RpcResponse, error) {
	logger.Debugw("simulateAlarm", log.Fields{"device-id": device.Id, "simulate-req-id": simulateReq.Id})
	rpc := "simulate_alarm"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "device", Value: device},
		{Key: "request", Value: simulateReq},
	}
	replyToTopic := ap.getCoreTopic()
	ap.deviceTopicRegistered = true
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

func (ap *AdapterProxy) disablePort(ctx context.Context, device *voltha.Device, port *voltha.Port) (chan *kafka.RpcResponse, error) {
	logger.Debugw("disablePort", log.Fields{"device-id": device.Id, "port-no": port.PortNo})
	rpc := "disable_port"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "deviceId", Value: &ic.StrType{Val: device.Id}},
		{Key: "port", Value: port},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

func (ap *AdapterProxy) enablePort(ctx context.Context, device *voltha.Device, port *voltha.Port) (chan *kafka.RpcResponse, error) {
	logger.Debugw("enablePort", log.Fields{"device-id": device.Id, "port-no": port.PortNo})
	rpc := "enable_port"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "deviceId", Value: &ic.StrType{Val: device.Id}},
		{Key: "port", Value: port},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id, args...)
}

// childDeviceLost invokes child device_lost rpc
func (ap *AdapterProxy) childDeviceLost(ctx context.Context, deviceType string, deviceID string, pPortNo uint32, onuID uint32) (chan *kafka.RpcResponse, error) {
	logger.Debugw("childDeviceLost", log.Fields{"device-id": deviceID, "parent-port-no": pPortNo, "onu-id": onuID})
	rpc := "child_device_lost"
	toTopic, err := ap.getAdapterTopic(deviceID, deviceType)
	if err != nil {
		return nil, err
	}
	args := []*kafka.KVArg{
		{Key: "pDeviceId", Value: &ic.StrType{Val: deviceID}},
		{Key: "pPortNo", Value: &ic.IntType{Val: int64(pPortNo)}},
		{Key: "onuID", Value: &ic.IntType{Val: int64(onuID)}},
	}
	replyToTopic := ap.getCoreTopic()
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, deviceID, args...)
}

func (ap *AdapterProxy) startOmciTest(ctx context.Context, device *voltha.Device, omcitestrequest *voltha.OmciTestRequest) (chan *kafka.RpcResponse, error) {
	logger.Debugw("Omci_test_Request_adapter_proxy", log.Fields{"device": device, "omciTestRequest": omcitestrequest})
	rpc := "start_omci_test"
	toTopic, err := ap.getAdapterTopic(device.Id, device.Adapter)
	if err != nil {
		return nil, err
	}
	// Use a device specific topic as we are the only core handling requests for this device
	replyToTopic := ap.getCoreTopic()
	// TODO: Perhaps this should have used omcitestrequest.uuid as the second argument rather
	//   than including the whole request, which is (deviceid, uuid)
	return ap.sendRPC(ctx, rpc, toTopic, &replyToTopic, true, device.Id,
		&kafka.KVArg{Key: "device", Value: device},
		&kafka.KVArg{Key: "omcitestrequest", Value: omcitestrequest})
}
