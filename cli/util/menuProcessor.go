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

package util

import (
	"fmt"
	"os"

	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

/*
ProcessTable parses table structure and executes functions
*/
func ProcessTable(functionTable map[string]func(bool), inputPrompt string) {
	keys := []string{}
	for k := range functionTable {
		keys = append(keys, k)
	}
	var b = make([]byte, 1)
	input := ""
	fmt.Print(inputPrompt)
	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			log.Errorw("unable-to-read-from-stdin-file", log.Fields{"error": err})
		}
		char := string(b)
		if char == "\t" || char == "\n" || char == "?" {
			fmt.Println("")
			ret, prompt := Test(input, keys)
			if len(ret) == 1 {
				input = ret[0]
				if input == "exit" {
					return
				}
				if char == "\n" {
					Route(input, functionTable, true)
				} else {
					Route(input, functionTable, false)
				}
				input = ""
				fmt.Print(inputPrompt)
			} else if len(ret) == 0 {
				input = ""
				fmt.Println("\nInvalid Input ")
				fmt.Print(inputPrompt)
			} else if len(ret) == 0 {
			} else {

				fmt.Println(ret)
				input = prompt
				fmt.Print(inputPrompt)
				fmt.Print(prompt)
			}
		} else if char == " " {
			_, ok := functionTable[input]
			if ok {
				Route(input, functionTable, false)
				fmt.Print(inputPrompt)
				input = ""
			} else {
				ret, prompt := Test(input, keys)
				if len(ret) == 1 {
					input = ret[0]
					Route(input, functionTable, false)
					input = ""
					fmt.Print(inputPrompt)
				} else if len(ret) == 0 {
					input = ""
					fmt.Println("\nInvalid Input ")
					fmt.Print(inputPrompt)
				} else {

					fmt.Println(ret)
					input = prompt
					fmt.Print(inputPrompt + input)
				}
			}

		} else if b[0] == 127 || char == "\b" {
			sz := len(input)

			if sz > 0 {
				fmt.Print("\b \b")
				input = input[:sz-1]
			}
		} else {
			fmt.Print(char)
			input += char
		}
	}
}
