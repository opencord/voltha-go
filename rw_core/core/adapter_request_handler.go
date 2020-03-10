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
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"time"
)

// AdapterRequestHandlerProxy represent adapter request handler proxy attributes
type AdapterRequestHandlerProxy struct {
	coreInstanceID            string
	deviceMgr                 *DeviceManager
	lDeviceMgr                *LogicalDeviceManager
	adapterMgr                *AdapterManager
	localDataProxy            *model.Proxy
	clusterDataProxy          *model.Proxy
	defaultRequestTimeout     time.Duration
	longRunningRequestTimeout time.Duration
	coreInCompetingMode       bool
	core                      *Core
}

// NewAdapterRequestHandlerProxy assigns values for adapter request handler proxy attributes and returns the new instance
func NewAdapterRequestHandlerProxy(core *Core, coreInstanceID string, dMgr *DeviceManager, ldMgr *LogicalDeviceManager,
	aMgr *AdapterManager, cdProxy *model.Proxy, ldProxy *model.Proxy, incompetingMode bool, longRunningRequestTimeout time.Duration,
	defaultRequestTimeout time.Duration) *AdapterRequestHandlerProxy {
	var proxy AdapterRequestHandlerProxy
	proxy.core = core
	proxy.coreInstanceID = coreInstanceID
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

// This is a helper function that attempts to acquire the request by using the device ownership model
func (rhp *AdapterRequestHandlerProxy) takeRequestOwnership(ctx context.Context, transactionID string, devID string, maxTimeout ...int64) (*KVTransaction, error) {
	timeout := rhp.defaultRequestTimeout.Milliseconds()
	if len(maxTimeout) > 0 {
		timeout = maxTimeout[0]
	}
	txn := NewKVTransaction(transactionID)
	if txn == nil {
		return nil, errors.New("fail-to-create-transaction")
	}

	var acquired bool
	var err error
	if devID != "" {
		var ownedByMe bool
		if ownedByMe, err = rhp.core.deviceOwnership.OwnedByMe(ctx, &utils.DeviceID{ID: devID}); err != nil {
			log.Warnw("getting-ownership-failed", log.Fields{"deviceId": devID, "error": err})
			return nil, kafka.ErrorTransactionInvalidId
		}
		acquired, err = txn.Acquired(ctx, timeout, ownedByMe)
	} else {
		acquired, err = txn.Acquired(ctx, timeout)
	}
	if err == nil && acquired {
		log.Debugw("transaction-acquired", log.Fields{"transactionId": txn.txnID})
		return txn, nil
	}
	log.Debugw("transaction-not-acquired", log.Fields{"transactionId": txn.txnID, "error": err})
	return nil, kafka.ErrorTransactionNotAcquired
}

// competeForTransaction is a helper function to determine whether every request needs to compete with another
// Core to execute the request
func (rhp *AdapterRequestHandlerProxy) competeForTransaction() bool {
	return rhp.coreInCompetingMode
}

// Register registers the adapter
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
	log.Debugw("Register", log.Fields{"adapter": *adapter, "device-types": deviceTypes, "transaction-id": transactionID.Val, "core-id": rhp.coreInstanceID})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, "")
		if err != nil {
			if err.Error() == kafka.ErrorTransactionNotAcquired.Error() {
				log.Debugw("Another core handled the request", log.Fields{"transactionId": transactionID})
				// Update our adapters in memory
				go rhp.adapterMgr.updateAdaptersAndDevicetypesInMemory(context.TODO(), adapter)
			}
			return nil, err
		}
		defer txn.Close(context.TODO())
	}
	return rhp.adapterMgr.registerAdapter(adapter, deviceTypes)
}

