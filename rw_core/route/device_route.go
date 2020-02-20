/*
 * Copyright 2020-present Open Networking Foundation
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
	"fmt"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

func init() {
	_, err := log.AddPackage(log.JSON, log.WarnLevel, nil)
	if err != nil {
		log.Fatalw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

// Hop represent a route hop
type Hop struct {
	DeviceID string
	Ingress  uint32
	Egress   uint32
}

// PathID is the identification of a route between two logical ports
type PathID struct {
	Ingress uint32
	Egress  uint32
}

type OFPortLink struct {
	Ingress uint32
	Egress  uint32
}

// GetDeviceFunc returns device function
type GetDeviceFunc func(ctx context.Context, id string) (*voltha.Device, error)

// DeviceRoutes represent the set of routes between logical ports of a logical device
type DeviceRoutes struct {
	logicalDeviceID     string
	getDeviceFromModel  GetDeviceFunc
	logicalPorts        []*voltha.LogicalPort
	RootPorts           map[uint32]uint32
	rootPortsLock       sync.RWMutex
	Routes              map[PathID][]Hop
	routeBuildLock      sync.RWMutex
	devicesPonPorts     map[string][]*voltha.Port
	devicesPonPortsLock sync.RWMutex
}

// NewDeviceRoutes creates device graph instance
func NewDeviceRoutes(logicalDeviceID string, getDevice GetDeviceFunc) *DeviceRoutes {
	var dr DeviceRoutes
	dr.logicalDeviceID = logicalDeviceID
	dr.getDeviceFromModel = getDevice
	dr.RootPorts = make(map[uint32]uint32)
	dr.Routes = make(map[PathID][]Hop)
	dr.devicesPonPorts = make(map[string][]*voltha.Port)
	log.Debug("new device routes created ...")
	return &dr
}

//IsRootPort returns true if the port is a root port on a logical device
func (dr *DeviceRoutes) IsRootPort(port uint32) bool {
	dr.rootPortsLock.RLock()
	defer dr.rootPortsLock.RUnlock()
	_, exist := dr.RootPorts[port]
	return exist
}

//ComputeRoutes calculates all the routes between the logical ports.  This will clear up any existing route
func (dr *DeviceRoutes) ComputeRoutes(ctx context.Context, lps []*voltha.LogicalPort) error {
	dr.routeBuildLock.Lock()
	defer dr.routeBuildLock.Unlock()

	log.Debugw("computing-all-routes", log.Fields{"len-logical-ports": len(lps)})
	var err error
	defer func() {
		// On error, clear the routes - any flow request or a port add/delete will trigger the rebuild
		if err != nil {
			dr.reset()
		}
	}()

	if len(lps) < 2 {
		return status.Error(codes.FailedPrecondition, "not-enough-logical-ports")
	}

	dr.reset()
	dr.logicalPorts = append(dr.logicalPorts, lps...)

	// Setup the physical ports to logical ports map, the nni ports as well as the root ports map
	physPortToLogicalPortMap := make(map[string]uint32)
	nniPorts := make([]*voltha.LogicalPort, 0)
	for _, lp := range lps {
		physPortToLogicalPortMap[concatDeviceIDPortID(lp.DeviceId, lp.DevicePortNo)] = lp.OfpPort.PortNo
		if lp.RootPort {
			nniPorts = append(nniPorts, lp)
			dr.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
		}
	}
	if len(nniPorts) == 0 {
		err = status.Error(codes.FailedPrecondition, "no nni port")
		return err
	}
	var rootDevice *voltha.Device
	var childDevice *voltha.Device
	var copyFromNNIPort *voltha.LogicalPort
	for idx, nniPort := range nniPorts {
		if idx == 0 {
			copyFromNNIPort = nniPort
		} else if len(dr.Routes) > 0 {
			dr.copyFromExistingNNIRoutes(nniPort, copyFromNNIPort)
			return nil
		}
		// Get root device
		rootDevice, err = dr.getDevice(ctx, nniPort.DeviceId)
		if err != nil {
			return err
		}
		if len(rootDevice.Ports) == 0 {
			err = status.Errorf(codes.FailedPrecondition, "no-port-%s", rootDevice.Id)
			return err
		}
		for _, rootDevicePort := range rootDevice.Ports {
			if rootDevicePort.Type == voltha.Port_PON_OLT {
				log.Debugw("peers", log.Fields{"root-device-id": rootDevice.Id, "port-no": rootDevicePort.PortNo, "len-peers": len(rootDevicePort.Peers)})
				for _, rootDevicePeer := range rootDevicePort.Peers {
					childDevice, err = dr.getDevice(ctx, rootDevicePeer.DeviceId)
					if err != nil {
						return err
					}
					childPonPorts := dr.getDevicePonPorts(childDevice.Id, nniPort.DeviceId)
					if len(childPonPorts) < 1 {
						err = status.Errorf(codes.Aborted, "no-child-pon-port-%s", childDevice.Id)
						return err
					}
					// We use the first PON port on the ONU whose parent is the root device.
					childPonPort := childPonPorts[0].PortNo
					for _, childDevicePort := range childDevice.Ports {
						if childDevicePort.Type == voltha.Port_ETHERNET_UNI {
							childLogicalPort, exist := physPortToLogicalPortMap[concatDeviceIDPortID(childDevice.Id, childDevicePort.PortNo)]
							if !exist {
								// This can happen if this logical port has not been created yet for that device
								continue
							}
							dr.Routes[PathID{Ingress: nniPort.OfpPort.PortNo, Egress: childLogicalPort}] = []Hop{
								{DeviceID: rootDevice.Id, Ingress: nniPort.DevicePortNo, Egress: rootDevicePort.PortNo},
								{DeviceID: childDevice.Id, Ingress: childPonPort, Egress: childDevicePort.PortNo},
							}
							dr.Routes[PathID{Ingress: childLogicalPort, Egress: nniPort.OfpPort.PortNo}] = getReverseRoute(
								dr.Routes[PathID{Ingress: nniPort.OfpPort.PortNo, Egress: childLogicalPort}])
						}
					}
				}
			}
		}
	}
	return nil
}

// verifyPrecondition verify whether the preconditions are met to proceed with addition of the new logical port
func (dr *DeviceRoutes) addPortAndVerifyPrecondition(lp *voltha.LogicalPort) error {
	var exist, nniLogicalPortExist, uniLogicalPortExist bool
	for _, existingLogicalPort := range dr.logicalPorts {
		nniLogicalPortExist = nniLogicalPortExist || existingLogicalPort.RootPort
		uniLogicalPortExist = uniLogicalPortExist || !existingLogicalPort.RootPort
		exist = exist || existingLogicalPort.OfpPort.PortNo == lp.OfpPort.PortNo
		if nniLogicalPortExist && uniLogicalPortExist && exist {
			break
		}
	}
	if !exist {
		dr.logicalPorts = append(dr.logicalPorts, lp)
		nniLogicalPortExist = nniLogicalPortExist || lp.RootPort
		uniLogicalPortExist = uniLogicalPortExist || !lp.RootPort
	}

	// If we do not have both NNI and UNI ports then return an error
	if !(nniLogicalPortExist && uniLogicalPortExist) {
		fmt.Println("errors", nniLogicalPortExist, uniLogicalPortExist)
		return status.Error(codes.FailedPrecondition, "no-uni-and-nni-ports-combination")
	}
	return nil
}

// AddPort augments the current set of routes with new routes corresponding to the logical port "lp".  If the routes have
// not been built yet then use logical port "lps" to compute all current routes (lps includes lp)
func (dr *DeviceRoutes) AddPort(ctx context.Context, lp *voltha.LogicalPort, lps []*voltha.LogicalPort) error {
	log.Debugw("add-port-to-routes", log.Fields{"port": lp, "len-logical-ports": len(lps)})

	dr.routeBuildLock.Lock()
	if len(dr.Routes) == 0 {
		dr.routeBuildLock.Unlock()
		return dr.ComputeRoutes(ctx, lps)
	}

	// A set of routes exists
	if err := dr.addPortAndVerifyPrecondition(lp); err != nil {
		dr.reset()
		dr.routeBuildLock.Unlock()
		return err
	}

	defer dr.routeBuildLock.Unlock()
	// Update the set of root ports, if applicable
	if lp.RootPort {
		dr.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
	}

	var copyFromNNIPort *voltha.LogicalPort
	// Setup the physical ports to logical ports map
	nniPorts := make([]*voltha.LogicalPort, 0)
	for _, lport := range dr.logicalPorts {
		if lport.RootPort {
			nniPorts = append(nniPorts, lport)
			if copyFromNNIPort == nil && lport.OfpPort.PortNo != lp.OfpPort.PortNo {
				copyFromNNIPort = lport
			}
		}
	}

	if copyFromNNIPort == nil {
		// Trying to add the same NNI port.  Just return
		return nil
	}

	// Adding NNI Port?   If we are here we already have an NNI port with a set of routes.  Just copy the existing
	// routes using an existing NNI port
	if lp.RootPort {
		dr.copyFromExistingNNIRoutes(lp, copyFromNNIPort)
		return nil
	}

	// Adding a UNI port
	for _, nniPort := range nniPorts {
		childPonPorts := dr.getDevicePonPorts(lp.DeviceId, nniPort.DeviceId)
		if len(childPonPorts) == 0 || len(childPonPorts[0].Peers) == 0 {
			// Ports may not have been cached yet - get the device info which sets the PON port cache
			if _, err := dr.getDevice(ctx, lp.DeviceId); err != nil {
				dr.reset()
				return err
			}
			childPonPorts = dr.getDevicePonPorts(lp.DeviceId, nniPort.DeviceId)
			if len(childPonPorts) == 0 || len(childPonPorts[0].Peers) == 0 {
				dr.reset()
				return status.Errorf(codes.FailedPrecondition, "no-pon-ports-%s", lp.DeviceId)
			}
		}
		// We use the first PON port on the child device
		childPonPort := childPonPorts[0]
		dr.Routes[PathID{Ingress: nniPort.OfpPort.PortNo, Egress: lp.OfpPort.PortNo}] = []Hop{
			{DeviceID: nniPort.DeviceId, Ingress: nniPort.DevicePortNo, Egress: childPonPort.Peers[0].PortNo},
			{DeviceID: lp.DeviceId, Ingress: childPonPort.PortNo, Egress: lp.DevicePortNo},
		}
		dr.Routes[PathID{Ingress: lp.OfpPort.PortNo, Egress: nniPort.OfpPort.PortNo}] = getReverseRoute(
			dr.Routes[PathID{Ingress: nniPort.OfpPort.PortNo, Egress: lp.OfpPort.PortNo}])
	}
	return nil
}

// Print prints routes
func (dr *DeviceRoutes) Print() error {
	log.Debugw("Print", log.Fields{"logical-device-id": dr.logicalDeviceID, "logical-ports": dr.logicalPorts})
	if log.V(log.DebugLevel) {
		output := ""
		routeNumber := 1
		for k, v := range dr.Routes {
			key := fmt.Sprintf("LP:%d->LP:%d", k.Ingress, k.Egress)
			val := ""
			for _, i := range v {
				val += fmt.Sprintf("{%d->%s->%d},", i.Ingress, i.DeviceID, i.Egress)
			}
			val = val[:len(val)-1]
			output += fmt.Sprintf("%d:{%s=>%s}   ", routeNumber, key, fmt.Sprintf("[%s]", val))
			routeNumber++
		}
		if len(dr.Routes) == 0 {
			log.Debugw("no-routes-found", log.Fields{"logical-device-id": dr.logicalDeviceID})
		} else {
			log.Debugw("graph_routes", log.Fields{"lDeviceId": dr.logicalDeviceID, "Routes": output})
		}
	}
	return nil
}

// IsUpToDate returns true if device is up to date
func (dr *DeviceRoutes) IsUpToDate(ld *voltha.LogicalDevice) bool {
	dr.routeBuildLock.Lock()
	defer dr.routeBuildLock.Unlock()
	numNNI, numUNI := 0, 0
	if ld != nil {
		if len(dr.logicalPorts) != len(ld.Ports) {
			return false
		}
		numNNI = len(dr.RootPorts)
		numUNI = len(ld.Ports) - numNNI
	}
	return len(dr.Routes) == numNNI*numUNI*2
}

// getDevicePonPorts returns all the PON ports of a device whose peer device ID is peerDeviceID
func (dr *DeviceRoutes) getDevicePonPorts(deviceID string, peerDeviceID string) []*voltha.Port {
	dr.devicesPonPortsLock.RLock()
	defer dr.devicesPonPortsLock.RUnlock()
	ponPorts := make([]*voltha.Port, 0)
	ports, exist := dr.devicesPonPorts[deviceID]
	if !exist {
		return ponPorts
	}
	//fmt.Println("getDevicePonPorts", deviceID, peerDeviceID, ports)
	for _, port := range ports {
		for _, peer := range port.Peers {
			if peer.DeviceId == peerDeviceID {
				ponPorts = append(ponPorts, port)
			}
		}
	}
	return ponPorts
}

//getDevice returns the from the model and updates the PON ports map of that device.
func (dr *DeviceRoutes) getDevice(ctx context.Context, deviceID string) (*voltha.Device, error) {
	device, err := dr.getDeviceFromModel(ctx, deviceID)
	if err != nil {
		log.Errorw("device-not-found", log.Fields{"deviceId": deviceID, "error": err})
		return nil, err
	}
	dr.devicesPonPortsLock.Lock()
	defer dr.devicesPonPortsLock.Unlock()
	for _, port := range device.Ports {
		if port.Type == voltha.Port_PON_ONU || port.Type == voltha.Port_PON_OLT {
			dr.devicesPonPorts[device.Id] = append(dr.devicesPonPorts[device.Id], port)
		}
	}
	return device, nil
}

//copyFromExistingNNIRoutes copies routes from an existing set of NNI routes
func (dr *DeviceRoutes) copyFromExistingNNIRoutes(newNNIPort *voltha.LogicalPort, copyFromNNIPort *voltha.LogicalPort) {
	updatedRoutes := make(map[PathID][]Hop)
	for key, val := range dr.Routes {
		if key.Ingress == copyFromNNIPort.OfpPort.PortNo {
			updatedRoutes[PathID{Ingress: newNNIPort.OfpPort.PortNo, Egress: key.Egress}] = []Hop{
				{DeviceID: newNNIPort.DeviceId, Ingress: newNNIPort.DevicePortNo, Egress: val[0].Egress},
				val[1],
			}
		}
		if key.Egress == copyFromNNIPort.OfpPort.PortNo {
			updatedRoutes[PathID{Ingress: key.Ingress, Egress: newNNIPort.OfpPort.PortNo}] = []Hop{
				val[0],
				{DeviceID: newNNIPort.DeviceId, Ingress: val[1].Ingress, Egress: newNNIPort.DevicePortNo},
			}
		}
		updatedRoutes[key] = val
	}
	dr.Routes = updatedRoutes
}

// reset cleans up the device graph
func (dr *DeviceRoutes) reset() {
	dr.rootPortsLock.Lock()
	dr.RootPorts = make(map[uint32]uint32)
	dr.rootPortsLock.Unlock()
	// Do not numGetDeviceCalledLock Routes, logicalPorts  as the callee function already holds its numGetDeviceCalledLock.
	dr.Routes = make(map[PathID][]Hop)
	dr.logicalPorts = make([]*voltha.LogicalPort, 0)
	dr.devicesPonPortsLock.Lock()
	dr.devicesPonPorts = make(map[string][]*voltha.Port)
	dr.devicesPonPortsLock.Unlock()
}

//concatDeviceIdPortId formats a portid using the device id and the port number
func concatDeviceIDPortID(deviceID string, portNo uint32) string {
	return fmt.Sprintf("%s:%d", deviceID, portNo)
}

//getReverseRoute returns the reverse of the route
func getReverseRoute(route []Hop) []Hop {
	reverse := make([]Hop, len(route))
	for i, j := 0, len(route)-1; j >= 0; i, j = i+1, j-1 {
		reverse[i].DeviceID, reverse[i].Ingress, reverse[i].Egress = route[j].DeviceID, route[j].Egress, route[j].Ingress
	}
	return reverse
}
