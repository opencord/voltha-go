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
	"fmt"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Loader hides all low-level locking & synchronization related to meter state updates
type Loader struct {
	dbProxy *model.Proxy
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

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy: dbProxy,
		meters:  make(map[uint32]*chunk),
	}
}

// Load queries existing meters from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var meters []*ofp.OfpMeterEntry
	if err := loader.dbProxy.List(ctx, &meters); err != nil {
		logger.Errorw(ctx, "failed-to-list-meters-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, meter := range meters {
		loader.meters[meter.Config.MeterId] = &chunk{meter: meter}
	}
}

// LockOrCreate locks this meter if it exists, or creates a new meter if it does not.
// In the case of meter creation, the provided "meter" must not be modified afterwards.
func (loader *Loader) LockOrCreate(ctx context.Context, meter *ofp.OfpMeterEntry) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := loader.Lock(meter.Config.MeterId); have {
		return handle, false, nil
	}

	loader.lock.Lock()
	entry, have := loader.meters[meter.Config.MeterId]
	if !have {
		entry := &chunk{meter: meter}
		loader.meters[meter.Config.MeterId] = entry
		entry.lock.Lock()
		loader.lock.Unlock()

		if err := loader.dbProxy.Set(ctx, fmt.Sprint(meter.Config.MeterId), meter); err != nil {
			// revert the map
			loader.lock.Lock()
			delete(loader.meters, meter.Config.MeterId)
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
		return loader.LockOrCreate(ctx, meter)
	}
	return &Handle{loader: loader, chunk: entry}, false, nil
}

// Lock acquires the lock for this meter, and returns a handle which can be used to access the meter until it's unlocked.
// This handle ensures that the meter cannot be accessed if the lock is not held.
// Returns false if the meter is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock(id uint32) (*Handle, bool) {
	loader.lock.RLock()
	entry, have := loader.meters[id]
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

// GetReadOnly returns an *ofp.OfpMeterEntry which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *ofp.OfpMeterEntry {
	return h.chunk.meter
}

// Update updates an existing meter in the kv.
// The provided "meter" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, meter *ofp.OfpMeterEntry) error {
	if err := h.loader.dbProxy.Set(ctx, fmt.Sprint(meter.Config.MeterId), meter); err != nil {
		return status.Errorf(codes.Internal, "failed-update-meter-%v: %s", meter.Config.MeterId, err)
	}
	h.chunk.meter = meter
	return nil
}

// Delete removes the device from the kv
func (h *Handle) Delete(ctx context.Context) error {
	if err := h.loader.dbProxy.Remove(ctx, fmt.Sprint(h.chunk.meter.Config.MeterId)); err != nil {
		return fmt.Errorf("couldnt-delete-meter-from-store-%v", h.chunk.meter.Config.MeterId)
	}
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
func (loader *Loader) ListIDs() map[uint32]struct{} {
	loader.lock.RLock()
	defer loader.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint32]struct{}, len(loader.meters))
	for id := range loader.meters {
		ret[id] = struct{}{}
	}
	return ret
}
