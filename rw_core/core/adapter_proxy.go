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
	"github.com/golang/protobuf/ptypes"
	a "github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/kafka"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdapterProxy struct {
	TestMode   bool
	kafkaProxy *kafka.KafkaMessagingProxy
}

func NewAdapterProxy(kafkaProxy *kafka.KafkaMessagingProxy) *AdapterProxy {
	var proxy AdapterProxy
	proxy.kafkaProxy = kafkaProxy
	return &proxy
}

func unPackResponse(rpc string, deviceId string, success bool, response *a.Any) error {
	if success {
		return nil
	} else {
		unpackResult := &ca.Error{}
		var err error
		if err = ptypes.UnmarshalAny(response, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		}
		log.Debugw("response", log.Fields{"rpc": rpc, "deviceId": deviceId, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return status.Errorf(codes.Canceled, "%s", unpackResult.Reason)
	}
}

func (ap *AdapterProxy) AdoptDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("AdoptDevice", log.Fields{"device": device})
	rpc := "adopt_device"
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	success, result := ap.kafkaProxy.InvokeRPC(ctx, rpc, &topic, true, args...)
	log.Debugw("AdoptDevice-response", log.Fields{"deviceid": device.Id, "success": success})
	return unPackResponse(rpc, device.Id, success, result)
}

func (ap *AdapterProxy) DisableDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("DisableDevice", log.Fields{"deviceId": device.Id})
	rpc := "disable_device"
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	success, result := ap.kafkaProxy.InvokeRPC(nil, rpc, &topic, true, args...)
	log.Debugw("DisableDevice-response", log.Fields{"deviceId": device.Id, "success": success})
	return unPackResponse(rpc, device.Id, success, result)
}

func (ap *AdapterProxy) ReEnableDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("ReEnableDevice", log.Fields{"deviceId": device.Id})
	rpc := "reenable_device"
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	success, result := ap.kafkaProxy.InvokeRPC(ctx, rpc, &topic, true, args...)
	log.Debugw("ReEnableDevice-response", log.Fields{"deviceid": device.Id, "success": success})
	return unPackResponse(rpc, device.Id, success, result)
}

func (ap *AdapterProxy) RebootDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("RebootDevice", log.Fields{"deviceId": device.Id})
	rpc := "reboot_device"
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	success, result := ap.kafkaProxy.InvokeRPC(ctx, rpc, &topic, true, args...)
	log.Debugw("RebootDevice-response", log.Fields{"deviceid": device.Id, "success": success})
	return unPackResponse(rpc, device.Id, success, result)
}

func (ap *AdapterProxy) DeleteDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("DeleteDevice", log.Fields{"deviceId": device.Id})
	rpc := "delete_device"
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	success, result := ap.kafkaProxy.InvokeRPC(ctx, rpc, &topic, true, args...)
	log.Debugw("DeleteDevice-response", log.Fields{"deviceid": device.Id, "success": success})
	return unPackResponse(rpc, device.Id, success, result)
}

