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
	conf "github.com/opencord/voltha-lib-go/v6/pkg/config"
	"github.com/opencord/voltha-lib-go/v6/pkg/events"
	grpcserver "github.com/opencord/voltha-lib-go/v6/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v6/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v6/pkg/log"
	"github.com/opencord/voltha-lib-go/v6/pkg/probe"
	"github.com/opencord/voltha-protos/v4/go/core"
	"github.com/opencord/voltha-protos/v4/go/extension"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc"
)

// Core represent read,write core attributes
type Core struct {
	Shutdown    context.CancelFunc
	Stopped     chan struct{}
	KafkaClient kafka.Client
}

const (
	clusterMessagingService = "cluster-message-service"
	grpcService             = "grpc-service"
	adapterService          = "adapter-service"
	kvService               = "kv-service"
	deviceService           = "device-service"
	logicalDeviceService    = "logical-device-service"
)

// NewCore creates instance of rw core
func NewCore(ctx context.Context, id string, cf *config.RWCoreFlags) (*Core, context.Context) {
	// If the context has a probe then fetch it and register our services
	if p := probe.GetProbeFromContext(ctx); p != nil {
		p.RegisterService(
			ctx,
			kvService,
			adapterService,
			grpcService,
			clusterMessagingService,
			deviceService,
			logicalDeviceService,
		)
	}

	// create kafka client for events
	KafkaClient := kafka.NewSaramaClient(
		kafka.Address(cf.KafkaClusterAddress),
		kafka.ProducerReturnOnErrors(true),
		kafka.ProducerReturnOnSuccess(true),
		kafka.ProducerMaxRetries(6),
		kafka.ProducerRetryBackoff(time.Millisecond*30),
		kafka.AutoCreateTopic(true),
		kafka.MetadatMaxRetries(15),
	)

	// new threads will be given a new cancelable context, so that they can be aborted later when Stop() is called
	shutdownCtx, cancelCtx := context.WithCancel(ctx)

	rwCore := &Core{Shutdown: cancelCtx, Stopped: make(chan struct{}), KafkaClient: KafkaClient}
	return rwCore, shutdownCtx
}

func (core *Core) Start(ctx context.Context, id string, cf *config.RWCoreFlags) {
	logger.Info(ctx, "starting-core-services", log.Fields{"coreId": id})

	// deferred functions are used to run cleanup
	// failing partway will stop anything that's been started
	defer close(core.Stopped)
	defer core.Shutdown()

	logger.Info(ctx, "starting-rw-core-components")

	// setup kv client
	logger.Debugw(ctx, "create-kv-client", log.Fields{"kvstore": cf.KVStoreType})
	kvClient, err := newKVClient(ctx, cf.KVStoreType, cf.KVStoreAddress, cf.KVStoreTimeout)
	if err != nil {
		logger.Fatal(ctx, err)
	}
	defer stopKVClient(log.WithSpanFromContext(context.Background(), ctx), kvClient)

	// sync logging config with kv store
	cm := conf.NewConfigManager(ctx, kvClient, cf.KVStoreType, cf.KVStoreAddress, cf.KVStoreTimeout)
	go conf.StartLogLevelConfigProcessing(cm, ctx)
	go conf.StartLogFeaturesConfigProcessing(cm, ctx)

	backend := cm.Backend
	backend.LivenessChannelInterval = cf.LiveProbeInterval / 2

	// wait until connection to KV Store is up
	if err := waitUntilKVStoreReachableOrMaxTries(ctx, kvClient, cf.MaxConnectionRetries, cf.ConnectionRetryInterval, kvService); err != nil {
		logger.Fatal(ctx, "unable-to-connect-to-kv-store")
	}
	go monitorKVStoreLiveness(ctx, backend, kvService, cf.LiveProbeInterval, cf.NotLiveProbeInterval)

	// Start kafka communications and artefacts
	if err := kafka.StartAndWaitUntilKafkaConnectionIsUp(ctx, core.KafkaClient, cf.ConnectionRetryInterval, clusterMessagingService); err != nil {
		logger.Fatal(ctx, "unable-to-connect-to-kafka")
	}
	defer core.KafkaClient.Stop(ctx)

	// Create the event proxy to post events to KAFKA
	eventProxy := events.NewEventProxy(events.MsgClient(core.KafkaClient), events.MsgTopic(kafka.Topic{Name: cf.EventTopic}))
	go eventProxy.Start()
	defer eventProxy.Stop()

	go kafka.MonitorKafkaReadiness(ctx, core.KafkaClient, cf.LiveProbeInterval, cf.NotLiveProbeInterval, clusterMessagingService)

	// create kv path
	dbPath := model.NewDBPath(backend)

	// load adapters & device types while other things are starting
	adapterMgr := adapter.NewAdapterManager(dbPath, id, backend, cf.LiveProbeInterval)
	go adapterMgr.Start(ctx, adapterService)

	// create the core of the system, the device managers
	deviceMgr, logicalDeviceMgr := device.NewManagers(dbPath, adapterMgr, cf, id, eventProxy)

	// Start the device manager to load the devices. Wait until it is completed to prevent multiple loading happening
	// triggered by logicalDeviceMgr.Start(Ctx)
	err = deviceMgr.Start(ctx, deviceService)
	if err != nil {
		logger.Fatalw(ctx, "failure-starting-device-manager", log.Fields{"error": err})
	}

	// Start the logical device manager to load the logical devices.
	logicalDeviceMgr.Start(ctx, logicalDeviceService)

	// start gRPC handler
	grpcServer := grpcserver.NewGrpcServer(cf.GrpcAddress, nil, false, probe.GetProbeFromContext(ctx))

	//Register the 'Extension' service on this gRPC server
	addGRPCExtensionService(ctx, grpcServer, device.GetNewExtensionManager(deviceMgr))

	//Register the 'core' service
	addCoreService(ctx, grpcServer, api.NewNBIHandler(deviceMgr, nil, adapterMgr))

	go startGRPCService(ctx, grpcServer, grpcService, api.NewNBIHandler(deviceMgr, logicalDeviceMgr, adapterMgr))
	defer grpcServer.Stop()

	// wait for core to be stopped, via Stop() or context cancellation, before running deferred functions
	<-ctx.Done()
}

// Stop brings down core services
func (core *Core) Stop() {
	core.Shutdown()
	<-core.Stopped
}

// startGRPCService creates the grpc service handlers, registers it to the grpc server and starts the server
func startGRPCService(ctx context.Context, server *grpcserver.GrpcServer, serviceName string, handler voltha.VolthaServiceServer) {
	logger.Info(ctx, "grpc-server-created")

	server.AddService(func(gs *grpc.Server) { voltha.RegisterVolthaServiceServer(gs, handler) })
	logger.Info(ctx, "grpc-service-added")

	probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusRunning)
	logger.Info(ctx, "grpc-server-started")
	// Note that there is a small window here in which the core could return its status as ready,
	// when it really isn't.  This is unlikely to cause issues, as the delay is incredibly short.
	server.Start(ctx)
	probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusStopped)
}

func addGRPCExtensionService(ctx context.Context, server *grpcserver.GrpcServer, handler extension.ExtensionServer) {
	logger.Info(ctx, "extension-grpc-server-created")

	server.AddService(func(server *grpc.Server) {
		extension.RegisterExtensionServer(server, handler)
	})

}

func addCoreService(ctx context.Context, server *grpcserver.GrpcServer, handler core.CoreServiceServer) {
	logger.Info(ctx, "core-grpc-server-created")

	server.AddService(func(server *grpc.Server) {
		core.RegisterCoreServiceServer(server, handler)
	})
}
