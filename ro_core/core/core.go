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
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	grpcserver "github.com/opencord/voltha-lib-go/v2/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/probe"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc"
)

type Core struct {
	instanceId        string
	genericMgr        *ModelProxyManager
	deviceMgr         *DeviceManager
	logicalDeviceMgr  *LogicalDeviceManager
	grpcServer        *grpcserver.GrpcServer
	grpcNBIAPIHandler *APIHandler
	config            *config.ROCoreFlags
	clusterDataRoot   model.Root
	localDataRoot     model.Root
	clusterDataProxy  *model.Proxy
	localDataProxy    *model.Proxy
	exitChannel       chan int
	kvClient          kvstore.Client
}

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

func NewCore(id string, cf *config.ROCoreFlags, kvClient kvstore.Client) *Core {
	var core Core
	core.instanceId = id
	core.exitChannel = make(chan int, 1)
	core.config = cf
	core.kvClient = kvClient

	// Setup the KV store
	// Do not call NewBackend constructor; it creates its own KV client
	// Commented the backend for now until the issue between the model and the KV store
	// is resolved.
	backend := db.Backend{
		Client:     kvClient,
		StoreType:  cf.KVStoreType,
		Host:       cf.KVStoreHost,
		Port:       cf.KVStorePort,
		Timeout:    cf.KVStoreTimeout,
		PathPrefix: "service/voltha"}
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, &backend)
	core.localDataRoot = model.NewRoot(&voltha.CoreInstance{}, &backend)
	core.clusterDataProxy, _ = core.clusterDataRoot.CreateProxy(context.Background(), "/", false)
	core.localDataProxy, _ = core.localDataRoot.CreateProxy(context.Background(), "/", false)
	return &core
}

func (core *Core) Start(ctx context.Context) {
	log.Info("starting-adaptercore", log.Fields{"coreId": core.instanceId})
	core.genericMgr = newModelProxyManager(core.clusterDataProxy)
	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceId)
	core.logicalDeviceMgr = newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)

	log.Info("adaptercore-started")
}

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
	log.Info("adaptercore-stopped")
}

//startGRPCService creates the grpc service handlers, registers it to the grpc server
// and starts the server
func (core *Core) startGRPCService(ctx context.Context) {
	//	create an insecure gserver server
	core.grpcServer = grpcserver.NewGrpcServer(core.config.GrpcHost, core.config.GrpcPort, nil, false, nil)
	log.Info("grpc-server-created")

	core.grpcNBIAPIHandler = NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)
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

	//	Start the server
	log.Info("grpc-server-started")
	core.grpcServer.Start(context.Background())

	probe.UpdateStatusFromContext(ctx, "grpc-service", probe.ServiceStatusStopped)
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
