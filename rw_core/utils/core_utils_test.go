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
package utils

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	timeoutError     error
	taskFailureError error
)

func init() {
	timeoutError = status.Errorf(codes.Aborted, "timeout")
	taskFailureError = status.Error(codes.Internal, "test failure task")
}

func runSuccessfulTask(response Response, durationRange int) {
	time.Sleep(time.Duration(rand.Intn(durationRange)) * time.Millisecond)
	response.Done()
}

func runFailureTask(response Response, durationRange int) {
	time.Sleep(time.Duration(rand.Intn(durationRange)) * time.Millisecond)
	response.Error(taskFailureError)
}

func runMultipleTasks(timeout time.Duration, numTasks, taskDurationRange, numSuccessfulTask, numFailuretask int) []error {
	if numTasks != numSuccessfulTask+numFailuretask {
		return []error{status.Error(codes.FailedPrecondition, "invalid-num-tasks")}
	}
	numSuccessfulTaskCreated := 0
	responses := make([]Response, numTasks)
	for i := 0; i < numTasks; i++ {
		responses[i] = NewResponse()
		if numSuccessfulTaskCreated < numSuccessfulTask {
			go runSuccessfulTask(responses[i], taskDurationRange)
			numSuccessfulTaskCreated++
			continue
		}
		go runFailureTask(responses[i], taskDurationRange)
	}
	return WaitForNilOrErrorResponses(timeout, responses...)
}

func getNumSuccessFailure(inputs []error) (numSuccess, numFailure, numTimeout int) {
	numFailure = 0
	numSuccess = 0
	numTimeout = 0
	for _, input := range inputs {
		if input != nil {
			if input.Error() == timeoutError.Error() {
				numTimeout++
			}
			numFailure++
		} else {
			numSuccess++
		}
	}
	return
}

func TestNoTimeouts(t *testing.T) {
	var (
		totalSuccess int
		totalFailure int
		results      []error
		nSuccess     int
		nFailure     int
		nTimeouts    int
	)
	numIterations := 5
	numTasks := 5
	for i := 0; i < numIterations; i++ {
		totalSuccess = rand.Intn(numTasks)
		totalFailure = numTasks - totalSuccess
		results = runMultipleTasks(110*time.Millisecond, numTasks, 50, totalSuccess, totalFailure)
		nSuccess, nFailure, nTimeouts = getNumSuccessFailure(results)
		assert.Equal(t, totalFailure, nFailure)
		assert.Equal(t, totalSuccess, nSuccess)
		assert.Equal(t, 0, nTimeouts)
	}
}

func TestSomeTasksTimeouts(t *testing.T) {
	var (
		totalSuccess int
		totalFailure int
		results      []error
		nSuccess     int
		nFailure     int
		nTimeouts    int
	)
	numIterations := 5
	numTasks := 5
	for i := 0; i < numIterations; i++ {
		totalSuccess = rand.Intn(numTasks)
		totalFailure = numTasks - totalSuccess
		results = runMultipleTasks(50*time.Millisecond, numTasks, 100, totalSuccess, totalFailure)
		nSuccess, nFailure, nTimeouts = getNumSuccessFailure(results)
		assert.True(t, nFailure >= totalFailure)
		assert.True(t, nSuccess <= totalSuccess)
		assert.True(t, nTimeouts > 0)
	}
}
