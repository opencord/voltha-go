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

package mainmenu

import (
	"context"
	"fmt"
	"strconv"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/cli/util"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

/*
 reason | proxy_address.device_id | proxy_address.onu_id | proxy_address.onu_session_id |
*/
func doDevices(enterPressed bool) {

	client := voltha.NewVolthaServiceClient(Conn)
	devices, err := client.ListDevices(context.Background(), &empty.Empty{})
	if err != nil {
		fmt.Println(err)
	}
	var rows []map[string]string
	items := devices.GetItems()
	var fields = []string{"id", "type", "root", "parent_id", "serial_number", "admin_state", "oper_status", "connect_status", "parent_port_no", "host_and_port", "reason",
		"proxy_address.device_id", "proxy_address.onu_id", "proxy_address.onu_session_id"}

	for i := 0; i < len(items); i++ {
		//fmt.Println(items[i])
		device := items[i]
		row := make(map[string]string)
		row["id"] = device.Id
		row["type"] = device.Type
		row["root"] = strconv.FormatBool(device.Root)
		row["parent_id"] = device.ParentId
		row["serial_number"] = device.SerialNumber
		row["admin_state"] = device.AdminState.String()
		row["oper_status"] = device.OperStatus.String()
		row["connect_status"] = device.ConnectStatus.String()
		row["parent_port_no"] = strconv.FormatUint(uint64(device.GetParentPortNo()), 10)
		row["host_and_port"] = device.GetHostAndPort()
		row["reason"] = device.Reason
		proxyAddress := device.GetProxyAddress()
		if proxyAddress != nil {
			row["proxy_address.device_id"] = proxyAddress.DeviceId
			row["proxy_address.onu_id"] = strconv.FormatUint(uint64(proxyAddress.OnuId), 10)
			row["proxy_address.onu_session_id"] = strconv.FormatUint(uint64(proxyAddress.OnuSessionId), 10)
		} else {
			row["proxy_address.device_id"] = ""
			row["proxy_address.onu_id"] = ""
			row["proxy_address.onu_session_id"] = ""
		}

		rows = append(rows, row)
	}
	output, err := util.BuildTable(fields, rows)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Print(output)

}
