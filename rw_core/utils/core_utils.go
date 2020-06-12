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
	"context"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResponseCallback is the function signature for callbacks to execute after a response is received.
type ResponseCallback func(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{})

// DeviceID represent device id attribute
type DeviceID struct {
	ID string
}

// LogicalDeviceID rpresent logical device id attribute
type LogicalDeviceID struct {
	ID string
}

// GetHostName returns host name
func GetHostName() string {
	return os.Getenv("HOSTNAME")
}

// Response -
type Response struct {
	*response
}
type response struct {
	err  error
	ch   chan struct{}
	done bool
}

// NewResponse -
func NewResponse() Response {
	return Response{
		&response{
			ch: make(chan struct{}),
		},
	}
}

// Fake a completed response.
func DoneResponse() Response {
	r := Response{
		&response{
			err:  nil,
			ch:   make(chan struct{}),
			done: true,
		},
	}
	close(r.ch)
	return r
}

// Error sends a response with the given error.  It may only be called once.
func (r Response) Error(err error) {
	// if this is called twice, it will panic; this is intentional
	r.err = err
	r.done = true
	close(r.ch)
}

// Done sends a non-error response unless Error has already been called, in which case this is a no-op.
func (r Response) Done() {
	if !r.done {
		close(r.ch)
	}
}

//WaitForNilOrErrorResponses waits on a variadic number of channels for either a nil response or an error
//response. If an error is received from a given channel then the returned error array will contain that error.
//The error will be at the index corresponding to the order in which the channel appear in the parameter list.
//If no errors is found then nil is returned.  This method also takes in a timeout in milliseconds. If a
//timeout is obtained then this function will stop waiting for the remaining responses and abort.
func WaitForNilOrErrorResponses(timeout time.Duration, responses ...Response) []error {
	timedOut := make(chan struct{})
	timer := time.AfterFunc(timeout, func() { close(timedOut) })
	defer timer.Stop()

	gotError := false
	errors := make([]error, 0, len(responses))
	for _, response := range responses {
		var err error
		select {
		case <-response.ch:
			// if a response is already available, use it
			err = response.err
		default:
			// otherwise, wait for either a response or a timeout
			select {
			case <-response.ch:
				err = response.err
			case <-timedOut:
				err = status.Error(codes.Aborted, "timeout")
			}
		}
		gotError = gotError || err != nil
		errors = append(errors, err)
	}

	if gotError {
		return errors
	}
	return nil
}
