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
package graph

import (
	"context"
	"errors"
	"fmt"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	ld              voltha.LogicalDevice
	olt             voltha.Device
	onus            map[int][]voltha.Device
	logicalDeviceID string
	oltDeviceID     string
	numCalled       int
	lock            sync.RWMutex
)

func init() {
	logicalDeviceID = "ld"
	oltDeviceID = "olt"
	lock = sync.RWMutex{}
}

func setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu int) {
	// Create the OLT and add the NNI ports
	olt = voltha.Device{Id: oltDeviceID, ParentId: logicalDeviceID}
	olt.Ports = make([]*voltha.Port, 0)
	for nniPort := 1; nniPort < numNNIPort+1; nniPort++ {
		p := voltha.Port{PortNo: uint32(nniPort), DeviceId: oltDeviceID, Type: voltha.Port_ETHERNET_NNI}
		olt.Ports = append(olt.Ports, &p)
	}

	// Create the ONUs and associate them with the OLT
	onus = make(map[int][]voltha.Device)
	for pPortNo := numNNIPort + 1; pPortNo < numPonPortOnOlt+numNNIPort+1; pPortNo++ {
		onusOnPon := make([]voltha.Device, 0)
		var onu voltha.Device
		oltPeerPort := uint32(pPortNo)
		oltPonPort := voltha.Port{PortNo: uint32(pPortNo), DeviceId: oltDeviceID, Type: voltha.Port_PON_OLT}
		oltPonPort.Peers = make([]*voltha.Port_PeerPort, 0)
		for i := 0; i < numOnuPerOltPonPort; i++ {
			id := fmt.Sprintf("%d-onu-%d", pPortNo, i)
			onu = voltha.Device{Id: id, ParentId: oltDeviceID, ParentPortNo: uint32(pPortNo)}
			ponPort := voltha.Port{PortNo: 1, DeviceId: onu.Id, Type: voltha.Port_PON_ONU}
			ponPort.Peers = make([]*voltha.Port_PeerPort, 0)
			peerPort := voltha.Port_PeerPort{DeviceId: oltDeviceID, PortNo: oltPeerPort}
			ponPort.Peers = append(ponPort.Peers, &peerPort)
			onu.Ports = make([]*voltha.Port, 0)
			onu.Ports = append(onu.Ports, &ponPort)
			for j := 2; j < numUniPerOnu+2; j++ {
				uniPort := voltha.Port{PortNo: uint32(j), DeviceId: onu.Id, Type: voltha.Port_ETHERNET_UNI}
				onu.Ports = append(onu.Ports, &uniPort)
			}
			onusOnPon = append(onusOnPon, onu)
			oltPeerPort := voltha.Port_PeerPort{DeviceId: onu.Id, PortNo: 1}
			oltPonPort.Peers = append(oltPonPort.Peers, &oltPeerPort)
		}
		onus[pPortNo] = onusOnPon
		olt.Ports = append(olt.Ports, &oltPonPort)
	}

	// Create the logical device
	ld = voltha.LogicalDevice{Id: logicalDeviceID}
	ld.Ports = make([]*voltha.LogicalPort, 0)
	ofpPortNo := 1
	var id string
	//Add olt NNI ports
	for i, port := range olt.Ports {
		if port.Type == voltha.Port_ETHERNET_NNI {
			id = fmt.Sprintf("nni-%d", i)
			lp := voltha.LogicalPort{Id: id, DeviceId: olt.Id, DevicePortNo: port.PortNo, OfpPort: &openflow_13.OfpPort{PortNo: uint32(ofpPortNo)}, RootPort: true}
			ld.Ports = append(ld.Ports, &lp)
			ofpPortNo = ofpPortNo + 1
		}
	}
	//Add onu UNI ports
	for _, onusOnPort := range onus {
		for _, onu := range onusOnPort {
			for j, port := range onu.Ports {
				if port.Type == voltha.Port_ETHERNET_UNI {
					id = fmt.Sprintf("%s:uni-%d", onu.Id, j)
					lp := voltha.LogicalPort{Id: id, DeviceId: onu.Id, DevicePortNo: port.PortNo, OfpPort: &openflow_13.OfpPort{PortNo: uint32(ofpPortNo)}, RootPort: false}
					ld.Ports = append(ld.Ports, &lp)
					ofpPortNo = ofpPortNo + 1
				}
			}
		}
	}
}

