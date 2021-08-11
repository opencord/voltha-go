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
package kvstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	v3Client "go.etcd.io/etcd/clientv3"
	v3rpcTypes "go.etcd.io/etcd/etcdserver/api/v3rpc/rpctypes"
)

const (
	poolCapacityEnvName = "VOLTHA_ETCD_CLIENT_POOL_CAPACITY"
	maxUsageEnvName     = "VOLTHA_ETCD_CLIENT_MAX_USAGE"
)

const (
	defaultMaxPoolCapacity = 1000 // Default size of an Etcd Client pool
	defaultMaxPoolUsage    = 100  // Maximum concurrent request an Etcd Client is allowed to process
)

// EtcdClient represents the Etcd KV store client
type EtcdClient struct {
	pool               EtcdClientAllocator
	watchedChannels    sync.Map
	watchedClients     map[string]*v3Client.Client
	watchedClientsLock sync.RWMutex
}

// NewEtcdCustomClient returns a new client for the Etcd KV store allowing
// the called to specify etcd client configuration
func NewEtcdCustomClient(ctx context.Context, addr string, timeout time.Duration, level log.LogLevel) (*EtcdClient, error) {
	// Get the capacity and max usage from the environment
	capacity := defaultMaxPoolCapacity
	maxUsage := defaultMaxPoolUsage
	if capacityStr, present := os.LookupEnv(poolCapacityEnvName); present {
		if val, err := strconv.Atoi(capacityStr); err == nil {
			capacity = val
			logger.Infow(ctx, "env-variable-set", log.Fields{"pool-capacity": capacity})
		} else {
			logger.Warnw(ctx, "invalid-capacity-value", log.Fields{"error": err, "capacity": capacityStr})
		}
	}
	if maxUsageStr, present := os.LookupEnv(maxUsageEnvName); present {
		if val, err := strconv.Atoi(maxUsageStr); err == nil {
			maxUsage = val
			logger.Infow(ctx, "env-variable-set", log.Fields{"max-usage": maxUsage})
		} else {
			logger.Warnw(ctx, "invalid-max-usage-value", log.Fields{"error": err, "max-usage": maxUsageStr})
		}
	}

	var err error

	pool, err := NewRoundRobinEtcdClientAllocator([]string{addr}, timeout, capacity, maxUsage, level)
	if err != nil {
		logger.Errorw(ctx, "failed-to-create-rr-client", log.Fields{
			"error": err,
		})
	}

	logger.Infow(ctx, "etcd-pool-created", log.Fields{"capacity": capacity, "max-usage": maxUsage})

	return &EtcdClient{pool: pool,
		watchedClients: make(map[string]*v3Client.Client),
	}, nil
}

// NewEtcdClient returns a new client for the Etcd KV store
func NewEtcdClient(ctx context.Context, addr string, timeout time.Duration, level log.LogLevel) (*EtcdClient, error) {
	return NewEtcdCustomClient(ctx, addr, timeout, level)
}

// IsConnectionUp returns whether the connection to the Etcd KV store is up.  If a timeout occurs then
// it is assumed the connection is down or unreachable.
func (c *EtcdClient) IsConnectionUp(ctx context.Context) bool {
	// Let's try to get a non existent key.  If the connection is up then there will be no error returned.
	if _, err := c.Get(ctx, "non-existent-key"); err != nil {
		return false
	}
	return true
}

// List returns an array of key-value pairs with key as a prefix.  Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) List(ctx context.Context, key string) (map[string]*KVPair, error) {
	client, err := c.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer c.pool.Put(client)
	resp, err := client.Get(ctx, key, v3Client.WithPrefix())

	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}
	m := make(map[string]*KVPair)
	for _, ev := range resp.Kvs {
		m[string(ev.Key)] = NewKVPair(string(ev.Key), ev.Value, "", ev.Lease, ev.Version)
	}
	return m, nil
}

// Get returns a key-value pair for a given key. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Get(ctx context.Context, key string) (*KVPair, error) {
	client, err := c.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer c.pool.Put(client)

	attempt := 0

