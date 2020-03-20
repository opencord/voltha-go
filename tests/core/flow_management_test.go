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
	fu "github.com/opencord/voltha-go/rw_core/utils"
	tu "github.com/opencord/voltha-go/tests/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/common"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	"math"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

var stub voltha.VolthaServiceClient
var volthaSerialNumberKey string
var mutex sync.Mutex

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
	GrpcPort        = 50057
	NumOfOLTs       = 1
	NumOfONUsPerOLT = 4 // This should coincide with the number of onus per olt in adapters-simulated.yml file
	MeterIDStart    = 1
	MeterIDStop     = 1000
)

var logger log.Logger

func setup() {
	var err error

	if logger, err = log.AddPackage(log.JSON, log.WarnLevel, log.Fields{"instanceId": "testing"}); err != nil {
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

	stub, err = tu.SetupGrpcConnectionToCore(grpcHostIP, GrpcPort)
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
	mutex.Lock()
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
	mutex.Unlock()
	return nil
}

func makeSimpleFlowMod(fa *fu.FlowArgs) *ofp.OfpFlowMod {
	matchFields := make([]*ofp.OfpOxmField, 0)
	for _, val := range fa.MatchFields {
		matchFields = append(matchFields, &ofp.OfpOxmField{Field: &ofp.OfpOxmField_OfbField{OfbField: val}})
	}
	return fu.MkSimpleFlowMod(matchFields, fa.Actions, fa.Command, fa.KV)
}

func addEAPOLFlow(stub voltha.VolthaServiceClient, ld *voltha.LogicalDevice, port *voltha.LogicalPort, meterID uint64,
	ch chan interface{}) {
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 2000, "meter_id": meterID},
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(port.OfpPort.PortNo),
			fu.EthType(0x888e),
		},
		Actions: []*ofp.OfpAction{
			fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
		},
	}
	// Don't add meterID 0
	if meterID == 0 {
		delete(fa.KV, "meter_id")
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

func verifyEAPOLFlows(t *testing.T, ld *voltha.LogicalDevice, meterID uint64, lPortNos ...uint32) {
	// First get the flows from the logical device
	fmt.Println("Info: verifying EAPOL flows")
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
			KV: fu.OfpFlowModArgs{"priority": 2000, "meter_id": meterID},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(lPort.OfpPort.PortNo),
				fu.EthType(0x888e),
			},
			Actions: []*ofp.OfpAction{
				fu.Output(uint32(ofp.OfpPortNo_OFPP_CONTROLLER)),
			},
		}
		if meterID == 0 {
			delete(fa.KV, "meter_id")
		}
		expectedLdFlow := fu.MkFlowStat(fa)
		assert.Equal(t, true, tu.IsFlowPresent(expectedLdFlow, lFlows.Items))
	}

	//	Verify the OLT flows
	retrievedOltFlows := allDevices[ld.RootDeviceId].Flows.Items
	assert.Equal(t, NumOfOLTs*getNumUniPort(ld, lPortNos...)*1, len(retrievedOltFlows))
	for _, lPort := range ld.Ports {
		if lPort.RootPort {
			continue
		}
		if filterOutPort(lPort, lPortNos...) {
			continue
		}

		fa := &fu.FlowArgs{
			KV: fu.OfpFlowModArgs{"priority": 2000, "meter_id": meterID},
			MatchFields: []*ofp.OfpOxmOfbField{
				fu.InPort(1),
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

func installEapolFlows(stub voltha.VolthaServiceClient, lDevice *voltha.LogicalDevice, meterID uint64, lPortNos ...uint32) error {
	requestNum := 0
	combineCh := make(chan interface{})
	if len(lPortNos) > 0 {
		fmt.Println("Installing EAPOL flows on ports:", lPortNos)
		for _, p := range lPortNos {
			for _, lport := range lDevice.Ports {
				if !lport.RootPort && lport.OfpPort.PortNo == p {
					go addEAPOLFlow(stub, lDevice, lport, meterID, combineCh)
					requestNum += 1
				}
			}
		}
	} else {
		fmt.Println("Installing EAPOL flows on logical device", lDevice.Id)
		for _, lport := range lDevice.Ports {
			if !lport.RootPort {
				go addEAPOLFlow(stub, lDevice, lport, meterID, combineCh)
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

func deleteEapolFlow(stub voltha.VolthaServiceClient, lDevice *voltha.LogicalDevice, meterID uint64, lPortNo uint32) error {
	fmt.Println("Deleting flows from port ", lPortNo, " of logical device ", lDevice.Id)
	ui := uuid.New()
	var fa *fu.FlowArgs
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, ui.String()))
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 2000, "meter_id": meterID},
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

func runInstallEapolFlows(t *testing.T, stub voltha.VolthaServiceClient, meterID uint64, lPortNos ...uint32) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, ld := range allLogicalDevices {
		err = installEapolFlows(stub, ld, meterID, lPortNos...)
		assert.Nil(t, err)
	}

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for _, ld := range allLogicalDevices {
		verifyEAPOLFlows(t, ld, meterID, lPortNos...)
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

func runDeleteEapolFlows(t *testing.T, stub voltha.VolthaServiceClient, ld *voltha.LogicalDevice, meterID uint64, lPortNos ...uint32) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	if len(lPortNos) == 0 {
		err = deleteAllFlows(stub, ld)
		assert.Nil(t, err)
	} else {
		for _, lPortNo := range lPortNos {
			err = deleteEapolFlow(stub, ld, meterID, lPortNo)
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

func formulateMeterModUpdateRequest(command ofp.OfpMeterModCommand, meterType ofp.OfpMeterBandType, ldID string, rate,
	burstSize uint32, meterID uint32) *ofp.MeterModUpdate {
	meterModUpdateRequest := &ofp.MeterModUpdate{
		Id: ldID,
		MeterMod: &ofp.OfpMeterMod{
			Command: command,
			Flags:   0,
			MeterId: meterID,
			Bands: []*ofp.OfpMeterBandHeader{{
				Type:      meterType,
				Rate:      rate,
				BurstSize: burstSize,
				Data:      nil,
			}},
		},
	}
	return meterModUpdateRequest
}

func formulateMeters(rate, burstsize, meterID uint32, flowCount uint32) *ofp.Meters {
	// Formulate and return the applied meter band
	ofpMeterBandHeaderSlice := []*ofp.OfpMeterBandHeader{{
		Type:      ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER,
		Rate:      rate,
		BurstSize: burstsize,
	}}
	BandStats := []*ofp.OfpMeterBandStats{{}}
	appliedMeters_Meters := []*ofp.OfpMeterEntry{{
		Config: &ofp.OfpMeterConfig{
			Flags:   0,
			MeterId: meterID,
			Bands:   ofpMeterBandHeaderSlice,
		},
		Stats: &ofp.OfpMeterStats{
			MeterId:   meterID,
			BandStats: BandStats,
			FlowCount: flowCount,
		},
	}}
	appliedMeters := &ofp.Meters{
		Items: appliedMeters_Meters,
	}
	return appliedMeters
}

func meterAdd(t *testing.T, meterID, rate, burstSize uint32) {
	var err error
	for _, logicalDevice := range allLogicalDevices {
		meterModupdateRequest := formulateMeterModUpdateRequest(ofp.OfpMeterModCommand_OFPMC_ADD,
			ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER, logicalDevice.Id, rate, burstSize, meterID)
		_, err = stub.UpdateLogicalDeviceMeterTable(context.Background(), meterModupdateRequest)
		assert.Nil(t, err)
	}
}

func meterMod(t *testing.T, meterID, rate, burstSize uint32) {
	for _, logicalDevice := range allLogicalDevices {
		meterModUpdateRequest := formulateMeterModUpdateRequest(ofp.OfpMeterModCommand_OFPMC_MODIFY,
			ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER, logicalDevice.Id, rate, burstSize, meterID)
		_, err := stub.UpdateLogicalDeviceMeterTable(context.Background(), meterModUpdateRequest)
		assert.Nil(t, err)
	}
}

//MeterDel deletes a meter with given meter-ID
func meterDel(t *testing.T, meterID uint32) {
	for _, logicalDevice := range allLogicalDevices {
		meterModUpdateRequest := formulateMeterModUpdateRequest(ofp.OfpMeterModCommand_OFPMC_DELETE,
			ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER, logicalDevice.Id, uint32(1600), uint32(1600), meterID)
		_, err := stub.UpdateLogicalDeviceMeterTable(context.Background(), meterModUpdateRequest)
		assert.Nil(t, err)
	}
}

// WaitGroup passed to this function must not be nil
func addMultipleMeterSequentially(t *testing.T, startMeterId, endMeterId, rate, burstSize uint32, wg *sync.WaitGroup) {
	defer wg.Done()
	for meterID := startMeterId; meterID <= endMeterId; meterID++ {
		meterAdd(t, meterID, rate, burstSize)
	}
}

func modMultipleMeterSequentially(t *testing.T, startMeterId, endMeterId, rate, burstSize uint32, wg *sync.WaitGroup) {
	defer wg.Done()
	for meterID := startMeterId; meterID <= endMeterId; meterID++ {
		meterMod(t, meterID, rate, burstSize)
	}
}

func delMultipleMeterSequential(t *testing.T, startMeterId, endMeterId uint32, wg *sync.WaitGroup) {
	defer wg.Done()
	for meterID := startMeterId; meterID <= endMeterId; meterID++ {
		meterDel(t, meterID)
	}
}

func verifyMeter(t *testing.T, meterID, rate, burstSize, flowCount uint32) {
	expectedMeter := formulateOfpMeterEntry(meterID, flowCount, rate, burstSize)
	isMeterPresent := false
	for _, lD := range allLogicalDevices {
		for _, meter := range lD.Meters.Items {
			isMeterPresent = reflect.DeepEqual(meter, expectedMeter)
			if isMeterPresent == true {
				break
			}
		}
		if isMeterPresent {
			break
		}
	}
	if !isMeterPresent {
		fmt.Printf("Error : Expected %+v\n", expectedMeter)
	}
	assert.Equal(t, true, isMeterPresent)
}

func verifyNoMeters(t *testing.T) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	expectedMeters := &ofp.Meters{}
	for _, logicalDevice := range allLogicalDevices {
		result := reflect.DeepEqual(logicalDevice.Meters, expectedMeters)
		fmt.Println("Meter Present After Delete :--- ", logicalDevice.Meters)
		assert.Equal(t, true, result)
	}
}

func formulateOfpMeterEntry(meterID, flowCount, rate, burstSize uint32) *ofp.OfpMeterEntry {
	value := &ofp.OfpMeterEntry{
		Config: &ofp.OfpMeterConfig{
			MeterId: meterID,
			Bands: []*ofp.OfpMeterBandHeader{{
				Type:      ofp.OfpMeterBandType_OFPMBT_EXPERIMENTER,
				Rate:      rate,
				BurstSize: burstSize,
			}},
		},
		Stats: &ofp.OfpMeterStats{
			MeterId:   meterID,
			FlowCount: flowCount,
			BandStats: []*ofp.OfpMeterBandStats{{}},
		},
	}

	return value
}

func deleteAllMeters(t *testing.T) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for _, lD := range allLogicalDevices {
		for _, meter := range lD.Meters.Items {
			meterDel(t, meter.Config.MeterId)
		}
	}
}

func installMultipleFlowsWithMeter(t *testing.T, startMeterId, stopMeterId uint64, wg *sync.WaitGroup) {
	defer wg.Done()
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for meterID := startMeterId; meterID <= stopMeterId; meterID++ {
		runInstallEapolFlows(t, stub, uint64(meterID), 100, 101)
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

	fmt.Println("Creation of ", NumOfOLTs, " OLT devices took:", time.Since(startTime))

	startTime = time.Now()

	//Enable all parent device - this will enable the child devices as well as validate the child devices
	err = tu.EnableDevice(stub, oltDevice, NumOfONUsPerOLT)
	assert.Nil(t, err)

	fmt.Println("Enabling of  OLT device took:", time.Since(startTime))

	// Wait until the core and adapters sync up after an enabled
	time.Sleep(time.Duration(math.Max(10, float64(NumOfOLTs*NumOfONUsPerOLT)/2)) * time.Second)

	err = tu.VerifyDevices(stub, NumOfONUsPerOLT)
	assert.Nil(t, err)

	lds, err := tu.VerifyLogicalDevices(stub, oltDevice, NumOfONUsPerOLT)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(lds.Items))
}

func TestFlowManagement(t *testing.T) {
	//1. Test creation and activation of the devices.  This will validate the devices as well as the logical device created
	createAndEnableDevices(t)

	//2. Test installation of EAPOL flows
	runInstallEapolFlows(t, stub, 0)

	//3. Test deletion of all EAPOL flows
	runDeleteAllFlows(t, stub)

	//4. Test installation of EAPOL flows on specific ports
	runInstallEapolFlows(t, stub, 0, 101, 102)

	lds, err := tu.ListLogicalDevices(stub)
	assert.Nil(t, err)

	//5. Test deletion of EAPOL on a specific port for a given logical device
	runDeleteEapolFlows(t, stub, lds.Items[0], 0, 101)
}

// Meter Add Test
func TestMeters(t *testing.T) {
	//1. Start test for meter add
	meterAdd(t, 1, 1600, 1600)
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	verifyMeter(t, 1, 1600, 1600, 0)

	//2. Start test for meter mod
	meterMod(t, 1, 6400, 6400)
	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	verifyMeter(t, 1, 6400, 6400, 0)

	//3. Start test for meter del
	meterDel(t, 1)
	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	verifyNoMeters(t)
}

func TestFlowManagementWithMeters(t *testing.T) {
	//1. Delete existing flows
	for _, logicaldevice := range allLogicalDevices {
		err := deleteAllFlows(stub, logicaldevice)
		assert.Nil(t, err)
	}
	deleteAllMeters(t)
	//2. Refresh the local cache so that the changes are reflected here
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//3. Add meter with ID
	meterAdd(t, 1, 1600, 1600)

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//4. Verify that the meter is installed
	verifyMeter(t, 1, 1600, 1600, 0)

	//3. Test installation of EAPOL flows with meter (Add and verify the flow)
	runInstallEapolFlows(t, stub, MeterIDStart)

	//4. Test deletion of all EAPOL flows
	runDeleteAllFlows(t, stub)

	//5. Test installation of EAPOL flows on specific ports with meter (Add and verify the flows)
	runInstallEapolFlows(t, stub, MeterIDStart, 101, 102)

	lds, err := tu.ListLogicalDevices(stub)
	assert.Nil(t, err)

	//6. Test deletion of EAPOL on a specific port for a given logical device
	runDeleteEapolFlows(t, stub, lds.Items[0], MeterIDStart, 101)
}

func TestMultipleMeterAddSequential(t *testing.T) {
	//1. Delete existing flows
	for _, logicaldevice := range allLogicalDevices {
		err := deleteAllFlows(stub, logicaldevice)
		assert.Nil(t, err)
	}

	// Delete All The Previously Installed Meters
	deleteAllMeters(t)
	// Verify that no meters are present
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	verifyNoMeters(t)
	//2. Add Meter Sequentially
	// Here wait-group is not required, creating and passing wait-group only because the function addMultipleMeterSequentially
	// expects wait group.
	var wg sync.WaitGroup
	wg.Add(1)
	addMultipleMeterSequentially(t, MeterIDStart, MeterIDStop, 1600, 1600, &wg)

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//3. Verify that the meters are installed
	for meterID := MeterIDStart; meterID <= MeterIDStop; meterID++ {
		verifyMeter(t, uint32(meterID), 1600, 1600, 0)
	}
}

func TestMeterDeleteParallel(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(4)
	go delMultipleMeterSequential(t, MeterIDStart, MeterIDStop/4, &wg)
	go delMultipleMeterSequential(t, MeterIDStop/4+1, (MeterIDStop / 2), &wg)
	go delMultipleMeterSequential(t, (MeterIDStop/2)+1, MeterIDStop/4*3, &wg)
	go delMultipleMeterSequential(t, (MeterIDStop/4*3)+1, MeterIDStop, &wg)
	wg.Wait()

	verifyNoMeters(t)
}

func TestMultipleMeterAddParallel(t *testing.T) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	for _, logicaldevice := range allLogicalDevices {
		err := deleteAllFlows(stub, logicaldevice)
		assert.Nil(t, err)
	}

	var wg sync.WaitGroup
	wg.Add(4)
	go addMultipleMeterSequentially(t, MeterIDStart, (MeterIDStop / 4), 3200, 3200, &wg)
	go addMultipleMeterSequentially(t, (MeterIDStop/4 + 1), MeterIDStop/2, 3200, 3200, &wg)
	go addMultipleMeterSequentially(t, MeterIDStop/2+1, (MeterIDStop / 4 * 3), 3200, 3200, &wg)
	go addMultipleMeterSequentially(t, (MeterIDStop/4*3)+1, MeterIDStop, 3200, 3200, &wg)

	wg.Wait()

	// Verify the devices
	err = tu.VerifyDevices(stub, NumOfONUsPerOLT)
	assert.Nil(t, err)

	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for meterID := MeterIDStart; meterID <= MeterIDStop; meterID++ {
		fmt.Println("Verifying meter ID :", meterID)
		verifyMeter(t, uint32(meterID), 3200, 3200, 0)
	}
}

func TestMultipleMeterModSequential(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	//1. Modify all the existing meters
	modMultipleMeterSequentially(t, MeterIDStart, MeterIDStop, 6400, 6400, &wg)
	//2. Verify that the device state is proper after updating so many meters
	err := tu.VerifyDevices(stub, NumOfONUsPerOLT)
	assert.Nil(t, err)

	//3. Refresh logical device cache
	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//4. Verify that all the meters got modified
	for meterID := MeterIDStart; meterID <= MeterIDStop; meterID++ {
		fmt.Println("Verifying meter ID :", meterID)
		verifyMeter(t, uint32(meterID), 6400, 6400, 0)
	}
}

func TestMultipleMeterModParallel(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(4)
	//1. Modify all the existing meters
	go modMultipleMeterSequentially(t, MeterIDStart, MeterIDStop/4, 1600, 1600, &wg)
	go modMultipleMeterSequentially(t, (MeterIDStop/4 + 1), MeterIDStop/2, 1600, 1600, &wg)
	go modMultipleMeterSequentially(t, (MeterIDStop/2 + 1), (MeterIDStop / 4 * 3), 1600, 1600, &wg)
	go modMultipleMeterSequentially(t, (MeterIDStop/4*3)+1, MeterIDStop, 1600, 1600, &wg)
	//2. Verify that the device state is proper after updating so many meters
	err := tu.VerifyDevices(stub, NumOfONUsPerOLT)
	assert.Nil(t, err)
	wg.Wait()

	//3. Refresh logical device cache
	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//4. Verify that all the meters got modified
	for meterID := MeterIDStart; meterID <= MeterIDStop; meterID++ {
		fmt.Println("Verifying meter ID :", meterID)
		verifyMeter(t, uint32(meterID), 1600, 1600, 0)
	}
}

func TestMultipleMeterDelSequential(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	//1. Delete all the given meters sequentially
	delMultipleMeterSequential(t, MeterIDStart, MeterIDStop, &wg)
	//2. Verify that all the given meters are deleted
	verifyNoMeters(t)
}

func TestMultipleFlowsWithMeters(t *testing.T) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//1. Delete all the previous flows
	for _, lD := range allLogicalDevices {
		deleteAllFlows(stub, lD)
	}

	//2. Add multiple meters for the flows to refer to
	var wg sync.WaitGroup
	wg.Add(2)
	addMultipleMeterSequentially(t, MeterIDStart, 10, 1600, 1600, &wg)
	// Adding meter verification here again will increase the time taken by the test. So leaving meter verification
	installMultipleFlowsWithMeter(t, 1, 10, &wg)
}

func TestFlowDeletionWithMeterDelete(t *testing.T) {
	err := refreshLocalDeviceCache(stub)
	assert.Nil(t, err)

	//1. Delete all meters
	deleteAllMeters(t)
	//2. Verify that all the meters are deleted
	verifyNoMeters(t)
	//3. As no meters are present, so all flows referring to these meters should also be deleted
	// Flow referring to meterID 1 - 10 were installed in the above test "TestMultipleFlowsWithMeters"
	err = refreshLocalDeviceCache(stub)
	assert.Nil(t, err)
	for _, lD := range allLogicalDevices {
		verifyNOFlows(t, lD, 100, 101)
	}
}

func TestAddFlowWithInvalidMeter(t *testing.T) {
	var err error
	for _, ld := range allLogicalDevices {
		// Adding flows with meter-ID which is not present should throw error stating -
		// "Meter-referred-by-flow-is-not-found-in-logicaldevice"
		err = installEapolFlows(stub, ld, 1, 100, 101)
		assert.NotNil(t, err)
	}
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	shutdown()
	os.Exit(code)
}
