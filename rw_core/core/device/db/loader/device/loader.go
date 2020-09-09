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

package device

import (
	"context"
	"fmt"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Loader hides all low-level locking & synchronization related to device state updates
type Loader struct {
	dbProxy *model.Proxy
	// this lock protects the devices map, it does not protect individual devices
	lock    sync.RWMutex
	devices map[string]*chunk
}

// chunk keeps a device and the lock for this device
type chunk struct {
	// this lock is used to synchronize all access to the device, and also to the "deleted" variable
	lock    sync.Mutex
	deleted bool

	device *voltha.Device
}

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy: dbProxy,
		devices: make(map[string]*chunk),
	}
}

// Load queries existing devices from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var devices []*voltha.Device
	if err := loader.dbProxy.List(ctx, &devices); err != nil {
		logger.Errorw(ctx, "failed-to-list-devices-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, device := range devices {
		loader.devices[device.Id] = &chunk{device: device}
	}
}

// LockOrCreate locks this device if it exists, or creates a new device if it does not.
// In the case of device creation, the provided "device" must not be modified afterwards.
func (loader *Loader) LockOrCreate(ctx context.Context, device *voltha.Device) (*Handle, bool, error) {
	// try to use read lock instead of full lock if possible
	if handle, have := loader.Lock(device.Id); have {
		return handle, false, nil
	}

	loader.lock.Lock()
	entry, have := loader.devices[device.Id]
	if !have {
		entry := &chunk{device: device}
		loader.devices[device.Id] = entry
		entry.lock.Lock()
		loader.lock.Unlock()

		if err := loader.dbProxy.Set(ctx, fmt.Sprint(device.Id), device); err != nil {
			// revert the map
			loader.lock.Lock()
			delete(loader.devices, device.Id)
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
		return loader.LockOrCreate(ctx, device)
	}
	return &Handle{loader: loader, chunk: entry}, false, nil
}

// Lock acquires the lock for this device, and returns a handle which can be used to access the device until it's unlocked.
// This handle ensures that the device cannot be accessed if the lock is not held.
// Returns false if the device is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock(id string) (*Handle, bool) {
	loader.lock.RLock()
	entry, have := loader.devices[id]
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

// GetReadOnly returns an *voltha.Device which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() *voltha.Device {
	return h.chunk.device
}

// Update updates an existing device in the kv.
// The provided "device" must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, device *voltha.Device) error {
	if err := h.loader.dbProxy.Set(ctx, fmt.Sprint(device.Id), device); err != nil {
		return status.Errorf(codes.Internal, "failed-update-device-%v: %s", device.Id, err)
	}
	h.chunk.device = device
	return nil
}

// Delete removes the device from the kv
func (h *Handle) Delete(ctx context.Context) error {
	if err := h.loader.dbProxy.Remove(ctx, fmt.Sprint(h.chunk.device.Id)); err != nil {
		return fmt.Errorf("couldnt-delete-device-from-store-%v", h.chunk.device.Id)
	}
	h.chunk.deleted = true

	h.loader.lock.Lock()
	delete(h.loader.devices, h.chunk.device.Id)
	h.loader.lock.Unlock()

	h.Unlock()
	return nil
}

// Unlock releases the lock on the device
func (h *Handle) Unlock() {
	if h.chunk != nil {
		h.chunk.lock.Unlock()
		h.chunk = nil // attempting to access the device through this handle in future will panic
	}
}

// ListIDs returns a snapshot of all the managed device IDs
// TODO: iterating through devices safely is expensive now, since all devices are stored & locked separately
//       should avoid this where possible
func (loader *Loader) ListIDs() map[string]struct{} {
	loader.lock.RLock()
	defer loader.lock.RUnlock()
	// copy the IDs so caller can safely iterate
	ret := make(map[string]struct{}, len(loader.devices))
	for id := range loader.devices {
		ret[id] = struct{}{}
	}
	return ret
}
