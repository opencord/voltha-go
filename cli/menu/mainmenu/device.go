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
	"os"
	"strings"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/cli/menu/devicemenu"
	"github.com/opencord/voltha-go/cli/util"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

func doDevice(enterPressed bool) {
	fmt.Print(" ")
	client := voltha.NewVolthaServiceClient(Conn)
	devices, err := client.ListDevices(context.Background(), &empty.Empty{})
	items := devices.GetItems()
	if err != nil {
		fmt.Println(err)
	}
	deviceIDs := []string{"exit", "quit"}
	for i := 0; i < len(items); i++ {
		deviceIDs = append(deviceIDs, items[i].Id)
	}
	var b = make([]byte, 1)
	input := ""

	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			log.Errorw("unable-to-read-from-stdin-file", log.Fields{"error": err})
		}
		char := string(b)
		if char == "\t" || char == "\n" || char == "?" {
			fmt.Println("")
			ret, prompt := util.Test(input, deviceIDs)
			if len(ret) == 1 {
				input = ret[0]
				if input == "quit" {
					util.Exit(true)
				} else if input == "exit" {
					return
				}

				devicemenu.MainLoop(Conn, input)
				return
			} else if len(ret) == 0 {
				input = ""
				fmt.Print("Invalid Input \ninput:")
			} else {

				fmt.Println(ret)
				input = prompt
				fmt.Print("input: " + prompt)
			}
		} else if b[0] == 127 || char == "\b" {

			sz := len(input)
			if sz > 0 {
				fmt.Print("\b \b")
				input = input[:sz-1]
			}
			if !(strings.HasPrefix(input, "device")) {
				return
			}
		} else {
			fmt.Print(char)
			input += char
		}
	}

}
