/*
 * Copyright 2020-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package events

import (
	"fmt"
	"strconv"

	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

type ContextType string

const (
	// ContextAdminState is for the admin state of the Device in the context of the event
	ContextAdminState ContextType = "admin-state"
	// ContextConnectState is for the connect state of the Device in the context of the event
	ContextConnectState ContextType = "connect-state"
	// ContextOperState is for the operational state of the Device in the context of the event
	ContextOperState ContextType = "oper-state"
	// ContextPrevdminState is for the previous admin state of the Device in the context of the event
	ContextPrevAdminState ContextType = "prev-admin-state"
	// ContextPrevConnectState is for the previous connect state of the Device in the context of the event
	ContextPrevConnectState ContextType = "prev-connect-state"
	// ContextPrevOperState is for the previous operational state of the Device in the context of the event
	ContextPrevOperState ContextType = "prev-oper-state"
	// ContextDeviceID is for the previous operational state of the Device in the context of the event
	ContextDeviceID ContextType = "id"
	// ContextParentID is for the parent id in the context of the event
	ContextParentID ContextType = "parent-id"
	// ContextSerialNumber is for the serial number of the Device in the context of the event
	ContextSerialNumber ContextType = "serial-number"
	// ContextIsRoot is for the root flag of Device in the context of the event
	ContextIsRoot ContextType = "is-root"
	// ContextParentPort is for the parent interface id of child in the context of the event
	ContextParentPort ContextType = "parent-port"
	//ContextPonID is the PON ID the Child device is connected to
	ContextPonID ContextType = "pon-id"
	//ContextOnuID is the Onu ID of the child device
	ContextOnuID ContextType = "onu-id"
	//ContextParentSerialNum is the serila number of the parent device
	ContextParentSerialNum ContextType = "olt-serial-number"
)

type EventName string

const (
	DeviceStateChangeEvent EventName = "DEVICE_STATE_CHANGE"
	OltDeviceStateDeleted  EventName = "OLT_DELETED_RAISE_EVENT"
	OnuDeviceStateDeleted  EventName = "ONU_DELETED_RAISE_EVENT"
)

type EventAction string

const (
	Raise EventAction = "RAISE_EVENT"
	Clear EventAction = "CLEAR_EVENT"
)

//CreateDeviceStateChangeEvent forms and returns a new DeviceStateChange Event
func CreateDeviceStateChangeEvent(serialNumber string, deviceID string, parentID string,
	prevOperStatus common.OperStatus_Types, prevConnStatus common.ConnectStatus_Types, prevAdminStatus common.AdminState_Types,
	operStatus common.OperStatus_Types, connStatus common.ConnectStatus_Types, adminStatus common.AdminState_Types,
	parentPort uint32, isRoot bool) *voltha.DeviceEvent {

	context := make(map[string]string)
	/* Populating event context */
	context[string(ContextSerialNumber)] = serialNumber
	context[string(ContextDeviceID)] = deviceID
	context[string(ContextParentID)] = parentID
	context[string(ContextPrevOperState)] = prevOperStatus.String()
	context[string(ContextPrevConnectState)] = prevConnStatus.String()
	context[string(ContextPrevAdminState)] = prevAdminStatus.String()
	context[string(ContextOperState)] = operStatus.String()
	context[string(ContextConnectState)] = connStatus.String()
	context[string(ContextAdminState)] = adminStatus.String()
	context[string(ContextIsRoot)] = strconv.FormatBool(isRoot)
	context[string(ContextParentPort)] = strconv.FormatUint(uint64(parentPort), 10)

	return &voltha.DeviceEvent{
		Context:         context,
		ResourceId:      deviceID,
		DeviceEventName: fmt.Sprintf("%s_%s", string(DeviceStateChangeEvent), string(Raise)),
	}
}

//CreateDeviceDeletedEvent forms and returns a new DeviceState Event
func CreateDeviceDeletedEvent(serialNumber string, deviceID string, parentID string,
	onuId uint32, parentPonPort uint32, isRoot bool) *voltha.DeviceEvent {

	context := make(map[string]string)
	eventName := string(OltDeviceStateDeleted)
	/* Populating event context */
	context[string(ContextSerialNumber)] = serialNumber
	context[string(ContextDeviceID)] = deviceID
	if !isRoot {
		context[string(ContextPonID)] = strconv.FormatUint(uint64(parentPonPort), 10)
		context[string(ContextOnuID)] = strconv.FormatUint(uint64(onuId), 10)
		context[string(ContextParentID)] = parentID
		eventName = string(OnuDeviceStateDeleted)

	}

	return &voltha.DeviceEvent{
		Context:         context,
		ResourceId:      deviceID,
		DeviceEventName: eventName,
	}
}
