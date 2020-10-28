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
	"sync"

	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v4/pkg/flows"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listLogicalDevicePorts returns logical device ports
func (agent *LogicalAgent) listLogicalDevicePorts(ctx context.Context) map[uint32]*voltha.LogicalPort {
	logger.Debug(ctx, "listLogicalDevicePorts")
	portIDs := agent.portLoader.ListIDs()
	ret := make(map[uint32]*voltha.LogicalPort, len(portIDs))
	for portID := range portIDs {
		if portHandle, have := agent.portLoader.Lock(portID); have {
			ret[portID] = portHandle.GetReadOnly()
			portHandle.Unlock()
		}
	}
	return ret
}

func (agent *LogicalAgent) updateLogicalPort(ctx context.Context, device *voltha.Device, devicePorts map[uint32]*voltha.Port, port *voltha.Port) error {
	logger.Debugw(ctx, "updateLogicalPort", log.Fields{"device-id": device.Id, "port": port})
	switch port.Type {
	case voltha.Port_ETHERNET_NNI:
		if err := agent.addNNILogicalPort(ctx, device.Id, devicePorts, port); err != nil {
			return err
		}
	case voltha.Port_ETHERNET_UNI:
		if err := agent.addUNILogicalPort(ctx, device.Id, device.AdminState, device.OperStatus, devicePorts, port); err != nil {
			return err
		}
	case voltha.Port_PON_OLT:
		// Rebuilt the routes on Parent PON port addition
		go func() {
			if err := agent.buildRoutes(log.WithSpanFromContext(context.Background(), ctx)); err != nil {
				// Not an error - temporary state
				logger.Infow(ctx, "failed-to-update-routes-after-adding-parent-pon-port", log.Fields{"device-id": device.Id, "port": port, "ports-count": len(devicePorts), "error": err})
			}
		}()
		//fallthrough
	case voltha.Port_PON_ONU:
		// Add the routes corresponding to that child device
		go func() {
			if err := agent.updateAllRoutes(log.WithSpanFromContext(context.Background(), ctx), device.Id, devicePorts); err != nil {
				// Not an error - temporary state
				logger.Infow(ctx, "failed-to-update-routes-after-adding-child-pon-port", log.Fields{"device-id": device.Id, "port": port, "ports-count": len(devicePorts), "error": err})
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
	logger.Infow(ctx, "setupLogicalPorts", log.Fields{"logical-device-id": agent.logicalDeviceID})
	// First add any NNI ports which could have been missing
	if err := agent.setupNNILogicalPorts(ctx, agent.rootDeviceID); err != nil {
		logger.Errorw(ctx, "error-setting-up-NNI-ports", log.Fields{"error": err, "device-id": agent.rootDeviceID})
		return err
	}

	// Now, set up the UNI ports if needed.
	children, err := agent.deviceMgr.GetAllChildDevices(ctx, agent.rootDeviceID)
	if err != nil {
		logger.Errorw(ctx, "error-getting-child-devices", log.Fields{"error": err, "device-id": agent.rootDeviceID})
		return err
	}
	responses := make([]coreutils.Response, 0)
	for _, child := range children.Items {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(ctx context.Context, child *voltha.Device) {
			defer response.Done()

			childPorts, err := agent.deviceMgr.listDevicePorts(ctx, child.Id)
			if err != nil {
				logger.Error(ctx, "setting-up-UNI-ports-failed", log.Fields{"device-id": child.Id})
				response.Error(status.Errorf(codes.Internal, "UNI-ports-setup-failed: %s", child.Id))
				return
			}

			if err = agent.setupUNILogicalPorts(ctx, child, childPorts); err != nil {
				logger.Error(ctx, "setting-up-UNI-ports-failed", log.Fields{"device-id": child.Id})
				response.Error(status.Errorf(codes.Internal, "UNI-ports-setup-failed: %s", child.Id))
			}
		}(log.WithSpanFromContext(context.Background(), ctx), child)
	}
	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, responses...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

// setupNNILogicalPorts creates an NNI port on the logical device that represents an NNI interface on a root device
func (agent *LogicalAgent) setupNNILogicalPorts(ctx context.Context, deviceID string) error {
	logger.Infow(ctx, "setupNNILogicalPorts-start", log.Fields{"logical-device-id": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter

	devicePorts, err := agent.deviceMgr.listDevicePorts(ctx, deviceID)
	if err != nil {
		logger.Errorw(ctx, "error-retrieving-device-ports", log.Fields{"error": err, "device-id": deviceID})
		return err
	}

	//Get UNI port number
	for _, port := range devicePorts {
		if port.Type == voltha.Port_ETHERNET_NNI {
			if err = agent.addNNILogicalPort(ctx, deviceID, devicePorts, port); err != nil {
				logger.Errorw(ctx, "error-adding-NNI-port", log.Fields{"error": err})
			}
		}
	}
	return err
}

// updatePortState updates the port state of the device
func (agent *LogicalAgent) updatePortState(ctx context.Context, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Infow(ctx, "updatePortState-start", log.Fields{"logical-device-id": agent.logicalDeviceID, "portNo": portNo, "state": operStatus})

	portHandle, have := agent.portLoader.Lock(portNo)
	if !have {
		return status.Errorf(codes.NotFound, "port-%d-not-exist", portNo)
	}
	defer portHandle.Unlock()

	newPort := clonePortSetState(portHandle.GetReadOnly(), operStatus)
	if err := portHandle.Update(ctx, newPort); err != nil {
		return err
	}
	agent.orderedEvents.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)
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
func (agent *LogicalAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device, childDevicePorts map[uint32]*voltha.Port) error {
	logger.Infow(ctx, "setupUNILogicalPorts", log.Fields{"logical-device-id": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error
	//Get UNI port number
	for _, port := range childDevicePorts {
		if port.Type == voltha.Port_ETHERNET_UNI {
			if err = agent.addUNILogicalPort(ctx, childDevice.Id, childDevice.AdminState, childDevice.OperStatus, childDevicePorts, port); err != nil {
				logger.Errorw(ctx, "error-adding-UNI-port", log.Fields{"error": err})
			}
		}
	}
	return err
}

// deleteAllLogicalPorts deletes all logical ports associated with this logical device
func (agent *LogicalAgent) deleteAllLogicalPorts(ctx context.Context) error {
	logger.Infow(ctx, "updatePortsState-start", log.Fields{"logical-device-id": agent.logicalDeviceID})

	// for each port
	for portID := range agent.portLoader.ListIDs() {
		// TODO: can just call agent.deleteLogicalPort()?
		if portHandle, have := agent.portLoader.Lock(portID); have {
			oldPort := portHandle.GetReadOnly()
			// delete
			err := portHandle.Delete(ctx)
			portHandle.Unlock()
			if err != nil {
				return err
			}
			// and send event
			agent.orderedEvents.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_DELETE, oldPort.OfpPort)
		}
	}

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(log.WithSpanFromContext(context.Background(), ctx)); err != nil {
			logger.Warnw(ctx, "device-routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
		}
	}()
	return nil
}

// deleteLogicalPorts removes the logical ports associated with that deviceId
func (agent *LogicalAgent) deleteLogicalPorts(ctx context.Context, deviceID string) error {
	logger.Debugw(ctx, "deleting-logical-ports", log.Fields{"device-id": deviceID})

	// for each port
	for portNo := range agent.portLoader.ListIDsForDevice(deviceID) {
		if portHandle, have := agent.portLoader.Lock(portNo); have {
			// if belongs to this device
			if oldPort := portHandle.GetReadOnly(); oldPort.DeviceId == deviceID {
				// delete
				if err := portHandle.Delete(ctx); err != nil {
					portHandle.Unlock()
					return err
				}
				// and send event
				agent.orderedEvents.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_DELETE, oldPort.OfpPort)
			}
			portHandle.Unlock()
		}
	}

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(log.WithSpanFromContext(context.Background(), ctx)); err != nil {
			logger.Warnw(ctx, "routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
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
	agent.orderedEvents.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)
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
	agent.orderedEvents.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_MODIFY, newPort.OfpPort)
	return nil
}

// addNNILogicalPort adds an NNI port to the logical device.  It returns a bool representing whether a port has been
// added and an error in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addNNILogicalPort(ctx context.Context, deviceID string, devicePorts map[uint32]*voltha.Port, port *voltha.Port) error {
	logger.Debugw(ctx, "addNNILogicalPort", log.Fields{"logical-device-id": agent.logicalDeviceID, "nni-port": port})

	label := fmt.Sprintf("nni-%d", port.PortNo)
	ofpPort := *port.OfpPort
	ofpPort.HwAddr = append([]uint32{}, port.OfpPort.HwAddr...)
	ofpPort.PortNo = port.PortNo
	ofpPort.Name = label
	nniPort := &voltha.LogicalPort{
		RootPort:     true,
		DeviceId:     deviceID,
		Id:           label,
		DevicePortNo: port.PortNo,
		OfpPort:      &ofpPort,
		OfpPortStats: &ofp.OfpPortStats{},
	}

	portHandle, created, err := agent.portLoader.LockOrCreate(ctx, nniPort)
	if err != nil {
		return err
	}
	defer portHandle.Unlock()

	if !created {
		logger.Debugw(ctx, "port-already-exist", log.Fields{"port": port})
		return nil
	}

	// ensure that no events will be sent until this one is
	queuePosition := agent.orderedEvents.assignQueuePosition()

	// Setup the routes for this device and then send the port update event to the OF Controller
	go func() {
		// First setup the routes
		if err := agent.updateRoutes(log.WithSpanFromContext(context.Background(), ctx), deviceID, devicePorts, nniPort, agent.listLogicalDevicePorts(ctx)); err != nil {
			// This is not an error as we may not have enough logical ports to set up routes or some PON ports have not been
			// created yet.
			logger.Infow(ctx, "routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "logical-port": nniPort.OfpPort.PortNo, "error": err})
		}

		// send event, and allow any queued events to be sent as well
		queuePosition.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_ADD, nniPort.OfpPort)
	}()
	return nil
}

// addUNILogicalPort adds an UNI port to the logical device.  It returns a bool representing whether a port has been
// added and an error in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addUNILogicalPort(ctx context.Context, deviceID string, deviceAdminState voltha.AdminState_Types, deviceOperStatus voltha.OperStatus_Types, devicePorts map[uint32]*voltha.Port, port *voltha.Port) error {
	logger.Debugw(ctx, "addUNILogicalPort", log.Fields{"port": port})
	if deviceAdminState != voltha.AdminState_ENABLED || deviceOperStatus != voltha.OperStatus_ACTIVE {
		logger.Infow(ctx, "device-not-ready", log.Fields{"device-id": deviceID, "admin": deviceAdminState, "oper": deviceOperStatus})
		return nil
	}
	ofpPort := *port.OfpPort
	ofpPort.HwAddr = append([]uint32{}, port.OfpPort.HwAddr...)
	ofpPort.PortNo = port.PortNo
	uniPort := &voltha.LogicalPort{
		RootPort:     false,
		DeviceId:     deviceID,
		Id:           port.Label,
		DevicePortNo: port.PortNo,
		OfpPort:      &ofpPort,
		OfpPortStats: &ofp.OfpPortStats{},
	}

	portHandle, created, err := agent.portLoader.LockOrCreate(ctx, uniPort)
	if err != nil {
		return err
	}
	defer portHandle.Unlock()

	if !created {
		logger.Debugw(ctx, "port-already-exist", log.Fields{"port": port})
		return nil
	}

	// ensure that no events will be sent until this one is
	queuePosition := agent.orderedEvents.assignQueuePosition()

	// Setup the routes for this device and then send the port update event to the OF Controller
	go func() {
		ctx = log.WithSpanFromContext(context.Background(), ctx)
		// First setup the routes
		if err := agent.updateRoutes(ctx, deviceID, devicePorts, uniPort, agent.listLogicalDevicePorts(ctx)); err != nil {
			// This is not an error as we may not have enough logical ports to set up routes or some PON ports have not been
			// created yet.
			logger.Infow(ctx, "routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "logical-port": uniPort.OfpPort.PortNo, "error": err})
		}

		// send event, and allow any queued events to be sent as well
		queuePosition.send(ctx, agent, agent.logicalDeviceID, ofp.OfpPortReason_OFPPR_ADD, uniPort.OfpPort)
	}()
	return nil
}

// send is a convenience to avoid calling both assignQueuePosition and qp.send
func (e *orderedEvents) send(ctx context.Context, agent *LogicalAgent, deviceID string, reason ofp.OfpPortReason, desc *ofp.OfpPort) {
	qp := e.assignQueuePosition()
	go qp.send(log.WithSpanFromContext(context.Background(), ctx), agent, deviceID, reason, desc)
}

// TODO: shouldn't need to guarantee event ordering like this
//       event ordering should really be protected by per-LogicalPort lock
//       once routing uses on-demand calculation only, this should be changed
// assignQueuePosition ensures that no events will be sent until this thread calls send() on the returned queuePosition
func (e *orderedEvents) assignQueuePosition() queuePosition {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	prev := e.last
	next := make(chan struct{})
	e.last = next
	return queuePosition{
		prev: prev,
		next: next,
	}
}

// orderedEvents guarantees the order that events are sent, while allowing events to back up.
type orderedEvents struct {
	mutex sync.Mutex
	last  <-chan struct{}
}

type queuePosition struct {
	prev <-chan struct{}
	next chan<- struct{}
}

// send waits for its turn, then sends the event, then notifies the next in line
func (qp queuePosition) send(ctx context.Context, agent *LogicalAgent, deviceID string, reason ofp.OfpPortReason, desc *ofp.OfpPort) {
	if qp.prev != nil {
		<-qp.prev // wait for turn
	}
	agent.ldeviceMgr.SendChangeEvent(ctx, deviceID, reason, desc)
	close(qp.next) // notify next
}

// GetWildcardInputPorts filters out the logical port number from the set of logical ports on the device and
// returns their port numbers.
func (agent *LogicalAgent) GetWildcardInputPorts(ctx context.Context, excludePort uint32) map[uint32]struct{} {
	portIDs := agent.portLoader.ListIDs()
	delete(portIDs, excludePort)
	return portIDs
}

// isNNIPort return true iff the specified port belongs to the parent (OLT) device
func (agent *LogicalAgent) isNNIPort(portNo uint32) bool {
	portHandle, have := agent.portLoader.Lock(portNo)
	if !have {
		return false
	}
	defer portHandle.Unlock()

	// any root-device logical port is an NNI port
	return portHandle.GetReadOnly().RootPort
}

// getAnyNNIPort returns an NNI port
func (agent *LogicalAgent) getAnyNNIPort() (uint32, error) {
	for portID := range agent.portLoader.ListIDsForDevice(agent.rootDeviceID) {
		return portID, nil
	}
	return 0, status.Error(codes.NotFound, "No NNI port found")
}

//GetNNIPorts returns all NNI ports
func (agent *LogicalAgent) GetNNIPorts() map[uint32]struct{} {
	return agent.portLoader.ListIDsForDevice(agent.rootDeviceID)
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
