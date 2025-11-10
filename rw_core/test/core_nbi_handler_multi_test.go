/*
 * Copyright 2021-2024 Open Networking Foundation (ONF) and the ONF Contributors
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

package test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-lib-go/v7/pkg/flows"
	"github.com/opencord/voltha-lib-go/v7/pkg/kafka"
	mock_kafka "github.com/opencord/voltha-lib-go/v7/pkg/mocks/kafka"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/omci"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc"

	"github.com/golang/protobuf/jsonpb"
	"github.com/opencord/voltha-go/rw_core/config"
	c "github.com/opencord/voltha-go/rw_core/core"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	mock_etcd "github.com/opencord/voltha-lib-go/v7/pkg/mocks/etcd"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

var oltAdapters = map[string]*AdapterInfo{
	"olt_adapter_type1": {
		TotalReplica:    1,
		DeviceType:      "olt-device-type1",
		Vendor:          "olt-mock-vendor1",
		ChildDeviceType: "onu-device-type1",
		ChildVendor:     "onu-mock-vendor1",
	},
	"olt_adapter_type2": {
		TotalReplica:    1,
		DeviceType:      "olt-device-type2",
		Vendor:          "olt-mock-vendor2",
		ChildDeviceType: "onu-device-type2",
		ChildVendor:     "onu-mock-vendor2",
	},
}

var onuAdapters = map[string]*AdapterInfo{
	"onu_adapter_type1": {
		TotalReplica: 1,
		DeviceType:   "onu-device-type1",
		Vendor:       "onu-mock-vendor1",
	},
	"onu_adapter_type2": {
		TotalReplica: 1,
		DeviceType:   "onu-device-type2",
		Vendor:       "onu-mock-vendor2",
	},
}

type NBTest struct {
	etcdServer        *mock_etcd.EtcdServer
	config            *config.RWCoreFlags
	kvClientPort      int
	kEventClient      kafka.Client
	kafkaBroker       *sarama.MockBroker
	numONUPerOLT      int
	startingUNIPortNo int
	oltAdapters       map[string][]*cm.OLTAdapter // map<adapter type>[adapter instances]
	onuAdapters       map[string][]*cm.ONUAdapter
	coreInstanceID    string
	internalTimeout   time.Duration
	maxTimeout        time.Duration
	coreRPCTimeout    time.Duration
	coreFlowTimeout   time.Duration
	core              *c.Core
	probe             *probe.Probe
	oltAdaptersLock   sync.RWMutex
	onuAdaptersLock   sync.RWMutex
	changeEventLister *ChangedEventListener
}

var testLogger log.CLogger

func init() {
	var err error
	testLogger, err = log.RegisterPackage(log.JSON, log.InfoLevel, log.Fields{"nbi-handler-test": true})
	if err != nil {
		panic(err)
	}

	if err = log.SetLogLevel(log.InfoLevel); err != nil {
		panic(err)
	}
}

func newNBTest(ctx context.Context, loadTest bool) *NBTest {
	test := &NBTest{}
	// Start the embedded etcd server
	var err error
	test.etcdServer, test.kvClientPort, err = StartEmbeddedEtcdServer(ctx, "voltha.rwcore.nb.test", "voltha.rwcore.nb.etcd", "error")
	if err != nil {
		logger.Fatal(ctx, err)
	}
	test.coreInstanceID = "rw-nbi-test"
	test.internalTimeout = 20 * time.Second
	test.maxTimeout = 20 * time.Second
	test.coreRPCTimeout = 20 * time.Second
	test.coreFlowTimeout = 30 * time.Second
	if loadTest {
		test.internalTimeout = 100 * time.Second
		test.maxTimeout = 300 * time.Second
		test.coreRPCTimeout = 100 * time.Second
		test.coreFlowTimeout = 120 * time.Second
		setRetryInterval(5 * time.Second)
	}
	return test
}

func (nb *NBTest) startGRPCCore(ctx context.Context) (coreEndpoint, nbiEndpoint string) {
	// Setup the configs
	cfg := &config.RWCoreFlags{}
	cfg.ParseCommandArguments([]string{})
	cfg.InternalTimeout = nb.internalTimeout
	cfg.RPCTimeout = nb.coreRPCTimeout
	cfg.FlowTimeout = nb.coreFlowTimeout
	cfg.KVStoreAddress = "127.0.0.1" + ":" + strconv.Itoa(nb.kvClientPort)
	cfg.LogLevel = "DEBUG"

	// Get a free port for the Core gRPC server
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for grpc core")
	}
	cfg.GrpcSBIAddress = "127.0.0.1:" + strconv.Itoa(grpcPort)
	coreEndpoint = cfg.GrpcSBIAddress

	// Get a free port for the NBI gRPC server
	grpcPort, err = freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for grpc NBI")
	}
	cfg.GrpcNBIAddress = "127.0.0.1:" + strconv.Itoa(grpcPort)
	nbiEndpoint = cfg.GrpcNBIAddress

	// Set up the probe service
	nb.probe = &probe.Probe{}
	probePort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for probe port")
	}
	cfg.ProbeAddress = "127.0.0.1:" + strconv.Itoa(probePort)
	go nb.probe.ListenAndServe(ctx, cfg.ProbeAddress)

	//Add the probe to the context to pass to all the services started
	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, nb.probe)

	// Set up a mock kafka broker
	kafkaPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatalw(probeCtx, "Cannot get a freeport for kafka port", log.Fields{"error": err})
	}
	cfg.KafkaClusterAddress = "127.0.0.1:" + strconv.Itoa(kafkaPort)

	// Register probe services
	nb.probe.RegisterService(
		ctx,
		"cluster-message-service",
		"grpc-sbi-service",
		"adapter-service",
		"kv-service",
		"device-service",
		"logical-device-service",
	)

	nb.kEventClient = mock_kafka.NewKafkaClient()

	nb.config = cfg
	shutdownCtx, cancelCtx := context.WithCancel(probeCtx)

	rwCore := &c.Core{Shutdown: cancelCtx, Stopped: make(chan struct{}), KafkaClient: nb.kEventClient}
	go rwCore.Start(shutdownCtx, "core-test", cfg)

	return
}

func (nb *NBTest) stopAll(ctx context.Context) {
	if nb.etcdServer != nil {
		StopEmbeddedEtcdServer(ctx, nb.etcdServer)
	}

	if nb.kEventClient != nil {
		nb.kEventClient.Stop(ctx)
	}

	if nb.kafkaBroker != nil {
		nb.kafkaBroker.Close()
	}

	// Stop all grpc clients first
	nb.oltAdaptersLock.Lock()
	if nb.oltAdapters != nil {
		for _, adapterInstances := range nb.oltAdapters {
			for _, instance := range adapterInstances {
				instance.StopGrpcClient()
			}
		}
	}
	nb.oltAdaptersLock.Unlock()
	nb.onuAdaptersLock.Lock()
	if nb.onuAdapters != nil {
		for _, adapterInstances := range nb.onuAdapters {
			for _, instance := range adapterInstances {
				instance.StopGrpcClient()
			}
		}
	}
	nb.onuAdaptersLock.Unlock()

	// Now stop the grpc servers
	nb.oltAdaptersLock.Lock()
	defer nb.oltAdaptersLock.Unlock()
	if nb.oltAdapters != nil {
		for _, adapterInstances := range nb.oltAdapters {
			for _, instance := range adapterInstances {
				instance.Stop()
			}
		}
	}

	nb.onuAdaptersLock.Lock()
	defer nb.onuAdaptersLock.Unlock()
	if nb.onuAdapters != nil {
		for _, adapterInstances := range nb.onuAdapters {
			for _, instance := range adapterInstances {
				instance.Stop()
			}
		}
	}
	if nb.core != nil {
		nb.core.Stop(ctx)
	}
}

func (nb *NBTest) verifyLogicalDevices(t *testing.T, oltDevice *voltha.Device, nbi voltha.VolthaServiceClient) {
	// Get the latest logical device
	logicalDevices, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, logicalDevices)
	var ld *voltha.LogicalDevice
	for _, logicalDevice := range logicalDevices.Items {
		if logicalDevice.RootDeviceId == oltDevice.Id {
			ld = logicalDevice
			break
		}
	}
	assert.NotNil(t, ld)
	ports, err := nbi.ListLogicalDevicePorts(getContext(), &voltha.ID{Id: ld.Id})
	assert.Nil(t, err)

	assert.NotEqual(t, "", ld.Id)
	assert.NotEqual(t, uint64(0), ld.DatapathId)
	assert.Equal(t, "olt_adapter_mock", ld.Desc.HwDesc)
	assert.Equal(t, "olt_adapter_mock", ld.Desc.SwDesc)
	assert.NotEqual(t, "", ld.RootDeviceId)
	assert.NotEqual(t, "", ld.Desc.SerialNum)
	assert.Equal(t, uint32(256), ld.SwitchFeatures.NBuffers)
	assert.Equal(t, uint32(2), ld.SwitchFeatures.NTables)
	assert.Equal(t, uint32(15), ld.SwitchFeatures.Capabilities)
	assert.Equal(t, 1+nb.numONUPerOLT, len(ports.Items))
	assert.Equal(t, oltDevice.ParentId, ld.Id)
	//Expected port no
	expectedPortNo := make(map[uint32]bool)
	expectedPortNo[uint32(2)] = false
	for i := 0; i < nb.numONUPerOLT; i++ {
		expectedPortNo[uint32(i+100)] = false
	}
	for _, p := range ports.Items {
		assert.Equal(t, p.OfpPort.PortNo, p.DevicePortNo)
		assert.Equal(t, uint32(4), p.OfpPort.State)
		expectedPortNo[p.OfpPort.PortNo] = true
		if strings.HasPrefix(p.Id, "nni") {
			assert.Equal(t, true, p.RootPort)
			//assert.Equal(t, uint32(2), p.OfpPort.PortNo)
			assert.Equal(t, p.Id, fmt.Sprintf("nni-%d", p.DevicePortNo))
		} else {
			assert.Equal(t, p.Id, fmt.Sprintf("uni-%d", p.DevicePortNo))
			assert.Equal(t, false, p.RootPort)
		}
	}
}

func (nb *NBTest) verifyDevices(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceID string) {
	// Get the latest set of devices
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)

	// A device is ready to be examined when its ADMIN state is ENABLED and OPERATIONAL state is ACTIVE
	var vFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED && device.OperStatus == voltha.OperStatus_ACTIVE
	}

	var wg sync.WaitGroup
	for _, device := range devices.Items {
		if (device.Root && device.Id != oltDeviceID) || (!device.Root && device.ParentId != oltDeviceID) {
			continue
		}
		wg.Add(1)
		go func(wg *sync.WaitGroup, device *voltha.Device) {
			// Wait until the device is in the right state
			err := waitUntilDeviceReadiness(device.Id, nb.maxTimeout, vFunction, nbi)
			assert.Nil(t, err)

			// Now, verify the details of the device.  First get the latest update
			d, err := nbi.GetDevice(getContext(), &voltha.ID{Id: device.Id})
			assert.Nil(t, err)
			dPorts, err := nbi.ListDevicePorts(getContext(), &voltha.ID{Id: device.Id})
			assert.Nil(t, err)
			assert.Equal(t, voltha.AdminState_ENABLED, d.AdminState)
			assert.Equal(t, voltha.ConnectStatus_REACHABLE, d.ConnectStatus)
			assert.Equal(t, voltha.OperStatus_ACTIVE, d.OperStatus)
			assert.NotEqual(t, "", d.MacAddress)
			assert.NotEqual(t, "", d.SerialNumber)

			if d.Type == "olt_adapter_mock" {
				assert.Equal(t, true, d.Root)
				assert.NotEqual(t, "", d.Id)
				assert.NotEqual(t, "", d.ParentId)
				assert.Nil(t, d.ProxyAddress)
			} else if d.Type == "onu_adapter_mock" {
				assert.Equal(t, false, d.Root)
				assert.NotEqual(t, uint32(0), d.Vlan)
				assert.NotEqual(t, "", d.Id)
				assert.NotEqual(t, "", d.ParentId)
				assert.NotEqual(t, "", d.ProxyAddress.DeviceId)
				assert.Equal(t, "olt_adapter_mock", d.ProxyAddress.DeviceType)
			} else {
				assert.Error(t, errors.New("invalid-device-type"))
			}
			assert.Equal(t, 2, len(dPorts.Items))
			for _, p := range dPorts.Items {
				assert.Equal(t, voltha.AdminState_ENABLED, p.AdminState)
				assert.Equal(t, voltha.OperStatus_ACTIVE, p.OperStatus)
				if p.Type == voltha.Port_ETHERNET_NNI || p.Type == voltha.Port_ETHERNET_UNI {
					assert.Equal(t, 0, len(p.Peers))
				} else if p.Type == voltha.Port_PON_OLT {
					assert.Equal(t, nb.numONUPerOLT, len(p.Peers))
					assert.Equal(t, uint32(1), p.PortNo)
				} else if p.Type == voltha.Port_PON_ONU {
					assert.Equal(t, 1, len(p.Peers))
					assert.Equal(t, uint32(1), p.PortNo)
				} else {
					assert.Error(t, errors.New("invalid-port"))
				}
			}
			wg.Done()
		}(&wg, device)
	}
	wg.Wait()
}

func (nb *NBTest) getChildDevices(parentID string, nbi voltha.VolthaServiceClient) (*voltha.Devices, error) {
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	if err != nil {
		return nil, err
	}
	var childDevice []*voltha.Device
	for _, d := range devices.Items {
		if d.Root != true && d.ParentId == parentID {
			childDevice = append(childDevice, d)
		}
	}
	return &voltha.Devices{Items: childDevice}, nil
}

func (nb *NBTest) testCoreWithoutData(t *testing.T, nbi voltha.VolthaServiceClient) {
	lds, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, lds)
	assert.Equal(t, 0, len(lds.Items))
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, devices)
	assert.Equal(t, 0, len(devices.Items))
	adapters, err := nbi.ListAdapters(getContext(), &empty.Empty{})
	assert.Equal(t, 0, len(adapters.Items))
	assert.Nil(t, err)
	assert.NotNil(t, adapters)
}
func (nb *NBTest) getNumAdapters() int {
	totalAdapters := int32(0)
	for _, aInfo := range onuAdapters {
		totalAdapters = totalAdapters + aInfo.TotalReplica
	}
	for _, aInfo := range oltAdapters {
		totalAdapters = totalAdapters + aInfo.TotalReplica
	}
	return int(totalAdapters)
}

func (nb *NBTest) testAdapterRegistration(t *testing.T, nbi voltha.VolthaServiceClient) {
	ctx := context.Background()
	adapters, err := nbi.ListAdapters(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, adapters)
	assert.Equal(t, nb.getNumAdapters(), len(adapters.Items))
	nb.oltAdaptersLock.RLock()
	defer nb.oltAdaptersLock.RUnlock()
	nb.onuAdaptersLock.RLock()
	defer nb.onuAdaptersLock.RUnlock()
	for _, a := range adapters.Items {
		if strings.Contains(a.Type, "olt") {
			_, exist := nb.oltAdapters[a.Type]
			assert.True(t, exist)
			assert.True(t, strings.Contains(a.Vendor, "olt-mock-vendor"))
		} else if strings.Contains(a.Type, "onu") {
			_, exist := nb.onuAdapters[a.Type]
			assert.True(t, exist)
			assert.True(t, strings.Contains(a.Vendor, "onu-mock-vendor"))
		} else {
			logger.Fatal(ctx, "unregistered-adapter", a.Id)
		}
	}
	deviceTypes, err := nbi.ListDeviceTypes(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, deviceTypes)
	assert.Equal(t, len(nb.oltAdapters)+len(nb.onuAdapters), len(deviceTypes.Items))
	for _, dt := range deviceTypes.Items {
		if strings.Contains(dt.AdapterType, "olt") {
			_, exist := nb.oltAdapters[dt.AdapterType]
			assert.True(t, exist)
			assert.Equal(t, false, dt.AcceptsBulkFlowUpdate)
			assert.Equal(t, true, dt.AcceptsAddRemoveFlowUpdates)
		} else if strings.Contains(dt.AdapterType, "onu") {
			_, exist := nb.onuAdapters[dt.AdapterType]
			assert.True(t, exist)
			assert.Equal(t, false, dt.AcceptsBulkFlowUpdate)
			assert.Equal(t, true, dt.AcceptsAddRemoveFlowUpdates)
		} else {
			logger.Fatal(ctx, "invalid-device-type", dt.Id)
		}
	}
}

func (nb *NBTest) testCreateDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create a valid device
	aRandomMacAddress := getRandomMacAddress()
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: aRandomMacAddress})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	oltD, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, oltD)
	assert.Equal(t, oltDevice.String(), oltD.String())

	// Try to create the same device
	_, err = nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: aRandomMacAddress})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "device is already pre-provisioned"))

	// Try to create a device with invalid data
	_, err = nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "no-device-info-present; MAC or HOSTIP&PORT"))

	// Ensure we still have the previous device still in the core
	createDevice, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, createDevice)

	//Remove the device
	err = cleanUpCreatedDevices(nb.maxTimeout, nbi, oltDevice.Id)
	assert.Nil(t, err)
}

func (nb *NBTest) enableDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDevice *voltha.Device) {
	// Subscribe to the event listener
	eventCh := nb.changeEventLister.Subscribe((nb.numONUPerOLT + 1) * nb.getNumAdapters())
	defer nb.changeEventLister.Unsubscribe(eventCh)

	// Enable the oltDevice
	_, err := nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Create a logical device monitor will automatically send trap and eapol flows to the devices being enables
	var wg sync.WaitGroup
	wg.Add(1)
	go nb.monitorLogicalDevices(t, nbi, 1, nb.numONUPerOLT, &wg, false, false, oltDevice.Id, eventCh)

	// Wait for the logical device to be in the ready state
	var vldFunction = func(ports []*voltha.LogicalPort) bool {
		return len(ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	// Verify that the devices have been setup correctly
	nb.verifyDevices(t, nbi, oltDevice.Id)

	// Get latest oltDevice data
	oltDevice, err = nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Verify that the logical device has been setup correctly
	nb.verifyLogicalDevices(t, oltDevice, nbi)

	// Wait until all flows has been sent to the devices successfully
	wg.Wait()
}

func (nb *NBTest) testForceDeletePreProvDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create a valid device
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: getRandomMacAddress()})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Ensure the device is present
	device, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, oltDevice.String(), device.String())

	//Remove the device forcefully
	_, err = nbi.ForceDeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	err = waitUntilDeviceIsRemoved(nb.maxTimeout, nbi, oltDevice.Id)
	assert.Nil(t, err)
}

func (nb *NBTest) testForceDeleteEnabledDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create a valid device
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: getRandomMacAddress()})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Enable device
	nb.enableDevice(t, nbi, oltDevice)

	//Remove the device forcefully
	_, err = nbi.ForceDeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	err = waitUntilDeviceIsRemoved(nb.maxTimeout, nbi, oltDevice.Id)
	assert.Nil(t, err)
}

func (nb *NBTest) testDeletePreProvDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create a valid device
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: getRandomMacAddress()})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Ensure device is present
	device, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, oltDevice.String(), device.String())

	err = cleanUpCreatedDevice(nb.maxTimeout, nbi, oltDevice.Id)
	assert.Nil(t, err)
}

func (nb *NBTest) testDeleteEnabledDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create a valid device
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: getRandomMacAddress()})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Enable device
	nb.enableDevice(t, nbi, oltDevice)

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	var vFunction isConditionSatisfied = func() bool {
		_, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
		if err != nil {
			return strings.Contains(err.Error(), "NotFound")
		}
		return false
	}

	err = waitUntilCondition(nb.maxTimeout, vFunction)
	assert.Nil(t, err)
}

func (nb *NBTest) testForceDeleteDeviceFailure(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create a valid device
	oltDevice, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: getRandomMacAddress()})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Enable the device
	nb.enableDevice(t, nbi, oltDevice)

	// Set the delete action on the relevant adapter
	oltAdapter, err := nb.getOLTAdapterInstance(t, nbi, oltDevice.Id)
	assert.Nil(t, err)
	oltAdapter.SetDeleteAction(oltDevice.Id, true)

	//Remove the device
	_, err = nbi.ForceDeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	err = waitUntilDeviceIsRemoved(nb.maxTimeout, nbi, oltDevice.Id)
	assert.Nil(t, err)

}

func (nb *NBTest) testDeleteDeviceFailure(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	// Create and enable a OLT device for that device type
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Set the delete action to fail device deletion
	oltAdapter, err := nb.getOLTAdapterInstance(t, nbi, oltDevice.Id)
	assert.Nil(t, err)
	oltAdapter.SetDeleteAction(oltDevice.Id, true)

	// Subscribe and wait asynchronously on the kafka message bus for a delete failure event
	ch := make(chan int, 1)
	eventTopic := &kafka.Topic{Name: nb.config.EventTopic}
	eventChnl, err := nb.kEventClient.Subscribe(getContext(), eventTopic)
	assert.Nil(t, err)
	defer func() {
		if eventChnl != nil {
			err = nb.kEventClient.UnSubscribe(getContext(), eventTopic, eventChnl)
			assert.Nil(t, err)
		}
	}()
	go func() {
		timer := time.NewTimer(nb.internalTimeout)
		defer timer.Stop()
	loop:
		for {
			select {
			case event := <-eventChnl:
				if evnt, ok := event.(*voltha.Event); ok {
					rpcEvent := evnt.GetRpcEvent()
					if rpcEvent != nil && rpcEvent.ResourceId == oltDevice.Id && rpcEvent.Rpc == "DeleteDevice" {
						ch <- 1
						break loop
					}
				}
			case <-timer.C:
				ch <- 0
				break loop
			}
		}
	}()

	//Now remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.NotNil(t, err)

	// Wait for the delete event
	event := <-ch
	assert.Equal(t, 1, event)

	// Set the delete action to pass device deletion
	oltAdapter.SetDeleteAction(oltDevice.Id, false)

	// Now Force Delete this device - needs to be done in a verification function because if the
	// previous failed delete action was not complete then a force delete will return an error
	var forceDeleteComplete isConditionSatisfied = func() bool {
		_, err := nbi.ForceDeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
		return err != nil

	}
	err = waitUntilCondition(nb.maxTimeout, forceDeleteComplete)
	assert.Nil(t, err)

	// Wait until device is gone
	var deviceDeleteComplete isConditionSatisfied = func() bool {
		_, err := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
		if err != nil {
			return strings.Contains(err.Error(), "NotFound")
		}
		return false
	}

	err = waitUntilCondition(nb.maxTimeout, deviceDeleteComplete)
	assert.Nil(t, err)
}

// createAndEnableOLTDevice creates and enables an OLT device. If there is a connection error (e.g. core communication is
// not fully ready or the relevant adapter has not been registered yet) then it will automatically retry on failure.
func (nb *NBTest) createAndEnableOLTDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) (*voltha.Device, error) {
	var oltDevice *voltha.Device
	var err error
	var enableDeviceWithRetry isConditionSatisfied = func() bool {
		// Create device
		oltDevice, err = nbi.CreateDevice(getContext(), &voltha.Device{Type: oltDeviceType, MacAddress: getRandomMacAddress()})
		assert.Nil(t, err)
		assert.NotNil(t, oltDevice)

		// Verify oltDevice exist in the core
		devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
		assert.Nil(t, err)
		exist := false
		for _, d := range devices.Items {
			if d.Id == oltDevice.Id {
				exist = true
				break
			}
		}
		assert.True(t, true, exist)
		_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
		if err == nil {
			return true
		}
		_, _ = nbi.ForceDeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
		assert.Nil(t, err)

		return false
	}
	err = waitUntilCondition(nb.maxTimeout, enableDeviceWithRetry)
	assert.Nil(t, err)

	// Wait for device to be fully enabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED &&
			device.OperStatus == voltha.OperStatus_ACTIVE &&
			device.ConnectStatus == voltha.ConnectStatus_REACHABLE
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Wait until all relevant ONUs are enabled and ready
	numOnuPerOlt := cm.GetNumONUPerOLT()
	var onusReady isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		if devices == nil || len(devices.Items) < numOnuPerOlt+1 {
			return false
		}
		count := 0
		for _, d := range devices.Items {
			if !d.Root && d.ParentId == oltDevice.Id {
				if d.AdminState == voltha.AdminState_ENABLED &&
					d.OperStatus == voltha.OperStatus_ACTIVE &&
					d.ConnectStatus == voltha.ConnectStatus_REACHABLE {
					count = count + 1
				}
			}
		}
		return count >= numOnuPerOlt
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, onusReady)
	assert.Nil(t, err)

	return oltDevice, err
}

func (nb *NBTest) testEnableDeviceFailed(t *testing.T, nbi voltha.VolthaServiceClient) {
	//Create a device that has no adapter registered
	macAddress := getRandomMacAddress()
	oltDeviceNoAdapter, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: "noAdapterRegistered", MacAddress: macAddress})
	assert.Nil(t, err)
	assert.NotNil(t, oltDeviceNoAdapter)

	// Try to enable the oltDevice and check the error message
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDeviceNoAdapter.Id})
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "adapter-not-registered-for-device-type noAdapterRegistered"))

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDeviceNoAdapter.Id})
	assert.Nil(t, err)

	// Wait until device is removed
	err = waitUntilDeviceIsRemoved(nb.maxTimeout, nbi, oltDeviceNoAdapter.Id)
	assert.Nil(t, err)
}

func (nb *NBTest) testEnableDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	// Subscribe to the event listener
	eventCh := nb.changeEventLister.Subscribe((nb.numONUPerOLT + 1) * nb.getNumAdapters())

	defer nb.changeEventLister.Unsubscribe(eventCh)

	// Create and enable a OLT device for that device type
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	//Create a logical device monitor will automatically send trap and eapol flows to the devices being enables
	var wg sync.WaitGroup
	wg.Add(1)
	go nb.monitorLogicalDevices(t, nbi, 1, nb.numONUPerOLT, &wg, false, false, oltDevice.Id, eventCh)

	// Wait for the logical device to be in the ready state
	var vldFunction = func(ports []*voltha.LogicalPort) bool {
		return len(ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	// Verify that the devices have been setup correctly
	nb.verifyDevices(t, nbi, oltDevice.Id)

	// Get latest oltDevice data
	oltDevice, err = nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Verify that the logical device has been setup correctly
	nb.verifyLogicalDevices(t, oltDevice, nbi)

	//Wait until all flows has been sent to the devices successfully
	wg.Wait()

	// log.SetAllLogLevel(log.DebugLevel)
	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, false)
	assert.Nil(t, err)
}

func (nb *NBTest) testDisableAndReEnableRootDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	// Create and enable an OLT device
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Wait until all ONU devices have been created and enabled

	// Disable the oltDevice
	_, err = nbi.DisableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the old device to be disabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_DISABLED && device.OperStatus == voltha.OperStatus_UNKNOWN
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Verify that all onu devices are disabled as well
	onuDevices, err := nb.getChildDevices(oltDevice.Id, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Greater(t, len(onuDevices.Items), 0)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Wait for the logical device to satisfy the expected condition
	var vlFunction = func(ports []*voltha.LogicalPort) bool {
		for _, lp := range ports {
			if (lp.OfpPort.Config&uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LINK_DOWN) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	// Reenable the oltDevice
	_, err = nbi.EnableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the old device to be enabled
	vdFunction = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_ENABLED && device.OperStatus == voltha.OperStatus_ACTIVE
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Verify that all onu devices are enabled as well
	onuDevices, err = nb.getChildDevices(oltDevice.Id, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Greater(t, len(onuDevices.Items), 0)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Wait for the logical device to satisfy the expected condition
	vlFunction = func(ports []*voltha.LogicalPort) bool {
		for _, lp := range ports {
			if (lp.OfpPort.Config&^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LIVE) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func (nb *NBTest) testDisableAndDeleteAllDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//Get an OLT device
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Disable the oltDevice
	_, err = nbi.DisableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the olt device to be disabled
	var vdFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_DISABLED && device.OperStatus == voltha.OperStatus_UNKNOWN
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)

	// Verify that all onu devices are disabled as well (previous test may have removed all ONUs)
	onuDevices, err := nb.getChildDevices(oltDevice.Id, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.GreaterOrEqual(t, len(onuDevices.Items), 0)
	for _, onu := range onuDevices.Items {
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunction, nbi)
		assert.Nil(t, err)
	}

	// Delete the oltDevice
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Verify all devices relevant to the OLT device are gone
	var vFunction isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		if (devices == nil) || len(devices.Items) == 0 {
			return true
		}
		for _, d := range devices.Items {
			if (d.Root && d.Id == oltDevice.Id) || (!d.Root && d.ParentId == oltDevice.Id) {
				return false
			}
		}
		return true
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)

	// Wait for absence of logical device
	var vlFunction isLogicalDevicesConditionSatisfied = func(lds *voltha.LogicalDevices) bool {
		if (lds == nil) || (len(lds.Items) == 0) {
			return true
		}
		for _, ld := range lds.Items {
			if ld.RootDeviceId == oltDevice.Id {
				return false
			}
		}
		return true
	}
	err = waitUntilConditionForLogicalDevices(nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func (nb *NBTest) testEnableAndDeleteAllDevice(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//Create/Enable an OLT device
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Wait for the logical device to be in the ready state
	var vldFunction = func(ports []*voltha.LogicalPort) bool {
		return len(ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	//Get all child devices
	onuDevices, err := nb.getChildDevices(oltDevice.Id, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Greater(t, len(onuDevices.Items), 0)

	// Wait for each onu device to get deleted
	var vdFunc isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device == nil
	}

	// Delete the onuDevice
	for _, onu := range onuDevices.Items {
		_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: onu.Id})
		assert.Nil(t, err)
		err = waitUntilDeviceReadiness(onu.Id, nb.maxTimeout, vdFunc, nbi)
		assert.Nil(t, err)
	}

	// Disable the oltDevice
	_, err = nbi.DisableDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Wait for the olt device to be disabled
	var vFunction isDeviceConditionSatisfied = func(device *voltha.Device) bool {
		return device.AdminState == voltha.AdminState_DISABLED && device.OperStatus == voltha.OperStatus_UNKNOWN
	}
	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vFunction, nbi)
	assert.Nil(t, err)

	// Delete the oltDevice
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Cleanup
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func (nb *NBTest) testDisableAndEnablePort(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//Create an OLT device
	var cp *voltha.Port
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	oltPorts, err := nbi.ListDevicePorts(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	for _, cp = range oltPorts.Items {
		if cp.Type == voltha.Port_PON_OLT {
			break
		}

	}
	assert.NotNil(t, cp)
	cp.DeviceId = oltDevice.Id

	// Disable the NW Port of oltDevice
	_, err = nbi.DisablePort(getContext(), cp)
	assert.Nil(t, err)
	// Wait for the olt device Port  to be disabled
	var vdFunction isDevicePortsConditionSatisfied = func(ports *voltha.Ports) bool {
		for _, port := range ports.Items {
			if port.PortNo == cp.PortNo {
				return port.AdminState == voltha.AdminState_DISABLED
			}
		}
		return false
	}
	err = waitUntilDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)
	// Wait for the logical device to satisfy the expected condition
	var vlFunction = func(ports []*voltha.LogicalPort) bool {
		for _, lp := range ports {
			if (lp.OfpPort.Config&^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LIVE) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	// Enable the NW Port of oltDevice
	_, err = nbi.EnablePort(getContext(), cp)
	assert.Nil(t, err)

	// Wait for the olt device Port to be enabled
	vdFunction = func(ports *voltha.Ports) bool {
		for _, port := range ports.Items {
			if port.PortNo == cp.PortNo {
				return port.AdminState == voltha.AdminState_ENABLED
			}
		}
		return false
	}
	err = waitUntilDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, vdFunction, nbi)
	assert.Nil(t, err)
	// Wait for the logical device to satisfy the expected condition
	vlFunction = func(ports []*voltha.LogicalPort) bool {
		for _, lp := range ports {
			if (lp.OfpPort.Config&^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN) != lp.OfpPort.Config) ||
				lp.OfpPort.State != uint32(ofp.OfpPortState_OFPPS_LIVE) {
				return false
			}
		}
		return true
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	// Disable a non-PON port
	for _, cp = range oltPorts.Items {
		if cp.Type != voltha.Port_PON_OLT {
			break
		}

	}
	assert.NotNil(t, cp)
	cp.DeviceId = oltDevice.Id

	// Disable the NW Port of oltDevice
	_, err = nbi.DisablePort(getContext(), cp)
	assert.NotNil(t, err)

	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func (nb *NBTest) testDeviceRebootWhenOltIsEnabled(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//Create an OLT device
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Reboot the OLT and very that Connection Status goes to UNREACHABLE and operation status to UNKNOWN
	_, err = nbi.RebootDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	var vlFunction0 = func(d *voltha.Device) bool {
		return d.ConnectStatus == voltha.ConnectStatus_UNREACHABLE && d.OperStatus == voltha.OperStatus_UNKNOWN
	}

	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vlFunction0, nbi)
	assert.Nil(t, err)

	oltAdapter, err := nb.getOLTAdapterInstance(t, nbi, oltDevice.Id)
	assert.Nil(t, err)

	oltAdapter.SetDeviceRebooted(oltDevice.Id)

	var vlFunctionreb = func(d *voltha.Device) bool {
		return d.ConnectStatus == voltha.ConnectStatus_REACHABLE && d.OperStatus == voltha.OperStatus_REBOOTED
	}

	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vlFunctionreb, nbi)
	assert.Nil(t, err)

	// Wait for the logical device to satisfy the expected condition
	var vlFunction1 = func(ld *voltha.LogicalDevice) bool {
		return ld == nil
	}

	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction1)
	assert.Nil(t, err)

	// Wait for the device to satisfy the expected condition (device does not have flows)
	var vlFunction2 = func(d *voltha.Device) bool {
		var deviceFlows *ofp.Flows
		var err error
		if deviceFlows, err = nbi.ListDeviceFlows(getContext(), &voltha.ID{Id: d.Id}); err != nil {
			return false
		}
		return len(deviceFlows.Items) == 0
	}

	err = waitUntilDeviceReadiness(oltDevice.Id, nb.maxTimeout, vlFunction2, nbi)
	assert.Nil(t, err)

	// Wait for the device to satisfy the expected condition (there are no child devices)
	var vlFunction3 isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		if (devices == nil) || (len(devices.Items) == 0) {
			return false
		}
		for _, d := range devices.Items {
			if !d.Root && d.ParentId == oltDevice.Id {
				return false
			}
		}
		return true
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vlFunction3)
	assert.Nil(t, err)

	// Update the OLT Connection Status to REACHABLE and operation status to ACTIVE
	// Normally, in a real adapter this happens after connection regain via a heartbeat mechanism with real hardware
	oltAdapter, err = nb.getOLTAdapterInstance(t, nbi, oltDevice.Id)
	assert.Nil(t, err)
	oltAdapter.SetDeviceActive(oltDevice.Id)

	// Verify the device connection and operation states
	oltDevice, err = nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)
	assert.Equal(t, oltDevice.ConnectStatus, voltha.ConnectStatus_REACHABLE)
	assert.Equal(t, oltDevice.AdminState, voltha.AdminState_ENABLED)

	// Wait for the logical device to satisfy the expected condition
	var vlFunction4 = func(ld *voltha.LogicalDevice) bool {
		return ld != nil
	}
	err = waitUntilLogicalDeviceReadiness(oltDevice.Id, nb.maxTimeout, nbi, vlFunction4)
	assert.Nil(t, err)

	// Verify that we have no ONUs
	onuDevices, err := nb.getChildDevices(oltDevice.Id, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Equal(t, 0, len(onuDevices.Items))
	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func (nb *NBTest) testStartOmciTestAction(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	// -----------------------------------------------------------------------
	// SubTest 1: Omci test action should fail due to nonexistent device id

	request := &omci.OmciTestRequest{Id: "123", Uuid: "456"}
	_, err := nbi.StartOmciTestAction(getContext(), request)
	assert.NotNil(t, err)
	assert.Equal(t, "rpc error: code = NotFound desc = 123", err.Error())

	// -----------------------------------------------------------------------
	// SubTest 2: Error should be returned for device with no adapter registered

	// Create a device that has no adapter registered
	deviceNoAdapter, err := nbi.CreateDevice(getContext(), &voltha.Device{Type: "noAdapterRegisteredOmciTest", MacAddress: getRandomMacAddress()})
	assert.Nil(t, err)
	assert.NotNil(t, deviceNoAdapter)

	// Omci test action should fail due to nonexistent adapter
	request = &omci.OmciTestRequest{Id: deviceNoAdapter.Id, Uuid: "456"}
	_, err = nbi.StartOmciTestAction(getContext(), request)
	assert.NotNil(t, err)
	assert.True(t, strings.Contains(err.Error(), "noAdapterRegisteredOmciTest"))

	//Remove the device
	_, err = nbi.DeleteDevice(getContext(), &voltha.ID{Id: deviceNoAdapter.Id})
	assert.Nil(t, err)

	//Ensure there are no devices in the Core now - wait until condition satisfied or timeout
	var vFunction isDevicesConditionSatisfied = func(devices *voltha.Devices) bool {
		if (devices == nil) || (len(devices.Items) == 0) {
			return true
		}
		for _, d := range devices.Items {
			if (d.Root && d.Id == deviceNoAdapter.Id) || (!d.Root && d.ParentId == deviceNoAdapter.Id) {
				return false
			}
		}
		return true
	}
	err = waitUntilConditionForDevices(nb.maxTimeout, nbi, vFunction)
	assert.Nil(t, err)

	// -----------------------------------------------------------------------
	// SubTest 3: Omci test action should succeed on valid ONU

	//	Create and enable device with valid data
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Wait for the logical device to be in the ready state
	var vldFunction = func(ports []*voltha.LogicalPort) bool {
		return len(ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	onuDevices, err := nb.getChildDevices(oltDevice.Id, nbi)
	assert.Nil(t, err)
	assert.NotNil(t, onuDevices)
	assert.Greater(t, len(onuDevices.Items), 0)

	onuDevice := onuDevices.Items[0]

	// Omci test action should succeed
	request = &omci.OmciTestRequest{Id: onuDevice.Id, Uuid: "456"}
	resp, err := nbi.StartOmciTestAction(getContext(), request)
	assert.Nil(t, err)
	assert.Equal(t, resp.Result, omci.TestResponse_SUCCESS)

	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, false)
	assert.Nil(t, err)
}

func makeSimpleFlowMod(fa *flows.FlowArgs) *ofp.OfpFlowMod {
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return flows.MkSimpleFlowMod(matchFields, fa.Actions, fa.Command, fa.KV)
}

func createMetadata(cTag int, techProfile int, port int) uint64 {
	md := 0
	md = (md | (cTag & 0xFFFF)) << 16
	md = (md | (techProfile & 0xFFFF)) << 32
	return uint64(md | (port & 0xFFFFFFFF))
}

func (nb *NBTest) verifyLogicalDeviceFlowCount(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceID string, numNNIPorts int, numUNIPorts int, flowAddFail bool) {
	expectedNumFlows := numNNIPorts*3 + numNNIPorts*numUNIPorts
	if flowAddFail {
		expectedNumFlows = 0
	}

	// Wait for logical device to have the flows (or none
	var vlFunction isLogicalDeviceConditionSatisfied = func(ld *voltha.LogicalDevice) bool {
		f, _ := nbi.ListLogicalDeviceFlows(getContext(), &voltha.ID{Id: ld.Id})
		return f != nil && len(f.Items) == expectedNumFlows
	}
	// No timeout implies a success
	err := waitUntilLogicalDeviceReadiness(oltDeviceID, nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)
}

func (nb *NBTest) sendTrapFlows(t *testing.T, nbi voltha.VolthaServiceClient, logicalDeviceID string, ports []*voltha.LogicalPort) (numNNIPorts, numUNIPorts int) {
	// Send flows for the parent device
	var nniPorts []*voltha.LogicalPort
	var uniPorts []*voltha.LogicalPort
	for _, p := range ports {
		if p.RootPort {
			nniPorts = append(nniPorts, p)
		} else {
			uniPorts = append(uniPorts, p)
		}
	}
	assert.Equal(t, 1, len(nniPorts))
	//assert.Greater(t, len(uniPorts), 1 )
	nniPort := nniPorts[0].OfpPort.PortNo
	maxInt32 := uint64(0xFFFFFFFF)
	controllerPortMask := uint32(4294967293) // will result in 4294967293&0x7fffffff => 2147483645 which is the actual controller port
	var fa *flows.FlowArgs
	fa = &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(nniPort),
			flows.EthType(35020),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowLLDP := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDeviceID}
	_, err := nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowLLDP)
	assert.Nil(t, err)

	fa = &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(nniPort),
			flows.EthType(2048),
			flows.IpProto(17),
			flows.UdpSrc(67),
			flows.UdpDst(68),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowIPV4 := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDeviceID}
	_, err = nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowIPV4)
	assert.Nil(t, err)

	fa = &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(nniPort),
			flows.EthType(34525),
			flows.IpProto(17),
			flows.UdpSrc(546),
			flows.UdpDst(547),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowIPV6 := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDeviceID}
	_, err = nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowIPV6)
	assert.Nil(t, err)

	return len(nniPorts), len(uniPorts)
}

func (nb *NBTest) sendEAPFlows(t *testing.T, nbi voltha.VolthaServiceClient, logicalDeviceID string, port *ofp.OfpPort, vlan int, meterID uint64) {
	maxInt32 := uint64(0xFFFFFFFF)
	controllerPortMask := uint32(4294967293) // will result in 4294967293&0x7fffffff => 2147483645 which is the actual controller port
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 10000, "buffer_id": maxInt32, "out_port": maxInt32, "out_group": maxInt32, "flags": 1, "write_metadata": createMetadata(vlan, 64, 0), "meter_id": meterID},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(port.PortNo),
			flows.EthType(34958),
			flows.VlanVid(8187),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(controllerPortMask),
		},
	}
	flowEAP := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: logicalDeviceID}
	maxTries := 3
	var err error
	for {
		if _, err = nbi.UpdateLogicalDeviceFlowTable(getContext(), &flowEAP); err == nil {
			if maxTries < 3 {
				t.Log("Re-sending EAPOL flow succeeded for port:", port)
			}
			break
		}
		t.Log("Sending EAPOL flows fail:", err)
		time.Sleep(50 * time.Millisecond)
		maxTries--
		if maxTries == 0 {
			break
		}
	}
	assert.Nil(t, err)
}

func (nb *NBTest) receiveChangeEvents(ctx context.Context, nbi voltha.VolthaServiceClient, ch chan *ofp.ChangeEvent) {
	opt := grpc.EmptyCallOption{}
	streamCtx, streamDone := context.WithCancel(log.WithSpanFromContext(context.Background(), ctx))
	defer streamDone()
	stream, err := nbi.ReceiveChangeEvents(streamCtx, &empty.Empty{}, opt)
	if err != nil {
		logger.Errorw(ctx, "cannot-establish-receive-change-events", log.Fields{"error": err})
		return
	}

	for {
		ce, err := stream.Recv()
		if err == nil {
			ch <- ce
			continue
		}
		if err == io.EOF || strings.Contains(err.Error(), "Unavailable") {
			logger.Debug(context.Background(), "receive-events-stream-closing")
		} else {
			logger.Errorw(ctx, "error-receiving-change-event", log.Fields{"error": err})
		}
		return
	}
}

func (nb *NBTest) getOLTAdapterInstance(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceID string) (*cm.OLTAdapter, error) {
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	nb.oltAdaptersLock.RLock()
	defer nb.oltAdaptersLock.RUnlock()
	for _, d := range devices.Items {
		if d.Id == oltDeviceID {
			for _, oltAdapters := range nb.oltAdapters {
				for _, oAdapter := range oltAdapters {
					if oAdapter.Adapter.GetEndPoint() == d.AdapterEndpoint {
						return oAdapter, nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("no-adapter-for-%s", oltDeviceID)
}

func (nb *NBTest) getAdapterInstancesWithDeviceIds(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceID string) (*cm.OLTAdapter, map[string]*cm.ONUAdapter, []string, error) {
	var oltAdapter *cm.OLTAdapter
	onuAdapters := make(map[string]*cm.ONUAdapter)
	devices, err := nbi.ListDevices(getContext(), &empty.Empty{})
	onuDeviceIDs := make([]string, 0)
	assert.Nil(t, err)
	oltAdapterFound := false
	nb.oltAdaptersLock.RLock()
	defer nb.oltAdaptersLock.RUnlock()
	nb.onuAdaptersLock.RLock()
	defer nb.onuAdaptersLock.RUnlock()
	for _, d := range devices.Items {
		if !oltAdapterFound && d.Id == oltDeviceID {
			for _, oltAdapters := range nb.oltAdapters {
				for _, oAdapter := range oltAdapters {
					if oAdapter.Adapter.GetEndPoint() == d.AdapterEndpoint {
						oltAdapter = oAdapter
						oltAdapterFound = true
					}
				}
			}
		}
		// We can have multiple ONU adapters managing the ONU devices off an OLT
		if !d.Root && d.ParentId == oltDeviceID {
			onuDeviceIDs = append(onuDeviceIDs, d.Id)
			for _, adapters := range nb.onuAdapters {
				for _, oAdapter := range adapters {
					if oAdapter.Adapter.GetEndPoint() == d.AdapterEndpoint {
						onuAdapters[d.AdapterEndpoint] = oAdapter
					}
				}
			}
		}
	}
	if len(onuAdapters) > 0 && oltAdapter != nil && len(onuDeviceIDs) > 0 {
		return oltAdapter, onuAdapters, onuDeviceIDs, nil
	}
	return nil, nil, nil, fmt.Errorf("no-adapter-for-%s", oltDeviceID)
}

func (nb *NBTest) monitorLogicalDevices(
	t *testing.T,
	nbi voltha.VolthaServiceClient,
	numNNIPorts int,
	numUNIPorts int,
	wg *sync.WaitGroup,
	flowAddFail bool,
	flowDeleteFail bool,
	oltID string,
	eventCh chan *ofp.ChangeEvent) {

	defer wg.Done()

	// Wait until a logical device is ready
	var vlFunction isLogicalDevicesConditionSatisfied = func(lds *voltha.LogicalDevices) bool {
		if lds == nil || len(lds.Items) == 0 {
			return false
		}
		// Ensure there are both NNI ports and at least one UNI port on the logical devices discovered
		for _, ld := range lds.Items {
			if ld.RootDeviceId != oltID {
				continue
			}
			ports, err := nbi.ListLogicalDevicePorts(getContext(), &voltha.ID{Id: ld.Id})
			if err != nil {
				return false
			}
			return len(ports.Items) == numNNIPorts+numUNIPorts // wait until all logical ports are created
		}
		return false
	}
	err := waitUntilConditionForLogicalDevices(nb.maxTimeout, nbi, vlFunction)
	assert.Nil(t, err)

	logicalDevices, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, logicalDevices)
	var logicalDevice *voltha.LogicalDevice
	for _, ld := range logicalDevices.Items {
		if ld.RootDeviceId == oltID {
			logicalDevice = ld
			break
		}
	}
	assert.NotNil(t, logicalDevice)
	logicalDeviceID := logicalDevice.Id

	// Figure out the olt and onuAdapter being used by that logicalDeviceld\DeviceId
	// Clear any existing flows on these adapters
	oltAdapter, onuAdapters, onuDeviceIDs, err := nb.getAdapterInstancesWithDeviceIds(t, nbi, oltID)
	assert.Nil(t, err)
	assert.NotNil(t, oltAdapter)
	assert.Greater(t, len(onuAdapters), 0)

	// Clear flows for that olt device and set the flow action
	oltAdapter.RemoveDevice(oltID)
	oltAdapter.SetFlowAction(oltID, flowAddFail, flowDeleteFail)

	// Clear flows for the onu devices and set the flow action
	for _, a := range onuAdapters {
		for _, id := range onuDeviceIDs {
			a.RemoveDevice(id)
			a.SetFlowAction(id, flowAddFail, flowDeleteFail)
		}
	}

	meterID := rand.Uint32()

	// Add a meter to the logical device
	meterMod := &ofp.OfpMeterMod{
		Command: ofp.OfpMeterModCommand_OFPMC_ADD,
		Flags:   rand.Uint32(),
		MeterId: meterID,
		Bands: []*ofp.OfpMeterBandHeader{
			{Type: ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER,
				Rate:      rand.Uint32(),
				BurstSize: rand.Uint32(),
				Data:      nil,
			},
		},
	}
	_, err = nbi.UpdateLogicalDeviceMeterTable(getContext(), &ofp.MeterModUpdate{Id: logicalDeviceID, MeterMod: meterMod})
	assert.Nil(t, err)

	ports, err := nbi.ListLogicalDevicePorts(getContext(), &voltha.ID{Id: logicalDeviceID})
	assert.Nil(t, err)

	// Send initial set of Trap flows
	startingVlan := 4091
	nb.sendTrapFlows(t, nbi, logicalDeviceID, ports.Items)

	//Listen for port events
	processedNniLogicalPorts := 0
	processedUniLogicalPorts := 0

	for event := range eventCh {
		if event.Id != logicalDeviceID {
			continue
		}
		startingVlan++
		if portStatus, ok := (event.Event).(*ofp.ChangeEvent_PortStatus); ok {
			ps := portStatus.PortStatus
			if ps.Reason == ofp.OfpPortReason_OFPPR_ADD {
				if ps.Desc.PortNo >= uint32(nb.startingUNIPortNo) {
					processedUniLogicalPorts++
					nb.sendEAPFlows(t, nbi, logicalDeviceID, ps.Desc, startingVlan, uint64(meterID))
				} else {
					processedNniLogicalPorts++
				}
			}
		}

		if processedNniLogicalPorts >= numNNIPorts && processedUniLogicalPorts >= numUNIPorts {
			break
		}
	}

	//Verify the flow count on the logical device
	nb.verifyLogicalDeviceFlowCount(t, nbi, oltID, numNNIPorts, numUNIPorts, flowAddFail)

	// Wait until all flows have been sent to the OLT adapters (or all failed)
	expectedFlowCount := (numNNIPorts * 3) + numNNIPorts*numUNIPorts
	if flowAddFail {
		expectedFlowCount = 0
	}
	var oltVFunc isConditionSatisfied = func() bool {
		return oltAdapter.GetFlowCount(oltID) >= expectedFlowCount
	}
	err = waitUntilCondition(nb.maxTimeout, oltVFunc)
	assert.Nil(t, err)

	// Wait until all flows have been sent to the ONU adapters (or all failed)
	expectedFlowCount = numUNIPorts
	if flowAddFail {
		expectedFlowCount = 0
	}
	var onuVFunc isConditionSatisfied = func() bool {
		count := 0
		for _, a := range onuAdapters {
			for _, id := range onuDeviceIDs {
				count = count + a.GetFlowCount(id)
			}
		}
		return count == expectedFlowCount
	}
	err = waitUntilCondition(nb.maxTimeout, onuVFunc)
	assert.Nil(t, err)
}

func (nb *NBTest) testFlowAddFailure(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	// Subscribe to the event listener
	eventCh := nb.changeEventLister.Subscribe((nb.numONUPerOLT + 1) * nb.getNumAdapters())

	defer nb.changeEventLister.Unsubscribe(eventCh)

	//	Create and enable device with valid data
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Create a logical device monitor will automatically send trap and eapol flows to the devices being enables
	var wg sync.WaitGroup
	wg.Add(1)
	go nb.monitorLogicalDevices(t, nbi, 1, nb.numONUPerOLT, &wg, true, false, oltDevice.Id, eventCh)

	// Wait for the logical device to be in the ready state
	var vldFunction = func(ports []*voltha.LogicalPort) bool {
		return len(ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	// Verify that the devices have been setup correctly
	nb.verifyDevices(t, nbi, oltDevice.Id)

	// Get latest oltDevice data
	oltDevice, err = nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)

	// Verify that the logical device has been setup correctly
	nb.verifyLogicalDevices(t, oltDevice, nbi)

	// Wait until all flows has been sent to the devices successfully
	wg.Wait()

	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func (nb *NBTest) testMPLSFlowsAddition(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string) {
	//	Create and enable device with valid data
	oltDevice, err := nb.createAndEnableOLTDevice(t, nbi, oltDeviceType)
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)

	// Wait for the logical device to be in the ready state
	var vldFunction = func(ports []*voltha.LogicalPort) bool {
		return len(ports) == nb.numONUPerOLT+1
	}
	err = waitUntilLogicalDevicePortsReadiness(oltDevice.Id, nb.maxTimeout, nbi, vldFunction)
	assert.Nil(t, err)

	// Get latest oltDevice data
	oltDevice, err = nbi.GetDevice(getContext(), &voltha.ID{Id: oltDevice.Id})
	assert.Nil(t, err)
	assert.NotNil(t, oltDevice)
	testLogger.Infow(getContext(), "olt-device-created-and-verified", log.Fields{"device-id": oltDevice.GetId()})

	// Verify that the logical device has been setup correctly
	nb.verifyLogicalDevices(t, oltDevice, nbi)

	logicalDevices, err := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
	assert.Nil(t, err)
	assert.NotNil(t, logicalDevices)
	var logicalDevice *voltha.LogicalDevice
	for _, ld := range logicalDevices.Items {
		if ld.RootDeviceId == oltDevice.Id {
			logicalDevice = ld
			break
		}
	}
	assert.NotNil(t, logicalDevice)

	testLogger.Infow(getContext(), "list-logical-devices", log.Fields{"logical-device": logicalDevice})
	// Add a meter to the logical device, which the flow can refer to
	meterMod := &ofp.OfpMeterMod{
		Command: ofp.OfpMeterModCommand_OFPMC_ADD,
		Flags:   rand.Uint32(),
		MeterId: 1,
		Bands: []*ofp.OfpMeterBandHeader{
			{Type: ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER,
				Rate:      rand.Uint32(),
				BurstSize: rand.Uint32(),
				Data:      nil,
			},
		},
	}
	_, err = nbi.UpdateLogicalDeviceMeterTable(getContext(), &ofp.MeterModUpdate{
		Id:       logicalDevices.GetItems()[0].GetId(),
		MeterMod: meterMod,
	})
	assert.NoError(t, err)

	meters, err := nbi.ListLogicalDeviceMeters(getContext(), &voltha.ID{Id: logicalDevice.Id})
	assert.NoError(t, err)

	for _, item := range meters.GetItems() {
		testLogger.Infow(getContext(), "list-logical-device-meters", log.Fields{"meter-config": item.GetConfig()})
	}

	logicalPorts, err := nbi.ListLogicalDevicePorts(context.Background(), &voltha.ID{Id: logicalDevice.Id})
	assert.NoError(t, err)
	m := jsonpb.Marshaler{}
	logicalPortsJson, err := m.MarshalToString(logicalPorts)
	assert.NoError(t, err)

	testLogger.Infow(getContext(), "list-logical-ports", log.Fields{"ports": logicalPortsJson})

	callables := []func() *ofp.OfpFlowMod{getOnuUpstreamRules, getOltUpstreamRules, getOLTDownstreamMplsSingleTagRules,
		getOLTDownstreamMplsDoubleTagRules, getOLTDownstreamRules, getOnuDownstreamRules}

	for _, callable := range callables {
		_, err = nbi.UpdateLogicalDeviceFlowTable(getContext(), &ofp.FlowTableUpdate{Id: logicalDevice.Id, FlowMod: callable()})
		assert.NoError(t, err)
	}

	//Remove the device
	err = cleanUpDevices(nb.maxTimeout, nbi, oltDevice.Id, true)
	assert.Nil(t, err)
}

func getOnuUpstreamRules() (flowMod *ofp.OfpFlowMod) {
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 1000, "table_id": 1, "meter_id": 1, "write_metadata": 4100100000},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(103),
			flows.VlanVid(4096),
		},
		Actions: []*ofp.OfpAction{},
	}

	flowMod = makeSimpleFlowMod(fa)
	flowMod.TableId = 0
	m := jsonpb.Marshaler{}
	flowModJson, _ := m.MarshalToString(flowMod)
	testLogger.Infow(getContext(), "onu-upstream-flow", log.Fields{"flow-mod": flowModJson})
	return
}

func getOltUpstreamRules() (flowMod *ofp.OfpFlowMod) {
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 1000, "table_id": 1, "meter_id": 1, "write_metadata": 4100000000},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(103),
			flows.VlanVid(4096),
		},
		Actions: []*ofp.OfpAction{
			flows.PushVlan(0x8100),
			flows.SetField(flows.VlanVid(2)),
			flows.SetField(flows.EthSrc(1111)),
			flows.SetField(flows.EthDst(2222)),
			flows.PushVlan(0x8847),
			flows.SetField(flows.MplsLabel(100)),
			flows.SetField(flows.MplsBos(1)),
			flows.PushVlan(0x8847),
			flows.SetField(flows.MplsLabel(200)),
			flows.MplsTtl(64),
			flows.Output(2),
		},
	}
	flowMod = makeSimpleFlowMod(fa)
	flowMod.TableId = 1
	m := jsonpb.Marshaler{}
	flowModJson, _ := m.MarshalToString(flowMod)
	testLogger.Infow(getContext(), "olt-upstream-flow", log.Fields{"flow-mod": flowModJson})
	return
}

func getOLTDownstreamMplsSingleTagRules() (flowMod *ofp.OfpFlowMod) {
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 1000, "table_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(2),
			flows.Metadata_ofp((1000 << 32) | 1),
			flows.EthType(0x8847),
			flows.MplsBos(1),
			flows.EthSrc(2222),
		},
		Actions: []*ofp.OfpAction{
			{Type: ofp.OfpActionType_OFPAT_DEC_MPLS_TTL, Action: &ofp.OfpAction_MplsTtl{MplsTtl: &ofp.OfpActionMplsTtl{MplsTtl: 62}}},
			flows.PopMpls(0x8847),
		},
	}
	flowMod = makeSimpleFlowMod(fa)
	flowMod.TableId = 0
	m := jsonpb.Marshaler{}
	flowModJson, _ := m.MarshalToString(flowMod)
	testLogger.Infow(getContext(), "olt-mpls-downstream-single-tag-flow", log.Fields{"flow-mod": flowModJson})
	return
}

func getOLTDownstreamMplsDoubleTagRules() (flowMod *ofp.OfpFlowMod) {
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 1000, "table_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(2),
			flows.EthType(0x8847),
			flows.EthSrc(2222),
		},
		Actions: []*ofp.OfpAction{
			{Type: ofp.OfpActionType_OFPAT_DEC_MPLS_TTL, Action: &ofp.OfpAction_MplsTtl{MplsTtl: &ofp.OfpActionMplsTtl{MplsTtl: 62}}},
			flows.PopMpls(0x8847),
			flows.PopMpls(0x8847),
		},
	}
	flowMod = makeSimpleFlowMod(fa)
	flowMod.TableId = 0
	m := jsonpb.Marshaler{}
	flowModJson, _ := m.MarshalToString(flowMod)
	testLogger.Infow(getContext(), "olt-mpls-downstream-double-tagged-flow", log.Fields{"flow-mod": flowModJson})
	return
}

func getOLTDownstreamRules() (flowMod *ofp.OfpFlowMod) {
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 1000, "table_id": 2, "meter_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(2),
			flows.VlanVid(2),
		},
		Actions: []*ofp.OfpAction{
			flows.PopVlan(),
		},
	}
	flowMod = makeSimpleFlowMod(fa)
	flowMod.TableId = 1
	m := jsonpb.Marshaler{}
	flowModJson, _ := m.MarshalToString(flowMod)
	testLogger.Infow(getContext(), "olt-downstream-flow", log.Fields{"flow-mod": flowModJson})
	return
}

func getOnuDownstreamRules() (flowMod *ofp.OfpFlowMod) {
	fa := &flows.FlowArgs{
		KV: flows.OfpFlowModArgs{"priority": 1000, "meter_id": 1},
		MatchFields: []*ofp.OfpOxmOfbField{
			flows.InPort(2),
			flows.Metadata_ofp((1000 << 32) | 1),
			flows.VlanVid(4096),
		},
		Actions: []*ofp.OfpAction{
			flows.Output(103),
		},
	}
	flowMod = makeSimpleFlowMod(fa)
	flowMod.TableId = 2
	m := jsonpb.Marshaler{}
	flowModJson, _ := m.MarshalToString(flowMod)
	testLogger.Infow(getContext(), "onu-downstream-flow", log.Fields{"flow-mod": flowModJson})
	return
}

func (nb *NBTest) runTestSuite(t *testing.T, nbi voltha.VolthaServiceClient, oltDeviceType string, testWg *sync.WaitGroup) {
	defer testWg.Done()

	// Test create device
	nb.testCreateDevice(t, nbi, oltDeviceType)

	//Test Delete Device Scenarios
	nb.testForceDeletePreProvDevice(t, nbi, oltDeviceType)
	nb.testDeletePreProvDevice(t, nbi, oltDeviceType)
	nb.testForceDeleteEnabledDevice(t, nbi, oltDeviceType)
	nb.testDeleteEnabledDevice(t, nbi, oltDeviceType)
	nb.testForceDeleteDeviceFailure(t, nbi, oltDeviceType)
	nb.testDeleteDeviceFailure(t, nbi, oltDeviceType)

	////Test failed enable device
	nb.testEnableDeviceFailed(t, nbi)

	//Test Enable a device
	nb.testEnableDevice(t, nbi, oltDeviceType)

	//Test disable and ReEnable a root device
	nb.testDisableAndReEnableRootDevice(t, nbi, oltDeviceType)

	// Test disable and Enable pon port of OLT device
	nb.testDisableAndEnablePort(t, nbi, oltDeviceType)

	// Test Device unreachable when OLT is enabled
	nb.testDeviceRebootWhenOltIsEnabled(t, nbi, oltDeviceType)

	// Test disable and delete all devices
	nb.testDisableAndDeleteAllDevice(t, nbi, oltDeviceType)

	// Test enable and delete all devices
	nb.testEnableAndDeleteAllDevice(t, nbi, oltDeviceType)

	// Test omci test
	nb.testStartOmciTestAction(t, nbi, oltDeviceType)

	// Test flow add failure
	nb.testFlowAddFailure(t, nbi, oltDeviceType)

	// Test MPLS flows addition where:
	/*
		Upstream
		ONU
		ADDED, bytes=0, packets=0, table=0, priority=1000, selector=[IN_PORT:32, VLAN_VID:ANY], treatment=[immediate=[],
		transition=TABLE:1, meter=METER:1, metadata=METADATA:4100010000/0]
		OLT
		ADDED, bytes=0, packets=0, table=1, priority=1000, selector=[IN_PORT:32, VLAN_VID:ANY], treatment=[immediate=[VLAN_PUSH:vlan,
		VLAN_ID:2, MPLS_PUSH:mpls_unicast, MPLS_LABEL:YYY,MPLS_BOS:true, MPLS_PUSH:mpls_unicast ,MPLS_LABEL:XXX, MPLS_BOS:false,
		EXTENSION:of:0000000000000227/VolthaPushL2Header{​​​​​​​}​​​​​​​, ETH_SRC:OLT_MAC, ETH_DST:LEAF_MAC,  TTL:64, OUTPUT:65536],
		meter=METER:1, metadata=METADATA:4100000000/0]

		Downstream
		OLT
		//Below flow rule to pop L2 Ethernet headers from packets which have a single MPLS label
		ADDED, bytes=0, packets=0, table=0, priority=1000, selector=[IN_PORT:65536, ETH_TYPE:mpls_unicast, MPLS_BOS:true, ETH_SRC:LEAF_MAC],
		treatment=[DefaultTrafficTreatment{immediate=[DEC_MPLS_TTL, TTL_IN, MPLS_POP:mpls_unicast, EXTENSION:of:0000000000000227/VolthaPopL2Header{},
		transition=TABLE:1]

		//Below flow rule to pop L2 Ethernet headers from packets which have two MPLS label
		ADDED, bytes=0, packets=0, table=0, priority=1000, selector=[IN_PORT:65536, ETH_TYPE:mpls_unicast, MPLS_BOS:false, ETH_SRC:LEAF_MAC],
		treatment=[DefaultTrafficTreatment{immediate=[DEC_MPLS_TTL, TTL_IN, MPLS_POP:mpls_unicast, MPLS_POP:mpls_unicast ,
		EXTENSION:of:0000000000000227/VolthaPopL2Header{}, transition=TABLE:1]

		//Below flow rules are unchanged from the current implementations except for the table numbers
		ADDED, bytes=0, packets=0, table=1, priority=1000, selector=[IN_PORT:65536, VLAN_VID:2], treatment=[immediate=[VLAN_POP], transition=TABLE:2,
		meter=METER:2, metadata=METADATA:1000004100000020/0]
		ONU
		ADDED, bytes=0, packets=0, table=2, priority=1000, selector=[IN_PORT:65536, METADATA:20 VLAN_VID:ANY], treatment=[immediate=[OUTPUT:32],
		meter=METER:2, metadata=METADATA:4100000000/0]
	*/
	nb.testMPLSFlowsAddition(t, nbi, oltDeviceType)
}

