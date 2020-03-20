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
package concurrency

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var conns []*grpc.ClientConn
var stubs []voltha.VolthaServiceClient
var volthaSerialNumberKey string
var grpcPorts []int

/*
 This series of tests are executed with two RW_Cores
*/

var devices map[string]*voltha.Device

func setup() {
	grpcPorts = []int{50057, 50058}
	stubs = make([]voltha.VolthaServiceClient, 0)
	conns = make([]*grpc.ClientConn, 0)

	volthaSerialNumberKey = "voltha_serial_number"
	devices = make(map[string]*voltha.Device)
}

func connectToCore(port int) (voltha.VolthaServiceClient, error) {
	grpcHostIP := os.Getenv("DOCKER_HOST_IP")
	grpcHost := fmt.Sprintf("%s:%d", grpcHostIP, port)
	conn, err := grpc.Dial(grpcHost, grpc.WithInsecure())
	if err != nil {
		logger.Fatalf("did not connect: %s", err)
		return nil, errors.New("failure-to-connect")
	}
	conns = append(conns, conn)
	return voltha.NewVolthaServiceClient(conn), nil
}

func setupGrpcConnection() []voltha.VolthaServiceClient {
	// We have 2 concurrent cores.  Connect to them
	for _, port := range grpcPorts {
		if client, err := connectToCore(port); err == nil {
			stubs = append(stubs, client)
			logger.Infow("connected", log.Fields{"port": port})
		}
	}
	return stubs
}

func clearAllDevices(clearMap bool) {
	for key, _ := range devices {
		ctx := context.Background()
		response, err := stubs[1].DeleteDevice(ctx, &voltha.ID{Id: key})
		logger.Infow("response", log.Fields{"res": response, "error": err})
		if clearMap {
			delete(devices, key)
		}
	}
}

// Verify if all ids are present in the global list of devices
func hasAllIds(ids *voltha.IDs) bool {
	if ids == nil && len(devices) == 0 {
		return true
	}
	if ids == nil {
		return false
	}
	for _, id := range ids.Items {
		if _, exist := devices[id.Id]; !exist {
			return false
		}
	}
	return true
}

func startKafka() {
	fmt.Println("Starting Kafka and Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-zk-kafka-test.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		logger.Fatal(err)
	}
}

func startEtcd() {
	fmt.Println("Starting Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-etcd.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		logger.Fatal(err)
	}
}

func stopKafka() {
	fmt.Println("Stopping Kafka and Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-zk-kafka-test.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		logger.Warn(err)
	}
}

func stopEtcd() {
	fmt.Println("Stopping Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-etcd.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		logger.Warn(err)
	}
}

func startCores() {
	fmt.Println("Starting voltha cores ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/rw_core_concurrency_test.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		logger.Fatal(err)
	}
}

func stopCores() {
	fmt.Println("Stopping voltha cores ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/rw_core_concurrency_test.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		logger.Warn(err)
	}
}

func startSimulatedOLTAndONUAdapters() {
	fmt.Println("Starting simulated OLT and ONU adapters ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/adapters-simulated.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		logger.Fatal(err)
	}
}

func stopSimulatedOLTAndONUAdapters() {
	fmt.Println("Stopping simulated OLT and ONU adapters ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/adapters-simulated.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		logger.Warn(err)
	}
}

