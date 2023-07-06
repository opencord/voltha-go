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

package group

import (
	"context"
	"sync"

	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
)

// Cache hides all low-level locking & synchronization related to group state updates
type Cache struct {
	// this lock protects the groups map, it does not protect individual groups
	lock   sync.RWMutex
	groups map[uint32]*chunk
}

// chunk keeps a group and the lock for this group
type chunk struct {
	// this lock is used to synchronize all access to the group, and also to the "deleted" variable
	lock    sync.Mutex
	deleted bool

	group *ofp.OfpGroupEntry
}

func NewCache() *Cache {
	return &Cache{
		groups: make(map[uint32]*chunk),
	}
}

// LockOrCreate locks this group if it exists, or creates a new group if it does not.
// In the case of group creation, the provided "group" must not be modified afterwards.
func (cache *Cache) LockOrCreate(ctx context.Context, group *ofp.OfpGroupEntry) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := cache.Lock(group.Desc.GroupId); have {
		return handle, false, nil
	}

	cache.lock.Lock()
	entry, have := cache.groups[group.Desc.GroupId]
	if !have {
		entry := &chunk{group: group}
		cache.groups[group.Desc.GroupId] = entry
		entry.lock.Lock()
		cache.lock.Unlock()

		return &Handle{loader: cache, chunk: entry}, true, nil
	}
	cache.lock.Unlock()

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		return cache.LockOrCreate(ctx, group)
	}
	return &Handle{loader: cache, chunk: entry}, false, nil
}

// Lock acquires the lock for this group, and returns a handle which can be used to access the group until it's unlocked.
// This handle ensures that the group cannot be accessed if the lock is not held.
// Returns false if the group is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (cache *Cache) Lock(id uint32) (*Handle, bool) {
	cache.lock.RLock()
	entry, have := cache.groups[id]
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

// GetReadOnly returns an *ofp.OfpGroupEntry which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *ofp.OfpGroupEntry {
	return h.chunk.group
}

// Update updates an existing group in cache.
// The provided "group" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, group *ofp.OfpGroupEntry) error {
	h.chunk.group = group
	return nil
}

// Delete removes the group from the cache
func (h *Handle) Delete(ctx context.Context) error {
	h.chunk.deleted = true

	h.loader.lock.Lock()
	delete(h.loader.groups, h.chunk.group.Desc.GroupId)
	h.loader.lock.Unlock()

	h.Unlock()
	return nil
}

// Unlock releases the lock on the group
func (h *Handle) Unlock() {
	if h.chunk != nil {
		h.chunk.lock.Unlock()
		h.chunk = nil // attempting to access the group through this handle in future will panic
	}
}

// ListIDs returns a snapshot of all the managed group IDs
// TODO: iterating through groups safely is expensive now, since all groups are stored & locked separately
//
//	should avoid this where possible
func (cache *Cache) ListIDs() map[uint32]struct{} {
	cache.lock.RLock()
	defer cache.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint32]struct{}, len(cache.groups))
	for id := range cache.groups {
		ret[id] = struct{}{}
	}
	return ret
}
