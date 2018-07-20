package kvstore

import (
	//log "../common"
	"context"
	"errors"
	"fmt"
	v3Client "github.com/coreos/etcd/clientv3"
	v3rpcTypes "github.com/coreos/etcd/etcdserver/api/v3rpc/rpctypes"
	log "github.com/opencord/voltha-go/common/log"
	"sync"
)

// EtcdClient represents the Etcd KV store client
type EtcdClient struct {
	ectdAPI         *v3Client.Client
	leaderRev       v3Client.Client
	keyReservations map[string]*v3Client.LeaseID
	watchedChannels map[string][]map[chan *Event]v3Client.Watcher
	writeLock       sync.Mutex
}

// NewEtcdClient returns a new client for the Etcd KV store
func NewEtcdClient(addr string, timeout int) (*EtcdClient, error) {

	duration := GetDuration(timeout)

	c, err := v3Client.New(v3Client.Config{
		Endpoints:   []string{addr},
		DialTimeout: duration,
	})
	if err != nil {
		log.Error(err)
		return nil, err
	}
	wc := make(map[string][]map[chan *Event]v3Client.Watcher)
	reservations := make(map[string]*v3Client.LeaseID)
	return &EtcdClient{ectdAPI: c, watchedChannels: wc, keyReservations: reservations}, nil
}

// List returns an array of key-value pairs with key as a prefix.  Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) List(key string, timeout int) (map[string]*KVPair, error) {
	duration := GetDuration(timeout)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	resp, err := c.ectdAPI.Get(ctx, key, v3Client.WithPrefix())
	cancel()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	m := make(map[string]*KVPair)
	for _, ev := range resp.Kvs {
		m[string(ev.Key)] = NewKVPair(string(ev.Key), ev.Value, "", ev.Lease)
	}
	return m, nil
}

// Get returns a key-value pair for a given key. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Get(key string, timeout int) (*KVPair, error) {
	duration := GetDuration(timeout)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	resp, err := c.ectdAPI.Get(ctx, key)
	cancel()
	if err != nil {
		log.Error(err)
		return nil, err
	}
	for _, ev := range resp.Kvs {
		// Only one value is returned
		return NewKVPair(string(ev.Key), ev.Value, "", ev.Lease), nil
	}
	return nil, nil
}

// Put writes a key-value pair to the KV store.  Value can only be a string or []byte since the etcd API
// accepts only a string as a value for a put operation. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Put(key string, value interface{}, timeout int) error {

	// Validate that we can convert value to a string as etcd API expects a string
	var val string
	var er error
	if val, er = ToString(value); er != nil {
		return fmt.Errorf("unexpected-type-%T", value)
	}

	duration := GetDuration(timeout)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	_, err := c.ectdAPI.Put(ctx, key, val)
	cancel()
	if err != nil {
		switch err {
		case context.Canceled:
			log.Warnw("context-cancelled", log.Fields{"error": err})
		case context.DeadlineExceeded:
			log.Warnw("context-deadline-exceeded", log.Fields{"error": err})
		case v3rpcTypes.ErrEmptyKey:
			log.Warnw("etcd-client-error", log.Fields{"error": err})
		default:
			log.Warnw("bad-endpoints", log.Fields{"error": err})
		}
		return err
	}
	return nil
}

// Delete removes a key from the KV store. Timeout defines how long the function will
// wait for a response
func (c *EtcdClient) Delete(key string, timeout int) error {

	duration := GetDuration(timeout)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	// count keys about to be deleted
	gresp, err := c.ectdAPI.Get(ctx, key, v3Client.WithPrefix())
	if err != nil {
		log.Error(err)
		return err
	}

	// delete the keys
	dresp, err := c.ectdAPI.Delete(ctx, key, v3Client.WithPrefix())
	if err != nil {
		log.Error(err)
		return err
	}

	if dresp == nil || gresp == nil {
		log.Debug("nothing-to-delete")
		return nil
	}

	log.Debugw("delete-keys", log.Fields{"all-keys-deleted": int64(len(gresp.Kvs)) == dresp.Deleted})
	if int64(len(gresp.Kvs)) == dresp.Deleted {
		log.Debug("All-keys-deleted")
	} else {
		log.Error("not-all-keys-deleted")
		err := errors.New("not-all-keys-deleted")
		return err
	}
	return nil
}

