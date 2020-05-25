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

package flow

import (
	"context"
	"fmt"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Loader hides all low-level locking & synchronization related to flow state updates
type Loader struct {
	dbProxy *model.Proxy
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

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy: dbProxy,
		flows:   make(map[uint64]*chunk),
	}
}

// Load queries existing flows from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var flows []*ofp.OfpFlowStats
	if err := loader.dbProxy.List(ctx, &flows); err != nil {
		logger.Errorw("failed-to-list-flows-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, flow := range flows {
		loader.flows[flow.Id] = &chunk{flow: flow}
	}
}

// LockOrCreate locks this flow if it exists, or creates a new flow if it does not.
// In the case of flow creation, the provided "flow" must not be modified afterwards.
func (loader *Loader) LockOrCreate(ctx context.Context, flow *ofp.OfpFlowStats) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := loader.Lock(flow.Id); have {
		return handle, false, nil
	}

	loader.lock.Lock()
	entry, have := loader.flows[flow.Id]
	if !have {
		entry := &chunk{flow: flow}
		loader.flows[flow.Id] = entry
		entry.lock.Lock()
		loader.lock.Unlock()

		if err := loader.dbProxy.Set(ctx, fmt.Sprint(flow.Id), flow); err != nil {
			// revert the map
			loader.lock.Lock()
			delete(loader.flows, flow.Id)
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
		return loader.LockOrCreate(ctx, flow)
	}
	return &Handle{loader: loader, chunk: entry}, false, nil
}

// Lock acquires the lock for this flow, and returns a handle which can be used to access the flow until it's unlocked.
// This handle ensures that the flow cannot be accessed if the lock is not held.
// Returns false if the flow is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock(id uint64) (*Handle, bool) {
	loader.lock.RLock()
	entry, have := loader.flows[id]
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

// GetReadOnly returns an *ofp.OfpFlowStats which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *ofp.OfpFlowStats {
	return h.chunk.flow
}

// Update updates an existing flow in the kv.
// The provided "flow" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, flow *ofp.OfpFlowStats) error {
	if err := h.loader.dbProxy.Set(ctx, fmt.Sprint(flow.Id), flow); err != nil {
		return status.Errorf(codes.Internal, "failed-update-flow-%v: %s", flow.Id, err)
	}
	h.chunk.flow = flow
	return nil
}

// Delete removes the device from the kv
func (h *Handle) Delete(ctx context.Context) error {
	if err := h.loader.dbProxy.Remove(ctx, fmt.Sprint(h.chunk.flow.Id)); err != nil {
		return fmt.Errorf("couldnt-delete-flow-from-store-%v", h.chunk.flow.Id)
	}
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

// List returns a snapshot of all the managed flow IDs
// TODO: iterating through flows safely is expensive now, since all flows are stored & locked separately
//       should avoid this where possible
func (loader *Loader) List() map[uint64]struct{} {
	loader.lock.RLock()
	defer loader.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint64]struct{}, len(loader.flows))
	for id := range loader.flows {
		ret[id] = struct{}{}
	}
	return ret
}
