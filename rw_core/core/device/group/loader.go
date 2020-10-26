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

package group

import (
	"context"
	"fmt"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Loader hides all low-level locking & synchronization related to group state updates
type Loader struct {
	dbProxy *model.Proxy
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

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy: dbProxy,
		groups:  make(map[uint32]*chunk),
	}
}

// Load queries existing groups from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var groups []*ofp.OfpGroupEntry
	if err := loader.dbProxy.List(ctx, &groups); err != nil {
		logger.Errorw(ctx, "failed-to-list-groups-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, group := range groups {
		loader.groups[group.Desc.GroupId] = &chunk{group: group}
	}
}

// LockOrCreate locks this group if it exists, or creates a new group if it does not.
// In the case of group creation, the provided "group" must not be modified afterwards.
func (loader *Loader) LockOrCreate(ctx context.Context, group *ofp.OfpGroupEntry) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := loader.Lock(group.Desc.GroupId); have {
		return handle, false, nil
	}

	loader.lock.Lock()
	entry, have := loader.groups[group.Desc.GroupId]
	if !have {
		entry := &chunk{group: group}
		loader.groups[group.Desc.GroupId] = entry
		entry.lock.Lock()
		loader.lock.Unlock()

		if err := loader.dbProxy.Set(ctx, fmt.Sprint(group.Desc.GroupId), group); err != nil {
			// revert the map
			loader.lock.Lock()
			delete(loader.groups, group.Desc.GroupId)
			loader.lock.Unlock()

			entry.deleted = true
			entry.lock.Unlock()
			return nil, false, err
		}
		return &Handle{loader: loader, chunk: entry}, true, nil
	}
	loader.lock.Unlock()

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		return loader.LockOrCreate(ctx, group)
	}
	return &Handle{loader: loader, chunk: entry}, false, nil
}

// Lock acquires the lock for this group, and returns a handle which can be used to access the group until it's unlocked.
// This handle ensures that the group cannot be accessed if the lock is not held.
// Returns false if the group is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock(id uint32) (*Handle, bool) {
	loader.lock.RLock()
	entry, have := loader.groups[id]
	loader.lock.RUnlock()

	if !have {
		return nil, false
	}

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		return loader.Lock(id)
	}
	return &Handle{loader: loader, chunk: entry}, true
}

// Handle is allocated for each Lock() call, all modifications are made using it, and it is invalidated by Unlock()
// This enforces correct Lock()-Usage()-Unlock() ordering.
type Handle struct {
	loader *Loader
	chunk  *chunk
}

// GetReadOnly returns an *ofp.OfpGroupEntry which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *ofp.OfpGroupEntry {
	return h.chunk.group
}

// Update updates an existing group in the kv.
// The provided "group" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, group *ofp.OfpGroupEntry) error {
	if err := h.loader.dbProxy.Set(ctx, fmt.Sprint(group.Desc.GroupId), group); err != nil {
		return status.Errorf(codes.Internal, "failed-update-group-%v: %s", group.Desc.GroupId, err)
	}
	h.chunk.group = group
	return nil
}

// Delete removes the device from the kv
func (h *Handle) Delete(ctx context.Context) error {
	if err := h.loader.dbProxy.Remove(ctx, fmt.Sprint(h.chunk.group.Desc.GroupId)); err != nil {
		return fmt.Errorf("couldnt-delete-group-from-store-%v", h.chunk.group.Desc.GroupId)
	}
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
//       should avoid this where possible
func (loader *Loader) ListIDs() map[uint32]struct{} {
	loader.lock.RLock()
	defer loader.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint32]struct{}, len(loader.groups))
	for id := range loader.groups {
		ret[id] = struct{}{}
	}
	return ret
}
