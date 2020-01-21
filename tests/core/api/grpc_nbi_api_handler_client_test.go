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

package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var conn *grpc.ClientConn
var stub voltha.VolthaServiceClient
var testMode string

/*
Prerequite:  These tests require the rw_core to run prior to executing these test cases.
*/

var devices map[string]*voltha.Device

//func init() {
//	log.AddPackage(log.JSON, log.ErrorLevel, nil)
//	log.UpdateAllLoggers(log.Fields{"instanceId": "testing"})
//	log.SetAllLogLevel(log.ErrorLevel)
//
//	//Start kafka and Etcd
//	startKafkaEtcd()
//	time.Sleep(10 * time.Second) //TODO: Find a better way to ascertain they are up
//
//	stub = setupGrpcConnection()
//	stub = voltha.NewVolthaServiceClient(conn)
//	devices = make(map[string]*voltha.Device)
//}

func setup() {
	var err error

	if _, err = log.AddPackage(log.JSON, log.WarnLevel, log.Fields{"instanceId": "testing"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	log.UpdateAllLoggers(log.Fields{"instanceId": "testing"})
	log.SetAllLogLevel(log.ErrorLevel)

	//Start kafka and Etcd
	startKafka()
	startEtcd()
	time.Sleep(10 * time.Second) //TODO: Find a better way to ascertain they are up

	stub = setupGrpcConnection()
	testMode = common.TestModeKeys_api_test.String()
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

func startKafka() {
	fmt.Println("Starting Kafka and Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-zk-kafka-test.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func startEtcd() {
	fmt.Println("Starting Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-etcd.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func stopKafka() {
	fmt.Println("Stopping Kafka and Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-zk-kafka-test.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
}

func stopEtcd() {
	fmt.Println("Stopping Etcd ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/docker-compose-etcd.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
}

func startCore() {
	fmt.Println("Starting voltha core ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/rw_core.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func stopCore() {
	fmt.Println("Stopping voltha core ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/rw_core.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
}

func startSimulatedOLTAndONUAdapters() {
	fmt.Println("Starting simulated OLT and ONU adapters ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/adapters-simulated.yml", "up", "-d")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func stopSimulatedOLTAndONUAdapters() {
	fmt.Println("Stopping simulated OLT and ONU adapters ...")
	command := "docker-compose"
	cmd := exec.Command(command, "-f", "../../../compose/adapters-simulated.yml", "down")
	if err := cmd.Run(); err != nil {
		// ignore error - as this is mostly due network being left behind as its being used by other
		// containers
		log.Warn(err)
	}
}

func TestListDeviceIds(t *testing.T) {
	fmt.Println("Testing list Devices Ids ...")
	//0. Start kafka and Ectd
	startKafka()
	startEtcd()

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
		assert.NotNil(t, response)
		assert.Nil(t, err)
		devices[response.Id] = response
	}

	//3. Verify devices have been added correctly
	ctx := context.Background()
	response, err := stub.ListDeviceIds(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.True(t, hasAllIds(response))

	//4. Stop the core
	stopCore()

	//5. Stop Kafka and Etcd
	stopKafka()
	stopEtcd()
}

func TestReconcileDevices(t *testing.T) {
	fmt.Println("Testing Reconcile Devices ...")

	//0. Start kafka and Ectd
	startKafka()
	startEtcd()

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

	//10. Stop Kafka and Etcd
	stopKafka()
	stopEtcd()
}

func TestDeviceManagement(t *testing.T) {
	fmt.Println("Testing Device Management ...")

	numberOfOLTDevices := 1

	//0. Start kafka and Ectd
	startKafka()
	startEtcd()

	//1. Start the core
	startCore()

	//2. Start the simulated adapters
	startSimulatedOLTAndONUAdapters()

	// Wait until the core and adapters sync up
	time.Sleep(10 * time.Second)

	//3. Create a set of devices into the Core
	devices = make(map[string]*voltha.Device)
	logicalDevices := make(map[string]*voltha.LogicalDevice)
	for i := 0; i < numberOfOLTDevices; i++ {
		ctx := context.Background()
		randomMacAddress := strings.ToUpper(com.GetRandomMacAddress())
		device := &voltha.Device{Type: "simulated_olt", MacAddress: randomMacAddress}
		response, err := stub.CreateDevice(ctx, device)
		log.Infow("response", log.Fields{"res": response, "error": err})
		assert.Nil(t, err)
		devices[response.Id] = response
	}

	//4. Enable all the devices
	for id, _ := range devices {
		ctx := context.Background()
		response, err := stub.EnableDevice(ctx, &common.ID{Id: id})
		log.Infow("response", log.Fields{"res": response, "error": err})
		assert.Nil(t, err)
	}

	// Wait until all devices have been enabled
	if numberOfOLTDevices < 5 {
		time.Sleep(3 * time.Second)
	} else if numberOfOLTDevices < 20 {
		time.Sleep(20 * time.Second)
	} else {
		time.Sleep(30 * time.Second)
	}
	//time.Sleep(1 * time.Second * time.Duration(numberOfDevices))

	//5. Verify that all devices are in enabled state
	ctx := context.Background()
	response, err := stub.ListDevices(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.Equal(t, len(devices)*2, len(response.Items))
	for _, d := range response.Items {
		devices[d.Id] = d
		assert.Equal(t, d.AdminState, voltha.AdminState_ENABLED)
	}

	//6. Get the logical devices
	ctx = context.Background()
	lresponse, lerr := stub.ListLogicalDevices(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": lerr})
	assert.Nil(t, lerr)
	assert.Equal(t, numberOfOLTDevices, len(lresponse.Items))
	for _, ld := range lresponse.Items {
		logicalDevices[ld.Id] = ld
		// Ensure each logical device have two ports
		assert.Equal(t, 2, len(ld.Ports))
	}

	//7. Disable all ONUs & check status & check logical device
	for id, d := range devices {
		ctx := context.Background()
		if d.Type == "simulated_onu" {
			response, err := stub.DisableDevice(ctx, &common.ID{Id: id})
			log.Infow("response", log.Fields{"res": response, "error": err})
			assert.Nil(t, err)
		}
	}

	// Wait for all the changes to be populated
	time.Sleep(3 * time.Second)

	ctx = context.Background()
	response, err = stub.ListDevices(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.Equal(t, len(devices), len(response.Items))
	for _, d := range response.Items {
		if d.Type == "simulated_onu" {
			assert.Equal(t, d.AdminState, voltha.AdminState_DISABLED)
			devices[d.Id] = d
		} else {
			assert.Equal(t, d.AdminState, voltha.AdminState_ENABLED)
		}
	}

	ctx = context.Background()
	lresponse, lerr = stub.ListLogicalDevices(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": lerr})
	assert.Nil(t, lerr)
	assert.Equal(t, numberOfOLTDevices, len(lresponse.Items))
	for _, ld := range lresponse.Items {
		logicalDevices[ld.Id] = ld
		// Ensure each logical device have one port - only olt port
		assert.Equal(t, 1, len(ld.Ports))
	}

	//8. Enable all ONUs & check status & check logical device
	for id, d := range devices {
		ctx := context.Background()
		if d.Type == "simulated_onu" {
			response, err := stub.EnableDevice(ctx, &common.ID{Id: id})
			log.Infow("response", log.Fields{"res": response, "error": err})
			assert.Nil(t, err)
		}
	}

	// Wait for all the changes to be populated
	time.Sleep(3 * time.Second)

	ctx = context.Background()
	response, err = stub.ListDevices(ctx, &empty.Empty{})
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Nil(t, err)
	assert.Equal(t, len(devices), len(response.Items))
	for _, d := range response.Items {
		assert.Equal(t, d.AdminState, voltha.AdminState_ENABLED)
		devices[d.Id] = d
	}

	//ctx = context.Background()
	//lresponse, lerr = stub.ListLogicalDevices(ctx, &empty.Empty{})
	//log.Infow("response", log.Fields{"res": response, "error": lerr})
	//assert.Nil(t, lerr)
	//assert.Equal(t, numberOfOLTDevices, len(lresponse.Items))
	//for _, ld := range (lresponse.Items) {
	//	logicalDevices[ld.Id] = ld
	//	// Ensure each logical device have two ports
	//	assert.Equal(t, 2, len(ld.Ports))
	//}

	//9. Disable all OLTs & check status & check logical device

	//10. Enable all OLTs & Enable all ONUs & check status & check logical device

	//11. Disable all OLTs & check status & check logical device

	//12. Delete all Devices & check status & check logical device

	////13. Store simulated adapters
	//stopSimulatedOLTAndONUAdapters()
	//
	////14. Store the core
	//stopCore()
	//
	////15. Stop Kafka and Etcd
	//stopKafka()
	//stopEtcd()
}

func TestGetDevice(t *testing.T) {
	var id common.ID
	id.Id = "anyid"
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetDevice(ctx, &id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, id.Id, st.Message())
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestUpdateLogLevelError(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	level := voltha.Logging{PackageName: "github.com/opencord/voltha-go/rw_core/core", Level: common.LogLevel_ERROR}
	response, err := stub.UpdateLogLevel(ctx, &level)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestGetVoltha(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetVoltha(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestUpdateLogLevelDebug(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	level := voltha.Logging{PackageName: "github.com/opencord/voltha-go/rw_core/core", Level: common.LogLevel_DEBUG}
	response, err := stub.UpdateLogLevel(ctx, &level)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestGetCoreInstance(t *testing.T) {
	id := &voltha.ID{Id: "getCoreInstance"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetCoreInstance(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestGetLogicalDevice(t *testing.T) {
	id := &voltha.ID{Id: "getLogicalDevice"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetLogicalDevice(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, id.Id, st.Message())
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetLogicalDevicePort(t *testing.T) {
	id := &voltha.LogicalPortId{Id: "GetLogicalDevicePort"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.GetLogicalDevicePort(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDevicePorts(t *testing.T) {
	id := &voltha.ID{Id: "listLogicalDevicePorts"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDevicePorts(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDeviceFlows(t *testing.T) {
	id := &voltha.ID{Id: "ListLogicalDeviceFlows"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDeviceFlows(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDeviceFlowGroups(t *testing.T) {
	id := &voltha.ID{Id: "ListLogicalDeviceFlowGroups"}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListLogicalDeviceFlowGroups(ctx, id)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListDevices(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, _ := stub.ListDevices(ctx, &empty.Empty{})
	assert.Equal(t, len(response.Items), 0)
}

func TestListAdapters(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListAdapters(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestListLogicalDevices(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, _ := stub.ListLogicalDevices(ctx, &empty.Empty{})
	assert.Equal(t, len(response.Items), 0)
}

func TestListCoreInstances(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	response, err := stub.ListCoreInstances(ctx, &empty.Empty{})
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

func TestCreateDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	device := &voltha.Device{Id: "newdevice"}
	response, err := stub.CreateDevice(ctx, device)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &voltha.Device{Id: "newdevice"}, response)
	assert.Nil(t, err)
}

func TestEnableDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "enabledevice"}
	response, err := stub.EnableDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestDisableDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "DisableDevice"}
	response, err := stub.DisableDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestRebootDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "RebootDevice"}
	response, err := stub.RebootDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestDeleteDevice(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.ID{Id: "DeleteDevice"}
	response, err := stub.DeleteDevice(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestEnableLogicalDevicePort(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.LogicalPortId{Id: "EnableLogicalDevicePort"}
	response, err := stub.EnableLogicalDevicePort(ctx, id)
	if e, ok := status.FromError(err); ok {
		log.Infow("response", log.Fields{"error": err, "errorcode": e.Code(), "msg": e.Message()})
	}
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestDisableLogicalDevicePort(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	id := &voltha.LogicalPortId{Id: "DisableLogicalDevicePort"}
	response, err := stub.DisableLogicalDevicePort(ctx, id)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestUpdateLogicalDeviceFlowGroupTable(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	flow := &openflow_13.FlowGroupTableUpdate{Id: "UpdateLogicalDeviceFlowGroupTable"}
	response, err := stub.UpdateLogicalDeviceFlowGroupTable(ctx, flow)
	log.Infow("response", log.Fields{"res": response, "error": err})
	assert.Equal(t, &empty.Empty{}, response)
	assert.Nil(t, err)
}

func TestGetImageDownloadStatus(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(testMode, "true"))
	img := &voltha.ImageDownload{Id: "GetImageDownloadStatus"}
	response, err := stub.GetImageDownloadStatus(ctx, img)
	assert.Nil(t, response)
	st, _ := status.FromError(err)
	assert.Equal(t, "UnImplemented", st.Message())
}

// TODO: complete the remaining tests

func shutdown() {
	conn.Close()
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
