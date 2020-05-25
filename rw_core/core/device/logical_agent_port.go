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

package device

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listLogicalDevicePorts returns logical device ports
func (agent *LogicalAgent) listLogicalDevicePorts() map[uint32]*voltha.LogicalPort {
	logger.Debug("listLogicalDevicePorts")
	portIDs := agent.portLoader.List()
	ret := make(map[uint32]*voltha.LogicalPort, len(portIDs))
	for portID := range portIDs {
		if portHandle, have := agent.portLoader.Lock(portID); have {
			ret[portID] = portHandle.GetReadOnly()
			portHandle.Unlock()
		}
	}
	return ret
}

func (agent *LogicalAgent) updateLogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) error {
	logger.Debugw("updateLogicalPort", log.Fields{"deviceId": device.Id, "port": port})
	var err error
	switch port.Type {
	case voltha.Port_ETHERNET_NNI:
		if _, err = agent.addNNILogicalPort(ctx, device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, true)
	case voltha.Port_ETHERNET_UNI:
		if _, err = agent.addUNILogicalPort(ctx, device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, false)
	case voltha.Port_PON_OLT:
		// Rebuilt the routes on Parent PON port addition
		go func() {
			if err = agent.buildRoutes(ctx); err != nil {
				// Not an error - temporary state
				logger.Infow("failed-to-update-routes-after-adding-parent-pon-port", log.Fields{"device-id": device.Id, "port": port, "ports-count": len(device.Ports), "error": err})
			}
		}()
		//fallthrough
	case voltha.Port_PON_ONU:
		// Add the routes corresponding to that child device
		go func() {
			if err = agent.updateAllRoutes(ctx, device); err != nil {
				// Not an error - temporary state
				logger.Infow("failed-to-update-routes-after-adding-child-pon-port", log.Fields{"device-id": device.Id, "port": port, "ports-count": len(device.Ports), "error": err})
			}
		}()
	default:
		return fmt.Errorf("invalid port type %v", port)
	}
	return nil
}

