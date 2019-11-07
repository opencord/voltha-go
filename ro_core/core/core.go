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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
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
	backend           db.Backend
}

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

func NewCore(id string, cf *config.ROCoreFlags, kvClient kvstore.Client) *Core {
	var core Core
	var err error
	core.instanceId = id
	core.exitChannel = make(chan int, 1)
	core.config = cf
	core.kvClient = kvClient

	// Configure backend to push Liveness Status at least every cf.LiveProbeInterval / 2 seconds
	// so as to avoid trigger of Liveness check (due to Liveness timeout) when backend is alive
	livenessChannelInterval := cf.LiveProbeInterval / 2

	// Setup the KV store
	// Do not call NewBackend constructor; it creates its own KV client
	// Commented the backend for now until the issue between the model and the KV store
	// is resolved.
	core.backend = db.Backend{
		Client:                  kvClient,
		StoreType:               cf.KVStoreType,
		Host:                    cf.KVStoreHost,
		Port:                    cf.KVStorePort,
		Timeout:                 cf.KVStoreTimeout,
		LivenessChannelInterval: livenessChannelInterval,
		PathPrefix:              "service/voltha"}
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, &core.backend)
	core.localDataRoot = model.NewRoot(&voltha.CoreInstance{}, &core.backend)
	core.clusterDataProxy, err = core.clusterDataRoot.CreateProxy(context.Background(), "/", false)
	if err != nil {
		log.Fatalf("error %v", err)
	}
	core.localDataProxy, err = core.localDataRoot.CreateProxy(context.Background(), "/", false)
	if err != nil {
		log.Fatalf("error %v", err)
	}
	return &core
}

// waitUntilKVStoreReachableOrMaxTries will wait until it can connect to a KV store or until maxtries has been reached
func (core *Core) waitUntilKVStoreReachableOrMaxTries(ctx context.Context, maxRetries int, retryInterval time.Duration) error {
	log.Infow("verifying-KV-store-connectivity", log.Fields{"host": core.config.KVStoreHost,
		"port": core.config.KVStorePort, "retries": maxRetries, "retryInterval": retryInterval})

	// Get timeout in seconds with 1 second set as minimum
	timeout := int(core.config.CoreTimeout.Seconds())
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
			count += 1
			//      Take a nap before retrying
			time.Sleep(retryInterval)
			log.Infow("retry-KV-store-connectivity", log.Fields{"retryCount": count, "maxRetries": maxRetries, "retryInterval": retryInterval})

		} else {
			break
		}
	}
	log.Info("KV-store-reachable")
	return nil
}

func (core *Core) Start(ctx context.Context) {
	log.Info("starting-adaptercore", log.Fields{"coreId": core.instanceId})

	// Wait until connection to KV Store is up
	if err := core.waitUntilKVStoreReachableOrMaxTries(ctx, core.config.MaxConnectionRetries, core.config.ConnectionRetryInterval); err != nil {
		log.Fatal("Unable-to-connect-to-KV-store")
	}

	probe.UpdateStatusFromContext(ctx, "kv-store", probe.ServiceStatusRunning)

	core.genericMgr = newModelProxyManager(core.clusterDataProxy)
	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceId)
	core.logicalDeviceMgr = newLogicalDeviceManager(core.deviceMgr, core.clusterDataProxy)
	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)
	go core.monitorKvstoreLiveness(ctx)

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
	core.grpcServer = grpcserver.NewGrpcServer(core.config.GrpcHost, core.config.GrpcPort, nil, false, probe.GetProbeFromContext(ctx))
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

/*
* Thread to monitor kvstore Liveness (connection status)
*
* This function constantly monitors Liveness State of kvstore as reported
* periodically by backend and updates the Status of kv-store service registered
* with ro_core probe.
*
* If no liveness event has been seen within a timeout, then the thread will make
* an trigger a "liveness" check, which will in turn trigger a liveness event on
* the liveness channel, true or false depending on whether the attempt succeeded.
*
* The gRPC server in turn monitors the state of the readiness probe and will
* start issuing UNAVAILABLE response while the probe is not ready.
 */
func (core *Core) monitorKvstoreLiveness(ctx context.Context) {
	log.Info("start-monitoring-kvstore-liveness")

	// Instruct backend to create Liveness channel for transporting state updates
	livenessChannel := core.backend.EnableLivenessChannel()

	log.Debug("enabled-kvstore-liveness-channel")

	// Default state for kvstore is not alive
	timeout := core.config.NotLiveProbeInterval
	for {
		timeoutTimer := time.NewTimer(timeout)
		select {

		case liveness := <-livenessChannel:
			log.Debugw("received-liveness-change-notification", log.Fields{"liveness": liveness})

			if !liveness {
				probe.UpdateStatusFromContext(ctx, "kv-store", probe.ServiceStatusNotReady)

				if core.grpcServer != nil {
					log.Info("kvstore-set-server-notready")
				}

				timeout = core.config.NotLiveProbeInterval
			} else {
				probe.UpdateStatusFromContext(ctx, "kv-store", probe.ServiceStatusRunning)

				if core.grpcServer != nil {
					log.Info("kvstore-set-server-ready")
				}

				timeout = core.config.LiveProbeInterval
			}

			if !timeoutTimer.Stop() {
				<-timeoutTimer.C
			}

		case <-timeoutTimer.C:
			log.Info("kvstore-perform-liveness-check-on-timeout")

			// Trigger Liveness check if no liveness update received within the timeout period.
			// The Liveness check will push Live state to same channel which this routine is
			// reading and processing. This, do it asynchronously to avoid blocking for
			// backend response and avoid any possibility of deadlock
			go core.backend.PerformLivenessCheck(core.config.KVStoreTimeout)
		}
	}
}
