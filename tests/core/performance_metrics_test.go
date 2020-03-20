// +build integration

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
	"fmt"
	"github.com/google/uuid"
	tu "github.com/opencord/voltha-go/tests/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	"math"
	"os"
	"testing"
	"time"
)

var stub voltha.VolthaServiceClient
var volthaSerialNumberKey string

/*
 This local "integration" test uses one RW-Core, one simulated_olt and one simulated_onu adapter to test performance
metrics, in a development environment. It uses docker-compose to set up the local environment. However, it can
easily be extended to run in k8s environment.

The compose files used are located under %GOPATH/src/github.com/opencord/voltha-go/compose. If the GOPATH is not set
then you can specify the location of the compose files by using COMPOSE_PATH to set the compose files location.

To run this test: DOCKER_HOST_IP=<local IP> go test -v

*/

var allDevices map[string]*voltha.Device
var allLogicalDevices map[string]*voltha.LogicalDevice

var composePath string

const (
	GRPC_PORT        = 50057
	NUM_OLTS         = 1
	NUM_ONUS_PER_OLT = 4 // This should coincide with the number of onus per olt in adapters-simulated.yml file
)

var parentPmNames = []string{
	"tx_64_pkts",
	"tx_65_127_pkts",
	"tx_128_255_pkts",
	"tx_256_511_pkts",
	"tx_512_1023_pkts",
	"tx_1024_1518_pkts",
	"tx_1519_9k_pkts",
	"rx_64_pkts",
	"rx_65_127_pkts",
	"rx_128_255_pkts",
	"rx_256_511_pkts",
	"rx_512_1023_pkts",
	"rx_1024_1518_pkts",
	"rx_1519_9k_pkts",
}

var childPmNames = []string{
	"tx_64_pkts",
	"tx_65_127_pkts",
	"tx_128_255_pkts",
	"tx_1024_1518_pkts",
	"tx_1519_9k_pkts",
	"rx_64_pkts",
	"rx_64_pkts",
	"rx_65_127_pkts",
	"rx_128_255_pkts",
	"rx_1024_1518_pkts",
	"rx_1519_9k_pkts",
}

func setup() {
	volthaSerialNumberKey = "voltha_serial_number"
	allDevices = make(map[string]*voltha.Device)
	allLogicalDevices = make(map[string]*voltha.LogicalDevice)

	grpcHostIP := os.Getenv("DOCKER_HOST_IP")
	goPath := os.Getenv("GOPATH")
	if goPath != "" {
		composePath = fmt.Sprintf("%s/src/github.com/opencord/voltha-go/compose", goPath)
	} else {
		composePath = os.Getenv("COMPOSE_PATH")
	}

	fmt.Println("Using compose path:", composePath)

	//Start the simulated environment
	if err = tu.StartSimulatedEnv(composePath); err != nil {
		fmt.Println("Failure starting simulated environment:", err)
		os.Exit(10)
	}

	stub, err = tu.SetupGrpcConnectionToCore(grpcHostIP, GRPC_PORT)
	if err != nil {
		fmt.Println("Failure connecting to Voltha Core:", err)
		os.Exit(11)
	}

	// Wait for the simulated devices to be registered in the Voltha Core
	adapters := []string{"simulated_olt", "simulated_onu"}
	if _, err = tu.WaitForAdapterRegistration(stub, adapters, 40); err != nil {
		fmt.Println("Failure retrieving adapters:", err)
		os.Exit(12)
	}
}

func shutdown() {
	err := tu.StopSimulatedEnv(composePath)
	if err != nil {
		fmt.Println("Failure stop simulated environment:", err)
	}
}

func refreshLocalDeviceCache(stub voltha.VolthaServiceClient) error {
	retrievedDevices, err := tu.ListDevices(stub)
	if err != nil {
		return err
	}
	for _, d := range retrievedDevices.Items {
		allDevices[d.Id] = d
	}

	retrievedLogicalDevices, err := tu.ListLogicalDevices(stub)
	if err != nil {
		return err
	}

	for _, ld := range retrievedLogicalDevices.Items {
		allLogicalDevices[ld.Id] = ld
	}
	return nil
}

func isPresent(pmName string, pmNames []string) bool {
	for _, name := range pmNames {
		if name == pmName {
			return true
		}
	}
	return false
}

func verifyDevicePMs(t *testing.T, stub voltha.VolthaServiceClient, device *voltha.Device, allPmNames []string, disabledPmNames []string, frequency uint32) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	pmConfigs, err := stub.ListDevicePmConfigs(ctx, &common.ID{Id: device.Id})
	assert.Nil(t, err)
	assert.Equal(t, device.Id, pmConfigs.Id)
	assert.Equal(t, uint32(frequency), pmConfigs.DefaultFreq)
	assert.Equal(t, false, pmConfigs.FreqOverride)
	assert.Equal(t, false, pmConfigs.Grouped)
	assert.Nil(t, pmConfigs.Groups)
	assert.True(t, len(pmConfigs.Metrics) > 0)
	metrics := pmConfigs.Metrics
	for _, m := range metrics {
		if m.Enabled {
			assert.True(t, isPresent(m.Name, allPmNames))
		} else {
			assert.True(t, isPresent(m.Name, disabledPmNames))
		}
		assert.Equal(t, voltha.PmConfig_COUNTER, m.Type)
	}
}