// GetDevice returns device info
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
	log.Debugw("GetDevice", log.Fields{"deviceID": pID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, pID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	// Get the device via the device manager
	device, err := rhp.deviceMgr.GetDevice(context.TODO(), pID.Id)
	if err != nil {
		log.Debugw("get-device-failed", log.Fields{"deviceID": pID.Id, "error": err})
	}
	return device, err
}

// DeviceUpdate updates device using adapter data
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
	log.Debugw("DeviceUpdate", log.Fields{"deviceID": device.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, device.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	log.Debugw("DeviceUpdate got txn", log.Fields{"deviceID": device.Id, "transactionID": transactionID.Val})

	if err := rhp.deviceMgr.updateDeviceUsingAdapterData(context.TODO(), device); err != nil {
		log.Debugw("unable-to-update-device-using-adapter-data", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// GetChildDevice returns details of child device
func (rhp *AdapterRequestHandlerProxy) GetChildDevice(args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	transactionID := &ic.StrType{}
	serialNumber := &ic.StrType{}
	onuID := &ic.IntType{}
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
			if err := ptypes.UnmarshalAny(arg.Value, onuID); err != nil {
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
	log.Debugw("GetChildDevice", log.Fields{"parentDeviceID": pID.Id, "args": args, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, pID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	return rhp.deviceMgr.GetChildDevice(context.TODO(), pID.Id, serialNumber.Val, onuID.Val, parentPortNo.Val)
}

// GetChildDeviceWithProxyAddress returns details of child device with proxy address
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
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, proxyAddress.DeviceId)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	return rhp.deviceMgr.GetChildDeviceWithProxyAddress(context.TODO(), proxyAddress)
}

// GetPorts returns the ports information of the device based on the port type.
func (rhp *AdapterRequestHandlerProxy) GetPorts(args []*ic.Argument) (*voltha.Ports, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	pt := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("GetPorts", log.Fields{"deviceID": deviceID.Id, "portype": pt.Val, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	return rhp.deviceMgr.getPorts(context.TODO(), deviceID.Id, voltha.Port_PortType(pt.Val))
}

// GetChildDevices gets all the child device IDs from the device passed as parameter
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
	log.Debugw("GetChildDevices", log.Fields{"deviceID": pID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, pID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	return rhp.deviceMgr.getAllChildDevices(context.TODO(), pID.Id)
}

// ChildDeviceDetected is invoked when a child device is detected.  The following parameters are expected:
// {parent_device_id, parent_port_no, child_device_type, channel_id, vendor_id, serial_number)
func (rhp *AdapterRequestHandlerProxy) ChildDeviceDetected(args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 5 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	portNo := &ic.IntType{}
	dt := &ic.StrType{}
	chnlID := &ic.IntType{}
	transactionID := &ic.StrType{}
	serialNumber := &ic.StrType{}
	vendorID := &ic.StrType{}
	onuID := &ic.IntType{}
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
			if err := ptypes.UnmarshalAny(arg.Value, chnlID); err != nil {
				log.Warnw("cannot-unmarshal-channel-id", log.Fields{"error": err})
				return nil, err
			}
		case "vendor_id":
			if err := ptypes.UnmarshalAny(arg.Value, vendorID); err != nil {
				log.Warnw("cannot-unmarshal-vendor-id", log.Fields{"error": err})
				return nil, err
			}
		case "serial_number":
			if err := ptypes.UnmarshalAny(arg.Value, serialNumber); err != nil {
				log.Warnw("cannot-unmarshal-serial-number", log.Fields{"error": err})
				return nil, err
			}
		case "onu_id":
			if err := ptypes.UnmarshalAny(arg.Value, onuID); err != nil {
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
	log.Debugw("ChildDeviceDetected", log.Fields{"parentDeviceID": pID.Id, "parentPortNo": portNo.Val,
		"deviceType": dt.Val, "channelID": chnlID.Val, "serialNumber": serialNumber.Val,
		"vendorID": vendorID.Val, "onuID": onuID.Val, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, pID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	device, err := rhp.deviceMgr.childDeviceDetected(context.TODO(), pID.Id, portNo.Val, dt.Val, chnlID.Val, vendorID.Val, serialNumber.Val, onuID.Val)
	if err != nil {
		log.Debugw("child-detection-failed", log.Fields{"parentID": pID.Id, "onuID": onuID.Val, "error": err})
	}
	return device, err
}

// DeviceStateUpdate updates device status
func (rhp *AdapterRequestHandlerProxy) DeviceStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	operStatus := &ic.IntType{}
	connStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("DeviceStateUpdate", log.Fields{"deviceID": deviceID.Id, "oper-status": operStatus,
		"conn-status": connStatus, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.updateDeviceStatus(context.TODO(), deviceID.Id, voltha.OperStatus_Types(operStatus.Val),
		voltha.ConnectStatus_Types(connStatus.Val)); err != nil {
		log.Debugw("unable-to-update-device-status", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ChildrenStateUpdate updates child device status
func (rhp *AdapterRequestHandlerProxy) ChildrenStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	operStatus := &ic.IntType{}
	connStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("ChildrenStateUpdate", log.Fields{"deviceID": deviceID.Id, "oper-status": operStatus,
		"conn-status": connStatus, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	// When the enum is not set (i.e. -1), Go still convert to the Enum type with the value being -1
	if err := rhp.deviceMgr.updateChildrenStatus(context.TODO(), deviceID.Id, voltha.OperStatus_Types(operStatus.Val),
		voltha.ConnectStatus_Types(connStatus.Val)); err != nil {
		log.Debugw("unable-to-update-children-status", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PortsStateUpdate updates the ports state related to the device
func (rhp *AdapterRequestHandlerProxy) PortsStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	operStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("PortsStateUpdate", log.Fields{"deviceID": deviceID.Id, "operStatus": operStatus, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.updatePortsState(context.TODO(), deviceID.Id, voltha.OperStatus_Types(operStatus.Val)); err != nil {
		log.Debugw("unable-to-update-ports-state", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PortStateUpdate updates the port state of the device
func (rhp *AdapterRequestHandlerProxy) PortStateUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	portType := &ic.IntType{}
	portNo := &ic.IntType{}
	operStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("PortStateUpdate", log.Fields{"deviceID": deviceID.Id, "operStatus": operStatus,
		"portType": portType, "portNo": portNo, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.updatePortState(context.TODO(), deviceID.Id, voltha.Port_PortType(portType.Val), uint32(portNo.Val),
		voltha.OperStatus_Types(operStatus.Val)); err != nil {
		// If the error doesn't change behavior and is essentially ignored, it is not an error, it is a
		// warning.
		// TODO: VOL-2707
		log.Debugw("unable-to-update-port-state", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// DeleteAllPorts deletes all ports of device
func (rhp *AdapterRequestHandlerProxy) DeleteAllPorts(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("DeleteAllPorts", log.Fields{"deviceID": deviceID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.deleteAllPorts(context.TODO(), deviceID.Id); err != nil {
		log.Debugw("unable-to-delete-ports", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ChildDevicesLost indicates that a parent device is in a state (Disabled) where it cannot manage the child devices.
// This will trigger the Core to disable all the child devices.
func (rhp *AdapterRequestHandlerProxy) ChildDevicesLost(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceID); err != nil {
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
	log.Debugw("ChildDevicesLost", log.Fields{"deviceID": parentDeviceID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, parentDeviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.childDevicesLost(context.TODO(), parentDeviceID.Id); err != nil {
		log.Debugw("unable-to-disable-child-devices", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ChildDevicesDetected invoked by an adapter when child devices are found, typically after after a disable/enable sequence.
// This will trigger the Core to Enable all the child devices of that parent.
func (rhp *AdapterRequestHandlerProxy) ChildDevicesDetected(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceID); err != nil {
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
	log.Debugw("ChildDevicesDetected", log.Fields{"deviceID": parentDeviceID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, parentDeviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.childDevicesDetected(context.TODO(), parentDeviceID.Id); err != nil {
		log.Debugw("child-devices-detection-failed", log.Fields{"parentID": parentDeviceID.Id, "error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PortCreated adds port to device
func (rhp *AdapterRequestHandlerProxy) PortCreated(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	port := &voltha.Port{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("PortCreated", log.Fields{"deviceID": deviceID.Id, "port": port, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.addPort(context.TODO(), deviceID.Id, port); err != nil {
		log.Debugw("unable-to-add-port", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// DevicePMConfigUpdate initializes the pm configs as defined by the adapter.
func (rhp *AdapterRequestHandlerProxy) DevicePMConfigUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pmConfigs := &voltha.PmConfigs{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_pm_config":
			if err := ptypes.UnmarshalAny(arg.Value, pmConfigs); err != nil {
				log.Warnw("cannot-unmarshal-pm-config", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DevicePMConfigUpdate", log.Fields{"deviceID": pmConfigs.Id, "configs": pmConfigs,
		"transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, pmConfigs.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.initPmConfigs(context.TODO(), pmConfigs.Id, pmConfigs); err != nil {
		log.Debugw("unable-to-initialize-pm-configs", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PacketIn sends the incoming packet of device
func (rhp *AdapterRequestHandlerProxy) PacketIn(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 4 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	portNo := &ic.IntType{}
	packet := &ic.Packet{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("PacketIn", log.Fields{"deviceID": deviceID.Id, "port": portNo.Val, "packet": packet,
		"transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	// TODO: If this adds too much latencies then needs to remove transaction and let OFAgent filter out
	// duplicates.
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.PacketIn(context.TODO(), deviceID.Id, uint32(portNo.Val), transactionID.Val, packet.Payload); err != nil {
		log.Debugw("unable-to-receive-packet-from-adapter", log.Fields{"error": err})
		return nil, err

	}
	return &empty.Empty{}, nil
}

// UpdateImageDownload updates image download
func (rhp *AdapterRequestHandlerProxy) UpdateImageDownload(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	img := &voltha.ImageDownload{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
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
	log.Debugw("UpdateImageDownload", log.Fields{"deviceID": deviceID.Id, "image-download": img,
		"transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.updateImageDownload(context.TODO(), deviceID.Id, img); err != nil {
		log.Debugw("unable-to-update-image-download", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ReconcileChildDevices reconciles child devices
func (rhp *AdapterRequestHandlerProxy) ReconcileChildDevices(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceID); err != nil {
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
	log.Debugw("ReconcileChildDevices", log.Fields{"deviceID": parentDeviceID.Id, "transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, parentDeviceID.Id)
		if err != nil {
			log.Debugw("Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.reconcileChildDevices(context.TODO(), parentDeviceID.Id); err != nil {
		log.Debugw("unable-to-reconcile-child-devices", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// DeviceReasonUpdate updates device reason
func (rhp *AdapterRequestHandlerProxy) DeviceReasonUpdate(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("DeviceReasonUpdate: invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("DeviceReasonUpdate: invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	reason := &ic.StrType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				log.Warnw("cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "device_reason":
			if err := ptypes.UnmarshalAny(arg.Value, reason); err != nil {
				log.Warnw("cannot-unmarshal-reason", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("DeviceReasonUpdate", log.Fields{"deviceId": deviceID.Id, "reason": reason.Val,
		"transactionID": transactionID.Val})

	// Try to grab the transaction as this core may be competing with another Core
	if rhp.competeForTransaction() {
		txn, err := rhp.takeRequestOwnership(context.TODO(), transactionID.Val, deviceID.Id)
		if err != nil {
			log.Debugw("DeviceReasonUpdate: Core did not process request", log.Fields{"transactionID": transactionID, "error": err})
			return nil, err
		}
		defer txn.Close(context.TODO())
	}

	if err := rhp.deviceMgr.updateDeviceReason(context.TODO(), deviceID.Id, reason.Val); err != nil {
		log.Debugw("unable-to-update-device-reason", log.Fields{"error": err})
		return nil, err

	}
	return &empty.Empty{}, nil
}
