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
	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	grpcserver "github.com/opencord/voltha-lib-go/v2/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

type roCore struct {
	kvClient    kvstore.Client
	config      *config.ROCoreFlags
	halted      bool
	exitChannel chan int
	grpcServer  *grpcserver.GrpcServer
	core        *Core
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

func MakeTestNewCore() (*config.ROCoreFlags, *roCore) {

	freePort, errP := freeport.GetFreePort()
	if errP == nil {
		freePortStr := strconv.Itoa(freePort)

		roCoreFlgs := config.NewROCoreFlags()
		roC := newROCore(roCoreFlgs)
		if (roC != nil) && (roCoreFlgs != nil) {
			addr := "127.0.0.1" + ":" + freePortStr
			cli, err := newKVClient("etcd", addr, 5)
			if err == nil {
				roC.kvClient = cli
				return roCoreFlgs, roC
			}
		}
	}
	return nil, nil
}

func TestNewCore(t *testing.T) {

	roCoreFlgs, roC := MakeTestNewCore()
	assert.NotNil(t, roCoreFlgs)
	assert.NotNil(t, roC)
	core := NewCore("ro_core", roCoreFlgs, roC.kvClient)
	assert.NotNil(t, core)
}

func TestNewCoreStartStop(t *testing.T) {

	var ctx context.Context

	roCoreFlgs, roC := MakeTestNewCore()
	assert.NotNil(t, roCoreFlgs)
	assert.NotNil(t, roC)
	core := NewCore("ro_core", roCoreFlgs, roC.kvClient)
	assert.NotNil(t, core)

	core.Start(ctx)
	core.Stop(ctx)
}
