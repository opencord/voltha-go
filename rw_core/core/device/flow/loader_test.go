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
	"strings"
	"testing"
)

// TestLoadersIdentical ensures that the group, flow, and meter loaders always have an identical implementation.
func TestLoadersIdentical(t *testing.T) {
	identical := [][]string{
		{`ofp\.OfpFlowStats`, `ofp\.OfpGroupEntry`, `ofp\.OfpMeterEntry`, `voltha\.LogicalPort`},
		{`\.Id`, `\.Desc\.GroupId`, `\.Config\.MeterId`, `\.Id`},
		{"uint64", "uint32", "uint32", "string"},
		{"Flow", "Group", "Meter", "Port"},
		{"flow", "group", "meter", "port"},
	}

	regexes := make([]*regexp.Regexp, len(identical))
	for i, group := range identical {
		regexes[i] = regexp.MustCompile(strings.Join(group, "|"))
	}

	for i := 1; i < len(identical[0]); i++ {
		if err := compare(regexes, "../"+identical[4][0]+"/loader.go", "../"+identical[4][i]+"/loader.go"); err != nil {
			t.Error(err)
			return
		}
	}
}

func compare(regexes []*regexp.Regexp, fileNameA, fileNameB string) error {
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
		for i, regex := range regexes {
			replacement := "{{type" + strconv.Itoa(i) + "}}"
			replacedA, replacedB = regex.ReplaceAllString(replacedA, replacement), regex.ReplaceAllString(replacedB, replacement)
		}

		// replace multiple spaces with single space
		replacedA, replacedB = spaceRegex.ReplaceAllString(replacedA, " "), spaceRegex.ReplaceAllString(replacedB, " ")

		// ignore difference: voltha-protos import of ofp vs voltha
		replacedA, replacedB = libGoImportRegex.ReplaceAllString(replacedA, "{{lib-go-import}}"), libGoImportRegex.ReplaceAllString(replacedB, "{{lib-go-import}}")

		if replacedA != replacedB {
			return fmt.Errorf("line %d: files %s and %s do not match: \n\t%s\n\t%s\n\n\t%s\n\t%s", line, fileNameA, fileNameB, textA, textB, replacedA, replacedB)
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
