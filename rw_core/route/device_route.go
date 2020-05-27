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
	"errors"
	"fmt"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

var ErrNoRoute = errors.New("no route")

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
	logicalDeviceID           string
	getDeviceFromModel        GetDeviceFunc
	logicalPorts              map[uint32]*voltha.LogicalPort
	RootPorts                 map[uint32]uint32
	rootPortsLock             sync.RWMutex
	Routes                    map[PathID][]Hop
	routeBuildLock            sync.RWMutex
	devicesPonPorts           map[string][]*voltha.Port
	childPonPortNoMapOnParent map[string]uint32
}

// NewDeviceRoutes creates device graph instance
func NewDeviceRoutes(logicalDeviceID string, getDevice GetDeviceFunc) *DeviceRoutes {
	var dr DeviceRoutes
	dr.logicalDeviceID = logicalDeviceID
	dr.getDeviceFromModel = getDevice
	dr.RootPorts = make(map[uint32]uint32)
	dr.Routes = make(map[PathID][]Hop)
	dr.devicesPonPorts = make(map[string][]*voltha.Port)
	dr.childPonPortNoMapOnParent = make(map[string]uint32)
	dr.logicalPorts = make(map[uint32]*voltha.LogicalPort)
	logger.Debug("new device routes created ...")
	return &dr
}

//IsRootPort returns true if the port is a root port on a logical device
func (dr *DeviceRoutes) IsRootPort(port uint32) bool {
	dr.rootPortsLock.RLock()
	defer dr.rootPortsLock.RUnlock()
	_, exist := dr.RootPorts[port]
	return exist
}

func (dr *DeviceRoutes) GetRoute(ctx context.Context, ingress, egress uint32) ([]Hop, error) {
	dr.routeBuildLock.Lock()
	defer dr.routeBuildLock.Unlock()

	if route, exist := dr.Routes[PathID{Ingress: ingress, Egress: egress}]; exist {
		return route, nil
	}

	uniPort, nniPort, err := dr.getLogicalPorts(ingress, egress)
	if err != nil {
		//fmt.Println("NO UNI or NNI port", err, len(dr.logicalPorts))
		return nil, fmt.Errorf("no route from:%d to:%d %w", ingress, egress, err)
	}

	childPonPort, err := dr.getChildPonPort(ctx, uniPort.DeviceId)
	if err != nil {
		return nil, err
	}
	rootDevicePonPort, err := dr.getParentPonPort(ctx, nniPort.DeviceId, uniPort.DeviceId)
	if err != nil {
		return nil, err
	}

	dr.Routes[PathID{Ingress: nniPort.OfpPort.PortNo, Egress: uniPort.DevicePortNo}] = []Hop{
		{DeviceID: nniPort.DeviceId, Ingress: nniPort.DevicePortNo, Egress: rootDevicePonPort},
		{DeviceID: uniPort.DeviceId, Ingress: childPonPort, Egress: uniPort.DevicePortNo},
	}
	dr.Routes[PathID{Ingress: uniPort.DevicePortNo, Egress: nniPort.OfpPort.PortNo}] = getReverseRoute(
		dr.Routes[PathID{Ingress: nniPort.OfpPort.PortNo, Egress: uniPort.DevicePortNo}])

	return dr.Routes[PathID{Ingress: ingress, Egress: egress}], nil

}

