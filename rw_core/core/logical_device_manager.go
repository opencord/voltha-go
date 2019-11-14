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

package core

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/probe"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalDeviceManager represent logical device manager attributes
type LogicalDeviceManager struct {
	logicalDeviceAgents            sync.Map
	core                           *Core
	deviceMgr                      *DeviceManager
	grpcNbiHdlr                    *APIHandler
	kafkaICProxy                   *kafka.InterContainerProxy
	clusterDataProxy               *model.Proxy
	exitChannel                    chan int
	defaultTimeout                 int64
	logicalDevicesLoadingLock      sync.RWMutex
	logicalDeviceLoadingInProgress map[string][]chan int
}

func newLogicalDeviceManager(core *Core, deviceMgr *DeviceManager, kafkaICProxy *kafka.InterContainerProxy, cdProxy *model.Proxy, timeout int64) *LogicalDeviceManager {
	var logicalDeviceMgr LogicalDeviceManager
	logicalDeviceMgr.core = core
	logicalDeviceMgr.exitChannel = make(chan int, 1)
	logicalDeviceMgr.deviceMgr = deviceMgr
	logicalDeviceMgr.kafkaICProxy = kafkaICProxy
	logicalDeviceMgr.clusterDataProxy = cdProxy
	logicalDeviceMgr.defaultTimeout = timeout
	logicalDeviceMgr.logicalDevicesLoadingLock = sync.RWMutex{}
	logicalDeviceMgr.logicalDeviceLoadingInProgress = make(map[string][]chan int)
	return &logicalDeviceMgr
}

func (ldMgr *LogicalDeviceManager) setGrpcNbiHandler(grpcNbiHandler *APIHandler) {
	ldMgr.grpcNbiHdlr = grpcNbiHandler
}

func (ldMgr *LogicalDeviceManager) start(ctx context.Context) {
	log.Info("starting-logical-device-manager")
	probe.UpdateStatusFromContext(ctx, "logical-device-manager", probe.ServiceStatusRunning)
	log.Info("logical-device-manager-started")
}

func (ldMgr *LogicalDeviceManager) stop(ctx context.Context) {
	log.Info("stopping-logical-device-manager")
	ldMgr.exitChannel <- 1
	probe.UpdateStatusFromContext(ctx, "logical-device-manager", probe.ServiceStatusStopped)
	log.Info("logical-device-manager-stopped")
}

func sendAPIResponse(ctx context.Context, ch chan interface{}, result interface{}) {
	if ctx.Err() == nil {
		// Returned response only of the ctx has not been cancelled/timeout/etc
		// Channel is automatically closed when a context is Done
		ch <- result
		log.Debugw("sendResponse", log.Fields{"result": result})
	} else {
		// Should the transaction be reverted back?
		log.Debugw("sendResponse-context-error", log.Fields{"context-error": ctx.Err()})
	}
}

func (ldMgr *LogicalDeviceManager) addLogicalDeviceAgentToMap(agent *LogicalDeviceAgent) {
	if _, exist := ldMgr.logicalDeviceAgents.Load(agent.logicalDeviceID); !exist {
		ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
	}
}

// getLogicalDeviceAgent returns the logical device agent.  If the device is not in memory then the device will
// be loaded from dB and a logical device agent created to managed it.
func (ldMgr *LogicalDeviceManager) getLogicalDeviceAgent(logicalDeviceID string) *LogicalDeviceAgent {
	agent, ok := ldMgr.logicalDeviceAgents.Load(logicalDeviceID)
	if ok {
		return agent.(*LogicalDeviceAgent)
	}
	//	Try to load into memory - loading will also create the logical device agent
	if err := ldMgr.load(logicalDeviceID); err == nil {
		if agent, ok = ldMgr.logicalDeviceAgents.Load(logicalDeviceID); ok {
			return agent.(*LogicalDeviceAgent)
		}
	}
	return nil
}

func (ldMgr *LogicalDeviceManager) deleteLogicalDeviceAgent(logicalDeviceID string) {
	ldMgr.logicalDeviceAgents.Delete(logicalDeviceID)
}

