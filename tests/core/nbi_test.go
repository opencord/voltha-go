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
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"os"
	"os/exec"
	"testing"
	"time"
)

var conn *grpc.ClientConn
var stub voltha.VolthaServiceClient
var devices map[string]*voltha.Device

func init() {
	log.AddPackage(log.JSON, log.ErrorLevel, nil)
	log.UpdateAllLoggers(log.Fields{"instanceId": "testing"})
	log.SetAllLogLevel(log.ErrorLevel)

	//Start kafka and Etcd
	startKafkaEtcd()
	time.Sleep(10 * time.Second) //TODO: Find a better way to ascertain they are up

	stub = setupGrpcConnection()
	stub = voltha.NewVolthaServiceClient(conn)
	devices = make(map[string]*voltha.Device)
}

func setupGrpcConnection() voltha.VolthaServiceClient {
	grpcHostIP := os.Getenv("DOCKER_HOST_IP")
	grpcPort := 50057
	grpcHost := fmt.Sprintf("%s:%d", grpcHostIP, grpcPort)
	var err error
	conn, err = grpc.Dial(grpcHost, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %s", err)
	}
	return voltha.NewVolthaServiceClient(conn)
}

func clearAllDevices(clearMap bool) {
	for key, _ := range devices {
		ctx := context.Background()
		response, err := stub.DeleteDevice(ctx, &voltha.ID{Id: key})
		log.Infow("response", log.Fields{"res": response, "error": err})
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

func startKafkaEtcd() {
	fmt.Println("Starting Kafka and Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../compose/docker-compose-zk-kafka-test.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	cmd = exec.Command(command, "-f", "../../compose/docker-compose-etcd.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func stopKafkaEtcd() {
	fmt.Println("Stopping Kafka and Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../compose/docker-compose-zk-kafka-test.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
	cmd = exec.Command(command, "-f", "../../compose/docker-compose-etcd.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
}

func startCore() {
	fmt.Println("Starting voltha core ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../compose/rw_core.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func stopCore() {
	fmt.Println("Stopping voltha core ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../compose/rw_core.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
}

func TestListDeviceIds(t *testing.T) {
	//1. Start the core
	startCore()

	// Wait until it's up - TODO: find a better way to check
	time.Sleep(10 * time.Second)

	//2. Create a set of devices into the Core
	for i := 0; i < 10; i++ {
		ctx := context.Background()
		device := &voltha.Device{Type: "simulated_olt"}
		response, err := stub.CreateDevice(ctx, device)
		log.Infow("response", log.Fields{"res": response, "error": err})
		assert.Nil(t, err)
		devices[response.Id] = response
	}

	//3. Verify devices have been added correctly
	ctx := context.Background()
	response, err := stub.ListDeviceIds(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.True(t, hasAllIds(response))

	//	4. Stop the core
	stopCore()
}

func TestReconcileDevices(t *testing.T) {
	//1. Start the core
	startCore()

	// Wait until it's up - TODO: find a better way to check
	time.Sleep(10 * time.Second)

	//2. Create a set of devices into the Core
	for i := 0; i < 10; i++ {
		ctx := context.Background()
		device := &voltha.Device{Type: "simulated_olt"}
		response, err := stub.CreateDevice(ctx, device)
		log.Infow("response", log.Fields{"res": response, "error": err})
		assert.Nil(t, err)
		devices[response.Id] = response
	}
	//3. Verify devices have been added correctly
	ctx := context.Background()
	response, err := stub.ListDeviceIds(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.True(t, hasAllIds(response))

	//4. Stop the core and restart it. This will start the core with no data in memory but
	// etcd will still have the data.
	stopCore()
	time.Sleep(5 * time.Second)
	startCore()
	time.Sleep(10 * time.Second)

	//5. Setup the connection again
	stub = setupGrpcConnection()

	//6. Verify there are no devices left
	ctx = context.Background()
	response, err = stub.ListDeviceIds(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.Equal(t, len(response.Items), 0)

	//7. Invoke reconcile with all stored list
	toRestore := &voltha.IDs{Items: make([]*voltha.ID, 0)}
	for key, _ := range devices {
		toRestore.Items = append(toRestore.Items, &voltha.ID{Id: key})
	}
	ctx = context.Background()
	_, err = stub.ReconcileDevices(ctx, toRestore)
	assert.Nil(t, err)

	//8. Verify all devices have been restored
	ctx = context.Background()
	response, err = stub.ListDeviceIds(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.True(t, hasAllIds(response))

	for _, id := range response.Items {
		fmt.Println("id", id.Id)
	}

	//9. Store the core
	stopCore()
}

func shutdown() {
	conn.Close()
	stopKafkaEtcd()
}

func TestMain(m *testing.M) {
	code := m.Run()
	shutdown()
	os.Exit(code)
}
