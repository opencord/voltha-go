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
	"context"
	"github.com/opencord/voltha-go/rw_core/coreif"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// DeviceType mentions type of device like parent, child
type DeviceType int32

const (
	parent DeviceType = 0
	child  DeviceType = 1
	any    DeviceType = 2
)

// DeviceState has admin, operational and connection status of device
type DeviceState struct {
	Admin       voltha.AdminState_Types
	Connection  voltha.ConnectStatus_Types
	Operational voltha.OperStatus_Types
}

// TransitionHandler function type which takes device as input parameter
type TransitionHandler func(context.Context, *voltha.Device) error

// Transition represent transition related attributes
type Transition struct {
	deviceType    DeviceType
	previousState DeviceState
	currentState  DeviceState
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
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.CreateLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_DISCOVERED},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.SetupUNILogicalPorts}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.SetupUNILogicalPorts}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.DisableAllChildDevices, dMgr.DeleteAllChildDevices, dMgr.DeleteLogicalDevice, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.DeleteLogicalPorts, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.DeleteLogicalPorts, dMgr.RunPostDeviceDelete}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.NotifyInvalidTransition}})

	return &transitionMap
}

func getDeviceStates(device *voltha.Device) *DeviceState {
	return &DeviceState{Admin: device.AdminState, Connection: device.ConnectStatus, Operational: device.OperStatus}
}

// isMatched matches a state transition.  It returns whether there is a match and if there is whether it is an exact match
func getHandler(previous *DeviceState, current *DeviceState, transition *Transition) ([]TransitionHandler, bool) {

	// Do we have an exact match?
	if *previous == transition.previousState && *current == transition.currentState {
		return transition.handlers, true
	}

	// Admin states must match
	if previous.Admin != transition.previousState.Admin || current.Admin != transition.currentState.Admin {
		return nil, false
	}

	// If the admin state changed then prioritize it first
	if previous.Admin != current.Admin {
		if previous.Admin == transition.previousState.Admin && current.Admin == transition.currentState.Admin {
			return transition.handlers, false
		}
	}
	// If the operational state changed then prioritize it in second position
	if previous.Operational != current.Operational {
		if previous.Operational == transition.previousState.Operational && current.Operational == transition.currentState.Operational {
			return transition.handlers, false
		}
	}
	// If the connection state changed then prioritize it in third position
	if previous.Connection != current.Connection {
		if previous.Connection == transition.previousState.Connection && current.Connection == transition.currentState.Connection {
			return transition.handlers, false
		}
	}
	return nil, false
}

// GetTransitionHandler returns transition handler
func (tMap *TransitionMap) GetTransitionHandler(pDevice *voltha.Device, cDevice *voltha.Device) []TransitionHandler {
	//1. Get the previous and current set of states
	pState := getDeviceStates(pDevice)
	cState := getDeviceStates(cDevice)
	//log.Infow("DeviceType", log.Fields{"device": pDevice})
	deviceType := parent
	if !pDevice.Root {
		log.Info("device is child")
		deviceType = child
	}
	log.Infof("deviceType:%d-deviceId:%s-previous:%v-current:%v", deviceType, pDevice.Id, pState, cState)

	//2. Go over transition array to get the right transition
	var currentMatch []TransitionHandler
	var tempHandler []TransitionHandler
	var exactStateMatch bool
	var stateMatchFound bool
	for _, aTransition := range tMap.transitions {
		// consider transition only if it matches deviceType or is a wild card - any
		if aTransition.deviceType != deviceType && aTransition.deviceType != any {
			continue
		}
		tempHandler, exactStateMatch = getHandler(pState, cState, &aTransition)
		if tempHandler != nil {
			if exactStateMatch && aTransition.deviceType == deviceType {
				return tempHandler
			} else if exactStateMatch {
				currentMatch = tempHandler
				stateMatchFound = true
			} else if currentMatch == nil && !stateMatchFound {
				currentMatch = tempHandler
			}
		}
	}
	return currentMatch
}
