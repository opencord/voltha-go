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
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type LogicalDeviceAgent struct {
	logicalDeviceId   string
	lastData          *voltha.LogicalDevice
	rootDeviceId      string
	deviceMgr         *DeviceManager
	ldeviceMgr        *LogicalDeviceManager
	clusterDataProxy  *model.Proxy
	exitChannel       chan int
	lockLogicalDevice sync.RWMutex
}

func newLogicalDeviceAgent(id string, deviceId string, ldeviceMgr *LogicalDeviceManager, deviceMgr *DeviceManager, cdProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceId = id
	agent.rootDeviceId = deviceId
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.lockLogicalDevice = sync.RWMutex{}
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	log.Info("logical_device-agent-started")
	return nil
}

// stop terminates the logical device agent.
func (agent *LogicalDeviceAgent) stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	//Remove the logical device from the model
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}

// GetLogicalDevice locks the logical device model and then retrieves the latest logical device information
func (agent *LogicalDeviceAgent) GetLogicalDevice() (*voltha.LogicalDevice, error) {
	log.Debug("GetLogicalDevice")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDevicePorts() (*voltha.LogicalPorts, error) {
	log.Debug("!!!!!ListLogicalDevicePorts")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		lPorts := make([]*voltha.LogicalPort, 0)
		for _, port := range lDevice.Ports {
			lPorts = append(lPorts, port)
		}
		return &voltha.LogicalPorts{Items: lPorts}, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// listFlows locks the logical device model and then retrieves the latest flow information
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlows() (*voltha.Flows, error) {
	log.Debug("listFlows")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId+"/flows", 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.Flows, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// listFlowGroups locks the logical device model and then retrieves the latest flow groups information
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlowGroups() (*voltha.FlowGroups, error) {
	log.Debug("listFlowGroups")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.FlowGroups, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}