func GetDeviceHelper(_ context.Context, id string) (*voltha.Device, error) {
	lock.Lock()
	numCalled++
	lock.Unlock()
	if id == "olt" {
		return &olt, nil
	}
	// Extract the olt pon port from the id ("<ponport>-onu-<onu number>")
	res := strings.Split(id, "-")
	if len(res) == 3 {
		if ponPort, err := strconv.Atoi(res[0]); err == nil {
			for _, onu := range onus[ponPort] {
				if onu.Id == id {
					return &onu, nil
				}
			}

		}
	}
	return nil, errors.New("Not-found")
}

func TestGetRoutesOneShot(t *testing.T) {
	numNNIPort := 1
	numPonPortOnOlt := 1
	numOnuPerOltPonPort := 64
	numUniPerOnu := 1

	setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu)
	getDevice := GetDeviceHelper

	fmt.Println(fmt.Sprintf("Test: Computing all routes. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d", len(ld.Ports), numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))
	// Create a device graph and computes Routes
	start := time.Now()
	dg := NewDeviceGraph(logicalDeviceID, getDevice)
	dg.ComputeRoutes(context.Background(), ld.Ports)
	assert.NotNil(t, dg.GGraph)
	fmt.Println(fmt.Sprintf("Total Time:%dms  Total Routes:%d", time.Since(start)/time.Millisecond, len(dg.Routes)))
	assert.EqualValues(t, (2 * numNNIPort * numPonPortOnOlt * numOnuPerOltPonPort * numUniPerOnu), len(dg.Routes))
}

