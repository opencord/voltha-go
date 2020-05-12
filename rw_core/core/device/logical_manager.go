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
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/core/device/event"
	"github.com/opencord/voltha-go/rw_core/utils"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalManager represent logical device manager attributes
type LogicalManager struct {
	*event.Manager
	logicalDeviceAgents            sync.Map
	deviceMgr                      *Manager
	kafkaICProxy                   kafka.InterContainerProxy
	clusterDataProxy               *model.Proxy
	defaultTimeout                 time.Duration
	logicalDevicesLoadingLock      sync.RWMutex
	logicalDeviceLoadingInProgress map[string][]chan int
}

func (ldMgr *LogicalManager) addLogicalDeviceAgentToMap(agent *LogicalAgent) {
	if _, exist := ldMgr.logicalDeviceAgents.Load(agent.logicalDeviceID); !exist {
		ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
	}
}

// getLogicalDeviceAgent returns the logical device agent.  If the device is not in memory then the device will
// be loaded from dB and a logical device agent created to managed it.
func (ldMgr *LogicalManager) getLogicalDeviceAgent(ctx context.Context, logicalDeviceID string) *LogicalAgent {
	logger.Debugw("get-logical-device-agent", log.Fields{"logical-device-id": logicalDeviceID})
	agent, ok := ldMgr.logicalDeviceAgents.Load(logicalDeviceID)
	if ok {
		lda := agent.(*LogicalAgent)
		if lda.logicalDevice == nil {
			// This can happen when an agent for the logical device has been created but the logical device
			// itself is not ready for action as it is waiting for switch and port capabilities from the
			// relevant adapter.  In such a case prevent any request aimed at that logical device.
			logger.Debugf("Logical device %s is not ready to serve requests", logicalDeviceID)
			return nil
		}
		return lda
	}
	//	Try to load into memory - loading will also create the logical device agent
	if err := ldMgr.load(ctx, logicalDeviceID); err == nil {
		if agent, ok = ldMgr.logicalDeviceAgents.Load(logicalDeviceID); ok {
			return agent.(*LogicalAgent)
		}
	}
	return nil
}

func (ldMgr *LogicalManager) deleteLogicalDeviceAgent(logicalDeviceID string) {
	ldMgr.logicalDeviceAgents.Delete(logicalDeviceID)
}

