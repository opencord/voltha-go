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

package api

import (
	"context"
	"errors"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v5/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v5/pkg/log"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

// AdapterRequestHandlerProxy represent adapter request handler proxy attributes
type AdapterRequestHandlerProxy struct {
	deviceMgr  *device.Manager
	adapterMgr *adapter.Manager
}

// NewAdapterRequestHandlerProxy assigns values for adapter request handler proxy attributes and returns the new instance
func NewAdapterRequestHandlerProxy(dMgr *device.Manager, aMgr *adapter.Manager) *AdapterRequestHandlerProxy {
	return &AdapterRequestHandlerProxy{
		deviceMgr:  dMgr,
		adapterMgr: aMgr,
	}
}

func (rhp *AdapterRequestHandlerProxy) Register(ctx context.Context, args []*ic.Argument) (*voltha.CoreInstance, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-adapter", log.Fields{"error": err})
				return nil, err
			}
		case "deviceTypes":
			if err := ptypes.UnmarshalAny(arg.Value, deviceTypes); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-types", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "register", log.Fields{"adapter": *adapter, "device-types": deviceTypes, "transaction-id": transactionID.Val})

	return rhp.adapterMgr.RegisterAdapter(ctx, adapter, deviceTypes)
}

// GetDevice returns device info
func (rhp *AdapterRequestHandlerProxy) GetDevice(ctx context.Context, args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "get-device", log.Fields{"device-id": pID.Id, "transaction-id": transactionID.Val})

	// Get the device via the device manager
	device, err := rhp.deviceMgr.GetDevice(log.WithSpanFromContext(context.TODO(), ctx), pID)
	if err != nil {
		logger.Debugw(ctx, "get-device-failed", log.Fields{"device-id": pID.Id, "error": err})
	}
	return device, err
}

