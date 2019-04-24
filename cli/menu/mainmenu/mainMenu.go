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
	"fmt"

	"github.com/bclicn/color"
	"github.com/opencord/voltha-go/cli/util"
	"google.golang.org/grpc"
)

/*
Conn - the grpc connection to use for making calls to voltha core
*/
var Conn *grpc.ClientConn

/*
MainLoop - the loop which processes commands at the main level
*/
func MainLoop(conn *grpc.ClientConn) {

	inputPrompt := fmt.Sprint("(" + color.LightBlue("voltha") + ") ")
	//	inputPromptSize := len(inputPrompt)
	Conn = conn
	mainFuncTable := make(map[string]func(bool))
	mainFuncTable["quit"] = util.Exit
	mainFuncTable["exit"] = nil
	mainFuncTable["cmdenvironment"] = doCmdEnvironment
	mainFuncTable["load"] = doLoad
	mainFuncTable["relative_load"] = doRelativeLoad
	mainFuncTable["reset_history"] = doResetHistory
	mainFuncTable["log"] = doLog
	mainFuncTable["launch"] = doLaunch
	mainFuncTable["restart"] = doRestart
	mainFuncTable["devices"] = doDevices
	mainFuncTable["device"] = doDevice
	mainFuncTable["logical_devices"] = doLogicalDevices
	mainFuncTable["logical_device"] = doLogicalDevice
	mainFuncTable["omci"] = doOmci
	mainFuncTable["pdb"] = doPdb
	mainFuncTable["version"] = doVersion
	mainFuncTable["health"] = doHealth
	mainFuncTable["preprovison_olt"] = doPreprovisionOlt
	mainFuncTable["enable"] = doEnable
	mainFuncTable["reboot"] = doReboot
	mainFuncTable["self_test"] = doSelfTest
	mainFuncTable["delete"] = doDelete
	mainFuncTable["disable"] = doDisable
	mainFuncTable["test"] = doTest
	mainFuncTable["alarm_filters"] = doAlarmFilters
	mainFuncTable["arrive_onus"] = doArriveOnus
	mainFuncTable["install_eapol_flow"] = doInstallEapolFlow
	mainFuncTable["install_all_controller_bound_flows"] = doInstallAllControllerBoundFlows
	mainFuncTable["install_all_sample_flows"] = doInstallAllSampleFlows
	mainFuncTable["install_dhcp_flows"] = doInstallDhcpFlows
	mainFuncTable["delete_all_flows"] = doDeleteAllFlows
	mainFuncTable["send_simulated_upstream_eapol"] = doSendSimulatedUpstreamEapol
	mainFuncTable["inject_eapol_start"] = doInjectEapolStart
	util.ProcessTable(mainFuncTable, inputPrompt)
}
