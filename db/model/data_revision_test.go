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
	"reflect"
	"testing"

	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
)

var (
	TestNodePort = []*voltha.Port{
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

	TestNodeData = &voltha.Device{
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
		Ports:           TestNodePort,
		Flows:           &openflow_13.Flows{},
		FlowGroups:      &openflow_13.FlowGroups{},
		PmConfigs:       &voltha.PmConfigs{},
		ImageDownloads:  []*voltha.ImageDownload{},
	}
)

func TestNewDataRevision(t *testing.T) {

	TestNodeRoot := &root{RevisionClass: reflect.TypeOf(NonPersistedRevision{})}
	dr := NewDataRevision(TestNodeRoot, TestNodeData)
	t.Logf("Data -->%v, Hash-->%v, ", dr.Data, dr.Hash)
	assert.NotNil(t, dr.Data)
	assert.True(t, reflect.TypeOf(dr.Data) == reflect.TypeOf(TestNodeData), "Data Type mismatch on NonPersistedRevision")
	assert.True(t, reflect.ValueOf(dr.Data) == reflect.ValueOf(TestNodeData), "Data Values mismatch on NonPersistedRevision")
	assert.NotNil(t, dr.Hash)

	drPR := NewDataRevision(&root{RevisionClass: reflect.TypeOf(PersistedRevision{})}, TestNodeData)
	assert.NotNil(t, drPR)
	assert.True(t, reflect.TypeOf(drPR.Data) == reflect.TypeOf(TestNodeData), "Data Type mismatc on PersistedRevisionh")
	assert.True(t, reflect.ValueOf(drPR.Data) == reflect.ValueOf(TestNodeData), "Data Values mismatch PersistedRevision")
	assert.NotNil(t, drPR.Hash)
}
func TestNoDataRevision(t *testing.T) {

	TestNodeData = nil
	TestNodeRoot = &root{RevisionClass: reflect.TypeOf(NonPersistedRevision{})}
	rev := NewDataRevision(TestNodeRoot, TestNodeData)
	assert.Nil(t, rev.Data, "Problem to marshal data when data is nil")

}
