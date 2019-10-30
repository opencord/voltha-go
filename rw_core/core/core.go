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

	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/model"
	grpcserver "github.com/opencord/voltha-lib-go/v2/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/probe"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Core -
type Core struct {
	instanceID        string
	deviceMgr         *DeviceManager
	logicalDeviceMgr  *LogicalDeviceManager
	grpcServer        *grpcserver.GrpcServer
	grpcNBIAPIHandler *APIHandler
	adapterMgr        *AdapterManager
	config            *config.RWCoreFlags
	kmp               *kafka.InterContainerProxy
	clusterDataRoot   model.Root
	localDataRoot     model.Root
	clusterDataProxy  *model.Proxy
	localDataProxy    *model.Proxy
	exitChannel       chan int
	kvClient          kvstore.Client
	kafkaClient       kafka.Client
	deviceOwnership   *DeviceOwnership
}

func init() {
	_, err := log.AddPackage(log.JSON, log.WarnLevel, nil)
	if err != nil {
		log.Errorw("failed", log.Fields{"error": err})
	}
}

// NewCore -
func NewCore(id string, cf *config.RWCoreFlags, kvClient kvstore.Client, kafkaClient kafka.Client) *Core {
	var core Core
	core.instanceID = id
	core.exitChannel = make(chan int, 1)
	core.config = cf
	core.kvClient = kvClient
	core.kafkaClient = kafkaClient

	// Setup the KV store
	backend := model.Backend{
		Client:     kvClient,
		StoreType:  cf.KVStoreType,
		Host:       cf.KVStoreHost,
		Port:       cf.KVStorePort,
		Timeout:    cf.KVStoreTimeout,
		PathPrefix: cf.KVStoreDataPrefix}
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, &backend)
	core.localDataRoot = model.NewRoot(&voltha.CoreInstance{}, &backend)
	core.clusterDataProxy = core.clusterDataRoot.CreateProxy(context.Background(), "/", false)
	core.localDataProxy = core.localDataRoot.CreateProxy(context.Background(), "/", false)
	return &core
}

// Start -
func (core *Core) Start(ctx context.Context) {

	// If the context has a probe then fetch it and register our services
	var p *probe.Probe
	if value := ctx.Value(probe.ProbeContextKey); value != nil {
		if _, ok := value.(*probe.Probe); ok {
			p = value.(*probe.Probe)
			p.RegisterService(
				"message-bus",
				"kv-store",
				"device-manager",
				"logical-device-manager",
				"adapter-manager",
				"grpc-service",
			)
		}
	}

	log.Info("starting-core-services", log.Fields{"coreId": core.instanceID})

	// Wait until connection to KV Store is up
	if err := core.waitUntilKVStoreReachableOrMaxTries(ctx, core.config.MaxConnectionRetries, core.config.ConnectionRetryInterval); err != nil {
		log.Fatal("Unable-to-connect-to-KV-store")
	}
	if p != nil {
		p.UpdateStatus("kv-store", probe.ServiceStatusRunning)
	}

	if err := core.waitUntilKafkaMessagingProxyIsUpOrMaxTries(ctx, core.config.MaxConnectionRetries, core.config.ConnectionRetryInterval); err != nil {
		log.Fatal("Failure-starting-kafkaMessagingProxy")
	}
	if p != nil {
		p.UpdateStatus("message-bus", probe.ServiceStatusRunning)
	}

	log.Debugw("values", log.Fields{"kmp": core.kmp})
	core.deviceMgr = newDeviceManager(core)
	core.adapterMgr = newAdapterManager(core.clusterDataProxy, core.instanceID, core.deviceMgr)
	core.deviceMgr.adapterMgr = core.adapterMgr
	core.logicalDeviceMgr = newLogicalDeviceManager(core, core.deviceMgr, core.kmp, core.clusterDataProxy, core.config.DefaultCoreTimeout)

	if err := core.registerAdapterRequestHandlers(ctx, core.instanceID, core.deviceMgr, core.logicalDeviceMgr, core.adapterMgr, core.clusterDataProxy, core.localDataProxy); err != nil {
		log.Fatal("Failure-registering-adapterRequestHandler")
	}

	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)
	go core.startAdapterManager(ctx)

	// Setup device ownership context
	core.deviceOwnership = NewDeviceOwnership(core.instanceID, core.kvClient, core.deviceMgr, core.logicalDeviceMgr,
		"service/voltha/owns_device", 10)

	log.Info("core-services-started")
}

