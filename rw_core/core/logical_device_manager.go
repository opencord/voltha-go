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
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"sync"
)

type LogicalDeviceManager struct {
	logicalDeviceAgents        map[string]*LogicalDeviceAgent
	deviceMgr                  *DeviceManager
	adapterProxy               *AdapterProxy
	kafkaProxy                 *kafka.KafkaMessagingProxy
	clusterDataProxy           *model.Proxy
	exitChannel                chan int
	lockLogicalDeviceAgentsMap sync.RWMutex
}

func newLogicalDeviceManager(deviceMgr *DeviceManager, kafkaProxy *kafka.KafkaMessagingProxy, cdProxy *model.Proxy) *LogicalDeviceManager {
	var logicalDeviceMgr LogicalDeviceManager
	logicalDeviceMgr.exitChannel = make(chan int, 1)
	logicalDeviceMgr.logicalDeviceAgents = make(map[string]*LogicalDeviceAgent)
	logicalDeviceMgr.deviceMgr = deviceMgr
	logicalDeviceMgr.kafkaProxy = kafkaProxy
	logicalDeviceMgr.clusterDataProxy = cdProxy
	logicalDeviceMgr.lockLogicalDeviceAgentsMap = sync.RWMutex{}
	return &logicalDeviceMgr
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

// getLogicalDevice provides a cloned most up to date logical device
func (ldMgr *LogicalDeviceManager) getLogicalDevice(id string) (*voltha.LogicalDevice, error) {
	log.Debugw("getlogicalDevice", log.Fields{"logicaldeviceid": id})
	if agent := ldMgr.getLogicalDeviceAgent(id); agent != nil {
		return agent.getLogicalDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (ldMgr *LogicalDeviceManager) listLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("listLogicalDevices")
	result := &voltha.LogicalDevices{}
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	for _, agent := range ldMgr.logicalDeviceAgents {
		if lDevice, err := agent.getLogicalDevice(); err == nil {
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

// DeleteLogicalDevice removes the logical port associated with a child device
func (ldMgr *LogicalDeviceManager) deleteLogicalPort(ctx context.Context, device *voltha.Device) error {
	log.Debugw("deleting-logical-port", log.Fields{"deviceId": device.Id})
	// Sanity check
	if device.Root {
		return errors.New("Device-root")
	}
	logDeviceId, _ := ldMgr.getLogicalDeviceId(device)
	if logDeviceId == nil {
		log.Debugw("no-logical-device-present", log.Fields{"deviceId": device.Id})
		return nil
	}
	if agent := ldMgr.getLogicalDeviceAgent(*logDeviceId); agent != nil {
		agent.deleteLogicalPort(device)
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
		return agent.addUNILogicalPort(ctx, childDevice, childDevice.ProxyAddress.ChannelId)
	}
	return status.Errorf(codes.NotFound, "%s", childDevice.Id)
}
