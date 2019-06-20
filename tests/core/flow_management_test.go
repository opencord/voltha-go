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
	"github.com/opencord/voltha-go/common/log"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	tu "github.com/opencord/voltha-go/tests/utils"
	"github.com/opencord/voltha-protos/go/common"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
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
 This local "integration" test uses one RW-Core, one simulated_olt and one simulated_onu adapter to test flows
(add/delete), in a development environment. It uses docker-compose to set up the local environment. However, it can
easily be extended to run in k8s environment.

The compose files used are located under %GOPATH/src/github.com/opencord/voltha-go/compose. If the GOPATH is not set
then you can specify the location of the compose files by using COMPOSE_PATH to set the compose files location.

To run this test: DOCKER_HOST_IP=<local IP> go test -v

NOTE:  Since this is an integration test that involves several containers and features (device creation, device
activation, validation of parent and discovered devices, validation of logical device as well as add/delete flows)
then a failure can occur anywhere not just when testing flows.

*/

var allDevices map[string]*voltha.Device
var allLogicalDevices map[string]*voltha.LogicalDevice

var composePath string

const (
	GRPC_PORT        = 50057
	NUM_OLTS         = 1
	NUM_ONUS_PER_OLT = 4 // This should coincide with the number of onus per olt in adapters-simulated.yml file
)

func setup() {
	var err error

	if _, err = log.AddPackage(log.JSON, log.WarnLevel, log.Fields{"instanceId": "testing"}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	log.UpdateAllLoggers(log.Fields{"instanceId": "testing"})
	log.SetAllLogLevel(log.ErrorLevel)

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
	if _, err = tu.WaitForAdapterRegistration(stub, adapters, 20); err != nil {
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

func makeSimpleFlowMod(fa *fu.FlowArgs) *ofp.OfpFlowMod {
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return fu.MkSimpleFlowMod(matchFields, fa.Actions, fa.Command, fa.KV)
}

func addEAPOLFlow(stub voltha.VolthaServiceClient, ld *voltha.LogicalDevice, port *voltha.LogicalPort, ch chan interface{}) {
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 2000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(port.OfpPort.PortNo),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	f := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: ld.Id}

	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	if response, err := stub.UpdateLogicalDeviceFlowTable(ctx, &f); err != nil {
		ch <- err
	} else {
		ch <- response
	}
}

func getNumUniPort(ld *voltha.LogicalDevice, lPortNos ...uint32) int {
	num := 0
	if len(lPortNos) > 0 {
		for _, pNo := range lPortNos {
			for _, lPort := range ld.Ports {
				if !lPort.RootPort && lPort.OfpPort.PortNo == pNo {
					num += 1
				}
			}
		}
	} else {
		for _, port := range ld.Ports {
			if !port.RootPort {
				num += 1
			}
		}
	}
	return num
}

func filterOutPort(lPort *voltha.LogicalPort, lPortNos ...uint32) bool {
	if len(lPortNos) == 0 {
		return false
	}
	for _, pNo := range lPortNos {
		if lPort.OfpPort.PortNo == pNo {
			return false
		}
	}
	return true
}

func verifyEAPOLFlows(t *testing.T, ld *voltha.LogicalDevice, lPortNos ...uint32) {
	// First get the flows from the logical device
	lFlows := ld.Flows
	assert.Equal(t, getNumUniPort(ld, lPortNos...), len(lFlows.Items))

	onuDeviceId := ""

	// Verify that the flows in the logical device is what was pushed
	for _, lPort := range ld.Ports {
		if lPort.RootPort {
			continue
		}
		if filterOutPort(lPort, lPortNos...) {
			continue
		}
		onuDeviceId = lPort.DeviceId
		var fa *fu.FlowArgs
		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": 2000},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(lPort.OfpPort.PortNo),
				fu.EthType(0x888e),
			},
			Actions: []*ofp.OfpAction{
				fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
			},
		}
		expectedLdFlow := fu.MkFlowStat(fa)
		assert.Equal(t, true, tu.IsFlowPresent(expectedLdFlow, lFlows.Items))
	}

	//	Verify the OLT flows
	retrievedOltFlows := allDevices[ld.RootDeviceId].Flows.Items
	assert.Equal(t, NUM_OLTS*getNumUniPort(ld, lPortNos...)*2, len(retrievedOltFlows))
	for _, lPort := range ld.Ports {
		if lPort.RootPort {
			continue
		}
		if filterOutPort(lPort, lPortNos...) {
			continue
		}

		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": 2000},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(1),
				fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | lPort.OfpPort.PortNo),
				fu.TunnelId(uint64(lPort.OfpPort.PortNo)),
				fu.EthType(0x888e),
			},
			Actions: []*ofp.OfpAction{
				fu.PushVlan(0x8100),
				fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000)),
				fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
			},
		}
		expectedOltFlow := fu.MkFlowStat(fa)
		assert.Equal(t, true, tu.IsFlowPresent(expectedOltFlow, retrievedOltFlows))

		fa = &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": 2000},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(2),
				fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 4000),
				fu.VlanPcp(0),
				fu.Metadata_ofp(uint64(lPort.OfpPort.PortNo)),
				fu.TunnelId(uint64(lPort.OfpPort.PortNo)),
			},
			Actions: []*ofp.OfpAction{
				fu.PopVlan(),
				fu.Output(1),
			},
		}
		expectedOltFlow = fu.MkFlowStat(fa)
		assert.Equal(t, true, tu.IsFlowPresent(expectedOltFlow, retrievedOltFlows))
	}
	//	Verify the ONU flows
	retrievedOnuFlows := allDevices[onuDeviceId].Flows.Items
	assert.Equal(t, 0, len(retrievedOnuFlows))
}

