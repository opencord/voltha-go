/*
 * Copyright 2019-present Open Networking Foundation

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
	"reflect"
	"testing"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/stretchr/testify/assert"
)

func MakeTestNewCoreConfig() *Core {
	var core Core
	core.instanceID = "ro_core"
	var err error
	core.config = config.NewROCoreFlags()
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, nil)
	core.clusterDataProxy, err = core.clusterDataRoot.CreateProxy(context.Background(), "/", false)
	if err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot create cluster data proxy")
	}
	core.genericMgr = newModelProxyManager(core.clusterDataProxy)
	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)

	return &core
}

func TestNewLogicalDeviceManager(t *testing.T) {
	core := MakeTestNewCoreConfig()

	logicalDevMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, logicalDevMgr)
}

func TestAddLogicalDeviceAgentToMap(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	// Before ADD
	ldAgentNil := ldMgr.getLogicalDeviceAgent("id")
	assert.Nil(t, ldAgentNil)

	/*** Case: addLogicalDeviceAgentToMap() is Success ***/
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldAgent)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify ADD is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)
}

func TestGetLogicalDeviceAgent(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: getLogicalDeviceAgent() is NIL ***/
	ldAgentNil := ldMgr.getLogicalDeviceAgent("id")
	assert.Nil(t, ldAgentNil)

	/*** Case: getLogicalDeviceAgent() is Success ***/
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldAgent)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)
}

func TestDeleteLogicalDeviceAgent(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: deleteLogicalDeviceAgent() with Invalid Value ***/
	ldAgentNilChk := ldMgr.getLogicalDeviceAgent("invalid_id")
	assert.Nil(t, ldAgentNilChk)
	ldMgr.deleteLogicalDeviceAgent("invalid_id")

	/*** Case: deleteLogicalDeviceAgent() is Success ***/
	// Initialize and Add Logical Device Agent
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldAgent)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify ADD is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)

	// Method under Test
	ldMgr.deleteLogicalDeviceAgent("id")

	// Verify DEL is successful
	ldAgentNil := ldMgr.getLogicalDeviceAgent("id")
	assert.Nil(t, ldAgentNil)
}

func TestLdMgrGetLogicalDevice(t *testing.T) {
	wantResult := &voltha.LogicalDevice{}

	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: getLogicalDevice() is NIL ***/
	logicalDevNil, errNotNil := ldMgr.getLogicalDevice("id")
	assert.Nil(t, logicalDevNil)
	assert.NotNil(t, errNotNil)

	/*** Case: getLogicalDevice() is Success ***/
	// Add Data
	added, err := core.clusterDataProxy.Add(
		context.Background(),
		"/logical_devices",
		&voltha.LogicalDevice{
			Id: "id",
		},
		"")
	if err != nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	}
	ldAgent := newLogicalDeviceAgent("id", "device_id", ldMgr, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldAgent)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify Add is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)

	// Verify getLogicalDevice() is NOT NIL
	logicalDevNotNil, errNil := ldMgr.getLogicalDevice("id")
	assert.NotNil(t, logicalDevNotNil)
	assert.Nil(t, errNil)
	if reflect.TypeOf(logicalDevNotNil) != reflect.TypeOf(wantResult) {
		t.Errorf("GetLogicalDevice() = %v, want %v", logicalDevNotNil, wantResult)
	}
	assert.Equal(t, "id", logicalDevNotNil.Id)
}

func TestListLogicalDevices(t *testing.T) {
	wantResult := &voltha.LogicalDevices{}

	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: listLogicalDevices() is Empty ***/
	result, error := ldMgr.listLogicalDevices()
	assert.NotNil(t, result)
	assert.Nil(t, error)
	if reflect.TypeOf(result) != reflect.TypeOf(wantResult) {
		t.Errorf("ListLogicalDevices() = %v, want %v", result, wantResult)
	}
	assert.Empty(t, result)
}

func TestLoad(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Error Scenario ***/
	error := ldMgr.load("id")
	assert.NotNil(t, error)
}

func TestGetLogicalDeviceId(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Logical Device Id Found ***/
	result0, error0 := ldMgr.getLogicalDeviceID(&voltha.Device{Id: "id", Root: true, ParentId: "parent_id"})
	assert.NotNil(t, result0)
	assert.Nil(t, error0)

	/*** Case: Logical Device Id Not Found ***/
	result1, error1 := ldMgr.getLogicalDeviceID(&voltha.Device{Id: "id", ParentId: "parent_id"})
	assert.Nil(t, result1)
	assert.NotNil(t, error1)
}

func TestGetLogicalPortId(t *testing.T) {
	wantResult := &voltha.LogicalPortId{}

	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Logical Port Id Not Found: getLogicalDeviceId() Error ***/
	result0, error0 := ldMgr.getLogicalPortID(&voltha.Device{Id: "id", ParentId: "parent_id"})
	assert.Nil(t, result0)
	assert.NotNil(t, error0)

	/*** Case: Logical Port Id Not Found: getLogicalDevice() Error ***/
	result1, error1 := ldMgr.getLogicalPortID(&voltha.Device{Id: "id", Root: true, ParentId: "parent_id"})
	assert.Nil(t, result1)
	assert.NotNil(t, error1)

	/*** Case: Logical Port Id Found ***/
	device := &voltha.Device{Id: "id", Root: true, ParentId: "parent_id"}

	// Add Data
	added, err := core.clusterDataProxy.Add(
		context.Background(),
		"/logical_devices",
		&voltha.LogicalDevice{
			Id: "parent_id",
			Ports: []*voltha.LogicalPort{
				{
					Id:       "123",
					DeviceId: "id",
				},
			},
		},
		"")
	if err != nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	}
	ldAgent := newLogicalDeviceAgent("parent_id", "device_id", ldMgr, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldAgent)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify Add is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("parent_id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "parent_id", ldAgentNotNil.logicalDeviceID)

	// Verify getLogicalPortId() is Success
	result2, error2 := ldMgr.getLogicalPortID(device)
	assert.NotNil(t, result2)
	assert.Nil(t, error2)
	if reflect.TypeOf(result2) != reflect.TypeOf(wantResult) {
		t.Errorf("GetLogicalPortId() = %v, want %v", result2, wantResult)
	}
	assert.Equal(t, "parent_id", result2.Id)
}