// setupLogicalPorts is invoked once the logical device has been created and is ready to get ports
// added to it.  While the logical device was being created we could have received requests to add
// NNI and UNI ports which were discarded.  Now is the time to add them if needed
func (agent *LogicalAgent) setupLogicalPorts(ctx context.Context) error {
	logger.Infow("setupLogicalPorts", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// First add any NNI ports which could have been missing
	if err := agent.setupNNILogicalPorts(ctx, agent.rootDeviceID); err != nil {
		logger.Errorw("error-setting-up-NNI-ports", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}

	// Now, set up the UNI ports if needed.
	children, err := agent.deviceMgr.GetAllChildDevices(ctx, agent.rootDeviceID)
	if err != nil {
		logger.Errorw("error-getting-child-devices", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}
	responses := make([]coreutils.Response, 0)
	for _, child := range children.Items {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(child *voltha.Device) {
			if err = agent.setupUNILogicalPorts(context.Background(), child); err != nil {
				logger.Error("setting-up-UNI-ports-failed", log.Fields{"deviceID": child.Id})
				response.Error(status.Errorf(codes.Internal, "UNI-ports-setup-failed: %s", child.Id))
			}
			response.Done()
		}(child)
	}
	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, responses...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

// setupNNILogicalPorts creates an NNI port on the logical device that represents an NNI interface on a root device
func (agent *LogicalAgent) setupNNILogicalPorts(ctx context.Context, deviceID string) error {
	logger.Infow("setupNNILogicalPorts-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error

	var device *voltha.Device
	if device, err = agent.deviceMgr.getDevice(ctx, deviceID); err != nil {
		logger.Errorw("error-retrieving-device", log.Fields{"error": err, "deviceId": deviceID})
		return err
	}

	//Get UNI port number
	for _, port := range device.Ports {
		if port.Type == voltha.Port_ETHERNET_NNI {
			if _, err = agent.addNNILogicalPort(ctx, device, port); err != nil {
				logger.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			agent.addLogicalPortToMap(port.PortNo, true)
		}
	}
	return err
}

// updatePortState updates the port state of the device
func (agent *LogicalAgent) updatePortState(ctx context.Context, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Infow("updatePortState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "portNo": portNo, "state": operStatus})

	portHandle, have := agent.portLoader.Lock(portNo)
	if !have {
		return status.Errorf(codes.NotFound, "port-%d-not-exist", portNo)
	}
	defer portHandle.Unlock()

	newPort := clonePortSetState(portHandle.GetReadOnly(), operStatus)
	if err := portHandle.Update(ctx, newPort); err != nil {
		return err
	}
	go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)
	return nil
}

// updatePortsState updates the ports state related to the device
func (agent *LogicalAgent) updatePortsState(ctx context.Context, deviceID string, state voltha.OperStatus_Types) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})

	for portNo := range agent.portLoader.ListForDevice(deviceID) {
		if portHandle, have := agent.portLoader.Lock(portNo); have {
			newPort := clonePortSetState(portHandle.GetReadOnly(), state)
			if err := portHandle.Update(ctx, newPort); err != nil {
				portHandle.Unlock()
				return err
			}
			go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)

			portHandle.Unlock()
		}
	}
	return nil
}

func clonePortSetState(oldPort *voltha.LogicalPort, state voltha.OperStatus_Types) *voltha.LogicalPort {
	newPort := *oldPort // only clone the struct(s) that will be changed
	newOfpPort := *oldPort.OfpPort
	newPort.OfpPort = &newOfpPort

	if state == voltha.OperStatus_ACTIVE {
		newOfpPort.Config = newOfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		newOfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
	} else {
		newOfpPort.Config = newOfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		newOfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
	}
	return &newPort
}

// setupUNILogicalPorts creates a UNI port on the logical device that represents a child UNI interface
func (agent *LogicalAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	logger.Infow("setupUNILogicalPort", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error
	var added bool
	//Get UNI port number
	for _, port := range childDevice.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			if added, err = agent.addUNILogicalPort(ctx, childDevice, port); err != nil {
				logger.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			if added {
				agent.addLogicalPortToMap(port.PortNo, false)
			}
		}
	}
	return err
}

// deleteAllLogicalPorts deletes all logical ports associated with this logical device
func (agent *LogicalAgent) deleteAllLogicalPorts(ctx context.Context) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})

	// for each port
	for portID := range agent.portLoader.List() {
		// TODO: can just call agent.deleteLogicalPort()?
		if portHandle, have := agent.portLoader.Lock(portID); have {
			oldPort := portHandle.GetReadOnly()
			// delete
			err := portHandle.Delete(ctx)
			portHandle.Unlock()
			if err != nil {
				return err
			}
			agent.deleteLogicalPortsFromMap([]uint32{oldPort.OfpPort.PortNo})
			// and send event
			go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_DELETE, oldPort.OfpPort)
		}
	}

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(context.Background()); err != nil {
			logger.Warnw("device-routes-not-ready", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
		}
	}()
	return nil
}

// deleteLogicalPort removes the logical port
func (agent *LogicalAgent) deleteLogicalPort(ctx context.Context, lPort *voltha.LogicalPort) error {
	portHandle, have := agent.portLoader.Lock(lPort.OfpPort.PortNo)
	if !have {
		return nil
	}
	defer portHandle.Unlock()

	port := portHandle.GetReadOnly()
	if err := portHandle.Delete(ctx); err != nil {
		return err
	}

	// Remove the logical port from cache
	agent.deleteLogicalPortsFromMap([]uint32{lPort.OfpPort.PortNo})

	// and send event
	go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_DELETE, port.OfpPort)

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(context.Background()); err != nil {
			logger.Warnw("device-routes-not-ready", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
		}
	}()
	return nil
}