func setUpCore(ctx context.Context, t *testing.T, nb *NBTest) (voltha.VolthaServiceClient, string) {
	// Start the Core
	coreAPIEndpoint, nbiEndpoint := nb.startGRPCCore(ctx)

	// Wait until the core is ready
	start := time.Now()
	logger.Infow(ctx, "waiting-for-core-to-be-ready", log.Fields{"start": start, "api-endpoint": coreAPIEndpoint})

	var vFunction isConditionSatisfied = func() bool {
		return nb.probe.IsReady()
	}
	err := waitUntilCondition(nb.internalTimeout, vFunction)
	assert.Nil(t, err)
	logger.Infow(ctx, "core-is-ready", log.Fields{"time-taken": time.Since(start)})

	// Create a grpc client to communicate with the Core
	conn, err := grpc.Dial(nbiEndpoint, grpc.WithInsecure())
	if err != nil {
		logger.Fatalw(ctx, "cannot connect to core", log.Fields{"error": err})
	}
	nbi := voltha.NewVolthaServiceClient(conn)
	if nbi == nil {
		logger.Fatalw(ctx, "cannot create a service to core", log.Fields{"error": err})
	}

	// Basic test with no data in Core
	nb.testCoreWithoutData(t, nbi)

	logger.Infow(ctx, "core-setup-complete", log.Fields{"time": time.Since(start), "api-endpoint": coreAPIEndpoint})

	return nbi, coreAPIEndpoint
}

