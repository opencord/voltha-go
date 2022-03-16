/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package grpc_test

import (
	"fmt"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/log"
)

const (
	/*
	 * This sets the GetLogLevel of the Voltha logger. It's pinned to FatalLevel here, as we
	 * generally don't want to see logger output, even when running go test in verbose
	 * mode. Even "Error" level messages are expected to be output by some unit tests.
	 *
	 * If you are developing a unit test, and experiencing problems or wish additional
	 * debugging from Voltha, then changing this constant to log.DebugLevel may be
	 * useful.
	 */

	volthaTestLogLevel = log.FatalLevel
	retryInterval      = 50 * time.Millisecond
)

type isConditionSatisfied func() bool

var logger log.CLogger

func init() {
	// Setup this package so that it's log level can be modified at run time
	var err error
	logger, err = log.RegisterPackage(log.JSON, log.DebugLevel, log.Fields{})
	if err != nil {
		panic(err)
	}
}

func waitUntilCondition(timeout time.Duration, verificationFunction isConditionSatisfied) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			if verificationFunction() {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-for-condition")
	}
}
