/*
 * Copyright 2018-present Open Networking Foundation

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

package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// Default Minimal Interval for posting alive state of backend kvstore on Liveness Channel
	DefaultLivenessChannelInterval = time.Second * 30
)

// Backend structure holds details for accessing the kv store
type Backend struct {
	Client                  kvstore.Client
	StoreType               string
	Timeout                 time.Duration
	Address                 string
	PathPrefix              string
	alive                   bool // Is this backend connection alive?
	livenessMutex           sync.Mutex
	liveness                chan bool     // channel to post alive state
	LivenessChannelInterval time.Duration // regularly push alive state beyond this interval
	lastLivenessTime        time.Time     // Instant of last alive state push
}

// NewBackend creates a new instance of a Backend structure
func NewBackend(ctx context.Context, storeType string, address string, timeout time.Duration, pathPrefix string) *Backend {
	var err error

	b := &Backend{
		StoreType:               storeType,
		Address:                 address,
		Timeout:                 timeout,
		LivenessChannelInterval: DefaultLivenessChannelInterval,
		PathPrefix:              pathPrefix,
		alive:                   false, // connection considered down at start
	}

	if b.Client, err = b.newClient(ctx, address, timeout); err != nil {
		logger.Errorw(ctx, "failed-to-create-kv-client",
			log.Fields{
				"type": storeType, "address": address,
				"timeout": timeout, "prefix": pathPrefix,
				"error": err.Error(),
			})
	}

	return b
}

func (b *Backend) newClient(ctx context.Context, address string, timeout time.Duration) (kvstore.Client, error) {
	switch b.StoreType {
	case "consul":
		return kvstore.NewConsulClient(ctx, address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(ctx, address, timeout, log.WarnLevel)
	}
	return nil, errors.New("unsupported-kv-store")
}

func (b *Backend) makePath(ctx context.Context, key string) string {
	path := fmt.Sprintf("%s/%s", b.PathPrefix, key)
	return path
}

func (b *Backend) updateLiveness(ctx context.Context, alive bool) {
	// Periodically push stream of liveness data to the channel,
	// so that in a live state, the core does not timeout and
	// send a forced liveness message. Push alive state if the
	// last push to channel was beyond livenessChannelInterval
	b.livenessMutex.Lock()
	defer b.livenessMutex.Unlock()
	if b.liveness != nil {
		if b.alive != alive {
			logger.Debug(ctx, "update-liveness-channel-reason-change")
			b.liveness <- alive
			b.lastLivenessTime = time.Now()
		} else if time.Since(b.lastLivenessTime) > b.LivenessChannelInterval {
			logger.Debug(ctx, "update-liveness-channel-reason-interval")
			b.liveness <- alive
			b.lastLivenessTime = time.Now()
		}
	}

	// Emit log message only for alive state change
	if b.alive != alive {
		logger.Debugw(ctx, "change-kvstore-alive-status", log.Fields{"alive": alive})
		b.alive = alive
	}
}

// Perform a dummy Key Lookup on kvstore to test Connection Liveness and
// post on Liveness channel
func (b *Backend) PerformLivenessCheck(ctx context.Context) bool {
	alive := b.Client.IsConnectionUp(ctx)
	logger.Debugw(ctx, "kvstore-liveness-check-result", log.Fields{"alive": alive})

	b.updateLiveness(ctx, alive)
	return alive
}

// Enable the liveness monitor channel. This channel will report
// a "true" or "false" on every kvstore operation which indicates whether
// or not the connection is still Live. This channel is then picked up
// by the service (i.e. rw_core / ro_core) to update readiness status
// and/or take other actions.
func (b *Backend) EnableLivenessChannel(ctx context.Context) chan bool {
	logger.Debug(ctx, "enable-kvstore-liveness-channel")
	b.livenessMutex.Lock()
	defer b.livenessMutex.Unlock()
	if b.liveness == nil {
		b.liveness = make(chan bool, 10)
		b.liveness <- b.alive
		b.lastLivenessTime = time.Now()
	}

	return b.liveness
}

// Extract Alive status of Kvstore based on type of error
func (b *Backend) isErrorIndicatingAliveKvstore(ctx context.Context, err error) bool {
	// Alive unless observed an error indicating so
	alive := true

	if err != nil {

		// timeout indicates kvstore not reachable/alive
		if err == context.DeadlineExceeded {
			alive = false
		}

		// Need to analyze client-specific errors based on backend type
		if b.StoreType == "etcd" {

			// For etcd backend, consider not-alive only for errors indicating
			// timedout request or unavailable/corrupted cluster. For all remaining
			// error codes listed in https://godoc.org/google.golang.org/grpc/codes#Code,
			// we would not infer a not-alive backend because such a error may also
			// occur due to bad client requests or sequence of operations
			switch status.Code(err) {
			case codes.DeadlineExceeded:
				fallthrough
			case codes.Unavailable:
				fallthrough
			case codes.DataLoss:
				alive = false
			}

			//} else {
			// TODO: Implement for consul backend; would it be needed ever?
		}
	}

	return alive
}

// List retrieves one or more items that match the specified key
func (b *Backend) List(ctx context.Context, key string) (map[string]*kvstore.KVPair, error) {
	span, ctx := log.CreateChildSpan(ctx, "etcd-list")
	defer span.Finish()

	formattedPath := b.makePath(ctx, key)
	logger.Debugw(ctx, "listing-key", log.Fields{"key": key, "path": formattedPath})

	pair, err := b.Client.List(ctx, formattedPath)

	b.updateLiveness(ctx, b.isErrorIndicatingAliveKvstore(ctx, err))

	return pair, err
}

// Get retrieves an item that matches the specified key
func (b *Backend) Get(ctx context.Context, key string) (*kvstore.KVPair, error) {
	span, ctx := log.CreateChildSpan(ctx, "etcd-get")
	defer span.Finish()

	formattedPath := b.makePath(ctx, key)
	logger.Debugw(ctx, "getting-key", log.Fields{"key": key, "path": formattedPath})

	pair, err := b.Client.Get(ctx, formattedPath)

	b.updateLiveness(ctx, b.isErrorIndicatingAliveKvstore(ctx, err))

	return pair, err
}

// Put stores an item value under the specifed key
func (b *Backend) Put(ctx context.Context, key string, value interface{}) error {
	span, ctx := log.CreateChildSpan(ctx, "etcd-put")
	defer span.Finish()

	formattedPath := b.makePath(ctx, key)
	logger.Debugw(ctx, "putting-key", log.Fields{"key": key, "path": formattedPath})

	err := b.Client.Put(ctx, formattedPath, value)

	b.updateLiveness(ctx, b.isErrorIndicatingAliveKvstore(ctx, err))

	return err
}

// Delete removes an item under the specified key
func (b *Backend) Delete(ctx context.Context, key string) error {
	span, ctx := log.CreateChildSpan(ctx, "etcd-delete")
	defer span.Finish()

	formattedPath := b.makePath(ctx, key)
	logger.Debugw(ctx, "deleting-key", log.Fields{"key": key, "path": formattedPath})

	err := b.Client.Delete(ctx, formattedPath)

	b.updateLiveness(ctx, b.isErrorIndicatingAliveKvstore(ctx, err))

	return err
}

// CreateWatch starts watching events for the specified key
func (b *Backend) CreateWatch(ctx context.Context, key string, withPrefix bool) chan *kvstore.Event {
	span, ctx := log.CreateChildSpan(ctx, "etcd-create-watch")
	defer span.Finish()

	formattedPath := b.makePath(ctx, key)
	logger.Debugw(ctx, "creating-key-watch", log.Fields{"key": key, "path": formattedPath})

	return b.Client.Watch(ctx, formattedPath, withPrefix)
}

// DeleteWatch stops watching events for the specified key
func (b *Backend) DeleteWatch(ctx context.Context, key string, ch chan *kvstore.Event) {
	span, ctx := log.CreateChildSpan(ctx, "etcd-delete-watch")
	defer span.Finish()

	formattedPath := b.makePath(ctx, key)
	logger.Debugw(ctx, "deleting-key-watch", log.Fields{"key": key, "path": formattedPath})

	b.Client.CloseWatch(ctx, formattedPath, ch)
}
