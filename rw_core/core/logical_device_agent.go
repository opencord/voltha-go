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
	"reflect"
)

type LogicalDeviceAgent struct {
	logicalDeviceId string
	lastData        *voltha.LogicalDevice
	rootDeviceId    string
	deviceMgr       *DeviceManager
	ldeviceMgr      *LogicalDeviceManager
	localDataProxy  *model.Proxy
	exitChannel     chan int
}

func NewLogicalDeviceAgent(id string, device *voltha.Device, ldeviceMgr *LogicalDeviceManager, deviceMgr *DeviceManager,
	ldProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceId = id
	agent.rootDeviceId = device.Id
	agent.deviceMgr = deviceMgr
	agent.localDataProxy = ldProxy
	agent.ldeviceMgr = ldeviceMgr
	return &agent
}

func (agent *LogicalDeviceAgent) Start(ctx context.Context) error {
	log.Info("starting-logical_device-agent")
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
	if nniPorts, err = agent.deviceMgr.getNNIPorts(ctx, agent.rootDeviceId); err != nil {
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
		ld.Ports = append(ld.Ports, lp)
	}
	// Save the logical device
	if added := agent.localDataProxy.Add("/logical_devices", ld, ""); added == nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-created", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}

	return nil
}

func (agent *LogicalDeviceAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device, portNo uint32) error {
	log.Info("addUNILogicalPort-start")
	// Build the logical device based on information retrieved from the device adapter
	var portCap *ca.PortCapability
	var err error
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, childDevice.Id, portNo); err != nil {
		log.Errorw("error-creating-logical-port", log.Fields{"error": err})
		return err
	}
	// Get stored logical device
	if ldevice, err := agent.ldeviceMgr.getLogicalDevice(agent.logicalDeviceId); err != nil {
		return status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		cloned := reflect.ValueOf(ldevice).Elem().Interface().(voltha.LogicalDevice)
		lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
		cloned.Ports = append(cloned.Ports, lp)
		afterUpdate := agent.localDataProxy.Update("/logical_devices/"+agent.logicalDeviceId, &cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "failed-add-UNI-port:%s", agent.logicalDeviceId)
		}
		return nil
	}
}

func (agent *LogicalDeviceAgent) Stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}

func (agent *LogicalDeviceAgent) getLogicalDevice(ctx context.Context) *voltha.LogicalDevice {
	log.Debug("getLogicalDevice")
	cp := proto.Clone(agent.lastData)
	return cp.(*voltha.LogicalDevice)
}
