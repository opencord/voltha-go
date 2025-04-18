/*
 * Copyright 2020-2024 Open Networking Foundation (ONF) and the ONF Contributors

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

package transientstate

import (
	"context"
	"fmt"
	"sync"

	"github.com/opencord/voltha-protos/v5/go/core"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Loader hides all low-level locking & synchronization related to device transient state updates
type Loader struct {
	dbProxy              *model.Proxy
	deviceTransientState *data
	// this lock protects the device transient state
	lock sync.RWMutex
}

type data struct {
	deviceID       string
	transientState core.DeviceTransientState_Types
}

func NewLoader(dbProxy *model.Proxy, deviceID string) *Loader {
	return &Loader{
		dbProxy: dbProxy,
		deviceTransientState: &data{
			transientState: core.DeviceTransientState_NONE,
			deviceID:       deviceID,
		},
	}
}

// Load queries existing transient state from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var deviceTransientState core.DeviceTransientState
	have, err := loader.dbProxy.Get(ctx, loader.deviceTransientState.deviceID, &deviceTransientState)
	if err != nil {
		logger.Errorw(ctx, "failed-to-get-device-transient-state-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	if have {
		loader.deviceTransientState.transientState = deviceTransientState.TransientState
		return
	}
	loader.deviceTransientState.transientState = core.DeviceTransientState_NONE
}

// Lock acquires the lock for deviceTransientStateLoader, and returns a handle
// which can be used to access it until it's unlocked.
// This handle ensures that the deviceTransientState cannot be accessed if the lock is not held.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock() *Handle {
	loader.lock.Lock()
	dataTransientState := loader.deviceTransientState
	return &Handle{loader: loader, data: dataTransientState}
}

// Handle is allocated for each Lock() call, all modifications are made using it, and it is invalidated by Unlock()
// This enforces correct Lock()-Usage()-Unlock() ordering.
type Handle struct {
	loader *Loader
	data   *data
}

// GetReadOnly returns device transient which MUST NOT be modified externally, but which is safe to keep indefinitely
func (h *Handle) GetReadOnly() core.DeviceTransientState_Types {
	return h.data.transientState
}

// Update updates device transient state in KV store
// The provided device transient state must not be modified afterwards.
func (h *Handle) Update(ctx context.Context, state core.DeviceTransientState_Types) error {
	var tState core.DeviceTransientState
	tState.TransientState = state
	if err := h.loader.dbProxy.Set(ctx, fmt.Sprint(h.data.deviceID), &tState); err != nil {
		return status.Errorf(codes.Internal, "failed-to-update-device-%v-transient-state: %s", h.data.deviceID, err)
	}
	h.data.transientState = state
	return nil
}

// Delete deletes device transient state from KV store
func (h *Handle) Delete(ctx context.Context) error {
	if err := h.loader.dbProxy.Remove(ctx, fmt.Sprint(h.data.deviceID)); err != nil {
		return status.Errorf(codes.Internal, "failed-to-delete-device-%v-transient-state: %s", h.data.deviceID, err)
	}
	return nil
}

// UnLock releases the lock on the device transient state.
func (h *Handle) UnLock() {
	defer h.loader.lock.Unlock()
	h.data = nil
}
