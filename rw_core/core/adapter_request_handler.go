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
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdapterRequestHandlerProxy struct {
	TestMode         bool
	coreInstanceId	string
	deviceMgr        *DeviceManager
	lDeviceMgr       *LogicalDeviceManager
	localDataProxy   *model.Proxy
	clusterDataProxy *model.Proxy
}

func NewAdapterRequestHandlerProxy(coreInstanceId string, dMgr *DeviceManager, ldMgr *LogicalDeviceManager, cdProxy *model.Proxy, ldProxy *model.Proxy) *AdapterRequestHandlerProxy {
	var proxy AdapterRequestHandlerProxy
	proxy.coreInstanceId = coreInstanceId
	proxy.deviceMgr = dMgr
	proxy.lDeviceMgr = ldMgr
	proxy.clusterDataProxy = cdProxy
	proxy.localDataProxy = ldProxy
	return &proxy
}

func (rhp *AdapterRequestHandlerProxy) Register(args []*ca.Argument) (*voltha.CoreInstance, error) {
	if len(args) != 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	adapter := &voltha.Adapter{}
	deviceTypes := &voltha.DeviceTypes{}
	for _, arg := range args {
		switch arg.Key {
		case "adapter":
			if err := ptypes.UnmarshalAny(arg.Value, adapter); err != nil {
				log.Warnw("cannot-unmarshal-adapter", log.Fields{"error": err})
				return nil, err
			}
		case "deviceTypes":
			if err := ptypes.UnmarshalAny(arg.Value, deviceTypes); err != nil {
				log.Warnw("cannot-unmarshal-device-types", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Register", log.Fields{"Adapter": *adapter, "DeviceTypes": deviceTypes, "coreId": rhp.coreInstanceId})
	// TODO process the request and store the data in the KV store

	if rhp.TestMode { // Execute only for test cases
		return &voltha.CoreInstance{InstanceId: "CoreInstance"}, nil
	}
	return &voltha.CoreInstance{InstanceId: rhp.coreInstanceId}, nil
}

func (rhp *AdapterRequestHandlerProxy) GetDevice(args []*ca.Argument) (*voltha.Device, error) {
	if len(args) != 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pID := &voltha.ID{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
		return nil, err
	}
	log.Debugw("GetDevice", log.Fields{"deviceId": pID.Id})

	if rhp.TestMode { // Execute only for test cases
		return &voltha.Device{Id: pID.Id}, nil
	}

	// Get the device via the device manager
	if device, err := rhp.deviceMgr.GetDevice(pID.Id); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	} else {
		return device, nil
	}
}

// updatePartialDeviceData updates a subset of a device that an Adapter can update.
// TODO:  May need a specific proto to handle only a subset of a device that can be changed by an adapter
func (rhp *AdapterRequestHandlerProxy) mergeDeviceInfoFromAdapter(device *voltha.Device) (*voltha.Device, error) {
	//		First retrieve the most up to date device info
	var currentDevice *voltha.Device
	var err error
	if currentDevice, err = rhp.deviceMgr.GetDevice(device.Id); err != nil {
		return nil, err
	}
	cloned := proto.Clone(currentDevice).(*voltha.Device)
	cloned.Root = device.Root
	cloned.Vendor = device.Vendor
	cloned.Model = device.Model
	cloned.SerialNumber = device.SerialNumber
	cloned.MacAddress = device.MacAddress
	cloned.Vlan = device.Vlan
	return cloned, nil
}

func (rhp *AdapterRequestHandlerProxy) DeviceUpdate(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) != 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	if err := ptypes.UnmarshalAny(args[0].Value, device); err != nil {
		log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
		return nil, err
	}
	log.Debugw("DeviceUpdate", log.Fields{"deviceId": device.Id})

	if rhp.TestMode { // Execute only for test cases
		return new(empty.Empty), nil
	}

	//Merge the adapter device info (only the ones an adapter can change) with the latest device data
	if updatedDevice, err := rhp.mergeDeviceInfoFromAdapter(device); err != nil {
		return nil, status.Errorf(codes.Internal, "%s", err.Error())
	} else {
		// An adapter request needs an Ack without having to wait for the update to be
		// completed.  We therefore run the update in its own routine.
		go rhp.deviceMgr.updateDevice(updatedDevice)
	}

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) GetChildDevice(args []*ca.Argument) (*voltha.Device, error) {
	if len(args) < 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pID := &voltha.ID{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
		return nil, err
	}
	log.Debugw("GetChildDevice", log.Fields{"deviceId": pID.Id})

	if rhp.TestMode { // Execute only for test cases
		return &voltha.Device{Id: pID.Id}, nil
	}
	return nil, nil
}

func (rhp *AdapterRequestHandlerProxy) GetPorts(args []*ca.Argument) (*voltha.Ports, error) {
	if len(args) != 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	pt := &ca.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port_type":
			if err := ptypes.UnmarshalAny(arg.Value, pt); err != nil {
				log.Warnw("cannot-unmarshal-porttype", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("GetPorts", log.Fields{"deviceID": deviceId.Id, "portype": pt.Val})
	if rhp.TestMode { // Execute only for test cases
		aPort := &voltha.Port{Label: "test_port"}
		allPorts := &voltha.Ports{}
		allPorts.Items = append(allPorts.Items, aPort)
		return allPorts, nil
	}
	return rhp.deviceMgr.getPorts(nil, deviceId.Id, voltha.Port_PortType(pt.Val))
}

func (rhp *AdapterRequestHandlerProxy) GetChildDevices(args []*ca.Argument) (*voltha.Device, error) {
	if len(args) != 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pID := &voltha.ID{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
		return nil, err
	}
	log.Debugw("GetChildDevice", log.Fields{"deviceId": pID.Id})

	if rhp.TestMode { // Execute only for test cases
		return &voltha.Device{Id: pID.Id}, nil
	}
	//TODO: Complete
	return nil, nil
}

// ChildDeviceDetected is invoked when a child device is detected.  The following
// parameters are expected:
// {parent_device_id, parent_port_no, child_device_type, proxy_address, admin_state, **kw)
func (rhp *AdapterRequestHandlerProxy) ChildDeviceDetected(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) < 4 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	portNo := &ca.IntType{}
	dt := &ca.StrType{}
	chnlId := &ca.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				log.Warnw("cannot-unmarshal-parent-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "parent_port_no":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				log.Warnw("cannot-unmarshal-parent-port", log.Fields{"error": err})
				return nil, err
			}
		case "child_device_type":
			if err := ptypes.UnmarshalAny(arg.Value, dt); err != nil {
				log.Warnw("cannot-unmarshal-child-device-type", log.Fields{"error": err})
				return nil, err
			}
		case "channel_id":
			if err := ptypes.UnmarshalAny(arg.Value, chnlId); err != nil {
				log.Warnw("cannot-unmarshal-channel-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("ChildDeviceDetected", log.Fields{"parentDeviceId": pID.Id, "parentPortNo": portNo.Val,
		"deviceType": dt.Val, "channelId": chnlId.Val})

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	// Run child detection in it's own go routine as it can be a lengthy process
	go rhp.deviceMgr.childDeviceDetected(pID.Id, portNo.Val, dt.Val, chnlId.Val)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) DeviceStateUpdate(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	operStatus := &ca.IntType{}
	connStatus := &ca.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				log.Warnw("cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case "connect_status":
			if err := ptypes.UnmarshalAny(arg.Value, connStatus); err != nil {
				log.Warnw("cannot-unmarshal-connStatus", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DeviceStateUpdate", log.Fields{"deviceId": deviceId.Id, "oper-status": operStatus, "conn-status": connStatus})
	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	// When the enum is not set (i.e. -1), Go still convert to the Enum type with the value being -1
	go rhp.deviceMgr.updateDeviceStatus(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val), voltha.ConnectStatus_ConnectStatus(connStatus.Val))
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) ChildrenStateUpdate(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	operStatus := &ca.IntType{}
	connStatus := &ca.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				log.Warnw("cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case "connect_status":
			if err := ptypes.UnmarshalAny(arg.Value, connStatus); err != nil {
				log.Warnw("cannot-unmarshal-connStatus", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("ChildrenStateUpdate", log.Fields{"deviceId": deviceId.Id, "oper-status": operStatus, "conn-status": connStatus})
	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	// When the enum is not set (i.e. -1), Go still convert to the Enum type with the value being -1
	go rhp.deviceMgr.updateChildrenStatus(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val), voltha.ConnectStatus_ConnectStatus(connStatus.Val))
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) PortStateUpdate(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	portType := &ca.IntType{}
	portNo := &ca.IntType{}
	operStatus := &ca.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				log.Warnw("cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case "port_type":
			if err := ptypes.UnmarshalAny(arg.Value, portType); err != nil {
				log.Warnw("cannot-unmarshal-porttype", log.Fields{"error": err})
				return nil, err
			}
		case "port_no":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				log.Warnw("cannot-unmarshal-portno", log.Fields{"error": err})
				return nil, err
			}

		}
	}
	log.Debugw("PortStateUpdate", log.Fields{"deviceId": deviceId.Id, "operStatus": operStatus, "portType": portType, "portNo": portNo})
	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	go rhp.deviceMgr.updatePortState(deviceId.Id, voltha.Port_PortType(portType.Val), uint32(portNo.Val), voltha.OperStatus_OperStatus(operStatus.Val))
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) PortCreated(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) != 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	port := &voltha.Port{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port":
			if err := ptypes.UnmarshalAny(arg.Value, port); err != nil {
				log.Warnw("cannot-unmarshal-port", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("PortCreated", log.Fields{"deviceId": deviceId.Id, "port": port})

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	// Run port creation in its own go routine
	go rhp.deviceMgr.addPort(deviceId.Id, port)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) DevicePMConfigUpdate(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) != 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pmConfigs := &voltha.PmConfigs{}
	init := &ca.BoolType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_pm_config":
			if err := ptypes.UnmarshalAny(arg.Value, pmConfigs); err != nil {
				log.Warnw("cannot-unmarshal-pm-config", log.Fields{"error": err})
				return nil, err
			}
		case "init":
			if err := ptypes.UnmarshalAny(arg.Value, init); err != nil {
				log.Warnw("cannot-unmarshal-boolean", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DevicePMConfigUpdate", log.Fields{"deviceId": pmConfigs.Id, "configs": pmConfigs,
		"init": init})

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	// Run PM config update in its own go routine
	go rhp.deviceMgr.updatePmConfigs(pmConfigs.Id, pmConfigs)

	return new(empty.Empty), nil
}


func (rhp *AdapterRequestHandlerProxy) PacketIn(args []*ca.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	portNo := &ca.IntType{}
	packet := &ca.Packet{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				log.Warnw("cannot-unmarshal-port-no", log.Fields{"error": err})
				return nil, err
			}
		case "packet":
			if err := ptypes.UnmarshalAny(arg.Value, packet); err != nil {
				log.Warnw("cannot-unmarshal-packet", log.Fields{"error": err})
				return nil, err
			}

		}
	}
	log.Debugw("PacketIn", log.Fields{"deviceId": deviceId.Id, "port": portNo.Val,  "packet": packet})
	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	go rhp.deviceMgr.PacketIn(deviceId.Id, uint32(portNo.Val), packet.Payload)
	return new(empty.Empty), nil
}
