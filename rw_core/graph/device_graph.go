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
	"github.com/gyuho/goraph"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/voltha"
	"strconv"
	"strings"
	"sync"
)

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

type RouteHop struct {
	DeviceID string
	Ingress  uint32
	Egress   uint32
}

type OFPortLink struct {
	Ingress uint32
	Egress  uint32
}

type GetDeviceFunc func(id string) (*voltha.Device, error)

func concatDeviceIdPortId(deviceId string, portId uint32) string {
	return fmt.Sprintf("%s:%d", deviceId, portId)
}

func splitIntoDeviceIdPortId(id string) (string, uint32, error) {
	result := strings.Split(id, ":")
	if len(result) != 2 {
		return "", 0, errors.New(fmt.Sprintf("invalid-id-%s", id))
	}
	if temp, err := strconv.ParseInt(result[1], 10, 32); err != nil {
		return "", 0, errors.New(fmt.Sprintf("invalid-id-%s-%s", id, err.Error()))
	} else {
		return result[0], uint32(temp), nil
	}
}

type DeviceGraph struct {
	GGraph        goraph.Graph
	getDevice     GetDeviceFunc
	logicalPorts  []*voltha.LogicalPort
	RootPorts     map[uint32]uint32
	Routes        map[OFPortLink][]RouteHop
	boundaryPorts sync.Map
}

func NewDeviceGraph(getDevice GetDeviceFunc) *DeviceGraph {
	var dg DeviceGraph
	dg.GGraph = goraph.NewGraph()
	dg.getDevice = getDevice
	return &dg
}

func (dg *DeviceGraph) ComputeRoutes(lps []*voltha.LogicalPort) {
	if dg == nil {
		return
	}
	dg.logicalPorts = lps
	// Set the root ports
	dg.RootPorts = make(map[uint32]uint32)
	for _, lp := range lps {
		if lp.RootPort {
			dg.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
		}
	}
	// set the boundary ports
	dg.boundaryPorts.Range(func(key interface{}, value interface{}) bool {
		dg.boundaryPorts.Delete(key)
		return true
	})
	//dg.boundaryPorts = sync.Map{}

	for _, lp := range lps {
		dg.boundaryPorts.Store(fmt.Sprintf("%s:%d", lp.DeviceId, lp.DevicePortNo), lp.OfpPort.PortNo)
	}
	dg.Routes = make(map[OFPortLink][]RouteHop)

	// Build the graph
	var device *voltha.Device
	devicesAdded := make(map[string]string)
	portsAdded := make(map[string]string)
	for _, logicalPort := range dg.logicalPorts {
		device, _ = dg.getDevice(logicalPort.DeviceId)
		dg.GGraph = dg.addDevice(device, dg.GGraph, &devicesAdded, &portsAdded, dg.boundaryPorts)
	}
	dg.Routes = dg.buildRoutes()
}

func (dg *DeviceGraph) addDevice(device *voltha.Device, g goraph.Graph, devicesAdded *map[string]string, portsAdded *map[string]string,
	boundaryPorts sync.Map) goraph.Graph {

	if device == nil {
		return g
	}

	if _, exist := (*devicesAdded)[device.Id]; exist {
		return g
	}
	g.AddNode(goraph.NewNode(device.Id))
	(*devicesAdded)[device.Id] = device.Id

	var portId string
	var peerPortId string
	for _, port := range device.Ports {
		portId = concatDeviceIdPortId(device.Id, port.PortNo)
		if _, exist := (*portsAdded)[portId]; !exist {
			(*portsAdded)[portId] = portId
			g.AddNode(goraph.NewNode(portId))
			g.AddEdge(goraph.StringID(device.Id), goraph.StringID(portId), 1)
			g.AddEdge(goraph.StringID(portId), goraph.StringID(device.Id), 1)
		}
		for _, peer := range port.Peers {
			if _, exist := (*devicesAdded)[peer.DeviceId]; !exist {
				d, _ := dg.getDevice(peer.DeviceId)
				g = dg.addDevice(d, g, devicesAdded, portsAdded, boundaryPorts)
			} else {
				peerPortId = concatDeviceIdPortId(peer.DeviceId, peer.PortNo)
				g.AddEdge(goraph.StringID(portId), goraph.StringID(peerPortId), 1)
				g.AddEdge(goraph.StringID(peerPortId), goraph.StringID(portId), 1)
			}
		}
	}
	return g
}

func (dg *DeviceGraph) IsRootPort(port uint32) bool {
	_, exist := dg.RootPorts[port]
	return exist
}

func (dg *DeviceGraph) buildRoutes() map[OFPortLink][]RouteHop {
	var pathIds []goraph.ID
	path := make([]RouteHop, 0)
	paths := make(map[OFPortLink][]RouteHop)
	var err error
	var hop RouteHop

	dg.boundaryPorts.Range(func(src, srcPort interface{}) bool {
		source := src.(string)
		sourcePort := srcPort.(uint32)

		dg.boundaryPorts.Range(func(dst, dstPort interface{}) bool {
			target := dst.(string)
			targetPort := dstPort.(uint32)

			if source == target {
				return true
			}
			//Ignore NNI - NNI Routes
			if dg.IsRootPort(sourcePort) && dg.IsRootPort(targetPort) {
				return true
			}

			//Ignore UNI - UNI Routes
			if !dg.IsRootPort(sourcePort) && !dg.IsRootPort(targetPort) {
				return true
			}

			if pathIds, _, err = goraph.Dijkstra(dg.GGraph, goraph.StringID(source), goraph.StringID(target)); err != nil {
				log.Errorw("no-path", log.Fields{"source": source, "target": target, "error": err})
				return true
			}
			if len(pathIds)%3 != 0 {
				return true
			}
			var deviceId string
			var ingressPort uint32
			var egressPort uint32
			for i := 0; i < len(pathIds); i = i + 3 {
				if deviceId, ingressPort, err = splitIntoDeviceIdPortId(pathIds[i].String()); err != nil {
					log.Errorw("id-error", log.Fields{"source": source, "target": target, "error": err})
					break
				}
				if _, egressPort, err = splitIntoDeviceIdPortId(pathIds[i+2].String()); err != nil {
					log.Errorw("id-error", log.Fields{"source": source, "target": target, "error": err})
					break
				}
				hop = RouteHop{Ingress: ingressPort, DeviceID: deviceId, Egress: egressPort}
				path = append(path, hop)
			}
			tmp := make([]RouteHop, len(path))
			copy(tmp, path)
			path = nil
			paths[OFPortLink{Ingress: sourcePort, Egress: targetPort}] = tmp
			return true
		})
		return true
	})
	return paths
}

func (dg *DeviceGraph) GetDeviceNodeIds() map[string]string {
	nodeIds := make(map[string]string)
	nodesMap := dg.GGraph.GetNodes()
	for id, node := range nodesMap {
		if len(strings.Split(node.String(), ":")) != 2 { // not port node
			nodeIds[id.String()] = id.String()
		}
	}
	return nodeIds
}
