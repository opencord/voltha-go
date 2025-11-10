/*
* Copyright 2018-2024 Open Networking Foundation (ONF) and the ONF Contributors

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
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
)

type RedisClient struct {
	redisAPI            *redis.Client
	keyReservations     map[string]time.Duration
	watchedChannels     sync.Map
	writeLock           sync.Mutex
	keyReservationsLock sync.RWMutex
}

func NewRedisClient(addr string, timeout time.Duration, useSentinel bool) (*RedisClient, error) {
	var r *redis.Client
	if !useSentinel {
		r = redis.NewClient(&redis.Options{Addr: addr})
	} else {
		// Redis Master-Replicas with Sentinel, sentinel masterSet config
		//  should be set to sebaRedis
		r = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    "sebaRedis",
			SentinelAddrs: []string{addr},
		})
	}

	reservations := make(map[string]time.Duration)
	return &RedisClient{redisAPI: r, keyReservations: reservations}, nil
}

func (c *RedisClient) Get(ctx context.Context, key string) (*KVPair, error) {

	val, err := c.redisAPI.Get(ctx, key).Result()
	valBytes, _ := ToByte(val)
	if err != nil {
		return nil, nil
	}
	return NewKVPair(key, valBytes, "", 0, 0), nil
}

func (c *RedisClient) Put(ctx context.Context, key string, value interface{}) error {

	// Validate that we can convert value to a string as etcd API expects a string
	var val string
	var er error
	if val, er = ToString(value); er != nil {
		return fmt.Errorf("unexpected-type-%T", value)
	}

	// Check if there is already a lease for this key - if there is then use it, otherwise a PUT will make
	// that KV key permanent instead of automatically removing it after a lease expiration
	setErr := c.redisAPI.Set(ctx, key, val, 0)
	err := setErr.Err()

	if err != nil {
		switch setErr.Err() {
		case context.Canceled:
			logger.Warnw(ctx, "context-cancelled", log.Fields{"error": err})
		case context.DeadlineExceeded:
			logger.Warnw(ctx, "context-deadline-exceeded", log.Fields{"error": err})
		default:
			logger.Warnw(ctx, "bad-endpoints", log.Fields{"error": err})
		}
		return err
	}
	return nil
}

func (c *RedisClient) scanAllKeysWithPrefix(ctx context.Context, key string) ([]string, error) {
	var err error
	allkeys := []string{}
	cont := true
	cursor := uint64(0)
	matchPrefix := key + "*"

	for cont {
		// search in the first 10000 entries starting from the point indicated by the cursor
		logger.Debugw(ctx, "redis-scan", log.Fields{"matchPrefix": matchPrefix, "cursor": cursor})
		var keys []string
		keys, cursor, err = c.redisAPI.Scan(context.Background(), cursor, matchPrefix, 10000).Result()
		if err != nil {
			return nil, err
		}
		if cursor == 0 {
			// all data searched. break the loop
			logger.Debugw(ctx, "redis-scan-ended", log.Fields{"matchPrefix": matchPrefix, "cursor": cursor})
			cont = false
		}
		if len(keys) == 0 {
			// no matched data found in this cycle. Continue to search
			logger.Debugw(ctx, "redis-scan-no-data-found-continue", log.Fields{"matchPrefix": matchPrefix, "cursor": cursor})
			continue
		}
		allkeys = append(allkeys, keys...)
	}
	return allkeys, nil
}

func (c *RedisClient) KeyExists(ctx context.Context, key string) (bool, error) {
	var err error
	var keyCount int64

	if keyCount, err = c.redisAPI.Exists(ctx, key).Result(); err != nil {
		return false, err
	}
	if keyCount > 0 {
		return true, nil
	}
	return false, nil
}

func (c *RedisClient) List(ctx context.Context, key string) (map[string]*KVPair, error) {
	var err error
	var keys []string
	m := make(map[string]*KVPair)
	var values []interface{}

	if keys, err = c.scanAllKeysWithPrefix(ctx, key); err != nil {
		return nil, err
	}

	if len(keys) != 0 {
		values, err = c.redisAPI.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, err
		}
	}
	for i, key := range keys {
		if valBytes, err := ToByte(values[i]); err == nil {
			m[key] = NewKVPair(key, interface{}(valBytes), "", 0, 0)
		}
	}
	return m, nil
}

func (c *RedisClient) Delete(ctx context.Context, key string) error {
	// delete the key
	if _, err := c.redisAPI.Del(ctx, key).Result(); err != nil {
		logger.Errorw(ctx, "failed-to-delete-key", log.Fields{"key": key, "error": err})
		return err
	}
	logger.Debugw(ctx, "key(s)-deleted", log.Fields{"key": key})
	return nil
}

func (c *RedisClient) DeleteWithPrefix(ctx context.Context, prefixKey string) error {
	var keys []string
	var err error
	if keys, err = c.scanAllKeysWithPrefix(ctx, prefixKey); err != nil {
		return err
	}
	if len(keys) == 0 {
		logger.Warn(ctx, "nothing-to-delete-from-kv", log.Fields{"key": prefixKey})
		return nil
	}
	//call delete for keys
	entryCount := int64(0)
	start := 0
	pageSize := 5000
	length := len(keys)
	for start < length {
		end := start + pageSize
		if end >= length {
			end = length
		}
		keysToDelete := keys[start:end]
		count := int64(0)
		if count, err = c.redisAPI.Del(ctx, keysToDelete...).Result(); err != nil {
			logger.Errorw(ctx, "DeleteWithPrefix method failed", log.Fields{"prefixKey": prefixKey, "numOfMatchedKeys": len(keysToDelete), "err": err})
			return err
		}
		entryCount += count
		start = end
	}
	logger.Debugf(ctx, "%d entries matching with the key prefix %s have been deleted successfully", entryCount, prefixKey)
	return nil
}

func (c *RedisClient) Reserve(ctx context.Context, key string, value interface{}, ttl time.Duration) (interface{}, error) {
	var val string
	var er error
	if val, er = ToString(value); er != nil {
		return nil, fmt.Errorf("unexpected-type%T", value)
	}

	// SetNX -- Only set the key if it does not already exist.
	c.redisAPI.SetNX(ctx, key, value, ttl)

	// Check if set is successful
	redisVal := c.redisAPI.Get(ctx, key).Val()
	if redisVal == "" {
		println("NULL")
		return nil, nil
	}

	if val == redisVal {
		// set is successful, return new reservation value
		c.keyReservationsLock.Lock()
		c.keyReservations[key] = ttl
		c.keyReservationsLock.Unlock()
		bytes, _ := ToByte(val)
		return bytes, nil
	} else {
		// set is not successful, return existing reservation value
		bytes, _ := ToByte(redisVal)
		return bytes, nil
	}

}

func (c *RedisClient) ReleaseReservation(ctx context.Context, key string) error {

	redisVal := c.redisAPI.Get(ctx, key).Val()
	if redisVal == "" {
		return nil
	}

	// Override SetNX value with no TTL
	_, err := c.redisAPI.Set(ctx, key, redisVal, 0).Result()
	if err != nil {
		delete(c.keyReservations, key)
	} else {
		return err
	}
	return nil

}

func (c *RedisClient) ReleaseAllReservations(ctx context.Context) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	for key := range c.keyReservations {
		err := c.ReleaseReservation(ctx, key)
		if err != nil {
			logger.Errorw(ctx, "cannot-release-reservation", log.Fields{"key": key, "error": err})
			return err
		}
	}
	return nil
}

func (c *RedisClient) RenewReservation(ctx context.Context, key string) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	// Verify the key was reserved
	ttl, ok := c.keyReservations[key]
	if !ok {
		return errors.New("key-not-reserved. Key not found")
	}

	redisVal := c.redisAPI.Get(ctx, key).Val()
	if redisVal != "" {
		c.redisAPI.Set(ctx, key, redisVal, ttl)
	}
	return nil
}

func (c *RedisClient) listenForKeyChange(ctx context.Context, redisCh <-chan *redis.Message, ch chan<- *Event, cancel context.CancelFunc) {
	logger.Debug(ctx, "start-listening-on-channel ...")
	defer cancel()
	defer close(ch)
	for msg := range redisCh {
		words := strings.Split(msg.Channel, ":")
		key := words[1]
		msgType := getMessageType(msg.Payload)
		var valBytes []byte
		if msgType == PUT {
			ev, _ := c.Get(ctx, key)
			valBytes, _ = ToByte(ev.Value)
		}
		ch <- NewEvent(getMessageType(msg.Payload), []byte(key), valBytes, 0)
	}
	logger.Debug(ctx, "stop-listening-on-channel ...")
}

func getMessageType(msg string) int {
	isPut := strings.HasSuffix(msg, "set")
	isDel := strings.HasSuffix(msg, "del")
	if isPut {
		return PUT
	} else if isDel {
		return DELETE
	} else {
		return UNKNOWN
	}
}

func (c *RedisClient) addChannelMap(key string, channelMap map[chan *Event]*redis.PubSub) []map[chan *Event]*redis.PubSub {

	var channels interface{}
	var exists bool

	if channels, exists = c.watchedChannels.Load(key); exists {
		channels = append(channels.([]map[chan *Event]*redis.PubSub), channelMap)
	} else {
		channels = []map[chan *Event]*redis.PubSub{channelMap}
	}
	c.watchedChannels.Store(key, channels)

	return channels.([]map[chan *Event]*redis.PubSub)
}

func (c *RedisClient) removeChannelMap(key string, pos int) []map[chan *Event]*redis.PubSub {
	var channels interface{}
	var exists bool

	if channels, exists = c.watchedChannels.Load(key); exists {
		channels = append(channels.([]map[chan *Event]*redis.PubSub)[:pos], channels.([]map[chan *Event]*redis.PubSub)[pos+1:]...)
		c.watchedChannels.Store(key, channels)
	}

	return channels.([]map[chan *Event]*redis.PubSub)
}

func (c *RedisClient) getChannelMaps(key string) ([]map[chan *Event]*redis.PubSub, bool) {
	var channels interface{}
	var exists bool

	channels, exists = c.watchedChannels.Load(key)

	if channels == nil {
		return nil, exists
	}

	return channels.([]map[chan *Event]*redis.PubSub), exists
}

func (c *RedisClient) Watch(ctx context.Context, key string, withPrefix bool) chan *Event {

	ctx, cancel := context.WithCancel(ctx)

	var subscribePath string
	subscribePath = "__key*__:" + key
	if withPrefix {
		subscribePath += "*"
	}
	pubsub := c.redisAPI.PSubscribe(ctx, subscribePath)
	redisCh := pubsub.Channel()

	// Create new channel
	ch := make(chan *Event, maxClientChannelBufferSize)

	// Keep track of the created channels so they can be closed when required
	channelMap := make(map[chan *Event]*redis.PubSub)
	channelMap[ch] = pubsub

	channelMaps := c.addChannelMap(key, channelMap)
	logger.Debugw(ctx, "watched-channels", log.Fields{"len": len(channelMaps)})

	// Launch a go routine to listen for updates
	go c.listenForKeyChange(ctx, redisCh, ch, cancel)
	return ch
}

func (c *RedisClient) CloseWatch(ctx context.Context, key string, ch chan *Event) {
	// Get the array of channels mapping
	var watchedChannels []map[chan *Event]*redis.PubSub
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
			// Close the Redis watcher before the client channel.  This should close the etcd channel as well
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
func (c *RedisClient) AcquireLock(ctx context.Context, lockName string, timeout time.Duration) error {
	return nil
}

func (c *RedisClient) ReleaseLock(lockName string) error {
	return nil
}

func (c *RedisClient) IsConnectionUp(ctx context.Context) bool {
	if _, err := c.redisAPI.Set(ctx, "connection-check", "1", 0).Result(); err != nil {
		return false
	}
	return true

}

func (c *RedisClient) Close(ctx context.Context) {
	if err := c.redisAPI.Close(); err != nil {
		logger.Errorw(ctx, "error-closing-client", log.Fields{"error": err})
	}
}

func (c *RedisClient) GetWithPrefix(ctx context.Context, prefix string) (map[string]*KVPair, error) {
	var err error
	var keys []string
	m := make(map[string]*KVPair)
	var values []interface{}

	if keys, err = c.scanAllKeysWithPrefix(ctx, prefix); err != nil {
		return nil, err
	}

	if len(keys) != 0 {
		values, err = c.redisAPI.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, err
		}
	}
	for i, key := range keys {
		if valBytes, err := ToByte(values[i]); err == nil {
			m[key] = NewKVPair(key, interface{}(valBytes), "", 0, 0)
		}
	}
	return m, nil
}

func (c *RedisClient) GetWithPrefixKeysOnly(ctx context.Context, prefix string) ([]string, error) {
	// Use the scanAllKeysWithPrefix function to fetch keys matching the prefix
	keys, err := c.scanAllKeysWithPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys with prefix %s: %v", prefix, err)
	}

	if len(keys) == 0 {
		logger.Debugw(ctx, "no-keys-found", log.Fields{"prefix": prefix})
	}

	return keys, nil
}
