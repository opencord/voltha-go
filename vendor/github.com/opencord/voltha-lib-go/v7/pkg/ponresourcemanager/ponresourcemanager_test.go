/*
 * Copyright 2020-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ponresourcemanager

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/boljen/go-bitmap"
	"strings"
	"testing"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	"github.com/opencord/voltha-lib-go/v7/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/stretchr/testify/assert"
)

const (
	GEM_POOL_PATH        = "gemport_id_pool"
	RESERVED_GEM_PORT_ID = uint32(5)
)

// MockKVClient mocks the AdapterProxy interface.
type MockResKVClient struct {
	resourceMap map[string]interface{}
}

func newMockKvClient(ctx context.Context) *MockResKVClient {
	var mockResKVClient MockResKVClient
	mockResKVClient.resourceMap = make(map[string]interface{})
	return &mockResKVClient
}

// List function implemented for KVClient.
func (kvclient *MockResKVClient) List(ctx context.Context, key string) (map[string]*kvstore.KVPair, error) {
	return nil, errors.New("key didn't find")
}

// Get mock function implementation for KVClient
func (kvclient *MockResKVClient) Get(ctx context.Context, key string) (*kvstore.KVPair, error) {
	logger.Debugw(ctx, "Get of MockKVClient called", log.Fields{"key": key})
	if key != "" {
		if strings.Contains(key, RESERVED_GEMPORT_IDS_PATH) {
			logger.Debug(ctx, "Getting Key:", RESERVED_GEMPORT_IDS_PATH)
			reservedGemPorts := []uint32{RESERVED_GEM_PORT_ID}
			str, _ := json.Marshal(reservedGemPorts)
			return kvstore.NewKVPair(key, str, "mock", 3000, 1), nil
		}
		if strings.Contains(key, GEM_POOL_PATH) {
			logger.Debug(ctx, "Getting Key:", GEM_POOL_PATH)
			resource := kvclient.resourceMap[key]
			return kvstore.NewKVPair(key, resource, "mock", 3000, 1), nil
		}
		maps := make(map[string]*kvstore.KVPair)
		maps[key] = &kvstore.KVPair{Key: key}
		return maps[key], nil
	}
	return nil, errors.New("key didn't find")
}

// Put mock function implementation for KVClient
func (kvclient *MockResKVClient) Put(ctx context.Context, key string, value interface{}) error {
	if key != "" {
		if strings.Contains(key, GEMPORT_ID_POOL_PATH) && value != nil {
			kvclient.resourceMap[key] = value
		}
		return nil
	}
	return errors.New("key didn't find")
}

// Delete mock function implementation for KVClient
func (kvclient *MockResKVClient) Delete(ctx context.Context, key string) error {
	return nil
}

func (c *MockResKVClient) DeleteWithPrefix(ctx context.Context, prefixKey string) error {
	return nil
}

// Reserve mock function implementation for KVClient
func (kvclient *MockResKVClient) Reserve(ctx context.Context, key string, value interface{}, ttl time.Duration) (interface{}, error) {
	return nil, errors.New("key didn't find")
}

// ReleaseReservation mock function implementation for KVClient
func (kvclient *MockResKVClient) ReleaseReservation(ctx context.Context, key string) error {
	return nil
}

// ReleaseAllReservations mock function implementation for KVClient
func (kvclient *MockResKVClient) ReleaseAllReservations(ctx context.Context) error {
	return nil
}

// RenewReservation mock function implementation for KVClient
func (kvclient *MockResKVClient) RenewReservation(ctx context.Context, key string) error {
	return nil
}

// Watch mock function implementation for KVClient
func (kvclient *MockResKVClient) Watch(ctx context.Context, key string, withPrefix bool) chan *kvstore.Event {
	return nil
}

// AcquireLock mock function implementation for KVClient
func (kvclient *MockResKVClient) AcquireLock(ctx context.Context, lockName string, timeout time.Duration) error {
	return nil
}

// ReleaseLock mock function implementation for KVClient
func (kvclient *MockResKVClient) ReleaseLock(lockName string) error {
	return nil
}

// IsConnectionUp mock function implementation for KVClient
func (kvclient *MockResKVClient) IsConnectionUp(ctx context.Context) bool { // timeout in second
	return true
}

// CloseWatch mock function implementation for KVClient
func (kvclient *MockResKVClient) CloseWatch(ctx context.Context, key string, ch chan *kvstore.Event) {
}

// Close mock function implementation for KVClient
func (kvclient *MockResKVClient) Close(ctx context.Context) {
}

func TestExcludeReservedGemPortIdFromThePool(t *testing.T) {
	ctx := context.Background()
	PONRMgr, err := NewPONResourceManager(ctx, "gpon", "onu", "olt1",
		"etcd", "1:1", "service/voltha")
	if err != nil {
		return
	}
	PONRMgr.KVStore = &db.Backend{
		Client: newMockKvClient(ctx),
	}

	PONRMgr.KVStoreForConfig = &db.Backend{
		Client: newMockKvClient(ctx),
	}
	// create a pool in the range of [1,16]
	// and exclude id 5 from this pool
	StartIndex := uint32(1)
	EndIndex := uint32(16)

	reservedGemPortIds, defined := PONRMgr.getReservedGemPortIdsFromKVStore(ctx)
	if !defined {
		return
	}

	FormatResult, err := PONRMgr.FormatResource(ctx, 1, StartIndex, EndIndex, reservedGemPortIds)
	if err != nil {
		t.Error("Failed to format resource", err)
		return
	}

	// Add resource as json in kv store.
	err = PONRMgr.KVStore.Put(ctx, GEMPORT_ID_POOL_PATH, FormatResult)
	if err != nil {
		t.Error("Error in posting data to kv store", GEMPORT_ID_POOL_PATH)
		return
	}

	for i := StartIndex; i <= (EndIndex - uint32(len(reservedGemPortIds))); i++ {
		// get gem port id pool from the kv store
		resource, err := PONRMgr.GetResource(context.Background(), GEMPORT_ID_POOL_PATH)
		if err != nil {
			t.Error("Failed to get resource from gem port id pool", err)
			return
		}
		// get a gem port id from the pool
		nextID, err := PONRMgr.GenerateNextID(ctx, resource)
		if err != nil {
			t.Error("Failed to get gem port id from the pool", err)
			return
		}

		//given gem port id should not equal to the reserved gem port id
		assert.NotEqual(t, nextID, RESERVED_GEM_PORT_ID)
		// put updated gem port id pool into the kv store
		err = PONRMgr.UpdateResource(context.Background(), GEMPORT_ID_POOL_PATH, resource)
		if err != nil {
			t.Error("Failed to put updated gem port id pool into the kv store", err)
			return
		}
	}

}

func TestResourcePoolOverflow(t *testing.T) {
	ctx := context.Background()
	PONRMgr, err := NewPONResourceManager(ctx, "gpon", "onu", "olt1",
		"etcd", "1:1", "service/voltha")
	if err != nil {
		return
	}
	PONRMgr.KVStore = &db.Backend{
		Client: newMockKvClient(ctx),
	}

	PONRMgr.KVStoreForConfig = &db.Backend{
		Client: newMockKvClient(ctx),
	}
	// create a pool in the range of [1,16]
	StartIndex := uint32(1)
	EndIndex := uint32(16)

	FormatResult, err := PONRMgr.FormatResource(ctx, 1, StartIndex, EndIndex, []uint32{})
	if err != nil {
		t.Error("Failed to format resource", err)
		return
	}

	// Add resource as json in kv store.
	err = PONRMgr.KVStore.Put(ctx, GEMPORT_ID_POOL_PATH, FormatResult)
	if err != nil {
		t.Error("Error in posting data to kv store", GEMPORT_ID_POOL_PATH)
		return
	}

	for i := 1; i <= 20; i++ {
		// get gem port id pool from the kv store
		resource, err := PONRMgr.GetResource(context.Background(), GEMPORT_ID_POOL_PATH)
		if err != nil {
			t.Error("Failed to get resource from gem port id pool", err)
			return
		}
		// get a gem port id from the pool
		nextID, err := PONRMgr.GenerateNextID(ctx, resource)
		// all free ids in the pool will be consumed by the first 16 steps of the loop
		// resource-exhausted error is expected from the pool at the 17th step of the loop
		if i > int(EndIndex) {
			assert.NotNil(t, err)
		} else if err != nil {
			t.Error("Failed to get gem port id from the pool", err)
			return
		} else {
			assert.NotEqual(t, 0, nextID)
			// put updated gem port id pool into the kv store
			err = PONRMgr.UpdateResource(context.Background(), GEMPORT_ID_POOL_PATH, resource)
			if err != nil {
				t.Error("Failed to put updated gem port id pool into the kv store", err)
				return
			}
		}
	}
}

func TestPONResourceManager_ReleaseInvalidID(t *testing.T) {
	ctx := context.Background()
	PONRMgr, err := NewPONResourceManager(ctx, "gpon", "onu", "olt1",
		"etcd", "1:1", "service/voltha")
	if err != nil {
		return
	}
	PONRMgr.KVStore = &db.Backend{
		Client: newMockKvClient(ctx),
	}

	PONRMgr.KVStoreForConfig = &db.Backend{
		Client: newMockKvClient(ctx),
	}
	// create a pool in the range of [1,16]
	StartIndex := uint32(1)
	EndIndex := uint32(16)

	FormatResult, err := PONRMgr.FormatResource(ctx, 1, StartIndex, EndIndex, []uint32{})
	if err != nil {
		t.Error("Failed to format resource", err)
		return
	}

	// Add resource as json in kv store.
	err = PONRMgr.KVStore.Put(ctx, GEMPORT_ID_POOL_PATH, FormatResult)
	if err != nil {
		t.Error("Error in posting data to kv store", GEMPORT_ID_POOL_PATH)
		return
	}

	// get gem port id pool from the kv store
	resource, err := PONRMgr.GetResource(context.Background(), GEMPORT_ID_POOL_PATH)
	if err != nil {
		t.Error("Failed to get resource from gem port id pool", err)
		return
	}
	//try to release an ID whose value is out of the boundaries of the pool and expect false
	released := PONRMgr.ReleaseID(ctx, resource, uint32(EndIndex+1))
	assert.Equal(t, false, released)
}

func TestPONResourceManager_ReserveInvalidID(t *testing.T) {
	ctx := context.Background()
	PONRMgr, err := NewPONResourceManager(ctx, "gpon", "onu", "olt1",
		"etcd", "1:1", "service/voltha")
	if err != nil {
		return
	}
	PONRMgr.KVStore = &db.Backend{
		Client: newMockKvClient(ctx),
	}

	PONRMgr.KVStoreForConfig = &db.Backend{
		Client: newMockKvClient(ctx),
	}
	// create a pool in the range of [1,16]
	StartIndex := uint32(1)
	EndIndex := uint32(16)

	FormatResult, err := PONRMgr.FormatResource(ctx, 1, StartIndex, EndIndex, []uint32{})
	if err != nil {
		t.Error("Failed to format resource", err)
		return
	}

	// Add resource as json in kv store.
	err = PONRMgr.KVStore.Put(ctx, GEMPORT_ID_POOL_PATH, FormatResult)
	if err != nil {
		t.Error("Error in posting data to kv store", GEMPORT_ID_POOL_PATH)
		return
	}
	// get gem port id pool from the kv store
	resource, err := PONRMgr.GetResource(context.Background(), GEMPORT_ID_POOL_PATH)
	if err != nil {
		t.Error("Failed to get resource from gem port id pool", err)
		return
	}
	ByteArray, err := ToByte(resource[POOL])
	if err != nil {
		t.Error(ctx, "Failed to convert resource to byte array")
		return
	}
	Data := bitmap.TSFromData(ByteArray, false)
	if Data == nil {
		t.Error(ctx, "Failed to get resource pool")
		return
	}
	//try to reserve an ID whose value is out of the boundaries of the pool and expect false
	reserved := PONRMgr.reserveID(ctx, Data, StartIndex, EndIndex+1)
	assert.Equal(t, false, reserved)
}
