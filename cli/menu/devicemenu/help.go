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
	"os"
	"strings"

	"github.com/opencord/voltha-go/cli/util"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

func doHelp(enterPressed bool) {
	input := ""
	var b = make([]byte, 1)
	inputPrompt := *InputPrompt + "help "
	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			log.Errorw("unable-to-read-from-stdin-file", log.Fields{"error": err})
		}
		char := string(b)
		if char == "\t" || char == "\n" || char == "?" {
			if enterPressed {
				baseHelp()
				return
			}

			fmt.Println("")
			ret, prompt := util.Test(input, *Commands)
			if len(ret) == 1 {
				input = ret[0]
				if input == "quit" {
					util.Exit(true)
				} else if input == "exit" {
					return
				}

				MainLoop(Conn, input)
				return
			} else if len(ret) == 0 {
				input = ""
				fmt.Print("Invalid Input \n" + inputPrompt)
			} else {

				fmt.Println(ret)
				input = prompt
				fmt.Print(prompt + inputPrompt)
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
func baseHelp() {
	message := `
Documented commands (type help <topic>):
========================================
edit   help          img_dnld_cancel   img_revert   ports  set
eof    history       img_dnld_list     list         py     shell
exit   images        img_dnld_request  pause        run    shortcuts
flows  img_activate  img_dnld_status   perf_config  save   show

Miscellaneous help topics:
==========================
load

Undocumented commands:
======================
quit

`
	fmt.Println(message)
}
