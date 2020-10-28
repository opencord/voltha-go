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
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Loader hides all low-level locking & synchronization related to port state updates
type Loader struct {
	dbProxy *model.Proxy
	// this lock protects the ports map, it does not protect individual ports
	lock         sync.RWMutex
	ports        map[uint32]*chunk
	deviceLookup map[string]map[uint32]struct{}
}

// chunk keeps a port and the lock for this port
type chunk struct {
	// this lock is used to synchronize all access to the port, and also to the "deleted" variable
	lock    sync.Mutex
	deleted bool

	port *voltha.LogicalPort
}

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy:      dbProxy,
		ports:        make(map[uint32]*chunk),
		deviceLookup: make(map[string]map[uint32]struct{}),
	}
}

// Load queries existing ports from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var ports []*voltha.LogicalPort
	if err := loader.dbProxy.List(ctx, &ports); err != nil {
		logger.Errorw(ctx, "failed-to-list-ports-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, port := range ports {
		loader.ports[port.OfpPort.PortNo] = &chunk{port: port}
		loader.addLookup(port.DeviceId, port.OfpPort.PortNo)
	}
}

// LockOrCreate locks this port if it exists, or creates a new port if it does not.
// In the case of port creation, the provided "port" must not be modified afterwards.
func (loader *Loader) LockOrCreate(ctx context.Context, port *voltha.LogicalPort) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := loader.Lock(port.OfpPort.PortNo); have {
		return handle, false, nil
	}

	loader.lock.Lock()
	entry, have := loader.ports[port.OfpPort.PortNo]
	if !have {
		entry := &chunk{port: port}
		loader.ports[port.OfpPort.PortNo] = entry
		loader.addLookup(port.DeviceId, port.OfpPort.PortNo)
		entry.lock.Lock()
		loader.lock.Unlock()

		if err := loader.dbProxy.Set(ctx, fmt.Sprint(port.OfpPort.PortNo), port); err != nil {
			// revert the map
			loader.lock.Lock()
			delete(loader.ports, port.OfpPort.PortNo)
			loader.removeLookup(port.DeviceId, port.OfpPort.PortNo)
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
		return loader.LockOrCreate(ctx, port)
	}
	return &Handle{loader: loader, chunk: entry}, false, nil
}

// Lock acquires the lock for this port, and returns a handle which can be used to access the port until it's unlocked.
// This handle ensures that the port cannot be accessed if the lock is not held.
// Returns false if the port is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock(id uint32) (*Handle, bool) {
	loader.lock.RLock()
	entry, have := loader.ports[id]
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

// GetReadOnly returns an *voltha.LogicalPort which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *voltha.LogicalPort {
	return h.chunk.port
}

// Update updates an existing port in the kv.
// The provided "port" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, port *voltha.LogicalPort) error {
	if err := h.loader.dbProxy.Set(ctx, fmt.Sprint(port.OfpPort.PortNo), port); err != nil {
		return status.Errorf(codes.Internal, "failed-update-port-%v: %s", port.OfpPort.PortNo, err)
	}
	h.chunk.port = port
	return nil
}

// Delete removes the device from the kv
func (h *Handle) Delete(ctx context.Context) error {
	if err := h.loader.dbProxy.Remove(ctx, fmt.Sprint(h.chunk.port.OfpPort.PortNo)); err != nil {
		return fmt.Errorf("couldnt-delete-port-from-store-%v", h.chunk.port.OfpPort.PortNo)
	}
	h.chunk.deleted = true

	h.loader.lock.Lock()
	delete(h.loader.ports, h.chunk.port.OfpPort.PortNo)
	h.loader.removeLookup(h.chunk.port.DeviceId, h.chunk.port.OfpPort.PortNo)
	h.loader.lock.Unlock()

	h.Unlock()
	return nil
}

// Unlock releases the lock on the port
func (h *Handle) Unlock() {
	if h.chunk != nil {
		h.chunk.lock.Unlock()
		h.chunk = nil // attempting to access the port through this handle in future will panic
	}
}

// ListIDs returns a snapshot of all the managed port IDs
// TODO: iterating through ports safely is expensive now, since all ports are stored & locked separately
//       should avoid this where possible
func (loader *Loader) ListIDs() map[uint32]struct{} {
	loader.lock.RLock()
	defer loader.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[uint32]struct{}, len(loader.ports))
	for id := range loader.ports {
		ret[id] = struct{}{}
	}
	return ret
}

// ListIDsForDevice lists ports belonging to the specified device
func (loader *Loader) ListIDsForDevice(deviceID string) map[uint32]struct{} {
	loader.lock.RLock()
	defer loader.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	devicePorts := loader.deviceLookup[deviceID]
	ret := make(map[uint32]struct{}, len(devicePorts))
	for id := range devicePorts {
		ret[id] = struct{}{}
	}
	return ret
}

func (loader *Loader) addLookup(deviceID string, portNo uint32) {
	if devicePorts, have := loader.deviceLookup[deviceID]; have {
		devicePorts[portNo] = struct{}{}
	} else {
		loader.deviceLookup[deviceID] = map[uint32]struct{}{portNo: {}}
	}
}

func (loader *Loader) removeLookup(deviceID string, portNo uint32) {
	if devicePorts, have := loader.deviceLookup[deviceID]; have {
		delete(devicePorts, portNo)
		if len(devicePorts) == 0 {
			delete(loader.deviceLookup, deviceID)
		}
	}
}
