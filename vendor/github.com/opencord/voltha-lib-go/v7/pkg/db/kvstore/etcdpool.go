/*
 * Copyright 2021-present Open Networking Foundation

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
package kvstore

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"go.etcd.io/etcd/clientv3"
)

// EtcdClientAllocator represents a generic interface to allocate an Etcd Client
type EtcdClientAllocator interface {
	Get(context.Context) (*clientv3.Client, error)
	Put(*clientv3.Client)
	Close(ctx context.Context)
}

// NewRoundRobinEtcdClientAllocator creates a new ETCD Client Allocator using a Round Robin scheme
func NewRoundRobinEtcdClientAllocator(endpoints []string, timeout time.Duration, capacity, maxUsage int, level log.LogLevel) (EtcdClientAllocator, error) {
	return &roundRobin{
		all:       make(map[*clientv3.Client]*rrEntry),
		full:      make(map[*clientv3.Client]*rrEntry),
		waitList:  list.New(),
		max:       maxUsage,
		capacity:  capacity,
		timeout:   timeout,
		endpoints: endpoints,
		logLevel:  level,
		closingCh: make(chan struct{}, capacity*maxUsage),
		stopCh:    make(chan struct{}),
	}, nil
}

type rrEntry struct {
	client *clientv3.Client
	count  int
	age    time.Time
}

type roundRobin struct {
	//block chan struct{}
	sync.Mutex
	available []*rrEntry
	all       map[*clientv3.Client]*rrEntry
	full      map[*clientv3.Client]*rrEntry
	waitList  *list.List
	max       int
	capacity  int
	timeout   time.Duration
	//ageOut    time.Duration
	endpoints []string
	size      int
	logLevel  log.LogLevel
	closing   bool
	closingCh chan struct{}
	stopCh    chan struct{}
}

// Get returns an Etcd client. If not is available, it will create one
// until the maximum allowed capacity.  If maximum capacity has been
// reached then it will wait until s used one is freed.
func (r *roundRobin) Get(ctx context.Context) (*clientv3.Client, error) {
	r.Lock()

	if r.closing {
		r.Unlock()
		return nil, errors.New("pool-is-closing")
	}

	// first determine if we need to block, which would mean the
	// available queue is empty and we are at capacity
	if len(r.available) == 0 && r.size >= r.capacity {

		// create a channel on which to wait and
		// add it to the list
		ch := make(chan struct{})
		element := r.waitList.PushBack(ch)
		r.Unlock()

		// block until it is our turn or context
		// expires or is canceled
		select {
		case <-r.stopCh:
			logger.Info(ctx, "stop-waiting-pool-is-closing")
			r.waitList.Remove(element)
			return nil, errors.New("stop-waiting-pool-is-closing")
		case <-ch:
			r.waitList.Remove(element)
		case <-ctx.Done():
			r.waitList.Remove(element)
			return nil, ctx.Err()
		}
		r.Lock()
	}

	defer r.Unlock()
	if len(r.available) > 0 {
		// pull off back end as it is operationally quicker
		last := len(r.available) - 1
		entry := r.available[last]
		entry.count++
		if entry.count >= r.max {
			r.available = r.available[:last]
			r.full[entry.client] = entry
		}
		entry.age = time.Now()
		return entry.client, nil
	}

	logConfig := log.ConstructZapConfig(log.JSON, r.logLevel, log.Fields{})
	// increase capacity
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   r.endpoints,
		DialTimeout: r.timeout,
		LogConfig:   &logConfig,
	})
	if err != nil {
		return nil, err
	}
	entry := &rrEntry{
		client: client,
		count:  1,
	}
	r.all[entry.client] = entry

	if r.max > 1 {
		r.available = append(r.available, entry)
	} else {
		r.full[entry.client] = entry
	}
	r.size++
	return client, nil
}

// Put returns the Etcd Client back to the pool
func (r *roundRobin) Put(client *clientv3.Client) {
	r.Lock()

	entry := r.all[client]
	entry.count--

	if r.closing {
		// Close client if count is 0
		if entry.count == 0 {
			if err := entry.client.Close(); err != nil {
				logger.Warnw(context.Background(), "error-closing-client", log.Fields{"error": err})
			}
			delete(r.all, entry.client)
		}
		// Notify Close function that a client was returned to the pool
		r.closingCh <- struct{}{}
		r.Unlock()
		return
	}

	// This entry is now available for use, so
	// if in full map add it to available and
	// remove from full
	if _, ok := r.full[client]; ok {
		r.available = append(r.available, entry)
		delete(r.full, client)
	}

	front := r.waitList.Front()
	if front != nil {
		ch := r.waitList.Remove(front)
		r.Unlock()
		// need to unblock if someone is waiting
		ch.(chan struct{}) <- struct{}{}
		return
	}
	r.Unlock()
}

func (r *roundRobin) Close(ctx context.Context) {
	r.Lock()
	r.closing = true

	// Notify anyone waiting for a client to stop waiting
	close(r.stopCh)

	// Clean-up unused clients
	for i := 0; i < len(r.available); i++ {
		// Count 0 means no one is using that client
		if r.available[i].count == 0 {
			if err := r.available[i].client.Close(); err != nil {
				logger.Warnw(ctx, "failure-closing-client", log.Fields{"client": r.available[i].client, "error": err})
			}
			// Remove client for all list
			delete(r.all, r.available[i].client)
		}
	}

	// Figure out how many clients are in use
	numberInUse := 0
	for _, rrEntry := range r.all {
		numberInUse += rrEntry.count
	}
	r.Unlock()

	if numberInUse == 0 {
		logger.Info(ctx, "no-connection-in-use")
		return
	}

	logger.Infow(ctx, "waiting-for-clients-return", log.Fields{"count": numberInUse})

	// Wait for notifications when a client is returned to the pool
	for {
		select {
		case <-r.closingCh:
			numberInUse--
			if numberInUse == 0 {
				logger.Info(ctx, "all-connections-closed")
				return
			}
		case <-ctx.Done():
			logger.Warnw(ctx, "context-done", log.Fields{"error": ctx.Err()})
			return
		}
	}
}
