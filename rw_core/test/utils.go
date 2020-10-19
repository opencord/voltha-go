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

// Package core Common Logger initialization
package test

import (
	"context"
	"testing"

	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v4/pkg/adapters"
	com "github.com/opencord/voltha-lib-go/v4/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v4/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	mock_etcd "github.com/opencord/voltha-lib-go/v4/pkg/mocks/etcd"
	"github.com/opencord/voltha-lib-go/v4/pkg/version"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	OltAdapter = iota
	OnuAdapter
)

//CreateMockAdapter creates mock OLT and ONU adapters
func CreateMockAdapter(ctx context.Context, adapterType int, kafkaClient kafka.Client, coreInstanceID string, coreName string, adapterName string) (adapters.IAdapter, error) {
	var err error
	var adapter adapters.IAdapter
	adapterKafkaICProxy := kafka.NewInterContainerProxy(
		kafka.MsgClient(kafkaClient),
		kafka.DefaultTopic(&kafka.Topic{Name: adapterName}))
	adapterCoreProxy := com.NewCoreProxy(ctx, adapterKafkaICProxy, adapterName, coreName)
	var adapterReqHandler *com.RequestHandlerProxy
	switch adapterType {
	case OltAdapter:
		adapter = cm.NewOLTAdapter(ctx, adapterCoreProxy)
	case OnuAdapter:
		adapter = cm.NewONUAdapter(ctx, adapterCoreProxy)
	default:
		logger.Fatalf(ctx, "invalid-adapter-type-%d", adapterType)
	}
	adapterReqHandler = com.NewRequestHandlerProxy(coreInstanceID, adapter, adapterCoreProxy)

	if err = adapterKafkaICProxy.Start(ctx); err != nil {
		logger.Errorw(ctx, "Failure-starting-adapter-intercontainerProxy", log.Fields{"error": err})
		return nil, err
	}
	if err = adapterKafkaICProxy.SubscribeWithRequestHandlerInterface(ctx, kafka.Topic{Name: adapterName}, adapterReqHandler); err != nil {
		logger.Errorw(ctx, "Failure-to-subscribe-onu-request-handler", log.Fields{"error": err})
		return nil, err
	}
	return adapter, nil
}

//CreateAndregisterAdapters creates mock ONU and OLT adapters and egisters them to rw-core
func CreateAndregisterAdapters(ctx context.Context, t *testing.T, kClient kafka.Client, coreInstanceID string, oltAdapterName string, onuAdapterName string, adapterMgr *adapter.Manager) (*cm.OLTAdapter, *cm.ONUAdapter) {
	// Setup the mock OLT adapter
	oltAdapter, err := CreateMockAdapter(ctx, OltAdapter, kClient, coreInstanceID, "rw_core", oltAdapterName)
	assert.Nil(t, err)
	assert.NotNil(t, oltAdapter)

	//	Register the adapter
	registrationData := &voltha.Adapter{
		Id:             oltAdapterName,
		Vendor:         "Voltha-olt",
		Version:        version.VersionInfo.Version,
		Type:           oltAdapterName,
		CurrentReplica: 1,
		TotalReplicas:  1,
		Endpoint:       oltAdapterName,
	}
	types := []*voltha.DeviceType{{Id: oltAdapterName, Adapter: oltAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes := &voltha.DeviceTypes{Items: types}
	if _, err := adapterMgr.RegisterAdapter(ctx, registrationData, deviceTypes); err != nil {
		logger.Errorw(ctx, "failed-to-register-adapter", log.Fields{"error": err})
		assert.NotNil(t, err)
	}

	// Setup the mock ONU adapter
	onuAdapter, err := CreateMockAdapter(ctx, OnuAdapter, kClient, coreInstanceID, "rw_core", onuAdapterName)

	assert.Nil(t, err)
	assert.NotNil(t, onuAdapter)
	//	Register the adapter
	registrationData = &voltha.Adapter{
		Id:             onuAdapterName,
		Vendor:         "Voltha-onu",
		Version:        version.VersionInfo.Version,
		Type:           onuAdapterName,
		CurrentReplica: 1,
		TotalReplicas:  1,
		Endpoint:       onuAdapterName,
	}
	types = []*voltha.DeviceType{{Id: onuAdapterName, Adapter: onuAdapterName, AcceptsAddRemoveFlowUpdates: true}}
	deviceTypes = &voltha.DeviceTypes{Items: types}
	if _, err := adapterMgr.RegisterAdapter(ctx, registrationData, deviceTypes); err != nil {
		logger.Errorw(ctx, "failed-to-register-adapter", log.Fields{"error": err})
		assert.NotNil(t, err)
	}
	return oltAdapter.(*cm.OLTAdapter), onuAdapter.(*cm.ONUAdapter)
}

//StartEmbeddedEtcdServer creates and starts an Embedded etcd server locally.
func StartEmbeddedEtcdServer(ctx context.Context, configName, storageDir, logLevel string) (*mock_etcd.EtcdServer, int, error) {
	kvClientPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	etcdServer := mock_etcd.StartEtcdServer(ctx, mock_etcd.MKConfig(ctx, configName, kvClientPort, peerPort, storageDir, logLevel))
	if etcdServer == nil {
		return nil, 0, status.Error(codes.Internal, "Embedded server failed to start")
	}
	return etcdServer, kvClientPort, nil
}

//StopEmbeddedEtcdServer stops the embedded etcd server
func StopEmbeddedEtcdServer(ctx context.Context, server *mock_etcd.EtcdServer) {
	if server != nil {
		server.Stop(ctx)
	}
}

//SetupKVClient creates a new etcd client
func SetupKVClient(ctx context.Context, cf *config.RWCoreFlags, coreInstanceID string) kvstore.Client {
	client, err := kvstore.NewEtcdClient(ctx, cf.KVStoreAddress, cf.KVStoreTimeout, log.FatalLevel)
	if err != nil {
		panic("no kv client")
	}
	return client
}
