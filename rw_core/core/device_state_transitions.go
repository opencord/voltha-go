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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/voltha"
)

type DeviceType int32

const (
	parent DeviceType = 0
	child  DeviceType = 1
	any    DeviceType = 2
)

type DeviceState struct {
	Admin       voltha.AdminState_AdminState
	Connection  voltha.ConnectStatus_ConnectStatus
	Operational voltha.OperStatus_OperStatus
}

type TransitionHandler func(*voltha.Device) error

type Transition struct {
	deviceType    DeviceType
	previousState DeviceState
	currentState  DeviceState
	handlers      []TransitionHandler
}

type TransitionMap struct {
	transitions []Transition
}

func NewTransitionMap(dMgr *DeviceManager) *TransitionMap {
	var transitionMap TransitionMap
	transitionMap.transitions = make([]Transition, 0)
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.activateDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.notAllowed}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.activateDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.notAllowed}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.notAllowed}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.createLogicalDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVATING},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_ACTIVE},
			handlers:      []TransitionHandler{dMgr.addUNILogicalPort}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.deleteLogicalDevice, dMgr.disableAllChildDevices}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.deleteLogicalPort}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.abandonDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    child,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_UNKNOWN, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.notAllowed}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_PREPROVISIONED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.abandonDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_ENABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.reEnableDevice}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    parent,
			previousState: DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DELETED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.deleteAllChildDevices}})
	transitionMap.transitions = append(transitionMap.transitions,
		Transition{
			deviceType:    any,
			previousState: DeviceState{Admin: voltha.AdminState_DOWNLOADING_IMAGE, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			currentState:  DeviceState{Admin: voltha.AdminState_DISABLED, Connection: voltha.ConnectStatus_UNKNOWN, Operational: voltha.OperStatus_UNKNOWN},
			handlers:      []TransitionHandler{dMgr.notAllowed}})

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
	var exactMatch bool
	var deviceTypeMatch bool
	for _, aTransition := range tMap.transitions {
		// consider transition only if it matches deviceType or is a wild card - any
		if aTransition.deviceType != deviceType && aTransition.deviceType != any {
			continue
		}
		tempHandler, exactMatch = getHandler(pState, cState, &aTransition)
		if tempHandler != nil {
			if exactMatch {
				return tempHandler
			} else {
				if currentMatch == nil {
					currentMatch = tempHandler
				} else if aTransition.deviceType == deviceType {
					currentMatch = tempHandler
					deviceTypeMatch = true
				} else if !deviceTypeMatch {
					currentMatch = tempHandler
				}
			}
		}
	}
	return currentMatch
}
