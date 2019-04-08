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

package devicemenu

import (
	"context"
	"fmt"
	"strconv"

	"github.com/opencord/voltha-go/cli/util"
	"github.com/opencord/voltha-protos/go/common"
	"github.com/opencord/voltha-protos/go/voltha"
)

func doShow(enterPressed bool) {
	client := voltha.NewVolthaServiceClient(Conn)
	fmt.Println(*DeviceId)
	device, err := client.GetDevice(context.Background(), &common.ID{Id: *DeviceId})
	if err != nil {
		fmt.Println(err)
	}
	fields := []string{"field", "value"}
	var rows []map[string]string

	id := make(map[string]string)
	id["field"] = "id"
	id["value"] = device.Id
	rows = append(rows, id)

	Type := make(map[string]string)
	Type["field"] = "type"
	Type["value"] = device.Type
	rows = append(rows, Type)

	parentId := make(map[string]string)
	parentId["field"] = "parent_id"
	parentId["value"] = device.ParentId
	rows = append(rows, parentId)

	vlan := make(map[string]string)
	vlan["field"] = "vlan"
	vlan["value"] = strconv.FormatUint(uint64(device.Vlan), 10)
	rows = append(rows, vlan)

	adminState := make(map[string]string)
	adminState["field"] = "admin_state"
	adminState["value"] = strconv.FormatUint(uint64(device.AdminState), 10)
	rows = append(rows, adminState)

	proxyAddress := device.GetProxyAddress()
	proxyDeviceId := make(map[string]string)
	proxyDeviceId["field"] = "proxy_address.device_id"
	proxyDeviceId["value"] = proxyAddress.DeviceId
	rows = append(rows, proxyDeviceId)

	proxyDeviceType := make(map[string]string)
	proxyDeviceType["field"] = "proxy_address.device_type"
	proxyDeviceType["value"] = proxyAddress.DeviceType
	rows = append(rows, proxyDeviceType)

	proxyChannelId := make(map[string]string)
	proxyChannelId["field"] = "proxy_address.channel_id"
	proxyChannelId["value"] = strconv.FormatUint(uint64(proxyAddress.ChannelId), 10)
	rows = append(rows, proxyChannelId)

	parentPortNumber := make(map[string]string)
	parentPortNumber["field"] = "parent_port_no"
	parentPortNumber["value"] = strconv.FormatUint(uint64(device.GetParentPortNo()), 10)
	rows = append(rows, parentPortNumber)

	output, _ := util.BuildTable(fields, rows)

	fmt.Println(output)
}