func TestListLogicalDevicePorts(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Logical Device Ports Not Found ***/
	result0, error0 := ldMgr.ListLogicalDevicePorts(context.Background(), "id")
	assert.Nil(t, result0)
	assert.NotNil(t, error0)

	/*** Case: Logical Device Ports Found ***/
	wantResult := &voltha.LogicalPorts{
		Items: []*voltha.LogicalPort{
			{
				Id: "123",
			},
		},
	}

	// Add Data
	added, err := core.clusterDataProxy.Add(
		context.Background(),
		"/logical_devices",
		&voltha.LogicalDevice{
			Id: "id",
			Ports: []*voltha.LogicalPort{
				{
					Id: "123",
				},
			},
		},
		"")
	if err != nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	}
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify Add is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)

	// Verify ListLogicalDevicePorts() is Success
	result1, error1 := ldMgr.ListLogicalDevicePorts(context.Background(), "id")
	assert.NotNil(t, result1)
	assert.Nil(t, error1)
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListLogicalDevicePorts() = %v, want %v", result1, wantResult)
	}
	assert.Equal(t, wantResult, result1)
}

func TestListLogicalDeviceFlows(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Logical Device Flows Not Found ***/
	result0, error0 := ldMgr.ListLogicalDeviceFlows(context.Background(), "id")
	assert.Nil(t, result0)
	assert.NotNil(t, error0)

	/*** Case: Logical Device Flows Found ***/
	wantResult := &voltha.Flows{}

	// Add Data
	added, err := core.clusterDataProxy.Add(
		context.Background(),
		"/logical_devices",
		&voltha.LogicalDevice{
			Id: "id",
			Flows: &openflow_13.Flows{
				Items: []*openflow_13.OfpFlowStats{
					&openflow_13.OfpFlowStats{
						Id: 1111,
					},
				},
			},
		},
		"")
	if err != nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	}
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify Add is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)

	// Verify ListLogicalDeviceFlows() is Success
	result1, error1 := ldMgr.ListLogicalDeviceFlows(context.Background(), "id")
	assert.NotNil(t, result1)
	assert.Nil(t, error1)
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListLogicalDeviceFlows() = %v, want %v", result1, wantResult)
	}
	assert.NotEmpty(t, result1)
}

func TestListLogicalDeviceFlowGroups(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Logical Device Flow Groups Not Found ***/
	result0, error0 := ldMgr.ListLogicalDeviceFlowGroups(context.Background(), "id")
	assert.Nil(t, result0)
	assert.NotNil(t, error0)

	/*** Case: Logical Device Flow Groups Found ***/
	wantResult := &voltha.FlowGroups{}

	// Add Data
	added, err := core.clusterDataProxy.Add(
		context.Background(),
		"/logical_devices",
		&voltha.LogicalDevice{
			Id: "id",
			FlowGroups: &openflow_13.FlowGroups{
				Items: []*openflow_13.OfpGroupEntry{
					{
						Stats: &openflow_13.OfpGroupStats{
							GroupId: 1,
						},
					},
				},
			},
		},
		"")
	if err != nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	}
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify Add is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)

	// Verify ListLogicalDeviceFlowGroups() is Success
	result1, error1 := ldMgr.ListLogicalDeviceFlowGroups(context.Background(), "id")
	assert.NotNil(t, result1)
	assert.Nil(t, error1)
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListLogicalDeviceFlowGroups() = %v, want %v", result1, wantResult)
	}
	assert.NotEmpty(t, result1)
}

func TestGetLogicalPort(t *testing.T) {
	core := MakeTestNewCoreConfig()
	ldMgr := newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, ldMgr)

	/*** Case: Logical Port Not Found: getLogicalDevice() Error ***/
	result0, error0 := ldMgr.getLogicalPort(&voltha.LogicalPortId{Id: "id", PortId: "123"})
	assert.Nil(t, result0)
	assert.NotNil(t, error0)

	/*** Case: Logical Port Found ***/
	wantResult := &voltha.LogicalPort{Id: "123"}

	// Add Data
	added, err := core.clusterDataProxy.Add(
		context.Background(),
		"/logical_devices",
		&voltha.LogicalDevice{
			Id: "id",
			Ports: []*voltha.LogicalPort{
				{
					Id: "123",
				},
			},
		},
		"")
	if err != nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	if added == nil {
		t.Error("Failed to add logical device")
	}
	ldAgent := newLogicalDeviceAgent("id", "", ldMgr, core.deviceMgr, core.clusterDataProxy)
	ldMgr.addLogicalDeviceAgentToMap(ldAgent)

	// Verify Add is successful
	ldAgentNotNil := ldMgr.getLogicalDeviceAgent("id")
	assert.NotNil(t, ldAgentNotNil)
	assert.Equal(t, "id", ldAgentNotNil.logicalDeviceID)

	// Verify getLogicalPort() is Success
	result1, error1 := ldMgr.getLogicalPort(&voltha.LogicalPortId{Id: "id", PortId: "123"})
	assert.NotNil(t, result1)
	assert.Nil(t, error1)
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("getLogicalPort() = %v, want %v", result1, wantResult)
	}
	assert.Equal(t, wantResult, result1)
}
