/*
 * Copyright 2018-2023 Open Networking Foundation (ONF) and the ONF Contributors

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

package flow

import (
	"context"
	"sync"

	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
)

// Cache hides all low-level locking & synchronization related to flow state updates
type Cache struct {
	// this lock protects the flows map, it does not protect individual flows
	lock  sync.RWMutex
	flows map[uint64]*chunk
}

// chunk keeps a flow and the lock for this flow
type chunk struct {
	// this lock is used to synchronize all access to the flow, and also to the "deleted" variable
	lock    sync.Mutex
	deleted bool

	flow *ofp.OfpFlowStats
}

func NewCache() *Cache {
	return &Cache{
		flows: make(map[uint64]*chunk),
	}
}

// LockOrCreate locks this flow if it exists, or creates a new flow if it does not.
// In the case of flow creation, the provided "flow" must not be modified afterwards.
func (cache *Cache) LockOrCreate(ctx context.Context, flow *ofp.OfpFlowStats) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := cache.Lock(flow.Id); have {
		return handle, false, nil
	}

	cache.lock.Lock()
	entry, have := cache.flows[flow.Id]
	if !have {
		entry := &chunk{flow: flow}
		cache.flows[flow.Id] = entry
		entry.lock.Lock()
		cache.lock.Unlock()

		return &Handle{loader: cache, chunk: entry}, true, nil
	}
	cache.lock.Unlock()

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		return cache.LockOrCreate(ctx, flow)
	}
	return &Handle{loader: cache, chunk: entry}, false, nil
}

// Lock acquires the lock for this flow, and returns a handle which can be used to access the flow until it's unlocked.
// This handle ensures that the flow cannot be accessed if the lock is not held.
// Returns false if the flow is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (cache *Cache) Lock(id uint64) (*Handle, bool) {
	cache.lock.RLock()
	entry, have := cache.flows[id]
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

// GetReadOnly returns an *ofp.OfpFlowStats which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *ofp.OfpFlowStats {
	return h.chunk.flow
}

// Update updates an existing flow in cache.
// The provided "flow" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, flow *ofp.OfpFlowStats) error {
	h.chunk.flow = flow
	return nil
}

// Delete removes the flow from the cache
func (h *Handle) Delete(ctx context.Context) error {
	h.chunk.deleted = true

	h.loader.lock.Lock()
	delete(h.loader.flows, h.chunk.flow.Id)
	h.loader.lock.Unlock()

	h.Unlock()
	return nil
}

// Unlock releases the lock on the flow
func (h *Handle) Unlock() {
	if h.chunk != nil {
		h.chunk.lock.Unlock()
		h.chunk = nil // attempting to access the flow through this handle in future will panic
	}
}

// ListIDs returns a snapshot of all the managed flow IDs
// TODO: iterating through flows safely is expensive now, since all flows are stored & locked separately
//
//	should avoid this where possible
func (cache *Cache) ListIDs() map[uint64]struct{} {
	cache.lock.RLock()
	defer cache.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint64]struct{}, len(cache.flows))
	for id := range cache.flows {
		ret[id] = struct{}{}
	}
	return ret
}
# [EOF] - delta:force
