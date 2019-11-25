/*
 * Copyright 2019-present Open Networking Foundation
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
package core

import (
	"context"
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/coreIf"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v2/pkg/adapters"
	com "github.com/opencord/voltha-lib-go/v2/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	lm "github.com/opencord/voltha-lib-go/v2/pkg/mocks"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/phayes/freeport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"strconv"
	"time"
)

const (
	logLevel                    = log.FatalLevel
	volthaSerialNumberKey       = "voltha_serial_number"
	retryInterval               = 50 * time.Millisecond
	ownershipPrefix             = "service/voltha/owns_device"
	ownershipReservationTimeout = 10 // in seconds. TODO: change timeout to duration once its done in Core
)

const (
	OltAdapter = iota
	OnuAdapter
)

var (
	coreInCompeteMode bool
)

type isLogicalDeviceConditionSatisfied func(ld *voltha.LogicalDevice) bool
type isDeviceConditionSatisfied func(ld *voltha.Device) bool
type isDevicesConditionSatisfied func(ds *voltha.Devices) bool

func init() {
	_, err := log.AddPackage(log.JSON, logLevel, log.Fields{"instanceId": "coreTests"})
	if err != nil {
		panic(err)
	}
	// Update all loggers to log level specified as input parameter
	log.SetAllLogLevel(4)

	// Exmaple on how to enable specific package log level
	//log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)

	//Default mode is two rw-core running in a pair of competing cores
	coreInCompeteMode = true
}

func setCoreCompeteMode(mode bool) {
	coreInCompeteMode = mode
}

func getContext() context.Context {
	if coreInCompeteMode {
		return metadata.NewIncomingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, uuid.New().String()))
	}
	return context.Background()
}

//startEmbeddedEtcdServer creates and starts an Embedded etcd server locally.
func startEmbeddedEtcdServer(configName, storageDir, logLevel string) (*lm.EtcdServer, int, error) {
	kvClientPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	etcdServer := lm.StartEtcdServer(lm.MKConfig(configName, kvClientPort, peerPort, storageDir, logLevel))
	if etcdServer == nil {
		return nil, 0, status.Error(codes.Internal, "Embedded server failed to start")
	}
	return etcdServer, kvClientPort, nil
}

func stopEmbeddedEtcdServer(server *lm.EtcdServer) {
	if server != nil {
		server.Stop()
	}
}

// rwCoreUnderTest represents the rw-core.  It does everything the rw-core does except that it does not
// start a gRPC server to listen to NB requests.  Instead it only registers a NB interface to the rw-core
// which will be used for unit test requests.
type rwCoreUnderTest struct {
	*Core
}

func newRWCoreUnderTest(ID string, cfg *config.RWCoreFlags, kvClient kvstore.Client, kafkaClient kafka.Client) *rwCoreUnderTest {
	setCoreCompeteMode(cfg.InCompetingMode)
	return &rwCoreUnderTest{NewCore(ID, cfg, kvClient, kafkaClient)}
}

//StartGRPCService "overwrites" Voltha Core StartGRPCService function.
func (rwc *rwCoreUnderTest) StartGRPCService(ctx context.Context) {
	rwc.grpcNBIAPIHandler = NewAPIHandler(rwc)
	rwc.logicalDeviceMgr.setGrpcNbiHandler(rwc.grpcNBIAPIHandler)
}

func (rwc *rwCoreUnderTest) Start(ctx context.Context) {
	if err := rwc.initKafkaManager(ctx); err != nil {
		log.Fatal("Failed-to-init-kafka-manager")
	}
	rwc.deviceMgr = newDeviceManager(rwc)
	rwc.adapterMgr = newAdapterManager(rwc.clusterDataProxy, rwc.instanceId, rwc.deviceMgr)
	rwc.deviceMgr.adapterMgr = rwc.adapterMgr
	rwc.logicalDeviceMgr = newLogicalDeviceManager(rwc, rwc.deviceMgr, rwc.kmp, rwc.clusterDataProxy, rwc.config.DefaultCoreTimeout)

	go rwc.StartKafkaManager(ctx,
		rwc.config.ConnectionRetryInterval,
		rwc.config.LiveProbeInterval,
		rwc.config.NotLiveProbeInterval)

	go rwc.StartDeviceManager(ctx)
	go rwc.StartLogicalDeviceManager(ctx)
	go rwc.StartGRPCService(ctx)
	go rwc.StartAdapterManager(ctx)

	// Setup device ownership context
	rwc.deviceOwnership = NewDeviceOwnership(rwc.instanceId, rwc.kvClient, rwc.deviceMgr, rwc.logicalDeviceMgr,
		ownershipPrefix, ownershipReservationTimeout)
}

func setupKVClient(cf *config.RWCoreFlags, coreInstanceId string) kvstore.Client {
	addr := cf.KVStoreHost + ":" + strconv.Itoa(cf.KVStorePort)
	client, err := kvstore.NewEtcdClient(addr, cf.KVStoreTimeout)
	if err != nil {
		panic("no kv client")
	}
	// Setup KV transaction context
	txnPrefix := cf.KVStoreDataPrefix + "/transactions/"
	if err = SetTransactionContext(coreInstanceId,
		txnPrefix,
		client,
		cf.KVStoreTimeout); err != nil {
		log.Fatal("creating-transaction-context-failed")
	}
	return client
}

func createMockAdapter(adapterType int, kafkaClient kafka.Client, coreInstanceId string, coreName string, adapterName string) (adapters.IAdapter, error) {
	var err error
	var adapter adapters.IAdapter
	adapterKafkaICProxy, err := kafka.NewInterContainerProxy(
		kafka.MsgClient(kafkaClient),
		kafka.DefaultTopic(&kafka.Topic{Name: adapterName}))
	if err != nil || adapterKafkaICProxy == nil {
		log.Errorw("Failure-creating-adapter-intercontainerProxy", log.Fields{"error": err, "adapter": adapterName})
		return nil, err
	}
	adapterCoreProxy := com.NewCoreProxy(adapterKafkaICProxy, adapterName, coreName)
	var adapterReqHandler *com.RequestHandlerProxy
	switch adapterType {
	case OltAdapter:
		adapter = cm.NewOLTAdapter(adapterCoreProxy)
	case OnuAdapter:
		adapter = cm.NewONUAdapter(adapterCoreProxy)
	default:
		log.Fatalf("invalid-adapter-type-%d", adapterType)
	}
	adapterReqHandler = com.NewRequestHandlerProxy(coreInstanceId, adapter, adapterCoreProxy)

	if err = adapterKafkaICProxy.Start(); err != nil {
		log.Errorw("Failure-starting-adapter-intercontainerProxy", log.Fields{"error": err})
		return nil, err
	}
	if err = adapterKafkaICProxy.SubscribeWithRequestHandlerInterface(kafka.Topic{Name: adapterName}, adapterReqHandler); err != nil {
		log.Errorw("Failure-to-subscribe-onu-request-handler", log.Fields{"error": err})
		return nil, err
	}
	return adapter, nil
}

func waitUntilDeviceReadiness(deviceID string,
	timeout time.Duration,
	verificationFunction isDeviceConditionSatisfied,
	nbi coreIf.APIHandler) error {
	ch := make(chan int)
	done := false
	go func() {
		for {
			device, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: deviceID})
			if device != nil && verificationFunction(device) {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("expected-states-not-reached-for-device%s", deviceID)
	}
}

func waitUntilLogicalDeviceReadiness(oltDeviceId string,
	timeout time.Duration,
	nbi coreIf.APIHandler,
	verificationFunction isLogicalDeviceConditionSatisfied,
) error {
	ch := make(chan int)
	done := false
	go func() {
		for {
			// Get the logical device from the olt device
			d, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDeviceId})
			if d != nil && d.ParentId != "" {
				ld, _ := nbi.GetLogicalDevice(getContext(), &voltha.ID{Id: d.ParentId})
				if ld != nil && verificationFunction(ld) {
					ch <- 1
					break
				}
				if done {
					break
				}
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-for-logical-device-readiness%s", oltDeviceId)
	}
}

func waitUntilConditionForDevices(timeout time.Duration, nbi coreIf.APIHandler, verificationFunction isDevicesConditionSatisfied) error {
	ch := make(chan int)
	done := false
	go func() {
		for {
			devices, _ := nbi.ListDevices(getContext(), &empty.Empty{})
			if verificationFunction(devices) {
				ch <- 1
				break
			}
			if done {
				break
			}

			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-devices")
	}
}
