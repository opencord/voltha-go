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
	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"testing"
)

func Test_Node_01_New(t *testing.T) {
	ports := []*voltha.Port{
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
	data := &voltha.Device{
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
		Ports:           ports,
		Flows:           &openflow_13.Flows{},
		FlowGroups:      &openflow_13.FlowGroups{},
		PmConfigs:       &voltha.PmConfigs{},
		ImageDownloads:  []*voltha.ImageDownload{},
	}
	root := &root{}
	txid := fmt.Sprintf("%x", md5.Sum([]byte("node_transaction_id")))

	node := NewNode(root, data, true, txid)

	t.Logf("new SomeNode created : %+v\n", node)
}
