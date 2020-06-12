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
	"bytes"
	"context"
	"errors"
	log "github.com/opencord/voltha-lib-go/v3/pkg/log"
	"sync"
	"time"
	//log "ciena.com/coordinator/common"
	consulapi "github.com/hashicorp/consul/api"
)

type channelContextMap struct {
	ctx     context.Context
	channel chan *Event
	cancel  context.CancelFunc
}

// ConsulClient represents the consul KV store client
type ConsulClient struct {
	session                *consulapi.Session
	sessionID              string
	consul                 *consulapi.Client
	doneCh                 *chan int
	keyReservations        map[string]interface{}
	watchedChannelsContext map[string][]*channelContextMap
	writeLock              sync.Mutex
}

// NewConsulClient returns a new client for the Consul KV store
func NewConsulClient(ctx context.Context, addr string, timeout time.Duration) (*ConsulClient, error) {
	config := consulapi.DefaultConfig()
	config.Address = addr
	config.WaitTime = timeout
	consul, err := consulapi.NewClient(config)
	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}

	doneCh := make(chan int, 1)
	wChannelsContext := make(map[string][]*channelContextMap)
	reservations := make(map[string]interface{})
	return &ConsulClient{consul: consul, doneCh: &doneCh, watchedChannelsContext: wChannelsContext, keyReservations: reservations}, nil
}

// IsConnectionUp returns whether the connection to the Consul KV store is up
func (c *ConsulClient) IsConnectionUp(ctx context.Context) bool {
	logger.Error(ctx, "Unimplemented function")
	return false
}

// List returns an array of key-value pairs with key as a prefix.  Timeout defines how long the function will
// wait for a response
func (c *ConsulClient) List(ctx context.Context, key string) (map[string]*KVPair, error) {

	deadline, _ := ctx.Deadline()
	kv := c.consul.KV()
	var queryOptions consulapi.QueryOptions
	// Substract current time from deadline to get the waitTime duration
	queryOptions.WaitTime = time.Until(deadline)

	// For now we ignore meta data
	kvps, _, err := kv.List(key, &queryOptions)
	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}
	m := make(map[string]*KVPair)
	for _, kvp := range kvps {
		m[string(kvp.Key)] = NewKVPair(string(kvp.Key), kvp.Value, string(kvp.Session), 0, -1)
	}
	return m, nil
}

// Get returns a key-value pair for a given key. Timeout defines how long the function will
// wait for a response
func (c *ConsulClient) Get(ctx context.Context, key string) (*KVPair, error) {

	deadline, _ := ctx.Deadline()
	kv := c.consul.KV()
	var queryOptions consulapi.QueryOptions
	// Substract current time from deadline to get the waitTime duration
	queryOptions.WaitTime = time.Until(deadline)

	// For now we ignore meta data
	kvp, _, err := kv.Get(key, &queryOptions)
	if err != nil {
		logger.Error(ctx, err)
		return nil, err
	}
	if kvp != nil {
		return NewKVPair(string(kvp.Key), kvp.Value, string(kvp.Session), 0, -1), nil
	}

	return nil, nil
}

// Put writes a key-value pair to the KV store.  Value can only be a string or []byte since the consul API
// accepts only a []byte as a value for a put operation. Timeout defines how long the function will
// wait for a response
func (c *ConsulClient) Put(ctx context.Context, key string, value interface{}) error {

	// Validate that we can create a byte array from the value as consul API expects a byte array
	var val []byte
	var er error
	if val, er = ToByte(value); er != nil {
		logger.Error(ctx, er)
		return er
	}

	// Create a key value pair
	kvp := consulapi.KVPair{Key: key, Value: val}
	kv := c.consul.KV()
	var writeOptions consulapi.WriteOptions
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	_, err := kv.Put(&kvp, &writeOptions)
	if err != nil {
		logger.Error(ctx, err)
		return err
	}
	return nil
}

// Delete removes a key from the KV store. Timeout defines how long the function will
// wait for a response
func (c *ConsulClient) Delete(ctx context.Context, key string) error {
	kv := c.consul.KV()
	var writeOptions consulapi.WriteOptions
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	_, err := kv.Delete(key, &writeOptions)
	if err != nil {
		logger.Error(ctx, err)
		return err
	}
	return nil
}

