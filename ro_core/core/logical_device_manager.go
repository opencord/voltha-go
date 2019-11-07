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
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/probe"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type LogicalDeviceManager struct {
	logicalDeviceAgents        sync.Map
	deviceMgr                  *DeviceManager
	grpcNbiHdlr                *APIHandler
	clusterDataProxy           *model.Proxy
	exitChannel                chan int
	lockLogicalDeviceAgentsMap sync.RWMutex
}

func newLogicalDeviceManager(deviceMgr *DeviceManager, cdProxy *model.Proxy) *LogicalDeviceManager {
	var logicalDeviceMgr LogicalDeviceManager
	logicalDeviceMgr.exitChannel = make(chan int, 1)
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
	probe.UpdateStatusFromContext(ctx, "logical-device-manager", probe.ServiceStatusRunning)
	log.Info("logical-device-manager-started")
}

func (ldMgr *LogicalDeviceManager) stop(ctx context.Context) {
	log.Info("stopping-logical-device-manager")
	ldMgr.exitChannel <- 1
	probe.UpdateStatusFromContext(ctx, "logical-device-manager", probe.ServiceStatusStopped)
	log.Info("logical-device-manager-stopped")
}

func (ldMgr *LogicalDeviceManager) addLogicalDeviceAgentToMap(agent *LogicalDeviceAgent) {
	if _, exist := ldMgr.logicalDeviceAgents.Load(agent.logicalDeviceId); !exist {
		ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceId, agent)
	}
}

// getLogicalDeviceAgent returns the logical device agent.  If the device is not in memory then the device will
// be loaded from dB and a logical device agent created to managed it.
func (ldMgr *LogicalDeviceManager) getLogicalDeviceAgent(logicalDeviceId string) *LogicalDeviceAgent {
	if agent, ok := ldMgr.logicalDeviceAgents.Load(logicalDeviceId); ok {
		return agent.(*LogicalDeviceAgent)
	} else {
		//	Try to load into memory - loading will also create the logical device agent
		if err := ldMgr.load(logicalDeviceId); err == nil {
			if agent, ok = ldMgr.logicalDeviceAgents.Load(logicalDeviceId); ok {
				return agent.(*LogicalDeviceAgent)
			}
		}
	}
	return nil
}

func (ldMgr *LogicalDeviceManager) deleteLogicalDeviceAgent(logicalDeviceId string) {
	ldMgr.logicalDeviceAgents.Delete(logicalDeviceId)
}

// GetLogicalDevice provides a cloned most up to date logical device
func (ldMgr *LogicalDeviceManager) getLogicalDevice(id string) (*voltha.LogicalDevice, error) {
	log.Debugw("getlogicalDevice", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.GetLogicalDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

//listLogicalDevices returns the list of all logical devices
func (ldMgr *LogicalDeviceManager) listLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("ListAllLogicalDevices")
	result := &voltha.LogicalDevices{}
	if logicalDevices, err := ldMgr.clusterDataProxy.List(context.Background(), "/logical_devices", 0, false,
		""); err != nil {
		log.Errorw("failed-to-list-devices", log.Fields{"error": err})
		return nil, err
	} else if logicalDevices != nil {
		for _, logicalDevice := range logicalDevices.([]interface{}) {
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
	if ldAgent, _ := ldMgr.logicalDeviceAgents.Load(lDeviceId); ldAgent == nil {
		// Logical device not in memory - create a temp logical device Agent and let it load from memory
		agent := newLogicalDeviceAgent(lDeviceId, "", ldMgr, ldMgr.deviceMgr, ldMgr.clusterDataProxy)
		if err := agent.start(nil, true); err != nil {
			agent.stop(nil)
			return err
		}
		ldMgr.logicalDeviceAgents.Store(agent.logicalDeviceId, agent)
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
	return nil, status.Errorf(codes.NotFound, "%s-%s", lPortId.Id, lPortId.PortId)
}