func setupAdapters(ctx context.Context, t *testing.T, nb *NBTest, coreAPIEndpoint string, nbi voltha.VolthaServiceClient) {
	// Create/register the adapters
	start := time.Now()
	nb.oltAdaptersLock.Lock()
	nb.onuAdaptersLock.Lock()
	nb.oltAdapters, nb.onuAdapters = CreateAndRegisterAdapters(ctx, t, oltAdapters, onuAdapters, coreAPIEndpoint)
	nb.oltAdaptersLock.Unlock()
	nb.onuAdaptersLock.Unlock()

	nb.numONUPerOLT = cm.GetNumONUPerOLT()
	nb.startingUNIPortNo = cm.GetStartingUNIPortNo()

	// Wait for adapters to be fully running
	var areAdaptersRunning isConditionSatisfied = func() bool {
		ready := true
		nb.onuAdaptersLock.RLock()
		defer nb.onuAdaptersLock.RUnlock()
		for _, adapters := range nb.onuAdapters {
			for _, a := range adapters {
				ready = ready && a.IsReady()
				if !ready {
					return false
				}
			}
		}
		nb.oltAdaptersLock.RLock()
		defer nb.oltAdaptersLock.RUnlock()
		for _, adapters := range nb.oltAdapters {
			for _, a := range adapters {
				ready = ready && a.IsReady()
				if !ready {
					return false
				}
			}
		}
		return true
	}
	err := waitUntilCondition(nb.internalTimeout, areAdaptersRunning)
	assert.Nil(t, err)
	logger.Infow(ctx, "adapters-are-ready", log.Fields{"time-taken": time.Since(start)})

	// Test adapter registration
	nb.testAdapterRegistration(t, nbi)
}