func TestGetRoutesPerPort(t *testing.T) {
	numNNIPort := 1
	numPonPortOnOlt := 1
	numOnuPerOltPonPort := 64
	numUniPerOnu := 1

	setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu)
	getDevice := GetDeviceHelper

	fmt.Println(fmt.Sprintf("Test: Compute routes per port. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d", len(ld.Ports), numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create a device graph and computes Routes
	start := time.Now()
	var pt time.Time
	dg := NewDeviceGraph(logicalDeviceID, getDevice)
	for k, lp := range ld.Ports {
		if k == len(ld.Ports)-1 {
			pt = time.Now()
		}
		dg.AddPort(context.Background(), lp)
	}
	assert.NotNil(t, dg.GGraph)
	fmt.Println(fmt.Sprintf("Total Time:%dms.  Total Routes:%d. LastPort_Time:%dms", time.Since(start)/time.Millisecond, len(dg.Routes), time.Since(pt)/time.Millisecond))
	assert.EqualValues(t, (2 * numNNIPort * numPonPortOnOlt * numOnuPerOltPonPort * numUniPerOnu), len(dg.Routes))
}

func TestGetRoutesPerPortMultipleUNIs(t *testing.T) {
	numNNIPort := 1
	numPonPortOnOlt := 1
	numOnuPerOltPonPort := 64
	numUniPerOnu := 5

	setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu)
	getDevice := GetDeviceHelper

	fmt.Println(fmt.Sprintf("Test: Compute routes per port - multiple UNIs. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d", len(ld.Ports), numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create a device graph and computes Routes
	start := time.Now()
	var pt time.Time
	dg := NewDeviceGraph(logicalDeviceID, getDevice)
	for k, lp := range ld.Ports {
		if k == len(ld.Ports)-1 {
			pt = time.Now()
		}
		dg.AddPort(context.Background(), lp)
	}
	assert.NotNil(t, dg.GGraph)
	fmt.Println(fmt.Sprintf("Total Time:%dms.  Total Routes:%d. LastPort_Time:%dms", time.Since(start)/time.Millisecond, len(dg.Routes), time.Since(pt)/time.Millisecond))
	assert.EqualValues(t, (2 * numNNIPort * numPonPortOnOlt * numOnuPerOltPonPort * numUniPerOnu), len(dg.Routes))
}

func TestGetRoutesPerPortNoUNI(t *testing.T) {
	numNNIPort := 1
	numPonPortOnOlt := 1
	numOnuPerOltPonPort := 1
	numUniPerOnu := 0

	setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu)
	getDevice := GetDeviceHelper
	assert.EqualValues(t, 1, len(ld.Ports))

	fmt.Println(fmt.Sprintf("Test: Compute routes per port - no UNI. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d", len(ld.Ports), numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create a device graph and computes Routes
	start := time.Now()
	var pt time.Time
	dg := NewDeviceGraph(logicalDeviceID, getDevice)
	for k, lp := range ld.Ports {
		if k == len(ld.Ports)-1 {
			pt = time.Now()
		}
		dg.AddPort(context.Background(), lp)
	}
	assert.NotNil(t, dg.GGraph)
	fmt.Println(fmt.Sprintf("Total Time:%dms.  Total Routes:%d. LastPort_Time:%dms", time.Since(start)/time.Millisecond, len(dg.Routes), time.Since(pt)/time.Millisecond))
	assert.EqualValues(t, 0, len(dg.Routes))
}

func TestGetRoutesPerPortNoONU(t *testing.T) {
	numNNIPort := 1
	numPonPortOnOlt := 1
	numOnuPerOltPonPort := 0
	numUniPerOnu := 0

	setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu)
	getDevice := GetDeviceHelper
	assert.EqualValues(t, 1, len(ld.Ports))

	fmt.Println(fmt.Sprintf("Test: Compute routes per port - no ONU. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d", len(ld.Ports), numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create a device graph and computes Routes
	start := time.Now()
	var pt time.Time
	dg := NewDeviceGraph(logicalDeviceID, getDevice)
	for k, lp := range ld.Ports {
		if k == len(ld.Ports)-1 {
			pt = time.Now()
		}
		dg.AddPort(context.Background(), lp)
	}
	assert.NotNil(t, dg.GGraph)
	fmt.Println(fmt.Sprintf("Total Time:%dms.  Total Routes:%d. LastPort_Time:%dms", time.Since(start)/time.Millisecond, len(dg.Routes), time.Since(pt)/time.Millisecond))
	assert.EqualValues(t, 0, len(dg.Routes))
}

func TestGetRoutesPerPortNoNNI(t *testing.T) {
	numNNIPort := 0
	numPonPortOnOlt := 1
	numOnuPerOltPonPort := 1
	numUniPerOnu := 1

	setupDevices(numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu)
	getDevice := GetDeviceHelper
	assert.EqualValues(t, 1, len(ld.Ports))

	fmt.Println(fmt.Sprintf("Test: Compute routes per port - no NNI. LogicalPorts:%d,  NNI:%d, Pon/OLT:%d, ONU/Pon:%d, Uni/Onu:%d", len(ld.Ports), numNNIPort, numPonPortOnOlt, numOnuPerOltPonPort, numUniPerOnu))

	// Create a device graph and computes Routes
	start := time.Now()
	var pt time.Time
	dg := NewDeviceGraph(logicalDeviceID, getDevice)
	for k, lp := range ld.Ports {
		if k == len(ld.Ports)-1 {
			pt = time.Now()
		}
		dg.AddPort(context.Background(), lp)
	}
	assert.NotNil(t, dg.GGraph)
	fmt.Println(fmt.Sprintf("Total Time:%dms.  Total Routes:%d. LastPort_Time:%dms", time.Since(start)/time.Millisecond, len(dg.Routes), time.Since(pt)/time.Millisecond))
	assert.EqualValues(t, 0, len(dg.Routes))
}

func TestReverseRoute(t *testing.T) {
	// Test the typical use case - 2 hops in a route
	route := make([]RouteHop, 2)
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
	route = make([]RouteHop, 3)
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
	route = make([]RouteHop, numRoutes)
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
