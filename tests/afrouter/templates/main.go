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
	"fmt"
	"time"
	"os/exec"
	"strings"
	"context"
	slog "log"
	"io/ioutil"
	"encoding/json"
	"google.golang.org/grpc/grpclog"
	"github.com/opencord/voltha-go/common/log"
)

type TestCase struct {
	Title string `json:"title"`
	Result bool `json:"result"`
	Info []string `json:"info"`
}

type TestSuite struct {
	Name string `json:"name"`
	TestCases []TestCase `json:"testCases"`
}

type TestRun struct {
	TestSuites []TestSuite
}

var resFile *tstLog
type tstLog struct {
	fp * os.File
	fn string
}

func (tr * tstLog) testLog(format string, a ...interface{}) {
	var err error
	if tr.fp == nil {
		if tr.fp, err = os.OpenFile(tr.fn, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err != nil {
			log.Errorf("Could not append to the results file")
			tr.fp = nil
		}
	}
	if tr.fp != nil {
		tr.fp.Write([]byte(fmt.Sprintf(format, a...)))
	}
}

func (tr * tstLog) testLogOnce(format string, a ...interface{}) {
	var err error
	if tr.fp == nil {
		if tr.fp, err = os.OpenFile(tr.fn, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err != nil {
			log.Errorf("Could not append to the results file")
			tr.fp = nil
		}
	}
	if tr.fp != nil {
		tr.fp.Write([]byte(fmt.Sprintf(format, a...)))
	}
	tr.fp.Close()
	tr.fp = nil
}

func (tr * tstLog) close() {
	if tr.fp != nil {
		tr.fp.Close()
	}
}


func startSut(cmdStr string) (*exec.Cmd, context.CancelFunc, error) {
	var err error = nil

	cmdAry := strings.Fields(cmdStr)
	log.Infof("Running the affinity router: %s",cmdStr)
	//ctx, cncl := context.WithCancel(context.Background())
	ctx, cncl := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cmdAry[0], cmdAry[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		log.Errorf("Failed to run the affinity router: %s %v", cmdStr,err)
	}
	time.Sleep(1 * time.Second) // Give the command time to get started
	return cmd, cncl, err
}

func cleanUp(cmd *exec.Cmd, cncl context.CancelFunc) {
	cncl()
	cmd.Wait() // This seems to hang
	// Give the child processes time to terminate
	//time.Sleep(1 * time.Second)
}

func readStats(stats * TestRun) () {
	// Check if the  stats file exists
	if _,err := os.Stat("stats.json"); err != nil {
		// Nothing to do, just return
		return
	}
	// The file is there, read it an unmarshal it into the stats struct
	if statBytes, err := ioutil.ReadFile("stats.json"); err != nil {
		log.Error(err)
		return
	} else if err := json.Unmarshal(statBytes, stats); err != nil {
		log.Error(err)
		return
	}
}

func writeStats(stats * TestRun) () {
	// Check if the  stats file exists
	// The file is there, read it an unmarshal it into the stats struct
	if statBytes, err := json.MarshalIndent(stats, "","    "); err != nil {
		log.Error(err)
		return
	} else if err := ioutil.WriteFile("stats.json.new", statBytes, 0644); err != nil {
		log.Error(err)
		return
	}
	os.Rename("stats.json", "stats.json~")
	os.Rename("stats.json.new", "stats.json")
}
var stats TestRun

func main() {
	var err error

	// Setup logging
	if _, err = log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	defer log.CleanUp()

	readStats(&stats)


	grpclog.SetLogger(slog.New(os.Stderr, "grpc: ", slog.LstdFlags))

	resFile = &tstLog{fn:os.Args[1]}
	defer resFile.close()


	// Initialize the servers
	if err := serverInit(); err != nil {
		log.Errorf("Error initializing server: %v", err)
		return
	}

	// Start the sofware under test
	cmd, cnclFunc, err := startSut("./{{.Command}}");
	defer cleanUp(cmd, cnclFunc)
	if  err != nil {
		return
	}

	// Initialize the clients
	if err := clientInit(); err != nil {
		log.Errorf("Error initializing client: %v", err)
		return
	}

	log.Infof("The servers are: %v",servers)

	// Run all the test cases now
	log.Infof("Executing tests")
	runTests()
	writeStats(&stats)

}
