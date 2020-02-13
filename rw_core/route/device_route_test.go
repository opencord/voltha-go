/*
 * Copyright 2018-present Open Networking Foundation
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
package route

import (
	"context"
	"errors"
	"fmt"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	logicalDeviceID = "ld"
	oltDeviceID     = "olt"
)

//portRegistration is a message sent from an OLT device to a logical device to create a logical port
type portRegistration struct {
	port     *voltha.Port
	rootPort bool
}

//onuRegistration is a message sent from an ONU device to an OLT device to register an ONU
type onuRegistration struct {
	onu      *voltha.Device
	oltPonNo uint32
	onuPonNo uint32
}

type logicalDeviceManager struct {
	logicalDevice   *voltha.LogicalDevice
	deviceRoutes    *DeviceRoutes
	ldChnl          chan portRegistration
	numLogicalPorts int
	done            chan struct{}
}

func newLogicalDeviceManager(ld *voltha.LogicalDevice, ch chan portRegistration, totalLogicalPorts int, done chan struct{}) *logicalDeviceManager {
	return &logicalDeviceManager{
		logicalDevice:   ld,
		ldChnl:          ch,
		numLogicalPorts: totalLogicalPorts,
		done:            done,
	}
}

func (ldM *logicalDeviceManager) start(getDevice GetDeviceFunc, buildRoutes bool) {
	ldM.deviceRoutes = NewDeviceRoutes(ldM.logicalDevice.Id, getDevice)
	ofpPortNo := uint32(1)
	for portReg := range ldM.ldChnl {
		if portReg.port == nil {
			// End of registration - exit loop
			break
		}
		lp := &voltha.LogicalPort{
			Id:           portReg.port.Label,
			OfpPort:      &openflow_13.OfpPort{PortNo: ofpPortNo},
			DeviceId:     portReg.port.DeviceId,
			DevicePortNo: portReg.port.PortNo,
			RootPort:     portReg.rootPort,
		}
		ldM.logicalDevice.Ports = append(ldM.logicalDevice.Ports, lp)
		if buildRoutes {
			err := ldM.deviceRoutes.AddPort(context.Background(), lp, ldM.logicalDevice.Ports)
			if err != nil && !strings.Contains(err.Error(), "code = FailedPrecondition") {
				fmt.Println("(Error when adding port:", lp, len(ldM.logicalDevice.Ports), err)
			}
		}
		ofpPortNo++
	}
	// Inform the caller we are now done
	ldM.done <- struct{}{}
}

type oltManager struct {
	olt              *voltha.Device
	logicalDeviceMgr *logicalDeviceManager
	numNNIPort       int
	numPonPortOnOlt  int
	oltChnl          chan onuRegistration
}

func newOltManager(oltDeviceID string, ldMgr *logicalDeviceManager, numNNIPort int, numPonPortOnOlt int, ch chan onuRegistration) *oltManager {
	return &oltManager{
		olt:              &voltha.Device{Id: oltDeviceID, ParentId: ldMgr.logicalDevice.Id, Root: true},
		logicalDeviceMgr: ldMgr,
		numNNIPort:       numNNIPort,
		numPonPortOnOlt:  numPonPortOnOlt,
		oltChnl:          ch,
	}
}

func (oltM *oltManager) start() {
	oltM.olt.Ports = make([]*voltha.Port, 0)
	// Setup the OLT nni ports and trigger the nni ports creation
	for nniPort := 1; nniPort < oltM.numNNIPort+1; nniPort++ {
		p := &voltha.Port{Label: fmt.Sprintf("nni-%d", nniPort), PortNo: uint32(nniPort), DeviceId: oltM.olt.Id, Type: voltha.Port_ETHERNET_NNI}
		oltM.olt.Ports = append(oltM.olt.Ports, p)
		oltM.logicalDeviceMgr.ldChnl <- portRegistration{port: p, rootPort: true}
	}

	// Create OLT pon ports
	for ponPort := oltM.numNNIPort + 1; ponPort < oltM.numPonPortOnOlt+oltM.numNNIPort+1; ponPort++ {
		p := voltha.Port{PortNo: uint32(ponPort), DeviceId: oltM.olt.Id, Type: voltha.Port_PON_OLT}
		oltM.olt.Ports = append(oltM.olt.Ports, &p)
	}

	// Wait for onu registration
	for onuReg := range oltM.oltChnl {
		if onuReg.onu == nil {
			// All onu has registered - exit the loop
			break
		}
		oltM.registerOnu(onuReg.onu, onuReg.oltPonNo, onuReg.onuPonNo)
	}
	// Inform the logical device manager we are done
	oltM.logicalDeviceMgr.ldChnl <- portRegistration{port: nil}
}

func (oltM *oltManager) registerOnu(onu *voltha.Device, oltPonNo uint32, onuPonNo uint32) {
	// Update the olt pon peers
	for _, port := range oltM.olt.Ports {
		if port.Type == voltha.Port_PON_OLT && port.PortNo == oltPonNo {
			port.Peers = append(port.Peers, &voltha.Port_PeerPort{DeviceId: onu.Id, PortNo: onuPonNo})
		}
	}
	// For each uni port on the ONU trigger the creation of a logical port
	for _, port := range onu.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			oltM.logicalDeviceMgr.ldChnl <- portRegistration{port: port, rootPort: false}
		}
	}
}

type onuManager struct {
	oltMgr                  *oltManager
	numOnus                 int
	numUnisPerOnu           int
	startingUniPortNo       int
	numGetDeviceInvoked     int
	numGetDeviceInvokedLock sync.RWMutex
	deviceLock              sync.RWMutex
	onus                    []*voltha.Device
}

func newOnuManager(oltMgr *oltManager, numOnus int, numUnisPerOnu int, startingUniPortNo int) *onuManager {
	return &onuManager{
		oltMgr:            oltMgr,
		numOnus:           numOnus,
		numUnisPerOnu:     numUnisPerOnu,
		startingUniPortNo: startingUniPortNo,
		onus:              make([]*voltha.Device, 0),
	}
}

func (onuM *onuManager) start(startingOltPeerPortNo int, numPonPortOnOlt int) {
	var wg sync.WaitGroup
	for oltPonNo := startingOltPeerPortNo; oltPonNo < startingOltPeerPortNo+numPonPortOnOlt; oltPonNo++ {
		for i := 0; i < onuM.numOnus; i++ {
			wg.Add(1)
			go func(idx int, oltPonNum int) {
				var onu *voltha.Device
				defer wg.Done()
				id := fmt.Sprintf("%d-onu-%d", oltPonNum, idx)
				onu = &voltha.Device{Id: id, ParentId: onuM.oltMgr.olt.Id, ParentPortNo: uint32(oltPonNum)}
				ponPort := &voltha.Port{Label: fmt.Sprintf("%s:pon-%d", onu.Id, idx), PortNo: 1, DeviceId: onu.Id, Type: voltha.Port_PON_ONU}
				ponPort.Peers = make([]*voltha.Port_PeerPort, 0)
				peerPort := voltha.Port_PeerPort{DeviceId: onuM.oltMgr.olt.Id, PortNo: uint32(oltPonNum)}
				ponPort.Peers = append(ponPort.Peers, &peerPort)
				onu.Ports = make([]*voltha.Port, 0)
				onu.Ports = append(onu.Ports, ponPort)
				for j := onuM.startingUniPortNo; j < onuM.numUnisPerOnu+onuM.startingUniPortNo; j++ {
					uniPort := &voltha.Port{Label: fmt.Sprintf("%s:uni-%d", onu.Id, j), PortNo: uint32(j), DeviceId: onu.Id, Type: voltha.Port_ETHERNET_UNI}
					onu.Ports = append(onu.Ports, uniPort)
				}
				onuM.deviceLock.Lock()
				onuM.onus = append(onuM.onus, onu)
				onuM.deviceLock.Unlock()
				onuM.oltMgr.oltChnl <- onuRegistration{
					onu:      onu,
					oltPonNo: uint32(oltPonNum),
					onuPonNo: 1,
				}
			}(i, oltPonNo)
		}
	}
	wg.Wait()
	//send an empty device to indicate the end of onu registration
	onuM.oltMgr.oltChnl <- onuRegistration{
		onu:      nil,
		oltPonNo: 0,
		onuPonNo: 1,
	}
}

func (onuM *onuManager) getOnu(deviceID string) *voltha.Device {
	onuM.deviceLock.Lock()
	defer onuM.deviceLock.Unlock()
	for _, onu := range onuM.onus {
		if onu.Id == deviceID {
			return onu
		}
	}
	return nil
}

func (onuM *onuManager) GetDeviceHelper(_ context.Context, id string) (*voltha.Device, error) {
	onuM.numGetDeviceInvokedLock.Lock()
	onuM.numGetDeviceInvoked++
	onuM.numGetDeviceInvokedLock.Unlock()
	if id == oltDeviceID {
		return onuM.oltMgr.olt, nil
	}
	if onu := onuM.getOnu(id); onu != nil {
		return onu, nil
	}
	return nil, errors.New("not-found")
}

func TestDeviceRoutes_ComputeRoutes(t *testing.T) {
	numNNIPort := 2
	numPonPortOnOlt := 8
	numOnuPerOltPonPort := 32
	numUniPerOnu := 4
	done := make(chan struct{})

	fmt.Println(fmt.Sprintf("Test: Computing all routes. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d",
		numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create all the devices and logical device before computing the routes in one go
	ld := &voltha.LogicalDevice{Id: logicalDeviceID}
	ldMgrChnl := make(chan portRegistration, numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu)
	ldMgr := newLogicalDeviceManager(ld, ldMgrChnl, numNNIPort+numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, done)
	oltMgrChnl := make(chan onuRegistration, numPonPortOnOlt*numOnuPerOltPonPort)
	oltMgr := newOltManager(oltDeviceID, ldMgr, numNNIPort, numPonPortOnOlt, oltMgrChnl)
	onuMgr := newOnuManager(oltMgr, numOnuPerOltPonPort, numUniPerOnu, 2)
	getDevice := onuMgr.GetDeviceHelper
	// Start the managers.  Only the devices are created.  No routes will be built.
	go ldMgr.start(getDevice, false)
	go oltMgr.start()
	go onuMgr.start(numNNIPort+1, numPonPortOnOlt)

	// Wait for all the devices to be created
	<-done
	close(oltMgrChnl)
	close(ldMgrChnl)

	// Computes the routes
	start := time.Now()
	err := ldMgr.deviceRoutes.ComputeRoutes(context.TODO(), ldMgr.logicalDevice.Ports)
	assert.Nil(t, err)

	// Validate the routes are up to date
	assert.True(t, ldMgr.deviceRoutes.IsUpToDate(ld))

	// Validate the expected number of routes
	assert.EqualValues(t, 2*numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, len(ldMgr.deviceRoutes.Routes))

	// Validate the root ports
	for _, port := range ldMgr.logicalDevice.Ports {
		assert.Equal(t, port.RootPort, ldMgr.deviceRoutes.IsRootPort(port.OfpPort.PortNo))
	}
	fmt.Println(fmt.Sprintf("Total Time:%dms, Total Routes:%d NumGetDeviceInvoked:%d", time.Since(start)/time.Millisecond, len(ldMgr.deviceRoutes.Routes), onuMgr.numGetDeviceInvoked))
}

func TestDeviceRoutes_AddPort(t *testing.T) {
	numNNIPort := 2
	numPonPortOnOlt := 8
	numOnuPerOltPonPort := 32
	numUniPerOnu := 4
	done := make(chan struct{})

	fmt.Println(fmt.Sprintf("Test: Computing all routes. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d",
		numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	start := time.Now()
	// Create all the devices and logical device before computing the routes in one go
	ld := &voltha.LogicalDevice{Id: logicalDeviceID}
	ldMgrChnl := make(chan portRegistration, numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu)
	ldMgr := newLogicalDeviceManager(ld, ldMgrChnl, numNNIPort+numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, done)
	oltMgrChnl := make(chan onuRegistration, numPonPortOnOlt*numOnuPerOltPonPort)
	oltMgr := newOltManager(oltDeviceID, ldMgr, numNNIPort, numPonPortOnOlt, oltMgrChnl)
	onuMgr := newOnuManager(oltMgr, numOnuPerOltPonPort, numUniPerOnu, 2)
	getDevice := onuMgr.GetDeviceHelper
	// Start the managers and trigger the routes to be built as the logical ports become available
	go ldMgr.start(getDevice, true)
	go oltMgr.start()
	go onuMgr.start(numNNIPort+1, numPonPortOnOlt)

	// Wait for all the devices to be created and routes created
	<-done
	close(oltMgrChnl)
	close(ldMgrChnl)

	ldMgr.deviceRoutes.Print()

	// Validate the routes are up to date
	assert.True(t, ldMgr.deviceRoutes.IsUpToDate(ld))

	// Validate the expected number of routes
	assert.EqualValues(t, 2*numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, len(ldMgr.deviceRoutes.Routes))

	// Validate the root ports
	for _, port := range ldMgr.logicalDevice.Ports {
		assert.Equal(t, port.RootPort, ldMgr.deviceRoutes.IsRootPort(port.OfpPort.PortNo))
	}

	fmt.Println(fmt.Sprintf("Total Time:%dms, Total Routes:%d NumGetDeviceInvoked:%d", time.Since(start)/time.Millisecond, len(ldMgr.deviceRoutes.Routes), onuMgr.numGetDeviceInvoked))
}

func TestDeviceRoutes_compareRoutesGeneration(t *testing.T) {
	numNNIPort := 2
	numPonPortOnOlt := 8
	numOnuPerOltPonPort := 32
	numUniPerOnu := 4
	done := make(chan struct{})

	fmt.Println(fmt.Sprintf("Test: Computing all routes. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d",
		numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create all the devices and logical device before computing the routes in one go
	ld1 := &voltha.LogicalDevice{Id: logicalDeviceID}
	ldMgrChnl1 := make(chan portRegistration, numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu)
	ldMgr1 := newLogicalDeviceManager(ld1, ldMgrChnl1, numNNIPort+numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, done)
	oltMgrChnl1 := make(chan onuRegistration, numPonPortOnOlt*numOnuPerOltPonPort)
	oltMgr1 := newOltManager(oltDeviceID, ldMgr1, numNNIPort, numPonPortOnOlt, oltMgrChnl1)
	onuMgr1 := newOnuManager(oltMgr1, numOnuPerOltPonPort, numUniPerOnu, 2)
	getDevice := onuMgr1.GetDeviceHelper
	// Start the managers.  Only the devices are created.  No routes will be built.
	go ldMgr1.start(getDevice, false)
	go oltMgr1.start()
	go onuMgr1.start(numNNIPort+1, numPonPortOnOlt)

	// Wait for all the devices to be created
	<-done
	close(oltMgrChnl1)
	close(ldMgrChnl1)

	err := ldMgr1.deviceRoutes.ComputeRoutes(context.TODO(), ldMgr1.logicalDevice.Ports)
	assert.Nil(t, err)

	routesGeneratedAllAtOnce := ldMgr1.deviceRoutes.Routes

	done = make(chan struct{})
	// Create all the devices and logical device before computing the routes in one go
	ld2 := &voltha.LogicalDevice{Id: logicalDeviceID}
	ldMgrChnl2 := make(chan portRegistration, numNNIPort*numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu)
	ldMgr2 := newLogicalDeviceManager(ld2, ldMgrChnl2, numNNIPort+numPonPortOnOlt*numOnuPerOltPonPort*numUniPerOnu, done)
	oltMgrChnl2 := make(chan onuRegistration, numPonPortOnOlt*numOnuPerOltPonPort)
	oltMgr2 := newOltManager(oltDeviceID, ldMgr2, numNNIPort, numPonPortOnOlt, oltMgrChnl2)
	onuMgr2 := newOnuManager(oltMgr2, numOnuPerOltPonPort, numUniPerOnu, 2)
	// Start the managers.  Only the devices are created.  No routes will be built.
	go ldMgr2.start(getDevice, true)
	go oltMgr2.start()
	go onuMgr2.start(numNNIPort+1, numPonPortOnOlt)

	// Wait for all the devices to be created
	<-done
	close(oltMgrChnl2)
	close(ldMgrChnl2)

	routesGeneratedPerPort := ldMgr1.deviceRoutes.Routes
	assert.True(t, isEqual(routesGeneratedAllAtOnce, routesGeneratedPerPort))
}

func TestDeviceRoutes_reverseRoute(t *testing.T) {
	// Test the typical use case - 2 hops in a route
	route := make([]Hop, 2)
	route[0].DeviceID = "d1"
	route[0].Ingress = 1
	route[0].Egress = 2
	route[1].DeviceID = "d2"
	route[1].Ingress = 10
	route[1].Egress = 15

	reverseRoute := getReverseRoute(route)
	assert.Equal(t, 2, len(reverseRoute))
	assert.Equal(t, "d2", reverseRoute[0].DeviceID)
	assert.Equal(t, "d1", reverseRoute[1].DeviceID)
	assert.Equal(t, uint32(15), reverseRoute[0].Ingress)
	assert.Equal(t, uint32(10), reverseRoute[0].Egress)
	assert.Equal(t, uint32(2), reverseRoute[1].Ingress)
	assert.Equal(t, uint32(1), reverseRoute[1].Egress)

	fmt.Println("Reverse of two hops successful.")

	//Test 3 hops in a route
	route = make([]Hop, 3)
	route[0].DeviceID = "d1"
	route[0].Ingress = 1
	route[0].Egress = 2
	route[1].DeviceID = "d2"
	route[1].Ingress = 10
	route[1].Egress = 15
	route[2].DeviceID = "d3"
	route[2].Ingress = 20
	route[2].Egress = 25
	reverseRoute = getReverseRoute(route)
	assert.Equal(t, 3, len(reverseRoute))
	assert.Equal(t, "d3", reverseRoute[0].DeviceID)
	assert.Equal(t, "d2", reverseRoute[1].DeviceID)
	assert.Equal(t, "d1", reverseRoute[2].DeviceID)
	assert.Equal(t, uint32(25), reverseRoute[0].Ingress)
	assert.Equal(t, uint32(20), reverseRoute[0].Egress)
	assert.Equal(t, uint32(15), reverseRoute[1].Ingress)
	assert.Equal(t, uint32(10), reverseRoute[1].Egress)
	assert.Equal(t, uint32(2), reverseRoute[2].Ingress)
	assert.Equal(t, uint32(1), reverseRoute[2].Egress)

	fmt.Println("Reverse of three hops successful.")

	// Test any number of hops in a route
	numRoutes := rand.Intn(100)
	route = make([]Hop, numRoutes)
	deviceIds := make([]string, numRoutes)
	ingressNos := make([]uint32, numRoutes)
	egressNos := make([]uint32, numRoutes)
	for i := 0; i < numRoutes; i++ {
		deviceIds[i] = fmt.Sprintf("d-%d", i)
		ingressNos[i] = rand.Uint32()
		egressNos[i] = rand.Uint32()
	}
	for i := 0; i < numRoutes; i++ {
		route[i].DeviceID = deviceIds[i]
		route[i].Ingress = ingressNos[i]
		route[i].Egress = egressNos[i]
	}
	reverseRoute = getReverseRoute(route)
	assert.Equal(t, numRoutes, len(reverseRoute))
	for i, j := 0, numRoutes-1; j >= 0; i, j = i+1, j-1 {
		assert.Equal(t, deviceIds[j], reverseRoute[i].DeviceID)
		assert.Equal(t, egressNos[j], reverseRoute[i].Ingress)
		assert.Equal(t, ingressNos[j], reverseRoute[i].Egress)
	}

	fmt.Println(fmt.Sprintf("Reverse of %d hops successful.", numRoutes))

	reverseOfReverse := getReverseRoute(reverseRoute)
	assert.Equal(t, route, reverseOfReverse)
	fmt.Println("Reverse of reverse successful.")
}

func isEqual(routes1 map[PathID][]Hop, routes2 map[PathID][]Hop) bool {
	if routes1 == nil && routes2 == nil {
		return true
	}
	if (routes1 == nil && routes2 != nil) || (routes2 == nil && routes1 != nil) {
		return false
	}
	if len(routes1) != len(routes2) {
		return false
	}
	for routeID1, routeHop1 := range routes1 {
		found := false
		for routeID2, routeHop2 := range routes2 {
			if routeID1 == routeID2 {
				if !reflect.DeepEqual(routeHop1, routeHop2) {
					return false
				}
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
