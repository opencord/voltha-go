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
	"github.com/opencord/voltha-lib-go/v5/pkg/probe"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/device/event"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v5/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v5/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalManager represent logical device manager attributes
type LogicalManager struct {
	*event.Manager
	logicalDeviceAgents            sync.Map
	deviceMgr                      *Manager
	kafkaICProxy                   kafka.InterContainerProxy
	dbPath                         *model.Path
	ldProxy                        *model.Proxy
	defaultTimeout                 time.Duration
	logicalDevicesLoadingLock      sync.RWMutex
	logicalDeviceLoadingInProgress map[string][]chan int
}

func (ldMgr *LogicalManager) Start(ctx context.Context) {
	logger.Info(ctx, "starting-logical-device-manager")
	probe.UpdateStatusFromContext(ctx, "logical-device-manager", probe.ServiceStatusPreparing)

	// Load all the logical devices from the dB
	var logicalDevices []*voltha.LogicalDevice
	if err := ldMgr.ldProxy.List(ctx, &logicalDevices); err != nil {
		logger.Fatalw(ctx, "failed-to-list-logical-devices-from-cluster-proxy", log.Fields{"error": err})
	}
	for _, lDevice := range logicalDevices {
		// Create an agent for each device
		agent := newLogicalAgent(ctx, lDevice.Id, "", "", ldMgr, ldMgr.deviceMgr, ldMgr.dbPath, ldMgr.ldProxy, ldMgr.defaultTimeout)
		if err := agent.start(ctx, true, lDevice); err != nil {
			logger.Warnw(ctx, "failure-starting-logical-agent", log.Fields{"logical-device-id": lDevice.Id})
		} else {
			ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
		}
	}

	probe.UpdateStatusFromContext(ctx, "logical-device-manager", probe.ServiceStatusRunning)
	logger.Info(ctx, "logical-device-manager-started")
}

func (ldMgr *LogicalManager) addLogicalDeviceAgentToMap(agent *LogicalAgent) {
	if _, exist := ldMgr.logicalDeviceAgents.Load(agent.logicalDeviceID); !exist {
		ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
	}
}