func (ap *AdapterProxy) GetOfpDeviceInfo(ctx context.Context, device *voltha.Device) (*ca.SwitchCapability, error) {
	log.Debugw("GetOfpDeviceInfo", log.Fields{"deviceId": device.Id})
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	success, result := ap.kafkaProxy.InvokeRPC(ctx, "get_ofp_device_info", &topic, true, args...)
	log.Debugw("GetOfpDeviceInfo-response", log.Fields{"deviceId": device.Id, "success": success, "result": result})
	if success {
		unpackResult := &ca.SwitchCapability{}
		if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		return unpackResult, nil
	} else {
		unpackResult := &ca.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		}
		log.Debugw("GetOfpDeviceInfo-return", log.Fields{"deviceid": device.Id, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return nil, status.Errorf(codes.Internal, "%s", unpackResult.Reason)
	}
}

func (ap *AdapterProxy) GetOfpPortInfo(ctx context.Context, device *voltha.Device, portNo uint32) (*ca.PortCapability, error) {
	log.Debugw("GetOfpPortInfo", log.Fields{"deviceId": device.Id})
	topic := kafka.Topic{Name: device.Type}
	args := make([]*kafka.KVArg, 2)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	pNo := &ca.IntType{Val: int64(portNo)}
	args[1] = &kafka.KVArg{
		Key:   "port_no",
		Value: pNo,
	}

	success, result := ap.kafkaProxy.InvokeRPC(ctx, "get_ofp_port_info", &topic, true, args...)
	log.Debugw("GetOfpPortInfo-response", log.Fields{"deviceid": device.Id, "success": success})
	if success {
		unpackResult := &ca.PortCapability{}
		if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		return unpackResult, nil
	} else {
		unpackResult := &ca.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		}
		log.Debugw("GetOfpPortInfo-return", log.Fields{"deviceid": device.Id, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return nil, status.Errorf(codes.Internal, "%s", unpackResult.Reason)
	}
}

//TODO: Implement the functions below

func (ap *AdapterProxy) AdapterDescriptor() (*voltha.Adapter, error) {
	log.Debug("AdapterDescriptor")
	return nil, nil
}

func (ap *AdapterProxy) DeviceTypes() (*voltha.DeviceType, error) {
	log.Debug("DeviceTypes")
	return nil, nil
}

func (ap *AdapterProxy) Health() (*voltha.HealthStatus, error) {
	log.Debug("Health")
	return nil, nil
}

func (ap *AdapterProxy) ReconcileDevice(device *voltha.Device) error {
	log.Debug("ReconcileDevice")
	return nil
}

func (ap *AdapterProxy) AbandonDevice(device voltha.Device) error {
	log.Debug("AbandonDevice")
	return nil
}

func (ap *AdapterProxy) GetDeviceDetails(device voltha.Device) (*voltha.Device, error) {
	log.Debug("GetDeviceDetails")
	return nil, nil
}

func (ap *AdapterProxy) DownloadImage(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("DownloadImage")
	return nil
}

func (ap *AdapterProxy) GetImageDownloadStatus(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("GetImageDownloadStatus")
	return nil
}

func (ap *AdapterProxy) CancelImageDownload(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("CancelImageDownload")
	return nil
}

func (ap *AdapterProxy) ActivateImageUpdate(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("ActivateImageUpdate")
	return nil
}

func (ap *AdapterProxy) RevertImageUpdate(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("RevertImageUpdate")
	return nil
}

func (ap *AdapterProxy) SelfTestDevice(device voltha.Device) (*voltha.SelfTestResponse, error) {
	log.Debug("SelfTestDevice")
	return nil, nil
}

func (ap *AdapterProxy) UpdateFlowsBulk(device voltha.Device, flows voltha.Flows, groups voltha.FlowGroups) error {
	log.Debug("UpdateFlowsBulk")
	return nil
}

func (ap *AdapterProxy) UpdateFlowsIncremental(device voltha.Device, flowChanges voltha.Flows, groupChanges voltha.FlowGroups) error {
	log.Debug("UpdateFlowsIncremental")
	return nil
}

func (ap *AdapterProxy) UpdatePmConfig(device voltha.Device, pmConfigs voltha.PmConfigs) error {
	log.Debug("UpdatePmConfig")
	return nil
}

func (ap *AdapterProxy) ReceivePacketOut(deviceId voltha.ID, egressPortNo int, msg interface{}) error {
	log.Debug("ReceivePacketOut")
	return nil
}

func (ap *AdapterProxy) SuppressAlarm(filter voltha.AlarmFilter) error {
	log.Debug("SuppressAlarm")
	return nil
}

func (ap *AdapterProxy) UnSuppressAlarm(filter voltha.AlarmFilter) error {
	log.Debug("UnSuppressAlarm")
	return nil
}