func verifyNOFlows(t *testing.T, ld *voltha.LogicalDevice, lPortNos ...uint32) {
	if len(lPortNos) == 0 {
		assert.Equal(t, 0, len(ld.Flows.Items))
		for _, d := range allDevices {
			if d.ParentId == ld.Id {
				assert.Equal(t, 0, len(d.Flows.Items))
			}
		}
		return
	}
	for _, p := range lPortNos {
		// Check absence of flows in logical device for that port
		for _, f := range ld.Flows.Items {
			assert.NotEqual(t, p, fu.GetInPort(f))
		}
		// Check absence of flows in the parent device for that port
		for _, d := range allDevices {
			if d.ParentId == ld.Id {
				for _, f := range d.Flows.Items {
					assert.NotEqual(t, p, fu.GetTunnelId(f))
				}
			}
		}
		//	TODO: check flows in child device.  Not required for the use cases being tested
	}

}

func installEapolFlows(stub voltha.VolthaServiceClient, lDevice *voltha.LogicalDevice, lPortNos ...uint32) error {
	requestNum := 0
	combineCh := make(chan interface{})
	if len(lPortNos) > 0 {
		fmt.Println("Installing EAPOL flows on ports:", lPortNos)
		for _, p := range lPortNos {
			for _, lport := range lDevice.Ports {
				if !lport.RootPort && lport.OfpPort.PortNo == p {
					go addEAPOLFlow(stub, lDevice, lport, combineCh)
					requestNum += 1
				}
			}
		}
	} else {
		fmt.Println("Installing EAPOL flows on logical device ", lDevice.Id)
		for _, lport := range lDevice.Ports {
			if !lport.RootPort {
				go addEAPOLFlow(stub, lDevice, lport, combineCh)
				requestNum += 1
			}
		}

	}
	receivedResponse := 0
	var err error
	for {
		select {
		case res, ok := <-combineCh:
			receivedResponse += 1
			if !ok {
			} else if er, ok := res.(error); ok {
				err = er
			}
		}
		if receivedResponse == requestNum {
			break
		}
	}
	return err
}