func WaitForCoreConnectionToAdapters(ctx context.Context, t *testing.T, nb *NBTest, nbi voltha.VolthaServiceClient) {
	// Create/register the adapters
	start := time.Now()
	numAdapters := 0
	nb.oltAdaptersLock.RLock()
	numAdapters += len(nb.onuAdapters)
	nb.oltAdaptersLock.RUnlock()
	nb.onuAdaptersLock.RLock()
	numAdapters += len(nb.oltAdapters)
	nb.onuAdaptersLock.RUnlock()

	// Wait for adapters to be fully running
	var isCoreConnectedToAdapters isConditionSatisfied = func() bool {
		adpts, err := nbi.ListAdapters(getContext(), &empty.Empty{})
		if err != nil || len(adpts.Items) < numAdapters {
			return false
		}
		// Now check the last communication time
		for _, adpt := range adpts.Items {
			if time.Since(time.Unix(adpt.LastCommunication, 0)) > 5*time.Second {
				return false
			}
		}
		return true
	}
	err := waitUntilCondition(nb.internalTimeout, isCoreConnectedToAdapters)
	assert.Nil(t, err)
	logger.Infow(ctx, "core-connection-to-adapters-is-ready", log.Fields{"time-taken": time.Since(start)})

	// Test adapter registration
	nb.testAdapterRegistration(t, nbi)
}