// deleteLogicalPorts removes the logical ports associated with that deviceId
func (agent *LogicalAgent) deleteLogicalPorts(ctx context.Context, deviceID string) error {
	logger.Debugw("deleting-logical-ports", log.Fields{"device-id": deviceID})

	// for each port
	for portNo := range agent.portLoader.ListForDevice(deviceID) {
		if portHandle, have := agent.portLoader.Lock(portNo); have {
			// if belongs to this device
			if oldPort := portHandle.GetReadOnly(); oldPort.DeviceId == deviceID {
				// delete
				if err := portHandle.Delete(ctx); err != nil {
					portHandle.Unlock()
					return err
				}
				agent.deleteLogicalPortsFromMap([]uint32{oldPort.OfpPort.PortNo})
				// and send event
				go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_DELETE, oldPort.OfpPort)
			}
			portHandle.Unlock()
		}
	}

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(context.Background()); err != nil {
			logger.Warnw("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
		}
	}()
	return nil
}

// enableLogicalPort enables the logical port
func (agent *LogicalAgent) enableLogicalPort(ctx context.Context, lPortNo uint32) error {
	portHandle, have := agent.portLoader.Lock(lPortNo)
	if !have {
		return status.Errorf(codes.NotFound, "port-%d-not-exist", lPortNo)
	}
	defer portHandle.Unlock()

	oldPort := portHandle.GetReadOnly()

	newPort := *oldPort // only clone the struct(s) that will be changed
	newOfpPort := *oldPort.OfpPort
	newPort.OfpPort = &newOfpPort

	newOfpPort.Config = newOfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
	if err := portHandle.Update(ctx, &newPort); err != nil {
		return err
	}
	go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)
	return nil
}

// disableLogicalPort disabled the logical port
func (agent *LogicalAgent) disableLogicalPort(ctx context.Context, lPortNo uint32) error {
	portHandle, have := agent.portLoader.Lock(lPortNo)
	if !have {
		return status.Errorf(codes.NotFound, "port-%d-not-exist", lPortNo)
	}
	defer portHandle.Unlock()

	oldPort := portHandle.GetReadOnly()

	newPort := *oldPort // only clone the struct(s) that will be changed
	newOfpPort := *oldPort.OfpPort
	newPort.OfpPort = &newOfpPort

	newOfpPort.Config = (newOfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)) | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
	if err := portHandle.Update(ctx, &newPort); err != nil {
		return err
	}
	go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)
	return nil
}

