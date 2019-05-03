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
	"github.com/opencord/voltha-go/adapters"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/kafka"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RequestHandlerProxy struct {
	TestMode       bool
	coreInstanceId string
	adapter        adapters.IAdapter
	coreProxy      *CoreProxy
}

func NewRequestHandlerProxy(coreInstanceId string, iadapter adapters.IAdapter, cProxy *CoreProxy) *RequestHandlerProxy {
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

func (rhp *RequestHandlerProxy) Adopt_device(args []*ic.Argument) (*empty.Empty, error) {
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
	if err := rhp.adapter.Adopt_device(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Reconcile_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Abandon_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Disable_device(args []*ic.Argument) (*empty.Empty, error) {
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
	if err := rhp.adapter.Disable_device(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Reenable_device(args []*ic.Argument) (*empty.Empty, error) {
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
	if err := rhp.adapter.Reenable_device(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Reboot_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Self_test_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Delete_device(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Get_device_details(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Update_flows_bulk(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Update_flows_incrementally(args []*ic.Argument) (*empty.Empty, error) {
	log.Debug("Update_flows_incrementally")
	if len(args) < 3 {
		log.Warn("Update_flows_incrementally-invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return nil, err
	}
	device := &voltha.Device{}
	transactionID := &ic.StrType{}
	flows := &openflow_13.FlowChanges{}
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
		case kafka.TransactionKey:
			if err := ptypes.UnmarshalAny(arg.Value, transactionID); err != nil {
				log.Warnw("cannot-unmarshal-transaction-ID", log.Fields{"error": err})
				return nil, err
			}
		}
	}
	log.Debugw("Update_flows_incrementally", log.Fields{"flows": flows, "groups": groups})
	//Invoke the adopt device on the adapter
	if err := rhp.adapter.Update_flows_incrementally(device, flows, groups); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Update_pm_config(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Receive_packet_out(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Suppress_alarm(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Unsuppress_alarm(args []*ic.Argument) (*empty.Empty, error) {
	return new(empty.Empty), nil
}

func (rhp *RequestHandlerProxy) Get_ofp_device_info(args []*ic.Argument) (*ic.SwitchCapability, error) {
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
	if cap, err = rhp.adapter.Get_ofp_device_info(device); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	log.Debugw("Get_ofp_device_info", log.Fields{"cap": cap})
	return cap, nil
}

func (rhp *RequestHandlerProxy) Get_ofp_port_info(args []*ic.Argument) (*ic.PortCapability, error) {
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
	if cap, err = rhp.adapter.Get_ofp_port_info(device, pNo.Val); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}
	return cap, nil
}

func (rhp *RequestHandlerProxy) Process_inter_adapter_message(args []*ic.Argument) (*empty.Empty, error) {
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
	if err := rhp.adapter.Process_inter_adapter_message(iaMsg); err != nil {
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
