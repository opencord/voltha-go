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

package state

import (
	"context"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"reflect"
	"runtime"
)

// deviceType mentions type of device like parent, child
type deviceType int32

const (
	parent deviceType = 0
	child  deviceType = 1
	any    deviceType = 2
)

type matchResult uint16

const (
	noMatch            matchResult = iota // current state has not match in the transition table
	currWildcardMatch                     // current state matches the wildcard *_UNKNOWN state in the transition table
	currStateOnlyMatch                    // current state matches the current state and previous state matches the wildcard in the transition table
	currPrevStateMatch                    // both current and previous states match in the transition table
)

// match is used to keep the current match states
type match struct {
	admin, oper, conn, transient matchResult
}

// toInt returns an integer representing the matching level of the match (the larger the number the better)
func (m *match) toInt() int {
	return int(m.transient<<8 | m.admin<<4 | m.oper<<2 | m.conn)
}

// isExactMatch returns true if match is an exact match
func (m *match) isExactMatch() bool {
	return m.admin == currPrevStateMatch && m.oper == currPrevStateMatch && m.conn == currPrevStateMatch &&
		m.transient == currPrevStateMatch
}

// isBetterMatch returns true if newMatch is a worse match
func (m *match) isBetterMatch(newMatch *match) bool {
	return m.toInt() > newMatch.toInt()
}

// deviceState has admin, operational and connection status of device
type deviceState struct {
	Admin       voltha.AdminState_Types
	Connection  voltha.ConnectStatus_Types
	Operational voltha.OperStatus_Types
	Transient   voltha.DeviceTransientState_Types
}

// transitionHandler function type which takes the current and previous device info as input parameter
type transitionHandler func(context.Context, *voltha.Device) error

// transition represent transition related attributes
type transition struct {
	deviceType    deviceType
	previousState deviceState
	currentState  deviceState
	handlers      []transitionHandler
}

// TransitionMap represent map of transitions and device manager
type TransitionMap struct {
	transitions []transition
	dMgr        DeviceManager
}

// DeviceManager represents a generic device manager
type DeviceManager interface {
	NotifyInvalidTransition(ctx context.Context, curr *voltha.Device) error
	CreateLogicalDevice(ctx context.Context, curr *voltha.Device) error
	SetupUNILogicalPorts(ctx context.Context, curr *voltha.Device) error
	DeleteLogicalDevice(ctx context.Context, curr *voltha.Device) error
	DeleteLogicalPorts(ctx context.Context, curr *voltha.Device) error
	DeleteAllChildDevices(ctx context.Context, curr *voltha.Device) error
	RunPostDeviceDelete(ctx context.Context, curr *voltha.Device) error
	ChildDeviceLost(ctx context.Context, curr *voltha.Device) error
	DeleteAllLogicalPorts(ctx context.Context, curr *voltha.Device) error
	DeleteAllDeviceFlows(ctx context.Context, curr *voltha.Device) error
}

// NewTransitionMap creates transition map
func NewTransitionMap(dMgr DeviceManager) *TransitionMap {
	var transitionMap TransitionMap
	transitionMap.dMgr = dMgr
	transitionMap.transitions = make([]transition, 0)
	transitionMap.transitions = append(
		transitionMap.transitions,
		transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.CreateLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.SetupUNILogicalPorts}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.SetupUNILogicalPorts}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE PRE PROVISIONED State device forcefully
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_FORCE_DELETING},
			handlers:      []transitionHandler{dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ // DELETE PRE PROVISIONED State device no force option set
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE},
			handlers:      []transitionHandler{dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE device forcefully
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_FORCE_DELETING},
			handlers:      []transitionHandler{dMgr.DeleteAllLogicalPorts, dMgr.DeleteAllChildDevices, dMgr.DeleteLogicalDevice, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE device after adapter response
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE},
			handlers:      []transitionHandler{dMgr.DeleteAllLogicalPorts, dMgr.DeleteAllChildDevices, dMgr.DeleteLogicalDevice, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE no operation transition
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_DELETING_FROM_ADAPTER},
			handlers:      []transitionHandler{}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_REACHABLE, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNREACHABLE, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.DeleteAllLogicalPorts, dMgr.DeleteAllChildDevices, dMgr.DeleteLogicalDevice, dMgr.DeleteAllDeviceFlows}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_REACHABLE, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNREACHABLE, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.DeleteAllLogicalPorts, dMgr.DeleteAllChildDevices, dMgr.DeleteLogicalDevice, dMgr.DeleteAllDeviceFlows}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNREACHABLE, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_REACHABLE, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.CreateLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNREACHABLE, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_REACHABLE, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.CreateLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE force case
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_FORCE_DELETING},
			handlers:      []transitionHandler{dMgr.ChildDeviceLost, dMgr.DeleteLogicalPorts, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE after adapter response case
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE},
			handlers:      []transitionHandler{dMgr.ChildDeviceLost, dMgr.DeleteLogicalPorts, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{ //DELETE wait for adapter response(no operation)
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_ANY},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_DELETING_FROM_ADAPTER},
			handlers:      []transitionHandler{}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			currentState:  deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN, Transient: voltha.DeviceTransientState_NONE},
			handlers:      []transitionHandler{dMgr.NotifyInvalidTransition}})

	return &transitionMap
}