func (c *ConsulClient) deleteSession(ctx context.Context) {
	if c.sessionID != "" {
		logger.Debug(ctx, "cleaning-up-session")
		session := c.consul.Session()
		_, err := session.Destroy(c.sessionID, nil)
		if err != nil {
			logger.Errorw(ctx, "error-cleaning-session", log.Fields{"session": c.sessionID, "error": err})
		}
	}
	c.sessionID = ""
	c.session = nil
}

func (c *ConsulClient) createSession(ctx context.Context, ttl time.Duration, retries int) (*consulapi.Session, string, error) {
	session := c.consul.Session()
	entry := &consulapi.SessionEntry{
		Behavior: consulapi.SessionBehaviorDelete,
		TTL:      ttl.String(),
	}

	for {
		id, meta, err := session.Create(entry, nil)
		if err != nil {
			logger.Errorw(ctx, "create-session-error", log.Fields{"error": err})
			if retries == 0 {
				return nil, "", err
			}
		} else if meta.RequestTime == 0 {
			logger.Errorw(ctx, "create-session-bad-meta-data", log.Fields{"meta-data": meta})
			if retries == 0 {
				return nil, "", errors.New("bad-meta-data")
			}
		} else if id == "" {
			logger.Error(ctx, "create-session-nil-id")
			if retries == 0 {
				return nil, "", errors.New("ID-nil")
			}
		} else {
			return session, id, nil
		}
		// If retry param is -1 we will retry indefinitely
		if retries > 0 {
			retries--
		}
		logger.Debug(ctx, "retrying-session-create-after-a-second-delay")
		time.Sleep(time.Duration(1) * time.Second)
	}
}

// Helper function to verify mostly whether the content of two interface types are the same.  Focus is []byte and
// string types
func isEqual(val1 interface{}, val2 interface{}) bool {
	b1, err := ToByte(val1)
	b2, er := ToByte(val2)
	if err == nil && er == nil {
		return bytes.Equal(b1, b2)
	}
	return val1 == val2
}

// Reserve is invoked to acquire a key and set it to a given value. Value can only be a string or []byte since
// the consul API accepts only a []byte.  Timeout defines how long the function will wait for a response.  TTL
// defines how long that reservation is valid.  When TTL expires the key is unreserved by the KV store itself.
// If the key is acquired then the value returned will be the value passed in.  If the key is already acquired
// then the value assigned to that key will be returned.
func (c *ConsulClient) Reserve(ctx context.Context, key string, value interface{}, ttl time.Duration) (interface{}, error) {

	// Validate that we can create a byte array from the value as consul API expects a byte array
	var val []byte
	var er error
	if val, er = ToByte(value); er != nil {
		logger.Error(ctx, er)
		return nil, er
	}

	// Cleanup any existing session and recreate new ones.  A key is reserved against a session
	if c.sessionID != "" {
		c.deleteSession(ctx)
	}

	// Clear session if reservation is not successful
	reservationSuccessful := false
	defer func() {
		if !reservationSuccessful {
			logger.Debug(ctx, "deleting-session")
			c.deleteSession(ctx)
		}
	}()

	session, sessionID, err := c.createSession(ctx, ttl, -1)
	if err != nil {
		logger.Errorw(ctx, "no-session-created", log.Fields{"error": err})
		return "", errors.New("no-session-created")
	}
	logger.Debugw(ctx, "session-created", log.Fields{"session-id": sessionID})
	c.sessionID = sessionID
	c.session = session

	// Try to grap the Key using the session
	kv := c.consul.KV()
	kvp := consulapi.KVPair{Key: key, Value: val, Session: c.sessionID}
	result, _, err := kv.Acquire(&kvp, nil)
	if err != nil {
		logger.Errorw(ctx, "error-acquiring-keys", log.Fields{"error": err})
		return nil, err
	}

	logger.Debugw(ctx, "key-acquired", log.Fields{"key": key, "status": result})

	// Irrespective whether we were successful in acquiring the key, let's read it back and see if it's us.
	m, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if m != nil {
		logger.Debugw(ctx, "response-received", log.Fields{"key": m.Key, "m.value": string(m.Value.([]byte)), "value": value})
		if m.Key == key && isEqual(m.Value, value) {
			// My reservation is successful - register it.  For now, support is only for 1 reservation per key
			// per session.
			reservationSuccessful = true
			c.writeLock.Lock()
			c.keyReservations[key] = m.Value
			c.writeLock.Unlock()
			return m.Value, nil
		}
		// My reservation has failed.  Return the owner of that key
		return m.Value, nil
	}
	return nil, nil
}

