/*
 * Copyright 2021-present Open Networking Foundation

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
package kvstore

import (
	"context"
	"github.com/opencord/voltha-lib-go/v6/pkg/log"
	mocks "github.com/opencord/voltha-lib-go/v6/pkg/mocks/etcd"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	embedEtcdServerHost = "localhost"
	defaultTimeout      = 1 * time.Second
)

var (
	embedEtcdServerPort int
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	embedEtcdServerPort, err = freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	etcdServer := mocks.StartEtcdServer(ctx, mocks.MKConfig(ctx,
		"voltha.db.kvstore.test",
		embedEtcdServerPort,
		peerPort,
		"voltha.lib.db.kvstore",
		"error"))
	res := m.Run()

	etcdServer.Stop(ctx)
	os.Exit(res)
}

func TestNewRoundRobinEtcdClientAllocator(t *testing.T) {
	address := embedEtcdServerHost + ":" + strconv.Itoa(embedEtcdServerPort)
	capacity := 20
	maxUsage := 10

	pool, err := NewRoundRobinEtcdClientAllocator([]string{address}, defaultTimeout, capacity, maxUsage, log.ErrorLevel)
	assert.NotNil(t, pool)
	assert.Nil(t, err)
}

func TestRoundRobin_Get_Put(t *testing.T) {
	address := embedEtcdServerHost + ":" + strconv.Itoa(embedEtcdServerPort)
	capacity := 20
	maxUsage := 10

	pool, err := NewRoundRobinEtcdClientAllocator([]string{address}, defaultTimeout, capacity, maxUsage, log.ErrorLevel)
	assert.NotNil(t, pool)
	assert.Nil(t, err)

	// Verify we can obtain the expected number of clients with no errors or waiting time
	var wg sync.WaitGroup
	for i := 0; i < capacity*maxUsage; i++ {
		wg.Add(1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			c, err := pool.Get(ctx)
			assert.NotNil(t, c)
			assert.Nil(t, err)
			time.Sleep(5 * time.Second)
			pool.Put(c)
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestRoundRobin_Get_Put_various_capacity(t *testing.T) {
	// Test single client with 1 concurrent usage
	getPutVaryingCapacity(t, 1, 1)

	// Test single client with multiple concurrent usage
	getPutVaryingCapacity(t, 1, 100)

	// Test multiple clients with single concurrent usage
	getPutVaryingCapacity(t, 10, 1)

	// Test multiple clients with multiple concurrent usage
	getPutVaryingCapacity(t, 10, 10)
}

func getPutVaryingCapacity(t *testing.T, capacity, maxUsage int) {
	address := embedEtcdServerHost + ":" + strconv.Itoa(embedEtcdServerPort)

	pool, err := NewRoundRobinEtcdClientAllocator([]string{address}, defaultTimeout, capacity, maxUsage, log.ErrorLevel)
	assert.NotNil(t, pool)
	assert.Nil(t, err)

	// Verify we can obtain the expected number of clients with no errors or waiting time
	var wg sync.WaitGroup
	totalSize := capacity * maxUsage
	ch := make(chan struct{})
	for i := 0; i < totalSize; i++ {
		wg.Add(1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			c, err := pool.Get(ctx)
			assert.NotNil(t, c)
			assert.Nil(t, err)
			// Inform the waiting loop that a client has been allocated
			ch <- struct{}{}
			// Keep the client for 5s and then return it to the pool
			time.Sleep(5 * time.Second)
			pool.Put(c)
			wg.Done()
		}()
	}

	// Wait until all clients are allocated
	allocated := 0
	for range ch {
		allocated++
		if allocated == totalSize {
			break
		}
	}

	// Try to get above capacity/usage with low timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	c, err := pool.Get(ctx)
	assert.NotNil(t, err)
	assert.Nil(t, c)
	cancel()

	// Try to get above capacity/usage with longer timeout
	ctx, cancel = context.WithTimeout(context.Background(), 6*time.Second)
	c, err = pool.Get(ctx)
	assert.NotNil(t, c)
	assert.Nil(t, err)
	pool.Put(c)
	cancel()

	wg.Wait()

	// Close the connection
	pool.Close(context.Background())
}

func TestRoundRobin_Close_various_capacity(t *testing.T) {
	// Test single client with 1 concurrent usage
	closeWithVaryingCapacity(t, 1, 1)

	// Test single client with multiple concurrent usage
	closeWithVaryingCapacity(t, 1, 100)

	// Test multiple clients with single concurrent usage
	closeWithVaryingCapacity(t, 10, 1)

	// Test multiple clients with multiple concurrent usage
	closeWithVaryingCapacity(t, 10, 10)
}

func closeWithVaryingCapacity(t *testing.T, capacity, maxUsage int) {
	address := embedEtcdServerHost + ":" + strconv.Itoa(embedEtcdServerPort)

	pool, err := NewRoundRobinEtcdClientAllocator([]string{address}, defaultTimeout, capacity, maxUsage, log.ErrorLevel)
	assert.NotNil(t, pool)
	assert.Nil(t, err)

	// Verify we can obtain the expected number of clients with no errors or waiting time
	var wg sync.WaitGroup
	totalSize := capacity * maxUsage
	ch := make(chan struct{})
	for i := 0; i < totalSize; i++ {
		wg.Add(1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			c, err := pool.Get(ctx)
			assert.NotNil(t, c)
			assert.Nil(t, err)
			// Inform the waiting loop that a client has been allocated
			ch <- struct{}{}
			// Keep the client for 5s and then return it to the pool
			time.Sleep(5 * time.Second)
			pool.Put(c)
			wg.Done()
		}()
	}

	// Wait until all clients are allocated
	allocated := 0
	for range ch {
		allocated++
		if allocated == totalSize {
			break
		}
	}
	// Try to get above capacity/usage with longer timeout
	wg.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		c, err := pool.Get(ctx)
		assert.NotNil(t, err)
		expected := err.Error() == "pool-is-closing" || err.Error() == "stop-waiting-pool-is-closing"
		assert.True(t, expected)
		assert.Nil(t, c)
		cancel()
		wg.Done()
	}()

	// Invoke close on the pool
	wg.Add(1)
	go func() {
		pool.Close(context.Background())
		wg.Done()
	}()

	// Try to get new client and ensure we can't get one
	c, err := pool.Get(context.Background())
	assert.NotNil(t, err)
	expected := err.Error() == "pool-is-closing" || err.Error() == "stop-waiting-pool-is-closing"
	assert.True(t, expected)
	assert.Nil(t, c)

	wg.Wait()
}
