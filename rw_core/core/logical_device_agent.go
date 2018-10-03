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
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/protos/openflow_13"
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

func NewLogicalDeviceAgent(id string, device *voltha.Device, ldeviceMgr *LogicalDeviceManager, deviceMgr *DeviceManager,
	cdProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceId = id
	agent.rootDeviceId = device.Id
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.lockLogicalDevice = sync.RWMutex{}
	return &agent
}

func (agent *LogicalDeviceAgent) Start(ctx context.Context) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	//Build the logical device based on information retrieved from the device adapter
	var switchCap *ca.SwitchCapability
	var err error
	if switchCap, err = agent.deviceMgr.getSwitchCapability(ctx, agent.rootDeviceId); err != nil {
		log.Errorw("error-creating-logical-device", log.Fields{"error": err})
		return err
	}
	ld := &voltha.LogicalDevice{Id: agent.logicalDeviceId, RootDeviceId: agent.rootDeviceId}
	ld.Desc = (proto.Clone(switchCap.Desc)).(*openflow_13.OfpDesc)
	ld.SwitchFeatures = (proto.Clone(switchCap.SwitchFeatures)).(*openflow_13.OfpSwitchFeatures)

	//Add logical ports to the logical device based on the number of NNI ports discovered
	//First get the default port capability - TODO:  each NNI port may have different capabilities,
	//hence. may need to extract the port by the NNI port id defined by the adapter during device
	//creation
	var nniPorts *voltha.Ports
	if nniPorts, err = agent.deviceMgr.getPorts(ctx, agent.rootDeviceId, voltha.Port_ETHERNET_NNI); err != nil {
		log.Errorw("error-creating-logical-port", log.Fields{"error": err})
	}
	var portCap *ca.PortCapability
	for _, port := range nniPorts.Items {
		log.Infow("NNI PORTS", log.Fields{"NNI": port})
		if portCap, err = agent.deviceMgr.getPortCapability(ctx, agent.rootDeviceId, port.PortNo); err != nil {
			log.Errorw("error-creating-logical-device", log.Fields{"error": err})
			return err
		}

		lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
		lp.DeviceId = agent.rootDeviceId
		ld.Ports = append(ld.Ports, lp)
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Save the logical device
	if added := agent.clusterDataProxy.Add("/logical_devices", ld, ""); added == nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-created", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}

	return nil
}

func (agent *LogicalDeviceAgent) getLogicalDevice() (*voltha.LogicalDevice, error) {
	log.Debug("getLogicalDevice")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cloned := proto.Clone(lDevice).(*voltha.LogicalDevice)
		return cloned, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) getLogicalDeviceWithoutLock() (*voltha.LogicalDevice, error) {
	log.Debug("getLogicalDeviceWithoutLock")
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cloned := proto.Clone(lDevice).(*voltha.LogicalDevice)
		return cloned, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device, portNo uint32) error {
	log.Infow("addUNILogicalPort-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// Build the logical device based on information retrieved from the device adapter
	var portCap *ca.PortCapability
	var err error
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, childDevice.Id, portNo); err != nil {
		log.Errorw("error-creating-logical-port", log.Fields{"error": err})
		return err
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get stored logical device
	if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		return status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		cloned := proto.Clone(ldevice).(*voltha.LogicalDevice)
		lp := proto.Clone(portCap.Port).(*voltha.LogicalPort)
		lp.DeviceId = childDevice.Id
		cloned.Ports = append(cloned.Ports, lp)
		return agent.updateLogicalDeviceWithoutLock(cloned)
	}
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceWithoutLock(logicalDevice *voltha.LogicalDevice) error {
	cloned := proto.Clone(logicalDevice).(*voltha.LogicalDevice)
	afterUpdate := agent.clusterDataProxy.Update("/logical_devices/"+agent.logicalDeviceId, cloned, false, "")
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device:%s", agent.logicalDeviceId)
	}
	return nil
}

// deleteLogicalPort removes the logical port associated with a child device
func (agent *LogicalDeviceAgent) deleteLogicalPort(device *voltha.Device) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "deviceId": device.Id})
		return nil
	}
	index := -1
	for i, logicalPort := range logicaldevice.Ports {
		if logicalPort.DeviceId == device.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		copy(logicaldevice.Ports[index:], logicaldevice.Ports[index+1:])
		logicaldevice.Ports[len(logicaldevice.Ports)-1] = nil
		logicaldevice.Ports = logicaldevice.Ports[:len(logicaldevice.Ports)-1]
		log.Debugw("logical-port-deleted", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return agent.updateLogicalDeviceWithoutLock(logicaldevice)
	}
	return nil
}

func (agent *LogicalDeviceAgent) Stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	//Remove the logical device from the model
	if removed := agent.clusterDataProxy.Remove("/logical_devices/"+agent.logicalDeviceId, ""); removed == nil {
		log.Errorw("failed-to-remove-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-removed", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}