func verifyInitialPmConfigs(t *testing.T, stub voltha.VolthaServiceClient) {
	fmt.Println("Verifying initial PM configs")
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for _, d := range allDevices {
		if d.Root {
			verifyDevicePMs(t, stub, d, parentPmNames, []string{}, 150)
		} else {
			verifyDevicePMs(t, stub, d, childPmNames, []string{}, 150)
		}
	}
}

func verifyPmFrequencyUpdate(t *testing.T, stub voltha.VolthaServiceClient, device *voltha.Device) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	pmConfigs, err := stub.ListDevicePmConfigs(ctx, &common.ID{Id: device.Id})
	assert.Nil(t, err)
	pmConfigs.DefaultFreq = 10
	_, err = stub.UpdateDevicePmConfigs(ctx, pmConfigs)
	assert.Nil(t, err)
	if device.Root {
		verifyDevicePMs(t, stub, device, parentPmNames, []string{}, 10)
	} else {
		verifyDevicePMs(t, stub, device, childPmNames, []string{}, 10)
	}
}

func updatePmFrequencies(t *testing.T, stub voltha.VolthaServiceClient) {
	fmt.Println("Verifying update to PMs frequencies")
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for _, d := range allDevices {
		verifyPmFrequencyUpdate(t, stub, d)
	}
}

func verifyDisablingSomePmMetrics(t *testing.T, stub voltha.VolthaServiceClient, device *voltha.Device) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	pmConfigs, err := stub.ListDevicePmConfigs(ctx, &common.ID{Id: device.Id})
	assert.Nil(t, err)
	metricsToDisable := []string{"tx_64_pkts", "rx_64_pkts", "tx_65_127_pkts", "rx_65_127_pkts"}
	for _, m := range pmConfigs.Metrics {
		if isPresent(m.Name, metricsToDisable) {
			m.Enabled = false
		}
	}
	_, err = stub.UpdateDevicePmConfigs(ctx, pmConfigs)
	assert.Nil(t, err)
	if device.Root {
		verifyDevicePMs(t, stub, device, parentPmNames, metricsToDisable, 10)
	} else {
		verifyDevicePMs(t, stub, device, childPmNames, metricsToDisable, 10)
	}
}

func disableSomePmMetrics(t *testing.T, stub voltha.VolthaServiceClient) {
	fmt.Println("Verifying disabling of some PMs")
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for _, d := range allDevices {
		verifyDisablingSomePmMetrics(t, stub, d)
	}
}

func createAndEnableDevices(t *testing.T) {
	err := tu.SetAllLogLevel(stub, voltha.Logging{Level: common.LogLevel_WARNING})
	assert.Nil(t, err)

	err = tu.SetLogLevel(stub, voltha.Logging{Level: common.LogLevel_DEBUG, PackageName: "github.com/opencord/voltha-go/rw_core/core"})
	assert.Nil(t, err)

	startTime := time.Now()

	//Pre-provision the parent device
	oltDevice, err := tu.PreProvisionDevice(stub)
	assert.Nil(t, err)

	fmt.Println("Creation of ", NUM_OLTS, " OLT devices took:", time.Since(startTime))

	startTime = time.Now()

	//Enable all parent device - this will enable the child devices as well as validate the child devices
	err = tu.EnableDevice(stub, oltDevice, NUM_ONUS_PER_OLT)
	assert.Nil(t, err)

	fmt.Println("Enabling of  OLT device took:", time.Since(startTime))

	// Wait until the core and adapters sync up after an enabled
	time.Sleep(time.Duration(math.Max(10, float64(NUM_OLTS*NUM_ONUS_PER_OLT)/2)) * time.Second)

	err = tu.VerifyDevices(stub, NUM_ONUS_PER_OLT)
	assert.Nil(t, err)

	lds, err := tu.VerifyLogicalDevices(stub, oltDevice, NUM_ONUS_PER_OLT)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(lds.Items))
}

func TestPerformanceMetrics(t *testing.T) {
	//1. Test creation and activation of the devices.  This will validate the devices as well as the logical device created/
	createAndEnableDevices(t)

	//	2. Test initial PMs on each device
	verifyInitialPmConfigs(t, stub)

	//	3. Test frequency update of the pmConfigs
	updatePmFrequencies(t, stub)

	//	4. Test disable some PM metrics
	disableSomePmMetrics(t, stub)
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
