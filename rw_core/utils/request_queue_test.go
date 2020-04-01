package utils

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRequestQueueOrdering(t *testing.T) {
	rq := NewRequestQueue()
	// acquire lock immediately, so our requests will queue up
	if err := rq.WaitForGreenLight(context.Background()); err != nil {
		t.Error(err)
		return
	}

	doneOrder := make([]int, 0, 10)

	wg := sync.WaitGroup{}
	wg.Add(10)

	// queue up 10 requests
	for i := 0; i < 10; i++ {
		go func(i int) {
			if err := rq.WaitForGreenLight(context.Background()); err != nil {
				t.Error(err)
			}
			doneOrder = append(doneOrder, i)
			rq.RequestComplete()

			wg.Done()
		}(i)

		// ensure that the last request is queued before starting the next one
		time.Sleep(time.Millisecond)
	}

	// complete the first process
	rq.RequestComplete()

	wg.Wait()

	// verify that the processes completed in the correct order
	for i := 0; i < 10; i++ {
		if doneOrder[i] != i {
			t.Errorf("Thread %d executed at time %d, should have been %d", doneOrder[i], i, doneOrder[i])
		}
	}
}

func TestRequestQueueCancellation(t *testing.T) {
	rq := NewRequestQueue()
	// acquire lock immediately, so our requests will queue up
	if err := rq.WaitForGreenLight(context.Background()); err != nil {
		t.Error(err)
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(10)

	willCancelContext, cancel := context.WithCancel(context.Background())

	// queue up 10 requests
	for i := 0; i < 10; i++ {
		go func(i int) {
			// will cancel processes 0, 1, 4, 5, 8, 9
			willCancel := (i/2)%2 == 0

			ctx := context.Background()
			if willCancel {
				ctx = willCancelContext
			}

			if err := rq.WaitForGreenLight(ctx); err != nil {
				if willCancel && err == context.Canceled {
					// canceled as expected
				} else {
					t.Errorf("wait gave unexpected error %s", err)
				}
			} else {
				if willCancel {
					t.Error("this should have been canceled")
				} else {
					// completed as expected
				}
				rq.RequestComplete()
			}
			wg.Done()
		}(i)
	}

	// cancel processes
	cancel()

	// wait a moment for the cancellations to go through
	time.Sleep(time.Millisecond)

	// release the lock, and allow the processes to complete
	rq.RequestComplete()

	// wait for all processes to complete
	wg.Wait()
}
