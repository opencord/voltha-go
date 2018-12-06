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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"sync"
)

type LogicalDeviceManager struct {
	logicalDeviceAgents        map[string]*LogicalDeviceAgent
	deviceMgr                  *DeviceManager
	grpcNbiHdlr                *APIHandler
	adapterProxy               *AdapterProxy
	kafkaICProxy               *kafka.InterContainerProxy
	clusterDataProxy           *model.Proxy
	exitChannel                chan int
	lockLogicalDeviceAgentsMap sync.RWMutex
}

func newLogicalDeviceManager(deviceMgr *DeviceManager, kafkaICProxy *kafka.InterContainerProxy, cdProxy *model.Proxy) *LogicalDeviceManager {
	var logicalDeviceMgr LogicalDeviceManager
	logicalDeviceMgr.exitChannel = make(chan int, 1)
	logicalDeviceMgr.logicalDeviceAgents = make(map[string]*LogicalDeviceAgent)
	logicalDeviceMgr.deviceMgr = deviceMgr
	logicalDeviceMgr.kafkaICProxy = kafkaICProxy
	logicalDeviceMgr.clusterDataProxy = cdProxy
	logicalDeviceMgr.lockLogicalDeviceAgentsMap = sync.RWMutex{}
	return &logicalDeviceMgr
}

func (ldMgr *LogicalDeviceManager) setGrpcNbiHandler(grpcNbiHandler *APIHandler) {
	ldMgr.grpcNbiHdlr = grpcNbiHandler
}

func (ldMgr *LogicalDeviceManager) start(ctx context.Context) {
	log.Info("starting-logical-device-manager")
	log.Info("logical-device-manager-started")
}

func (ldMgr *LogicalDeviceManager) stop(ctx context.Context) {
	log.Info("stopping-logical-device-manager")
	ldMgr.exitChannel <- 1
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
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	if _, exist := ldMgr.logicalDeviceAgents[agent.logicalDeviceId]; !exist {
		ldMgr.logicalDeviceAgents[agent.logicalDeviceId] = agent
	}
}

func (ldMgr *LogicalDeviceManager) getLogicalDeviceAgent(logicalDeviceId string) *LogicalDeviceAgent {
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	if agent, ok := ldMgr.logicalDeviceAgents[logicalDeviceId]; ok {
		return agent
	}
	return nil
}

func (ldMgr *LogicalDeviceManager) deleteLogicalDeviceAgent(logicalDeviceId string) {
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	delete(ldMgr.logicalDeviceAgents, logicalDeviceId)
}

