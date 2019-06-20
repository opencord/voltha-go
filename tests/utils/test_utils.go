// "build integration

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
	"os/exec"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	com "github.com/opencord/voltha-go/adapters/common"
	"github.com/opencord/voltha-protos/go/common"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	VOLTHA_SERIAL_NUMBER_KEY = "voltha_serial_number"
)

func startKafka(composePath string) error {
	fmt.Println("Starting Kafka and Etcd ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/docker-compose-zk-kafka-test.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "up", "-d")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func startEtcd(composePath string) error {
	fmt.Println("Starting Etcd ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/docker-compose-etcd.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "up", "-d")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func stopKafka(composePath string) error {
	fmt.Println("Stopping Kafka and Etcd ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/docker-compose-zk-kafka-test.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "down")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func stopEtcd(composePath string) error {
	fmt.Println("Stopping Etcd ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/docker-compose-etcd.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "down")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func startCore(composePath string) error {
	fmt.Println("Starting voltha core ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/rw_core.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "up", "-d")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func stopCore(composePath string) error {
	fmt.Println("Stopping voltha core ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/rw_core.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "down")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func startSimulatedOLTAndONUAdapters(composePath string) error {
	fmt.Println("Starting simulated OLT and ONU adapters ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/adapters-simulated.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "up", "-d")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func stopSimulatedOLTAndONUAdapters(composePath string) error {
	fmt.Println("Stopping simulated OLT and ONU adapters ...")
	command := "docker-compose"
	fileName := fmt.Sprintf("%s/adapters-simulated.yml", composePath)
	cmd := exec.Command(command, "-f", fileName, "down")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func ListLogicalDevices(stub voltha.VolthaServiceClient) (*voltha.LogicalDevices, error) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	if response, err := stub.ListLogicalDevices(ctx, &empty.Empty{}); err != nil {
		return nil, err
	} else {
		return response, nil
	}
}

func getNumUniPort(ld *voltha.LogicalDevice) int {
	num := 0
	for _, port := range ld.Ports {
		if !port.RootPort {
			num += 1
		}
	}
	return num
}

func sendCreateDeviceRequest(ctx context.Context, stub voltha.VolthaServiceClient, device *voltha.Device, ch chan interface{}) {
	if response, err := stub.CreateDevice(ctx, device); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func sendListAdapters(ctx context.Context, stub voltha.VolthaServiceClient, ch chan interface{}) {
	if response, err := stub.ListAdapters(ctx, &empty.Empty{}); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func sendEnableDeviceRequest(ctx context.Context, stub voltha.VolthaServiceClient, deviceId string, ch chan interface{}) {
	if response, err := stub.EnableDevice(ctx, &common.ID{Id: deviceId}); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func sendDisableDeviceRequest(ctx context.Context, stub voltha.VolthaServiceClient, deviceId string, ch chan interface{}) {
	if response, err := stub.DisableDevice(ctx, &common.ID{Id: deviceId}); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func sendDeleteDeviceRequest(ctx context.Context, stub voltha.VolthaServiceClient, deviceId string, ch chan interface{}) {
	if response, err := stub.DeleteDevice(ctx, &common.ID{Id: deviceId}); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func getDevices(ctx context.Context, stub voltha.VolthaServiceClient) (*voltha.Devices, error) {
	if response, err := stub.ListDevices(ctx, &empty.Empty{}); err != nil {
		return nil, err
	} else {
		return response, nil
	}
}

func getLogicalDevices(ctx context.Context, stub voltha.VolthaServiceClient) (*voltha.LogicalDevices, error) {
	if response, err := stub.ListLogicalDevices(ctx, &empty.Empty{}); err != nil {
		return nil, err
	} else {
		return response, nil
	}
}

func IsFlowPresent(lookingFor *voltha.OfpFlowStats, flows []*voltha.OfpFlowStats) bool {
	for _, f := range flows {
		if f.String() == lookingFor.String() {
			return true
		}
	}
	return false
}

func ListDevices(stub voltha.VolthaServiceClient) (*voltha.Devices, error) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	if devices, err := getDevices(ctx, stub); err == nil {
		return devices, nil
	} else {
		return nil, err
	}
}

func sendFlow(ctx context.Context, stub voltha.VolthaServiceClient, flow *ofp.FlowTableUpdate, ch chan interface{}) {
	if response, err := stub.UpdateLogicalDeviceFlowTable(ctx, flow); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func SetLogLevel(stub voltha.VolthaServiceClient, l voltha.Logging) error {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	_, err := stub.UpdateLogLevel(ctx, &l)
	return err
}

func SetAllLogLevel(stub voltha.VolthaServiceClient, l voltha.Logging) error {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	_, err := stub.UpdateLogLevel(ctx, &l)
	return err
}

func SetupGrpcConnectionToCore(grpcHostIP string, grpcPort int) (voltha.VolthaServiceClient, error) {
	grpcHost := fmt.Sprintf("%s:%d", grpcHostIP, grpcPort)
	fmt.Println("Connecting to voltha using:", grpcHost)
	conn, err := grpc.Dial(grpcHost, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return voltha.NewVolthaServiceClient(conn), nil
}

func VerifyLogicalDevices(stub voltha.VolthaServiceClient, parentDevice *voltha.Device, numONUsPerOLT int) (*voltha.LogicalDevices, error) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	retrievedLogicalDevices, err := getLogicalDevices(ctx, stub)
	if err != nil {
		return nil, err
	}
	if len(retrievedLogicalDevices.Items) != 1 {
		return nil, status.Errorf(codes.Internal, "Logical device number incorrect. Expected:{%d}, Created:{%d}", 1, len(retrievedLogicalDevices.Items))
	}

	// Verify that each device has two ports
	for _, ld := range retrievedLogicalDevices.Items {
		if ld.Id == "" ||
			ld.DatapathId == uint64(0) ||
			ld.Desc.HwDesc != "simulated_pon" ||
			ld.Desc.SwDesc != "simulated_pon" ||
			ld.RootDeviceId == "" ||
			ld.Desc.SerialNum == "" ||
			ld.SwitchFeatures.NBuffers != uint32(256) ||
			ld.SwitchFeatures.NTables != uint32(2) ||
			ld.SwitchFeatures.Capabilities != uint32(15) ||
			len(ld.Ports) != 1+numONUsPerOLT ||
			ld.RootDeviceId != parentDevice.Id {
			return nil, status.Errorf(codes.Internal, "incorrect logical device status:{%v}", ld)
		}
		for _, p := range ld.Ports {
			if p.DevicePortNo != p.OfpPort.PortNo ||
				p.OfpPort.State != uint32(4) {
				return nil, status.Errorf(codes.Internal, "incorrect logical ports status:{%v}", p)
			}
			if strings.HasPrefix(p.Id, "nni") {
				if !p.RootPort || fmt.Sprintf("nni-%d", p.DevicePortNo) != p.Id {
					return nil, status.Errorf(codes.Internal, "incorrect nni port status:{%v}", p)
				}
			} else {
				if p.RootPort || fmt.Sprintf("uni-%d", p.DevicePortNo) != p.Id {
					return nil, status.Errorf(codes.Internal, "incorrect uni port status:{%v}", p)
				}
			}
		}
	}
	return retrievedLogicalDevices, nil
}

func VerifyDevices(stub voltha.VolthaServiceClient, numONUsPerOLT int) error {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	retrievedDevices, err := getDevices(ctx, stub)
	if err != nil {
		return err
	}
	if len(retrievedDevices.Items) != 1+numONUsPerOLT {
		return status.Errorf(codes.Internal, "Device number incorrect. Expected:{%d}, Created:{%d}", 1, len(retrievedDevices.Items))
	}
	// Verify that each device has two ports
	for _, d := range retrievedDevices.Items {
		if d.AdminState != voltha.AdminState_ENABLED ||
			d.ConnectStatus != voltha.ConnectStatus_REACHABLE ||
			d.OperStatus != voltha.OperStatus_ACTIVE ||
			d.Type != d.Adapter ||
			d.Id == "" ||
			d.MacAddress == "" ||
			d.SerialNumber == "" {
			return status.Errorf(codes.Internal, "incorrect device state - %s", d.Id)
		}

		if d.Type == "simulated_olt" && (!d.Root || d.ProxyAddress != nil) {
			return status.Errorf(codes.Internal, "invalid olt status:{%v}", d)
		} else if d.Type == "simulated_onu" && (d.Root ||
			d.Vlan == uint32(0) ||
			d.ParentId == "" ||
			d.ProxyAddress.DeviceId == "" ||
			d.ProxyAddress.DeviceType != "simulated_olt") {
			return status.Errorf(codes.Internal, "invalid onu status:{%s}", d.Id)
		}

		if len(d.Ports) != 2 {
			return status.Errorf(codes.Internal, "invalid number of ports:{%s, %v}", d.Id, d.Ports)
		}

		for _, p := range d.Ports {
			if p.AdminState != voltha.AdminState_ENABLED ||
				p.OperStatus != voltha.OperStatus_ACTIVE {
				return status.Errorf(codes.Internal, "invalid port state:{%s, %v}", d.Id, p)
			}

			if p.Type == voltha.Port_ETHERNET_NNI || p.Type == voltha.Port_ETHERNET_UNI {
				if len(p.Peers) != 0 {
					return status.Errorf(codes.Internal, "invalid length of peers:{%s, %d}", d.Id, p.Type)
				}
			} else if p.Type == voltha.Port_PON_OLT {
				if len(p.Peers) != numONUsPerOLT ||
					p.PortNo != uint32(1) {
					return status.Errorf(codes.Internal, "invalid length of peers for PON OLT port:{%s, %v}", d.Id, p)
				}
			} else if p.Type == voltha.Port_PON_ONU {
				if len(p.Peers) != 1 ||
					p.PortNo != uint32(1) {
					return status.Errorf(codes.Internal, "invalid length of peers for PON ONU port:{%s, %v}", d.Id, p)
				}
			}
		}
	}
	return nil
}

func areAdaptersPresent(requiredAdapterNames []string, retrievedAdapters *voltha.Adapters) bool {
	if len(requiredAdapterNames) == 0 {
		return true
	}
	for _, nAName := range requiredAdapterNames {
		found := false
		for _, rA := range retrievedAdapters.Items {
			if nAName == rA.Id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func WaitForAdapterRegistration(stub voltha.VolthaServiceClient, requiredAdapterNames []string, timeout int) (*voltha.Adapters, error) {
	fmt.Println("Waiting for adapter registration ...")
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	ch := make(chan interface{})
	defer close(ch)
	for {
		go sendListAdapters(ctx, stub, ch)
		select {
		case res, ok := <-ch:
			if !ok {
				return nil, status.Error(codes.Aborted, "channel closed")
			} else if er, ok := res.(error); ok {
				return nil, er
			} else if a, ok := res.(*voltha.Adapters); ok {
				if areAdaptersPresent(requiredAdapterNames, a) {
					fmt.Println("All adapters registered:", a.Items)
					return a, nil
				}
			}
		case <-time.After(time.Duration(timeout) * time.Second):
			return nil, status.Error(codes.Aborted, "timeout while waiting for adapter registration")
		}
		time.Sleep(1 * time.Second)
	}
}

func PreProvisionDevice(stub voltha.VolthaServiceClient) (*voltha.Device, error) {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	randomMacAddress := strings.ToUpper(com.GetRandomMacAddress())
	device := &voltha.Device{Type: "simulated_olt", MacAddress: randomMacAddress}
	ch := make(chan interface{})
	defer close(ch)
	go sendCreateDeviceRequest(ctx, stub, device, ch)
	res, ok := <-ch
	if !ok {
		return nil, status.Error(codes.Aborted, "channel closed")
	} else if er, ok := res.(error); ok {
		return nil, er
	} else if d, ok := res.(*voltha.Device); ok {
		return d, nil
	}
	return nil, status.Errorf(codes.Unknown, "cannot provision device:{%v}", device)
}

func EnableDevice(stub voltha.VolthaServiceClient, device *voltha.Device, numONUs int) error {
	if device.AdminState == voltha.AdminState_PREPROVISIONED {
		ui := uuid.New()
		ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
		ch := make(chan interface{})
		defer close(ch)
		go sendEnableDeviceRequest(ctx, stub, device.Id, ch)
		res, ok := <-ch
		if !ok {
			return status.Error(codes.Aborted, "channel closed")
		} else if er, ok := res.(error); ok {
			return er
		} else if _, ok := res.(*empty.Empty); ok {
			return nil
		}
	}
	return status.Errorf(codes.Unknown, "cannot enable device:{%s}", device.Id)
}

func UpdateFlow(stub voltha.VolthaServiceClient, flow *ofp.FlowTableUpdate) error {
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(VOLTHA_SERIAL_NUMBER_KEY, ui.String()))
	ch := make(chan interface{})
	defer close(ch)
	go sendFlow(ctx, stub, flow, ch)
	res, ok := <-ch
	if !ok {
		return status.Error(codes.Aborted, "channel closed")
	} else if er, ok := res.(error); ok {
		return er
	} else if _, ok := res.(*empty.Empty); ok {
		return nil
	}
	return status.Errorf(codes.Unknown, "cannot add flow:{%v}", flow)
}

func StartSimulatedEnv(composePath string) error {
	fmt.Println("Starting simulated environment ...")
	// Start kafka and Etcd
	if err := startKafka(composePath); err != nil {
		return err
	}
	if err := startEtcd(composePath); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)

	//Start the simulated adapters
	if err := startSimulatedOLTAndONUAdapters(composePath); err != nil {
		return err
	}

	//Start the core
	if err := startCore(composePath); err != nil {
		return err
	}

	time.Sleep(10 * time.Second)

	fmt.Println("Simulated environment started.")
	return nil
}

func StopSimulatedEnv(composePath string) error {
	stopSimulatedOLTAndONUAdapters(composePath)
	stopCore(composePath)
	stopKafka(composePath)
	stopEtcd(composePath)
	return nil
}
