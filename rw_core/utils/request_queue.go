/*
 * Copyright 2020-2024 Open Networking Foundation (ONF) and the ONF Contributors
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
	"sync"
)

type request struct {
	prev, next       *request
	notifyOnComplete chan<- struct{}
}

// RequestQueue represents a request processing queue where each request is processed to completion before another
// request is given the green light to proceed.
type RequestQueue struct {
	last, current  *request
	lastCompleteCh <-chan struct{}
	mutex          sync.Mutex
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

	if rq.last != nil {
		rq.last.next, r.prev = r, rq.last
	}
	rq.last = r
	rq.mutex.Unlock()

	// wait for our turn
	select {
	case <-ctx.Done():
		// canceled, so cleanup
		rq.mutex.Lock()
		defer rq.mutex.Unlock()

		select {
		case <-waitingOn:
			// chan has been closed, so the lock has been acquired
			// context is canceled, so just release the lock immediately
			rq.current = r
			rq.releaseWithoutLock()
		default:
			// on abort, skip our position in the queue
			if r.prev != nil {
				r.prev.notifyOnComplete = r.notifyOnComplete
			} else {
				// On abort, if the previous pointer is nil, transfer notifyOnComplete to the current processing request
				if rq.current != nil {
					rq.current.notifyOnComplete = r.notifyOnComplete
				}
			}
			// Remove ourselves from the queue
			if r.next != nil && r.prev != nil { // If we are somewhere in the middle of the queue
				r.prev.next = r.next
				r.next.prev = r.prev
			} else if r.prev != nil { // If we are at the end of the queue
				rq.last = r.prev
				r.prev.next = nil
			} else if r.next != nil { // If we are at the start of the queue
				r.next.prev = nil
			} else { // If we are the only request in the queue
				rq.last = nil
			}
			// Clear references to help garbage collection
			r.prev = nil
			r.next = nil
		}
		return ctx.Err()

	case <-waitingOn:
		// Previous request has signaled that it is complete.
		// This request now can proceed as the active
		// request
		rq.mutex.Lock()
		defer rq.mutex.Unlock()
		rq.current = r

		// Remove the processed request from the queue
		if r.prev != nil {
			r.prev.next = r.next
		}
		if r.next != nil {
			r.next.prev = r.prev
		}
		if rq.last == r {
			rq.last = r.prev
		}

		// Clear references to help garbage collection
		r.prev = nil
		r.next = nil
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
	if rq.current.notifyOnComplete != nil {
		close(rq.current.notifyOnComplete)
		rq.current.notifyOnComplete = nil
	}

	if rq.current.next != nil {
		rq.current.next.prev = nil
	}

	// Clear the current request reference to help garbage collection
	rq.current = nil
}
