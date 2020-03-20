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

	"github.com/opencord/voltha-lib-go/v3/pkg/log"
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
	channel chan struct{}
	done    chan struct{}
}

func newRequest() *request {
	return &request{
		channel: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// RequestQueue represents a request processing queue where each request is processed to completion before another
// request is given the green light to proceed.
type RequestQueue struct {
	queue                     chan *request
	requestCompleteIndication chan struct{}
	queueID                   string
	stopOnce                  sync.Once
	stopped                   bool
}

// NewRequestQueue creates a new request queue. maxQueueSize is the maximum size of the queue. queueID is used mostly
// for logging.
func NewRequestQueue(queueID string, maxQueueSize int) *RequestQueue {
	return &RequestQueue{
		queueID:                   queueID,
		queue:                     make(chan *request, maxQueueSize),
		requestCompleteIndication: make(chan struct{}),
	}
}

// Start starts the request processing queue in its own go routine
func (rq *RequestQueue) Start() {
	go func() {
		for {
			req, ok := <-rq.queue
			if !ok {
				logger.Warnw("request-sequencer-queue-closed", log.Fields{"id": rq.queueID})
				break
			}
			// If the request is waiting then closing the reqChnl will trigger the request to proceed.  Otherwise,
			// if the request was cancelled then this will just clean up.
			close(req.channel)

			// Wait for either a request complete indication or a request aborted due to timeout
			select {
			case <-req.done:
			case <-rq.requestCompleteIndication:
			}
		}
	}()
}

// WaitForGreenLight is invoked by a function processing a request to receive the green light before
// proceeding.  The caller can also provide a context with timeout.  The timeout will be triggered if the wait is
// too long (previous requests taking too long)
func (rq *RequestQueue) WaitForGreenLight(ctx context.Context) error {
	if rq.stopped {
		return status.Errorf(codes.Aborted, "queue-already-stopped-%s", rq.queueID)
	}
	request := newRequest()
	// Queue the request
	rq.queue <- request
	select {
	case <-request.channel:
		return nil
	case <-ctx.Done():
		close(request.done)
		return ctx.Err()
	}
}

// RequestComplete must be invoked by a process when it completes processing the request.  That process must have
// invoked WaitForGreenLight() before.
func (rq *RequestQueue) RequestComplete() {
	if !rq.stopped {
		rq.requestCompleteIndication <- struct{}{}
	}
}

// Stop must only be invoked by the process that started the request queue.   Prior to invoking Stop, WaitForGreenLight
// must be invoked.
func (rq *RequestQueue) Stop() {
	rq.stopOnce.Do(func() {
		rq.stopped = true
		close(rq.requestCompleteIndication)
		close(rq.queue)
	})
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
