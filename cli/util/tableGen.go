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
	"strings"
)

func BuildTable(keys []string, rows []map[string]string) (string, error) {
	var returnString string
	fieldSizes := make(map[string]int)

	for i := 0; i < len(rows); i++ {
		for key, value := range rows[i] {
			currentSize := len(value)
			if currentSize > fieldSizes[key] {
				fieldSizes[key] = currentSize
			}

		}
	}
	for i := 0; i < len(keys); i++ {
		currentSize := len(keys[i])
		if currentSize > fieldSizes[keys[i]] {
			fieldSizes[keys[i]] = currentSize
		}
	}
	bottom := "+"

	for i := 0; i < len(rows); i++ {
		header := "|"
		line := "|"
		for j := 0; j < len(keys); j++ {
			key := keys[j]
			value := rows[i][key]
			if i == 0 {
				pad := 2 + fieldSizes[key] - len(key)
				field := fmt.Sprintf("%s%s|", strings.Repeat(" ", pad), key)
				spacer := fmt.Sprintf("%s+", strings.Repeat("-", fieldSizes[key]+2))
				header = header + field
				bottom = bottom + spacer
			}
			pad := 2 + fieldSizes[key] - len(value)
			field := fmt.Sprintf("%s%s|", strings.Repeat(" ", pad), value)
			line = line + field

		}
		if i == 0 {
			returnString = bottom + "\n" + header + "\n" + bottom + "\n"
		}

		returnString = returnString + line + "\n"
	}
	returnString = returnString + bottom + "\n"

	return returnString, nil

}
