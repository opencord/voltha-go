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

package port

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/device/db/loader"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// Loader hides all low-level locking & synchronization related to port state updates
type Loader struct {
	dbProxy *model.Proxy
	// this lock protects the ports map, it does not protect individual ports
	lock  sync.RWMutex
	ports map[uint32]*chunk
}

// chunk keeps a port and the lock for this port
type chunk struct {
	// this lock is used to synchronize all access to the port, and also to the "deleted" variable
	lock    sync.Mutex
	deleted bool

	port *voltha.Port
}

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy: dbProxy,
		ports:   make(map[uint32]*chunk),
	}
}

// Load queries existing ports from the kv,
// and should only be called once when first created.
func (l *Loader) Load(ctx context.Context) {
	l.lock.Lock()
	defer l.lock.Unlock()

	var ports []*voltha.Port
	if err := l.dbProxy.List(ctx, &ports); err != nil {
		logger.Errorw(ctx, "failed-to-list-ports-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, port := range ports {
		l.ports[port.PortNo] = &chunk{port: port}
	}
}

// LockOrCreate locks this port if it exists, or creates a new port if it does not.
// In the case of port creation, the provided "port" must not be modified afterwards.
func (l *Loader) LockOrCreate(txn loader.Txn, port *voltha.Port) (*Handle, bool) {
	// try to use read lock instead of full lock if possible
	if handle, have := l.Lock(txn, port.PortNo); have {
		return handle, false
	}

retry:
	l.lock.Lock()
	if entry, have := l.ports[port.PortNo]; have {
		l.lock.Unlock()

		// if the entry exists, just need to lock it
		entry.lock.Lock()
		if entry.deleted {
			entry.lock.Unlock()
			goto retry // entry was deleted, restart from the beginning
		}
		return &Handle{loader: l, chunk: entry}, false
	}

	// if entry is not found, create a new one
	entry := &chunk{port: port}
	entry.lock.Lock()
	// and add it to the list
	l.ports[port.PortNo] = entry
	l.lock.Unlock()

	// add the port creation to the port loader
	txn.Set(l.dbProxy, fmt.Sprint(port.PortNo), port, func(success bool) {
		defer entry.lock.Unlock()
		if !success {
			// revert the map
			l.lock.Lock()
			delete(l.ports, port.PortNo)
			l.lock.Unlock()

			entry.deleted = true
		}
	})
	return nil, true
}

// Lock acquires the lock for this port, and returns a handle which can be used to access the port until it's unlocked.
// This handle ensures that the port cannot be accessed if the lock is not held.
// Returns false if the port is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (l *Loader) Lock(txn loader.Txn, id uint32) (*Handle, bool) {
	txn.CheckSaneLockOrder(loader.LockID{Type: loader.Port, ID: id})

retry:
	l.lock.RLock()
	entry, have := l.ports[id]
	l.lock.RUnlock()

	if !have {
		return nil, false
	}

	entry.lock.Lock()
	if entry.deleted {
		entry.lock.Unlock()
		goto retry // entry was deleted, restart from the beginning
	}
	return &Handle{loader: l, chunk: entry}, true
}

// Handle is allocated for each Lock() call, all modifications are made using it, and it is invalidated by Unlock()
// This enforces correct Lock()-Usage()-Unlock() ordering.
type Handle struct {
	txn    loader.Txn
	loader *Loader
	chunk  *chunk
}

// GetReadOnly returns an *voltha.Port which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *voltha.Port {
	return h.chunk.port
}

// Update updates an existing port in the kv.
// The provided "port" must not be modified afterwards.
func (h *Handle) Update(port *voltha.Port) {
	localH := h.takeHandleOwnership()
	localH.txn.Set(localH.loader.dbProxy, fmt.Sprint(port.PortNo), port, func(success bool) {
		defer localH.Unlock()
		if success {
			localH.chunk.port = port
		}
	})
}

// Delete removes the device from the kv
func (h *Handle) Delete() {
	localH := h.takeHandleOwnership()
	localH.txn.Remove(localH.loader.dbProxy, fmt.Sprint(localH.chunk.port.PortNo), func(success bool) {
		defer localH.Unlock()
		if success {
			localH.chunk.deleted = true

			localH.loader.lock.Lock()
			delete(localH.loader.ports, localH.chunk.port.PortNo)
			localH.loader.lock.Unlock()
		}
	})
}

// Unlock releases the lock on the port
func (h *Handle) Unlock() {
	if h.chunk != nil {
		h.chunk.lock.Unlock()
		h.chunk, h.txn = nil, nil
	}
}

// takeHandleOwnership ensures that any further access using the old handle will panic,
// and returns a new handle which is valid to use instead
func (h *Handle) takeHandleOwnership() Handle {
	newHandle := *h
	h.chunk, h.txn = nil, nil
	return newHandle
}

// ListIDs returns a snapshot of all the managed port IDs
// TODO: iterating through ports safely is expensive now, since all ports are stored & locked separately
//       should avoid this where possible
func (l *Loader) ListIDs() []uint32 {
	l.lock.RLock()
	defer l.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ctr, ret := 0, make([]uint32, len(l.ports))
	for id := range l.ports {
		ret[ctr] = id
		ctr++
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i] < ret[j] })
	return ret
}
