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

package device

import (
	"context"
	"github.com/opencord/voltha-go/rw_core/coreif"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// deviceType mentions type of device like parent, child
type deviceType int32

const (
	parent deviceType = 0
	child  deviceType = 1
	any    deviceType = 2
)

type matchResult uint8

const (
	noMatch            matchResult = iota // current state has not match in the transition table
	currWildcardMatch                     // current state matches the wildcard *_UNKNOWN state in the transition table
	currStateOnlyMatch                    // current state matches the current state and previous state matches the wildcard in the transition table
	currPrevStateMatch                    // both current and previous states match in the transition table
)

// match is used to keep the current match states
type match struct {
	admin, oper, conn matchResult
}

// toInt returns an integer representing the matching level of the match (the larger the number the better)
func (m *match) toInt() int {
	return int(m.admin<<4 | m.oper<<2 | m.conn)
}

// isExactMatch returns true if match is an exact match
func (m *match) isExactMatch() bool {
	return m.admin == currPrevStateMatch && m.oper == currPrevStateMatch && m.conn == currPrevStateMatch
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
}

// TransitionHandler function type which takes the current and previous device info as input parameter
type TransitionHandler func(context.Context, *voltha.Device) error

// Transition represent transition related attributes
type Transition struct {
	deviceType    deviceType
	previousState deviceState
	currentState  deviceState
	handlers      []TransitionHandler
}

// TransitionMap represent map of transitions and device manager
type TransitionMap struct {
	transitions []Transition
	dMgr        coreif.DeviceManager
}

// NewTransitionMap creates transition map
func NewTransitionMap(dMgr coreif.DeviceManager) *TransitionMap {
	var transitionMap TransitionMap
	transitionMap.dMgr = dMgr
	transitionMap.transitions = make([]Transition, 0)
	transitionMap.transitions = append(
		transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.CreateLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			handlers:      []TransitionHandler{}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.SetupUNILogicalPorts}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED},
			handlers:      []TransitionHandler{}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.SetupUNILogicalPorts}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.DisableAllChildDevices, dMgr.DeleteAllUNILogicalPorts, dMgr.DeleteAllChildDevices, dMgr.DeleteLogicalDevice, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_REACHABLE, Operational: voltha.OperStatus_ACTIVE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNREACHABLE, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.DeleteAllLogicalPorts, dMgr.DeleteLogicalDevice, dMgr.DeleteAllChildDevices, dMgr.DeleteAllDeviceFlows}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNREACHABLE, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_REACHABLE, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.CreateLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.ChildDeviceLost, dMgr.DeleteLogicalPorts, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.ChildDeviceLost, dMgr.DeleteLogicalPorts, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: deviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  deviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})

	return &transitionMap
}

func getDeviceStates(device *voltha.Device) *deviceState {
	return &deviceState{Admin: device.AdminState, Connection: device.ConnectStatus, Operational: device.OperStatus}
}

// isMatched matches a state transition.  It returns whether there is a match and if there is whether it is an exact match
func getHandler(previous *deviceState, current *deviceState, transition *Transition) ([]TransitionHandler, *match) {
	m := &match{}
	// Do we have an exact match?
	if *previous == transition.previousState && *current == transition.currentState {
		return transition.handlers, &match{admin: currPrevStateMatch, oper: currPrevStateMatch, conn: currPrevStateMatch}
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
	}
	if m.admin == noMatch {
		// invalid transition - need to match on current admin state
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

// GetTransitionHandler returns transition handler
func (tMap *TransitionMap) GetTransitionHandler(device *voltha.Device, pState *deviceState) []TransitionHandler {
	//1. Get the previous and current set of states
	cState := getDeviceStates(device)

	// Do nothing is there are no states change
	if *pState == *cState {
		return nil
	}

	//logger.Infow("deviceType", log.Fields{"device": pDevice})
	deviceType := parent
	if !device.Root {
		logger.Info("device is child")
		deviceType = child
	}
	logger.Infof("deviceType:%d-deviceId:%s-previous:%v-current:%v", deviceType, device.Id, pState, cState)

	//2. Go over transition array to get the right transition
	var currentMatch []TransitionHandler
	var tempHandler []TransitionHandler
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
