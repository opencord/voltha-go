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
	"sync"

	"github.com/golang/protobuf/ptypes"
	a "github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CoreProxy struct {
	kafkaICProxy        kafka.InterContainerProxy
	adapterTopic        string
	coreTopic           string
	deviceIdCoreMap     map[string]string
	lockDeviceIdCoreMap sync.RWMutex
}

func NewCoreProxy(ctx context.Context, kafkaProxy kafka.InterContainerProxy, adapterTopic string, coreTopic string) *CoreProxy {
	var proxy CoreProxy
	proxy.kafkaICProxy = kafkaProxy
	proxy.adapterTopic = adapterTopic
	proxy.coreTopic = coreTopic
	proxy.deviceIdCoreMap = make(map[string]string)
	proxy.lockDeviceIdCoreMap = sync.RWMutex{}
	logger.Debugw(ctx, "TOPICS", log.Fields{"core": proxy.coreTopic, "adapter": proxy.adapterTopic})

	return &proxy
}

func unPackResponse(ctx context.Context, rpc string, deviceId string, success bool, response *a.Any) error {
	if success {
		return nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(response, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "response", log.Fields{"rpc": rpc, "deviceId": deviceId, "success": success, "error": err})
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
	logger.Debugw(ctx, "registering-adapter", log.Fields{"coreTopic": ap.coreTopic, "adapterTopic": ap.adapterTopic})
	rpc := "Register"
	topic := kafka.Topic{Name: ap.coreTopic}
	replyToTopic := ap.getAdapterTopic()
	args := make([]*kafka.KVArg, 2)

	if adapter.TotalReplicas == 0 && adapter.CurrentReplica != 0 {
		logger.Fatal(ctx, "totalReplicas can't be 0, since you're here you have at least one")
	}

	if adapter.CurrentReplica == 0 && adapter.TotalReplicas != 0 {
		logger.Fatal(ctx, "currentReplica can't be 0, it has to start from 1")
	}

	if adapter.CurrentReplica == 0 && adapter.TotalReplicas == 0 {
		// if the adapter is not setting these fields they default to 0,
		// in that case it means the adapter is not ready to be scaled and thus it defaults
		// to a single instance
		adapter.CurrentReplica = 1
		adapter.TotalReplicas = 1
	}

	if adapter.CurrentReplica > adapter.TotalReplicas {
		logger.Fatalf(ctx, "CurrentReplica (%d) can't be greater than TotalReplicas (%d)",
			adapter.CurrentReplica, adapter.TotalReplicas)
	}

	args[0] = &kafka.KVArg{
		Key:   "adapter",
		Value: adapter,
	}
	args[1] = &kafka.KVArg{
		Key:   "deviceTypes",
		Value: deviceTypes,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(ctx, rpc, &topic, &replyToTopic, true, "", args...)
	logger.Debugw(ctx, "Register-Adapter-response", log.Fields{"replyTopic": replyToTopic, "success": success})
	return unPackResponse(ctx, rpc, "", success, result)
}

func (ap *CoreProxy) DeviceUpdate(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "DeviceUpdate", log.Fields{"deviceId": device.Id})
	rpc := "DeviceUpdate"
	toTopic := ap.getCoreTopic(device.Id)
	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device",
		Value: device,
	}
	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, device.Id, args...)
	logger.Debugw(ctx, "DeviceUpdate-response", log.Fields{"deviceId": device.Id, "success": success})
	return unPackResponse(ctx, rpc, device.Id, success, result)
}