startLoop:
	for {
		resp, err := client.Get(ctx, key)
		if err != nil {
			switch err {
			case context.Canceled:
				logger.Warnw(ctx, "context-cancelled", log.Fields{"error": err})
			case context.DeadlineExceeded:
				logger.Warnw(ctx, "context-deadline-exceeded", log.Fields{"error": err, "context": ctx})
			case v3rpcTypes.ErrEmptyKey:
				logger.Warnw(ctx, "etcd-client-error", log.Fields{"error": err})
			case v3rpcTypes.ErrLeaderChanged,
				v3rpcTypes.ErrGRPCNoLeader,
				v3rpcTypes.ErrTimeout,
				v3rpcTypes.ErrTimeoutDueToLeaderFail,
				v3rpcTypes.ErrTimeoutDueToConnectionLost:
				// Retry for these server errors
				attempt += 1
				if er := backoff(ctx, attempt); er != nil {
					logger.Warnw(ctx, "get-retries-failed", log.Fields{"key": key, "error": er, "attempt": attempt})
					return nil, err
				}
				logger.Warnw(ctx, "retrying-get", log.Fields{"key": key, "error": err, "attempt": attempt})
				goto startLoop
			default:
				logger.Warnw(ctx, "etcd-server-error", log.Fields{"error": err})
			}
			return nil, err
		}

		for _, ev := range resp.Kvs {
			// Only one value is returned
			return NewKVPair(string(ev.Key), ev.Value, "", ev.Lease, ev.Version), nil
		}
		return nil, nil
	}
}

// Put writes a key-value pair to the KV store.  Value can only be a string or []byte since the etcd API
// accepts only a string as a value for a put operation. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Put(ctx context.Context, key string, value interface{}) error {

	// Validate that we can convert value to a string as etcd API expects a string
	var val string
	var err error
	if val, err = ToString(value); err != nil {
		return fmt.Errorf("unexpected-type-%T", value)
	}

	client, err := c.pool.Get(ctx)
	if err != nil {
		return err
	}
	defer c.pool.Put(client)

	attempt := 0
startLoop:
	for {
		_, err = client.Put(ctx, key, val)
		if err != nil {
			switch err {
			case context.Canceled:
				logger.Warnw(ctx, "context-cancelled", log.Fields{"error": err})
			case context.DeadlineExceeded:
				logger.Warnw(ctx, "context-deadline-exceeded", log.Fields{"error": err, "context": ctx})
			case v3rpcTypes.ErrEmptyKey:
				logger.Warnw(ctx, "etcd-client-error", log.Fields{"error": err})
			case v3rpcTypes.ErrLeaderChanged,
				v3rpcTypes.ErrGRPCNoLeader,
				v3rpcTypes.ErrTimeout,
				v3rpcTypes.ErrTimeoutDueToLeaderFail,
				v3rpcTypes.ErrTimeoutDueToConnectionLost:
				// Retry for these server errors
				attempt += 1
				if er := backoff(ctx, attempt); er != nil {
					logger.Warnw(ctx, "put-retries-failed", log.Fields{"key": key, "error": er, "attempt": attempt})
					return err
				}
				logger.Warnw(ctx, "retrying-put", log.Fields{"key": key, "error": err, "attempt": attempt})
				goto startLoop
			default:
				logger.Warnw(ctx, "etcd-server-error", log.Fields{"error": err})
			}
			return err
		}
		return nil
	}
}

// Delete removes a key from the KV store. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Delete(ctx context.Context, key string) error {
	client, err := c.pool.Get(ctx)
	if err != nil {
		return err
	}
	defer c.pool.Put(client)

	attempt := 0
