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
	"errors"
	"fmt"
	grpcserver "github.com/opencord/voltha-go/common/grpc"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type Core struct {
	instanceId        string
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
	coreMembership    *voltha.Membership
	membershipLock    sync.RWMutex
	deviceOwnership   *DeviceOwnership
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func NewCore(id string, cf *config.RWCoreFlags, kvClient kvstore.Client, kafkaClient kafka.Client) *Core {
	var core Core
	core.instanceId = id
	core.exitChannel = make(chan int, 1)
	core.config = cf
	core.kvClient = kvClient
	core.kafkaClient = kafkaClient

	// Setup the KV store
	// Do not call NewBackend constructor; it creates its own KV client
	// Commented the backend for now until the issue between the model and the KV store
	// is resolved.
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

func (core *Core) Start(ctx context.Context) {
	log.Info("starting-adaptercore", log.Fields{"coreId": core.instanceId})
	if err := core.startKafkaMessagingProxy(ctx); err != nil {
		log.Fatal("Failure-starting-kafkaMessagingProxy")
	}
	log.Debugw("values", log.Fields{"kmp": core.kmp})
	core.adapterMgr = newAdapterManager(core.clusterDataProxy, core.instanceId)
	core.deviceMgr = newDeviceManager(core)
	core.logicalDeviceMgr = newLogicalDeviceManager(core, core.deviceMgr, core.kmp, core.clusterDataProxy, core.config.DefaultCoreTimeout)

	if err := core.registerAdapterRequestHandlers(ctx, core.instanceId, core.deviceMgr, core.logicalDeviceMgr, core.adapterMgr, core.clusterDataProxy, core.localDataProxy); err != nil {
		log.Fatal("Failure-registering-adapterRequestHandler")
	}
	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)
	go core.startAdapterManager(ctx)

	// Setup device ownership context
	core.deviceOwnership = NewDeviceOwnership(core.instanceId, core.kvClient, core.deviceMgr, core.logicalDeviceMgr,
		"service/voltha/owns_device", 10)

	log.Info("adaptercore-started")
}

func (core *Core) Stop(ctx context.Context) {
	log.Info("stopping-adaptercore")
	core.exitChannel <- 1
	// Stop all the started services
	core.grpcServer.Stop()
	core.logicalDeviceMgr.stop(ctx)
	core.deviceMgr.stop(ctx)
	core.kmp.Stop()
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

	//	Start the server
	core.grpcServer.Start(context.Background())
	log.Info("grpc-server-started")
}

func (core *Core) startKafkaMessagingProxy(ctx context.Context) error {
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

	if err = core.kmp.Start(); err != nil {
		log.Fatalw("error-starting-messaging-proxy", log.Fields{"error": err})
		return err
	}

	log.Info("kafka-messaging-proxy-created")
	return nil
}

func (core *Core) registerAdapterRequestHandlers(ctx context.Context, coreInstanceId string, dMgr *DeviceManager,
	ldMgr *LogicalDeviceManager, aMgr *AdapterManager, cdProxy *model.Proxy, ldProxy *model.Proxy,
) error {
	requestProxy := NewAdapterRequestHandlerProxy(core, coreInstanceId, dMgr, ldMgr, aMgr, cdProxy, ldProxy,
		core.config.InCompetingMode, core.config.LongRunningRequestTimeout, core.config.DefaultRequestTimeout)

	// Register the broadcast topic to handle any core-bound broadcast requests
	if err := core.kmp.SubscribeWithRequestHandlerInterface(kafka.Topic{Name: core.config.CoreTopic}, requestProxy); err != nil {
		log.Fatalw("Failed-registering-broadcast-handler", log.Fields{"topic": core.config.CoreTopic})
		return err
	}

	log.Info("request-handler-registered")
	return nil
}

func (core *Core) isMembershipRegistrationComplete() bool {
	core.membershipLock.RLock()
	defer core.membershipLock.RUnlock()
	return core.coreMembership != nil
}

func (core *Core) updateCoreMembership(ctx context.Context, membership *voltha.Membership) error {
	core.membershipLock.Lock()
	defer core.membershipLock.Unlock()
	// Sanity check - can only register once
	if core.coreMembership != nil {
		log.Warnw("core-reregistration-not-allowed", log.Fields{"membership": membership})
		return status.Errorf(codes.AlreadyExists, "%s", membership.GroupName)
	}

	if membership == nil || membership.GroupName == "" {
		return errors.New("invalid-membership-info")
	}

	core.coreMembership = membership

	// Use the group name to register a specific kafka topic for this container
	go func(groupName string) {
		// Register the core-pair topic to handle core-bound requests destined to the core pair
		pTopic := kafka.Topic{Name: fmt.Sprintf("%s_%s", core.config.CoreTopic, groupName)}
		// TODO: Set a retry here to ensure this subscription happens
		if err := core.kmp.SubscribeWithDefaultRequestHandler(pTopic, kafka.OffsetNewest); err != nil {
			log.Fatalw("Failed-registering-pair-handler", log.Fields{"topic": core.config.CoreTopic})
		}
		// Update the CoreTopic to be used by the adapter proxy
		core.deviceMgr.adapterProxy.updateCoreTopic(&pTopic)
	}(membership.GroupName)

	log.Info("core-membership-registered")
	return nil
}

func (core *Core) getCoreMembership(ctx context.Context) *voltha.Membership {
	core.membershipLock.RLock()
	defer core.membershipLock.RUnlock()
	return core.coreMembership
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
