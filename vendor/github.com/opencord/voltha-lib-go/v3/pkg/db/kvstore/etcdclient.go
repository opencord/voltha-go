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
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	v3Client "go.etcd.io/etcd/clientv3"
	v3Concurrency "go.etcd.io/etcd/clientv3/concurrency"
	v3rpcTypes "go.etcd.io/etcd/etcdserver/api/v3rpc/rpctypes"
)

// EtcdClient represents the Etcd KV store client
type EtcdClient struct {
	ectdAPI             *v3Client.Client
	keyReservations     map[string]*v3Client.LeaseID
	watchedChannels     sync.Map
	keyReservationsLock sync.RWMutex
	lockToMutexMap      map[string]*v3Concurrency.Mutex
	lockToSessionMap    map[string]*v3Concurrency.Session
	lockToMutexLock     sync.Mutex
}

// NewEtcdClient returns a new client for the Etcd KV store
func NewEtcdClient(ctx context.Context, addr string, timeout time.Duration, level log.LogLevel) (*EtcdClient, error) {
	logconfig := log.ConstructZapConfig(log.JSON, level, log.Fields{})

	c, err := v3Client.New(v3Client.Config{
		Endpoints:   []string{addr},
		DialTimeout: timeout,
		LogConfig:   &logconfig,
	})
	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}

	reservations := make(map[string]*v3Client.LeaseID)
	lockMutexMap := make(map[string]*v3Concurrency.Mutex)
	lockSessionMap := make(map[string]*v3Concurrency.Session)

	return &EtcdClient{ectdAPI: c, keyReservations: reservations, lockToMutexMap: lockMutexMap,
		lockToSessionMap: lockSessionMap}, nil
}

// IsConnectionUp returns whether the connection to the Etcd KV store is up.  If a timeout occurs then
// it is assumed the connection is down or unreachable.
func (c *EtcdClient) IsConnectionUp(ctx context.Context) bool {
	// Let's try to get a non existent key.  If the connection is up then there will be no error returned.
	if _, err := c.Get(ctx, "non-existent-key"); err != nil {
		return false
	}
	//cancel()
	return true
}

// List returns an array of key-value pairs with key as a prefix.  Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) List(ctx context.Context, key string) (map[string]*KVPair, error) {
	resp, err := c.ectdAPI.Get(ctx, key, v3Client.WithPrefix())
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

	resp, err := c.ectdAPI.Get(ctx, key)

	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}
	for _, ev := range resp.Kvs {
		// Only one value is returned
		return NewKVPair(string(ev.Key), ev.Value, "", ev.Lease, ev.Version), nil
	}
	return nil, nil
}

// Put writes a key-value pair to the KV store.  Value can only be a string or []byte since the etcd API
// accepts only a string as a value for a put operation. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Put(ctx context.Context, key string, value interface{}) error {

	// Validate that we can convert value to a string as etcd API expects a string
	var val string
	var er error
	if val, er = ToString(value); er != nil {
		return fmt.Errorf("unexpected-type-%T", value)
	}

	var err error
	// Check if there is already a lease for this key - if there is then use it, otherwise a PUT will make
	// that KV key permanent instead of automatically removing it after a lease expiration
	c.keyReservationsLock.RLock()
	leaseID, ok := c.keyReservations[key]
	c.keyReservationsLock.RUnlock()
	if ok {
		_, err = c.ectdAPI.Put(ctx, key, val, v3Client.WithLease(*leaseID))
	} else {
		_, err = c.ectdAPI.Put(ctx, key, val)
	}

	if err != nil {
		switch err {
		case context.Canceled:
			logger.Warnw(ctx, "context-cancelled", log.Fields{"error": err})
		case context.DeadlineExceeded:
			logger.Warnw(ctx, "context-deadline-exceeded", log.Fields{"error": err})
		case v3rpcTypes.ErrEmptyKey:
			logger.Warnw(ctx, "etcd-client-error", log.Fields{"error": err})
		default:
			logger.Warnw(ctx, "bad-endpoints", log.Fields{"error": err})
		}
		return err
	}
	return nil
}

// Delete removes a key from the KV store. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Delete(ctx context.Context, key string) error {

	// delete the key
	if _, err := c.ectdAPI.Delete(ctx, key); err != nil {
		logger.Errorw(ctx, "failed-to-delete-key", log.Fields{"key": key, "error": err})
		return err
	}
	logger.Debugw(ctx, "key(s)-deleted", log.Fields{"key": key})
	return nil
}

// Reserve is invoked to acquire a key and set it to a given value. Value can only be a string or []byte since
// the etcd API accepts only a string.  Timeout defines how long the function will wait for a response.  TTL
// defines how long that reservation is valid.  When TTL expires the key is unreserved by the KV store itself.
// If the key is acquired then the value returned will be the value passed in.  If the key is already acquired
// then the value assigned to that key will be returned.
func (c *EtcdClient) Reserve(ctx context.Context, key string, value interface{}, ttl time.Duration) (interface{}, error) {
	// Validate that we can convert value to a string as etcd API expects a string
	var val string
	var er error
	if val, er = ToString(value); er != nil {
		return nil, fmt.Errorf("unexpected-type%T", value)
	}

	resp, err := c.ectdAPI.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}
	// Register the lease id
	c.keyReservationsLock.Lock()
	c.keyReservations[key] = &resp.ID
	c.keyReservationsLock.Unlock()

	// Revoke lease if reservation is not successful
	reservationSuccessful := false
	defer func() {
		if !reservationSuccessful {
			if err = c.ReleaseReservation(context.Background(), key); err != nil {
				logger.Error(ctx, "cannot-release-lease")
			}
		}
	}()

	// Try to grap the Key with the above lease
	c.ectdAPI.Txn(context.Background())
	txn := c.ectdAPI.Txn(context.Background())
	txn = txn.If(v3Client.Compare(v3Client.Version(key), "=", 0))
	txn = txn.Then(v3Client.OpPut(key, val, v3Client.WithLease(resp.ID)))
	txn = txn.Else(v3Client.OpGet(key))
	result, er := txn.Commit()
	if er != nil {
		return nil, er
	}

	if !result.Succeeded {
		// Verify whether we are already the owner of that Key
		if len(result.Responses) > 0 &&
			len(result.Responses[0].GetResponseRange().Kvs) > 0 {
			kv := result.Responses[0].GetResponseRange().Kvs[0]
			if string(kv.Value) == val {
				reservationSuccessful = true
				return value, nil
			}
			return kv.Value, nil
		}
	} else {
		// Read the Key to ensure this is our Key
		m, err := c.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if m != nil {
			if m.Key == key && isEqual(m.Value, value) {
				// My reservation is successful - register it.  For now, support is only for 1 reservation per key
				// per session.
				reservationSuccessful = true
				return value, nil
			}
			// My reservation has failed.  Return the owner of that key
			return m.Value, nil
		}
	}
	return nil, nil
}

