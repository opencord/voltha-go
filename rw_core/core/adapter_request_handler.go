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
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-go/rw_core/utils"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AdapterRequestHandlerProxy struct {
	TestMode                  bool
	coreInstanceId            string
	deviceMgr                 *DeviceManager
	lDeviceMgr                *LogicalDeviceManager
	adapterMgr                *AdapterManager
	localDataProxy            *model.Proxy
	clusterDataProxy          *model.Proxy
	defaultRequestTimeout     int64
	longRunningRequestTimeout int64
	coreInCompetingMode       bool
	core                      *Core
}

func NewAdapterRequestHandlerProxy(core *Core, coreInstanceId string, dMgr *DeviceManager, ldMgr *LogicalDeviceManager,
	aMgr *AdapterManager, cdProxy *model.Proxy, ldProxy *model.Proxy, incompetingMode bool, longRunningRequestTimeout int64,
	defaultRequestTimeout int64) *AdapterRequestHandlerProxy {
	var proxy AdapterRequestHandlerProxy
	proxy.core = core
	proxy.coreInstanceId = coreInstanceId
	proxy.deviceMgr = dMgr
	proxy.lDeviceMgr = ldMgr
	proxy.clusterDataProxy = cdProxy
	proxy.localDataProxy = ldProxy
	proxy.adapterMgr = aMgr
	proxy.coreInCompetingMode = incompetingMode
	proxy.defaultRequestTimeout = defaultRequestTimeout
	proxy.longRunningRequestTimeout = longRunningRequestTimeout
	return &proxy
}

func (rhp *AdapterRequestHandlerProxy) acquireRequest(transactionId string, maxTimeout ...int64) (*KVTransaction, error) {
	timeout := rhp.defaultRequestTimeout
	if len(maxTimeout) > 0 {
		timeout = maxTimeout[0]
	}
	txn := NewKVTransaction(transactionId)
	if txn == nil {
		return nil, errors.New("fail-to-create-transaction")
	} else if txn.Acquired(timeout) {
		log.Debugw("acquired-request", log.Fields{"xtrnsId": transactionId})
		return txn, nil
	} else {
		return nil, errors.New("failed-to-seize-request")
	}
}

// This is a helper function that attempts to acquire the request by using the device ownership model
func (rhp *AdapterRequestHandlerProxy) takeRequestOwnership(transactionId string, devId string, maxTimeout ...int64) (*KVTransaction, error) {
	timeout := rhp.defaultRequestTimeout
	if len(maxTimeout) > 0 {
		timeout = maxTimeout[0]
	}
	txn := NewKVTransaction(transactionId)
	if txn == nil {
		return nil, errors.New("fail-to-create-transaction")
	}

	if rhp.core.deviceOwnership.OwnedByMe(&utils.DeviceID{Id: devId}) {
		log.Debugw("owned-by-me", log.Fields{"Id": devId})
		if txn.Acquired(timeout) {
			log.Debugw("processing-request", log.Fields{"Id": devId})
			return txn, nil
		} else {
			return nil, errors.New("failed-to-seize-request")
		}
	} else {
		log.Debugw("not-owned-by-me", log.Fields{"Id": devId})
		if txn.Monitor(timeout) {
			log.Debugw("timeout-processing-request", log.Fields{"Id": devId})
			return txn, nil
		} else {
			return nil, errors.New("device-not-owned")
		}
	}
}

// competeForTransaction is a helper function to determine whether every request needs to compete with another
// Core to execute the request
func (rhp *AdapterRequestHandlerProxy) competeForTransaction() bool {
	return rhp.coreInCompetingMode
}

func (rhp *AdapterRequestHandlerProxy) Register(args []*ic.Argument) (*voltha.CoreInstance, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	adapter := &voltha.Adapter{}
	deviceTypes := &voltha.DeviceTypes{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Register", log.Fields{"Adapter": *adapter, "DeviceTypes": deviceTypes, "transactionID": transactionID.Val, "coreId": rhp.coreInstanceId})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.acquireRequest(transactionID.Val); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// Update our adapters in memory
			go rhp.adapterMgr.updateAdaptersAndDevicetypesInMemory()
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return &voltha.CoreInstance{InstanceId: "CoreInstance"}, nil
	}
	return rhp.adapterMgr.registerAdapter(adapter, deviceTypes), nil
}

