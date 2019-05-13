/*
 * Copyright 2018-present Open Networking Foundation
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"time"
)

type DeviceID struct {
	Id string
}

type LogicalDeviceID struct {
	Id string
}

//WaitForNilOrErrorResponses waits on a variadic number of channels for either a nil response or an error
//response. If an error is received from a given channel then the returned error array will contain that error.
//The error will be at the index corresponding to the order in which the channel appear in the parameter list.
//If no errors is found then nil is returned.  This method also takes in a timeout in milliseconds. If a
//timeout is obtained then this function will stop waiting for the remaining responses and abort.
func WaitForNilOrErrorResponses(timeout int64, chnls ...chan interface{}) []error {
	// Create a timeout channel
	tChnl := make(chan *interface{})
	go func() {
		time.Sleep(time.Duration(timeout) * time.Millisecond)
		tChnl <- nil
	}()

	errorsReceived := false
	errors := make([]error, len(chnls))
	cases := make([]reflect.SelectCase, len(chnls)+1)
	for i, ch := range chnls {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}
	// Add the timeout channel
	cases[len(chnls)] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(tChnl)}

	resultsReceived := make([]bool, len(errors)+1)
	remaining := len(cases) - 1
	for remaining > 0 {
		index, value, ok := reflect.Select(cases)
		if !ok { // closed channel
			//Set the channel at that index to nil to disable this case, hence preventing it from interfering with other cases.
			cases[index].Chan = reflect.ValueOf(nil)
			errors[index] = status.Error(codes.Internal, "channel closed")
			errorsReceived = true
		} else if index == len(chnls) { // Timeout has occurred
			for k := range errors {
				if !resultsReceived[k] {
					errors[k] = status.Error(codes.Aborted, "timeout")
				}
			}
			errorsReceived = true
			break
		} else if value.IsNil() { // Nil means a good response
			//do nothing
		} else if err, ok := value.Interface().(error); ok { // error returned
			errors[index] = err
			errorsReceived = true
		} else { // unknown value
			errors[index] = status.Errorf(codes.Internal, "%s", value)
			errorsReceived = true
		}
		resultsReceived[index] = true
		remaining -= 1
	}

	if errorsReceived {
		return errors
	}
	return nil
}
