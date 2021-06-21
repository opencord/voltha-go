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
	"github.com/opencord/voltha-lib-go/v5/pkg/adapters"
	"github.com/opencord/voltha-lib-go/v5/pkg/adapters/adapterif"
	"github.com/opencord/voltha-lib-go/v5/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v5/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/extension"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
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
	logger.Debugw(ctx, "Update_pm_config", log.Fields{"device-id": device.Id, "pmConfigs": pmConfigs})
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
				logger.Warnw(ctx, "cannot-unmarshal-device-id", log.Fields{"error": err})
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
	logger.Debugw(ctx, "Receive_packet_out", log.Fields{"device-id": deviceId.Val, "outPort": egressPort, "packet": packet})
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

	logger.Debugw(ctx, "Get_ofp_device_info", log.Fields{"device-id": device.Id})

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

func (rhp *RequestHandlerProxy) Process_tech_profile_instance_request(ctx context.Context, args []*ic.Argument) (*ic.InterAdapterTechProfileDownloadMessage, error) {
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	iaTpReqMsg := &ic.InterAdapterTechProfileInstanceRequestMessage{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "msg":
			if err := ptypes.UnmarshalAny(arg.Value, iaTpReqMsg); err != nil {
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

	logger.Debugw(ctx, "Process_tech_profile_instance_request", log.Fields{"tpPath": iaTpReqMsg.TpInstancePath})

	//Invoke the tech profile instance request
	tpInst := rhp.adapter.Process_tech_profile_instance_request(ctx, iaTpReqMsg)

	return tpInst, nil
}

func (rhp *RequestHandlerProxy) Download_image(ctx context.Context, args []*ic.Argument) (*voltha.ImageDownload, error) {
	device, image, err := unMarshalImageDowload(args, ctx)
	if err != nil {
		return nil, err
	}

	imageDownload, err := rhp.adapter.Download_image(ctx, device, image)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageDownload, nil
}

func (rhp *RequestHandlerProxy) Get_image_download_status(ctx context.Context, args []*ic.Argument) (*voltha.ImageDownload, error) {
	device, image, err := unMarshalImageDowload(args, ctx)
	if err != nil {
		return nil, err
	}

	imageDownload, err := rhp.adapter.Get_image_download_status(ctx, device, image)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageDownload, nil
}

func (rhp *RequestHandlerProxy) Cancel_image_download(ctx context.Context, args []*ic.Argument) (*voltha.ImageDownload, error) {
	device, image, err := unMarshalImageDowload(args, ctx)
	if err != nil {
		return nil, err
	}

	imageDownload, err := rhp.adapter.Cancel_image_download(ctx, device, image)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageDownload, nil
}

func (rhp *RequestHandlerProxy) Activate_image_update(ctx context.Context, args []*ic.Argument) (*voltha.ImageDownload, error) {

	device, image, err := unMarshalImageDowload(args, ctx)
	if err != nil {
		return nil, err
	}

	imageDownload, err := rhp.adapter.Activate_image_update(ctx, device, image)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageDownload, nil
}

func (rhp *RequestHandlerProxy) Revert_image_update(ctx context.Context, args []*ic.Argument) (*voltha.ImageDownload, error) {
	device, image, err := unMarshalImageDowload(args, ctx)
	if err != nil {
		return nil, err
	}

	imageDownload, err := rhp.adapter.Revert_image_update(ctx, device, image)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageDownload, nil
}

func unMarshalImageDowload(args []*ic.Argument, ctx context.Context) (*voltha.Device, *voltha.ImageDownload, error) {
	if len(args) < 4 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, nil, err
	}
	device := &voltha.Device{}
	image := &voltha.ImageDownload{}
	transactionID := &ic.StrType{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, nil, err
			}
		case "request":
			if err := ptypes.UnmarshalAny(arg.Value, image); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-image", log.Fields{"error": err})
				return nil, nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, nil, err
			}
		}
	}
	return device, image, nil
}

func (rhp *RequestHandlerProxy) Enable_port(ctx context.Context, args []*ic.Argument) error {
	logger.Debugw(ctx, "enable_port", log.Fields{"args": args})
	deviceId, port, err := rhp.getEnableDisableParams(ctx, args)
	if err != nil {
		logger.Warnw(ctx, "enable_port", log.Fields{"args": args, "device-id": deviceId, "port": port})
		return err
	}
	return rhp.adapter.Enable_port(ctx, deviceId, port)
}

