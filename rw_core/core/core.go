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
	grpcserver "github.com/opencord/voltha-go/common/grpc"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-go/protos/voltha"
	"github.com/opencord/voltha-go/rw_core/config"
	"google.golang.org/grpc"
)

type Core struct {
	instanceId        string
	deviceMgr         *DeviceManager
	logicalDeviceMgr  *LogicalDeviceManager
	grpcServer        *grpcserver.GrpcServer
	grpcNBIAPIHandler *APIHandler
	config            *config.RWCoreFlags
	kmp               *kafka.InterContainerProxy
	clusterDataRoot   model.Root
	localDataRoot     model.Root
	clusterDataProxy  *model.Proxy
	localDataProxy    *model.Proxy
	exitChannel       chan int
	kvClient          kvstore.Client
	kafkaClient       kafka.Client
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
	//backend := model.Backend{
	//	MsgClient:     kvClient,
	//	StoreType:  cf.KVStoreType,
	//	Host:       cf.KVStoreHost,
	//	Port:       cf.KVStorePort,
	//	Timeout:    cf.KVStoreTimeout,
	//	PathPrefix: "service/voltha"}
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, nil)
	core.localDataRoot = model.NewRoot(&voltha.CoreInstance{}, nil)
	core.clusterDataProxy = core.clusterDataRoot.CreateProxy("/", false)
	core.localDataProxy = core.localDataRoot.CreateProxy("/", false)
	return &core
}

func (core *Core) Start(ctx context.Context) {
	log.Info("starting-adaptercore", log.Fields{"coreId": core.instanceId})
	core.startKafkaMessagingProxy(ctx)
	log.Info("values", log.Fields{"kmp": core.kmp})
	core.deviceMgr = newDeviceManager(core.kmp, core.clusterDataProxy, core.instanceId)
	core.logicalDeviceMgr = newLogicalDeviceManager(core.deviceMgr, core.kmp, core.clusterDataProxy)
	core.registerAdapterRequestHandler(ctx, core.instanceId, core.deviceMgr, core.logicalDeviceMgr, core.clusterDataProxy, core.localDataProxy)
	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)

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

//startGRPCService creates the grpc service handlers, registers it to the grpc server
// and starts the server
func (core *Core) startGRPCService(ctx context.Context) {
	//	create an insecure gserver server
	core.grpcServer = grpcserver.NewGrpcServer(core.config.GrpcHost, core.config.GrpcPort, nil, false)
	log.Info("grpc-server-created")

	core.grpcNBIAPIHandler = NewAPIHandler(core.deviceMgr, core.logicalDeviceMgr)
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

func (core *Core) registerAdapterRequestHandler(ctx context.Context, coreInstanceId string, dMgr *DeviceManager, ldMgr *LogicalDeviceManager,
	cdProxy *model.Proxy, ldProxy *model.Proxy) error {
	requestProxy := NewAdapterRequestHandlerProxy(coreInstanceId, dMgr, ldMgr, cdProxy, ldProxy)
	core.kmp.SubscribeWithRequestHandlerInterface(kafka.Topic{Name: core.config.CoreTopic}, requestProxy)

	log.Info("request-handlers")
	return nil
}

func (core *Core) startDeviceManager(ctx context.Context) {
	// TODO: Interaction between the logicaldevicemanager and devicemanager should mostly occur via
	// callbacks.  For now, until the model is ready, devicemanager will keep a reference to the
	// logicaldevicemanager to initiate the creation of logical devices
	log.Info("starting-DeviceManager")
	core.deviceMgr.start(ctx, core.logicalDeviceMgr)
	log.Info("started-DeviceManager")
}

func (core *Core) startLogicalDeviceManager(ctx context.Context) {
	log.Info("starting-Logical-DeviceManager")
	core.logicalDeviceMgr.start(ctx)
	log.Info("started-Logical-DeviceManager")
}