// ReleaseAllReservations releases all key reservations previously made (using Reserve API)
func (c *ConsulClient) ReleaseAllReservations(ctx context.Context) error {
	kv := c.consul.KV()
	var kvp consulapi.KVPair
	var result bool
	var err error

	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	for key, value := range c.keyReservations {
		kvp = consulapi.KVPair{Key: key, Value: value.([]byte), Session: c.sessionID}
		result, _, err = kv.Release(&kvp, nil)
		if err != nil {
			logger.Errorw(ctx, "cannot-release-reservation", log.Fields{"key": key, "error": err})
			return err
		}
		if !result {
			logger.Errorw(ctx, "cannot-release-reservation", log.Fields{"key": key})
		}
		delete(c.keyReservations, key)
	}
	return nil
}

// ReleaseReservation releases reservation for a specific key.
func (c *ConsulClient) ReleaseReservation(ctx context.Context, key string) error {
	var ok bool
	var reservedValue interface{}
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if reservedValue, ok = c.keyReservations[key]; !ok {
		return errors.New("key-not-reserved:" + key)
	}
	// Release the reservation
	kv := c.consul.KV()
	kvp := consulapi.KVPair{Key: key, Value: reservedValue.([]byte), Session: c.sessionID}

	result, _, er := kv.Release(&kvp, nil)
	if er != nil {
		return er
	}
	// Remove that key entry on success
	if result {
		delete(c.keyReservations, key)
		return nil
	}
	return errors.New("key-cannot-be-unreserved")
}

// RenewReservation renews a reservation.  A reservation will go stale after the specified TTL (Time To Live)
// period specified when reserving the key
func (c *ConsulClient) RenewReservation(ctx context.Context, key string) error {
	// In the case of Consul, renew reservation of a reserve key only require renewing the client session.

	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	// Verify the key was reserved
	if _, ok := c.keyReservations[key]; !ok {
		return errors.New("key-not-reserved")
	}

	if c.session == nil {
		return errors.New("no-session-exist")
	}

	var writeOptions consulapi.WriteOptions
	if _, _, err := c.session.Renew(c.sessionID, &writeOptions); err != nil {
		return err
	}
	return nil
}

// Watch provides the watch capability on a given key.  It returns a channel onto which the callee needs to
// listen to receive Events.
func (c *ConsulClient) Watch(ctx context.Context, key string, withPrefix bool) chan *Event {

	// Create a new channel
	ch := make(chan *Event, maxClientChannelBufferSize)

	// Create a context to track this request
	watchContext, cFunc := context.WithCancel(context.Background())

	// Save the channel and context reference for later
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	ccm := channelContextMap{channel: ch, ctx: watchContext, cancel: cFunc}
	c.watchedChannelsContext[key] = append(c.watchedChannelsContext[key], &ccm)

	// Launch a go routine to listen for updates
	go c.listenForKeyChange(watchContext, key, ch)

	return ch
}

// CloseWatch closes a specific watch. Both the key and the channel are required when closing a watch as there
// may be multiple listeners on the same key.  The previously created channel serves as a key
func (c *ConsulClient) CloseWatch(ctx context.Context, key string, ch chan *Event) {
	// First close the context
	var ok bool
	var watchedChannelsContexts []*channelContextMap
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if watchedChannelsContexts, ok = c.watchedChannelsContext[key]; !ok {
		logger.Errorw(ctx, "key-has-no-watched-context-or-channel", log.Fields{"key": key})
		return
	}
	// Look for the channels
	var pos = -1
	for i, chCtxMap := range watchedChannelsContexts {
		if chCtxMap.channel == ch {
			logger.Debug(ctx, "channel-found")
			chCtxMap.cancel()
			//close the channel
			close(ch)
			pos = i
			break
		}
	}
	// Remove that entry if present
	if pos >= 0 {
		c.watchedChannelsContext[key] = append(c.watchedChannelsContext[key][:pos], c.watchedChannelsContext[key][pos+1:]...)
	}
	logger.Debugw(ctx, "watched-channel-exiting", log.Fields{"key": key, "channel": c.watchedChannelsContext[key]})
}