// GetLogicalDevice provides a cloned most up to date logical device.  If device is not in memory
// it will be fetched from the dB
func (ldMgr *LogicalDeviceManager) getLogicalDevice(id string) (*voltha.LogicalDevice, error) {
	log.Debugw("getlogicalDevice", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.GetLogicalDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (ldMgr *LogicalDeviceManager) listManagedLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("listManagedLogicalDevices")
	result := &voltha.LogicalDevices{}
	ldMgr.logicalDeviceAgents.Range(func(key, value interface{}) bool {
		agent := value.(*LogicalDeviceAgent)
		if ld, _ := agent.GetLogicalDevice(); ld != nil {
			result.Items = append(result.Items, ld)
		}
		return true
	})

	return result, nil
}

//listLogicalDevices returns the list of all logical devices
func (ldMgr *LogicalDeviceManager) listLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("ListAllLogicalDevices")
	result := &voltha.LogicalDevices{}
	if logicalDevices := ldMgr.clusterDataProxy.List(context.Background(), "/logical_devices", 0, true, ""); logicalDevices != nil {
		for _, logicalDevice := range logicalDevices.([]interface{}) {
			result.Items = append(result.Items, logicalDevice.(*voltha.LogicalDevice))
		}
	}
	return result, nil
}

func (ldMgr *LogicalDeviceManager) createLogicalDevice(ctx context.Context, device *voltha.Device) (*string, error) {
	log.Debugw("creating-logical-device", log.Fields{"deviceId": device.Id})
	// Sanity check
	if !device.Root {
		return nil, errors.New("device-not-root")
	}

	// Create a logical device agent - the logical device Id is based on the mac address of the device
	// For now use the serial number - it may contain any combination of alphabetic characters and numbers,
	// with length varying from eight characters to a maximum of 14 characters.   Mac Address is part of oneof
	// in the Device model.  May need to be moved out.
	macAddress := device.MacAddress
	id := strings.Replace(macAddress, ":", "", -1)
	if id == "" {
		log.Errorw("mac-address-not-set", log.Fields{"deviceId": device.Id})
		return nil, errors.New("mac-address-not-set")
	}
	log.Debugw("logical-device-id", log.Fields{"logicaldeviceId": id})

	agent := newLogicalDeviceAgent(id, device.Id, ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy, ldMgr.defaultTimeout)
	ldMgr.addLogicalDeviceAgentToMap(agent)

	// Update the root device with the logical device Id reference
	if err := ldMgr.deviceMgr.setParentID(device, id); err != nil {
		log.Errorw("failed-setting-parent-id", log.Fields{"logicalDeviceId": id, "deviceId": device.Id})
		return nil, err
	}

	go func() {
		err := agent.start(ctx, false)
		if err != nil {
			log.Errorw("unable-to-create-the-logical-device", log.Fields{"error": err})
		}
	}()

	log.Debug("creating-logical-device-ends")
	return &id, nil
}

// stopManagingLogicalDeviceWithDeviceId stops the management of the logical device.  This implies removal of any
// reference of this logical device in cache.  The device Id is passed as param because the logical device may already
// have been removed from the model.  This function returns the logical device Id if found
func (ldMgr *LogicalDeviceManager) stopManagingLogicalDeviceWithDeviceID(id string) string {
	log.Infow("stop-managing-logical-device", log.Fields{"deviceId": id})
	// Go over the list of logical device agents to find the one which has rootDeviceId as id
	var ldID = ""
	ldMgr.logicalDeviceAgents.Range(func(key, value interface{}) bool {
		ldAgent := value.(*LogicalDeviceAgent)
		if ldAgent.rootDeviceID == id {
			log.Infow("stopping-logical-device-agent", log.Fields{"lDeviceId": key})
			ldAgent.stop(context.TODO())
			ldID = key.(string)
			ldMgr.logicalDeviceAgents.Delete(ldID)
		}
		return true
	})
	return ldID
}

//getLogicalDeviceFromModel retrieves the logical device data from the model.
func (ldMgr *LogicalDeviceManager) getLogicalDeviceFromModel(lDeviceID string) (*voltha.LogicalDevice, error) {
	if logicalDevice := ldMgr.clusterDataProxy.Get(context.Background(), "/logical_devices/"+lDeviceID, 0, false, ""); logicalDevice != nil {
		if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
			return lDevice, nil
		}
	}
	return nil, status.Error(codes.NotFound, lDeviceID)
}

