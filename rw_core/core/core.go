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

package core

import (
	"context"
	"time"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/api"
	"github.com/opencord/voltha-go/rw_core/core/device"
	conf "github.com/opencord/voltha-lib-go/v3/pkg/config"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	grpcserver "github.com/opencord/voltha-lib-go/v3/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc"
)

// Core represent read,write core attributes
type Core struct {
	shutdown context.CancelFunc
	stopped  chan struct{}
}

// NewCore creates instance of rw core
func NewCore(ctx context.Context, id string, cf *config.RWCoreFlags) *Core {
	// If the context has a probe then fetch it and register our services
	if p := probe.GetProbeFromContext(ctx); p != nil {
		p.RegisterService(
			"message-bus",
			"kv-store",
			"adapter-manager",
			"grpc-service",
		)
	}

	// new threads will be given a new cancelable context, so that they can be aborted later when Stop() is called
	shutdownCtx, cancelCtx := context.WithCancel(ctx)

	core := &Core{shutdown: cancelCtx, stopped: make(chan struct{})}
	go core.start(shutdownCtx, id, cf)
	return core
}

func (core *Core) start(ctx context.Context, id string, cf *config.RWCoreFlags) {
	logger.Info("starting-core-services", log.Fields{"coreId": id})

	// deferred functions are used to run cleanup
	// failing partway will stop anything that's been started
	defer close(core.stopped)
	defer core.shutdown()

	logger.Info("Starting RW Core components")

	// setup kv client
	logger.Debugw("create-kv-client", log.Fields{"kvstore": cf.KVStoreType})
	kvClient, err := newKVClient(cf.KVStoreType, cf.KVStoreAddress, cf.KVStoreTimeout)
	if err != nil {
		logger.Fatal(err)
	}
	defer stopKVClient(context.Background(), kvClient)

	// sync logging config with kv store
	cm := conf.NewConfigManager(kvClient, cf.KVStoreType, cf.KVStoreAddress, cf.KVStoreTimeout)
	go conf.StartLogLevelConfigProcessing(cm, ctx)

	backend := &db.Backend{
		Client:    kvClient,
		StoreType: cf.KVStoreType,
		Address:   cf.KVStoreAddress,
		Timeout:   cf.KVStoreTimeout,
		// Configure backend to push Liveness Status at least every (cf.LiveProbeInterval / 2) seconds
		// so as to avoid trigger of Liveness check (due to Liveness timeout) when backend is alive
		LivenessChannelInterval: cf.LiveProbeInterval / 2,
		PathPrefix:              cf.KVStoreDataPrefix,
	}

	// wait until connection to KV Store is up
	if err := waitUntilKVStoreReachableOrMaxTries(ctx, kvClient, cf.MaxConnectionRetries, cf.ConnectionRetryInterval); err != nil {
		logger.Fatal("Unable-to-connect-to-KV-store")
	}
	go monitorKVStoreLiveness(ctx, backend, cf.LiveProbeInterval, cf.NotLiveProbeInterval)

	// create kafka client
	kafkaClient := kafka.NewSaramaClient(
		kafka.Address(cf.KafkaAdapterAddress),
		kafka.ConsumerType(kafka.GroupCustomer),
		kafka.ProducerReturnOnErrors(true),
		kafka.ProducerReturnOnSuccess(true),
		kafka.ProducerMaxRetries(6),
		kafka.NumPartitions(3),
		kafka.ConsumerGroupName(id),
		kafka.ConsumerGroupPrefix(id),
		kafka.AutoCreateTopic(true),
		kafka.ProducerFlushFrequency(5),
		kafka.ProducerRetryBackoff(time.Millisecond*30),
		kafka.LivenessChannelInterval(cf.LiveProbeInterval/2),
	)
	// defer kafkaClient.Stop()

	// create kv path
	dbPath := model.NewDBPath(backend)

	// load adapters & device types while other things are starting
	adapterMgr := adapter.NewAdapterManager(dbPath, id, kafkaClient)
	go adapterMgr.Start(ctx)

	// connect to kafka, then wait until reachable and publisher/consumer created
	// core.kmp must be created before deviceMgr and adapterMgr
	kmp, err := startKafkInterContainerProxy(ctx, kafkaClient, cf.KafkaAdapterAddress, cf.CoreTopic, cf.AffinityRouterTopic, cf.ConnectionRetryInterval)
	if err != nil {
		logger.Warn("Failed to setup kafka connection")
		return
	}
	defer kmp.Stop()
	go monitorKafkaLiveness(ctx, kmp, cf.LiveProbeInterval, cf.NotLiveProbeInterval)

	// create the core of the system, the device managers
	endpointMgr := kafka.NewEndpointManager(backend)
	deviceMgr, logicalDeviceMgr := device.NewManagers(dbPath, adapterMgr, kmp, endpointMgr, cf.CorePairTopic, id, cf.DefaultCoreTimeout)

	// register kafka RPC handler
	registerAdapterRequestHandlers(kmp, deviceMgr, adapterMgr, cf.CoreTopic, cf.CorePairTopic)

	// start gRPC handler
	grpcServer := grpcserver.NewGrpcServer(cf.GrpcAddress, nil, false, probe.GetProbeFromContext(ctx))
	go startGRPCService(ctx, grpcServer, api.NewNBIHandler(deviceMgr, logicalDeviceMgr, adapterMgr))
	defer grpcServer.Stop()

	// wait for core to be stopped, via Stop() or context cancellation, before running deferred functions
	<-ctx.Done()
}

// Stop brings down core services
func (core *Core) Stop() {
	core.shutdown()
	<-core.stopped
}

// startGRPCService creates the grpc service handlers, registers it to the grpc server and starts the server
func startGRPCService(ctx context.Context, server *grpcserver.GrpcServer, handler voltha.VolthaServiceServer) {
	logger.Info("grpc-server-created")

	server.AddService(func(gs *grpc.Server) { voltha.RegisterVolthaServiceServer(gs, handler) })
	logger.Info("grpc-service-added")

	probe.UpdateStatusFromContext(ctx, "grpc-service", probe.ServiceStatusRunning)
	logger.Info("grpc-server-started")
	// Note that there is a small window here in which the core could return its status as ready,
	// when it really isn't.  This is unlikely to cause issues, as the delay is incredibly short.
	server.Start(ctx)
	probe.UpdateStatusFromContext(ctx, "grpc-service", probe.ServiceStatusStopped)
}