func (ap *CoreProxy) PortCreated(ctx context.Context, deviceId string, port *voltha.Port) error {
	logger.Debugw(ctx, "PortCreated", log.Fields{"portNo": port.PortNo})
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
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "PortCreated-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}

func (ap *CoreProxy) PortsStateUpdate(ctx context.Context, deviceId string, portTypeFilter uint32, operStatus voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "PortsStateUpdate", log.Fields{"deviceId": deviceId})
	rpc := "PortsStateUpdate"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	args := []*kafka.KVArg{{
		Key:   "device_id",
		Value: &voltha.ID{Id: deviceId},
	}, {
		Key:   "port_type_filter",
		Value: &ic.IntType{Val: int64(portTypeFilter)},
	}, {
		Key:   "oper_status",
		Value: &ic.IntType{Val: int64(operStatus)},
	}}

	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "PortsStateUpdate-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}

func (ap *CoreProxy) DeleteAllPorts(ctx context.Context, deviceId string) error {
	logger.Debugw(ctx, "DeleteAllPorts", log.Fields{"deviceId": deviceId})
	rpc := "DeleteAllPorts"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	args := make([]*kafka.KVArg, 2)
	id := &voltha.ID{Id: deviceId}

	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}

	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "DeleteAllPorts-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}

func (ap *CoreProxy) GetDevicePort(ctx context.Context, deviceID string, portNo uint32) (*voltha.Port, error) {
	logger.Debugw(ctx, "GetDevicePort", log.Fields{"device-id": deviceID})
	rpc := "GetDevicePort"

	toTopic := ap.getCoreTopic(deviceID)
	replyToTopic := ap.getAdapterTopic()

	args := []*kafka.KVArg{{
		Key:   "device_id",
		Value: &voltha.ID{Id: deviceID},
	}, {
		Key:   "port_no",
		Value: &ic.IntType{Val: int64(portNo)},
	}}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceID, args...)
	logger.Debugw(ctx, "GetDevicePort-response", log.Fields{"device-id": deviceID, "success": success})

	if success {
		port := &voltha.Port{}
		if err := ptypes.UnmarshalAny(result, port); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return port, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "GetDevicePort-return", log.Fields{"device-id": deviceID, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return nil, status.Error(ICProxyErrorCodeToGrpcErrorCode(ctx, unpackResult.Code), unpackResult.Reason)
	}
}

func (ap *CoreProxy) ListDevicePorts(ctx context.Context, deviceID string) ([]*voltha.Port, error) {
	logger.Debugw(ctx, "ListDevicePorts", log.Fields{"device-id": deviceID})
	rpc := "ListDevicePorts"

	toTopic := ap.getCoreTopic(deviceID)
	replyToTopic := ap.getAdapterTopic()

	args := []*kafka.KVArg{{
		Key:   "device_id",
		Value: &voltha.ID{Id: deviceID},
	}}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceID, args...)
	logger.Debugw(ctx, "ListDevicePorts-response", log.Fields{"device-id": deviceID, "success": success})

	if success {
		ports := &voltha.Ports{}
		if err := ptypes.UnmarshalAny(result, ports); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return ports.Items, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "ListDevicePorts-return", log.Fields{"device-id": deviceID, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return nil, status.Error(ICProxyErrorCodeToGrpcErrorCode(ctx, unpackResult.Code), unpackResult.Reason)
	}
}

func (ap *CoreProxy) DeviceStateUpdate(ctx context.Context, deviceId string,
	connStatus voltha.ConnectStatus_Types, operStatus voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "DeviceStateUpdate", log.Fields{"deviceId": deviceId})
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
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "DeviceStateUpdate-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}

func (ap *CoreProxy) ChildDeviceDetected(ctx context.Context, parentDeviceId string, parentPortNo int,
	childDeviceType string, channelId int, vendorId string, serialNumber string, onuId int64) (*voltha.Device, error) {
	logger.Debugw(ctx, "ChildDeviceDetected", log.Fields{"pDeviceId": parentDeviceId, "channelId": channelId})
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

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "ChildDeviceDetected-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})

	if success {
		volthaDevice := &voltha.Device{}
		if err := ptypes.UnmarshalAny(result, volthaDevice); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return volthaDevice, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "ChildDeviceDetected-return", log.Fields{"deviceid": parentDeviceId, "success": success, "error": err})

		return nil, status.Error(ICProxyErrorCodeToGrpcErrorCode(ctx, unpackResult.Code), unpackResult.Reason)
	}

}

func (ap *CoreProxy) ChildDevicesLost(ctx context.Context, parentDeviceId string) error {
	logger.Debugw(ctx, "ChildDevicesLost", log.Fields{"pDeviceId": parentDeviceId})
	rpc := "ChildDevicesLost"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 1)
	id := &voltha.ID{Id: parentDeviceId}
	args[0] = &kafka.KVArg{
		Key:   "parent_device_id",
		Value: id,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "ChildDevicesLost-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})
	return unPackResponse(ctx, rpc, parentDeviceId, success, result)
}