// Stop -
func (core *Core) Stop(ctx context.Context) {
	log.Info("stopping-adaptercore")
	if core.exitChannel != nil {
		core.exitChannel <- 1
	}
	// Stop all the started services
	if core.grpcServer != nil {
		core.grpcServer.Stop()
	}
	if core.logicalDeviceMgr != nil {
		core.logicalDeviceMgr.stop(ctx)
	}
	if core.deviceMgr != nil {
		core.deviceMgr.stop(ctx)
	}
	if core.kmp != nil {
		core.kmp.Stop()
	}
	log.Info("adaptercore-stopped")
}

//startGRPCService creates the grpc service handlers, registers it to the grpc server and starts the server
func (core *Core) startGRPCService(ctx context.Context) {
	//	create an insecure gserver server
	core.grpcServer = grpcserver.NewGrpcServer(core.config.GrpcHost, core.config.GrpcPort, nil, false)
	log.Info("grpc-server-created")

	core.grpcNBIAPIHandler = NewAPIHandler(core)
	log.Infow("grpc-handler", log.Fields{"core_binding_key": core.config.CoreBindingKey})
	core.logicalDeviceMgr.setGrpcNbiHandler(core.grpcNBIAPIHandler)
	//	Create a function to register the core GRPC service with the GRPC server
	f := func(gs *grpc.Server) {
		voltha.RegisterVolthaServiceServer(
			gs,
			core.grpcNBIAPIHandler,
		)
	}

	core.grpcServer.AddService(f)
	log.Info("grpc-service-added")

	/*
	 * Start the GRPC server
	 *
	 * This is a bit sub-optimal here as the grpcServer.Start call does not return (blocks)
	 * until something fails, but we want to send a "start" status update. As written this
	 * means that we are actually sending the "start" status update before the server is
	 * started, which means it is possible that the status is "running" before it actually is.
	 *
	 * This means that there is a small window in which the core could return its status as
	 * ready, when it really isn't.
	 */
	probe.UpdateStatusFromContext(ctx, "grpc-service", probe.ServiceStatusRunning)
	log.Info("grpc-server-started")
	core.grpcServer.Start(context.Background())
	probe.UpdateStatusFromContext(ctx, "grpc-service", probe.ServiceStatusStopped)
}

func (core *Core) waitUntilKafkaMessagingProxyIsUpOrMaxTries(ctx context.Context, maxRetries int, retryInterval int) error {
	log.Infow("starting-kafka-messaging-proxy", log.Fields{"host": core.config.KafkaAdapterHost,
		"port": core.config.KafkaAdapterPort, "topic": core.config.CoreTopic})
	var err error
	if core.kmp, err = kafka.NewInterContainerProxy(
		kafka.InterContainerHost(core.config.KafkaAdapterHost),
		kafka.InterContainerPort(core.config.KafkaAdapterPort),
		kafka.MsgClient(core.kafkaClient),
		kafka.DefaultTopic(&kafka.Topic{Name: core.config.CoreTopic}),
		kafka.DeviceDiscoveryTopic(&kafka.Topic{Name: core.config.AffinityRouterTopic})); err != nil {
		log.Errorw("fail-to-create-kafka-proxy", log.Fields{"error": err})
		return err
	}
	count := 0
	for {
		if err = core.kmp.Start(); err != nil {
			log.Infow("error-starting-kafka-messaging-proxy", log.Fields{"error": err})
			if maxRetries != -1 {
				if count >= maxRetries {
					return err
				}
			}
			count++
			log.Infow("retry-starting-kafka-messaging-proxy", log.Fields{"retryCount": count, "maxRetries": maxRetries, "retryInterval": retryInterval})
			//	Take a nap before retrying
			time.Sleep(time.Duration(retryInterval) * time.Second)
		} else {
			break
		}
	}
	log.Info("kafka-messaging-proxy-created")
	return nil
}