func (rhp *AdapterRequestHandlerProxy) GetDevice(args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("GetDevice", log.Fields{"deviceId": pID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, pID.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

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
	cloned.Reason = device.Reason
	return cloned, nil
}

func (rhp *AdapterRequestHandlerProxy) DeviceUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DeviceUpdate", log.Fields{"deviceId": device.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, device.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return new(empty.Empty), nil
	}

	//Merge the adapter device info (only the ones an adapter can change) with the latest device data
	if updatedDevice, err := rhp.mergeDeviceInfoFromAdapter(device); err != nil {
		return nil, status.Errorf(codes.Internal, "%s", err.Error())
	} else {
		go rhp.deviceMgr.updateDevice(updatedDevice)
		//if err := rhp.deviceMgr.updateDevice(updatedDevice); err != nil {
		//	return nil, err
		//}
	}

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) GetChildDevice(args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	transactionID := &ic.StrType{}
	serialNumber := &ic.StrType{}
	onuId := &ic.IntType{}
	parentPortNo := &ic.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case "serial_number":
			if err := ptypes.UnmarshalAny(arg.Value, serialNumber); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case "onu_id":
			if err := ptypes.UnmarshalAny(arg.Value, onuId); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case "parent_port_no":
			if err := ptypes.UnmarshalAny(arg.Value, parentPortNo); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("GetChildDevice", log.Fields{"parentDeviceId": pID.Id, "args": args, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, pID.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return &voltha.Device{Id: pID.Id}, nil
	}
	return rhp.deviceMgr.GetChildDevice(pID.Id, serialNumber.Val, onuId.Val, parentPortNo.Val)
}

func (rhp *AdapterRequestHandlerProxy) GetChildDeviceWithProxyAddress(args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	proxyAddress := &voltha.Device_ProxyAddress{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "proxy_address":
			if err := ptypes.UnmarshalAny(arg.Value, proxyAddress); err != nil {
				log.Warnw("cannot-unmarshal-proxy-address", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("GetChildDeviceWithProxyAddress", log.Fields{"proxyAddress": proxyAddress, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, proxyAddress.DeviceId); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return &voltha.Device{Id: proxyAddress.DeviceId}, nil
	}
	return rhp.deviceMgr.GetChildDeviceWithProxyAddress(proxyAddress)
}

func (rhp *AdapterRequestHandlerProxy) GetPorts(args []*ic.Argument) (*voltha.Ports, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	pt := &ic.IntType{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("GetPorts", log.Fields{"deviceID": deviceId.Id, "portype": pt.Val, "transactionID": transactionID.Val})
	if rhp.TestMode { // Execute only for test cases
		aPort := &voltha.Port{Label: "test_port"}
		allPorts := &voltha.Ports{}
		allPorts.Items = append(allPorts.Items, aPort)
		return allPorts, nil
	}
	return rhp.deviceMgr.getPorts(nil, deviceId.Id, voltha.Port_PortType(pt.Val))
}

func (rhp *AdapterRequestHandlerProxy) GetChildDevices(args []*ic.Argument) (*voltha.Devices, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				log.Warnw("cannot-unmarshal-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("GetChildDevices", log.Fields{"deviceId": pID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, pID.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return &voltha.Devices{Items: nil}, nil
	}

	return rhp.deviceMgr.getAllChildDevices(pID.Id)
}

// ChildDeviceDetected is invoked when a child device is detected.  The following
// parameters are expected:
// {parent_device_id, parent_port_no, child_device_type, channel_id, vendor_id, serial_number)
func (rhp *AdapterRequestHandlerProxy) ChildDeviceDetected(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 5 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	portNo := &ic.IntType{}
	dt := &ic.StrType{}
	chnlId := &ic.IntType{}
	transactionID := &ic.StrType{}
	serialNumber := &ic.StrType{}
	vendorId := &ic.StrType{}
	onuId := &ic.IntType{}
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
		case "vendor_id":
			if err := ptypes.UnmarshalAny(arg.Value, vendorId); err != nil {
				log.Warnw("cannot-unmarshal-vendor-id", log.Fields{"error": err})
				return nil, err
			}
		case "serial_number":
			if err := ptypes.UnmarshalAny(arg.Value, serialNumber); err != nil {
				log.Warnw("cannot-unmarshal-serial-number", log.Fields{"error": err})
				return nil, err
			}
		case "onu_id":
			if err := ptypes.UnmarshalAny(arg.Value, onuId); err != nil {
				log.Warnw("cannot-unmarshal-onu-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("ChildDeviceDetected", log.Fields{"parentDeviceId": pID.Id, "parentPortNo": portNo.Val,
		"deviceType": dt.Val, "channelId": chnlId.Val, "serialNumber": serialNumber.Val,
		"vendorId": vendorId.Val, "onuId": onuId.Val, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, pID.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	// Run child detection in it's own go routine as it can be a lengthy process
	go rhp.deviceMgr.childDeviceDetected(pID.Id, portNo.Val, dt.Val, chnlId.Val, vendorId.Val, serialNumber.Val, onuId.Val)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) DeviceStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	operStatus := &ic.IntType{}
	connStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DeviceStateUpdate", log.Fields{"deviceId": deviceId.Id, "oper-status": operStatus,
		"conn-status": connStatus, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	// When the enum is not set (i.e. -1), Go still convert to the Enum type with the value being -1
	go rhp.deviceMgr.updateDeviceStatus(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val),
		voltha.ConnectStatus_ConnectStatus(connStatus.Val))

	//if err := rhp.deviceMgr.updateDeviceStatus(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val),
	//	voltha.ConnectStatus_ConnectStatus(connStatus.Val)); err != nil {
	//	return nil, err
	//}
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) ChildrenStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	operStatus := &ic.IntType{}
	connStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("ChildrenStateUpdate", log.Fields{"deviceId": deviceId.Id, "oper-status": operStatus,
		"conn-status": connStatus, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	// When the enum is not set (i.e. -1), Go still convert to the Enum type with the value being -1
	go rhp.deviceMgr.updateChildrenStatus(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val),
		voltha.ConnectStatus_ConnectStatus(connStatus.Val))

	//if err := rhp.deviceMgr.updateChildrenStatus(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val),
	//	voltha.ConnectStatus_ConnectStatus(connStatus.Val)); err != nil {
	//	return nil, err
	//}
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) PortsStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	operStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("PortsStateUpdate", log.Fields{"deviceId": deviceId.Id, "operStatus": operStatus, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	go rhp.deviceMgr.updatePortsState(deviceId.Id, voltha.OperStatus_OperStatus(operStatus.Val))

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) PortStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	portType := &ic.IntType{}
	portNo := &ic.IntType{}
	operStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("PortStateUpdate", log.Fields{"deviceId": deviceId.Id, "operStatus": operStatus,
		"portType": portType, "portNo": portNo, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	go rhp.deviceMgr.updatePortState(deviceId.Id, voltha.Port_PortType(portType.Val), uint32(portNo.Val),
		voltha.OperStatus_OperStatus(operStatus.Val))

	//if err := rhp.deviceMgr.updatePortState(deviceId.Id, voltha.Port_PortType(portType.Val), uint32(portNo.Val),
	//	voltha.OperStatus_OperStatus(operStatus.Val)); err != nil {
	//	return nil, err
	//}
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) DeleteAllPorts(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DeleteAllPorts", log.Fields{"deviceId": deviceId.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	go rhp.deviceMgr.deleteAllPorts(deviceId.Id)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) ChildDevicesLost(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceId := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("ChildDevicesLost", log.Fields{"deviceId": parentDeviceId.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, parentDeviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	go rhp.deviceMgr.childDevicesLost(parentDeviceId.Id)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) ChildDevicesDetected(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceId := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("ChildDevicesDetected", log.Fields{"deviceId": parentDeviceId.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, parentDeviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	go rhp.deviceMgr.childDevicesDetected(parentDeviceId.Id)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) PortCreated(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	port := &voltha.Port{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("PortCreated", log.Fields{"deviceId": deviceId.Id, "port": port, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	go rhp.deviceMgr.addPort(deviceId.Id, port)

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) DevicePMConfigUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pmConfigs := &voltha.PmConfigs{}
	init := &ic.BoolType{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DevicePMConfigUpdate", log.Fields{"deviceId": pmConfigs.Id, "configs": pmConfigs,
		"init": init, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, pmConfigs.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}

	go rhp.deviceMgr.updatePmConfigs(pmConfigs.Id, pmConfigs)
	//if err := rhp.deviceMgr.updatePmConfigs(pmConfigs.Id, pmConfigs); err != nil {
	//	return nil, err
	//}

	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) PacketIn(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 4 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	portNo := &ic.IntType{}
	packet := &ic.Packet{}
	transactionID := &ic.StrType{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("PacketIn", log.Fields{"deviceId": deviceId.Id, "port": portNo.Val, "packet": packet,
		"transactionID": transactionID.Val})

	// For performance reason, we do not compete for packet-in.  We process it and send the packet in.  later in the
	// processing flow the duplicate packet will be discarded

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	go rhp.deviceMgr.PacketIn(deviceId.Id, uint32(portNo.Val), transactionID.Val, packet.Payload)
	//if err := rhp.deviceMgr.PacketIn(deviceId.Id, uint32(portNo.Val), transactionID.Val, packet.Payload); err != nil {
	//	return nil, err
	//}
	return new(empty.Empty), nil
}

func (rhp *AdapterRequestHandlerProxy) UpdateImageDownload(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &voltha.ID{}
	img := &voltha.ImageDownload{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "image_download":
			if err := ptypes.UnmarshalAny(arg.Value, img); err != nil {
				log.Warnw("cannot-unmarshal-imgaeDownload", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("UpdateImageDownload", log.Fields{"deviceId": deviceId.Id, "image-download": img,
		"transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		if txn, err := rhp.takeRequestOwnership(transactionID.Val, deviceId.Id); err != nil {
			log.Debugw("Another core handled the request", log.Fields{"transactionID": transactionID})
			// returning nil, nil instructs the callee to ignore this request
			return nil, nil
		} else {
			defer txn.Close()
		}
	}

	if rhp.TestMode { // Execute only for test cases
		return nil, nil
	}
	go rhp.deviceMgr.updateImageDownload(deviceId.Id, img)
	//if err := rhp.deviceMgr.updateImageDownload(deviceId.Id, img); err != nil {
	//	return nil, err
	//}
	return new(empty.Empty), nil
}
