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

package update

import (
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// Loader hides all low-level locking & synchronization related to device updates
type Loader struct {
	dbProxy *model.Proxy
	// this lock protects the deviceUpdate Array, it does not protect individual entries
	lock sync.RWMutex
}

func NewLoader(dbProxy *model.Proxy) *Loader {
	return &Loader{
		dbProxy: dbProxy,
	}
}

// Load queries existing ports from the kv,
// and should only be called once when first created.
func (loader *Loader) Load(ctx context.Context) *voltha.DeviceUpdates {
	loader.lock.Lock()
	defer loader.lock.Unlock()

	var updates []*voltha.DeviceUpdate
	if err := loader.dbProxy.List(ctx, &updates); err != nil {
		logger.Errorw(ctx, "failed-to-list-updates-from-cluster-data-proxy", log.Fields{"error": err})
		return nil
	}
	return &voltha.DeviceUpdates{Items: updates}
}

// Lock acquires the lock for this port, and returns a handle which can be used to access the port until it's unlocked.
// This handle ensures that the port cannot be accessed if the lock is not held.
// Returns false if the port is not present.
// TODO: consider accepting a ctx and aborting the lock attempt on cancellation
func (loader *Loader) Lock() *Handle {
	loader.lock.Lock()
	return &Handle{loader: loader}
}

// Unlock releases the lock on the port
func (h *Handle) Unlock() {
	h.loader.lock.Unlock()
}

// Handle is allocated for each Lock() call, all modifications are made using it, and it is invalidated by Unlock()
// This enforces correct Lock()-Usage()-Unlock() ordering.
type Handle struct {
	loader *Loader
}

// Update updates an device operation update in the kv.
func (h *Handle) Update(ctx context.Context, update *voltha.DeviceUpdate) error {
	id := fmt.Sprintf("%v.%v", update.Timestamp.Seconds, update.Timestamp.Nanos)

	if err := h.loader.dbProxy.Set(ctx, id, update); err != nil {
		return status.Errorf(codes.Internal, "failed-update-device-%v: %s", update.DeviceId, err)
	}

	return nil
}