//ComputeRoutes calculates all the routes between the logical ports.  This will clear up any existing route
func (dr *DeviceRoutes) ComputeRoutes(ctx context.Context, lps []*voltha.LogicalPort) error {
	dr.routeBuildLock.Lock()
	defer dr.routeBuildLock.Unlock()

	logger.Debugw("computing-all-routes", log.Fields{"len-logical-ports": len(lps)})
	var err error
	defer func() {
		// On error, clear the routes - any flow request or a port add/delete will trigger the rebuild
		if err != nil {
			dr.reset()
		}
	}()

	if len(lps) < 2 {
		return fmt.Errorf("not enough logical port :%w", ErrNoRoute)
	}

	dr.reset()

	// Setup the physical ports to logical ports map, the nni ports as well as the root ports map
	physPortToLogicalPortMap := make(map[string]uint32)
	nniPorts := make([]*voltha.LogicalPort, 0)
	for _, lp := range lps {
		physPortToLogicalPortMap[concatDeviceIDPortID(lp.DeviceId, lp.DevicePortNo)] = lp.OfpPort.PortNo
		if lp.RootPort {
			nniPorts = append(nniPorts, lp)
			dr.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
		}
		dr.logicalPorts[lp.OfpPort.PortNo] = lp
	}

	if len(nniPorts) == 0 {
		return fmt.Errorf("no nni port :%w", ErrNoRoute)
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
		rootDevice, err = dr.getDeviceWithCacheUpdate(ctx, nniPort.DeviceId)
		if err != nil {
			return err
		}
		if len(rootDevice.Ports) == 0 {
			err = status.Errorf(codes.FailedPrecondition, "no-port-%s", rootDevice.Id)
			return err
		}
		for _, rootDevicePort := range rootDevice.Ports {
			if rootDevicePort.Type == voltha.Port_PON_OLT {
				logger.Debugw("peers", log.Fields{"root-device-id": rootDevice.Id, "port-no": rootDevicePort.PortNo, "len-peers": len(rootDevicePort.Peers)})
				for _, rootDevicePeer := range rootDevicePort.Peers {
					childDevice, err = dr.getDeviceWithCacheUpdate(ctx, rootDevicePeer.DeviceId)
					if err != nil {
						return err
					}
					childPonPort, err := dr.getChildPonPort(ctx, childDevice.Id)
					if err != nil {
						return err
					}
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

// AddPort augments the current set of routes with new routes corresponding to the logical port "lp".  If the routes have
// not been built yet then use logical port "lps" to compute all current routes (lps includes lp)
func (dr *DeviceRoutes) AddPort(ctx context.Context, lp *voltha.LogicalPort, device *voltha.Device, lps []*voltha.LogicalPort) error {
	logger.Debugw("add-port-to-routes", log.Fields{"port": lp, "count-logical-ports": len(lps)})

	// Adding NNI port
	if lp.RootPort {
		return dr.AddNNIPort(ctx, lp, device, lps)
	}

	// Adding UNI port
	return dr.AddUNIPort(ctx, lp, device, lps)
}

func (dr *DeviceRoutes) AddUNIPort(ctx context.Context, lp *voltha.LogicalPort, device *voltha.Device, lps []*voltha.LogicalPort) error {
	logger.Debugw("add-uni-port-to-routes", log.Fields{"port": lp, "count-logical-ports": len(lps)})

	dr.routeBuildLock.Lock()
	defer dr.routeBuildLock.Unlock()

	// Add port to logical ports
	dr.logicalPorts[lp.OfpPort.PortNo] = lp

	// Update internal structures with device data
	dr.updateCache(device)

	// Adding a UNI port
	childPonPort, err := dr.getChildPonPort(ctx, lp.DeviceId)
	if err != nil {
		return err
	}
	rootDevicePonPort, err := dr.getParentPonPort(ctx, device.ParentId, device.Id)
	if err != nil {
		return err
	}

	// Adding a UNI port
	for _, lPort := range lps {
		if lPort.RootPort {
			dr.Routes[PathID{Ingress: lPort.OfpPort.PortNo, Egress: lp.OfpPort.PortNo}] = []Hop{
				{DeviceID: lPort.DeviceId, Ingress: lPort.DevicePortNo, Egress: rootDevicePonPort},
				{DeviceID: lp.DeviceId, Ingress: childPonPort, Egress: lp.DevicePortNo},
			}
			dr.Routes[PathID{Ingress: lp.OfpPort.PortNo, Egress: lPort.OfpPort.PortNo}] = getReverseRoute(
				dr.Routes[PathID{Ingress: lPort.OfpPort.PortNo, Egress: lp.OfpPort.PortNo}])
		}
	}
	return nil
}

func (dr *DeviceRoutes) AddNNIPort(ctx context.Context, lp *voltha.LogicalPort, device *voltha.Device, lps []*voltha.LogicalPort) error {
	logger.Debugw("add-port-to-routes", log.Fields{"port": lp, "logical-ports-count": len(lps), "device-id": device.Id})

	dr.routeBuildLock.Lock()
	defer dr.routeBuildLock.Unlock()

	// Update internal structures with device data
	dr.updateCache(device)

	// Setup the physical ports to logical ports map, the nni ports as well as the root ports map
	physPortToLogicalPortMap := make(map[string]uint32)
	for _, lp := range lps {
		physPortToLogicalPortMap[concatDeviceIDPortID(lp.DeviceId, lp.DevicePortNo)] = lp.OfpPort.PortNo
		if lp.RootPort {
			dr.rootPortsLock.Lock()
			dr.RootPorts[lp.OfpPort.PortNo] = lp.OfpPort.PortNo
			dr.rootPortsLock.Unlock()
		}
		dr.logicalPorts[lp.OfpPort.PortNo] = lp
	}

	for _, rootDevicePort := range device.Ports {
		if rootDevicePort.Type == voltha.Port_PON_OLT {
			logger.Debugw("peers", log.Fields{"root-device-id": device.Id, "port-no": rootDevicePort.PortNo, "len-peers": len(rootDevicePort.Peers)})
			for _, rootDevicePeer := range rootDevicePort.Peers {
				childDevice, err := dr.getDeviceWithCacheUpdate(ctx, rootDevicePeer.DeviceId)
				if err != nil {
					continue
				}

				childPonPort, err := dr.getChildPonPort(ctx, childDevice.Id)
				if err != nil {
					continue
				}

				for _, childDevicePort := range childDevice.Ports {
					childLogicalPort, exist := physPortToLogicalPortMap[concatDeviceIDPortID(childDevice.Id, childDevicePort.PortNo)]
					if !exist {
						// This can happen if this logical port has not been created yet for that device
						continue
					}

					if childDevicePort.Type == voltha.Port_ETHERNET_UNI {
						dr.Routes[PathID{Ingress: lp.OfpPort.PortNo, Egress: childLogicalPort}] = []Hop{
							{DeviceID: device.Id, Ingress: lp.DevicePortNo, Egress: rootDevicePort.PortNo},
							{DeviceID: childDevice.Id, Ingress: childPonPort, Egress: childDevicePort.PortNo},
						}
						dr.Routes[PathID{Ingress: childLogicalPort, Egress: lp.OfpPort.PortNo}] = getReverseRoute(
							dr.Routes[PathID{Ingress: lp.OfpPort.PortNo, Egress: childLogicalPort}])
					}
				}
			}
		}
	}
	return nil
}

// AddAllPorts setups up new routes using all ports on the device. lps includes the device's logical port
func (dr *DeviceRoutes) AddAllPorts(ctx context.Context, device *voltha.Device, lps []*voltha.LogicalPort) error {
	logger.Debugw("add-all-port-to-routes", log.Fields{"logical-ports-count": len(lps), "device-id": device.Id})
	for _, lp := range lps {
		if lp.DeviceId == device.Id {
			if err := dr.AddPort(ctx, lp, device, lps); err != nil {
				return err
			}
		}
	}
	return nil
}

// Print prints routes
func (dr *DeviceRoutes) Print() error {
	logger.Debugw("Print", log.Fields{"logical-device-id": dr.logicalDeviceID, "logical-ports": dr.logicalPorts})
	if logger.V(log.DebugLevel) {
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
			logger.Debugw("no-routes-found", log.Fields{"logical-device-id": dr.logicalDeviceID})
		} else {
			logger.Debugw("graph_routes", log.Fields{"lDeviceId": dr.logicalDeviceID, "Routes": output})
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

//getDeviceWithCacheUpdate returns the from the model and updates the PON ports map of that device.
func (dr *DeviceRoutes) getDeviceWithCacheUpdate(ctx context.Context, deviceID string) (*voltha.Device, error) {
	device, err := dr.getDeviceFromModel(ctx, deviceID)
	if err != nil {
		logger.Errorw("device-not-found", log.Fields{"deviceId": deviceID, "error": err})
		return nil, err
	}
	dr.updateCache(device)
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
	dr.Routes = make(map[PathID][]Hop)
	dr.logicalPorts = make(map[uint32]*voltha.LogicalPort)
	dr.devicesPonPorts = make(map[string][]*voltha.Port)
	dr.childPonPortNoMapOnParent = make(map[string]uint32)
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

//getChildPonPort returns the from the model and updates the PON ports map of that device.
func (dr *DeviceRoutes) getChildPonPort(ctx context.Context, deviceID string) (uint32, error) {
	if pNo, err := dr.getChildPonPortFromCache(ctx, deviceID); err == nil {
		return pNo, nil
	}
	// Get child device from model
	if _, err := dr.getDeviceWithCacheUpdate(ctx, deviceID); err != nil {
		logger.Errorw("device-not-found", log.Fields{"device-id": deviceID, "error": err})
		return 0, err
	}

	// Try again
	return dr.getChildPonPortFromCache(ctx, deviceID)
}

//getChildPonPortFromCache returns the PON port of the child device from cache
func (dr *DeviceRoutes) getChildPonPortFromCache(ctx context.Context, deviceID string) (uint32, error) {
	if port, exist := dr.devicesPonPorts[deviceID]; exist {
		// Return only the first PON port of that child device
		return port[0].PortNo, nil
	}
	return 0, fmt.Errorf("pon port not found %s", deviceID)
}

// getParentPonPort returns the parent PON port of the child device
func (dr *DeviceRoutes) getParentPonPort(ctx context.Context, deviceID string, childDeviceID string) (uint32, error) {
	if pNo, err := dr.getParentPonPortFromCache(ctx, childDeviceID); err == nil {
		return pNo, nil
	}
	// Get parent device from the model
	if _, err := dr.getDeviceWithCacheUpdate(ctx, deviceID); err != nil {
		logger.Errorw("device-not-found", log.Fields{"deviceId": deviceID, "error": err})
		return 0, err
	}
	// Try again
	return dr.getParentPonPortFromCache(ctx, childDeviceID)
}

// getParentPonPortFromCache returns the parent PON port of the child device from cache
func (dr *DeviceRoutes) getParentPonPortFromCache(ctx context.Context, childDeviceID string) (uint32, error) {
	if pNo, exist := dr.childPonPortNoMapOnParent[childDeviceID]; exist {
		return pNo, nil
	}
	return 0, fmt.Errorf("pon port associated with child device %s not found", childDeviceID)
}

func (dr *DeviceRoutes) updateCache(device *voltha.Device) {
	for _, port := range device.Ports {
		if port.Type == voltha.Port_PON_ONU || port.Type == voltha.Port_PON_OLT {
			dr.devicesPonPorts[device.Id] = append(dr.devicesPonPorts[device.Id], port)
			for _, peer := range port.Peers {
				if port.Type == voltha.Port_PON_ONU {
					dr.childPonPortNoMapOnParent[port.DeviceId] = peer.PortNo
				} else {
					dr.childPonPortNoMapOnParent[peer.DeviceId] = port.PortNo
				}
			}
		}
	}
}

func (dr *DeviceRoutes) getLogicalPorts(ingress, egress uint32) (uniPort, nniPort *voltha.LogicalPort, err error) {
	inPort, exist := dr.logicalPorts[ingress]
	if !exist {
		err = fmt.Errorf("ingress port %d not found", ingress)
		return
	}
	outPort, exist := dr.logicalPorts[egress]
	if !exist {
		err = fmt.Errorf("egress port %d not found", egress)
		return
	}

	if inPort.RootPort {
		nniPort = inPort
		uniPort = outPort
	} else {
		nniPort = outPort
		uniPort = inPort
	}

	return
}
