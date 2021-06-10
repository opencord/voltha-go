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

package meter

import (
	"context"
	"sync"

	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
)

// Cache hides all low-level locking & synchronization related to meter state updates
type Cache struct {
	// this lock protects the meters map, it does not protect individual meters
	lock   sync.RWMutex
	meters map[uint32]*chunk
}

// chunk keeps a meter and the lock for this meter
type chunk struct {
	// this lock is used to synchronize all access to the meter, and also to the "deleted" variable
	lock    sync.Mutex
	deleted bool

	meter *ofp.OfpMeterEntry
}

func NewCache() *Cache {
	return &Cache{
		meters: make(map[uint32]*chunk),
	}
}

// LockOrCreate locks this meter if it exists, or creates a new meter if it does not.
// In the case of meter creation, the provided "meter" must not be modified afterwards.
func (cache *Cache) LockOrCreate(ctx context.Context, meter *ofp.OfpMeterEntry) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := cache.Lock(meter.Config.MeterId); have {
		return handle, false, nil
	}

	cache.lock.Lock()
	entry, have := cache.meters[meter.Config.MeterId]
	if !have {
		entry := &chunk{meter: meter}
		cache.meters[meter.Config.MeterId] = entry
		entry.lock.Lock()
		cache.lock.Unlock()

		return &Handle{loader: cache, chunk: entry}, true, nil
	}
	cache.lock.Unlock()

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		return cache.LockOrCreate(ctx, meter)
	}
	return &Handle{loader: cache, chunk: entry}, false, nil
}

// Lock acquires the lock for this meter, and returns a handle which can be used to access the meter until it's unlocked.
// This handle ensures that the meter cannot be accessed if the lock is not held.
// Returns false if the meter is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (cache *Cache) Lock(id uint32) (*Handle, bool) {
	cache.lock.RLock()
	entry, have := cache.meters[id]
	cache.lock.RUnlock()

	if !have {
		return nil, false
	}

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		return cache.Lock(id)
	}
	return &Handle{loader: cache, chunk: entry}, true
}

// Handle is allocated for each Lock() call, all modifications are made using it, and it is invalidated by Unlock()
// This enforces correct Lock()-Usage()-Unlock() ordering.
type Handle struct {
	loader *Cache
	chunk  *chunk
}

// GetReadOnly returns an *ofp.OfpMeterEntry which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *ofp.OfpMeterEntry {
	return h.chunk.meter
}

// Update updates an existing meter in cache.
// The provided "meter" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, meter *ofp.OfpMeterEntry) error {
	h.chunk.meter = meter
	return nil
}

// Delete removes the meter from the cache
func (h *Handle) Delete(ctx context.Context) error {
	h.chunk.deleted = true

	h.loader.lock.Lock()
	delete(h.loader.meters, h.chunk.meter.Config.MeterId)
	h.loader.lock.Unlock()

	h.Unlock()
	return nil
}

// Unlock releases the lock on the meter
func (h *Handle) Unlock() {
	if h.chunk != nil {
		h.chunk.lock.Unlock()
		h.chunk = nil // attempting to access the meter through this handle in future will panic
	}
}

// ListIDs returns a snapshot of all the managed meter IDs
// TODO: iterating through meters safely is expensive now, since all meters are stored & locked separately
//       should avoid this where possible
func (cache *Cache) ListIDs() map[uint32]struct{} {
	cache.lock.RLock()
	defer cache.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint32]struct{}, len(cache.meters))
	for id := range cache.meters {
		ret[id] = struct{}{}
	}
	return ret
}