func (rhp *RequestHandlerProxy) Disable_port(ctx context.Context, args []*ic.Argument) error {
	logger.Debugw(ctx, "disable_port", log.Fields{"args": args})
	deviceId, port, err := rhp.getEnableDisableParams(ctx, args)
	if err != nil {
		logger.Warnw(ctx, "disable_port", log.Fields{"args": args, "device-id": deviceId, "port": port})
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
	if len(args) < 2 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return errors.New("invalid-number-of-args")
	}
	childDevice := &voltha.Device{}
	fromTopic := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "childDevice":
			if err := ptypes.UnmarshalAny(arg.Value, childDevice); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-child-device", log.Fields{"error": err})
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
	rhp.coreProxy.UpdateCoreReference(childDevice.ParentId, fromTopic.Val)
	//Invoke the Child_device_lost API on the adapter
	if err := rhp.adapter.Child_device_lost(ctx, childDevice); err != nil {
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
				logger.Warnw(ctx, "cannot-unmarshal-parent-device-id", log.Fields{"error": err})
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

func (rhp *RequestHandlerProxy) Single_get_value_request(ctx context.Context, args []*ic.Argument) (*extension.SingleGetValueResponse, error) {
	logger.Debugw(ctx, "req handler Single_get_value_request", log.Fields{"no of args": len(args), "args": args})

	if len(args) < 1 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return nil, errors.New("invalid-number-of-args")
	}
	singleGetvalueReq := extension.SingleGetValueRequest{}
	errResp := func(status extension.GetValueResponse_Status, reason extension.GetValueResponse_ErrorReason) *extension.SingleGetValueResponse {
		return &extension.SingleGetValueResponse{
			Response: &extension.GetValueResponse{
				Status:    status,
				ErrReason: reason,
			},
		}
	}
	for _, arg := range args {
		switch arg.Key {
		case "request":
			if err := ptypes.UnmarshalAny(arg.Value, &singleGetvalueReq); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-singleGetvalueReq", log.Fields{"error": err})
				return errResp(extension.GetValueResponse_ERROR, extension.GetValueResponse_REASON_UNDEFINED), nil
			}
		default:
			logger.Warnw(ctx, "key-not-found", log.Fields{"arg.Key": arg.Key})
		}
	}
	logger.Debugw(ctx, "invoke rhp.adapter.Single_get_value_request ", log.Fields{"singleGetvalueReq": singleGetvalueReq})
	return rhp.adapter.Single_get_value_request(ctx, singleGetvalueReq)
}

func (rhp *RequestHandlerProxy) getDeviceDownloadImageRequest(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageDownloadRequest, error) {
	logger.Debugw(ctx, "getDeviceDownloadImageRequest", log.Fields{"args": args})
	if len(args) < 1 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return nil, errors.New("invalid-number-of-args")
	}

	deviceDownloadImageReq := ic.DeviceImageDownloadRequest{}

	for _, arg := range args {
		switch arg.Key {
		case "deviceImageDownloadReq":
			if err := ptypes.UnmarshalAny(arg.Value, &deviceDownloadImageReq); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	return &deviceDownloadImageReq, nil
}

func (rhp *RequestHandlerProxy) getDeviceImageRequest(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageRequest, error) {
	logger.Debugw(ctx, "getDeviceImageRequest", log.Fields{"args": args})
	if len(args) < 1 {
		logger.Warn(ctx, "invalid-number-of-args", log.Fields{"args": args})
		return nil, errors.New("invalid-number-of-args")
	}

	deviceImageReq := ic.DeviceImageRequest{}

	for _, arg := range args {
		switch arg.Key {
		case "deviceImageReq":
			if err := ptypes.UnmarshalAny(arg.Value, &deviceImageReq); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	return &deviceImageReq, nil
}

func (rhp *RequestHandlerProxy) getDeviceID(ctx context.Context, args []*ic.Argument) (string, error) {
	logger.Debugw(ctx, "getDeviceID", log.Fields{"args": args})

	deviceId := &ic.StrType{}

	for _, arg := range args {
		switch arg.Key {
		case "deviceId":
			if err := ptypes.UnmarshalAny(arg.Value, deviceId); err != nil {
				logger.Warnw(ctx, "cannot-unmarshal-device", log.Fields{"error": err})
				return "", err
			}
		}
	}

	if deviceId.Val == "" {
		return "", errors.New("invalid argument")
	}

	return deviceId.Val, nil
}

func (rhp *RequestHandlerProxy) Download_onu_image(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageResponse, error) {
	imageDownloadReq, err := rhp.getDeviceDownloadImageRequest(ctx, args)
	if err != nil {
		return nil, err
	}

	imageDownloadRsp, err := rhp.adapter.Download_onu_image(ctx, imageDownloadReq)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageDownloadRsp, nil
}

func (rhp *RequestHandlerProxy) Get_onu_image_status(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageResponse, error) {
	imageStatusReq, err := rhp.getDeviceImageRequest(ctx, args)
	if err != nil {
		return nil, err
	}

	imageStatus, err := rhp.adapter.Get_onu_image_status(ctx, imageStatusReq)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageStatus, nil
}

func (rhp *RequestHandlerProxy) Abort_onu_image_upgrade(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageResponse, error) {
	imageAbortReq, err := rhp.getDeviceImageRequest(ctx, args)
	if err != nil {
		return nil, err
	}

	imageAbortRsp, err := rhp.adapter.Abort_onu_image_upgrade(ctx, imageAbortReq)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageAbortRsp, nil
}

func (rhp *RequestHandlerProxy) Activate_onu_image(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageResponse, error) {
	imageActivateReq, err := rhp.getDeviceImageRequest(ctx, args)
	if err != nil {
		return nil, err
	}

	imageActivateRsp, err := rhp.adapter.Activate_onu_image(ctx, imageActivateReq)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return imageActivateRsp, nil
}

func (rhp *RequestHandlerProxy) Commit_onu_image(ctx context.Context, args []*ic.Argument) (*voltha.DeviceImageResponse, error) {
	imageCommitReq, err := rhp.getDeviceImageRequest(ctx, args)
	if err != nil {
		return nil, err
	}

	imageCommitRsp, err := rhp.adapter.Commit_onu_image(ctx, imageCommitReq)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	return imageCommitRsp, nil
}

func (rhp *RequestHandlerProxy) Get_onu_images(ctx context.Context, args []*ic.Argument) (*voltha.OnuImages, error) {
	deviceID, err := rhp.getDeviceID(ctx, args)
	if err != nil {
		return nil, err
	}

	onuImages, err := rhp.adapter.Get_onu_images(ctx, deviceID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return onuImages, nil
}