// GetLogicalDevice provides a cloned most up to date logical device
func (ldMgr *LogicalDeviceManager) getLogicalDevice(id string) (*voltha.LogicalDevice, error) {
	log.Debugw("getlogicalDevice", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.GetLogicalDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (ldMgr *LogicalDeviceManager) listLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("listLogicalDevices")
	result := &voltha.LogicalDevices{}
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	for _, agent := range ldMgr.logicalDeviceAgents {
		if lDevice, err := agent.GetLogicalDevice(); err == nil {
			result.Items = append(result.Items, lDevice)
		}
	}
	return result, nil
}

func (ldMgr *LogicalDeviceManager) createLogicalDevice(ctx context.Context, device *voltha.Device) (*string, error) {
	log.Debugw("creating-logical-device", log.Fields{"deviceId": device.Id})
	// Sanity check
	if !device.Root {
		return nil, errors.New("Device-not-root")
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

	agent := newLogicalDeviceAgent(id, device, ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy)
	ldMgr.addLogicalDeviceAgentToMap(agent)
	go agent.start(ctx)

	log.Debug("creating-logical-device-ends")
	return &id, nil
}

func (ldMgr *LogicalDeviceManager) deleteLogicalDevice(ctx context.Context, device *voltha.Device) error {
	log.Debugw("deleting-logical-device", log.Fields{"deviceId": device.Id})
	// Sanity check
	if !device.Root {
		return errors.New("Device-not-root")
	}
	logDeviceId := device.ParentId
	if agent := ldMgr.getLogicalDeviceAgent(logDeviceId); agent != nil {
		// Stop the logical device agent
		agent.stop(ctx)
		//Remove the logical device agent from the Map
		ldMgr.deleteLogicalDeviceAgent(logDeviceId)
	}

	log.Debug("deleting-logical-device-ends")
	return nil
}

func (ldMgr *LogicalDeviceManager) getLogicalDeviceId(device *voltha.Device) (*string, error) {
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

func (ldMgr *LogicalDeviceManager) getLogicalPortId(device *voltha.Device) (*voltha.LogicalPortId, error) {
	// Get the logical device where this device is attached
	var lDeviceId *string
	var err error
	if lDeviceId, err = ldMgr.getLogicalDeviceId(device); err != nil {
		return nil, err
	}
	var lDevice *voltha.LogicalDevice
	if lDevice, err = ldMgr.getLogicalDevice(*lDeviceId); err != nil {
		return nil, err
	}
	// Go over list of ports
	for _, port := range lDevice.Ports {
		if port.DeviceId == device.Id {
			return &voltha.LogicalPortId{Id: *lDeviceId, PortId: port.Id}, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "%s", device.Id)
}

func (ldMgr *LogicalDeviceManager) ListLogicalDevicePorts(ctx context.Context, id string) (*voltha.LogicalPorts, error) {
	log.Debugw("ListLogicalDevicePorts", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDevicePorts()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)

}

func (ldMgr *LogicalDeviceManager) getLogicalPort(lPortId *voltha.LogicalPortId) (*voltha.LogicalPort, error) {
	// Get the logical device where this device is attached
	var err error
	var lDevice *voltha.LogicalDevice
	if lDevice, err = ldMgr.getLogicalDevice(lPortId.Id); err != nil {
		return nil, err
	}
	// Go over list of ports
	for _, port := range lDevice.Ports {
		if port.Id == lPortId.PortId {
			return port, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "%s-$s", lPortId.Id, lPortId.PortId)
}

// deleteLogicalPort removes the logical port associated with a child device
func (ldMgr *LogicalDeviceManager) deleteLogicalPort(ctx context.Context, lPortId *voltha.LogicalPortId) error {
	log.Debugw("deleting-logical-port", log.Fields{"LDeviceId": lPortId.Id})
	// Get logical port
	var logicalPort *voltha.LogicalPort
	var err error
	if logicalPort, err = ldMgr.getLogicalPort(lPortId); err != nil {
		log.Debugw("no-logical-device-port-present", log.Fields{"logicalPortId": lPortId.PortId})
		return err
	}
	// Sanity check
	if logicalPort.RootPort {
		return errors.New("Device-root")
	}
	if agent := ldMgr.getLogicalDeviceAgent(lPortId.Id); agent != nil {
		agent.deleteLogicalPort(logicalPort)
	}

	log.Debug("deleting-logical-port-ends")
	return nil
}

func (ldMgr *LogicalDeviceManager) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device) error {
	log.Debugw("AddUNILogicalPort", log.Fields{"deviceId": childDevice.Id})
	// Sanity check
	if childDevice.Root {
		return errors.New("Device-root")
	}

	// Get the logical device id parent device
	parentId := childDevice.ParentId
	logDeviceId := ldMgr.deviceMgr.GetParentDeviceId(parentId)

	log.Debugw("AddUNILogicalPort", log.Fields{"logDeviceId": logDeviceId, "parentId": parentId})

	if agent := ldMgr.getLogicalDeviceAgent(*logDeviceId); agent != nil {
		return agent.addUNILogicalPort(ctx, childDevice)
	}
	return status.Errorf(codes.NotFound, "%s", childDevice.Id)
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
	// Get logical port
	var logicalPort *voltha.LogicalPort
	var err error
	if logicalPort, err = ldMgr.getLogicalPort(id); err != nil {
		log.Debugw("no-logical-device-port-present", log.Fields{"logicalPortId": id.PortId})
		res = err
	}
	if agent := ldMgr.getLogicalDeviceAgent(id.Id); agent != nil {
		res = agent.enableLogicalPort(logicalPort)
		log.Debugw("enableLogicalPort-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendAPIResponse(ctx, ch, res)
}

func (ldMgr *LogicalDeviceManager) disableLogicalPort(ctx context.Context, id *voltha.LogicalPortId, ch chan interface{}) {
	log.Debugw("disableLogicalPort", log.Fields{"logicalDeviceId": id})
	var res interface{}
	// Get logical port
	var logicalPort *voltha.LogicalPort
	var err error
	if logicalPort, err = ldMgr.getLogicalPort(id); err != nil {
		log.Debugw("no-logical-device-port-present", log.Fields{"logicalPortId": id.PortId})
		res = err
	}
	if agent := ldMgr.getLogicalDeviceAgent(id.Id); agent != nil {
		res = agent.disableLogicalPort(logicalPort)
		log.Debugw("disableLogicalPort-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendAPIResponse(ctx, ch, res)
}

func (ldMgr *LogicalDeviceManager) packetOut(packetOut *openflow_13.PacketOut) {
	log.Debugw("packetOut", log.Fields{"logicalDeviceId": packetOut.Id})
	if agent := ldMgr.getLogicalDeviceAgent(packetOut.Id); agent != nil {
		agent.packetOut(packetOut.PacketOut)
	} else {
		log.Error("logical-device-not-exist", log.Fields{"logicalDeviceId": packetOut.Id})
	}
}

func (ldMgr *LogicalDeviceManager) packetIn(logicalDeviceId string, port uint32, packet []byte) error {
	log.Debugw("packetIn", log.Fields{"logicalDeviceId": logicalDeviceId, "port": port})
	if agent := ldMgr.getLogicalDeviceAgent(logicalDeviceId); agent != nil {
		agent.packetIn(port, packet)
	} else {
		log.Error("logical-device-not-exist", log.Fields{"logicalDeviceId": logicalDeviceId})
	}
	return nil
}
