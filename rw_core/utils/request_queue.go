/*
 * Copyright 2020-present Open Networking Foundation
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
			r.prev.notifyOnComplete = r.notifyOnComplete
			// and remove ourselves from the queue
			if r.next != nil { // if we are somewhere in the middle of the queue
				r.prev.next = r.next
				r.next.prev = r.prev
			} else { // if we are at the end of the queue
				rq.last = r.prev
				r.prev.next = nil
			}
		}
		return ctx.Err()

	case <-waitingOn:
		// Previous request has signaled that it is complete.
		// This request now can proceed as the active
		// request

		rq.mutex.Lock()
		defer rq.mutex.Unlock()
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
