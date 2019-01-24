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
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	ic "github.com/opencord/voltha-go/protos/inter_container"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdapterRequestHandlerProxy struct {
	TestMode         bool
	coreInstanceId   string
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

func (rhp *AdapterRequestHandlerProxy) Register(args []*ic.Argument) (*voltha.CoreInstance, error) {
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

func (rhp *AdapterRequestHandlerProxy) GetDevice(args []*ic.Argument) (*voltha.Device, error) {
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
		log.Debugw("GetDevice-response", log.Fields{"deviceId": pID.Id})
		return device, nil
	}
}

func (rhp *AdapterRequestHandlerProxy) GetChildDevice(args []*ic.Argument) (*voltha.Device, error) {
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

func (rhp *AdapterRequestHandlerProxy) GetPorts(args []*ic.Argument) (*voltha.Ports, error) {
	if len(args) != 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	pt := &ic.IntType{}
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
