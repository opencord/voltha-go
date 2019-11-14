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
package model

import (
	"crypto/md5"
	"fmt"
	"reflect"
	"testing"

	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

var (
	TestNodeDevice = &voltha.Device{
		Id:              "Config-SomeNode-01-new-test",
		Type:            "simulated_olt",
		Root:            true,
		ParentId:        "",
		ParentPortNo:    0,
		Vendor:          "voltha-test",
		Model:           "GetLatest-voltha-simulated-olt",
		HardwareVersion: "1.0.0",
		FirmwareVersion: "1.0.0",
		Images:          &voltha.Images{},
		SerialNumber:    "abcdef-123456",
		VendorId:        "DEADBEEF-INC",
		Adapter:         "simulated_olt",
		Vlan:            1234,
		Address:         &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
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

	TestNodeTxid = fmt.Sprintf("%x", md5.Sum([]byte("node_transaction_id")))
	TestNodeRoot = &root{RevisionClass: reflect.TypeOf(NonPersistedRevision{})}
)

// Exercise node creation code
// This test will
func TestNode_01_NewNode(t *testing.T) {
	node := NewNode(TestNodeRoot, TestNodeDevice, false, TestNodeTxid)

	if reflect.ValueOf(node.Type).Type() != reflect.TypeOf(TestNodeDevice) {
		t.Errorf("Node type does not match original data type: %+v", reflect.ValueOf(node.Type).Type())
	} else if node.GetBranch(TestNodeTxid) == nil || node.GetBranch(TestNodeTxid).Latest == nil {
		t.Errorf("No branch associated to txid: %s", TestNodeTxid)
	} else if node.GetBranch(TestNodeTxid).Latest == nil {
		t.Errorf("Branch has no latest revision : %s", TestNodeTxid)
	} else if node.GetBranch(TestNodeTxid).GetLatest().GetConfig() == nil {
		t.Errorf("Latest revision has no assigned data: %+v", node.GetBranch(TestNodeTxid).GetLatest())
	}

	t.Logf("Created new node successfully : %+v\n", node)
}
