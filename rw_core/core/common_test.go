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
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/rw_core/config"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v3/pkg/adapters"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	mock_etcd "github.com/opencord/voltha-lib-go/v3/pkg/mocks/etcd"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/phayes/freeport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	volthaSerialNumberKey = "voltha_serial_number"
	retryInterval         = 50 * time.Millisecond
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
type isLogicalDevicesConditionSatisfied func(lds *voltha.LogicalDevices) bool
type isConditionSatisfied func() bool

func init() {
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
func startEmbeddedEtcdServer(configName, storageDir, logLevel string) (*mock_etcd.EtcdServer, int, error) {
	kvClientPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	etcdServer := mock_etcd.StartEtcdServer(mock_etcd.MKConfig(configName, kvClientPort, peerPort, storageDir, logLevel))
	if etcdServer == nil {
		return nil, 0, status.Error(codes.Internal, "Embedded server failed to start")
	}
	return etcdServer, kvClientPort, nil
}

func stopEmbeddedEtcdServer(server *mock_etcd.EtcdServer) {
	if server != nil {
		server.Stop()
	}
}

func setupKVClient(cf *config.RWCoreFlags, coreInstanceID string) kvstore.Client {
	addr := cf.KVStoreHost + ":" + strconv.Itoa(cf.KVStorePort)
	client, err := kvstore.NewEtcdClient(addr, cf.KVStoreTimeout)
	if err != nil {
		panic("no kv client")
	}
	return client
}

func createMockAdapter(adapterType int, kafkaClient kafka.Client, coreInstanceID string, coreName string, adapterName string) (adapters.IAdapter, error) {
	var err error
	var adapter adapters.IAdapter
	adapterKafkaICProxy := kafka.NewInterContainerProxy(
		kafka.MsgClient(kafkaClient),
		kafka.DefaultTopic(&kafka.Topic{Name: adapterName}))
	adapterCoreProxy := com.NewCoreProxy(adapterKafkaICProxy, adapterName, coreName)
	var adapterReqHandler *com.RequestHandlerProxy
	switch adapterType {
	case OltAdapter:
		adapter = cm.NewOLTAdapter(adapterCoreProxy)
	case OnuAdapter:
		adapter = cm.NewONUAdapter(adapterCoreProxy)
	default:
		logger.Fatalf("invalid-adapter-type-%d", adapterType)
	}
	adapterReqHandler = com.NewRequestHandlerProxy(coreInstanceID, adapter, adapterCoreProxy)

	if err = adapterKafkaICProxy.Start(); err != nil {
		logger.Errorw("Failure-starting-adapter-intercontainerProxy", log.Fields{"error": err})
		return nil, err
	}
	if err = adapterKafkaICProxy.SubscribeWithRequestHandlerInterface(kafka.Topic{Name: adapterName}, adapterReqHandler); err != nil {
		logger.Errorw("Failure-to-subscribe-onu-request-handler", log.Fields{"error": err})
		return nil, err
	}
	return adapter, nil
}

func waitUntilDeviceReadiness(deviceID string,
	timeout time.Duration,
	verificationFunction isDeviceConditionSatisfied,
	nbi *APIHandler) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			device, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: deviceID})
			if verificationFunction(device) {
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

func waitUntilLogicalDeviceReadiness(oltDeviceID string,
	timeout time.Duration,
	nbi *APIHandler,
	verificationFunction isLogicalDeviceConditionSatisfied,
) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			// Get the logical device from the olt device
			d, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDeviceID})
			if d != nil && d.ParentId != "" {
				ld, _ := nbi.GetLogicalDevice(getContext(), &voltha.ID{Id: d.ParentId})
				if verificationFunction(ld) {
					ch <- 1
					break
				}
				if done {
					break
				}
			} else if d != nil && d.ParentId == "" { // case where logical device deleted
				if verificationFunction(nil) {
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
		return fmt.Errorf("timeout-waiting-for-logical-device-readiness%s", oltDeviceID)
	}
}

func waitUntilConditionForDevices(timeout time.Duration, nbi *APIHandler, verificationFunction isDevicesConditionSatisfied) error {
	ch := make(chan int, 1)
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

func waitUntilConditionForLogicalDevices(timeout time.Duration, nbi *APIHandler, verificationFunction isLogicalDevicesConditionSatisfied) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			lDevices, _ := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
			if verificationFunction(lDevices) {
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
		return fmt.Errorf("timeout-waiting-logical-devices")
	}
}

func waitUntilCondition(timeout time.Duration, nbi *APIHandler, verificationFunction isConditionSatisfied) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			if verificationFunction() {
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
		return fmt.Errorf("timeout-waiting-for-condition")
	}
}
