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
	"errors"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-lib-go/v2/pkg/adapters"
	"github.com/opencord/voltha-lib-go/v2/pkg/adapters/adapterif"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	ic "github.com/opencord/voltha-protos/v2/go/inter_container"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
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

func (rhp *RequestHandlerProxy) AdapterDescriptor() (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) DeviceTypes() (*voltha.DeviceTypes, error) {
	return nil, nil
}

func (rhp *RequestHandlerProxy) Health() (*voltha.HealthStatus, error) {
	return nil, nil
}

func (rhp *RequestHandlerProxy) AdoptDevice(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				log.Warnw("cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}

	log.Debugw("Adopt_device", log.Fields{"deviceId": device.Id})

	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)

	//Invoke the adopt device on the adapter
	if err := rhp.adapter.AdoptDevice(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) ReconcileDevice(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				log.Warnw("cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)

	//Invoke the reconcile device API on the adapter
	if err := rhp.adapter.ReconcileDevice(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) AbandonDevice(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) DisableDevice(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				log.Warnw("cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the Disable_device API on the adapter
	if err := rhp.adapter.DisableDevice(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) ReenableDevice(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				log.Warnw("cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the Reenable_device API on the adapter
	if err := rhp.adapter.ReenableDevice(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) RebootDevice(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				log.Warnw("cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the Reboot_device API on the adapter
	if err := rhp.adapter.RebootDevice(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil

}

func (rhp *RequestHandlerProxy) SelfTestDevice(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) DeleteDevice(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		case kafka.FromTopic:
			if err := ptypes.UnmarshalAny(arg.Value, fromTopic); err != nil {
				log.Warnw("cannot-unmarshal-from-topic", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	//Update the core reference for that device
	rhp.coreProxy.UpdateCoreReference(device.Id, fromTopic.Val)
	//Invoke the delete_device API on the adapter
	if err := rhp.adapter.DeleteDevice(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) GetDeviceDetails(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) UpdateFlowsBulk(args []*ic.Argument) (*empty.Empty, error) {
	log.Debug("Update_flows_bulk")
	if len(args) < 5 {
		log.Warn("Update_flows_bulk-invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "flows":
			if err := ptypes.UnmarshalAny(arg.Value, flows); err != nil {
				log.Warnw("cannot-unmarshal-flows", log.Fields{"error": err})
				return nil, err
			}
		case "groups":
			if err := ptypes.UnmarshalAny(arg.Value, groups); err != nil {
				log.Warnw("cannot-unmarshal-groups", log.Fields{"error": err})
				return nil, err
			}
		case "flow_metadata":
			if err := ptypes.UnmarshalAny(arg.Value, flowMetadata); err != nil {
				log.Warnw("cannot-unmarshal-metadata", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Update_flows_bulk", log.Fields{"flows": flows, "groups": groups})
	//Invoke the bulk flow update API of the adapter
	if err := rhp.adapter.UpdateFlowsBulk(device, flows, groups, flowMetadata); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) UpdateFlowsIncrementally(args []*ic.Argument) (*empty.Empty, error) {
	log.Debug("Update_flows_incrementally")
	if len(args) < 5 {
		log.Warn("Update_flows_incrementally-invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "flow_changes":
			if err := ptypes.UnmarshalAny(arg.Value, flows); err != nil {
				log.Warnw("cannot-unmarshal-flows", log.Fields{"error": err})
				return nil, err
			}
		case "group_changes":
			if err := ptypes.UnmarshalAny(arg.Value, groups); err != nil {
				log.Warnw("cannot-unmarshal-groups", log.Fields{"error": err})
				return nil, err
			}
		case "flow_metadata":
			if err := ptypes.UnmarshalAny(arg.Value, flowMetadata); err != nil {
				log.Warnw("cannot-unmarshal-metadata", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Update_flows_incrementally", log.Fields{"flows": flows, "groups": groups})
	//Invoke the incremental flow update API of the adapter
	if err := rhp.adapter.UpdateFlowsIncrementally(device, flows, groups, flowMetadata); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) UpdatePmConfig(args []*ic.Argument) (*empty.Empty, error) {
	log.Debug("Update_pm_config")
	if len(args) < 2 {
		log.Warn("Update_pm_config-invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "pm_configs":
			if err := ptypes.UnmarshalAny(arg.Value, pmConfigs); err != nil {
				log.Warnw("cannot-unmarshal-pm-configs", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Update_pm_config", log.Fields{"deviceId": device.Id, "pmConfigs": pmConfigs})
	//Invoke the pm config update API of the adapter
	if err := rhp.adapter.UpdatePmConfig(device, pmConfigs); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) ReceivePacketOut(args []*ic.Argument) (*empty.Empty, error) {
	log.Debugw("Receive_packet_out", log.Fields{"args": args})
	if len(args) < 3 {
		log.Warn("Receive_packet_out-invalid-number-of-args", log.Fields{"args": args})
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
				log.Warnw("cannot-unmarshal-deviceId", log.Fields{"error": err})
				return nil, err
			}
		case "outPort":
			if err := ptypes.UnmarshalAny(arg.Value, egressPort); err != nil {
				log.Warnw("cannot-unmarshal-egressPort", log.Fields{"error": err})
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
	log.Debugw("Receive_packet_out", log.Fields{"deviceId": deviceId.Val, "outPort": egressPort, "packet": packet})
	//Invoke the adopt device on the adapter
	if err := rhp.adapter.ReceivePacketOut(deviceId.Val, int(egressPort.Val), packet); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) SuppressAlarm(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) UnsuppressAlarm(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) GetOfpDeviceInfo(args []*ic.Argument) (*ic.SwitchCapability, error) {
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
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}

	log.Debugw("Get_ofp_device_info", log.Fields{"deviceId": device.Id})

	var cap *ic.SwitchCapability
	var err error
	if cap, err = rhp.adapter.GetOfpDeviceInfo(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	log.Debugw("Get_ofp_device_info", log.Fields{"cap": cap})
	return cap, nil
}

func (rhp *RequestHandlerProxy) GetOfpPortInfo(args []*ic.Argument) (*ic.PortCapability, error) {
	if len(args) < 3 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	pNo := &ic.IntType{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "device":
			if err := ptypes.UnmarshalAny(arg.Value, device); err != nil {
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case "port_no":
			if err := ptypes.UnmarshalAny(arg.Value, pNo); err != nil {
				log.Warnw("cannot-unmarshal-port-no", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Get_ofp_port_info", log.Fields{"deviceId": device.Id, "portNo": pNo.Val})
	var cap *ic.PortCapability
	var err error
	if cap, err = rhp.adapter.GetOfpPortInfo(device, pNo.Val); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return cap, nil
}

func (rhp *RequestHandlerProxy) ProcessInterAdapterMessage(args []*ic.Argument) (*empty.Empty, error) {
	if len(args) < 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	iaMsg := &ic.InterAdapterMessage{}
	transactionID := &ic.StrType{}
	for _, arg := range args {
		switch arg.Key {
		case "msg":
			if err := ptypes.UnmarshalAny(arg.Value, iaMsg); err != nil {
				log.Warnw("cannot-unmarshal-device", log.Fields{"error": err})
				return nil, err
			}
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}

	log.Debugw("Process_inter_adapter_message", log.Fields{"msgId": iaMsg.Header.Id})

	//Invoke the inter adapter API on the handler
	if err := rhp.adapter.ProcessInterAdapterMessage(iaMsg); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) DownloadImage(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) GetImageDownloadStatus(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) CancelImageDownload(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) ActivateImageUpdate(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (rhp *RequestHandlerProxy) RevertImageUpdate(args []*ic.Argument) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}