// DeviceUpdate updates device using adapter data
func (rhp *AdapterRequestHandlerProxy) DeviceUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "device-update", log.Fields{"device-id": device.Id, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "DeviceUpdate")
	if err := rhp.deviceMgr.UpdateDeviceUsingAdapterData(rpcCtx, device); err != nil {
		logger.Debugw(ctx, "unable-to-update-device-using-adapter-data", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// GetChildDevice returns details of child device
func (rhp *AdapterRequestHandlerProxy) GetChildDevice(ctx context.Context, args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case "serial_number":
			if err := ptypes.UnmarshalAny(arg.Value, serialNumber); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case "onu_id":
			if err := ptypes.UnmarshalAny(arg.Value, onuID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case "parent_port_no":
			if err := ptypes.UnmarshalAny(arg.Value, parentPortNo); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "get-child-device", log.Fields{"parent-device-id": pID.Id, "args": args, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "GetChildDevice")
	return rhp.deviceMgr.GetChildDevice(rpcCtx, pID.Id, serialNumber.Val, onuID.Val, parentPortNo.Val)
}

// GetChildDeviceWithProxyAddress returns details of child device with proxy address
func (rhp *AdapterRequestHandlerProxy) GetChildDeviceWithProxyAddress(ctx context.Context, args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	proxyAddress := &voltha.Device_ProxyAddress{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "proxy_address":
			if err := ptypes.UnmarshalAny(arg.Value, proxyAddress); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-proxy-address", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "get-child-device-with-proxy-address", log.Fields{"proxy-address": proxyAddress, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "GetChildDeviceWithProxyAddress")
	return rhp.deviceMgr.GetChildDeviceWithProxyAddress(rpcCtx, proxyAddress)
}

// GetPorts returns the ports information of the device based on the port type.
func (rhp *AdapterRequestHandlerProxy) GetPorts(ctx context.Context, args []*ic.Argument) (*voltha.Ports, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port_type":
			if err := ptypes.UnmarshalAny(arg.Value, pt); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-porttype", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "get-ports", log.Fields{"device-id": deviceID.Id, "port-type": pt.Val, "transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "GetPorts")
	return rhp.deviceMgr.GetPorts(rpcCtx, deviceID.Id, voltha.Port_PortType(pt.Val))
}

// GetChildDevices gets all the child device IDs from the device passed as parameter
func (rhp *AdapterRequestHandlerProxy) GetChildDevices(ctx context.Context, args []*ic.Argument) (*voltha.Devices, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	pID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "get-child-devices", log.Fields{"device-id": pID.Id, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "GetChildDevices")

	return rhp.deviceMgr.GetAllChildDevices(rpcCtx, pID.Id)
}

// ChildDeviceDetected is invoked when a child device is detected.  The following parameters are expected:
// {parent_device_id, parent_port_no, child_device_type, channel_id, vendor_id, serial_number)
func (rhp *AdapterRequestHandlerProxy) ChildDeviceDetected(ctx context.Context, args []*ic.Argument) (*voltha.Device, error) {
	if len(args) < 5 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, pID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-parent-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "parent_port_no":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-parent-port", log.Fields{"error": err})
				return nil, err
			}
		case "child_device_type":
			if err := ptypes.UnmarshalAny(arg.Value, dt); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-child-device-type", log.Fields{"error": err})
				return nil, err
			}
		case "channel_id":
			if err := ptypes.UnmarshalAny(arg.Value, chnlID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-channel-id", log.Fields{"error": err})
				return nil, err
			}
		case "vendor_id":
			if err := ptypes.UnmarshalAny(arg.Value, vendorID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-vendor-id", log.Fields{"error": err})
				return nil, err
			}
		case "serial_number":
			if err := ptypes.UnmarshalAny(arg.Value, serialNumber); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-serial-number", log.Fields{"error": err})
				return nil, err
			}
		case "onu_id":
			if err := ptypes.UnmarshalAny(arg.Value, onuID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-onu-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		case "fromTopic":
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-fromTopic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "child-device-detected", log.Fields{"parent-device-id": pID.Id, "parent-port-no": portNo.Val,
		"device-type": dt.Val, "channel-id": chnlID.Val, "serial-number": serialNumber.Val,
		"vendor-id": vendorID.Val, "onu-id": onuID.Val, "transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "ChildDeviceDetected")
	fromTopicContext := utils.WithFromTopicMetadataContext(rpcCtx, fromTopic.Val)
	device, err := rhp.deviceMgr.ChildDeviceDetected(fromTopicContext, pID.Id, portNo.Val, dt.Val, chnlID.Val, vendorID.Val, serialNumber.Val, onuID.Val)
	if err != nil {
		logger.Debugw(ctx, "child-detection-failed", log.Fields{"parent-device-id": pID.Id, "onu-id": onuID.Val, "error": err})
	}
	return device, err
}

// DeviceStateUpdate updates device status
func (rhp *AdapterRequestHandlerProxy) DeviceStateUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case "connect_status":
			if err := ptypes.UnmarshalAny(arg.Value, connStatus); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-connStatus", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "device-state-update", log.Fields{"device-id": deviceID.Id, "oper-status": operStatus,
		"conn-status": connStatus, "transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "DeviceStateUpdate")

	if err := rhp.deviceMgr.UpdateDeviceStatus(rpcCtx, deviceID.Id, voltha.OperStatus_Types(operStatus.Val),
		voltha.ConnectStatus_Types(connStatus.Val)); err != nil {
		logger.Debugw(ctx, "unable-to-update-device-status", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ChildrenStateUpdate updates child device status
func (rhp *AdapterRequestHandlerProxy) ChildrenStateUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case "connect_status":
			if err := ptypes.UnmarshalAny(arg.Value, connStatus); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-connStatus", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "children-state-update", log.Fields{"device-id": deviceID.Id, "oper-status": operStatus,
		"conn-status": connStatus, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "ChildrenStateUpdate")
	// When the enum is not set (i.e. -1), Go still convert to the Enum type with the value being -1
	if err := rhp.deviceMgr.UpdateChildrenStatus(rpcCtx, deviceID.Id, voltha.OperStatus_Types(operStatus.Val),
		voltha.ConnectStatus_Types(connStatus.Val)); err != nil {
		logger.Debugw(ctx, "unable-to-update-children-status", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PortsStateUpdate updates the ports state related to the device
func (rhp *AdapterRequestHandlerProxy) PortsStateUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	portTypeFilter := &ic.IntType{}
	operStatus := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port_type_filter":
			if err := ptypes.UnmarshalAny(arg.Value, portTypeFilter); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "ports-state-update", log.Fields{"device-id": deviceID.Id, "oper-status": operStatus, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "PortsStateUpdate")
	if err := rhp.deviceMgr.UpdatePortsState(rpcCtx, deviceID.Id, uint32(portTypeFilter.Val), voltha.OperStatus_Types(operStatus.Val)); err != nil {
		logger.Debugw(ctx, "unable-to-update-ports-state", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PortStateUpdate updates the port state of the device
func (rhp *AdapterRequestHandlerProxy) PortStateUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "oper_status":
			if err := ptypes.UnmarshalAny(arg.Value, operStatus); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-operStatus", log.Fields{"error": err})
				return nil, err
			}
		case "port_type":
			if err := ptypes.UnmarshalAny(arg.Value, portType); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-porttype", log.Fields{"error": err})
				return nil, err
			}
		case "port_no":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-portno", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "port-state-update", log.Fields{"device-id": deviceID.Id, "oper-status": operStatus,
		"port-type": portType, "port-no": portNo, "transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "PortStateUpdate")

	if err := rhp.deviceMgr.UpdatePortState(rpcCtx, deviceID.Id, voltha.Port_PortType(portType.Val), uint32(portNo.Val),
		voltha.OperStatus_Types(operStatus.Val)); err != nil {
		// If the error doesn't change behavior and is essentially ignored, it is not an error, it is a
		// warning.
		// TODO: VOL-2707
		logger.Debugw(ctx, "unable-to-update-port-state", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// DeleteAllPorts deletes all ports of device
func (rhp *AdapterRequestHandlerProxy) DeleteAllPorts(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "delete-all-ports", log.Fields{"device-id": deviceID.Id, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "DeleteAllPorts")
	if err := rhp.deviceMgr.DeleteAllPorts(rpcCtx, deviceID.Id); err != nil {
		logger.Debugw(ctx, "unable-to-delete-ports", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// GetDevicePort returns a single port
func (rhp *AdapterRequestHandlerProxy) GetDevicePort(ctx context.Context, args []*ic.Argument) (*voltha.Port, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	portNo := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port_no":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-port-no", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "get-device-port", log.Fields{"device-id": deviceID.Id, "port-no": portNo.Val, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "GetDevicePort")

	return rhp.deviceMgr.GetDevicePort(rpcCtx, deviceID.Id, uint32(portNo.Val))
}

// ListDevicePorts returns all ports belonging to the device
func (rhp *AdapterRequestHandlerProxy) ListDevicePorts(ctx context.Context, args []*ic.Argument) (*voltha.Ports, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "list-device-ports", log.Fields{"device-id": deviceID.Id, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "ListDevicePorts")
	return rhp.deviceMgr.ListDevicePorts(rpcCtx, deviceID)
}

// ChildDevicesLost indicates that a parent device is in a state (Disabled) where it cannot manage the child devices.
// This will trigger the Core to disable all the child devices.
func (rhp *AdapterRequestHandlerProxy) ChildDevicesLost(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "child-devices-lost", log.Fields{"device-id": parentDeviceID.Id, "transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "ChildDevicesLost")

	if err := rhp.deviceMgr.ChildDevicesLost(rpcCtx, parentDeviceID.Id); err != nil {
		logger.Debugw(ctx, "unable-to-disable-child-devices", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ChildDevicesDetected invoked by an adapter when child devices are found, typically after after a disable/enable sequence.
// This will trigger the Core to Enable all the child devices of that parent.
func (rhp *AdapterRequestHandlerProxy) ChildDevicesDetected(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "child-devices-detected", log.Fields{"parent-device-id": parentDeviceID.Id, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "ChildDevicesDetected")

	if err := rhp.deviceMgr.ChildDevicesDetected(rpcCtx, parentDeviceID.Id); err != nil {
		logger.Debugw(ctx, "child-devices-detection-failed", log.Fields{"parent-device-id": parentDeviceID.Id, "error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PortCreated adds port to device
func (rhp *AdapterRequestHandlerProxy) PortCreated(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	port := &voltha.Port{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port":
			if err := ptypes.UnmarshalAny(arg.Value, port); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-port", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		case "fromTopic":
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-fromTopic", log.Fields{"error": err})
				return nil, err
			}
			//log.EnrichSpan(ctx,log.Fields{"fromTopic": fromTopic})
		}
	}
	logger.Debugw(ctx, "port-created", log.Fields{"device-id": deviceID.Id, "port": port, "transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "PortCreated")
	fromTopicContext := utils.WithFromTopicMetadataContext(rpcCtx, fromTopic.Val)

	if err := rhp.deviceMgr.AddPort(fromTopicContext, deviceID.Id, port); err != nil {
		logger.Debugw(ctx, "unable-to-add-port", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// DevicePMConfigUpdate initializes the pm configs as defined by the adapter.
func (rhp *AdapterRequestHandlerProxy) DevicePMConfigUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	pmConfigs := &voltha.PmConfigs{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_pm_config":
			if err := ptypes.UnmarshalAny(arg.Value, pmConfigs); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-pm-config", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "device-pm-config-update", log.Fields{"device-id": pmConfigs.Id, "configs": pmConfigs,
		"transaction-id": transactionID.Val})
	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "DevicePMConfigUpdate")

	if err := rhp.deviceMgr.InitPmConfigs(rpcCtx, pmConfigs.Id, pmConfigs); err != nil {
		logger.Debugw(ctx, "unable-to-initialize-pm-configs", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// PacketIn sends the incoming packet of device
func (rhp *AdapterRequestHandlerProxy) PacketIn(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 4 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "port":
			if err := ptypes.UnmarshalAny(arg.Value, portNo); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-port-no", log.Fields{"error": err})
				return nil, err
			}
		case "packet":
			if err := ptypes.UnmarshalAny(arg.Value, packet); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-packet", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "packet-in", log.Fields{"device-id": deviceID.Id, "port": portNo.Val, "packet": packet,
		"transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "PacketIn")

	if err := rhp.deviceMgr.PacketIn(rpcCtx, deviceID.Id, uint32(portNo.Val), transactionID.Val, packet.Payload); err != nil {
		logger.Debugw(ctx, "unable-to-receive-packet-from-adapter", log.Fields{"error": err})
		return nil, err

	}
	return &empty.Empty{}, nil
}

// UpdateImageDownload updates image download
func (rhp *AdapterRequestHandlerProxy) UpdateImageDownload(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "image_download":
			if err := ptypes.UnmarshalAny(arg.Value, img); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-imgaeDownload", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "update-image-download", log.Fields{"device-id": deviceID.Id, "image-download": img,
		"transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "UpdateImageDownload")

	if err := rhp.deviceMgr.UpdateImageDownload(rpcCtx, deviceID.Id, img); err != nil {
		logger.Debugw(ctx, "unable-to-update-image-download", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// ReconcileChildDevices reconciles child devices
func (rhp *AdapterRequestHandlerProxy) ReconcileChildDevices(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	parentDeviceID := &voltha.ID{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "parent_device_id":
			if err := ptypes.UnmarshalAny(arg.Value, parentDeviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "reconcile-child-devices", log.Fields{"parent-device-id": parentDeviceID.Id, "transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "ReconcileChildDevices")

	if err := rhp.deviceMgr.ReconcileChildDevices(rpcCtx, parentDeviceID.Id); err != nil {
		logger.Debugw(ctx, "unable-to-reconcile-child-devices", log.Fields{"error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

// DeviceReasonUpdate updates device reason
func (rhp *AdapterRequestHandlerProxy) DeviceReasonUpdate(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "device-reason-update-invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("DeviceReasonUpdate: invalid-number-of-args")
		return nil, err
	}
	deviceID := &voltha.ID{}
	reason := &ic.StrType{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device_id":
			if err := ptypes.UnmarshalAny(arg.Value, deviceID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
				return nil, err
			}
		case "device_reason":
			if err := ptypes.UnmarshalAny(arg.Value, reason); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-reason", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-id", log.Fields{"error": err})
				return nil, err
			}
		case "fromTopic":
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-fromTopic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "device-reason-update", log.Fields{"device-id": deviceID.Id, "reason": reason.Val,
		"transaction-id": transactionID.Val})

	rpcCtx := utils.WithRPCMetadataContext(log.WithSpanFromContext(context.TODO(), ctx), "DeviceReasonUpdate")
	fromTopicContext := utils.WithFromTopicMetadataContext(rpcCtx, fromTopic.Val)

	if err := rhp.deviceMgr.UpdateDeviceReason(fromTopicContext, deviceID.Id, reason.Val); err != nil {
		logger.Debugw(ctx, "unable-to-update-device-reason", log.Fields{"error": err})
		return nil, err

	}
	return &empty.Empty{}, nil
}