func deleteAllFlows(stub voltha.VolthaServiceClient, lDevice *voltha.LogicalDevice) error {
	fmt.Println("Deleting all flows for logical device:", lDevice.Id)
	ui := uuid.New()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	ch := make(chan interface{})
	defer close(ch)
	fa := &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"table_id": uint64(ofp.OfpTable_OFPTT_ALL),
			"cookie_mask": 0,
			"out_port":    uint64(ofp.OfpPortNo_OFPP_ANY),
			"out_group":   uint64(ofp.OfpGroup_OFPG_ANY),
		},
	}
	cmd := ofp.OfpFlowModCommand_OFPFC_DELETE
	fa.Command = &cmd
	flowMod := fu.MkSimpleFlowMod(fu.ToOfpOxmField(fa.MatchFields), fa.Actions, fa.Command, fa.KV)
	f := ofp.FlowTableUpdate{FlowMod: flowMod, Id: lDevice.Id}
	_, err := stub.UpdateLogicalDeviceFlowTable(ctx, &f)
	return err
}

func deleteEapolFlow(stub voltha.VolthaServiceClient, lDevice *voltha.LogicalDevice, lPortNo uint32) error {
	fmt.Println("Deleting flows from port ", lPortNo, " of logical device ", lDevice.Id)
	ui := uuid.New()
	var fa *fu.FlowArgs
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 2000},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(lPortNo),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	cmd := ofp.OfpFlowModCommand_OFPFC_DELETE
	fa.Command = &cmd
	f := ofp.FlowTableUpdate{FlowMod: makeSimpleFlowMod(fa), Id: lDevice.Id}
	_, err := stub.UpdateLogicalDeviceFlowTable(ctx, &f)
	return err
}

func runInstallEapolFlows(t *testing.T, stub voltha.VolthaServiceClient, lPortNos ...uint32) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, ld := range allLogicalDevices {
		err = installEapolFlows(stub, ld, lPortNos...)
		assert.Nil(t, err)
	}

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, ld := range allLogicalDevices {
		verifyEAPOLFlows(t, ld, lPortNos...)
	}
}

func runDeleteAllFlows(t *testing.T, stub voltha.VolthaServiceClient) {
	fmt.Println("Removing ALL flows ...")
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, ld := range allLogicalDevices {
		err = deleteAllFlows(stub, ld)
		assert.Nil(t, err)
	}

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, ld := range allLogicalDevices {
		verifyNOFlows(t, ld)
	}
}

func runDeleteEapolFlows(t *testing.T, stub voltha.VolthaServiceClient, ld *voltha.LogicalDevice, lPortNos ...uint32) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	if len(lPortNos) == 0 {
		err = deleteAllFlows(stub, ld)
		assert.Nil(t, err)
	} else {
		for _, lPortNo := range lPortNos {
			err = deleteEapolFlow(stub, ld, lPortNo)
			assert.Nil(t, err)
		}
	}

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, lde := range allLogicalDevices {
		if lde.Id == ld.Id {
			verifyNOFlows(t, lde, lPortNos...)
			break
		}
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

func TestFlowManagement(t *testing.T) {
	//1. Test creation and activation of the devices.  This will validate the devices as well as the logical device created/
	createAndEnableDevices(t)

	//2. Test installation of EAPOL flows
	runInstallEapolFlows(t, stub)

	//3. Test deletion of all EAPOL flows
	runDeleteAllFlows(t, stub)

	//4. Test installation of EAPOL flows on specific ports
	runInstallEapolFlows(t, stub, 101, 102)

	lds, err := tu.ListLogicalDevices(stub)
	assert.Nil(t, err)

	//5. Test deletion of EAPOL on a specific port for a given logical device
	runDeleteEapolFlows(t, stub, lds.Items[0], 101)
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
