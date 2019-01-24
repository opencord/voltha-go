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
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var ld voltha.LogicalDevice
var olt voltha.Device
var onusOnPort4 []voltha.Device
var onusOnPort5 []voltha.Device

const (
	maxOnuOnPort4 int = 64
	maxOnuOnPort5 int = 64
)

func init() {

	logicalDeviceId := "ld"
	oltDeviceId := "olt"

	// Setup ONUs on OLT port 4
	onusOnPort4 = make([]voltha.Device, 0)
	var onu voltha.Device
	var id string
	oltPeerPort := uint32(4)
	for i := 0; i < maxOnuOnPort4; i++ {
		id := fmt.Sprintf("onu%d", i)
		onu = voltha.Device{Id: id, ParentId: oltDeviceId}
		ponPort := voltha.Port{PortNo: 1, DeviceId: onu.Id, Type: voltha.Port_PON_ONU}
		ponPort.Peers = make([]*voltha.Port_PeerPort, 0)
		peerPort := voltha.Port_PeerPort{DeviceId: oltDeviceId, PortNo: oltPeerPort}
		ponPort.Peers = append(ponPort.Peers, &peerPort)
		uniPort := voltha.Port{PortNo: 2, DeviceId: onu.Id, Type: voltha.Port_ETHERNET_UNI}
		onu.Ports = make([]*voltha.Port, 0)
		onu.Ports = append(onu.Ports, &ponPort)
		onu.Ports = append(onu.Ports, &uniPort)
		onusOnPort4 = append(onusOnPort4, onu)
	}

	// Setup ONUs on OLT port 5
	onusOnPort5 = make([]voltha.Device, 0)
	oltPeerPort = uint32(5)
	for i := 0; i < maxOnuOnPort5; i++ {
		id := fmt.Sprintf("onu%d", i+maxOnuOnPort4)
		onu = voltha.Device{Id: id, ParentId: oltDeviceId}
		ponPort := voltha.Port{PortNo: 1, DeviceId: onu.Id, Type: voltha.Port_PON_ONU}
		ponPort.Peers = make([]*voltha.Port_PeerPort, 0)
		peerPort := voltha.Port_PeerPort{DeviceId: oltDeviceId, PortNo: oltPeerPort}
		ponPort.Peers = append(ponPort.Peers, &peerPort)
		uniPort := voltha.Port{PortNo: 2, DeviceId: onu.Id, Type: voltha.Port_ETHERNET_UNI}
		onu.Ports = make([]*voltha.Port, 0)
		onu.Ports = append(onu.Ports, &ponPort)
		onu.Ports = append(onu.Ports, &uniPort)
		onusOnPort5 = append(onusOnPort5, onu)
	}

	// Setup OLT
	//	Setup the OLT device
	olt = voltha.Device{Id: oltDeviceId, ParentId: logicalDeviceId}
	p1 := voltha.Port{PortNo: 2, DeviceId: oltDeviceId, Type: voltha.Port_ETHERNET_NNI}
	p2 := voltha.Port{PortNo: 3, DeviceId: oltDeviceId, Type: voltha.Port_ETHERNET_NNI}
	p3 := voltha.Port{PortNo: 4, DeviceId: oltDeviceId, Type: voltha.Port_PON_OLT}
	p4 := voltha.Port{PortNo: 5, DeviceId: oltDeviceId, Type: voltha.Port_PON_OLT}
	p3.Peers = make([]*voltha.Port_PeerPort, 0)
	for _, onu := range onusOnPort4 {
		peerPort := voltha.Port_PeerPort{DeviceId: onu.Id, PortNo: p3.PortNo}
		p3.Peers = append(p3.Peers, &peerPort)
	}
	p4.Peers = make([]*voltha.Port_PeerPort, 0)
	for _, onu := range onusOnPort5 {
		peerPort := voltha.Port_PeerPort{DeviceId: onu.Id, PortNo: p4.PortNo}
		p4.Peers = append(p4.Peers, &peerPort)
	}
	olt.Ports = make([]*voltha.Port, 0)
	olt.Ports = append(olt.Ports, &p1)
	olt.Ports = append(olt.Ports, &p2)
	olt.Ports = append(olt.Ports, &p3)
	olt.Ports = append(olt.Ports, &p4)

	// Setup the logical device
	ld = voltha.LogicalDevice{Id: logicalDeviceId}
	ld.Ports = make([]*voltha.LogicalPort, 0)
	ofpPortNo := 1
	//Add olt ports
	for i, port := range olt.Ports {
		if port.Type == voltha.Port_ETHERNET_NNI {
			id = fmt.Sprintf("nni-%d", i)
			lp := voltha.LogicalPort{Id: id, DeviceId: olt.Id, DevicePortNo: port.PortNo, OfpPort: &openflow_13.OfpPort{PortNo: uint32(ofpPortNo)}, RootPort: true}
			ld.Ports = append(ld.Ports, &lp)
			ofpPortNo = ofpPortNo + 1
		}
	}
	//Add onu ports on port 4
	for i, onu := range onusOnPort4 {
		for _, port := range onu.Ports {
			if port.Type == voltha.Port_ETHERNET_UNI {
				id = fmt.Sprintf("uni-%d", i)
				lp := voltha.LogicalPort{Id: id, DeviceId: onu.Id, DevicePortNo: port.PortNo, OfpPort: &openflow_13.OfpPort{PortNo: uint32(ofpPortNo)}, RootPort: false}
				ld.Ports = append(ld.Ports, &lp)
				ofpPortNo = ofpPortNo + 1
			}
		}
	}
	//Add onu ports on port 5
	for i, onu := range onusOnPort5 {
		for _, port := range onu.Ports {
			if port.Type == voltha.Port_ETHERNET_UNI {
				id = fmt.Sprintf("uni-%d", i+10)
				lp := voltha.LogicalPort{Id: id, DeviceId: onu.Id, DevicePortNo: port.PortNo, OfpPort: &openflow_13.OfpPort{PortNo: uint32(ofpPortNo)}, RootPort: false}
				ld.Ports = append(ld.Ports, &lp)
				ofpPortNo = ofpPortNo + 1
			}
		}
	}
}

func GetDeviceHelper(id string) (*voltha.Device, error) {
	if id == "olt" {
		return &olt, nil
	}
	for _, onu := range onusOnPort4 {
		if onu.Id == id {
			return &onu, nil
		}
	}
	for _, onu := range onusOnPort5 {
		if onu.Id == id {
			return &onu, nil
		}
	}
	return nil, errors.New("Not-found")
}

func TestGetRoutes(t *testing.T) {

	getDevice := GetDeviceHelper

	// Create a device graph and computes Routes
	start := time.Now()
	dg := NewDeviceGraph(getDevice)
	dg.ComputeRoutes(ld.Ports)
	fmt.Println("Total Time creating graph & compute Routes:", time.Since(start))
	assert.NotNil(t, dg.GGraph)
	assert.EqualValues(t, (maxOnuOnPort4*4 + maxOnuOnPort5*4), len(dg.Routes))
	//for k, v := range dg.Routes {
	//	fmt.Println("key", k, " value:", v)
	//}

}
