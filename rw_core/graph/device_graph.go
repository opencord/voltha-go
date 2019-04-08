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
	"github.com/opencord/voltha-protos/go/voltha"
	"strconv"
	"strings"
	"sync"
)

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
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

type ofPortLinkToPath struct {
	link OFPortLink
	path []RouteHop
}

type GetDeviceFunc func(id string) (*voltha.Device, error)

type DeviceGraph struct {
	logicalDeviceId    string
	GGraph             goraph.Graph
	getDeviceFromModel GetDeviceFunc
	logicalPorts       []*voltha.LogicalPort
	rootPortsString    map[string]uint32
	nonRootPortsString map[string]uint32
	RootPorts          map[uint32]uint32
	rootPortsLock      sync.RWMutex
	Routes             map[OFPortLink][]RouteHop
	graphBuildLock     sync.RWMutex
	boundaryPorts      map[string]uint32
	boundaryPortsLock  sync.RWMutex
	cachedDevices      map[string]*voltha.Device
	cachedDevicesLock  sync.RWMutex
	devicesAdded       map[string]string
	portsAdded         map[string]string
}

func NewDeviceGraph(logicalDeviceId string, getDevice GetDeviceFunc) *DeviceGraph {
	var dg DeviceGraph
	dg.logicalDeviceId = logicalDeviceId
	dg.GGraph = goraph.NewGraph()
	dg.getDeviceFromModel = getDevice
	dg.graphBuildLock = sync.RWMutex{}
	dg.cachedDevicesLock = sync.RWMutex{}
	dg.rootPortsLock = sync.RWMutex{}
	dg.devicesAdded = make(map[string]string)
	dg.portsAdded = make(map[string]string)
	dg.rootPortsString = make(map[string]uint32)
	dg.nonRootPortsString = make(map[string]uint32)
	dg.RootPorts = make(map[uint32]uint32)
	dg.boundaryPorts = make(map[string]uint32)
	dg.Routes = make(map[OFPortLink][]RouteHop)
	dg.cachedDevices = make(map[string]*voltha.Device)
	log.Debug("new device graph created ...")
	return &dg
}

//IsRootPort returns true if the port is a root port on a logical device
func (dg *DeviceGraph) IsRootPort(port uint32) bool {
	dg.rootPortsLock.RLock()
	defer dg.rootPortsLock.RUnlock()
	_, exist := dg.RootPorts[port]
	return exist
}

//GetDeviceNodeIds retrieves all the nodes in the device graph
func (dg *DeviceGraph) GetDeviceNodeIds() map[string]string {
	dg.graphBuildLock.RLock()
	defer dg.graphBuildLock.RUnlock()
	nodeIds := make(map[string]string)
	nodesMap := dg.GGraph.GetNodes()
	for id, node := range nodesMap {
		if len(strings.Split(node.String(), ":")) != 2 { // not port node
			nodeIds[id.String()] = id.String()
		}
	}
	return nodeIds
}

//ComputeRoutes creates a device graph from the logical ports and then calculates all the routes
//between the logical ports.  This will clear up the graph and routes if there were any.
func (dg *DeviceGraph) ComputeRoutes(lps []*voltha.LogicalPort) {
	if dg == nil {
		return
	}
	dg.graphBuildLock.Lock()
	defer dg.graphBuildLock.Unlock()

	// Clear the graph
	dg.reset()

	dg.logicalPorts = lps

	// Set the root, non-root ports and boundary ports
	for _, lp := range lps {
		portId := concatDeviceIdPortId(lp.DeviceId, lp.DevicePortNo)
		if lp.RootPort {
			dg.rootPortsString[portId] = lp.OfpPort.PortNo
			dg.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
		} else {
			dg.nonRootPortsString[portId] = lp.OfpPort.PortNo
		}
		dg.boundaryPorts[portId] = lp.OfpPort.PortNo
	}

	// Build the graph
	var device *voltha.Device
	for _, logicalPort := range dg.logicalPorts {
		device, _ = dg.getDevice(logicalPort.DeviceId)
		dg.GGraph = dg.addDevice(device, dg.GGraph, &dg.devicesAdded, &dg.portsAdded, dg.boundaryPorts)
	}

	dg.Routes = dg.buildRoutes()
}

