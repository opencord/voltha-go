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
package common

import (
	"context"
	"github.com/golang/protobuf/ptypes"
	a "github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/kafka"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type CoreProxy struct {
	kafkaICProxy *kafka.InterContainerProxy
	adapterTopic string
	coreTopic    string
	deviceIdCoreMap map[string]string
	lockDeviceIdCoreMap sync.RWMutex

}

func NewCoreProxy(kafkaProxy *kafka.InterContainerProxy, adapterTopic string, coreTopic string) *CoreProxy {
	var proxy CoreProxy
	proxy.kafkaICProxy = kafkaProxy
	proxy.adapterTopic = adapterTopic
	proxy.coreTopic = coreTopic
	proxy.deviceIdCoreMap = make(map[string]string)
	proxy.lockDeviceIdCoreMap = sync.RWMutex{}
	log.Debugw("TOPICS", log.Fields{"core": proxy.coreTopic, "adapter": proxy.adapterTopic})

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

// UpdateCoreReference adds or update a core reference (really the topic name) for a given device Id
func (ap *CoreProxy) UpdateCoreReference(deviceId string, coreReference string) {
	ap.lockDeviceIdCoreMap.Lock()
	defer ap.lockDeviceIdCoreMap.Unlock()
	ap.deviceIdCoreMap[deviceId] = coreReference
}

// DeleteCoreReference removes a core reference (really the topic name) for a given device Id
func (ap *CoreProxy) DeleteCoreReference(deviceId string) {
	ap.lockDeviceIdCoreMap.Lock()
	defer ap.lockDeviceIdCoreMap.Unlock()
	delete(ap.deviceIdCoreMap, deviceId)
}

func (ap *CoreProxy) getCoreTopic(deviceId string) kafka.Topic {
	ap.lockDeviceIdCoreMap.Lock()
	defer ap.lockDeviceIdCoreMap.Unlock()

	if t, exist := ap.deviceIdCoreMap[deviceId]; exist {
		return kafka.Topic{Name: t}
	}

	return kafka.Topic{Name: ap.coreTopic}
}

func (ap *CoreProxy) getAdapterTopic(args ...string) kafka.Topic {
	return kafka.Topic{Name: ap.adapterTopic}
}

func (ap *CoreProxy) RegisterAdapter(ctx context.Context, adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) error {
	log.Debugw("registering-adapter", log.Fields{"coreTopic": ap.coreTopic, "adapterTopic": ap.adapterTopic})
	rpc := "Register"
	topic := kafka.Topic{Name: ap.coreTopic}
	replyToTopic := ap.getAdapterTopic()
	args := make([]*kafka.KVArg, 2)
	args[0] = &kafka.KVArg{
		Key:   "adapter",
		Value: adapter,
	}
	args[1] = &kafka.KVArg{
		Key:   "deviceTypes",
		Value: deviceTypes,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(ctx, rpc, &topic, &replyToTopic, true, "", args...)
	log.Debugw("Register-Adapter-response", log.Fields{"replyTopic": replyToTopic, "success": success})
	return unPackResponse(rpc, "", success, result)
}

func (ap *CoreProxy) DeviceUpdate(ctx context.Context, device *voltha.Device) error {
	log.Debugw("DeviceUpdate", log.Fields{"deviceId": device.Id})
	rpc := "DeviceUpdate"
	toTopic := ap.getCoreTopic(device.Id)
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(nil, rpc, &toTopic, &replyToTopic, true, device.Id, args...)
	log.Debugw("DeviceUpdate-response", log.Fields{"deviceId": device.Id, "success": success})
	return unPackResponse(rpc, device.Id, success, result)
}

func (ap *CoreProxy) PortCreated(ctx context.Context, deviceId string, port *voltha.Port) error {
	log.Debugw("PortCreated", log.Fields{"portNo": port.PortNo})
	rpc := "PortCreated"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	args := make([]*kafka.KVArg, 2)
	id := &voltha.ID{Id: deviceId}
	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}
	args[1] = &kafka.KVArg{
		Key:   "port",
		Value: port,
	}

	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(nil, rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	log.Debugw("PortCreated-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(rpc, deviceId, success, result)
}

func (ap *CoreProxy) DeviceStateUpdate(ctx context.Context, deviceId string,
	connStatus voltha.ConnectStatus_ConnectStatus, operStatus voltha.OperStatus_OperStatus) error {
	log.Debugw("DeviceStateUpdate", log.Fields{"deviceId": deviceId})
	rpc := "DeviceStateUpdate"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	args := make([]*kafka.KVArg, 3)
	id := &voltha.ID{Id: deviceId}
	oStatus := &ic.IntType{Val: int64(operStatus)}
	cStatus := &ic.IntType{Val: int64(connStatus)}

	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}
	args[1] = &kafka.KVArg{
		Key:   "oper_status",
		Value: oStatus,
	}
	args[2] = &kafka.KVArg{
		Key:   "connect_status",
		Value: cStatus,
	}
	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(nil, rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	log.Debugw("DeviceStateUpdate-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(rpc, deviceId, success, result)
}

func (ap *CoreProxy) ChildDeviceDetected(ctx context.Context, parentDeviceId string, parentPortNo int,
	childDeviceType string, channelId int, vendorId string, serialNumber string, onuId int64 ) error {
	log.Debugw("ChildDeviceDetected", log.Fields{"pPeviceId": parentDeviceId, "channelId": channelId})
	rpc := "ChildDeviceDetected"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 7)
	id := &voltha.ID{Id: parentDeviceId}
	args[0] = &kafka.KVArg{
		Key:   "parent_device_id",
		Value: id,
	}
	ppn := &ic.IntType{Val: int64(parentPortNo)}
	args[1] = &kafka.KVArg{
		Key:   "parent_port_no",
		Value: ppn,
	}
	cdt := &ic.StrType{Val: childDeviceType}
	args[2] = &kafka.KVArg{
		Key:   "child_device_type",
		Value: cdt,
	}
	channel := &ic.IntType{Val: int64(channelId)}
	args[3] = &kafka.KVArg{
		Key:   "channel_id",
		Value: channel,
	}
	vId := &ic.StrType{Val: vendorId}
	args[4] = &kafka.KVArg{
		Key:   "vendor_id",
		Value: vId,
	}
	sNo := &ic.StrType{Val: serialNumber}
	args[5] = &kafka.KVArg{
		Key:   "serial_number",
		Value: sNo,
	}
	oId := &ic.IntType{Val: int64(onuId)}
	args[6] = &kafka.KVArg{
		Key:   "onu_id",
		Value: oId,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(nil, rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	log.Debugw("ChildDeviceDetected-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})
	return unPackResponse(rpc, parentDeviceId, success, result)
}
