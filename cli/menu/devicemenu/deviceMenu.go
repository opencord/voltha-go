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
	"fmt"

	"github.com/bclicn/color"
	"github.com/opencord/voltha-go/cli/util"
	"google.golang.org/grpc"
)

/*
Conn - the grpc connection to use for making calls to voltha core
*/
var (
	Conn        *grpc.ClientConn
	DeviceId    *string
	InputPrompt *string
	Commands    *[]string
)

/*
MainLoop - the loop which processes commands at the main level
*/
func MainLoop(conn *grpc.ClientConn, deviceId string) {

	DeviceId = &deviceId
	inputPrompt := fmt.Sprint("(" + color.LightRed("device "+deviceId) + ") ")
	InputPrompt = &inputPrompt
	funcTable := make(map[string]func(bool))
	//	inputPromptSize := len(inputPrompt)
	Conn = conn
	funcTable["quit"] = util.Exit
	funcTable["exit"] = nil
	funcTable["edit"] = doEdit
	funcTable["history"] = doHistory
	funcTable["img_dnld_request"] = doImgDnldRequest
	funcTable["perf_config"] = doPerfConfig
	funcTable["save"] = doSave
	funcTable["eof"] = doEof
	funcTable["images"] = doImages
	funcTable["img_dnld_status"] = doImgDnldStatus
	funcTable["ports"] = doPorts
	funcTable["set"] = doSet
	funcTable["img_activate"] = doImgActivate
	funcTable["img_revert"] = doImgRevert
	funcTable["py"] = doPy
	funcTable["shell"] = doShell
	funcTable["flows"] = doFlows
	funcTable["img_dnld_canel"] = doImgDnldCancel
	funcTable["list"] = doList
	funcTable["shortcuts"] = doShortCuts
	funcTable["help"] = doHelp
	funcTable["img_dnld_list"] = doImgDnldList
	funcTable["pause"] = doPause
	funcTable["run"] = doRun
	funcTable["show"] = doShow

	commands := make([]string, len(funcTable))
	i := 0
	for key, _ := range funcTable {
		commands[i] = key
		i++
	}
	Commands = &commands

	util.ProcessTable(funcTable, inputPrompt)

}
