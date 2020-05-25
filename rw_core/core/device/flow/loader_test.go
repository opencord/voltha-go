/*
 * Copyright 2020-present Open Networking Foundation

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

package flow

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestLoadersIdentical ensures that the group, flow, and meter loaders always have an identical implementation.
func TestLoadersIdentical(t *testing.T) {
	identical := [][]string{
		{`\.flows\[flow\.Id] = .*$`, `\.groups\[group\.Desc\.GroupId] = .*$`, `\.meters\[meter\.Config\.MeterId] = .*$`, `\.addUnsafe\(port\.DeviceId, port\.DevicePortNo, .*$`},
		{`delete\(.*loader\.flows, `, `delete\(.*loader\.groups, `, `delete\(.*loader\.meters, `, `(?:[a-z].*)?loader\.deletedUnsafe\(`},
		{`ofp\.OfpFlowStats`, `ofp\.OfpGroupEntry`, `ofp\.OfpMeterEntry`, `voltha\.LogicalPort`},
		{`flow\.Id`, `group\.Desc\.GroupId`, `meter\.Config\.MeterId`, `port\.DeviceId, port\.DevicePortNo|port\.DeviceId]\[port\.DevicePortNo|port\.DeviceId, "/", port\.DevicePortNo`},
		{`%v`, `%v`, `%v`, `%s-%d`},
		{`id uint64`, `id uint32`, `id uint32`, `deviceID string, portNo uint32`},
		{`\[id]`, `\[id]`, `\[id]`, `\[deviceID]\[portNo]`},
		{`\(id\)`, `\(id\)`, `\(id\)`, `\(deviceID, portNo\)`},
		{`uint64`, `uint32`, `uint32`, `string]map\[uint32|ID`},
		{`Flow`, `Group`, `Meter`, `Port`},
		{`flow`, `group`, `meter`, `port`},
	}

	regexes := make([][]*regexp.Regexp, len(identical[0]))
	for i := range regexes {
		regexes[i] = make([]*regexp.Regexp, len(identical))
	}
	for i, group := range identical {
		for j, regexStr := range group {
			// convert from column-wise to row-wise for convenience
			regexes[j][i] = regexp.MustCompile(regexStr)
		}
	}

	for i := 1; i < len(identical); i++ {
		if err := compare(regexes[0], regexes[i], "../"+identical[9][0]+"/loader.go", "../"+identical[9][i]+"/loader.go"); err != nil {
			t.Error(err)
			return
		}
	}
}

func compare(regexesA, regexesB []*regexp.Regexp, fileNameA, fileNameB string) error {
	fileA, err := os.Open(fileNameA)
	if err != nil {
		return err
	}
	defer fileA.Close()

	fileB, err := os.Open(fileNameB)
	if err != nil {
		return err
	}
	defer fileB.Close()

	scannerA, scannerB := bufio.NewScanner(fileA), bufio.NewScanner(fileB)

	spaceRegex := regexp.MustCompile(" +")
	libGoImportRegex := regexp.MustCompile(`^.*github\.com/opencord/voltha-protos/.*$`)

	line := 1
	for {
		if continueA, continueB := scannerA.Scan(), scannerB.Scan(); continueA != continueB {
			if !continueA && continueB {
				if err := scannerA.Err(); err != nil {
					return err
				}
			}
			if continueA && !continueB {
				if err := scannerB.Err(); err != nil {
					return err
				}
			}
			return fmt.Errorf("line %d: files are not the same length", line)
		} else if !continueA {
			// EOF from both files
			break
		}

		textA, textB := scannerA.Text(), scannerB.Text()

		replacedA, replacedB := textA, textB
		for i := range regexesA {
			replacement := "{{type" + strconv.Itoa(i) + "}}"
			replacedA, replacedB = regexesA[i].ReplaceAllString(replacedA, replacement), regexesB[i].ReplaceAllString(replacedB, replacement)
		}

		// replace multiple spaces with single space
		replacedA, replacedB = spaceRegex.ReplaceAllString(replacedA, " "), spaceRegex.ReplaceAllString(replacedB, " ")

		// ignore difference: voltha-protos import of ofp vs voltha
		replacedA, replacedB = libGoImportRegex.ReplaceAllString(replacedA, "{{lib-go-import}}"), libGoImportRegex.ReplaceAllString(replacedB, "{{lib-go-import}}")

		if replacedA != replacedB && textA != textB {
			return fmt.Errorf("files do not match: \n  %s:%d\n    %s\n  %s:%d\n    %s\n\n\t%s\n\t%s", fileNameA, line, textA, fileNameB, line, textB, replacedA, replacedB)
		}

		line++
	}

	if err := scannerA.Err(); err != nil {
		return err
	}
	if err := scannerB.Err(); err != nil {
		return err
	}
	return nil
}
