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
package state

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/stretchr/testify/assert"
)

var transitionMap *TransitionMap
var tdm DeviceManager

type testDeviceManager struct {
	DeviceManager
}

func newTestDeviceManager() *testDeviceManager {
	return &testDeviceManager{}
}

func init() {
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

func assertInvalidTransition(t *testing.T, device, prevDevice *voltha.Device) {
	handlers := transitionMap.getTransitionHandler(context.Background(), device, prevDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.NotifyInvalidTransition).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
}

func assertNoOpTransition(t *testing.T, device, prevDevice *voltha.Device) {
	handlers := transitionMap.getTransitionHandler(context.Background(), device, prevDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 0, len(handlers))
}

func TestValidTransitions(t *testing.T) {
	ctx := context.Background()

	previousDevice := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers := transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_FORCE_DELETING,
		voltha.DeviceTransientState_ANY)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE,
		voltha.DeviceTransientState_ANY)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_FORCE_DELETING,
		voltha.DeviceTransientState_ANY)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	device = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_FAILED)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_FORCE_DELETING,
		voltha.DeviceTransientState_ANY)
	assert.Equal(t, 3, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.ChildDeviceLost).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalPorts).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[2]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 4, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.DeleteAllLogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteAllChildDevices).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalDevice).Pointer() == reflect.ValueOf(handlers[2]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteAllDeviceFlows).Pointer() == reflect.ValueOf(handlers[3]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 4, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.DeleteAllLogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteAllChildDevices).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalDevice).Pointer() == reflect.ValueOf(handlers[2]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteAllDeviceFlows).Pointer() == reflect.ValueOf(handlers[3]).Pointer())

	previousDevice = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_NONE,
		voltha.DeviceTransientState_NONE)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	var deleteDeviceTest = struct {
		previousDevices        []*voltha.Device
		devices                []*voltha.Device
		expectedParentHandlers []transitionHandler
		expectedChildHandlers  []transitionHandler
	}{
		previousDevices: []*voltha.Device{
			getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_FAILED),
			getDevice(false, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVE),
			getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN),
		},
		devices: []*voltha.Device{
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_UNKNOWN),
			getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNREACHABLE, voltha.OperStatus_FAILED),
		},
		expectedParentHandlers: []transitionHandler{
			tdm.DeleteAllLogicalPorts,
			tdm.DeleteAllChildDevices,
			tdm.DeleteLogicalDevice,
			tdm.RunPostDeviceDelete,
		},
		expectedChildHandlers: []transitionHandler{
			tdm.ChildDeviceLost,
			tdm.DeleteLogicalPorts,
			tdm.RunPostDeviceDelete,
		},
	}

	testName := "delete-parent-device-post-provisioning"
	for _, previousDevice := range deleteDeviceTest.previousDevices {
		for _, device := range deleteDeviceTest.devices {
			device.Root = true
			t.Run(testName, func(t *testing.T) {
				handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_FORCE_DELETING,
					voltha.DeviceTransientState_ANY)
				assert.Equal(t, 4, len(handlers))
				for idx, expHandler := range deleteDeviceTest.expectedParentHandlers {
					assert.True(t, reflect.ValueOf(expHandler).Pointer() == reflect.ValueOf(handlers[idx]).Pointer())
				}
			})
		}
	}

	testName = "delete-child-device"
	for _, previousDevice := range deleteDeviceTest.previousDevices {
		for _, device := range deleteDeviceTest.devices {
			device.Root = false
			t.Run(testName, func(t *testing.T) {
				handlers = transitionMap.getTransitionHandler(ctx, device, previousDevice, voltha.DeviceTransientState_FORCE_DELETING,
					voltha.DeviceTransientState_ANY)
				assert.Equal(t, 3, len(handlers))
				for idx, expHandler := range deleteDeviceTest.expectedChildHandlers {
					assert.True(t, reflect.ValueOf(expHandler).Pointer() == reflect.ValueOf(handlers[idx]).Pointer())
				}
			})
		}
	}

}

func TestInvalidTransitions(t *testing.T) {
	previousDevice := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	device := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	assertInvalidTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, device, previousDevice)
}

func TestNoOpTransitions(t *testing.T) {

	previousDevice := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousDevice)

	previousDevice = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousDevice)

	previousDevice = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	device = getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, device, previousDevice)

	previousDevice = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	device = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	assertNoOpTransition(t, device, previousDevice)

	previousDevice = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	device = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVATING)
	assertNoOpTransition(t, device, previousDevice)
}

func TestMatch(t *testing.T) {
	best := &match{admin: currPrevStateMatch, oper: currPrevStateMatch, conn: currPrevStateMatch, transient: currWildcardMatch}
	m := &match{admin: currStateOnlyMatch, oper: currWildcardMatch, conn: currWildcardMatch, transient: currWildcardMatch}
	fmt.Println(m.isBetterMatch(best), m.toInt(), best.toInt())
}