func (ap *CoreProxy) ChildDevicesDetected(ctx context.Context, parentDeviceId string) error {
	logger.Debugw(ctx, "ChildDevicesDetected", log.Fields{"pDeviceId": parentDeviceId})
	rpc := "ChildDevicesDetected"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 1)
	id := &voltha.ID{Id: parentDeviceId}
	args[0] = &kafka.KVArg{
		Key:   "parent_device_id",
		Value: id,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "ChildDevicesDetected-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})
	return unPackResponse(ctx, rpc, parentDeviceId, success, result)
}

func (ap *CoreProxy) GetDevice(ctx context.Context, parentDeviceId string, deviceId string) (*voltha.Device, error) {
	logger.Debugw(ctx, "GetDevice", log.Fields{"deviceId": deviceId})
	rpc := "GetDevice"

	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 1)
	id := &voltha.ID{Id: deviceId}
	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "GetDevice-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})

	if success {
		volthaDevice := &voltha.Device{}
		if err := ptypes.UnmarshalAny(result, volthaDevice); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return volthaDevice, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "GetDevice-return", log.Fields{"deviceid": parentDeviceId, "success": success, "error": err})
		// TODO:  Need to get the real error code
		return nil, status.Error(ICProxyErrorCodeToGrpcErrorCode(ctx, unpackResult.Code), unpackResult.Reason)
	}
}

func (ap *CoreProxy) GetChildDevice(ctx context.Context, parentDeviceId string, kwargs map[string]interface{}) (*voltha.Device, error) {
	logger.Debugw(ctx, "GetChildDevice", log.Fields{"parentDeviceId": parentDeviceId, "kwargs": kwargs})
	rpc := "GetChildDevice"

	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 4)
	id := &voltha.ID{Id: parentDeviceId}
	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}

	var cnt uint8 = 0
	for k, v := range kwargs {
		cnt += 1
		if k == "serial_number" {
			val := &ic.StrType{Val: v.(string)}
			args[cnt] = &kafka.KVArg{
				Key:   k,
				Value: val,
			}
		} else if k == "onu_id" {
			val := &ic.IntType{Val: int64(v.(uint32))}
			args[cnt] = &kafka.KVArg{
				Key:   k,
				Value: val,
			}
		} else if k == "parent_port_no" {
			val := &ic.IntType{Val: int64(v.(uint32))}
			args[cnt] = &kafka.KVArg{
				Key:   k,
				Value: val,
			}
		}
	}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "GetChildDevice-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})

	if success {
		volthaDevice := &voltha.Device{}
		if err := ptypes.UnmarshalAny(result, volthaDevice); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return volthaDevice, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "GetChildDevice-return", log.Fields{"deviceid": parentDeviceId, "success": success, "error": err})

		return nil, status.Error(ICProxyErrorCodeToGrpcErrorCode(ctx, unpackResult.Code), unpackResult.Reason)
	}
}

func (ap *CoreProxy) GetChildDevices(ctx context.Context, parentDeviceId string) (*voltha.Devices, error) {
	logger.Debugw(ctx, "GetChildDevices", log.Fields{"parentDeviceId": parentDeviceId})
	rpc := "GetChildDevices"

	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 1)
	id := &voltha.ID{Id: parentDeviceId}
	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "GetChildDevices-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})

	if success {
		volthaDevices := &voltha.Devices{}
		if err := ptypes.UnmarshalAny(result, volthaDevices); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return volthaDevices, nil
	} else {
		unpackResult := &ic.Error{}
		var err error
		if err = ptypes.UnmarshalAny(result, unpackResult); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		}
		logger.Debugw(ctx, "GetChildDevices-return", log.Fields{"deviceid": parentDeviceId, "success": success, "error": err})

		return nil, status.Error(ICProxyErrorCodeToGrpcErrorCode(ctx, unpackResult.Code), unpackResult.Reason)
	}
}

