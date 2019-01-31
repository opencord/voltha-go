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
// The template for the tester.
// This template is filled in by the
// test driver based on the configuration.

package main

import (
	"os"
	//"time"
	"fmt"
	"os/exec"
	"io/ioutil"
	"github.com/opencord/voltha-go/common/log"
)

func main() {
	var cmd *exec.Cmd
	var cmdStr string
	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	defer log.CleanUp()

	log.Info("Running tests")
	{{range .}}
	if err:= os.Chdir(os.Args[1]); err != nil {
		log.Error("Could not change directory to %s: %v", os.Args[1], err)
	}
	cmdStr =  "./"+"{{.}}"[:len("{{.}}")-5]
	log.Infof("Running test %s",cmdStr)
	cmd = exec.Command(cmdStr, "results.txt")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Errorf("Test '%s' failed", cmdStr)
	}
	{{end}}
	// Open the results file and output it.
	if resFile, err := ioutil.ReadFile("results.txt"); err == nil {
		fmt.Println(string(resFile))
	} else {
		log.Error("Could not load the results file 'results.txt'")
	}
	log.Info("Tests complete")
}