// AddPort adds a port to the graph.  If the graph is empty it will just invoke ComputeRoutes function
func (dg *DeviceGraph) AddPort(lp *voltha.LogicalPort) {
	//  If the graph does not exist invoke ComputeRoutes.
	if len(dg.boundaryPorts) == 0 {
		dg.ComputeRoutes([]*voltha.LogicalPort{lp})
		return
	}

	dg.graphBuildLock.Lock()
	defer dg.graphBuildLock.Unlock()

	portId := concatDeviceIdPortId(lp.DeviceId, lp.DevicePortNo)

	//	If the port is already part of the boundary ports, do nothing
	if dg.portExist(portId) {
		fmt.Println("port exists")
		return
	}
	// Add the device where this port is located to the device graph. If the device is already added then
	// only the missing port will be added
	device, _ := dg.getDevice(lp.DeviceId)
	dg.GGraph = dg.addDevice(device, dg.GGraph, &dg.devicesAdded, &dg.portsAdded, dg.boundaryPorts)

	if lp.RootPort {
		// Compute the route from this root port to all non-root ports
		dg.rootPortsString[portId] = lp.OfpPort.PortNo
		dg.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
		dg.Routes = dg.buildPathsToAllNonRootPorts(lp)
	} else {
		// Compute the route from this port to all root ports
		dg.nonRootPortsString[portId] = lp.OfpPort.PortNo
		dg.Routes = dg.buildPathsToAllRootPorts(lp)
	}

	dg.Print()
}

func (dg *DeviceGraph) Print() error {
	if level, err := log.GetPackageLogLevel(); err == nil && level == log.DebugLevel {
		output := ""
		routeNumber := 1
		for k, v := range dg.Routes {
			key := fmt.Sprintf("LP:%d->LP:%d", k.Ingress, k.Egress)
			val := ""
			for _, i := range v {
				val += fmt.Sprintf("{%d->%s->%d},", i.Ingress, i.DeviceID, i.Egress)
			}
			val = val[:len(val)-1]
			output += fmt.Sprintf("%d:{%s=>%s}   ", routeNumber, key, fmt.Sprintf("[%s]", val))
			routeNumber += 1
		}
		log.Debugw("graph_routes", log.Fields{"lDeviceId": dg.logicalDeviceId, "Routes": output})
	}
	return nil
}

//getDevice returns the device either from the local cache (default) or from the model.
//TODO: Set a cache timeout such that we do not use invalid data.  The full device lifecycle should also
//be taken in consideration
func (dg *DeviceGraph) getDevice(id string) (*voltha.Device, error) {
	dg.cachedDevicesLock.RLock()
	if d, exist := dg.cachedDevices[id]; exist {
		dg.cachedDevicesLock.RUnlock()
		//log.Debugw("getDevice - returned from cache", log.Fields{"deviceId": id})
		return d, nil
	}
	dg.cachedDevicesLock.RUnlock()
	//	Not cached
	if d, err := dg.getDeviceFromModel(id); err != nil {
		log.Errorw("device-not-found", log.Fields{"deviceId": id, "error": err})
		return nil, err
	} else { // cache it
		dg.cachedDevicesLock.Lock()
		dg.cachedDevices[id] = d
		dg.cachedDevicesLock.Unlock()
		//log.Debugw("getDevice - returned from model", log.Fields{"deviceId": id})
		return d, nil
	}
}

