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
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/protos/voltha"
)

type RequestHandlerProxy struct {
	TestMode bool
}

func (rhp *RequestHandlerProxy) GetDevice(args []*ca.Argument) (error, *voltha.Device) {
	if len(args) != 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return err, nil
	}
	pID := &ca.StrType{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err, nil
	}
	log.Debugw("GetDevice", log.Fields{"deviceId": pID.Val})
	// TODO process the request

	if rhp.TestMode { // Execute only for test cases
		return nil, &voltha.Device{Id: pID.Val}
	}
	return nil, nil
}

func (rhp *RequestHandlerProxy) GetChildDevice(args []*ca.Argument) (error, *voltha.Device) {
	if len(args) < 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return err, nil
	}
	pID := &ca.StrType{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err, nil
	}
	// TODO decompose the other parameteres for matching criteria and process
	log.Debugw("GetChildDevice", log.Fields{"deviceId": pID.Val})

	if rhp.TestMode { // Execute only for test cases
		return nil, &voltha.Device{Id: pID.Val}
	}
	return nil, nil
}

func (rhp *RequestHandlerProxy) GetPorts(args []*ca.Argument) (error, *voltha.Ports) {
	if len(args) != 2 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return err, nil
	}
	pID := &ca.StrType{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err, nil
	}
	// Porttype is an enum sent as an integer proto
	pt := &ca.IntType{}
	if err := ptypes.UnmarshalAny(args[1].Value, pt); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err, nil
	}

	// TODO decompose the other parameteres for matching criteria
	log.Debugw("GetPorts", log.Fields{"deviceID": pID.Val, "portype": pt.Val})

	if rhp.TestMode { // Execute only for test cases
		aPort := &voltha.Port{Label: "test_port"}
		allPorts := &voltha.Ports{}
		allPorts.Items = append(allPorts.Items, aPort)
		return nil, allPorts
	}
	return nil, nil

}

func (rhp *RequestHandlerProxy) GetChildDevices(args []*ca.Argument) (error, *voltha.Device) {
	if len(args) != 1 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return err, nil
	}
	pID := &ca.StrType{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err, nil
	}
	// TODO decompose the other parameteres for matching criteria and process
	log.Debugw("GetChildDevice", log.Fields{"deviceId": pID.Val})

	if rhp.TestMode { // Execute only for test cases
		return nil, &voltha.Device{Id: pID.Val}
	}
	return nil, nil
}

// ChildDeviceDetected is invoked when a child device is detected.  The following
// parameters are expected:
// {parent_device_id, parent_port_no, child_device_type, proxy_address, admin_state, **kw)
func (rhp *RequestHandlerProxy) ChildDeviceDetected(args []*ca.Argument) error {
	if len(args) < 5 {
		log.Warn("invalid-number-of-args", log.Fields{"args": args})
		err := errors.New("invalid-number-of-args")
		return err
	}

	pID := &ca.StrType{}
	if err := ptypes.UnmarshalAny(args[0].Value, pID); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err
	}
	portNo := &ca.IntType{}
	if err := ptypes.UnmarshalAny(args[1].Value, portNo); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err
	}
	dt := &ca.StrType{}
	if err := ptypes.UnmarshalAny(args[2].Value, dt); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err
	}
	pAddr := &voltha.Device_ProxyAddress{}
	if err := ptypes.UnmarshalAny(args[3].Value, pAddr); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err
	}
	adminState := &ca.IntType{}
	if err := ptypes.UnmarshalAny(args[4].Value, adminState); err != nil {
		log.Warnw("cannot-unmarshal-argument", log.Fields{"error": err})
		return err
	}

	// Need to decode the other params - in this case the key will represent the proto type
	// TODO decompose the other parameteres for matching criteria and process
	log.Debugw("ChildDeviceDetected", log.Fields{"deviceId": pID.Val, "portNo": portNo.Val,
		"deviceType": dt.Val, "proxyAddress": pAddr, "adminState": adminState})

	if rhp.TestMode { // Execute only for test cases
		return nil
	}
	return nil
}
