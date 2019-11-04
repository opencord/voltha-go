/*
 * Copyright 2019-present Open Networking Foundation
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
package core

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/opencord/voltha-go/rw_core/coreif"
	"github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
)

var transitionMap *TransitionMap
var tdm coreif.DeviceManager

type testDeviceManager struct {
	mocks.DeviceManager
}

func newTestDeviceManager() *testDeviceManager {
	return &testDeviceManager{}
}

func init() {
	if _, err := log.AddPackage(log.JSON, log.WarnLevel, nil); err != nil {
		log.Fatal("failure-adding-package-core")
	}
	tdm = newTestDeviceManager()
	transitionMap = NewTransitionMap(tdm)
}

func getDevice(root bool, admin voltha.AdminState_Types, conn voltha.ConnectStatus_Types, oper voltha.OperStatus_Types) *voltha.Device {
	return &voltha.Device{
		Id:            "test",
		Root:          root,
		AdminState:    admin,
		ConnectStatus: conn,
		OperStatus:    oper,
	}
}

func getDeviceState(admin voltha.AdminState_Types, conn voltha.ConnectStatus_Types, oper voltha.OperStatus_Types) *DeviceState {
	return &DeviceState{
		Admin:       admin,
		Connection:  conn,
		Operational: oper,
	}
}

func assertInvalidTransition(t *testing.T, device *voltha.Device, previousState *DeviceState) {
	handlers := transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.NotifyInvalidTransition).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
}

func assertNoOpTransition(t *testing.T, device *voltha.Device, previousState *DeviceState) {
	handlers := transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 0, len(handlers))
}

func TestValidTransitions(t *testing.T) {
	previousState := getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers := transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DELETED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousState = getDeviceState(voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(false, voltha.AdminState_DELETED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.GetTransitionHandler(device, previousState)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	from = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)

	to = getDevice(false, voltha.AdminState_DELETED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_FAILED)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 3, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.ChildDeviceLost).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalPorts).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[2]).Pointer())

	from = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	to = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 4, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.DeleteAllLogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalDevice).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteAllChildDevices).Pointer() == reflect.ValueOf(handlers[2]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteAllDeviceFlows).Pointer() == reflect.ValueOf(handlers[3]).Pointer())

	from = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	var deleteDeviceTest = struct {
		from                   []*voltha.Device
		to                     []*voltha.Device
		expectedParentHandlers []TransitionHandler
		expectedChildHandlers  []TransitionHandler
	}{
		from: []*voltha.Device{
			getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_FAILED),
			getDevice(false, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE),
			getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN),
		},
		to: []*voltha.Device{
			getDevice(false, voltha.AdminState_DELETED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_DELETED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_DELETED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_FAILED),
		},
		expectedParentHandlers: []TransitionHandler{
			tdm.DisableAllChildDevices,
			tdm.DeleteAllUNILogicalPorts,
			tdm.DeleteAllChildDevices,
			tdm.DeleteLogicalDevice,
			tdm.RunPostDeviceDelete,
		},
		expectedChildHandlers: []TransitionHandler{
			tdm.ChildDeviceLost,
			tdm.DeleteLogicalPorts,
			tdm.RunPostDeviceDelete,
		},
	}

	testName := "delete-parent-device-post-provisioning"
	for _, from := range deleteDeviceTest.from {
		from.Root = true
		for _, to := range deleteDeviceTest.to {
			to.Root = true
			t.Run(testName, func(t *testing.T) {
				handlers = transitionMap.GetTransitionHandler(from, to)
				assert.Equal(t, 5, len(handlers))
				for idx, expHandler := range deleteDeviceTest.expectedParentHandlers {
					assert.True(t, reflect.ValueOf(expHandler).Pointer() == reflect.ValueOf(handlers[idx]).Pointer())
				}
			})
		}
	}

	testName = "delete-child-device"
	for _, from := range deleteDeviceTest.from {
		from.Root = false
		for _, to := range deleteDeviceTest.to {
			to.Root = false
			t.Run(testName, func(t *testing.T) {
				handlers = transitionMap.GetTransitionHandler(from, to)
				assert.Equal(t, 3, len(handlers))
				for idx, expHandler := range deleteDeviceTest.expectedChildHandlers {
					assert.True(t, reflect.ValueOf(expHandler).Pointer() == reflect.ValueOf(handlers[idx]).Pointer())
				}
			})
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	previousState := getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	device := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	assertInvalidTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousState)
}

func TestNoOpTransitions(t *testing.T) {
	previousState := getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	assertNoOpTransition(t, device, previousState)

	previousState = getDeviceState(voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVATING)
	assertNoOpTransition(t, device, previousState)
}

func TestMatch(t *testing.T) {
	best := &match{admin: currPrevStateMatch, oper: currPrevStateMatch, conn: currPrevStateMatch}
	m := &match{admin: currStateOnlyMatch, oper: currWildcardMatch, conn: currWildcardMatch}
	fmt.Println(m.isBetterMatch(best), m.toInt(), best.toInt())
}