// addDevice adds a device to a device graph and setup edges that represent the device connections to its peers
func (dg *DeviceGraph) addDevice(device *voltha.Device, g goraph.Graph, devicesAdded *map[string]string, portsAdded *map[string]string,
	boundaryPorts map[string]uint32) goraph.Graph {

	if device == nil {
		return g
	}

	if _, exist := (*devicesAdded)[device.Id]; !exist {
		g.AddNode(goraph.NewNode(device.Id))
		(*devicesAdded)[device.Id] = device.Id
	}

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

//portExist returns true if the port ID is already part of the boundary ports map.
func (dg *DeviceGraph) portExist(id string) bool {
	dg.boundaryPortsLock.RLock()
	defer dg.boundaryPortsLock.RUnlock()
	_, exist := dg.boundaryPorts[id]
	return exist
}

// buildPathsToAllRootPorts builds all the paths from the non-root logical port to all root ports
// on the logical device
func (dg *DeviceGraph) buildPathsToAllRootPorts(lp *voltha.LogicalPort) map[OFPortLink][]RouteHop {
	paths := dg.Routes
	source := concatDeviceIdPortId(lp.DeviceId, lp.DevicePortNo)
	sourcePort := lp.OfpPort.PortNo
	ch := make(chan *ofPortLinkToPath)
	numBuildRequest := 0
	for target, targetPort := range dg.rootPortsString {
		go dg.buildRoute(source, target, sourcePort, targetPort, ch)
		numBuildRequest += 1
	}
	responseReceived := 0
forloop:
	for {
		if responseReceived == numBuildRequest {
			break
		}
		select {
		case res, ok := <-ch:
			if !ok {
				log.Debug("channel closed")
				break forloop
			}
			if res != nil && len(res.path) > 0 {
				paths[res.link] = res.path
				paths[OFPortLink{Ingress: res.link.Egress, Egress: res.link.Ingress}] = getReverseRoute(res.path)
			}
		}
		responseReceived += 1
	}
	return paths
}

// buildPathsToAllNonRootPorts builds all the paths from the root logical port to all non-root ports
// on the logical device
func (dg *DeviceGraph) buildPathsToAllNonRootPorts(lp *voltha.LogicalPort) map[OFPortLink][]RouteHop {
	paths := dg.Routes
	source := concatDeviceIdPortId(lp.DeviceId, lp.DevicePortNo)
	sourcePort := lp.OfpPort.PortNo
	ch := make(chan *ofPortLinkToPath)
	numBuildRequest := 0
	for target, targetPort := range dg.nonRootPortsString {
		go dg.buildRoute(source, target, sourcePort, targetPort, ch)
		numBuildRequest += 1
	}
	responseReceived := 0
forloop:
	for {
		if responseReceived == numBuildRequest {
			break
		}
		select {
		case res, ok := <-ch:
			if !ok {
				log.Debug("channel closed")
				break forloop
			}
			if res != nil && len(res.path) > 0 {
				paths[res.link] = res.path
				paths[OFPortLink{Ingress: res.link.Egress, Egress: res.link.Ingress}] = getReverseRoute(res.path)
			}
		}
		responseReceived += 1
	}
	return paths
}

//buildRoute builds a route between a source and a target logical port
func (dg *DeviceGraph) buildRoute(sourceId, targetId string, sourcePort, targetPort uint32, ch chan *ofPortLinkToPath) {
	var pathIds []goraph.ID
	path := make([]RouteHop, 0)
	var err error
	var hop RouteHop
	var result *ofPortLinkToPath

	if sourceId == targetId {
		ch <- result
		return
	}
	//Ignore Root - Root Routes
	if dg.IsRootPort(sourcePort) && dg.IsRootPort(targetPort) {
		ch <- result
		return
	}

	//Ignore non-Root - non-Root Routes
	if !dg.IsRootPort(sourcePort) && !dg.IsRootPort(targetPort) {
		ch <- result
		return
	}

	if pathIds, _, err = goraph.Dijkstra(dg.GGraph, goraph.StringID(sourceId), goraph.StringID(targetId)); err != nil {
		log.Errorw("no-path", log.Fields{"sourceId": sourceId, "targetId": targetId, "error": err})
		ch <- result
		return
	}
	if len(pathIds)%3 != 0 {
		ch <- result
		return
	}
	var deviceId string
	var ingressPort uint32
	var egressPort uint32
	for i := 0; i < len(pathIds); i = i + 3 {
		if deviceId, ingressPort, err = splitIntoDeviceIdPortId(pathIds[i].String()); err != nil {
			log.Errorw("id-error", log.Fields{"sourceId": sourceId, "targetId": targetId, "error": err})
			break
		}
		if _, egressPort, err = splitIntoDeviceIdPortId(pathIds[i+2].String()); err != nil {
			log.Errorw("id-error", log.Fields{"sourceId": sourceId, "targetId": targetId, "error": err})
			break
		}
		hop = RouteHop{Ingress: ingressPort, DeviceID: deviceId, Egress: egressPort}
		path = append(path, hop)
	}
	result = &ofPortLinkToPath{link: OFPortLink{Ingress: sourcePort, Egress: targetPort}, path: path}
	ch <- result
}

//buildRoutes build all routes between all the ports on the logical device
func (dg *DeviceGraph) buildRoutes() map[OFPortLink][]RouteHop {
	paths := make(map[OFPortLink][]RouteHop)
	ch := make(chan *ofPortLinkToPath)
	numBuildRequest := 0
	for source, sourcePort := range dg.boundaryPorts {
		for target, targetPort := range dg.boundaryPorts {
			go dg.buildRoute(source, target, sourcePort, targetPort, ch)
			numBuildRequest += 1
		}
	}
	responseReceived := 0
forloop:
	for {
		if responseReceived == numBuildRequest {
			break
		}
		select {
		case res, ok := <-ch:
			if !ok {
				log.Debug("channel closed")
				break forloop
			}
			if res != nil && len(res.path) > 0 {
				paths[res.link] = res.path
			}
		}
		responseReceived += 1
	}
	return paths
}

// reset cleans up the device graph
func (dg *DeviceGraph) reset() {
	dg.devicesAdded = make(map[string]string)
	dg.portsAdded = make(map[string]string)
	dg.rootPortsString = make(map[string]uint32)
	dg.nonRootPortsString = make(map[string]uint32)
	dg.RootPorts = make(map[uint32]uint32)
	dg.boundaryPorts = make(map[string]uint32)
	dg.Routes = make(map[OFPortLink][]RouteHop)
	dg.cachedDevices = make(map[string]*voltha.Device)
}

//concatDeviceIdPortId formats a portid using the device id and the port number
func concatDeviceIdPortId(deviceId string, portNo uint32) string {
	return fmt.Sprintf("%s:%d", deviceId, portNo)
}

// splitIntoDeviceIdPortId extracts the device id and port number from the portId
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

//getReverseRoute returns the reverse of the route in param
func getReverseRoute(route []RouteHop) []RouteHop {
	reverse := make([]RouteHop, len(route))
	for i, j := 0, len(route)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = route[j], route[i]
	}
	return reverse
}
