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
	"errors"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-lib-go/v3/pkg/adapters"
	"github.com/opencord/voltha-lib-go/v3/pkg/adapters/adapterif"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RequestHandlerProxy struct {
	TestMode       bool
	coreInstanceId string
	adapter        adapters.IAdapter
	coreProxy      adapterif.CoreProxy
}

func NewRequestHandlerProxy(coreInstanceId string, iadapter adapters.IAdapter, cProxy adapterif.CoreProxy) *RequestHandlerProxy {
	var proxy RequestHandlerProxy
	proxy.coreInstanceId = coreInstanceId
	proxy.adapter = iadapter
	proxy.coreProxy = cProxy
	return &proxy
}

func (rhp *RequestHandlerProxy) Adapter_descriptor() (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Device_types() (*voltha.DeviceTypes, error) {
	return nil, nil
}

func (rhp *RequestHandlerProxy) Health() (*voltha.HealthStatus, error) {
	return nil, nil
}

func (rhp *RequestHandlerProxy) Adopt_device(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}

	logger.Debugw(ctx, "Adopt_device", log.Fields{"deviceId": device.Id})

	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)

	//Invoke the adopt device on the adapter
	if err := rhp.adapter.Adopt_device(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Reconcile_device(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)

	//Invoke the reconcile device API on the adapter
	if err := rhp.adapter.Reconcile_device(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Abandon_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Disable_device(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the Disable_device API on the adapter
	if err := rhp.adapter.Disable_device(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Reenable_device(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the Reenable_device API on the adapter
	if err := rhp.adapter.Reenable_device(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Reboot_device(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the Reboot_device API on the adapter
	if err := rhp.adapter.Reboot_device(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil

}

func (rhp *RequestHandlerProxy) Self_test_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Delete_device(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the delete_device API on the adapter
	if err := rhp.adapter.Delete_device(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Get_device_details(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Update_flows_bulk(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	logger.Debug(ctx, "Update_flows_bulk")
	if len(args) < 5 {
		logger.Warn(ctx, "Update_flows_bulk-invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	flows := &voltha.Flows{}
	flowMetadata := &voltha.FlowMetadata{}
	groups := &voltha.FlowGroups{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "flows":
			if err := ptypes.UnmarshalAny(arg.Value, flows); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-flows", log.Fields{"error": err})
				return nil, err
			}
		case "groups":
			if err := ptypes.UnmarshalAny(arg.Value, groups); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-groups", log.Fields{"error": err})
				return nil, err
			}
		case "flow_metadata":
			if err := ptypes.UnmarshalAny(arg.Value, flowMetadata); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-metadata", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "Update_flows_bulk", log.Fields{"flows": flows, "groups": groups})
	//Invoke the bulk flow update API of the adapter
	if err := rhp.adapter.Update_flows_bulk(ctx, device, flows, groups, flowMetadata); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Update_flows_incrementally(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	logger.Debug(ctx, "Update_flows_incrementally")
	if len(args) < 5 {
		logger.Warn(ctx, "Update_flows_incrementally-invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	flows := &openflow_13.FlowChanges{}
	flowMetadata := &voltha.FlowMetadata{}
	groups := &openflow_13.FlowGroupChanges{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "flow_changes":
			if err := ptypes.UnmarshalAny(arg.Value, flows); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-flows", log.Fields{"error": err})
				return nil, err
			}
		case "group_changes":
			if err := ptypes.UnmarshalAny(arg.Value, groups); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-groups", log.Fields{"error": err})
				return nil, err
			}
		case "flow_metadata":
			if err := ptypes.UnmarshalAny(arg.Value, flowMetadata); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-metadata", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "Update_flows_incrementally", log.Fields{"flows": flows, "groups": groups})
	//Invoke the incremental flow update API of the adapter
	if err := rhp.adapter.Update_flows_incrementally(ctx, device, flows, groups, flowMetadata); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Update_pm_config(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	logger.Debug(ctx, "Update_pm_config")
	if len(args) < 2 {
		logger.Warn(ctx, "Update_pm_config-invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	pmConfigs := &voltha.PmConfigs{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "pm_configs":
			if err := ptypes.UnmarshalAny(arg.Value, pmConfigs); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-pm-configs", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "Update_pm_config", log.Fields{"deviceId": device.Id, "pmConfigs": pmConfigs})
	//Invoke the pm config update API of the adapter
	if err := rhp.adapter.Update_pm_config(ctx, device, pmConfigs); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Receive_packet_out(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	logger.Debugw(ctx, "Receive_packet_out", log.Fields{"args": args})
	if len(args) < 3 {
		logger.Warn(ctx, "Receive_packet_out-invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	deviceId := &ic.StrType{}
	egressPort := &ic.IntType{}
	packet := &openflow_13.OfpPacketOut{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "deviceId":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-deviceId", log.Fields{"error": err})
				return nil, err
			}
		case "outPort":
			if err := ptypes.UnmarshalAny(arg.Value, egressPort); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-egressPort", log.Fields{"error": err})
				return nil, err
			}
		case "packet":
			if err := ptypes.UnmarshalAny(arg.Value, packet); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-packet", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "Receive_packet_out", log.Fields{"deviceId": deviceId.Val, "outPort": egressPort, "packet": packet})
	//Invoke the adopt device on the adapter
	if err := rhp.adapter.Receive_packet_out(ctx, deviceId.Val, int(egressPort.Val), packet); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Suppress_alarm(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Unsuppress_alarm(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Get_ofp_device_info(ctx context.Context, args []*ic.Argument) (*ic.SwitchCapability, error) {
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
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}

	logger.Debugw(ctx, "Get_ofp_device_info", log.Fields{"deviceId": device.Id})

	var cap *ic.SwitchCapability
	var err error
	if cap, err = rhp.adapter.Get_ofp_device_info(ctx, device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	logger.Debugw(ctx, "Get_ofp_device_info", log.Fields{"cap": cap})
	return cap, nil
}

func (rhp *RequestHandlerProxy) Process_inter_adapter_message(ctx context.Context, args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	iaMsg := &ic.InterAdapterMessage{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "msg":
			if err := ptypes.UnmarshalAny(arg.Value, iaMsg); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}

	logger.Debugw(ctx, "Process_inter_adapter_message", log.Fields{"msgId": iaMsg.Header.Id})

	//Invoke the inter adapter API on the handler
	if err := rhp.adapter.Process_inter_adapter_message(ctx, iaMsg); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Download_image(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) Get_image_download_status(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) Cancel_image_download(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) Activate_image_update(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) Revert_image_update(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) Enable_port(ctx context.Context, args []*ic.Argument) error {
	logger.Debugw(ctx, "enable_port", log.Fields{"args": args})
	deviceId, port, err := rhp.getEnableDisableParams(ctx, args)
	if err != nil {
		logger.Warnw(ctx, "enable_port", log.Fields{"args": args, "deviceId": deviceId, "port": port})
		return err
	}
	return rhp.adapter.Enable_port(ctx, deviceId, port)
}

func (rhp *RequestHandlerProxy) Disable_port(ctx context.Context, args []*ic.Argument) error {
	logger.Debugw(ctx, "disable_port", log.Fields{"args": args})
	deviceId, port, err := rhp.getEnableDisableParams(ctx, args)
	if err != nil {
		logger.Warnw(ctx, "disable_port", log.Fields{"args": args, "deviceId": deviceId, "port": port})
		return err
	}
	return rhp.adapter.Disable_port(ctx, deviceId, port)
}

func (rhp *RequestHandlerProxy) getEnableDisableParams(ctx context.Context, args []*ic.Argument) (string, *voltha.Port, error) {
	logger.Debugw(ctx, "getEnableDisableParams", log.Fields{"args": args})
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return "", nil, errors.New("invalid-number-of-args")
	}
	deviceId := &ic.StrType{}
	port := &voltha.Port{}
	for _, arg := range args {
		switch arg.Key {
		case "deviceId":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return "", nil, err
			}
		case "port":
			if err := ptypes.UnmarshalAny(arg.Value, port); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-port", log.Fields{"error": err})
				return "", nil, err
			}
		}
	}
	return deviceId.Val, port, nil
}

func (rhp *RequestHandlerProxy) Child_device_lost(ctx context.Context, args []*ic.Argument) error {
	if len(args) < 4 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return errors.New("invalid-number-of-args")
	}

	pDeviceId := &ic.StrType{}
	pPortNo := &ic.IntType{}
	onuID := &ic.IntType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "pDeviceId":
			if err := ptypes.UnmarshalAny(arg.Value, pDeviceId); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-parent-deviceId", log.Fields{"error": err})
				return err
			}
		case "pPortNo":
			if err := ptypes.UnmarshalAny(arg.Value, pPortNo); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-port", log.Fields{"error": err})
				return err
			}
		case "onuID":
			if err := ptypes.UnmarshalAny(arg.Value, onuID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(pDeviceId.Val, fromTopic.Val)
	//Invoke the Child_device_lost API on the adapter
	if err := rhp.adapter.Child_device_lost(ctx, pDeviceId.Val, uint32(pPortNo.Val), uint32(onuID.Val)); err != nil {
		return status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return nil
}

func (rhp *RequestHandlerProxy) Start_omci_test(ctx context.Context, args []*ic.Argument) (*ic.TestResponse, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}

	// TODO: See related comment in voltha-go:adapter_proxy_go:startOmciTest()
	//   Second argument should perhaps be uuid instead of omcitestrequest

	device := &voltha.Device{}
	request := &voltha.OmciTestRequest{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "omcitestrequest":
			if err := ptypes.UnmarshalAny(arg.Value, request); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-omcitestrequest", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	logger.Debugw(ctx, "Start_omci_test", log.Fields{"device-id": device.Id, "req": request})
	result, err := rhp.adapter.Start_omci_test(ctx, device, request)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return result, nil
}
func (rhp *RequestHandlerProxy) Get_ext_value(ctx context.Context, args []*ic.Argument) (*voltha.ReturnValues, error) {
	if len(args) < 3 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return nil, errors.New("invalid-number-of-args")
	}

	pDeviceId := &ic.StrType{}
	device := &voltha.Device{}
	valuetype := &ic.IntType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "pDeviceId":
			if err := ptypes.UnmarshalAny(arg.Value, pDeviceId); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-parent-deviceId", log.Fields{"error": err})
				return nil, err
			}
		case "valuetype":
			if err := ptypes.UnmarshalAny(arg.Value, valuetype); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-valuetype", log.Fields{"error": err})
				return nil, err
			}
		default:
			logger.Warnw(ctx, "key-not-found", log.Fields{"arg.Key": arg.Key})
		}
	}

	//Invoke the Get_value API on the adapter
	return rhp.adapter.Get_ext_value(ctx, pDeviceId.Val, device, voltha.ValueType_Type(valuetype.Val))
}
