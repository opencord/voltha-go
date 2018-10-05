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
	grpcNBIAPIHanfler *APIHandler
	config            *config.RWCoreFlags
	kmp               *kafka.KafkaMessagingProxy
	clusterDataRoot   model.Root
	localDataRoot     model.Root
	clusterDataProxy  *model.Proxy
	localDataProxy    *model.Proxy
	exitChannel       chan int
}

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

func NewCore(id string, cf *config.RWCoreFlags) *Core {
	var core Core
	core.instanceId = id
	core.exitChannel = make(chan int, 1)
	core.config = cf
	// TODO: Setup the KV store
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, nil)
	core.localDataRoot = model.NewRoot(&voltha.CoreInstance{}, nil)
	core.clusterDataProxy = core.clusterDataRoot.GetProxy("/", false)
	core.localDataProxy = core.localDataRoot.GetProxy("/", false)
	return &core
}

func (core *Core) Start(ctx context.Context) {
	log.Info("starting-core")
	core.startKafkaMessagingProxy(ctx)
	log.Info("values", log.Fields{"kmp": core.kmp})
	core.deviceMgr = newDeviceManager(core.kmp, core.clusterDataProxy)
	core.logicalDeviceMgr = newLogicalDeviceManager(core.deviceMgr, core.kmp, core.clusterDataProxy)
	core.registerAdapterRequestHandler(ctx, core.deviceMgr, core.logicalDeviceMgr, core.localDataProxy, core.clusterDataProxy)
	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)

	log.Info("core-started")
}

func (core *Core) Stop(ctx context.Context) {
	log.Info("stopping-core")
	core.exitChannel <- 1
	log.Info("core-stopped")
}

//startGRPCService creates the grpc service handlers, registers it to the grpc server
// and starts the server
func (core *Core) startGRPCService(ctx context.Context) {
	//	create an insecure gserver server
	core.grpcServer = grpcserver.NewGrpcServer(core.config.GrpcHost, core.config.GrpcPort, nil, false)
	log.Info("grpc-server-created")

	core.grpcNBIAPIHanfler = NewAPIHandler(core.deviceMgr, core.logicalDeviceMgr)
	//	Create a function to register the core GRPC service with the GRPC server
	f := func(gs *grpc.Server) {
		voltha.RegisterVolthaServiceServer(
			gs,
			core.grpcNBIAPIHanfler,
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
	if core.kmp, err = kafka.NewKafkaMessagingProxy(
		kafka.KafkaHost(core.config.KafkaAdapterHost),
		kafka.KafkaPort(core.config.KafkaAdapterPort),
		kafka.DefaultTopic(&kafka.Topic{Name: core.config.CoreTopic})); err != nil {
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

func (core *Core) registerAdapterRequestHandler(ctx context.Context, dMgr *DeviceManager, ldMgr *LogicalDeviceManager,
	cdProxy *model.Proxy, ldProxy *model.Proxy) error {
	requestProxy := NewAdapterRequestHandlerProxy(dMgr, ldMgr, cdProxy, ldProxy)
	core.kmp.SubscribeWithTarget(kafka.Topic{Name: core.config.CoreTopic}, requestProxy)

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