// getLogicalDeviceAgent returns the logical device agent.  If the device is not in memory then the device will
// be loaded from dB and a logical device agent created to managed it.
func (ldMgr *LogicalManager) getLogicalDeviceAgent(ctx context.Context, logicalDeviceID string) *LogicalAgent {
	logger.Debugw(ctx, "get-logical-device-agent", log.Fields{"logical-device-id": logicalDeviceID})
	agent, ok := ldMgr.logicalDeviceAgents.Load(logicalDeviceID)
	if ok {
		lda := agent.(*LogicalAgent)
		if lda.logicalDevice == nil {
			// This can happen when an agent for the logical device has been created but the logical device
			// itself is not ready for action as it is waiting for switch and port capabilities from the
			// relevant adapter.  In such a case prevent any request aimed at that logical device.
			logger.Debugf(ctx, "logical-device-%s-is-not-ready-to-serve-requests", logicalDeviceID)
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
	ctx = utils.WithRPCMetadataContext(ctx, "GetLogicalDevice")
	logger.Debugw(ctx, "get-logical-device", log.Fields{"logical-device-id": id})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id); agent != nil {
		return agent.GetLogicalDeviceReadOnly(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

//ListLogicalDevices returns the list of all logical devices
func (ldMgr *LogicalManager) ListLogicalDevices(ctx context.Context, _ *empty.Empty) (*voltha.LogicalDevices, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListLogicalDevices")
	logger.Debug(ctx, "list-all-logical-devices")

	var logicalDevices []*voltha.LogicalDevice
	ldMgr.logicalDeviceAgents.Range(func(key, value interface{}) bool {
		if ld, err := value.(*LogicalAgent).GetLogicalDeviceReadOnly(ctx); err == nil {
			logicalDevices = append(logicalDevices, ld)
		} else {
			logger.Errorw(ctx, "unable-to-get-logical-device", log.Fields{"err": err})
		}
		return true
	})
	logger.Debugw(ctx, "list-all-logical-devices", log.Fields{"num-logical-devices": len(logicalDevices)})

	return &voltha.LogicalDevices{Items: logicalDevices}, nil
}

func (ldMgr *LogicalManager) createLogicalDevice(ctx context.Context, device *voltha.Device) (*string, error) {
	logger.Debugw(ctx, "creating-logical-device", log.Fields{"device-id": device.Id})
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
		logger.Errorw(ctx, "mac-address-not-set", log.Fields{"device-id": device.Id, "serial-number": sn})
		return nil, errors.New("mac-address-not-set")
	}

	logger.Debugw(ctx, "logical-device-id", log.Fields{"logical-device-id": id})

	agent := newLogicalAgent(ctx, id, sn, device.Id, ldMgr, ldMgr.deviceMgr, ldMgr.dbPath, ldMgr.ldProxy, ldMgr.defaultTimeout)
	ldMgr.addLogicalDeviceAgentToMap(agent)

	// Update the root device with the logical device Id reference
	if err := ldMgr.deviceMgr.setParentID(ctx, device, id); err != nil {
		logger.Errorw(ctx, "failed-setting-parent-id", log.Fields{"logical-device-id": id, "device-id": device.Id})
		return nil, err
	}

	go func() {
		//TODO: either wait for the agent to be started before returning, or
		//      implement locks in the agent to ensure request are not processed before start() is complete
		ldCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)
		err := agent.start(ldCtx, false, nil)
		if err != nil {
			logger.Errorw(ctx, "unable-to-create-the-logical-device", log.Fields{"error": err})
			ldMgr.deleteLogicalDeviceAgent(id)
		}
	}()

	logger.Debug(ctx, "creating-logical-device-ends")
	return &id, nil
}

// stopManagingLogicalDeviceWithDeviceId stops the management of the logical device.  This implies removal of any
// reference of this logical device in cache.  The device Id is passed as param because the logical device may already
// have been removed from the model.  This function returns the logical device Id if found
func (ldMgr *LogicalManager) stopManagingLogicalDeviceWithDeviceID(ctx context.Context, id string) string {
	logger.Infow(ctx, "stop-managing-logical-device", log.Fields{"device-id": id})
	// Go over the list of logical device agents to find the one which has rootDeviceId as id
	var ldID = ""
	ldMgr.logicalDeviceAgents.Range(func(key, value interface{}) bool {
		ldAgent := value.(*LogicalAgent)
		if ldAgent.rootDeviceID == id {
			logger.Infow(ctx, "stopping-logical-device-agent", log.Fields{"logical-device-id": key})
			if err := ldAgent.stop(ctx); err != nil {
				logger.Errorw(ctx, "failed-to-stop-LDAgent", log.Fields{"error": err})
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
	if have, err := ldMgr.ldProxy.Get(ctx, lDeviceID, logicalDevice); err != nil {
		logger.Errorw(ctx, "failed-to-get-logical-devices-from-cluster-proxy", log.Fields{"error": err})
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
				logger.Debugw(ctx, "loading-logical-device", log.Fields{"lDeviceId": lDeviceID})
				agent := newLogicalAgent(ctx, lDeviceID, "", "", ldMgr, ldMgr.deviceMgr, ldMgr.dbPath, ldMgr.ldProxy, ldMgr.defaultTimeout)
				if err := agent.start(ctx, true, nil); err != nil {
					return err
				}
				ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
			} else {
				logger.Debugw(ctx, "logical-device-not-in-model", log.Fields{"logical-device-id": lDeviceID})
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
	logger.Debugw(ctx, "deleting-logical-device", log.Fields{"device-id": device.Id})
	// Sanity check
	if !device.Root {
		return errors.New("device-not-root")
	}
	logDeviceID := device.ParentId
	if agent := ldMgr.getLogicalDeviceAgent(ctx, logDeviceID); agent != nil {
		// Stop the logical device agent
		if err := agent.stop(ctx); err != nil {
			logger.Errorw(ctx, "failed-to-stop-agent", log.Fields{"error": err})
			return err
		}
		//Remove the logical device agent from the Map
		ldMgr.deleteLogicalDeviceAgent(logDeviceID)
	}

	logger.Debug(ctx, "deleting-logical-device-ends")
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
	// TODO: return (string, have) instead of *string
	//       also: If not root device, just return device.parentID instead of loading the parent device.
	if parentDevice := ldMgr.deviceMgr.getParentDevice(ctx, device); parentDevice != nil {
		return &parentDevice.ParentId, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", device.Id)
}

func (ldMgr *LogicalManager) getLogicalDeviceIDFromDeviceID(ctx context.Context, deviceID string) (*string, error) {
	// Get the device
	var device *voltha.Device
	var err error
	if device, err = ldMgr.deviceMgr.getDeviceReadOnly(ctx, deviceID); err != nil {
		return nil, err
	}
	return ldMgr.getLogicalDeviceID(ctx, device)
}

// ListLogicalDeviceFlows returns the flows of logical device
func (ldMgr *LogicalManager) ListLogicalDeviceFlows(ctx context.Context, id *voltha.ID) (*openflow_13.Flows, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListLogicalDeviceFlows")
	logger.Debugw(ctx, "list-logical-device-flows", log.Fields{"logical-device-id": id.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}

	flows := agent.listLogicalDeviceFlows()
	ctr, ret := 0, make([]*openflow_13.OfpFlowStats, len(flows))
	for _, flow := range flows {
		ret[ctr] = flow
		ctr++
	}
	logger.Debugw(ctx, "list-logical-device-flows", log.Fields{"logical-device-id": id.Id, "num-flows": len(flows)})
	return &openflow_13.Flows{Items: ret}, nil
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (ldMgr *LogicalManager) ListLogicalDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*openflow_13.FlowGroups, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListLogicalDeviceFlowGroups")
	logger.Debugw(ctx, "list-logical-device-flow-groups", log.Fields{"logical-device-id": id.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}

	groups := agent.listLogicalDeviceGroups()
	ctr, ret := 0, make([]*openflow_13.OfpGroupEntry, len(groups))
	for _, group := range groups {
		ret[ctr] = group
		ctr++
	}
	logger.Debugw(ctx, "list-logical-device-flow-groups", log.Fields{"logical-device-id": id.Id, "num-groups": len(groups)})
	return &openflow_13.FlowGroups{Items: ret}, nil
}

// ListLogicalDevicePorts returns logical device ports
func (ldMgr *LogicalManager) ListLogicalDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.LogicalPorts, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListLogicalDevicePorts")
	logger.Debugw(ctx, "list-logical-device-ports", log.Fields{"logical-device-id": id.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}

	ports := agent.listLogicalDevicePorts(ctx)
	ctr, ret := 0, make([]*voltha.LogicalPort, len(ports))
	for _, port := range ports {
		ret[ctr] = port
		ctr++
	}
	logger.Debugw(ctx, "list-logical-device-ports", log.Fields{"logical-device-id": id.Id, "num-ports": len(ports)})
	return &voltha.LogicalPorts{Items: ret}, nil
}

// GetLogicalDevicePort returns logical device port details
func (ldMgr *LogicalManager) GetLogicalDevicePort(ctx context.Context, lPortID *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetLogicalDevicePort")
	// Get the logical device where this port is attached
	agent := ldMgr.getLogicalDeviceAgent(ctx, lPortID.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", lPortID.Id)
	}

	for _, port := range agent.listLogicalDevicePorts(ctx) {
		if port.Id == lPortID.PortId {
			return port, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "%s-%s", lPortID.Id, lPortID.PortId)
}

// updateLogicalPort sets up a logical port on the logical device based on the device port
// information, if needed
func (ldMgr *LogicalManager) updateLogicalPort(ctx context.Context, device *voltha.Device, devicePorts map[uint32]*voltha.Port, port *voltha.Port) error {
	ldID, err := ldMgr.getLogicalDeviceID(ctx, device)
	if err != nil || *ldID == "" {
		// This is not an error as the logical device may not have been created at this time.  In such a case,
		// the ports will be created when the logical device is ready.
		return nil
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.updateLogicalPort(ctx, device, devicePorts, port); err != nil {
			return err
		}
	}
	return nil
}

// deleteLogicalPort removes the logical port associated with a child device
func (ldMgr *LogicalManager) deleteLogicalPorts(ctx context.Context, deviceID string) error {
	logger.Debugw(ctx, "deleting-logical-ports", log.Fields{"device-id": deviceID})
	// Get logical port
	ldID, err := ldMgr.getLogicalDeviceIDFromDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err = agent.deleteLogicalPorts(ctx, deviceID); err != nil {
			logger.Warnw(ctx, "delete-logical-ports-failed", log.Fields{"logical-device-id": *ldID})
			return err
		}
	}
	logger.Debug(ctx, "deleting-logical-ports-ends")
	return nil
}

func (ldMgr *LogicalManager) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device, childDevicePorts map[uint32]*voltha.Port) error {
	logger.Debugw(ctx, "setup-uni-logical-ports", log.Fields{"child-device-id": childDevice.Id, "parent-device-id": childDevice.ParentId, "current-data": childDevice})
	// Sanity check
	if childDevice.Root {
		return errors.New("Device-root")
	}

	// Get the logical device id parent device
	parentID := childDevice.ParentId
	logDeviceID := ldMgr.deviceMgr.GetParentDeviceID(ctx, parentID)

	logger.Debugw(ctx, "setup-uni-logical-ports", log.Fields{"logical-device-id": logDeviceID, "parentId": parentID})

	if parentID == "" || logDeviceID == "" {
		return errors.New("device-in-invalid-state")
	}

	if agent := ldMgr.getLogicalDeviceAgent(ctx, logDeviceID); agent != nil {
		if err := agent.setupUNILogicalPorts(ctx, childDevice, childDevicePorts); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalManager) deleteAllLogicalPorts(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-logical-ports", log.Fields{"device-id": device.Id})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceID(ctx, device); err != nil {
		logger.Warnw(ctx, "no-logical-device-found", log.Fields{"device-id": device.Id, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.deleteAllLogicalPorts(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalManager) updatePortState(ctx context.Context, deviceID string, portNo uint32, state voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "update-Port-state", log.Fields{"device-id": deviceID, "state": state, "port-no": portNo})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceIDFromDeviceID(ctx, deviceID); err != nil {
		logger.Warnw(ctx, "no-logical-device-found", log.Fields{"device-id": deviceID, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(ctx, *ldID); agent != nil {
		if err := agent.updatePortState(ctx, portNo, state); err != nil {
			return err
		}
	}
	return nil
}

// UpdateLogicalDeviceFlowTable updates logical device flow table
func (ldMgr *LogicalManager) UpdateLogicalDeviceFlowTable(ctx context.Context, flow *openflow_13.FlowTableUpdate) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "UpdateLogicalDeviceFlowTable")
	logger.Debugw(ctx, "update-logical-device-flow-table", log.Fields{"logical-device-id": flow.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, flow.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", flow.Id)
	}
	return &empty.Empty{}, agent.updateFlowTable(ctx, flow)
}

// UpdateLogicalDeviceMeterTable - This function sends meter mod request to logical device manager and waits for response
func (ldMgr *LogicalManager) UpdateLogicalDeviceMeterTable(ctx context.Context, meter *openflow_13.MeterModUpdate) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "UpdateLogicalDeviceMeterTable")
	logger.Debugw(ctx, "update-logical-device-meter-table", log.Fields{"logical-device-id": meter.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, meter.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", meter.Id)
	}
	return &empty.Empty{}, agent.updateMeterTable(ctx, meter.MeterMod)
}

// ListLogicalDeviceMeters returns logical device meters
func (ldMgr *LogicalManager) ListLogicalDeviceMeters(ctx context.Context, id *voltha.ID) (*openflow_13.Meters, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListLogicalDeviceMeters")
	logger.Debugw(ctx, "list-logical-device-meters", log.Fields{"logical-device-id": id.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	meters := agent.listLogicalDeviceMeters()
	ctr, ret := 0, make([]*openflow_13.OfpMeterEntry, len(meters))
	for _, meter := range meters {
		ret[ctr] = meter
		ctr++
	}
	return &openflow_13.Meters{Items: ret}, nil
}

// UpdateLogicalDeviceFlowGroupTable updates logical device flow group table
func (ldMgr *LogicalManager) UpdateLogicalDeviceFlowGroupTable(ctx context.Context, flow *openflow_13.FlowGroupTableUpdate) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "UpdateLogicalDeviceFlowGroupTable")
	logger.Debugw(ctx, "update-group-table", log.Fields{"logical-device-id": flow.Id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, flow.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", flow.Id)
	}
	return &empty.Empty{}, agent.updateGroupTable(ctx, flow.GroupMod)
}

// EnableLogicalDevicePort enables logical device port
func (ldMgr *LogicalManager) EnableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "EnableLogicalDevicePort")
	logger.Debugw(ctx, "enable-logical-device-port", log.Fields{"logical-device-id": id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	portNo, err := strconv.ParseUint(id.PortId, 10, 32)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse %s as a number", id.PortId)
	}
	return &empty.Empty{}, agent.enableLogicalPort(ctx, uint32(portNo))
}

// DisableLogicalDevicePort disables logical device port
func (ldMgr *LogicalManager) DisableLogicalDevicePort(ctx context.Context, id *voltha.LogicalPortId) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DisableLogicalDevicePort")
	logger.Debugw(ctx, "disable-logical-device-port", log.Fields{"logical-device-id": id})
	agent := ldMgr.getLogicalDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	portNo, err := strconv.ParseUint(id.PortId, 10, 32)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse %s as a number", id.PortId)
	}
	return &empty.Empty{}, agent.disableLogicalPort(ctx, uint32(portNo))
}

func (ldMgr *LogicalManager) packetIn(ctx context.Context, logicalDeviceID string, port uint32, transactionID string, packet []byte) error {
	logger.Debugw(ctx, "packet-in", log.Fields{"logical-device-id": logicalDeviceID, "port": port})
	if agent := ldMgr.getLogicalDeviceAgent(ctx, logicalDeviceID); agent != nil {
		agent.packetIn(ctx, port, transactionID, packet)
	} else {
		logger.Error(ctx, "logical-device-not-exist", log.Fields{"logical-device-id": logicalDeviceID})
	}
	return nil
}

// StreamPacketsOut sends packets to adapter
func (ldMgr *LogicalManager) StreamPacketsOut(packets voltha.VolthaService_StreamPacketsOutServer) error {
	ctx := context.Background()
	logger.Debugw(ctx, "stream-packets-out-request", log.Fields{"packets": packets})

loop:
	for {
		select {
		case <-packets.Context().Done():
			logger.Infow(ctx, "stream-packets-out-context-done", log.Fields{"packets": packets, "error": packets.Context().Err()})
			break loop
		default:
		}

		packet, err := packets.Recv()

		pktCtx := utils.WithRPCMetadataContext(packets.Context(), "StreamPacketsOut")

		if err == io.EOF {
			logger.Debugw(ctx, "received-eof", log.Fields{"packets": packets})
			break loop
		}
		if err != nil {
			logger.Errorw(ctx, "failed-to-receive-packet-out", log.Fields{"error": err})
			// we do not have the resource Id here due to error in the packet, setting to empty
			ldMgr.SendRPCEvent(pktCtx, "", err.Error(), nil,
				"RPC_ERROR_RAISE_EVENT", voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())
			continue
		}

		if agent := ldMgr.getLogicalDeviceAgent(pktCtx, packet.Id); agent != nil {
			agent.packetOut(pktCtx, packet.PacketOut)
		} else {
			logger.Errorf(ctx, "no-logical-device-agent-present", log.Fields{"logical-device-id": packet.Id})
		}
	}

	logger.Debugw(ctx, "stream-packets-out-request-done", log.Fields{"packets": packets})
	return nil
}

func (ldMgr *LogicalManager) SendRPCEvent(ctx context.Context, resourceID, desc string, context map[string]string,
	id string, category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	ldMgr.Manager.Agent.GetAndSendRPCEvent(ctx, resourceID, desc, context, id,
		category, subCategory, raisedTs)
}
