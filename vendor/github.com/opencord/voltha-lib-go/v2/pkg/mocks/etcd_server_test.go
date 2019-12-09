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

package mocks

import (
	"fmt"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"log"
	"os"
	"testing"
)

var etcdServer *EtcdServer
var client *kvstore.EtcdClient

func setup() {
	clientPort, err := freeport.GetFreePort()
	if err != nil {
		log.Fatal(err)
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		log.Fatal(err)
	}
	etcdServer = StartEtcdServer(MKConfig("voltha.mock.test", clientPort, peerPort, "voltha.lib.mocks.etcd", "error"))
	if etcdServer == nil {
		log.Fatal("Embedded server failed to start")
	}
	clientAddr := fmt.Sprintf("localhost:%d", clientPort)
	client, err = kvstore.NewEtcdClient(clientAddr, 10)
	if err != nil || client == nil {
		etcdServer.Stop()
		log.Fatal("Failed to create an Etcd client")
	}
}

func TestEtcdServerRW(t *testing.T) {
	key := "myKey-1"
	value := "myVal-1"
	err := client.Put(key, value, 10)
	assert.Nil(t, err)
	kvp, err := client.Get(key, 10)
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
	val, err := client.Reserve("transactions", txId, 10)
	assert.Nil(t, err)
	assert.NotNil(t, val)
	assert.Equal(t, val, txId)
}

func shutdown() {
	if client != nil {
		client.Close()
	}
	if etcdServer != nil {
		etcdServer.Stop()
	}
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
