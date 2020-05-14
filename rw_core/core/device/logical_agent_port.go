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
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListLogicalDevicePorts returns logical device ports
func (agent *LogicalAgent) ListLogicalDevicePorts(ctx context.Context) (*voltha.LogicalPorts, error) {
	logger.Debug("ListLogicalDevicePorts")
	logicalDevice, err := agent.GetLogicalDevice(ctx)
	if err != nil {
		return nil, err
	}
	if logicalDevice == nil {
		return &voltha.LogicalPorts{}, nil
	}
	lPorts := make([]*voltha.LogicalPort, 0)
	lPorts = append(lPorts, logicalDevice.Ports...)
	return &voltha.LogicalPorts{Items: lPorts}, nil
}

func (agent *LogicalAgent) updateLogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) error {
	logger.Debugw("updateLogicalPort", log.Fields{"deviceId": device.Id, "port": port})
	var err error
	if port.Type == voltha.Port_ETHERNET_NNI {
		if _, err = agent.addNNILogicalPort(ctx, device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, true)
	} else if port.Type == voltha.Port_ETHERNET_UNI {
		if _, err = agent.addUNILogicalPort(ctx, device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, false)
	} else {
		// Update the device routes to ensure all routes on the logical device have been calculated
		if err = agent.buildRoutes(ctx); err != nil {
			// Not an error - temporary state
			logger.Warnw("failed-to-update-routes", log.Fields{"device-id": device.Id, "port": port, "error": err})
		}
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
func (agent *LogicalAgent) updatePortState(ctx context.Context, deviceID string, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Infow("updatePortState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "portNo": portNo, "state": operStatus})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	original := agent.getLogicalDeviceWithoutLock()
	updatedPorts := clonePorts(original.Ports)
	for _, port := range updatedPorts {
		if port.DeviceId == deviceID && port.DevicePortNo == portNo {
			if operStatus == voltha.OperStatus_ACTIVE {
				port.OfpPort.Config = port.OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				port.OfpPort.Config = port.OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}
			// Update the logical device
			if err := agent.updateLogicalDevicePortsWithoutLock(ctx, original, updatedPorts); err != nil {
				logger.Errorw("error-updating-logical-device", log.Fields{"error": err})
				return err
			}
			return nil
		}
	}
	return status.Errorf(codes.NotFound, "port-%d-not-exist", portNo)
}

// updatePortsState updates the ports state related to the device
func (agent *LogicalAgent) updatePortsState(ctx context.Context, device *voltha.Device, state voltha.OperStatus_Types) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	original := agent.getLogicalDeviceWithoutLock()
	updatedPorts := clonePorts(original.Ports)
	for _, port := range updatedPorts {
		if port.DeviceId == device.Id {
			if state == voltha.OperStatus_ACTIVE {
				port.OfpPort.Config = port.OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				port.OfpPort.Config = port.OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}
		}
	}
	// Updating the logical device will trigger the poprt change events to be populated to the controller
	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, original, updatedPorts); err != nil {
		logger.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
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
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	cloned := agent.getLogicalDeviceWithoutLock()

	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, cloned, []*voltha.LogicalPort{}); err != nil {
		logger.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
}

// deleteLogicalPort removes the logical port
func (agent *LogicalAgent) deleteLogicalPort(ctx context.Context, lPort *voltha.LogicalPort) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logicalDevice := agent.getLogicalDeviceWithoutLock()

	index := -1
	for i, logicalPort := range logicalDevice.Ports {
		if logicalPort.Id == lPort.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		clonedPorts := clonePorts(logicalDevice.Ports)
		if index < len(clonedPorts)-1 {
			copy(clonedPorts[index:], clonedPorts[index+1:])
		}
		clonedPorts[len(clonedPorts)-1] = nil
		clonedPorts = clonedPorts[:len(clonedPorts)-1]
		logger.Debugw("logical-port-deleted", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		if err := agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, clonedPorts); err != nil {
			logger.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

		// Remove the logical port from cache
		agent.deleteLogicalPortsFromMap([]uint32{lPort.DevicePortNo})
		// Reset the logical device routes
		go func() {
			if err := agent.buildRoutes(context.Background()); err != nil {
				logger.Warnw("device-routes-not-ready", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			}
		}()
	}
	return nil
}

// deleteLogicalPorts removes the logical ports associated with that deviceId
func (agent *LogicalAgent) deleteLogicalPorts(ctx context.Context, deviceID string) error {
	logger.Debugw("deleting-logical-ports", log.Fields{"device-id": deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logicalDevice := agent.getLogicalDeviceWithoutLock()
	lPortstoKeep := []*voltha.LogicalPort{}
	lPortsNoToDelete := []uint32{}
	for _, logicalPort := range logicalDevice.Ports {
		if logicalPort.DeviceId != deviceID {
			lPortstoKeep = append(lPortstoKeep, logicalPort)
		} else {
			lPortsNoToDelete = append(lPortsNoToDelete, logicalPort.DevicePortNo)
		}
	}
	logger.Debugw("deleted-logical-ports", log.Fields{"ports": lPortstoKeep})
	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, lPortstoKeep); err != nil {
		logger.Errorw("logical-device-update-failed", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return err
	}
	// Remove the port from the cached logical ports set
	agent.deleteLogicalPortsFromMap(lPortsNoToDelete)

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(context.Background()); err != nil {
			logger.Warnw("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
		}
	}()

	return nil
}

// enableLogicalPort enables the logical port
func (agent *LogicalAgent) enableLogicalPort(ctx context.Context, lPortID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logicalDevice := agent.getLogicalDeviceWithoutLock()

	index := -1
	for i, logicalPort := range logicalDevice.Ports {
		if logicalPort.Id == lPortID {
			index = i
			break
		}
	}
	if index >= 0 {
		clonedPorts := clonePorts(logicalDevice.Ports)
		clonedPorts[index].OfpPort.Config = clonedPorts[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, clonedPorts)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

// disableLogicalPort disabled the logical port
func (agent *LogicalAgent) disableLogicalPort(ctx context.Context, lPortID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	// Get the most up to date logical device
	logicalDevice := agent.getLogicalDeviceWithoutLock()
	index := -1
	for i, logicalPort := range logicalDevice.Ports {
		if logicalPort.Id == lPortID {
			index = i
			break
		}
	}
	if index >= 0 {
		clonedPorts := clonePorts(logicalDevice.Ports)
		clonedPorts[index].OfpPort.Config = (clonedPorts[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)) | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, clonedPorts)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

// addNNILogicalPort adds an NNI port to the logical device.  It returns a bool representing whether a port has been
// added and an eror in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addNNILogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) (bool, error) {
	logger.Debugw("addNNILogicalPort", log.Fields{"NNI": port})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}
	if agent.portExist(device, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		agent.requestQueue.RequestComplete()
		return false, nil
	}
	agent.requestQueue.RequestComplete()

	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, device.Id, port.PortNo); err != nil {
		logger.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}

	defer agent.requestQueue.RequestComplete()
	// Double check again if this port has been already added since the getPortCapability could have taken a long time
	if agent.portExist(device, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	portCap.Port.RootPort = true
	lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
	lp.DeviceId = device.Id
	lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
	lp.OfpPort.PortNo = port.PortNo
	lp.OfpPort.Name = lp.Id
	lp.DevicePortNo = port.PortNo

	ld := agent.getLogicalDeviceWithoutLock()

	clonedPorts := clonePorts(ld.Ports)
	if clonedPorts == nil {
		clonedPorts = make([]*voltha.LogicalPort, 0)
	}
	clonedPorts = append(clonedPorts, lp)

	if err = agent.updateLogicalDevicePortsWithoutLock(ctx, ld, clonedPorts); err != nil {
		logger.Errorw("error-updating-logical-device", log.Fields{"error": err})
		return false, err
	}

	// Update the device routes with this new logical port
	clonedLP := (proto.Clone(lp)).(*voltha.LogicalPort)
	go func() {
		if err := agent.updateRoutes(context.Background(), clonedLP); err != nil {
			logger.Warnw("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "logical-port": lp.OfpPort.PortNo, "error": err})
		}
	}()

	return true, nil
}

func (agent *LogicalAgent) portExist(device *voltha.Device, port *voltha.Port) bool {
	ldevice := agent.getLogicalDeviceWithoutLock()
	for _, lPort := range ldevice.Ports {
		if lPort.DeviceId == device.Id && lPort.DevicePortNo == port.PortNo && lPort.Id == port.Label {
			return true
		}
	}
	return false
}

// addUNILogicalPort adds an UNI port to the logical device.  It returns a bool representing whether a port has been
// added and an eror in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device, port *voltha.Port) (bool, error) {
	logger.Debugw("addUNILogicalPort", log.Fields{"port": port})
	if childDevice.AdminState != voltha.AdminState_ENABLED || childDevice.OperStatus != voltha.OperStatus_ACTIVE {
		logger.Infow("device-not-ready", log.Fields{"deviceId": childDevice.Id, "admin": childDevice.AdminState, "oper": childDevice.OperStatus})
		return false, nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}

	if agent.portExist(childDevice, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		agent.requestQueue.RequestComplete()
		return false, nil
	}
	agent.requestQueue.RequestComplete()
	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, childDevice.Id, port.PortNo); err != nil {
		logger.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}
	defer agent.requestQueue.RequestComplete()
	// Double check again if this port has been already added since the getPortCapability could have taken a long time
	if agent.portExist(childDevice, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}
	// Get stored logical device
	ldevice := agent.getLogicalDeviceWithoutLock()

	logger.Debugw("adding-uni", log.Fields{"deviceId": childDevice.Id})
	portCap.Port.RootPort = false
	portCap.Port.Id = port.Label
	portCap.Port.OfpPort.PortNo = port.PortNo
	portCap.Port.DeviceId = childDevice.Id
	portCap.Port.DevicePortNo = port.PortNo
	clonedPorts := clonePorts(ldevice.Ports)
	if clonedPorts == nil {
		clonedPorts = make([]*voltha.LogicalPort, 0)
	}
	clonedPorts = append(clonedPorts, portCap.Port)
	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, ldevice, clonedPorts); err != nil {
		return false, err
	}
	// Update the device graph with this new logical port
	clonedLP := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)

	go func() {
		if err := agent.updateRoutes(context.Background(), clonedLP); err != nil {
			logger.Warn("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
		}
	}()

	return true, nil
}

func clonePorts(ports []*voltha.LogicalPort) []*voltha.LogicalPort {
	return proto.Clone(&voltha.LogicalPorts{Items: ports}).(*voltha.LogicalPorts).Items
}

//updateLogicalDevicePortsWithoutLock updates the
func (agent *LogicalAgent) updateLogicalDevicePortsWithoutLock(ctx context.Context, device *voltha.LogicalDevice, newPorts []*voltha.LogicalPort) error {
	oldPorts := device.Ports
	device.Ports = newPorts
	if err := agent.updateLogicalDeviceWithoutLock(ctx, device); err != nil {
		return err
	}
	agent.portUpdated(oldPorts, newPorts)
	return nil
}

// diff go over two lists of logical ports and return what's new, what's changed and what's removed.
func diff(oldList, newList []*voltha.LogicalPort) (newPorts, changedPorts, deletedPorts map[string]*voltha.LogicalPort) {
	newPorts = make(map[string]*voltha.LogicalPort, len(newList))
	changedPorts = make(map[string]*voltha.LogicalPort, len(oldList))
	deletedPorts = make(map[string]*voltha.LogicalPort, len(oldList))

	for _, n := range newList {
		newPorts[n.Id] = n
	}

	for _, o := range oldList {
		if n, have := newPorts[o.Id]; have {
			delete(newPorts, o.Id) // not new
			if !proto.Equal(n, o) {
				changedPorts[n.Id] = n // changed
			}
		} else {
			deletedPorts[o.Id] = o // deleted
		}
	}

	return newPorts, changedPorts, deletedPorts
}

// portUpdated is invoked when a port is updated on the logical device
func (agent *LogicalAgent) portUpdated(prevPorts, currPorts []*voltha.LogicalPort) interface{} {
	// Get the difference between the two list
	newPorts, changedPorts, deletedPorts := diff(prevPorts, currPorts)

	// Send the port change events to the OF controller
	for _, newP := range newPorts {
		go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_ADD, Desc: newP.OfpPort})
	}
	for _, change := range changedPorts {
		go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_MODIFY, Desc: change.OfpPort})
	}
	for _, del := range deletedPorts {
		go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_DELETE, Desc: del.OfpPort})
	}

	return nil
}

//GetWildcardInputPorts filters out the logical port number from the set of logical ports on the device and
//returns their port numbers.  This function is invoked only during flow decomposition where the lock on the logical
//device is already held.  Therefore it is safe to retrieve the logical device without lock.
func (agent *LogicalAgent) GetWildcardInputPorts(excludePort ...uint32) []uint32 {
	lPorts := make([]uint32, 0)
	var exclPort uint32
	if len(excludePort) == 1 {
		exclPort = excludePort[0]
	}
	lDevice := agent.getLogicalDeviceWithoutLock()
	for _, port := range lDevice.Ports {
		if port.OfpPort.PortNo != exclPort {
			lPorts = append(lPorts, port.OfpPort.PortNo)
		}
	}
	return lPorts
}
