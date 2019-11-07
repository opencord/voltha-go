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
package core

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/mocks"
	ic "github.com/opencord/voltha-protos/v2/go/inter_container"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

type roCore struct {
	kvClient    kvstore.Client
	config      *config.ROCoreFlags
	halted      bool
	exitChannel chan int
	//For test
	receiverChannels []<-chan *ic.InterContainerMessage
}

func newROCore(cf *config.ROCoreFlags) *roCore {
	var roCoreV roCore
	roCoreV.config = cf
	roCoreV.halted = false
	roCoreV.exitChannel = make(chan int, 1)
	roCoreV.receiverChannels = make([]<-chan *ic.InterContainerMessage, 0)
	return &roCoreV
}

func newKVClient(storeType string, address string, timeout int) (kvstore.Client, error) {

	log.Infow("kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("unsupported-kv-store")
}

func makeTestNewCore() (*config.ROCoreFlags, *roCore) {

	clientPort, err := freeport.GetFreePort()
	if err == nil {
		peerPort, err := freeport.GetFreePort()
		if err != nil {
			log.Fatal(err)
		}
		etcdServer := mocks.StartEtcdServer(mocks.MKConfig("voltha.mock.test", clientPort, peerPort, "voltha.lib.mocks.etcd", "error"))
		if etcdServer == nil {
			log.Fatal("Embedded server failed to start")
		}
		clientAddr := fmt.Sprintf("localhost:%d", clientPort)

		roCoreFlgs := config.NewROCoreFlags()
		roC := newROCore(roCoreFlgs)
		if (roC != nil) && (roCoreFlgs != nil) {
			cli, err := newKVClient("etcd", clientAddr, 5)
			if err == nil {
				roC.kvClient = cli
				return roCoreFlgs, roC
			}
			etcdServer.Stop()
			log.Fatal("Failed to create an Etcd client")
		}
	}
	return nil, nil
}

func TestNewCore(t *testing.T) {

	var ctx context.Context

	roCoreFlgs, roC := makeTestNewCore()
	assert.NotNil(t, roCoreFlgs)
	assert.NotNil(t, roC)
	core := NewCore(ctx, "ro_core", roCoreFlgs, roC.kvClient)
	assert.NotNil(t, core)
}

func TestNewCoreStartStop(t *testing.T) {

	var ctx context.Context

	roCoreFlgs, roC := makeTestNewCore()
	assert.NotNil(t, roCoreFlgs)
	assert.NotNil(t, roC)
	core := NewCore(ctx, "ro_core", roCoreFlgs, roC.kvClient)
	assert.NotNil(t, core)

	err := core.Start(ctx)
	assert.Nil(t, err)
	core.Stop(ctx)
}