func (c *ConsulClient) isKVEqual(kv1 *consulapi.KVPair, kv2 *consulapi.KVPair) bool {
	if (kv1 == nil) && (kv2 == nil) {
		return true
	} else if (kv1 == nil) || (kv2 == nil) {
		return false
	}
	// Both the KV should be non-null here
	if kv1.Key != kv2.Key ||
		!bytes.Equal(kv1.Value, kv2.Value) ||
		kv1.Session != kv2.Session ||
		kv1.LockIndex != kv2.LockIndex ||
		kv1.ModifyIndex != kv2.ModifyIndex {
		return false
	}
	return true
}

func (c *ConsulClient) listenForKeyChange(ctx context.Context, key string, ch chan *Event) {
	logger.Debugw(ctx, "start-watching-channel", log.Fields{"key": key, "channel": ch})

	defer c.CloseWatch(ctx, key, ch)
	kv := c.consul.KV()
	var queryOptions consulapi.QueryOptions
	queryOptions.WaitTime = defaultKVGetTimeout

	// Get the existing value, if any
	previousKVPair, meta, err := kv.Get(key, &queryOptions)
	if err != nil {
		logger.Debug(ctx, err)
	}
	lastIndex := meta.LastIndex

	// Wait for change.  Push any change onto the channel and keep waiting for new update
	//var waitOptions consulapi.QueryOptions
	var pair *consulapi.KVPair
	//watchContext, _ := context.WithCancel(context.Background())
	waitOptions := queryOptions.WithContext(ctx)
	for {
		//waitOptions = consulapi.QueryOptions{WaitIndex: lastIndex}
		waitOptions.WaitIndex = lastIndex
		pair, meta, err = kv.Get(key, waitOptions)
		select {
		case <-ctx.Done():
			logger.Debug(ctx, "done-event-received-exiting")
			return
		default:
			if err != nil {
				logger.Warnw(ctx, "error-from-watch", log.Fields{"error": err})
				ch <- NewEvent(CONNECTIONDOWN, key, []byte(""), -1)
			} else {
				logger.Debugw(ctx, "index-state", log.Fields{"lastindex": lastIndex, "newindex": meta.LastIndex, "key": key})
			}
		}
		if err != nil {
			logger.Debug(ctx, err)
			// On error, block for 10 milliseconds to prevent endless loop
			time.Sleep(10 * time.Millisecond)
		} else if meta.LastIndex <= lastIndex {
			logger.Info(ctx, "no-index-change-or-negative")
		} else {
			logger.Debugw(ctx, "update-received", log.Fields{"pair": pair})
			if pair == nil {
				ch <- NewEvent(DELETE, key, []byte(""), -1)
			} else if !c.isKVEqual(pair, previousKVPair) {
				// Push the change onto the channel if the data has changed
				// For now just assume it's a PUT change
				logger.Debugw(ctx, "pair-details", log.Fields{"session": pair.Session, "key": pair.Key, "value": pair.Value})
				ch <- NewEvent(PUT, pair.Key, pair.Value, -1)
			}
			previousKVPair = pair
			lastIndex = meta.LastIndex
		}
	}
}

// Close closes the KV store client
func (c *ConsulClient) Close(ctx context.Context) {
	var writeOptions consulapi.WriteOptions
	// Inform any goroutine it's time to say goodbye.
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if c.doneCh != nil {
		close(*c.doneCh)
	}

	// Clear the sessionID
	if _, err := c.consul.Session().Destroy(c.sessionID, &writeOptions); err != nil {
		logger.Errorw(ctx, "error-closing-client", log.Fields{"error": err})
	}
}

func (c *ConsulClient) AcquireLock(ctx context.Context, lockName string, timeout time.Duration) error {
	return nil
}

func (c *ConsulClient) ReleaseLock(lockName string) error {
	return nil
}
