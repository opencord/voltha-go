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
		{"ofp\\.OfpFlowStats", "ofp\\.OfpGroupEntry", "ofp\\.OfpMeterEntry"},
		{"\\.Id", "\\.Desc\\.GroupId", "\\.Config.MeterId"},
		{"uint64", "uint32", "uint32"},
		{"Flow", "Group", "Meter"},
		{"flow", "group", "meter"},
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