func getDeviceStates(device *voltha.Device, transientState voltha.DeviceTransientState_Types) deviceState {
	return deviceState{Admin: device.AdminState, Connection: device.ConnectStatus, Operational: device.OperStatus,
		Transient: transientState}
}

// isMatched matches a state transition.  It returns whether there is a match and if there is whether it is an exact match
func getHandler(previous deviceState, current deviceState, transition *transition) ([]transitionHandler, *match) {
	m := &match{}
	var waitForOtherStatesMatch bool
	// Do we have an exact match?
	if previous == transition.previousState && current == transition.currentState {
		return transition.handlers, &match{admin: currPrevStateMatch, oper: currPrevStateMatch, conn: currPrevStateMatch,
			transient: currPrevStateMatch}
	}
	// Do we have transient state match?
	if current.Transient == transition.currentState.Transient && transition.currentState.Transient != voltha.DeviceTransientState_ANY {
		if previous.Transient == transition.previousState.Transient || transition.previousState.Transient == voltha.DeviceTransientState_ANY {
			m.transient = currPrevStateMatch
		} else {
			m.transient = currStateOnlyMatch
		}
	} else if current.Transient == transition.currentState.Transient && transition.currentState.Transient == voltha.DeviceTransientState_ANY {
		if previous.Transient == transition.previousState.Transient || transition.previousState.Transient == voltha.DeviceTransientState_ANY {
			m.transient = currWildcardMatch
		}
	}
	if m.transient == noMatch {
		return nil, m
	}

	// Do we have Admin state match?
	if current.Admin == transition.currentState.Admin && transition.currentState.Admin != voltha.AdminState_UNKNOWN {
		if previous.Admin == transition.previousState.Admin {
			m.admin = currPrevStateMatch
		} else if transition.previousState.Admin == voltha.AdminState_UNKNOWN {
			m.admin = currStateOnlyMatch
		}
	} else if current.Admin == transition.currentState.Admin && transition.currentState.Admin == voltha.AdminState_UNKNOWN {
		if previous.Admin == transition.previousState.Admin || transition.previousState.Admin == voltha.AdminState_UNKNOWN {
			m.admin = currWildcardMatch
		}
	} else if transition.previousState.Admin == voltha.AdminState_UNKNOWN && transition.currentState.Admin == voltha.AdminState_UNKNOWN {
		// Will only be the case of DELETION currently.(to allow only transient match, we can avoid this check if
		// we can allow wild card in admin state)
		waitForOtherStatesMatch = true
	}
	if !waitForOtherStatesMatch && m.admin == noMatch {
		return nil, m
	}
	// Do we have an operational state match?
	if current.Operational == transition.currentState.Operational && transition.previousState.Operational != voltha.OperStatus_UNKNOWN {
		if previous.Operational == transition.previousState.Operational || transition.previousState.Operational == voltha.OperStatus_UNKNOWN {
			m.oper = currPrevStateMatch
		} else {
			m.oper = currStateOnlyMatch
		}
	} else if current.Operational == transition.currentState.Operational && transition.previousState.Operational == voltha.OperStatus_UNKNOWN {
		if previous.Operational == transition.previousState.Operational || transition.previousState.Operational == voltha.OperStatus_UNKNOWN {
			m.oper = currWildcardMatch
		}
	}

	// Do we have an connection state match?
	if current.Connection == transition.currentState.Connection && transition.previousState.Connection != voltha.ConnectStatus_UNKNOWN {
		if previous.Connection == transition.previousState.Connection || transition.previousState.Connection == voltha.ConnectStatus_UNKNOWN {
			m.conn = currPrevStateMatch
		} else {
			m.conn = currStateOnlyMatch
		}
	} else if current.Connection == transition.currentState.Connection && transition.previousState.Connection == voltha.ConnectStatus_UNKNOWN {
		if previous.Connection == transition.previousState.Connection || transition.previousState.Connection == voltha.ConnectStatus_UNKNOWN {
			m.conn = currWildcardMatch
		}
	}
	return transition.handlers, m
}