startLoop:
	for {
		_, err = client.Delete(ctx, key)
		if err != nil {
			switch err {
			case context.Canceled:
				logger.Warnw(ctx, "context-cancelled", log.Fields{"error": err})
			case context.DeadlineExceeded:
				logger.Warnw(ctx, "context-deadline-exceeded", log.Fields{"error": err, "context": ctx})
			case v3rpcTypes.ErrEmptyKey:
				logger.Warnw(ctx, "etcd-client-error", log.Fields{"error": err})
			case v3rpcTypes.ErrLeaderChanged,
				v3rpcTypes.ErrGRPCNoLeader,
				v3rpcTypes.ErrTimeout,
				v3rpcTypes.ErrTimeoutDueToLeaderFail,
				v3rpcTypes.ErrTimeoutDueToConnectionLost:
				// Retry for these server errors
				attempt += 1
				if er := backoff(ctx, attempt); er != nil {
					logger.Warnw(ctx, "delete-retries-failed", log.Fields{"key": key, "error": er, "attempt": attempt})
					return err
				}
				logger.Warnw(ctx, "retrying-delete", log.Fields{"key": key, "error": err, "attempt": attempt})
				goto startLoop
			default:
				logger.Warnw(ctx, "etcd-server-error", log.Fields{"error": err})
			}
			return err
		}
		logger.Debugw(ctx, "key(s)-deleted", log.Fields{"key": key})
		return nil
	}
}

func (c *EtcdClient) DeleteWithPrefix(ctx context.Context, prefixKey string) error {

	client, err := c.pool.Get(ctx)
	if err != nil {
		return err
	}
	defer c.pool.Put(client)

	//delete the prefix
	if _, err := client.Delete(ctx, prefixKey, v3Client.WithPrefix()); err != nil {
		logger.Errorw(ctx, "failed-to-delete-prefix-key", log.Fields{"key": prefixKey, "error": err})
		return err
	}
	logger.Debugw(ctx, "key(s)-deleted", log.Fields{"key": prefixKey})
	return nil
}

// Watch provides the watch capability on a given key.  It returns a channel onto which the callee needs to
// listen to receive Events.
func (c *EtcdClient) Watch(ctx context.Context, key string, withPrefix bool) chan *Event {
	var err error
	// Reuse the Etcd client when multiple callees are watching the same key.
	c.watchedClientsLock.Lock()
	client, exist := c.watchedClients[key]
	if !exist {
		client, err = c.pool.Get(ctx)
		if err != nil {
			logger.Errorw(ctx, "failed-to-an-etcd-client", log.Fields{"key": key, "error": err})
			c.watchedClientsLock.Unlock()
			return nil
		}
		c.watchedClients[key] = client
	}
	c.watchedClientsLock.Unlock()

	w := v3Client.NewWatcher(client)
	ctx, cancel := context.WithCancel(ctx)
	var channel v3Client.WatchChan
	if withPrefix {
		channel = w.Watch(ctx, key, v3Client.WithPrefix())
	} else {
		channel = w.Watch(ctx, key)
	}

	// Create a new channel
	ch := make(chan *Event, maxClientChannelBufferSize)

	// Keep track of the created channels so they can be closed when required
	channelMap := make(map[chan *Event]v3Client.Watcher)
	channelMap[ch] = w
	channelMaps := c.addChannelMap(key, channelMap)

	// Changing the log field (from channelMaps) as the underlying logger cannot format the map of channels into a
	// json format.
	logger.Debugw(ctx, "watched-channels", log.Fields{"len": len(channelMaps)})
	// Launch a go routine to listen for updates
	go c.listenForKeyChange(ctx, channel, ch, cancel)

	return ch

}

func (c *EtcdClient) addChannelMap(key string, channelMap map[chan *Event]v3Client.Watcher) []map[chan *Event]v3Client.Watcher {
	var channels interface{}
	var exists bool

	if channels, exists = c.watchedChannels.Load(key); exists {
		channels = append(channels.([]map[chan *Event]v3Client.Watcher), channelMap)
	} else {
		channels = []map[chan *Event]v3Client.Watcher{channelMap}
	}
	c.watchedChannels.Store(key, channels)

	return channels.([]map[chan *Event]v3Client.Watcher)
}

func (c *EtcdClient) removeChannelMap(key string, pos int) []map[chan *Event]v3Client.Watcher {
	var channels interface{}
	var exists bool

	if channels, exists = c.watchedChannels.Load(key); exists {
		channels = append(channels.([]map[chan *Event]v3Client.Watcher)[:pos], channels.([]map[chan *Event]v3Client.Watcher)[pos+1:]...)
		c.watchedChannels.Store(key, channels)
	}

	return channels.([]map[chan *Event]v3Client.Watcher)
}