// load loads a logical device manager in memory
func (ldMgr *LogicalDeviceManager) load(lDeviceID string) error {
	if lDeviceID == "" {
		return nil
	}
	// Add a lock to prevent two concurrent calls from loading the same device twice
	ldMgr.logicalDevicesLoadingLock.Lock()
	if _, exist := ldMgr.logicalDeviceLoadingInProgress[lDeviceID]; !exist {
		if ldAgent, _ := ldMgr.logicalDeviceAgents.Load(lDeviceID); ldAgent == nil {
			ldMgr.logicalDeviceLoadingInProgress[lDeviceID] = []chan int{make(chan int, 1)}
			ldMgr.logicalDevicesLoadingLock.Unlock()
			if _, err := ldMgr.getLogicalDeviceFromModel(lDeviceID); err == nil {
				log.Debugw("loading-logical-device", log.Fields{"lDeviceId": lDeviceID})
				agent := newLogicalDeviceAgent(lDeviceID, "", ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy, ldMgr.defaultTimeout)
				if err := agent.start(context.TODO(), true); err != nil {
					agent.stop(context.TODO())
				} else {
					ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceID, agent)
				}
			} else {
				log.Debugw("logicalDevice not in model", log.Fields{"lDeviceId": lDeviceID})
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

func (ldMgr *LogicalDeviceManager) deleteLogicalDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("deleting-logical-device", log.Fields{"deviceId": device.Id})
	// Sanity check
	if !device.Root {
		return errors.New("device-not-root")
	}
	logDeviceID := device.ParentId
	if agent := ldMgr.getLogicalDeviceAgent(logDeviceID); agent != nil {
		// Stop the logical device agent
		agent.stop(ctx)
		//Remove the logical device agent from the Map
		ldMgr.deleteLogicalDeviceAgent(logDeviceID)
		err := ldMgr.core.deviceOwnership.AbandonDevice(logDeviceID)
		if err != nil {
			log.Errorw("unable-to-abandon-the-device", log.Fields{"error": err})
		}
	}

	log.Debug("deleting-logical-device-ends")
	return nil
}

func (ldMgr *LogicalDeviceManager) getLogicalDeviceID(device *voltha.Device) (*string, error) {
	// Device can either be a parent or a child device
	if device.Root {
		// Parent device.  The ID of a parent device is the logical device ID
		return &device.ParentId, nil
	}
	// Device is child device
	//	retrieve parent device using child device ID
	if parentDevice := ldMgr.deviceMgr.getParentDevice(device); parentDevice != nil {
		return &parentDevice.ParentId, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", device.Id)
}

func (ldMgr *LogicalDeviceManager) getLogicalDeviceIDFromDeviceID(deviceID string) (*string, error) {
	// Get the device
	var device *voltha.Device
	var err error
	if device, err = ldMgr.deviceMgr.GetDevice(deviceID); err != nil {
		return nil, err
	}
	return ldMgr.getLogicalDeviceID(device)
}

func (ldMgr *LogicalDeviceManager) getLogicalPortID(device *voltha.Device) (*voltha.LogicalPortId, error) {
	// Get the logical device where this device is attached
	var lDeviceID *string
	var err error
	if lDeviceID, err = ldMgr.getLogicalDeviceID(device); err != nil {
		return nil, err
	}
	var lDevice *voltha.LogicalDevice
	if lDevice, err = ldMgr.getLogicalDevice(*lDeviceID); err != nil {
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
func (ldMgr *LogicalDeviceManager) ListLogicalDeviceFlows(ctx context.Context, id string) (*openflow_13.Flows, error) {
	log.Debugw("ListLogicalDeviceFlows", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDeviceFlows()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (ldMgr *LogicalDeviceManager) ListLogicalDeviceFlowGroups(ctx context.Context, id string) (*openflow_13.FlowGroups, error) {
	log.Debugw("ListLogicalDeviceFlowGroups", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDeviceFlowGroups()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

// ListLogicalDevicePorts returns logical device ports
func (ldMgr *LogicalDeviceManager) ListLogicalDevicePorts(ctx context.Context, id string) (*voltha.LogicalPorts, error) {
	log.Debugw("ListLogicalDevicePorts", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDevicePorts()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (ldMgr *LogicalDeviceManager) getLogicalPort(lPortID *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	// Get the logical device where this device is attached
	var err error
	var lDevice *voltha.LogicalDevice
	if lDevice, err = ldMgr.getLogicalDevice(lPortID.Id); err != nil {
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
func (ldMgr *LogicalDeviceManager) updateLogicalPort(device *voltha.Device, port *voltha.Port) error {
	ldID, err := ldMgr.getLogicalDeviceID(device)
	if err != nil || *ldID == "" {
		// This is not an error as the logical device may not have been created at this time.  In such a case,
		// the ports will be created when the logical device is ready.
		return nil
	}
	if agent := ldMgr.getLogicalDeviceAgent(*ldID); agent != nil {
		if err := agent.updateLogicalPort(device, port); err != nil {
			return err
		}
	}
	return nil
}

// deleteLogicalPort removes the logical port associated with a device
func (ldMgr *LogicalDeviceManager) deleteLogicalPort(ctx context.Context, lPortID *voltha.LogicalPortId) error {
	log.Debugw("deleting-logical-port", log.Fields{"LDeviceId": lPortID.Id})
	// Get logical port
	var logicalPort *voltha.LogicalPort
	var err error
	if logicalPort, err = ldMgr.getLogicalPort(lPortID); err != nil {
		log.Debugw("no-logical-device-port-present", log.Fields{"logicalPortId": lPortID.PortId})
		return err
	}
	// Sanity check
	if logicalPort.RootPort {
		return errors.New("device-root")
	}
	if agent := ldMgr.getLogicalDeviceAgent(lPortID.Id); agent != nil {
		if err := agent.deleteLogicalPort(logicalPort); err != nil {
			log.Warnw("deleting-logicalport-failed", log.Fields{"LDeviceId": lPortID.Id, "error": err})
		}
	}

	log.Debug("deleting-logical-port-ends")
	return nil
}

// deleteLogicalPort removes the logical port associated with a child device
func (ldMgr *LogicalDeviceManager) deleteLogicalPorts(deviceID string) error {
	log.Debugw("deleting-logical-ports", log.Fields{"deviceId": deviceID})
	// Get logical port
	ldID, err := ldMgr.getLogicalDeviceIDFromDeviceID(deviceID)
	if err != nil {
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(*ldID); agent != nil {
		if err = agent.deleteLogicalPorts(deviceID); err != nil {
			log.Warnw("deleteLogicalPorts-failed", log.Fields{"ldeviceId": *ldID})
			return err
		}
	}
	log.Debug("deleting-logical-port-ends")
	return nil
}

func (ldMgr *LogicalDeviceManager) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	log.Debugw("setupUNILogicalPorts", log.Fields{"childDeviceId": childDevice.Id, "parentDeviceId": childDevice.ParentId, "current-data": childDevice})
	// Sanity check
	if childDevice.Root {
		return errors.New("Device-root")
	}

	// Get the logical device id parent device
	parentID := childDevice.ParentId
	logDeviceID := ldMgr.deviceMgr.GetParentDeviceID(parentID)

	log.Debugw("setupUNILogicalPorts", log.Fields{"logDeviceId": logDeviceID, "parentId": parentID})

	if parentID == "" || logDeviceID == "" {
		return errors.New("device-in-invalid-state")
	}

	if agent := ldMgr.getLogicalDeviceAgent(logDeviceID); agent != nil {
		if err := agent.setupUNILogicalPorts(ctx, childDevice); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalDeviceManager) deleteAllLogicalPorts(device *voltha.Device) error {
	log.Debugw("deleteAllLogicalPorts", log.Fields{"deviceId": device.Id})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceID(device); err != nil {
		log.Warnw("no-logical-device-found", log.Fields{"deviceId": device.Id, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(*ldID); agent != nil {
		if err := agent.deleteAllLogicalPorts(device); err != nil {
			return err
		}
	}
	return nil
}
func (ldMgr *LogicalDeviceManager) updatePortState(deviceID string, portNo uint32, state voltha.OperStatus_OperStatus) error {
	log.Debugw("updatePortState", log.Fields{"deviceId": deviceID, "state": state, "portNo": portNo})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceIDFromDeviceID(deviceID); err != nil {
		log.Warnw("no-logical-device-found", log.Fields{"deviceId": deviceID, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(*ldID); agent != nil {
		if err := agent.updatePortState(deviceID, portNo, state); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalDeviceManager) updatePortsState(device *voltha.Device, state voltha.AdminState_AdminState) error {
	log.Debugw("updatePortsState", log.Fields{"deviceId": device.Id, "state": state, "current-data": device})

	var ldID *string
	var err error
	//Get the logical device Id for this device
	if ldID, err = ldMgr.getLogicalDeviceID(device); err != nil {
		log.Warnw("no-logical-device-found", log.Fields{"deviceId": device.Id, "error": err})
		return err
	}
	if agent := ldMgr.getLogicalDeviceAgent(*ldID); agent != nil {
		if err := agent.updatePortsState(device, state); err != nil {
			return err
		}
	}
	return nil
}

func (ldMgr *LogicalDeviceManager) updateFlowTable(ctx context.Context, id string, flow *openflow_13.OfpFlowMod, ch chan interface{}) {
	log.Debugw("updateFlowTable", log.Fields{"logicalDeviceId": id})
	var res interface{}
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		res = agent.updateFlowTable(ctx, flow)
		log.Debugw("updateFlowTable-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id)
	}
	sendAPIResponse(ctx, ch, res)
}

func (ldMgr *LogicalDeviceManager) updateMeterTable(ctx context.Context, id string, meter *openflow_13.OfpMeterMod, ch chan interface{}) {
	log.Debugw("updateMeterTable", log.Fields{"logicalDeviceId": id})
	var res interface{}
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		res = agent.updateMeterTable(ctx, meter)
		log.Debugw("updateMeterTable-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id)
	}
	sendAPIResponse(ctx, ch, res)
}

// ListLogicalDeviceMeters returns logical device meters
func (ldMgr *LogicalDeviceManager) ListLogicalDeviceMeters(ctx context.Context, id string) (*openflow_13.Meters, error) {
	log.Debugw("ListLogicalDeviceMeters", log.Fields{"logicalDeviceId": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDeviceMeters()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}
func (ldMgr *LogicalDeviceManager) updateGroupTable(ctx context.Context, id string, groupMod *openflow_13.OfpGroupMod, ch chan interface{}) {
	log.Debugw("updateGroupTable", log.Fields{"logicalDeviceId": id})
	var res interface{}
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		res = agent.updateGroupTable(ctx, groupMod)
		log.Debugw("updateGroupTable-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id)
	}
	sendAPIResponse(ctx, ch, res)
}

func (ldMgr *LogicalDeviceManager) enableLogicalPort(ctx context.Context, id *voltha.LogicalPortId, ch chan interface{}) {
	log.Debugw("enableLogicalPort", log.Fields{"logicalDeviceId": id})
	var res interface{}
	if agent := ldMgr.getLogicalDeviceAgent(id.Id); agent != nil {
		res = agent.enableLogicalPort(id.PortId)
		log.Debugw("enableLogicalPort-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendAPIResponse(ctx, ch, res)
}

func (ldMgr *LogicalDeviceManager) disableLogicalPort(ctx context.Context, id *voltha.LogicalPortId, ch chan interface{}) {
	log.Debugw("disableLogicalPort", log.Fields{"logicalDeviceId": id})
	var res interface{}
	if agent := ldMgr.getLogicalDeviceAgent(id.Id); agent != nil {
		res = agent.disableLogicalPort(id.PortId)
		log.Debugw("disableLogicalPort-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendAPIResponse(ctx, ch, res)
}

func (ldMgr *LogicalDeviceManager) packetIn(logicalDeviceID string, port uint32, transactionID string, packet []byte) error {
	log.Debugw("packetIn", log.Fields{"logicalDeviceId": logicalDeviceID, "port": port})
	if agent := ldMgr.getLogicalDeviceAgent(logicalDeviceID); agent != nil {
		agent.packetIn(port, transactionID, packet)
	} else {
		log.Error("logical-device-not-exist", log.Fields{"logicalDeviceId": logicalDeviceID})
	}
	return nil
}