func (ap *CoreProxy) SendPacketIn(ctx context.Context, deviceId string, port uint32, pktPayload []byte) error {
	logger.Debugw(ctx, "SendPacketIn", log.Fields{"deviceId": deviceId, "port": port, "pktPayload": pktPayload})
	rpc := "PacketIn"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 3)
	id := &voltha.ID{Id: deviceId}
	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}
	portNo := &ic.IntType{Val: int64(port)}
	args[1] = &kafka.KVArg{
		Key:   "port",
		Value: portNo,
	}
	pkt := &ic.Packet{Payload: pktPayload}
	args[2] = &kafka.KVArg{
		Key:   "packet",
		Value: pkt,
	}
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "SendPacketIn-response", log.Fields{"pDeviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}

func (ap *CoreProxy) DeviceReasonUpdate(ctx context.Context, deviceId string, deviceReason string) error {
	logger.Debugw(ctx, "DeviceReasonUpdate", log.Fields{"deviceId": deviceId, "deviceReason": deviceReason})
	rpc := "DeviceReasonUpdate"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 2)
	id := &voltha.ID{Id: deviceId}
	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: id,
	}
	reason := &ic.StrType{Val: deviceReason}
	args[1] = &kafka.KVArg{
		Key:   "device_reason",
		Value: reason,
	}
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "DeviceReason-response", log.Fields{"pDeviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}

func (ap *CoreProxy) DevicePMConfigUpdate(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	logger.Debugw(ctx, "DevicePMConfigUpdate", log.Fields{"pmConfigs": pmConfigs})
	rpc := "DevicePMConfigUpdate"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(pmConfigs.Id)
	replyToTopic := ap.getAdapterTopic()

	args := make([]*kafka.KVArg, 1)
	args[0] = &kafka.KVArg{
		Key:   "device_pm_config",
		Value: pmConfigs,
	}
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, pmConfigs.Id, args...)
	logger.Debugw(ctx, "DevicePMConfigUpdate-response", log.Fields{"pDeviceId": pmConfigs.Id, "success": success})
	return unPackResponse(ctx, rpc, pmConfigs.Id, success, result)
}

func (ap *CoreProxy) ReconcileChildDevices(ctx context.Context, parentDeviceId string) error {
	logger.Debugw(ctx, "ReconcileChildDevices", log.Fields{"parentDeviceId": parentDeviceId})
	rpc := "ReconcileChildDevices"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(parentDeviceId)
	replyToTopic := ap.getAdapterTopic()

	args := []*kafka.KVArg{
		{Key: "parent_device_id", Value: &voltha.ID{Id: parentDeviceId}},
	}

	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, parentDeviceId, args...)
	logger.Debugw(ctx, "ReconcileChildDevices-response", log.Fields{"pDeviceId": parentDeviceId, "success": success})
	return unPackResponse(ctx, rpc, parentDeviceId, success, result)
}

func (ap *CoreProxy) PortStateUpdate(ctx context.Context, deviceId string, pType voltha.Port_PortType, portNum uint32,
	operStatus voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "PortStateUpdate", log.Fields{"deviceId": deviceId, "portType": pType, "portNo": portNum, "operation_status": operStatus})
	rpc := "PortStateUpdate"
	// Use a device specific topic to send the request.  The adapter handling the device creates a device
	// specific topic
	toTopic := ap.getCoreTopic(deviceId)
	args := make([]*kafka.KVArg, 4)
	deviceID := &voltha.ID{Id: deviceId}
	portNo := &ic.IntType{Val: int64(portNum)}
	portType := &ic.IntType{Val: int64(pType)}
	oStatus := &ic.IntType{Val: int64(operStatus)}

	args[0] = &kafka.KVArg{
		Key:   "device_id",
		Value: deviceID,
	}
	args[1] = &kafka.KVArg{
		Key:   "oper_status",
		Value: oStatus,
	}
	args[2] = &kafka.KVArg{
		Key:   "port_type",
		Value: portType,
	}
	args[3] = &kafka.KVArg{
		Key:   "port_no",
		Value: portNo,
	}

	// Use a device specific topic as we are the only adaptercore handling requests for this device
	replyToTopic := ap.getAdapterTopic()
	success, result := ap.kafkaICProxy.InvokeRPC(log.WithSpanFromContext(context.Background(), ctx), rpc, &toTopic, &replyToTopic, true, deviceId, args...)
	logger.Debugw(ctx, "PortStateUpdate-response", log.Fields{"deviceId": deviceId, "success": success})
	return unPackResponse(ctx, rpc, deviceId, success, result)
}
