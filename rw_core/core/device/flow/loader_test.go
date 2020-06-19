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
	types := []string{"flow", "group", "meter", "port", "logical_port"}

	identical := [][]string{
		{`ofp\.OfpFlowStats`, `ofp\.OfpGroupEntry`, `ofp\.OfpMeterEntry`, `voltha\.Port`, `voltha\.LogicalPort`},
		{`\.Id`, `\.Desc\.GroupId`, `\.Config\.MeterId`, `\.PortNo`, `\.OfpPort\.PortNo`},
		{`uint64`, `uint32`, `uint32`, `uint32`, `uint32`},
		{`Flow`, `Group`, `Meter`, `Port`, `Port`},
		{`flow`, `group`, `meter`, `port`, `port|logical_port`},
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

	for i := 1; i < len(types); i++ {
		if err := compare(regexes[0], regexes[i],
			"../"+types[0]+"/loader.go",
			"../"+types[i]+"/loader.go"); err != nil {
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

	// treat any number of spaces as a single space
	spaceRegex := regexp.MustCompile(` +`)
	// extra lines are permitted before a "blank" line, or before a lock/unlock
	spacerRegex := regexp.MustCompile(`^(?:[^a-z]*|.*Lock\(\)|.*Unlock\(\))$`)
	// ignore import type differences
	libGoImportRegex := regexp.MustCompile(`^.*github\.com/opencord/voltha-protos/.*$`)

	lineA, lineB := 1, 1
linesLoop:
	for {
		if continueA, continueB := scannerA.Scan(), scannerB.Scan(); !continueA || !continueB {
			// EOF
			break linesLoop
		}
		textA, textB := scannerA.Text(), scannerB.Text()

		// allow any number of "extra" lines just before a spacer line
		for {
			isSpacerA, isSpacerB := spacerRegex.MatchString(textA), spacerRegex.MatchString(textB)
			if isSpacerA && !isSpacerB {
				if !scannerB.Scan() {
					// EOF
					break linesLoop
				}
				lineB++
				textB = scannerB.Text()
				continue
			} else if isSpacerB && !isSpacerA {
				if !scannerA.Scan() {
					// EOF
					break linesLoop
				}
				lineA++
				textA = scannerA.Text()
				continue
			}
			break
		}

		replacedA, replacedB := textA, textB
		for i := range regexesA {
			replacement := "{{type" + strconv.Itoa(i) + "}}"
			replacedA, replacedB = regexesA[i].ReplaceAllString(replacedA, replacement), regexesB[i].ReplaceAllString(replacedB, replacement)
		}

		// replace multiple spaces with single space
		replacedA, replacedB = spaceRegex.ReplaceAllString(replacedA, " "), spaceRegex.ReplaceAllString(replacedB, " ")

		// ignore voltha-protos import of ofp vs voltha
		replacedA, replacedB = libGoImportRegex.ReplaceAllString(replacedA, "{{lib-go-import}}"), libGoImportRegex.ReplaceAllString(replacedB, "{{lib-go-import}}")

		if replacedA != replacedB && textA != textB {
			return fmt.Errorf("files which must be identical do not match: \n  %s:%d\n    %s\n  %s:%d\n    %s\n\n\t%s\n\t%s", fileNameA, lineA, textA, fileNameB, lineB, textB, replacedA, replacedB)
		}

		lineA++
		lineB++
	}

	if err := scannerA.Err(); err != nil {
		return err
	}
	if err := scannerB.Err(); err != nil {
		return err
	}
	return nil
}
