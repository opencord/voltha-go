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
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
	"sync"
	"time"
)

const (
	// Default Minimal Interval for posting alive state of backend kvstore on Liveness Channel
	DefaultLivenessChannelInterval = time.Second * 30
)

// Backend structure holds details for accessing the kv store
type Backend struct {
	sync.RWMutex
	Client                  kvstore.Client
	StoreType               string
	Host                    string
	Port                    int
	Timeout                 int
	PathPrefix              string
	alive                   bool          // Is this backend connection alive?
	liveness                chan bool     // channel to post alive state
	LivenessChannelInterval time.Duration // regularly push alive state beyond this interval
	lastLivenessTime        time.Time     // Instant of last alive state push
}

// NewBackend creates a new instance of a Backend structure
func NewBackend(storeType string, host string, port int, timeout int, pathPrefix string) *Backend {
	var err error

	b := &Backend{
		StoreType:               storeType,
		Host:                    host,
		Port:                    port,
		Timeout:                 timeout,
		LivenessChannelInterval: DefaultLivenessChannelInterval,
		PathPrefix:              pathPrefix,
		alive:                   false, // connection considered down at start
	}

	address := host + ":" + strconv.Itoa(port)
	if b.Client, err = b.newClient(address, timeout); err != nil {
		log.Errorw("failed-to-create-kv-client",
			log.Fields{
				"type": storeType, "host": host, "port": port,
				"timeout": timeout, "prefix": pathPrefix,
				"error": err.Error(),
			})
	}

	return b
}

func (b *Backend) newClient(address string, timeout int) (kvstore.Client, error) {
	switch b.StoreType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("unsupported-kv-store")
}

func (b *Backend) makePath(key string) string {
	path := fmt.Sprintf("%s/%s", b.PathPrefix, key)
	return path
}

func (b *Backend) updateLiveness(alive bool) {
	// Periodically push stream of liveness data to the channel,
	// so that in a live state, the core does not timeout and
	// send a forced liveness message. Push alive state if the
	// last push to channel was beyond livenessChannelInterval
	if b.liveness != nil {

		if b.alive != alive {
			log.Debug("update-liveness-channel-reason-change")
			b.liveness <- alive
			b.lastLivenessTime = time.Now()
		} else if time.Now().Sub(b.lastLivenessTime) > b.LivenessChannelInterval {
			log.Debug("update-liveness-channel-reason-interval")
			b.liveness <- alive
			b.lastLivenessTime = time.Now()
		}
	}

	// Emit log message only for alive state change
	if b.alive != alive {
		log.Debugw("change-kvstore-alive-status", log.Fields{"alive": alive})
		b.alive = alive
	}
}

// Perform a dummy Key Lookup on kvstore to test Connection Liveness and
// post on Liveness channel
func (b *Backend) PerformLivenessCheck(timeout int) bool {
	alive := b.Client.IsConnectionUp(timeout)
	log.Debugw("kvstore-liveness-check-result", log.Fields{"alive": alive})

	b.updateLiveness(alive)
	return alive
}

// Enable the liveness monitor channel. This channel will report
// a "true" or "false" on every kvstore operation which indicates whether
// or not the connection is still Live. This channel is then picked up
// by the service (i.e. rw_core / ro_core) to update readiness status
// and/or take other actions.
func (b *Backend) EnableLivenessChannel() chan bool {
	log.Debug("enable-kvstore-liveness-channel")

	if b.liveness == nil {
		log.Debug("create-kvstore-liveness-channel")

		// Channel size of 10 to avoid any possibility of blocking in Load conditions
		b.liveness = make(chan bool, 10)

		// Post initial alive state
		b.liveness <- b.alive
		b.lastLivenessTime = time.Now()
	}

	return b.liveness
}

// Extract Alive status of Kvstore based on type of error
func (b *Backend) isErrorIndicatingAliveKvstore(err error) bool {
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
func (b *Backend) List(key string) (map[string]*kvstore.KVPair, error) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("listing-key", log.Fields{"key": key, "path": formattedPath})

	pair, err := b.Client.List(formattedPath, b.Timeout)

	b.updateLiveness(b.isErrorIndicatingAliveKvstore(err))

	return pair, err
}

// Get retrieves an item that matches the specified key
func (b *Backend) Get(key string) (*kvstore.KVPair, error) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("getting-key", log.Fields{"key": key, "path": formattedPath})

	pair, err := b.Client.Get(formattedPath, b.Timeout)

	b.updateLiveness(b.isErrorIndicatingAliveKvstore(err))

	return pair, err
}

// Put stores an item value under the specifed key
func (b *Backend) Put(key string, value interface{}) error {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("putting-key", log.Fields{"key": key, "value": string(value.([]byte)), "path": formattedPath})

	err := b.Client.Put(formattedPath, value, b.Timeout)

	b.updateLiveness(b.isErrorIndicatingAliveKvstore(err))

	return err
}

// Delete removes an item under the specified key
func (b *Backend) Delete(key string) error {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("deleting-key", log.Fields{"key": key, "path": formattedPath})

	err := b.Client.Delete(formattedPath, b.Timeout)

	b.updateLiveness(b.isErrorIndicatingAliveKvstore(err))

	return err
}

// CreateWatch starts watching events for the specified key
func (b *Backend) CreateWatch(key string) chan *kvstore.Event {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("creating-key-watch", log.Fields{"key": key, "path": formattedPath})

	return b.Client.Watch(formattedPath)
}

// DeleteWatch stops watching events for the specified key
func (b *Backend) DeleteWatch(key string, ch chan *kvstore.Event) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("deleting-key-watch", log.Fields{"key": key, "path": formattedPath})

	b.Client.CloseWatch(formattedPath, ch)
}
