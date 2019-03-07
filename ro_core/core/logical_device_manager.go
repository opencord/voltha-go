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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type LogicalDeviceManager struct {
	logicalDeviceAgents        map[string]*LogicalDeviceAgent
	deviceMgr                  *DeviceManager
	grpcNbiHdlr                *APIHandler
	clusterDataProxy           *model.Proxy
	exitChannel                chan int
	lockLogicalDeviceAgentsMap sync.RWMutex
}

func newLogicalDeviceManager(deviceMgr *DeviceManager, cdProxy *model.Proxy) *LogicalDeviceManager {
	var logicalDeviceMgr LogicalDeviceManager
	logicalDeviceMgr.exitChannel = make(chan int, 1)
	logicalDeviceMgr.logicalDeviceAgents = make(map[string]*LogicalDeviceAgent)
	logicalDeviceMgr.deviceMgr = deviceMgr
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

func (ldMgr *LogicalDeviceManager) IsLogicalDeviceInCache(id string) bool {
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	_, exist := ldMgr.logicalDeviceAgents[id]
	return exist
}

func (ldMgr *LogicalDeviceManager) listLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("ListAllLogicalDevices")
	result := &voltha.LogicalDevices{}
	if logicalDevices := ldMgr.clusterDataProxy.List("/logical_devices", 0, false, ""); logicalDevices != nil {
		for _, logicalDevice := range logicalDevices.([]interface{}) {
			// If device is not in memory then set it up
			if !ldMgr.IsLogicalDeviceInCache(logicalDevice.(*voltha.LogicalDevice).Id) {
				agent := newLogicalDeviceAgent(
					logicalDevice.(*voltha.LogicalDevice).Id,
					logicalDevice.(*voltha.LogicalDevice).RootDeviceId,
					ldMgr,
					ldMgr.deviceMgr,
					ldMgr.clusterDataProxy,
				)
				if err := agent.start(nil, true); err != nil {
					log.Warnw("failure-starting-agent", log.Fields{"logicalDeviceId": logicalDevice.(*voltha.LogicalDevice).Id})
					agent.stop(nil)
				} else {
					ldMgr.addLogicalDeviceAgentToMap(agent)
				}
			}
			result.Items = append(result.Items, logicalDevice.(*voltha.LogicalDevice))
		}
	}
	return result, nil
}

// load loads a logical device manager in memory
func (ldMgr *LogicalDeviceManager) load(lDeviceId string) error {
	log.Debugw("loading-logical-device", log.Fields{"lDeviceId": lDeviceId})
	// To prevent a race condition, let's hold the logical device agent map lock.  This will prevent a loading and
	// a create logical device callback from occurring at the same time.
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	if ldAgent, _ := ldMgr.logicalDeviceAgents[lDeviceId]; ldAgent == nil {
		// Logical device not in memory - create a temp logical device Agent and let it load from memory
		agent := newLogicalDeviceAgent(lDeviceId, "", ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy)
		if err := agent.start(nil, true); err != nil {
			agent.stop(nil)
			return err
		}
		ldMgr.logicalDeviceAgents[agent.logicalDeviceId] = agent
	}
	// TODO: load the child device
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

func (ldMgr *LogicalDeviceManager) ListLogicalDeviceFlows(ctx context.Context, id string) (*voltha.Flows, error) {
	log.Debugw("ListLogicalDeviceFlows", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDeviceFlows()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)

}

func (ldMgr *LogicalDeviceManager) ListLogicalDeviceFlowGroups(ctx context.Context, id string) (*voltha.FlowGroups, error) {
	log.Debugw("ListLogicalDeviceFlowGroups", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.ListLogicalDeviceFlowGroups()
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