// waitUntilKVStoreReachableOrMaxTries will wait until it can connect to a KV store or until maxtries has been reached
func (core *Core) waitUntilKVStoreReachableOrMaxTries(ctx context.Context, maxRetries int, retryInterval int) error {
	log.Infow("verifying-KV-store-connectivity", log.Fields{"host": core.config.KVStoreHost,
		"port": core.config.KVStorePort, "retries": maxRetries, "retryInterval": retryInterval})
	// Get timeout in seconds with 1 second set as minimum
	timeout := int(core.config.DefaultCoreTimeout / 1000)
	if timeout < 1 {
		timeout = 1
	}
	count := 0
	for {
		if !core.kvClient.IsConnectionUp(timeout) {
			log.Info("KV-store-unreachable")
			if maxRetries != -1 {
				if count >= maxRetries {
					return status.Error(codes.Unavailable, "kv store unreachable")
				}
			}
			count++
			//	Take a nap before retrying
			time.Sleep(time.Duration(retryInterval) * time.Second)
			log.Infow("retry-KV-store-connectivity", log.Fields{"retryCount": count, "maxRetries": maxRetries, "retryInterval": retryInterval})

		} else {
			break
		}
	}
	log.Info("KV-store-reachable")
	return nil
}

func (core *Core) registerAdapterRequestHandlers(ctx context.Context, coreInstanceID string, dMgr *DeviceManager,
	ldMgr *LogicalDeviceManager, aMgr *AdapterManager, cdProxy *model.Proxy, ldProxy *model.Proxy,
) error {
	requestProxy := NewAdapterRequestHandlerProxy(core, coreInstanceID, dMgr, ldMgr, aMgr, cdProxy, ldProxy,
		core.config.InCompetingMode, core.config.LongRunningRequestTimeout, core.config.DefaultRequestTimeout)

	// Register the broadcast topic to handle any core-bound broadcast requests
	if err := core.kmp.SubscribeWithRequestHandlerInterface(kafka.Topic{Name: core.config.CoreTopic}, requestProxy); err != nil {
		log.Fatalw("Failed-registering-broadcast-handler", log.Fields{"topic": core.config.CoreTopic})
		return err
	}

	// Register the core-pair topic to handle core-bound requests destined to the core pair
	if err := core.kmp.SubscribeWithDefaultRequestHandler(kafka.Topic{Name: core.config.CorePairTopic}, kafka.OffsetNewest); err != nil {
		log.Fatalw("Failed-registering-pair-handler", log.Fields{"topic": core.config.CorePairTopic})
		return err
	}

	log.Info("request-handler-registered")
	return nil
}

func (core *Core) startDeviceManager(ctx context.Context) {
	log.Info("DeviceManager-Starting...")
	core.deviceMgr.start(ctx, core.logicalDeviceMgr)
	log.Info("DeviceManager-Started")
}

func (core *Core) startLogicalDeviceManager(ctx context.Context) {
	log.Info("Logical-DeviceManager-Starting...")
	core.logicalDeviceMgr.start(ctx)
	log.Info("Logical-DeviceManager-Started")
}

func (core *Core) startAdapterManager(ctx context.Context) {
	log.Info("Adapter-Manager-Starting...")
	core.adapterMgr.start(ctx)
	log.Info("Adapter-Manager-Started")
}