func sendCreateDeviceRequest(ctx context.Context, stub voltha.VolthaServiceClient, device *voltha.Device, ch chan interface{}) {
	fmt.Println("Sending  create device ...")
	if response, err := stub.CreateDevice(ctx, device); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func sendEnableDeviceRequest(ctx context.Context, stub voltha.VolthaServiceClient, deviceId string, ch chan interface{}) {
	fmt.Println("Sending enable device ...")
	if response, err := stub.EnableDevice(ctx, &common.ID{Id: deviceId}); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

//// createPonsimDevice sends two requests to each core and waits for both responses
//func createPonsimDevice(stubs []voltha.VolthaServiceClient) (*voltha.Device, error) {
//	ui := uuid.New()
//	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
//	//preprovision_olt -t ponsim_olt -H 172.20.0.11:50060
//	device := &voltha.Device{Type: "ponsim_olt"}
//	device.Address = &voltha.Device_HostAndPort{HostAndPort:"172.20.0.11:50060"}
//	ch := make(chan interface{})
//	defer close(ch)
//	requestNum := 0
//	for _, stub := range stubs {
//		go sendCreateDeviceRequest(ctx, stub, device, ch)
//		requestNum += 1
//	}
//	fmt.Println("Waiting for create device response ...")
//	receivedResponse := 0
//	var err error
//	var returnedDevice *voltha.Device
//	select {
//	case res, ok := <-ch:
//		receivedResponse += 1
//		if !ok {
//		} else if er, ok := res.(error); ok {
//			err = er
//		} else if d, ok := res.(*voltha.Device); ok {
//			returnedDevice = d
//		}
//		if receivedResponse == requestNum {
//			break
//		}
//	}
//	if returnedDevice != nil {
//		return returnedDevice, nil
//	}
//	return nil, err
//}

// createDevice sends two requests to each core and waits for both responses
func createDevice(stubs []voltha.VolthaServiceClient) (*voltha.Device, error) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	randomMacAddress := strings.ToUpper(com.GetRandomMacAddress())
	device := &voltha.Device{Type: "simulated_olt", MacAddress: randomMacAddress}
	ch := make(chan interface{})
	defer close(ch)
	requestNum := 0
	for _, stub := range stubs {
		go sendCreateDeviceRequest(ctx, stub, device, ch)
		requestNum += 1
	}
	fmt.Println("Waiting for create device response ...")
	receivedResponse := 0
	var err error
	var returnedDevice *voltha.Device
	select {
	case res, ok := <-ch:
		receivedResponse += 1
		if !ok {
		} else if er, ok := res.(error); ok {
			err = er
		} else if d, ok := res.(*voltha.Device); ok {
			returnedDevice = d
		}
		if receivedResponse == requestNum {
			break
		}
	}
	if returnedDevice != nil {
		return returnedDevice, nil
	}
	return nil, err
}

// enableDevices sends two requests to each core for each device and waits for both responses before sending another
// enable request for a different device.
func enableAllDevices(stubs []voltha.VolthaServiceClient) error {
	for deviceId, val := range devices {
		if val.AdminState == voltha.AdminState_PREPROVISIONED {
			ui := uuid.New()
			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
			ch := make(chan interface{})
			defer close(ch)
			requestNum := 0
			for _, stub := range stubs {
				go sendEnableDeviceRequest(ctx, stub, deviceId, ch)
				requestNum += 1
			}
			receivedResponse := 0
			var err error
			fmt.Println("Waiting for enable device response ...")
			validResponseReceived := false
			select {
			case res, ok := <-ch:
				receivedResponse += 1
				if !ok {
				} else if er, ok := res.(error); ok {
					err = er
				} else if _, ok := res.(*empty.Empty); ok {
					validResponseReceived = true
				}
				if receivedResponse == requestNum {
					break
				}
			}
			if validResponseReceived {
				return nil
			}
			return err
		}
	}
	return nil
}

func TestConcurrentRequests(t *testing.T) {
	fmt.Println("Testing Concurrent requests ...")

	////0. Start kafka and Ectd
	startKafka()
	defer stopKafka()
	startEtcd()
	defer stopKafka()
	//
	////1. Start the core
	startCores()
	defer stopCores()
	//
	////2. Start the simulated adapters
	startSimulatedOLTAndONUAdapters()
	defer stopSimulatedOLTAndONUAdapters()
	//
	//// Wait until the core and adapters sync up
	//time.Sleep(10 * time.Second)

	stubs = setupGrpcConnection()

	//3.  Create the devices
	response, err := createDevice(stubs)
	logger.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	devices[response.Id] = response

	//4. Enable all the devices
	err = enableAllDevices(stubs)
	assert.Nil(t, err)

	////5. Store simulated adapters
	//stopSimulatedOLTAndONUAdapters()
	//
	////6. Store the core
	//stopCores()
	//
	////7. Stop Kafka and Etcd
	//stopKafka()
	//stopEtcd()
}

func shutdown() {
	for _, conn := range conns {
		conn.Close()
	}
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