func (c *EtcdClient) getChannelMaps(key string) ([]map[chan *Event]v3Client.Watcher, bool) {
	var channels interface{}
	var exists bool

	channels, exists = c.watchedChannels.Load(key)

	if channels == nil {
		return nil, exists
	}

	return channels.([]map[chan *Event]v3Client.Watcher), exists
}

// CloseWatch closes a specific watch. Both the key and the channel are required when closing a watch as there
// may be multiple listeners on the same key.  The previously created channel serves as a key
func (c *EtcdClient) CloseWatch(ctx context.Context, key string, ch chan *Event) {
	// Get the array of channels mapping
	var watchedChannels []map[chan *Event]v3Client.Watcher
	var ok bool

	if watchedChannels, ok = c.getChannelMaps(key); !ok {
		logger.Warnw(ctx, "key-has-no-watched-channels", log.Fields{"key": key})
		return
	}
	// Look for the channels
	var pos = -1
	for i, chMap := range watchedChannels {
		if t, ok := chMap[ch]; ok {
			logger.Debug(ctx, "channel-found")
			// Close the etcd watcher before the client channel.  This should close the etcd channel as well
			if err := t.Close(); err != nil {
				logger.Errorw(ctx, "watcher-cannot-be-closed", log.Fields{"key": key, "error": err})
			}
			pos = i
			break
		}
	}

	channelMaps, _ := c.getChannelMaps(key)
	// Remove that entry if present
	if pos >= 0 {
		channelMaps = c.removeChannelMap(key, pos)
	}

	// If we don't have any keys being watched then return the Etcd client to the pool
	if len(channelMaps) == 0 {
		c.watchedClientsLock.Lock()
		// Sanity
		if client, ok := c.watchedClients[key]; ok {
			c.pool.Put(client)
			delete(c.watchedClients, key)
		}
		c.watchedClientsLock.Unlock()
	}
	logger.Infow(ctx, "watcher-channel-exiting", log.Fields{"key": key, "channel": channelMaps})
}

func (c *EtcdClient) listenForKeyChange(ctx context.Context, channel v3Client.WatchChan, ch chan<- *Event, cancel context.CancelFunc) {
	logger.Debug(ctx, "start-listening-on-channel ...")
	defer cancel()
	defer close(ch)
	for resp := range channel {
		for _, ev := range resp.Events {
			ch <- NewEvent(getEventType(ev), ev.Kv.Key, ev.Kv.Value, ev.Kv.Version)
		}
	}
	logger.Debug(ctx, "stop-listening-on-channel ...")
}

func getEventType(event *v3Client.Event) int {
	switch event.Type {
	case v3Client.EventTypePut:
		return PUT
	case v3Client.EventTypeDelete:
		return DELETE
	}
	return UNKNOWN
}

// Close closes all the connection in the pool store client
func (c *EtcdClient) Close(ctx context.Context) {
	logger.Debug(ctx, "closing-etcd-pool")
	c.pool.Close(ctx)
}

// The APIs below are not used
var errUnimplemented = errors.New("deprecated")

// Reserve is deprecated
func (c *EtcdClient) Reserve(ctx context.Context, key string, value interface{}, ttl time.Duration) (interface{}, error) {
	return nil, errUnimplemented
}

// ReleaseAllReservations is deprecated
func (c *EtcdClient) ReleaseAllReservations(ctx context.Context) error {
	return errUnimplemented
}

// ReleaseReservation is deprecated
func (c *EtcdClient) ReleaseReservation(ctx context.Context, key string) error {
	return errUnimplemented
}

// RenewReservation is deprecated
func (c *EtcdClient) RenewReservation(ctx context.Context, key string) error {
	return errUnimplemented
}

// AcquireLock is deprecated
func (c *EtcdClient) AcquireLock(ctx context.Context, lockName string, timeout time.Duration) error {
	return errUnimplemented
}

// ReleaseLock is deprecated
func (c *EtcdClient) ReleaseLock(lockName string) error {
	return errUnimplemented
}
