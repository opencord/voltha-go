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
	"context"
	"fmt"
	"strconv"

	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

const (
	// ContextAdminState is for the admin state of the Device in the context of the event
	ContextAdminState = "admin-state"
	// ContextConnectState is for the connect state of the Device in the context of the event
	ContextConnectState = "connect-state"
	// ContextOperState is for the operational state of the Device in the context of the event
	ContextOperState = "oper-state"
	// ContextPrevdminState is for the previous admin state of the Device in the context of the event
	ContextPrevAdminState = "prev-admin-state"
	// ContextPrevConnectState is for the previous connect state of the Device in the context of the event
	ContextPrevConnectState = "prev-connect-state"
	// ContextPrevOperState is for the previous operational state of the Device in the context of the event
	ContextPrevOperState = "prev-oper-state"
	// ContextDeviceID is for the previous operational state of the Device in the context of the event
	ContextDeviceID = "id"
	// ContextParentID is for the parent id in the context of the event
	ContextParentID = "parent-id"
	// ContextSerialNumber is for the serial number of the Device in the context of the event
	ContextSerialNumber = "serial-number"
	// ContextIsRoot is for the root flag of Device in the context of the event
	ContextIsRoot = "is-root"
	// ContextParentPort is for the parent interface id of child in the context of the event
	ContextParentPort = "parent-port"
)

const (
	DeviceStateChangeEvent = "DEVICE_STATE_CHANGE"
)

var logger log.CLogger

func init() {
	// Setup this package so that it's log level can be modified at run time
	var err error
	logger, err = log.RegisterPackage(log.JSON, log.ErrorLevel, log.Fields{})
	if err != nil {
		panic(err)
	}
}

func PrepareDeviceStateEvent(ctx context.Context, serialNumber string, deviceID string, parentID string,
	prevOperStatus string, prevConnStatus string, prevAdminStatus string,
	operStatus string, connStatus string, adminStatus string,
	parentPort uint32, isRoot bool) *voltha.DeviceEvent {

	var de voltha.DeviceEvent
	context := make(map[string]string)
	/* Populating event context */
	context[ContextSerialNumber] = serialNumber
	context[ContextDeviceID] = deviceID
	context[ContextParentID] = parentID
	context[ContextPrevOperState] = prevOperStatus
	context[ContextPrevConnectState] = prevConnStatus
	context[ContextPrevAdminState] = prevAdminStatus
	context[ContextOperState] = operStatus
	context[ContextConnectState] = connStatus
	context[ContextAdminState] = adminStatus
	context[ContextIsRoot] = strconv.FormatBool(isRoot)
	context[ContextParentPort] = strconv.FormatUint(uint64(parentPort), 10)
	/* Populating device event body */
	de.Context = context
	de.ResourceId = deviceID
	de.DeviceEventName = fmt.Sprintf("%s_%s", DeviceStateChangeEvent, "RAISE_EVENT")

	return &de
}
