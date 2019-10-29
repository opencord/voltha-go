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
	"time"
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

	log.Info("starting-core-services", log.Fields{"coreId": core.instanceId})

	// Wait until connection to KV Store is up
	if err := core.waitUntilKVStoreReachableOrMaxTries(ctx, core.config.MaxConnectionRetries, core.config.ConnectionRetryInterval); err != nil {
		log.Fatal("Unable-to-connect-to-KV-store")
	}
	if p != nil {
		p.UpdateStatus("kv-store", probe.ServiceStatusRunning)
	}

	// core.kmp must be created before deviceMgr and adapterMgr, as they will make
	// private copies of the poiner to core.kmp.
	if err := core.initKafkaManager(ctx); err != nil {
		log.Fatal("Failed-to-init-kafka-manager")
	}

	log.Debugw("values", log.Fields{"kmp": core.kmp})
	core.deviceMgr = newDeviceManager(core)
	core.adapterMgr = newAdapterManager(core.clusterDataProxy, core.instanceId, core.deviceMgr)
	core.deviceMgr.adapterMgr = core.adapterMgr
	core.logicalDeviceMgr = newLogicalDeviceManager(core, core.deviceMgr, core.kmp, core.clusterDataProxy, core.config.DefaultCoreTimeout)

	// Start the KafkaManager. This must be done after the deviceMgr, adapterMgr, and
	// logicalDeviceMgr have been created, as once the kmp is started, it will register
	// the above with the kmp.

	go core.startKafkaManager(ctx, core.config.ConnectionRetryInterval)

	go core.startDeviceManager(ctx)
	go core.startLogicalDeviceManager(ctx)
	go core.startGRPCService(ctx)
	go core.startAdapterManager(ctx)

	// Setup device ownership context
	core.deviceOwnership = NewDeviceOwnership(core.instanceId, core.kvClient, core.deviceMgr, core.logicalDeviceMgr,
		"service/voltha/owns_device", 10)

	log.Info("core-services-started")
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

// Initialize the kafka manager, but we will start it later
func (core *Core) initKafkaManager(ctx context.Context) error {
	log.Infow("initialize-kafka-manager", log.Fields{"host": core.config.KafkaAdapterHost,
		"port": core.config.KafkaAdapterPort, "topic": core.config.CoreTopic})

	probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusPreparing)

	// create the proxy
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

	probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusPrepared)

	return nil
}

/*
 * KafkaMonitorThread
 *
 * Repsonsible for starting the Kafka Interadapter Proxy and monitoring its liveness
 * state.
 *
 * Any producer that fails to send will cause KafkaInterContainerProxy to set its
 * post a false event on its liveness channel. Any producer that succeeds in sending
 * will cause KafkaInterContainerProxy to post a true event on its liveness
 * channel.
 *
 * This thread monitors the status of KafkaInterContainerProxy's liveness and pushes
 * that state to the core's readiness probes. If no liveness event has been seen
 * within a timeout, then the thread will make an attempt to produce a "liveness"
 * message, which will in turn trigger a liveness event (either true or false,
 * depending on whether the Produce succeeds).
 *
 * The timeout is dynamically adjusted so that it is long when the system is in a
 * live state, with the idea being that routine kafka traffic will continue to
 * refresh the liveness and prevent the timeout from firing. When in a NotLive
 * state, the timeout is reduced so liveness messages are produced at a much
 * more frequent interval (while not live, the messages are discarded, so only
 * the final message during a NotLive state gets actually produced).
 */

func (core *Core) startKafkaManager(ctx context.Context, startupRetryInterval int) {
	log.Infow("starting-kafka-manager-thread", log.Fields{"host": core.config.KafkaAdapterHost,
		"port": core.config.KafkaAdapterPort, "topic": core.config.CoreTopic})

	started := false
	for !started {
		// If we haven't started yet, then try to start
		if !started {
			log.Infow("starting-kafka-proxy", log.Fields{})
			if err := core.kmp.Start(); err != nil {
				// We failed to start. Delay and then try again later.
				// Don't worry about liveness, as we can't be live until we've started.
				probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusNotReady)
				log.Infow("error-starting-kafka-messaging-proxy", log.Fields{"error": err})
				time.Sleep(time.Duration(startupRetryInterval) * time.Second)
				continue
			} else {
				// We started. We only need to do this once.
				// Next we'll fall through and start checking liveness.
				log.Infow("started-kafka-proxy", log.Fields{})

				// cannot do this until after the kmp is started
				if err := core.registerAdapterRequestHandlers(ctx, core.instanceId, core.deviceMgr, core.logicalDeviceMgr, core.adapterMgr, core.clusterDataProxy, core.localDataProxy); err != nil {
					log.Fatal("Failure-registering-adapterRequestHandler")
				}

				started = true
			}
		}
	}

	log.Info("started-kafka-message-proxy")

	livenessChannel := core.kmp.EnableLivenessChannel(true)

	log.Info("enabled-kafka-liveness-channel")

	timeout := 60 * time.Second // TODO: Make configurable
	for {
		select {
		case liveness := <-livenessChannel:
			log.Infow("kafka-manager-thread-liveness-event", log.Fields{"liveness": liveness})
			// there was a state change in Kafka liveness
			if !liveness {
				// went from live to notLive
				probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusNotReady)
				// retry frequently while life is bad
				timeout = 5 * time.Second
			} else {
				// went from notLive to live
				probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusRunning)
				// retry infrequently while life is good
				timeout = 60 * time.Second
			}
		case <-time.After(timeout):
			log.Info("kafka-proxy-liveness-recheck")
			err := core.kmp.SendLiveness()
			if err != nil {
				// it could be that we're sending the liveness while sarama is stopped
				// TODO: this was happening due to a bug and probably doesn't happen anymore
				log.Warnw("error-kafka-send-liveness", log.Fields{"error": err})
			}
		}
	}
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
			count += 1
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