// Reserve is invoked to acquire a key and set it to a given value. Value can only be a string or []byte since
// the etcd API accepts only a string.  Timeout defines how long the function will wait for a response.  TTL
// defines how long that reservation is valid.  When TTL expires the key is unreserved by the KV store itself.
// If the key is acquired then the value returned will be the value passed in.  If the key is already acquired
// then the value assigned to that key will be returned.
func (c *EtcdClient) Reserve(key string, value interface{}, ttl int64) (interface{}, error) {
	// Validate that we can convert value to a string as etcd API expects a string
	var val string
	var er error
	if val, er = ToString(value); er != nil {
		return nil, fmt.Errorf("unexpected-type%T", value)
	}

	// Create a lease
	resp, err := c.ectdAPI.Grant(context.Background(), ttl)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	// Register the lease id
	c.writeLock.Lock()
	c.keyReservations[key] = &resp.ID
	c.writeLock.Unlock()

	// Revoke lease if reservation is not successful
	reservationSuccessful := false
	defer func() {
		if !reservationSuccessful {
			if err = c.ReleaseReservation(key); err != nil {
				log.Errorf("cannot-release-lease")
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
		m, err := c.Get(key, defaultKVGetTimeout)
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
func (c *EtcdClient) ReleaseAllReservations() error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	for key, leaseID := range c.keyReservations {
		_, err := c.ectdAPI.Revoke(context.Background(), *leaseID)
		if err != nil {
			log.Errorw("cannot-release-reservation", log.Fields{"key": key, "error": err})
			return err
		}
		delete(c.keyReservations, key)
	}
	return nil
}

// ReleaseReservation releases reservation for a specific key.
func (c *EtcdClient) ReleaseReservation(key string) error {
	// Get the leaseid using the key
	var ok bool
	var leaseID *v3Client.LeaseID
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if leaseID, ok = c.keyReservations[key]; !ok {
		return errors.New("key-not-reserved")
	}
	if leaseID != nil {
		_, err := c.ectdAPI.Revoke(context.Background(), *leaseID)
		if err != nil {
			log.Error(err)
			return err
		}
		delete(c.keyReservations, key)
	}
	return nil
}

// RenewReservation renews a reservation.  A reservation will go stale after the specified TTL (Time To Live)
// period specified when reserving the key
func (c *EtcdClient) RenewReservation(key string) error {
	// Get the leaseid using the key
	var ok bool
	var leaseID *v3Client.LeaseID
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if leaseID, ok = c.keyReservations[key]; !ok {
		return errors.New("key-not-reserved")
	}

	if leaseID != nil {
		_, err := c.ectdAPI.KeepAliveOnce(context.Background(), *leaseID)
		if err != nil {
			log.Errorw("lease-may-have-expired", log.Fields{"error": err})
			return err
		}
	} else {
		return errors.New("lease-expired")
	}
	return nil
}

// Watch provides the watch capability on a given key.  It returns a channel onto which the callee needs to
// listen to receive Events.
func (c *EtcdClient) Watch(key string) chan *Event {
	w := v3Client.NewWatcher(c.ectdAPI)
	channel := w.Watch(context.Background(), key, v3Client.WithPrefix())

	// Create a new channel
	ch := make(chan *Event, maxClientChannelBufferSize)

	// Keep track of the created channels so they can be closed when required
	channelMap := make(map[chan *Event]v3Client.Watcher)
	channelMap[ch] = w
	//c.writeLock.Lock()
	//defer c.writeLock.Unlock()
	c.watchedChannels[key] = append(c.watchedChannels[key], channelMap)

	log.Debugw("watched-channels", log.Fields{"channels": c.watchedChannels[key]})
	// Launch a go routine to listen for updates
	go c.listenForKeyChange(channel, ch)

	return ch

}

// CloseWatch closes a specific watch. Both the key and the channel are required when closing a watch as there
// may be multiple listeners on the same key.  The previously created channel serves as a key
func (c *EtcdClient) CloseWatch(key string, ch chan *Event) {
	// Get the array of channels mapping
	var watchedChannels []map[chan *Event]v3Client.Watcher
	var ok bool
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	if watchedChannels, ok = c.watchedChannels[key]; !ok {
		log.Warnw("key-has-no-watched-channels", log.Fields{"key": key})
		return
	}
	// Look for the channels
	var pos = -1
	for i, chMap := range watchedChannels {
		if t, ok := chMap[ch]; ok {
			log.Debug("channel-found")
			// Close the etcd watcher before the client channel.  This should close the etcd channel as well
			if err := t.Close(); err != nil {
				log.Errorw("watcher-cannot-be-closed", log.Fields{"key": key, "error": err})
			}
			close(ch)
			pos = i
			break
		}
	}
	// Remove that entry if present
	if pos >= 0 {
		c.watchedChannels[key] = append(c.watchedChannels[key][:pos], c.watchedChannels[key][pos+1:]...)
	}
	log.Infow("watcher-channel-exiting", log.Fields{"key": key, "channel": c.watchedChannels[key]})
}

func (c *EtcdClient) listenForKeyChange(channel v3Client.WatchChan, ch chan<- *Event) {
	log.Infow("start-listening-on-channel", log.Fields{"channel": ch})
	for resp := range channel {
		for _, ev := range resp.Events {
			//log.Debugf("%s %q : %q\n", ev.Type, ev.Kv.Key, ev.Kv.Value)
			ch <- NewEvent(getEventType(ev), ev.Kv.Key, ev.Kv.Value)
		}
	}
	log.Info("stop-listening-on-channel")
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
func (c *EtcdClient) Close() {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if err := c.ectdAPI.Close(); err != nil {
		log.Errorw("error-closing-client", log.Fields{"error": err})
	}
}
