/*
 * Copyright 2019-present Open Networking Foundation

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

package db

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	mocks "github.com/opencord/voltha-lib-go/v4/pkg/mocks/etcd"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	embedEtcdServerHost = "localhost"
	defaultTimeout      = 1 * time.Second
	defaultPathPrefix   = "Prefix"
)

var (
	embedEtcdServerPort int
	dummyEtcdServerPort int
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	embedEtcdServerPort, err = freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	dummyEtcdServerPort, err = freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	etcdServer := mocks.StartEtcdServer(ctx, mocks.MKConfig(ctx, "voltha.db.test", embedEtcdServerPort, peerPort, "voltha.lib.db", "error"))
	res := m.Run()

	etcdServer.Stop(ctx)
	os.Exit(res)
}

func provisionBackendWithEmbeddedEtcdServer(t *testing.T) *Backend {
	ctx := context.Background()
	backend := NewBackend(ctx, "etcd", embedEtcdServerHost+":"+strconv.Itoa(embedEtcdServerPort), defaultTimeout, defaultPathPrefix)
	assert.NotNil(t, backend)
	assert.NotNil(t, backend.Client)
	return backend
}

func provisionBackendWithDummyEtcdServer(t *testing.T) *Backend {
	backend := NewBackend(context.Background(), "etcd", embedEtcdServerHost+":"+strconv.Itoa(dummyEtcdServerPort), defaultTimeout, defaultPathPrefix)
	assert.NotNil(t, backend)
	assert.NotNil(t, backend.Client)
	return backend
}

// Create instance using Etcd Kvstore
func TestNewBackend_EtcdKvStore(t *testing.T) {
	address := embedEtcdServerHost + ":" + strconv.Itoa(embedEtcdServerPort)
	backend := NewBackend(context.Background(), "etcd", address, defaultTimeout, defaultPathPrefix)

	// Verify all attributes of backend have got set correctly
	assert.NotNil(t, backend)
	assert.NotNil(t, backend.Client)
	assert.Equal(t, backend.StoreType, "etcd")
	assert.Equal(t, backend.Address, address)
	assert.Equal(t, backend.Timeout, defaultTimeout)
	assert.Equal(t, backend.PathPrefix, defaultPathPrefix)
	assert.Equal(t, backend.alive, false) // backend is not alive at start
	assert.Nil(t, backend.liveness)       // no liveness channel is created at start
	assert.Equal(t, backend.LivenessChannelInterval, DefaultLivenessChannelInterval)
}

// Create instance using Invalid Kvstore; instance creation should fail
func TestNewBackend_InvalidKvstore(t *testing.T) {
	backend := NewBackend(context.Background(), "unknown", embedEtcdServerHost+":"+strconv.Itoa(embedEtcdServerPort), defaultTimeout, defaultPathPrefix)

	assert.NotNil(t, backend)
	assert.Nil(t, backend.Client)
}

func TestMakePath(t *testing.T) {
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	path := backend.makePath(context.Background(), "Suffix")
	assert.Equal(t, defaultPathPrefix+"/Suffix", path)
}

// Liveness Check against Embedded Etcd Server should return alive state
func TestPerformLivenessCheck_EmbeddedEtcdServer(t *testing.T) {
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	alive := backend.PerformLivenessCheck(ctx)
	assert.True(t, alive)
}

// Liveness Check against Dummy Etcd Server should return not-alive state
func TestPerformLivenessCheck_DummyEtcdServer(t *testing.T) {
	backend := provisionBackendWithDummyEtcdServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	alive := backend.PerformLivenessCheck(ctx)
	assert.False(t, alive)
}

// Enabling Liveness Channel before First Liveness Check
func TestEnableLivenessChannel_EmbeddedEtcdServer_BeforeLivenessCheck(t *testing.T) {
	backend := provisionBackendWithEmbeddedEtcdServer(t)

	alive := backend.EnableLivenessChannel(context.Background())
	assert.NotNil(t, alive)
	assert.Equal(t, 1, len(alive))
	assert.Equal(t, false, <-alive)
	assert.NotNil(t, backend.liveness)
}

// Enabling Liveness Channel after First Liveness Check
func TestEnableLivenessChannel_EmbeddedEtcdServer_AfterLivenessCheck(t *testing.T) {
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	backend.PerformLivenessCheck(ctx)

	alive := backend.EnableLivenessChannel(ctx)
	assert.NotNil(t, alive)
	assert.Equal(t, 1, len(alive))
	assert.Equal(t, true, <-alive)
	assert.NotNil(t, backend.liveness)
}

// Update Liveness with alive status change
func TestUpdateLiveness_AliveStatusChange(t *testing.T) {
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	// Enable Liveness Channel and verify initial state is not-alive
	aliveState := backend.EnableLivenessChannel(context.Background())
	assert.NotNil(t, aliveState)
	assert.Equal(t, 1, len(backend.liveness))
	assert.Equal(t, false, <-backend.liveness)
	lastUpdateTime := backend.lastLivenessTime

	// Update with changed alive state. Verify alive state push & liveness time update
	backend.updateLiveness(context.Background(), true)
	assert.Equal(t, 1, len(backend.liveness))
	assert.Equal(t, true, <-backend.liveness)
	assert.NotEqual(t, lastUpdateTime, backend.lastLivenessTime)
}

// Update Liveness with same alive status reporting
func TestUpdateLiveness_AliveStatusUnchanged(t *testing.T) {
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	// Enable Liveness Channel and verify initial state is not-alive
	aliveState := backend.EnableLivenessChannel(context.Background())
	assert.NotNil(t, aliveState)
	assert.Equal(t, false, <-backend.liveness)
	lastUpdateTime := backend.lastLivenessTime

	// Update with same alive state. Verify no further alive state push
	backend.updateLiveness(context.Background(), false)
	assert.Equal(t, 0, len(backend.liveness))
	assert.Equal(t, lastUpdateTime, backend.lastLivenessTime)

	// Now set lastUpdateTime 10 min back and push again
	tenMinDuration, _ := time.ParseDuration("10m")
	backend.lastLivenessTime = time.Now().Add(-tenMinDuration)
	lastUpdateTime = backend.lastLivenessTime

	backend.updateLiveness(context.Background(), false)
	assert.Equal(t, 1, len(backend.liveness))
	assert.Equal(t, false, <-backend.liveness)
	assert.NotEqual(t, lastUpdateTime, backend.lastLivenessTime)
}

func TestIsErrorIndicatingAliveKvstore(t *testing.T) {
	tests := []struct {
		name string
		arg  error
		want bool
	}{
		{"No Error", nil, true},
		{"Request Canceled", context.Canceled, true},
		{"Request Timeout", context.DeadlineExceeded, false},
		{"Etcd Error - InvalidArgument", status.New(codes.InvalidArgument, "").Err(), true},
		{"Etcd Error - DeadlineExceeded", status.New(codes.DeadlineExceeded, "").Err(), false},
		{"Etcd Error - Unavailable", status.New(codes.Unavailable, "").Err(), false},
		{"Etcd Error - DataLoss", status.New(codes.DataLoss, "").Err(), false},
		{"Etcd Error - NotFound", status.New(codes.NotFound, "").Err(), true},
		{"Etcd Error - PermissionDenied ", status.New(codes.PermissionDenied, "").Err(), true},
		{"Etcd Error - FailedPrecondition  ", status.New(codes.FailedPrecondition, "").Err(), true},
	}

	backend := provisionBackendWithEmbeddedEtcdServer(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if backend.isErrorIndicatingAliveKvstore(context.Background(), tt.arg) != tt.want {
				t.Errorf("isErrorIndicatingAliveKvstore failed for %s: expected %t but got %t", tt.name, tt.want, !tt.want)
			}
		})
	}
}

func TestPut_EmbeddedEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	err := backend.Put(ctx, "key1", []uint8("value1"))
	assert.Nil(t, err)

	// Assert alive state has become true
	assert.True(t, backend.alive)

	// Assert that kvstore has this value stored
	kvpair, err := backend.Get(ctx, "key1")
	assert.Nil(t, err)
	assert.NotNil(t, kvpair)
	assert.Equal(t, defaultPathPrefix+"/key1", kvpair.Key)
	assert.Equal(t, []uint8("value1"), kvpair.Value)

	// Assert that Put overrides the Value
	err = backend.Put(ctx, "key1", []uint8("value11"))
	assert.Nil(t, err)
	kvpair, err = backend.Get(ctx, "key1")
	assert.Nil(t, err)
	assert.NotNil(t, kvpair)
	assert.Equal(t, defaultPathPrefix+"/key1", kvpair.Key)
	assert.Equal(t, []uint8("value11"), kvpair.Value)
}

// Put operation should fail against Dummy Non-existent Etcd Server
func TestPut_DummyEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithDummyEtcdServer(t)
	err := backend.Put(ctx, "key1", []uint8("value1"))
	assert.NotNil(t, err)

	// Assert alive state is still false
	assert.False(t, backend.alive)
}

// Test Get for existing and non-existing key
func TestGet_EmbeddedEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	err := backend.Put(ctx, "key2", []uint8("value2"))
	assert.Nil(t, err)

	// Assert alive state has become true
	assert.True(t, backend.alive)

	// Assert that kvstore has this key stored
	kvpair, err := backend.Get(ctx, "key2")
	assert.NotNil(t, kvpair)
	assert.Nil(t, err)
	assert.Equal(t, defaultPathPrefix+"/key2", kvpair.Key)
	assert.Equal(t, []uint8("value2"), kvpair.Value)

	// Assert that Get works fine for absent key3
	kvpair, err = backend.Get(ctx, "key3")
	assert.Nil(t, err) // no error as lookup is successful
	assert.Nil(t, kvpair)
}

// Get operation should fail against Dummy Non-existent Etcd Server
func TestGet_DummyEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithDummyEtcdServer(t)
	kvpair, err := backend.Get(ctx, "key2")
	assert.NotNil(t, err)
	assert.Nil(t, kvpair)

	// Assert alive state is still false
	assert.False(t, backend.alive)
}

// Test Delete for existing and non-existing key
func TestDelete_EmbeddedEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	err := backend.Put(ctx, "key3", []uint8("value3"))
	assert.Nil(t, err)

	// Assert alive state has become true
	assert.True(t, backend.alive)

	// Assert that kvstore has this key stored
	kvpair, err := backend.Get(ctx, "key3")
	assert.Nil(t, err)
	assert.NotNil(t, kvpair)

	// Delete and Assert that key has been removed
	err = backend.Delete(ctx, "key3")
	assert.Nil(t, err)
	kvpair, err = backend.Get(ctx, "key3")
	assert.Nil(t, err)
	assert.Nil(t, kvpair)

	// Assert that Delete silently ignores absent key3
	err = backend.Delete(ctx, "key3")
	assert.Nil(t, err)
}

// Delete operation should fail against Dummy Non-existent Etcd Server
func TestDelete_DummyEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithDummyEtcdServer(t)
	err := backend.Delete(ctx, "key3")
	assert.NotNil(t, err)

	// Assert alive state is still false
	assert.False(t, backend.alive)
}

// Test List for series of values under a key path
func TestList_EmbeddedEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key41 := "key4/subkey1"
	key42 := "key4/subkey2"

	backend := provisionBackendWithEmbeddedEtcdServer(t)
	err := backend.Put(ctx, key41, []uint8("value4-1"))
	assert.Nil(t, err)
	err = backend.Put(ctx, key42, []uint8("value4-2"))
	assert.Nil(t, err)

	// Assert alive state has become true
	assert.True(t, backend.alive)

	// Assert that Get does not retrieve these Subkeys
	kvpair, err := backend.Get(ctx, "key4")
	assert.Nil(t, kvpair)
	assert.Nil(t, err)

	// Assert that List operation retrieves these Child Keys
	kvmap, err := backend.List(ctx, "key4")
	assert.NotNil(t, kvmap)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(kvmap))
	fullkey41 := defaultPathPrefix + "/" + key41
	fullkey42 := defaultPathPrefix + "/" + key42
	assert.Equal(t, fullkey41, kvmap[fullkey41].Key)
	assert.Equal(t, []uint8("value4-1"), kvmap[fullkey41].Value)
	assert.Equal(t, fullkey42, kvmap[fullkey42].Key)
	assert.Equal(t, []uint8("value4-2"), kvmap[fullkey42].Value)
}

// List operation should fail against Dummy Non-existent Etcd Server
func TestList_DummyEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithDummyEtcdServer(t)
	kvmap, err := backend.List(ctx, "key4")
	assert.Nil(t, kvmap)
	assert.NotNil(t, err)

	// Assert alive state is still false
	assert.False(t, backend.alive)
}

// Test Create and Delete Watch for Embedded Etcd Server
func TestCreateWatch_EmbeddedEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	eventChan := backend.CreateWatch(ctx, "key5", false)
	assert.NotNil(t, eventChan)
	assert.Equal(t, 0, len(eventChan))

	// Assert this method does not change alive state
	assert.False(t, backend.alive)

	// Put a value for watched key and event should appear
	err := backend.Put(ctx, "key5", []uint8("value5"))
	assert.Nil(t, err)
	time.Sleep(time.Millisecond * 100)
	assert.Equal(t, 1, len(eventChan))

	backend.DeleteWatch(context.Background(), "key5", eventChan)
}

// Test Create and Delete Watch with prefix for Embedded Etcd Server
func TestCreateWatch_With_Prefix_EmbeddedEtcdServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend := provisionBackendWithEmbeddedEtcdServer(t)
	eventChan := backend.CreateWatch(ctx, "key6", true)
	assert.NotNil(t, eventChan)
	assert.Equal(t, 0, len(eventChan))

	// Assert this method does not change alive state
	assert.False(t, backend.alive)

	// Put a value for watched key and event should appear
	err := backend.Put(ctx, "key6/is-a-prefix", []uint8("value5"))
	assert.Nil(t, err)
	time.Sleep(time.Millisecond * 100)
	assert.Equal(t, 1, len(eventChan))

	backend.DeleteWatch(context.Background(), "key6", eventChan)
}
