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
	"os"
	"strings"
)

func Test(chars string, values []string) ([]string, string) {

	var ret []string
	for i := 0; i < len(values); i++ {
		if strings.HasPrefix(values[i], chars) {
			ret = append(ret, values[i])
		}
	}
	if len(ret) == 0 {
		return ret, ""
	}
	shortIndex := 0
	if len(ret) > 1 {
		for i := 0; i < len(ret); i++ {
			if len(ret[i]) < len(ret[shortIndex]) {
				shortIndex = i
			}
		}
	}
	for i := len(chars); i < len(ret[shortIndex]); i++ {
		inAllWords := true
		for j := 0; j < len(ret); j++ {
			inAllWords = inAllWords && ret[j][i] == ret[shortIndex][i]
		}
		if inAllWords {
			chars += string(ret[shortIndex][i])
		} else {
			return ret, chars
		}

	}

	return ret, chars
}

func Route(command string, table map[string]func(bool), enterPressed bool) {
	cmd := table[command]
	cmd(enterPressed)

}

func Exit(notUsed bool) {
	os.Exit(0)
}