// ReleaseAllReservations releases all key reservations previously made (using Reserve API)
func (c *EtcdClient) ReleaseAllReservations(ctx context.Context) error {
	c.keyReservationsLock.Lock()
	defer c.keyReservationsLock.Unlock()

	for key, leaseID := range c.keyReservations {
		_, err := c.ectdAPI.Revoke(ctx, *leaseID)
		if err != nil {
			logger.Errorw(ctx, "cannot-release-reservation", log.Fields{"key": key, "error": err})
			return err
		}
		delete(c.keyReservations, key)
	}
	return nil
}

// ReleaseReservation releases reservation for a specific key.
func (c *EtcdClient) ReleaseReservation(ctx context.Context, key string) error {
	// Get the leaseid using the key
	logger.Debugw(ctx, "Release-reservation", log.Fields{"key": key})
	var ok bool
	var leaseID *v3Client.LeaseID
	c.keyReservationsLock.Lock()
	defer c.keyReservationsLock.Unlock()
	if leaseID, ok = c.keyReservations[key]; !ok {
		return nil
	}

	if leaseID != nil {
		_, err := c.ectdAPI.Revoke(ctx, *leaseID)
		if err != nil {
			logger.Error(ctx, err)
			return err
		}
		delete(c.keyReservations, key)
	}
	return nil
}

// RenewReservation renews a reservation.  A reservation will go stale after the specified TTL (Time To Live)
// period specified when reserving the key
func (c *EtcdClient) RenewReservation(ctx context.Context, key string) error {
	// Get the leaseid using the key
	var ok bool
	var leaseID *v3Client.LeaseID
	c.keyReservationsLock.RLock()
	leaseID, ok = c.keyReservations[key]
	c.keyReservationsLock.RUnlock()

	if !ok {
		return errors.New("key-not-reserved")
	}

	if leaseID != nil {
		_, err := c.ectdAPI.KeepAliveOnce(ctx, *leaseID)
		if err != nil {
			logger.Errorw(ctx, "lease-may-have-expired", log.Fields{"error": err})
			return err
		}
	} else {
		return errors.New("lease-expired")
	}
	return nil
}

// Watch provides the watch capability on a given key.  It returns a channel onto which the callee needs to
// listen to receive Events.
func (c *EtcdClient) Watch(ctx context.Context, key string, withPrefix bool) chan *Event {
	w := v3Client.NewWatcher(c.ectdAPI)
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

// Close closes the KV store client
func (c *EtcdClient) Close(ctx context.Context) {
	if err := c.ectdAPI.Close(); err != nil {
		logger.Errorw(ctx, "error-closing-client", log.Fields{"error": err})
	}
}

func (c *EtcdClient) addLockName(lockName string, lock *v3Concurrency.Mutex, session *v3Concurrency.Session) {
	c.lockToMutexLock.Lock()
	defer c.lockToMutexLock.Unlock()
	c.lockToMutexMap[lockName] = lock
	c.lockToSessionMap[lockName] = session
}

func (c *EtcdClient) deleteLockName(lockName string) {
	c.lockToMutexLock.Lock()
	defer c.lockToMutexLock.Unlock()
	delete(c.lockToMutexMap, lockName)
	delete(c.lockToSessionMap, lockName)
}

func (c *EtcdClient) getLock(lockName string) (*v3Concurrency.Mutex, *v3Concurrency.Session) {
	c.lockToMutexLock.Lock()
	defer c.lockToMutexLock.Unlock()
	var lock *v3Concurrency.Mutex
	var session *v3Concurrency.Session
	if l, exist := c.lockToMutexMap[lockName]; exist {
		lock = l
	}
	if s, exist := c.lockToSessionMap[lockName]; exist {
		session = s
	}
	return lock, session
}

func (c *EtcdClient) AcquireLock(ctx context.Context, lockName string, timeout time.Duration) error {
	session, _ := v3Concurrency.NewSession(c.ectdAPI, v3Concurrency.WithContext(ctx))
	mu := v3Concurrency.NewMutex(session, "/devicelock_"+lockName)
	if err := mu.Lock(context.Background()); err != nil {
		//cancel()
		return err
	}
	c.addLockName(lockName, mu, session)
	return nil
}

func (c *EtcdClient) ReleaseLock(lockName string) error {
	lock, session := c.getLock(lockName)
	var err error
	if lock != nil {
		if e := lock.Unlock(context.Background()); e != nil {
			err = e
		}
	}
	if session != nil {
		if e := session.Close(); e != nil {
			err = e
		}
	}
	c.deleteLockName(lockName)

	return err
}
