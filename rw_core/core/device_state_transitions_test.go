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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/rw_core/coreIf"
	"github.com/opencord/voltha-protos/go/voltha"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

var transitionMap *TransitionMap
var tdm coreIf.DeviceManager

type testDeviceManager struct {
}

func newTestDeviceManager() *testDeviceManager {
	return &testDeviceManager{}
}

func (tdm *testDeviceManager) GetDevice(string) (*voltha.Device, error) {
	return nil, nil
}

func (tdm *testDeviceManager) IsRootDevice(string) (bool, error) {
	return false, nil
}

func (tdm *testDeviceManager) NotifyInvalidTransition(pto *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) SetAdminStateToEnable(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) CreateLogicalDevice(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) SetupUNILogicalPorts(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DisableAllChildDevices(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DeleteLogicalDevice(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DeleteLogicalPorts(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) DeleteAllChildDevices(to *voltha.Device) error {
	return nil
}

func (tdm *testDeviceManager) RunPostDeviceDelete(to *voltha.Device) error {
	return nil
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
	//log.UpdateAllLoggers(log.Fields{"instanceId": "device-state-transition"})
	//log.SetAllLogLevel(log.DebugLevel)
	tdm = newTestDeviceManager()
	transitionMap = NewTransitionMap(tdm)
}

func getDevice(root bool, admin voltha.AdminState_AdminState, conn voltha.ConnectStatus_ConnectStatus, oper voltha.OperStatus_OperStatus) *voltha.Device {
	return &voltha.Device{
		Id:            "test",
		Root:          root,
		AdminState:    admin,
		ConnectStatus: conn,
		OperStatus:    oper,
	}
}

func assertInvalidTransition(t *testing.T, from *voltha.Device, to *voltha.Device) {
	handlers := transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.NotifyInvalidTransition).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
}

func assertNoOpTransition(t *testing.T, from *voltha.Device, to *voltha.Device) {
	handlers := transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 0, len(handlers))
}

func TestValidTransitions(t *testing.T) {
	from := getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	to := getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers := transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetAdminStateToEnable).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	from = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	to = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.CreateLogicalDevice).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	from = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	to = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetAdminStateToEnable).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	from = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	to = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetupUNILogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	from = getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	to = getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetAdminStateToEnable).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	from = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	to = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 1, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.SetAdminStateToEnable).Pointer() == reflect.ValueOf(handlers[0]).Pointer())

	from = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_DELETED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 3, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.DeleteAllChildDevices).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalDevice).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[2]).Pointer())

	from = getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(false, voltha.AdminState_DELETED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	handlers = transitionMap.GetTransitionHandler(from, to)
	assert.Equal(t, 2, len(handlers))
	assert.True(t, reflect.ValueOf(tdm.DeleteLogicalPorts).Pointer() == reflect.ValueOf(handlers[0]).Pointer())
	assert.True(t, reflect.ValueOf(tdm.RunPostDeviceDelete).Pointer() == reflect.ValueOf(handlers[1]).Pointer())
}

func TestInvalidTransitions(t *testing.T) {
	from := getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVE)
	to := getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	assertInvalidTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	to = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_UNKNOWN, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_DOWNLOADING_IMAGE, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertInvalidTransition(t, from, to)
}

func TestNoOpTransitions(t *testing.T) {
	from := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to := getDevice(true, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, from, to)

	from = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(true, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, from, to)

	from = getDevice(false, voltha.AdminState_ENABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	to = getDevice(false, voltha.AdminState_DISABLED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_UNKNOWN)
	assertNoOpTransition(t, from, to)

	from = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_UNKNOWN, voltha.OperStatus_ACTIVATING)
	to = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	assertNoOpTransition(t, from, to)

	from = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_DISCOVERED)
	to = getDevice(false, voltha.AdminState_PREPROVISIONED, voltha.ConnectStatus_REACHABLE, voltha.OperStatus_ACTIVATING)
	assertNoOpTransition(t, from, to)
}