// getTransitionHandler returns transition handler & a flag that's set if the transition is invalid
func (tMap *TransitionMap) getTransitionHandler(ctx context.Context, cDevice, pDevice *voltha.Device,
	cTransientState, pTransientState voltha.DeviceTransientState_Types) []transitionHandler {
	//1. Get the previous and current set of states
	cState := getDeviceStates(cDevice, cTransientState)
	pState := getDeviceStates(pDevice, pTransientState)

	// Do nothing is there are no states change
	if pState == cState {
		return nil
	}

	//logger.Infow(ctx, "deviceType", log.Fields{"device": pDevice})
	deviceType := parent
	if !cDevice.Root {
		logger.Info(ctx, "device is child")
		deviceType = child
	}
	logger.Infof(ctx, "deviceType:%d-deviceId:%s-previous:%v-current:%v", deviceType, cDevice.Id, pState, cState)

	//2. Go over transition array to get the right transition
	var currentMatch []transitionHandler
	var tempHandler []transitionHandler
	var m *match
	bestMatch := &match{}
	for _, aTransition := range tMap.transitions {
		// consider transition only if it matches deviceType or is a wild card - any
		if aTransition.deviceType != deviceType && aTransition.deviceType != any {
			continue
		}
		tempHandler, m = getHandler(pState, cState, &aTransition)
		if tempHandler != nil {
			if m.isExactMatch() && aTransition.deviceType == deviceType {
				return tempHandler
			} else if m.isExactMatch() || m.isBetterMatch(bestMatch) {
				currentMatch = tempHandler
				bestMatch = m
			}
		}
	}
	return currentMatch
}

func (tMap *TransitionMap) ProcessTransition(ctx context.Context, device, prevDevice *voltha.Device,
	deviceTransientState, prevDeviceTransientState voltha.DeviceTransientState_Types) error {
	// This will be triggered on every state update
	logger.Debugw(ctx, "state-transition", log.Fields{
		"device":               device.Id,
		"prev-admin-state":     prevDevice.AdminState,
		"prev-oper-state":      prevDevice.OperStatus,
		"prev-conn-state":      prevDevice.ConnectStatus,
		"curr-admin-state":     device.AdminState,
		"curr-oper-state":      device.OperStatus,
		"curr-conn-state":      device.ConnectStatus,
		"curr-transient-state": deviceTransientState,
		"prev-transient-state": prevDeviceTransientState,
	})
	handlers := tMap.getTransitionHandler(ctx, device, prevDevice, deviceTransientState, prevDeviceTransientState)
	if handlers == nil {
		logger.Debugw(ctx, "no-op-transition", log.Fields{"deviceId": device.Id})
		return nil
	}
	logger.Debugw(ctx, "handler-found", log.Fields{"num-expectedHandlers": len(handlers), "isParent": device.Root,
		"current-data": device, "previous-data": prevDevice})
	for _, handler := range handlers {
		logger.Debugw(ctx, "running-handler", log.Fields{"handler": funcName(handler)})
		if err := handler(ctx, device); err != nil {
			logger.Warnw(ctx, "handler-failed", log.Fields{"handler": funcName(handler), "error": err})
			return err
		}
	}
	return nil
}

func funcName(f interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}
