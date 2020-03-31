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
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResponseCallback is the function signature for callbacks to execute after a response is received.
type ResponseCallback func(rpc string, response interface{}, reqArgs ...interface{})

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

type request struct {
	prev, next       *request
	notifyOnComplete chan<- struct{}
}

// RequestQueue represents a request processing queue where each request is processed to completion before another
// request is given the green light to proceed.
type RequestQueue struct {
	mutex sync.Mutex

	last, current  *request
	lastCompleteCh <-chan struct{}
}

// NewRequestQueue creates a new request queue
func NewRequestQueue() *RequestQueue {
	ch := make(chan struct{})
	close(ch) // assume the "current" request is already complete
	return &RequestQueue{lastCompleteCh: ch}
}

// WaitForGreenLight is invoked by a function processing a request to receive the green light before
// proceeding.  The caller can also provide a context with timeout.  The timeout will be triggered if the wait is
// too long (previous requests taking too long)
func (rq *RequestQueue) WaitForGreenLight(ctx context.Context) error {
	// add ourselves to the end of the queue
	rq.mutex.Lock()
	waitingOn := rq.lastCompleteCh

	ch := make(chan struct{})
	rq.lastCompleteCh = ch
	r := &request{notifyOnComplete: ch}

	r.prev = rq.last
	rq.last = r
	rq.mutex.Unlock()

	// wait for our turn
	select {
	case <-ctx.Done():
		// canceled, so cleanup
		rq.mutex.Lock()
		defer rq.mutex.Unlock()

		if _, notified := <-waitingOn; !notified {
			// on abort, skip our position in the queue
			r.prev.notifyOnComplete = r.notifyOnComplete
			// and remove ourselves from the queue
			if r.next != nil { // if we are somewhere in the middle of the queue
				r.prev.next = r.next
				r.next.prev = r.prev
			} else { // if we are at the end of the queue
				rq.last = r.prev
				r.prev.next = nil
			}

		} else {
			// context is canceled, but lock has been acquired, so just release the lock immediately
			rq.current = r
			rq.releaseWithoutLock()
		}
		return ctx.Err()

	case <-waitingOn:
		// lock is acquired
		rq.current = r
		return nil
	}
}

// RequestComplete must be invoked by a process when it completes processing the request.  That process must have
// invoked WaitForGreenLight() before.
func (rq *RequestQueue) RequestComplete() {
	rq.mutex.Lock()
	defer rq.mutex.Unlock()

	rq.releaseWithoutLock()
}

func (rq *RequestQueue) releaseWithoutLock() {
	// Notify the next waiting request.  This will panic if the lock is released more than once.
	close(rq.current.notifyOnComplete)

	if rq.current.next != nil {
		rq.current.next.prev = nil
	}
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