// TestLogDeviceUpdate is used to extract and format device updates.  Not to be run on jenkins.
func TestLogDeviceUpdate(t *testing.T) {
	t.Skip()
	var inputFile = os.Getenv("LGF")
	var deviceID = os.Getenv("DID")

	prettyPrintDeviceUpdateLog(inputFile, deviceID)
}

func TestOMCIData(t *testing.T) {
	t.Skip()
	var inputFile = os.Getenv("LGF")
	var deviceID = os.Getenv("DID")
	omciLog(inputFile, deviceID)
}

func TestRandomMacGenerator(t *testing.T) {
	t.Skip()
	var wg sync.WaitGroup
	myMap := make(map[string]int)
	var myMapLock sync.Mutex
	max := 1000000
	for i := 0; i < max; i++ {
		wg.Add(1)
		go func() {
			str := getRandomMacAddress()
			myMapLock.Lock()
			myMap[str]++
			myMapLock.Unlock()
			wg.Done()
		}()
	}
	wg.Wait()
	// Look for duplicates
	for str, val := range myMap {
		if val != 1 {
			fmt.Println("duplicate", str)
		}
	}
}

func TestSuite(t *testing.T) {
	log.SetAllLogLevel(log.DebugLevel)

	// Create a context to be cancelled at the end of all tests.  This will trigger closing of any ressources used.
	ctx, cancel := context.WithCancel(context.Background())

	// Setup CPU profiling
	f, err := os.Create("grpc_profile.cpu")
	// f, err := os.Create("../../../tests/results/grpc_profile.cpu")
	if err != nil {
		logger.Fatalf(ctx, "could not create CPU profile: %v\n ", err)
	}
	defer func() { _ = f.Close() }()
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(-1)
	runtime.SetCPUProfileRate(200)
	if err := pprof.StartCPUProfile(f); err != nil {
		logger.Fatalf(ctx, "could not start CPU profile: %v\n", err)
	}
	defer pprof.StopCPUProfile()

	// Create test object
	nb := newNBTest(ctx, false)
	assert.NotNil(t, nb)
	defer nb.stopAll(ctx)

	// Setup the Core
	nbi, coreAPIEndpoint := setUpCore(ctx, t, nb)

	// Setup the adapters
	setupAdapters(ctx, t, nb, coreAPIEndpoint, nbi)

	// Wait until the Core can connect to the adapters
	WaitForCoreConnectionToAdapters(ctx, t, nb, nbi)

	// Start the change events listener and dispatcher to receive all change events from the Core
	nb.changeEventLister = NewChangedEventListener(len(nb.oltAdapters))
	ch := make(chan *ofp.ChangeEvent, (nb.numONUPerOLT+1)*len(nb.oltAdapters))
	go nb.changeEventLister.Start(ctx, ch)
	go nb.receiveChangeEvents(ctx, nbi, ch)

	// Run the full set of tests in parallel for each olt device type
	start := time.Now()
	fmt.Println("starting test at:", start)
	var wg sync.WaitGroup
	nb.oltAdaptersLock.RLock()
	numTestCycles := 1
	for i := 1; i <= numTestCycles; i++ {
		for oltAdapterType, oltAdapters := range nb.oltAdapters {
			for _, a := range oltAdapters {
				wg.Add(1)
				fmt.Printf("Launching test for OLT adapter type:%s supporting OLT device type:%s and ONU device type:%s\n", oltAdapterType, a.DeviceType, a.ChildDeviceType)
				go nb.runTestSuite(t, nbi, a.DeviceType, &wg)
			}
		}
	}
	nb.oltAdaptersLock.RUnlock()

	// Wait for all tests to complete
	wg.Wait()
	fmt.Println("Execution time:", time.Since(start))

	// Cleanup before leaving
	fmt.Println("Cleaning up ... grpc warnings can be safely ignored")
	cancel()
}