// GetLogicalDevice provides a cloned most up to date logical device.  If device is not in memory
// it will be fetched from the dB
func (ldMgr *LogicalManager) GetLogicalDevice(ctx context.Context, id *voltha.ID) (*voltha.LogicalDevice, error) {
	logger.Debugw("getlogicalDevice", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id); agent != nil {
		return agent.GetLogicalDevice(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

//ListLogicalDevices returns the list of all logical devices
func (ldMgr *LogicalManager) ListLogicalDevices(ctx context.Context, _ *empty.Empty) (*voltha.LogicalDevices, error) {
	logger.Debug("ListAllLogicalDevices")

	var logicalDevices []*voltha.LogicalDevice
	if err := ldMgr.clusterDataProxy.List(ctx, "logical_devices", &logicalDevices); err != nil {
		logger.Errorw("failed-to-list-logical-devices-from-cluster-proxy", log.Fields{"error": err})
		return nil, err
	}

	ret := make(map[string]*voltha.LogicalDevice, len(logicalDevices))
	for _, ld := range logicalDevices {
		ret[ld.Id] = ld
	}
	return &voltha.LogicalDevices{Items: ret}, nil
}

func (ldMgr *LogicalManager) createLogicalDevice(ctx context.Context, device *voltha.Device) (*string, error) {
	logger.Debugw("creating-logical-device", log.Fields{"deviceId": device.Id})
	// Sanity check
	if !device.Root {
		return nil, errors.New("device-not-root")
	}

	// Create a logical device agent - the logical device Id is based on the mac address of the device
	// For now use the serial number - it may contain any combination of alphabetic characters and numbers,
	// with length varying from eight characters to a maximum of 14 characters.   Mac Address is part of oneof
	// in the Device model.  May need to be moved out.
	id := utils.CreateLogicalDeviceID()
	sn := strings.Replace(device.MacAddress, ":", "", -1)
	if id == "" {
		logger.Errorw("mac-address-not-set", log.Fields{"deviceId": device.Id, "serial-number": sn})
		return nil, errors.New("mac-address-not-set")
	}

	logger.Debugw("logical-device-id", log.Fields{"logicaldeviceId": id})

	agent := newLogicalDeviceAgent(id, sn, device.Id, ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy, ldMgr.defaultTimeout)
	ldMgr.addLogicalDeviceAgentToMap(agent)

	// Update the root device with the logical device Id reference
	if err := ldMgr.deviceMgr.setParentID(ctx, device, id); err != nil {
		logger.Errorw("failed-setting-parent-id", log.Fields{"logicalDeviceId": id, "deviceId": device.Id})
		return nil, err
	}

	go func() {
		//agent := newLogicalDeviceAgent(id, device.Id, ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy, ldMgr.defaultTimeout)
		err := agent.start(context.Background(), false)
		if err != nil {
			logger.Errorw("unable-to-create-the-logical-device", log.Fields{"error": err})
			ldMgr.deleteLogicalDeviceAgent(id)
		}
	}()

	logger.Debug("creating-logical-device-ends")
	return &id, nil
}

// stopManagingLogicalDeviceWithDeviceId stops the management of the logical device.  This implies removal of any
// reference of this logical device in cache.  The device Id is passed as param because the logical device may already
// have been removed from the model.  This function returns the logical device Id if found
func (ldMgr *LogicalManager) stopManagingLogicalDeviceWithDeviceID(ctx context.Context, id string) string {
	logger.Infow("stop-managing-logical-device", log.Fields{"deviceId": id})
	// Go over the list of logical device agents to find the one which has rootDeviceId as id
	var ldID = ""
	ldMgr.logicalDeviceAgents.Range(func(key, value interface{}) bool {
		ldAgent := value.(*LogicalAgent)
		if ldAgent.rootDeviceID == id {
			logger.Infow("stopping-logical-device-agent", log.Fields{"lDeviceId": key})
			if err := ldAgent.stop(ctx); err != nil {
				logger.Errorw("failed-to-stop-LDAgent", log.Fields{"error": err})
				return false
			}
			ldID = key.(string)
			ldMgr.logicalDeviceAgents.Delete(ldID)
		}
		return true
	})
	return ldID
}

//getLogicalDeviceFromModel retrieves the logical device data from the model.
func (ldMgr *LogicalManager) getLogicalDeviceFromModel(ctx context.Context, lDeviceID string) (*voltha.LogicalDevice, error) {
	logicalDevice := &voltha.LogicalDevice{}
	if have, err := ldMgr.clusterDataProxy.Get(ctx, "logical_devices/"+lDeviceID, logicalDevice); err != nil {
		logger.Errorw("failed-to-get-logical-devices-from-cluster-proxy", log.Fields{"error": err})
		return nil, err
	} else if !have {
		return nil, status.Error(codes.NotFound, lDeviceID)
	}

	return logicalDevice, nil
}

// load loads a logical device manager in memory
func (ldMgr *LogicalManager) load(ctx context.Context, lDeviceID string) error {
	if lDeviceID == "" {
		return nil
	}
	// Add a lock to prevent two concurrent calls from loading the same device twice
	ldMgr.logicalDevicesLoadingLock.Lock()
	if _, exist := ldMgr.logicalDeviceLoadingInProgress[lDeviceID]; !exist {
		if ldAgent, _ := ldMgr.logicalDeviceAgents.Load(lDeviceID); ldAgent == nil {
			ldMgr.logicalDeviceLoadingInProgress[lDeviceID] = []chan int{make(chan int, 1)}
			ldMgr.logicalDevicesLoadingLock.Unlock()
			if _, err := ldMgr.getLogicalDeviceFromModel(ctx, lDeviceID); err == nil {
				logger.Debugw("loading-logical-device", log.Fields{"lDeviceId": lDeviceID})
				agent := newLogicalDeviceAgent(lDeviceID, "", "", ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy, ldMgr.defaultTimeout)
				if err := agent.start(ctx, true); err != nil {
					return err
				}
				ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
			} else {
				logger.Debugw("logicalDevice not in model", log.Fields{"lDeviceId": lDeviceID})
			}
			// announce completion of task to any number of waiting channels
			ldMgr.logicalDevicesLoadingLock.Lock()
			if v, ok := ldMgr.logicalDeviceLoadingInProgress[lDeviceID]; ok {
				for _, ch := range v {
					close(ch)
				}
				delete(ldMgr.logicalDeviceLoadingInProgress, lDeviceID)
			}
			ldMgr.logicalDevicesLoadingLock.Unlock()
		} else {
			ldMgr.logicalDevicesLoadingLock.Unlock()
		}
	} else {
		ch := make(chan int, 1)
		ldMgr.logicalDeviceLoadingInProgress[lDeviceID] = append(ldMgr.logicalDeviceLoadingInProgress[lDeviceID], ch)
		ldMgr.logicalDevicesLoadingLock.Unlock()
		//	Wait for the channel to be closed, implying the process loading this device is done.
		<-ch
	}
	if _, exist := ldMgr.logicalDeviceAgents.Load(lDeviceID); exist {
		return nil
	}
	return status.Errorf(codes.Aborted, "Error loading logical device %s", lDeviceID)
}

func (ldMgr *LogicalManager) deleteLogicalDevice(ctx context.Context, device *voltha.Device) error {
	logger.Debugw("deleting-logical-device", log.Fields{"deviceId": device.Id})
	// Sanity check
	if !device.Root {
		return errors.New("device-not-root")
	}
	logDeviceID := device.ParentId
	if agent := ldMgr.getLogicalDeviceAgent(ctx, logDeviceID); agent != nil {
		// Stop the logical device agent
		if err := agent.stop(ctx); err != nil {
			logger.Errorw("failed-to-stop-agent", log.Fields{"error": err})
			return err
		}
		//Remove the logical device agent from the Map
		ldMgr.deleteLogicalDeviceAgent(logDeviceID)
	}

	logger.Debug("deleting-logical-device-ends")
	return nil
}

func (ldMgr *LogicalManager) getLogicalDeviceID(ctx context.Context, device *voltha.Device) (*string, error) {
	// Device can either be a parent or a child device
	if device.Root {
		// Parent device.  The ID of a parent device is the logical device ID
		return &device.ParentId, nil
	}
	// Device is child device
	//	retrieve parent device using child device ID
	if parentDevice := ldMgr.deviceMgr.getParentDevice(ctx, device); parentDevice != nil {
		return &parentDevice.ParentId, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", device.Id)
}

func (ldMgr *LogicalManager) getLogicalDeviceIDFromDeviceID(ctx context.Context, deviceID string) (*string, error) {
	// Get the device
	var device *voltha.Device
	var err error
	if device, err = ldMgr.deviceMgr.getDevice(ctx, deviceID); err != nil {
		return nil, err
	}
	return ldMgr.getLogicalDeviceID(ctx, device)
}

func (ldMgr *LogicalManager) getLogicalPortID(ctx context.Context, device *voltha.Device) (*voltha.LogicalPortId, error) {
	// Get the logical device where this device is attached
	var lDeviceID *string
	var err error
	if lDeviceID, err = ldMgr.getLogicalDeviceID(ctx, device); err != nil {
		return nil, err
	}
	var lDevice *voltha.LogicalDevice
	if lDevice, err = ldMgr.GetLogicalDevice(ctx, &voltha.ID{Id: *lDeviceID}); err != nil {
		return nil, err
	}
	// Go over list of ports
	for _, port := range lDevice.Ports {
		if port.DeviceId == device.Id {
			return &voltha.LogicalPortId{Id: *lDeviceID, PortId: port.Id}, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "%s", device.Id)
}

// ListLogicalDeviceFlows returns the flows of logical device
func (ldMgr *LogicalManager) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	logger.Debugw("ListLogicalDeviceFlows", log.Fields{"logicaldeviceid": id.Id})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id); agent != nil {
		return agent.ListLogicalDeviceFlows(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id.Id)
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (ldMgr *LogicalManager) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	logger.Debugw("ListLogicalDeviceFlowGroups", log.Fields{"logicaldeviceid": id.Id})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id); agent != nil {
		return agent.ListLogicalDeviceFlowGroups(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id.Id)
}

// ListLogicalDevicePorts returns logical device ports
func (ldMgr *LogicalManager) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	logger.Debugw("ListLogicalDevicePorts", log.Fields{"logicaldeviceid": id.Id})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id); agent != nil {
		return agent.ListLogicalDevicePorts(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id.Id)
}

// GetLogicalDevicePort returns logical device port details
func (ldMgr *LogicalManager) GetLogicalDevicePort(ctx context.Context, lPortID *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	// Get the logical device where this device is attached
	var err error
	var lDevice *voltha.LogicalDevice
	if lDevice, err = ldMgr.GetLogicalDevice(ctx, &voltha.ID{Id: lPortID.Id}); err != nil {
		return nil, err
	}
	// Go over list of ports
	for _, port := range lDevice.Ports {
		if port.Id == lPortID.PortId {
			return port, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "%s-%s", lPortID.Id, lPortID.PortId)
}

// updateLogicalPort sets up a logical port on the logical device based on the device port
// information, if needed
func (ldMgr *LogicalManager) updateLogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) error {
	ldID, err := ldMgr.getLogicalDeviceID(ctx, device)
	if err != nil || *ldID == "" {
		// This is not an error as the logical device may not have been created at this time.  In such a case,
		// the ports will be created when the logical device is ready.
		return nil
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.updateLogicalPort(ctx, device, port); err != nil {
			return err
		}
	}
	return nil
}

// deleteLogicalPort removes the logical port associated with a device
func (ldMgr *LogicalManager) deleteLogicalPort(ctx context.Context, lPortID *voltha.LogicalPortId) error {
	logger.Debugw("deleting-logical-port", log.Fields{"LDeviceId": lPortID.Id})
	// Get logical port
	var logicalPort *voltha.LogicalPort
	var err error
	if logicalPort, err = ldMgr.GetLogicalDevicePort(ctx, lPortID); err != nil {
		logger.Debugw("no-logical-device-port-present", log.Fields{"logicalPortId": lPortID.PortId})
		return err
	}
	// Sanity check
	if logicalPort.RootPort {
		return errors.New("device-root")
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, lPortID.Id); agent != nil {
		if err := agent.deleteLogicalPort(ctx, logicalPort); err != nil {
			logger.Warnw("deleting-logicalport-failed", log.Fields{"LDeviceId": lPortID.Id, "error": err})
		}
	}

	logger.Debug("deleting-logical-port-ends")
	return nil
}

// deleteLogicalPort removes the logical port associated with a child device
func (ldMgr *LogicalManager) deleteLogicalPorts(ctx context.Context, deviceID string) error {
	logger.Debugw("deleting-logical-ports", log.Fields{"device-id": deviceID})
	// Get logical port
	ldID, err := ldMgr.getLogicalDeviceIDFromDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err = agent.deleteLogicalPorts(ctx, deviceID); err != nil {
			logger.Warnw("delete-logical-ports-failed", log.Fields{"logical-device-id": *ldID})
			return err
		}
	}
	logger.Debug("deleting-logical-ports-ends")
	return nil
}

func (ldMgr *LogicalManager) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	logger.Debugw("setupUNILogicalPorts", log.Fields{"childDeviceId": childDevice.Id, "parentDeviceId": childDevice.ParentId, "current-data": childDevice})
	// Sanity check
	if childDevice.Root {
		return errors.New("Device-root")
	}

	// Get the logical device id parent device
	parentID := childDevice.ParentId
	logDeviceID := ldMgr.deviceMgr.GetParentDeviceID(ctx, parentID)

	logger.Debugw("setupUNILogicalPorts", log.Fields{"logDeviceId": logDeviceID, "parentId": parentID})

	if parentID == "" || logDeviceID == "" {
		return errors.New("device-in-invalid-state")
	}

	if agent := ldMgr.getLogicalDeviceAgent(ctx, logDeviceID); agent != nil {
		if err := agent.setupUNILogicalPorts(ctx, childDevice); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalManager) deleteAllLogicalPorts(ctx context.Context, device *voltha.Device) error {
	logger.Debugw("deleteAllLogicalPorts", log.Fields{"deviceId": device.Id})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceID(ctx, device); err != nil {
		logger.Warnw("no-logical-device-found", log.Fields{"deviceId": device.Id, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.deleteAllLogicalPorts(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalManager) deleteAllUNILogicalPorts(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw("delete-all-uni-logical-ports", log.Fields{"parent-device-id": parentDevice.Id})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceID(ctx, parentDevice); err != nil {
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.deleteAllUNILogicalPorts(ctx, parentDevice); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalManager) updatePortState(ctx context.Context, deviceID string, portNo uint32, state voltha.OperStatus_Types) error {
	logger.Debugw("updatePortState", log.Fields{"deviceId": deviceID, "state": state, "portNo": portNo})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceIDFromDeviceID(ctx, deviceID); err != nil {
		logger.Warnw("no-logical-device-found", log.Fields{"deviceId": deviceID, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.updatePortState(ctx, deviceID, portNo, state); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalManager) updatePortsState(ctx context.Context, device *voltha.Device, state voltha.OperStatus_Types) error {
	logger.Debugw("updatePortsState", log.Fields{"deviceId": device.Id, "state": state, "current-data": device})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceID(ctx, device); err != nil {
		logger.Warnw("no-logical-device-found", log.Fields{"deviceId": device.Id, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.updatePortsState(ctx, device, state); err != nil {
			return err
		}
	}
	return nil
}

// UpdateLogicalDeviceFlowTable updates logical device flow table
func (ldMgr *LogicalManager) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	logger.Debugw("UpdateLogicalDeviceFlowTable", log.Fields{"logicalDeviceId": flow.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, flow.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", flow.Id)
	}
	return &empty.Empty{}, agent.updateFlowTable(ctx, flow.FlowMod)
}

// UpdateLogicalDeviceMeterTable - This function sends meter mod request to logical device manager and waits for response
func (ldMgr *LogicalManager) UpdateLogicalDeviceMeterTable(ctx context.Context, meter *openflow_13.MeterModUpdate) (*empty.Empty, error) {
	logger.Debugw("UpdateLogicalDeviceMeterTable", log.Fields{"logicalDeviceId": meter.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, meter.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", meter.Id)
	}
	return &empty.Empty{}, agent.updateMeterTable(ctx, meter.MeterMod)
}

// ListLogicalDeviceMeters returns logical device meters
func (ldMgr *LogicalManager) ListLogicalDeviceMeters(ctx context.Context, id *voltha.ID) (*openflow_13.Meters, error) {
	logger.Debugw("ListLogicalDeviceMeters", log.Fields{"logicalDeviceId": id.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return agent.ListLogicalDeviceMeters(ctx)
}

// UpdateLogicalDeviceFlowGroupTable updates logical device flow group table
func (ldMgr *LogicalManager) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	logger.Debugw("UpdateGroupTable", log.Fields{"logicalDeviceId": flow.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, flow.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", flow.Id)
	}
	return &empty.Empty{}, agent.updateGroupTable(ctx, flow.GroupMod)
}

// EnableLogicalDevicePort enables logical device port
func (ldMgr *LogicalManager) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	logger.Debugw("EnableLogicalDevicePort", log.Fields{"logicalDeviceId": id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.enableLogicalPort(ctx, id.PortId)
}

// DisableLogicalDevicePort disables logical device port
func (ldMgr *LogicalManager) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	logger.Debugw("DisableLogicalDevicePort", log.Fields{"logicalDeviceId": id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.disableLogicalPort(ctx, id.PortId)
}

func (ldMgr *LogicalManager) packetIn(ctx context.Context, logicalDeviceID string, port uint32, transactionID string, packet []byte) error {
	logger.Debugw("packetIn", log.Fields{"logicalDeviceId": logicalDeviceID, "port": port})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, logicalDeviceID); agent != nil {
		agent.packetIn(port, transactionID, packet)
	} else {
		logger.Error("logical-device-not-exist", log.Fields{"logicalDeviceId": logicalDeviceID})
	}
	return nil
}

// StreamPacketsOut sends packets to adapter
func (ldMgr *LogicalManager) StreamPacketsOut(packets voltha.VolthaService_StreamPacketsOutServer) error {
	logger.Debugw("StreamPacketsOut-request", log.Fields{"packets": packets})
loop:
	for {
		select {
		case <-packets.Context().Done():
			logger.Infow("StreamPacketsOut-context-done", log.Fields{"packets": packets, "error": packets.Context().Err()})
			break loop
		default:
		}

		packet, err := packets.Recv()

		if err == io.EOF {
			logger.Debugw("Received-EOF", log.Fields{"packets": packets})
			break loop
		}

		if err != nil {
			logger.Errorw("Failed to receive packet out", log.Fields{"error": err})
			continue
		}

		if agent := ldMgr.getLogicalDeviceAgent(packets.Context(), packet.Id); agent != nil {
			agent.packetOut(packets.Context(), packet.PacketOut)
		} else {
			logger.Errorf("No logical device agent present", log.Fields{"logicalDeviceID": packet.Id})
		}
	}

	logger.Debugw("StreamPacketsOut-request-done", log.Fields{"packets": packets})
	return nil
}