// addNNILogicalPort adds an NNI port to the logical device.  It returns a bool representing whether a port has been
// added and an error in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addNNILogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) (bool, error) {
	logger.Debugw("addNNILogicalPort", log.Fields{"NNI": port})

	portHandle, have := agent.portLoader.Lock(port.PortNo)
	if have {
		portHandle.Unlock()
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	// TODO: Change the port creation logic to include the port capability.  This will eliminate the port capability
	// request that the Core makes following a port create event.

	// First get the port capability
	portCap, err := agent.deviceMgr.getPortCapability(ctx, device.Id, port.PortNo)
	if err != nil {
		logger.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}

	portCap.Port.RootPort = true
	lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
	lp.DeviceId = device.Id
	lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
	lp.OfpPort.PortNo = port.PortNo
	lp.OfpPort.Name = lp.Id
	lp.DevicePortNo = port.PortNo

	portHandle, created, err := agent.portLoader.LockOrCreate(ctx, lp)
	if err != nil {
		return false, err
	}
	defer portHandle.Unlock()

	if !created {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	// Setup the routes for this device and then send the port update event to the OF Controller
	go func() {
		// First setup the routes
		if err := agent.updateRoutes(context.Background(), device, lp, agent.listLogicalDevicePorts()); err != nil {
			// This is not an error as we may not have enough logical ports to set up routes or some PON ports have not been
			// created yet.
			logger.Infow("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "logical-port": lp.OfpPort.PortNo, "error": err})
		}

		// Send a port update event
		agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_ADD, lp.OfpPort)
	}()
	return true, nil
}

// addUNILogicalPort adds an UNI port to the logical device.  It returns a bool representing whether a port has been
// added and an error in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device, port *voltha.Port) (bool, error) {
	logger.Debugw("addUNILogicalPort", log.Fields{"port": port})
	if childDevice.AdminState != voltha.AdminState_ENABLED || childDevice.OperStatus != voltha.OperStatus_ACTIVE {
		logger.Infow("device-not-ready", log.Fields{"deviceId": childDevice.Id, "admin": childDevice.AdminState, "oper": childDevice.OperStatus})
		return false, nil
	}

	portHandle, have := agent.portLoader.Lock(port.PortNo)
	if have {
		portHandle.Unlock()
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	// TODO: Change the port creation logic to include the port capability.  This will eliminate the port capability
	// request that the Core makes following a port create event.

	// First get the port capability
	portCap, err := agent.deviceMgr.getPortCapability(ctx, childDevice.Id, port.PortNo)
	if err != nil {
		logger.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}

	logger.Debugw("adding-uni", log.Fields{"deviceId": childDevice.Id})
	newPort := portCap.Port
	newPort.RootPort = false
	newPort.Id = port.Label
	newPort.OfpPort.PortNo = port.PortNo
	newPort.DeviceId = childDevice.Id
	newPort.DevicePortNo = port.PortNo

	portHandle, created, err := agent.portLoader.LockOrCreate(ctx, newPort)
	if err != nil {
		return false, err
	}
	defer portHandle.Unlock()

	if !created {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	// Setup the routes for this device and then send the port update event to the OF Controller
	go func() {
		// First setup the routes
		if err := agent.updateRoutes(context.Background(), childDevice, newPort, agent.listLogicalDevicePorts()); err != nil {
			// This is not an error as we may not have enough logical ports to set up routes or some PON ports have not been
			// created yet.
			logger.Infow("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "logical-port": newPort.OfpPort.PortNo, "error": err})
		}

		// Send a port update event
		agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_ADD, newPort.OfpPort)
	}()
	return true, nil
}

//GetWildcardInputPorts filters out the logical port number from the set of logical ports on the device and
//returns their port numbers.  This function is invoked only during flow decomposition where the lock on the logical
//device is already held.  Therefore it is safe to retrieve the logical device without lock.
func (agent *LogicalAgent) GetWildcardInputPorts(excludePort uint32) map[uint32]struct{} {
	portIDs := agent.portLoader.List()
	delete(portIDs, excludePort)
	return portIDs
}

// helpers for agent.logicalPortsNo

func (agent *LogicalAgent) addLogicalPortToMap(portNo uint32, nniPort bool) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	if exist := agent.logicalPortsNo[portNo]; !exist {
		agent.logicalPortsNo[portNo] = nniPort
	}
}

func (agent *LogicalAgent) addLogicalPortsToMap(lps map[uint32]*voltha.LogicalPort) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, lp := range lps {
		if exist := agent.logicalPortsNo[lp.DevicePortNo]; !exist {
			agent.logicalPortsNo[lp.DevicePortNo] = lp.RootPort
		}
	}
}

func (agent *LogicalAgent) deleteLogicalPortsFromMap(portsNo []uint32) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, pNo := range portsNo {
		delete(agent.logicalPortsNo, pNo)
	}
}

func (agent *LogicalAgent) isNNIPort(portNo uint32) bool {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	if exist := agent.logicalPortsNo[portNo]; exist {
		return agent.logicalPortsNo[portNo]
	}
	return false
}

func (agent *LogicalAgent) getFirstNNIPort() (uint32, error) {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			return portNo, nil
		}
	}
	return 0, status.Error(codes.NotFound, "No NNI port found")
}

//GetNNIPorts returns NNI ports.
func (agent *LogicalAgent) GetNNIPorts() []uint32 {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	nniPorts := make([]uint32, 0)
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			nniPorts = append(nniPorts, portNo)
		}
	}
	return nniPorts
}

// getUNILogicalPortNo returns the UNI logical port number specified in the flow
func (agent *LogicalAgent) getUNILogicalPortNo(flow *ofp.OfpFlowStats) (uint32, error) {
	var uniPort uint32
	inPortNo := fu.GetInPort(flow)
	outPortNo := fu.GetOutPort(flow)
	if agent.isNNIPort(inPortNo) {
		uniPort = outPortNo
	} else if agent.isNNIPort(outPortNo) {
		uniPort = inPortNo
	}
	if uniPort != 0 {
		return uniPort, nil
	}
	return 0, status.Errorf(codes.NotFound, "no-uni-port: %v", flow)
}
