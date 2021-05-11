/*
 * Copyright 2020-present Open Networking Foundation

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

package common

import (
	"context"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	mocks "github.com/opencord/voltha-lib-go/v4/pkg/mocks/kafka"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

const (
	embedEtcdServerHost = "localhost"
	defaultTimeout      = 1
	defaultPathPrefix   = "Prefix"
)

var embedEtcdServerPort int

func init() {

	ctx := context.Background()
	var err error
	embedEtcdServerPort, err = freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get freeport for KvClient")
	}
}

func TestNewAdapterProxy(t *testing.T) {

	var mockKafkaIcProxy = &mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDeviceId"},
		},
	}
	backend := db.NewBackend(context.Background(), "etcd", embedEtcdServerHost+":"+strconv.Itoa(embedEtcdServerPort), defaultTimeout, defaultPathPrefix)
	adapter := NewAdapterProxy(context.Background(), mockKafkaIcProxy, "testCoreTopic", backend)

	assert.NotNil(t, adapter)
}

func TestSendInterAdapterMessage(t *testing.T) {

	var mockKafkaIcProxy = &mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDeviceId"},
		},
	}
	backend := db.NewBackend(context.Background(), "etcd", embedEtcdServerHost+":"+strconv.Itoa(embedEtcdServerPort), defaultTimeout, defaultPathPrefix)

	adapter := NewAdapterProxy(context.Background(), mockKafkaIcProxy, "testCoreTopic", backend)

	adapter.endpointMgr = mocks.NewEndpointManager()

	delGemPortMsg := &ic.InterAdapterDeleteGemPortMessage{UniId: 1, TpPath: "tpPath", GemPortId: 2}

	err := adapter.SendInterAdapterMessage(context.TODO(), delGemPortMsg, ic.InterAdapterMessageType_DELETE_GEM_PORT_REQUEST, "Adapter1", "Adapter2", "testDeviceId", "testProxyDeviceId", "testMessage")

	assert.Nil(t, err)

	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)

	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]

	assert.Equal(t, call.Rpc, "process_inter_adapter_message")
	assert.Equal(t, *call.ToTopic, kafka.Topic{Name: "Adapter2"})
	assert.Equal(t, *call.ReplyToTopic, kafka.Topic{Name: "Adapter1"})
	assert.Equal(t, call.WaitForResponse, true)
	assert.Equal(t, call.Key, "testProxyDeviceId")

	kvArgs := call.KvArgs[0].(*kafka.KVArg)

	adapterMessage := kvArgs.Value.(*ic.InterAdapterMessage)

	assert.Equal(t, adapterMessage.Header.Id, "testMessage")
	assert.Equal(t, adapterMessage.Header.Type, ic.InterAdapterMessageType_DELETE_GEM_PORT_REQUEST)
	assert.Equal(t, adapterMessage.Header.FromTopic, "Adapter1")
	assert.Equal(t, adapterMessage.Header.ToTopic, "Adapter2")
	assert.Equal(t, adapterMessage.Header.ToDeviceId, "testDeviceId")
	assert.Equal(t, adapterMessage.Header.ProxyDeviceId, "testProxyDeviceId")

	assert.Equal(t, kvArgs.Key, "msg")
}

func TestHeaderId(t *testing.T) {

	var mockKafkaIcProxy = &mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDeviceId"},
		},
	}
	backend := db.NewBackend(context.Background(), "etcd", embedEtcdServerHost+":"+strconv.Itoa(embedEtcdServerPort), defaultTimeout, defaultPathPrefix)

	adapter := NewAdapterProxy(context.Background(), mockKafkaIcProxy, "testCoreTopic", backend)

	adapter.endpointMgr = mocks.NewEndpointManager()

	delGemPortMsg := &ic.InterAdapterDeleteGemPortMessage{UniId: 1, TpPath: "tpPath", GemPortId: 2}

	err := adapter.SendInterAdapterMessage(context.TODO(), delGemPortMsg, ic.InterAdapterMessageType_DELETE_GEM_PORT_REQUEST, "Adapter1", "Adapter2", "testDeviceId", "testProxyDeviceId", "")
	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]

	kvArgs := call.KvArgs[0].(*kafka.KVArg)

	adapterMessage := kvArgs.Value.(*ic.InterAdapterMessage)

	assert.Nil(t, err)
	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)
	assert.Len(t, adapterMessage.Header.Id, 36)
}

func TestInvalidProtoMessage(t *testing.T) {

	var mockKafkaIcProxy = &mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDeviceId"},
		},
	}
	backend := db.NewBackend(context.Background(), "etcd", embedEtcdServerHost+":"+strconv.Itoa(embedEtcdServerPort), defaultTimeout, defaultPathPrefix)

	adapter := NewAdapterProxy(context.Background(), mockKafkaIcProxy, "testCoreTopic", backend)

	adapter.endpointMgr = mocks.NewEndpointManager()

	err := adapter.SendInterAdapterMessage(context.TODO(), nil, ic.InterAdapterMessageType_DELETE_GEM_PORT_REQUEST, "Adapter1", "Adapter2", "testDeviceId", "testProxyDeviceId", "testMessage")

	assert.NotNil(t, err)
}
