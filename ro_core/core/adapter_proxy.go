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
	ic "github.com/opencord/voltha-go/protos/inter_container"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdapterProxy struct {
	TestMode     bool
	kafkaICProxy *kafka.InterContainerProxy
}

func NewAdapterProxy(kafkaProxy *kafka.InterContainerProxy) *AdapterProxy {
	var proxy AdapterProxy
	proxy.kafkaICProxy = kafkaProxy
	return &proxy
}

func unPackResponse(rpc string, deviceId string, success bool, response *a.Any) error {
	if success {
		return nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(response, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		}
		log.Debugw("response", log.Fields{"rpc": rpc, "deviceId": deviceId, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return status.Errorf(codes.Canceled, "%s", unpackResult.Reason)
	}
}

//func kafka.CreateSubTopic(args ...string) kafka.Topic{
//	topic := ""
//	for index , arg := range args {
//		if index == 0 {
//			topic = arg
//		} else {
//			topic = fmt.Sprintf("%s_%s",  topic, arg)
//		}
//	}
//	return kafka.Topic{Name:topic}
//}

func (ap *AdapterProxy) GetOfpDeviceInfo(ctx context.Context, device *voltha.Device) (*ic.SwitchCapability, error) {
	log.Debugw("GetOfpDeviceInfo", log.Fields{"deviceId": device.Id})
	toTopic := kafka.CreateSubTopic(device.Type, device.Id)
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	// Use a device specific topic as we are the only core handling requests for this device
	replyToTopic := kafka.CreateSubTopic(ap.kafkaICProxy.DefaultTopic.Name, device.Id)
	success, result := ap.kafkaICProxy.InvokeRPC(ctx, "get_ofp_device_info", &toTopic, &replyToTopic, true, args...)
	log.Debugw("GetOfpDeviceInfo-response", log.Fields{"deviceId": device.Id, "success": success, "result": result})
	if success {
		unpackResult := &ic.SwitchCapability{}
		if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		return unpackResult, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		}
		log.Debugw("GetOfpDeviceInfo-return", log.Fields{"deviceid": device.Id, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return nil, status.Errorf(codes.Internal, "%s", unpackResult.Reason)
	}
}

func (ap *AdapterProxy) GetOfpPortInfo(ctx context.Context, device *voltha.Device, portNo uint32) (*ic.PortCapability, error) {
	log.Debugw("GetOfpPortInfo", log.Fields{"deviceId": device.Id})
	toTopic := kafka.CreateSubTopic(device.Type, device.Id)
	args := make([]*kafka.KVArg, 2)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	pNo := &ic.IntType{Val: int64(portNo)}
	args[1] = &kafka.KVArg{
		Key:   "port_no",
		Value: pNo,
	}
	// Use a device specific topic as we are the only core handling requests for this device
	replyToTopic := kafka.CreateSubTopic(ap.kafkaICProxy.DefaultTopic.Name, device.Id)
	success, result := ap.kafkaICProxy.InvokeRPC(ctx, "get_ofp_port_info", &toTopic, &replyToTopic, true, args...)
	log.Debugw("GetOfpPortInfo-response", log.Fields{"deviceid": device.Id, "success": success})
	if success {
		unpackResult := &ic.PortCapability{}
		if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
			log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		return unpackResult, nil
	} else {
		unpackResult := &ic.Error{}
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

func (ap *AdapterProxy) GetDeviceDetails(device voltha.Device) (*voltha.Device, error) {
	log.Debug("GetDeviceDetails")
	return nil, nil
}

func (ap *AdapterProxy) GetImageDownloadStatus(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("GetImageDownloadStatus")
	return nil
}

func (ap *AdapterProxy) CancelImageDownload(device voltha.Device, download voltha.ImageDownload) error {
	log.Debug("CancelImageDownload")
	return nil
}

func (ap *AdapterProxy) SelfTestDevice(device voltha.Device) (*voltha.SelfTestResponse, error) {
	log.Debug("SelfTestDevice")
	return nil, nil
}

