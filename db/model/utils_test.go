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

package model

import (
	"github.com/golang/protobuf/ptypes/any"
	//	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

var (
	TestUtilsPort = []*voltha.Port{
		{
			PortNo:     123,
			Label:      "test-etcd_port-0",
			Type:       voltha.Port_PON_OLT,
			AdminState: common.AdminState_ENABLED,
			OperStatus: common.OperStatus_ACTIVE,
			DeviceId:   "etcd_port-0-device-id",
			Peers:      []*voltha.Port_PeerPort{},
		},
	}

	TestUtilsStats = &openflow_13.OfpFlowStats{
		Id: 1111,
	}

	TestUtilsFlows = &openflow_13.Flows{
		Items: []*openflow_13.OfpFlowStats{TestProxyStats},
	}

	TestUtilsDevice = &voltha.Device{
		Id:              "Config-Node-1",
		Type:            "simulated_olt",
		Root:            true,
		ParentId:        "",
		ParentPortNo:    0,
		Vendor:          "voltha-test",
		Model:           "Modelxx",
		HardwareVersion: "0.0.1",
		FirmwareVersion: "0.0.1",
		Images:          &voltha.Images{},
		SerialNumber:    "1234567890",
		VendorId:        "XXBB-INC",
		Adapter:         "simulated_olt",
		Vlan:            1234,
		Address:         &voltha.Device_HostAndPort{HostAndPort: "127.0.0.1:1234"},
		ExtraArgs:       "",
		ProxyAddress:    &voltha.Device_ProxyAddress{},
		AdminState:      voltha.AdminState_PREPROVISIONED,
		OperStatus:      common.OperStatus_ACTIVE,
		Reason:          "",
		ConnectStatus:   common.ConnectStatus_REACHABLE,
		Custom:          &any.Any{},
		Ports:           TestUtilsPort,
		Flows:           TestUtilsFlows,
		FlowGroups:      &openflow_13.FlowGroups{},
		PmConfigs:       &voltha.PmConfigs{},
		ImageDownloads:  []*voltha.ImageDownload{},
	}

	TestUtilsEmpty interface{}
)

func TestIsProtoMessage(t *testing.T) {
	result := IsProtoMessage(TestUtilsDevice)
	assert.True(t, result)

	result1 := IsProtoMessage(TestUtilsEmpty)
	assert.False(t, result1)
}

func TestFindOwnerType(t *testing.T) {
	result := FindOwnerType(reflect.ValueOf(TestUtilsDevice), "ports", 0, false)
	if result != reflect.TypeOf(&voltha.Port{}) {
		t.Errorf("TestFindOwnerType: Case0: result: %v, expected: %v", result, reflect.TypeOf(&voltha.Port{}))
	}

	result1 := FindOwnerType(reflect.ValueOf(TestUtilsDevice), "items", 1, false)
	if result1 != reflect.TypeOf(&openflow_13.OfpFlowStats{}) {
		t.Errorf("TestFindOwnerType: Case1: result: %v, expected: %v", result1, reflect.TypeOf(&openflow_13.OfpFlowStats{}))
	}

	result2 := FindOwnerType(reflect.ValueOf(TestUtilsDevice), "abcd", 1, false)
	assert.Nil(t, result2)
}

func TestKeyOwner(t *testing.T) {
	result := FindKeyOwner(TestUtilsDevice, "ports", 0)
	if result != reflect.TypeOf(TestUtilsPort) {
		t.Errorf("TestFindOwnerType: Case0: result: %v, expected: %v", result, reflect.TypeOf(TestUtilsPort))
	}

	result1 := FindKeyOwner(TestUtilsDevice, "items", 1)
	if result1 != reflect.TypeOf(TestUtilsFlows.Items) {
		t.Errorf("TestFindOwnerType: Case1: result: %v, expected: %v", result1, reflect.TypeOf(TestUtilsFlows.Items))
	}

	result2 := FindKeyOwner(TestUtilsDevice, "abcd", 1)
	assert.Nil(t, result2)
}

func TestGetAttributeValue(t *testing.T) {
	result1, result2 := GetAttributeValue(TestUtilsDevice, "ports", 0)
	assert.Equal(t, "Ports", result1)
	assert.Equal(t, reflect.ValueOf(TestUtilsPort).Index(0), result2.Index(0))

	result3, _ := GetAttributeValue(TestUtilsDevice, "items", 1)
	assert.Equal(t, "Items", result3)

	result4, _ := GetAttributeValue(TestUtilsDevice, "abcd", 1)
	assert.Empty(t, result4)
}

func TestGetAttributeStructure(t *testing.T) {
	result := GetAttributeStructure(TestUtilsDevice, "ports", 0)
	assert.Equal(t, "Ports", result.Name)
	assert.Equal(t, reflect.TypeOf(TestUtilsPort), result.Type)

	result1 := GetAttributeStructure(TestUtilsDevice, "items", 1)
	assert.Equal(t, "Items", result1.Name)
	assert.Equal(t, reflect.TypeOf(TestUtilsFlows.Items), result1.Type)

	result2 := GetAttributeStructure(TestUtilsDevice, "abcd", 1)
	assert.Empty(t, result2.Name)
	assert.Nil(t, result2.Type)
}
