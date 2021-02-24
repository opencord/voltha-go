/*
 * Copyright 2019-present Open Networking Foundation
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

package etcd

import (
	"context"
	"fmt"
	"github.com/opencord/voltha-lib-go/v4/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
)

var etcdServer *EtcdServer
var client *kvstore.EtcdClient

func setup() {
	ctx := context.Background()
	clientPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, err)
	}
	etcdServer = StartEtcdServer(ctx, MKConfig(ctx, "voltha.mock.test", clientPort, peerPort, "voltha.lib.mocks.etcd", "error"))
	if etcdServer == nil {
		logger.Fatal(ctx, "Embedded server failed to start")
	}
	clientAddr := fmt.Sprintf("localhost:%d", clientPort)
	client, err = kvstore.NewEtcdClient(ctx, clientAddr, 10*time.Second, log.WarnLevel)
	if err != nil || client == nil {
		etcdServer.Stop(ctx)
		logger.Fatal(ctx, "Failed to create an Etcd client")
	}
}

func TestEtcdServerRW(t *testing.T) {
	key := "myKey-1"
	value := "myVal-1"
	err := client.Put(context.Background(), key, value)
	assert.Nil(t, err)
	kvp, err := client.Get(context.Background(), key)
	assert.Nil(t, err)
	assert.NotNil(t, kvp)
	assert.Equal(t, kvp.Key, key)
	val, err := kvstore.ToString(kvp.Value)
	assert.Nil(t, err)
	assert.Equal(t, value, val)
}

func TestEtcdServerReserve(t *testing.T) {
	assert.NotNil(t, client)
	txId := "tnxId-1"
	val, err := client.Reserve(context.Background(), "transactions", txId, 10)
	assert.Nil(t, err)
	assert.NotNil(t, val)
	assert.Equal(t, val, txId)
}

func shutdown() {
	if client != nil {
		client.Close(context.Background())
	}
	if etcdServer != nil {
		etcdServer.Stop(context.Background())
	}
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
