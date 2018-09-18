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
	"reflect"
	"strings"
	"sync"
)

type LogicalDeviceManager struct {
	logicalDeviceAgents        map[string]*LogicalDeviceAgent
	deviceMgr                  *DeviceManager
	adapterProxy               *AdapterProxy
	kafkaProxy                 *kafka.KafkaMessagingProxy
	localDataProxy             *model.Proxy
	exitChannel                chan int
	lockLogicalDeviceAgentsMap sync.RWMutex
}

func NewLogicalDeviceManager(deviceMgr *DeviceManager, kafkaProxy *kafka.KafkaMessagingProxy, ldProxy *model.Proxy) *LogicalDeviceManager {
	var logicalDeviceMgr LogicalDeviceManager
	logicalDeviceMgr.exitChannel = make(chan int, 1)
	logicalDeviceMgr.logicalDeviceAgents = make(map[string]*LogicalDeviceAgent)
	logicalDeviceMgr.deviceMgr = deviceMgr
	logicalDeviceMgr.kafkaProxy = kafkaProxy
	logicalDeviceMgr.localDataProxy = ldProxy
	logicalDeviceMgr.lockLogicalDeviceAgentsMap = sync.RWMutex{}
	return &logicalDeviceMgr
}

func (ldMgr *LogicalDeviceManager) Start(ctx context.Context) {
	log.Info("starting-logical-device-manager")
	log.Info("logical-device-manager-started")
}

func (ldMgr *LogicalDeviceManager) Stop(ctx context.Context) {
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

func (ldMgr *LogicalDeviceManager) getLogicalDevice(id string) (*voltha.LogicalDevice, error) {
	log.Debugw("getlogicalDevice-start", log.Fields{"logicaldeviceid": id})
	logicalDevice := ldMgr.localDataProxy.Get("/logical_devices/"+id, 1, false, "")
	if logicalDevice != nil {
		cloned := reflect.ValueOf(logicalDevice).Elem().Interface().(voltha.LogicalDevice)
		return &cloned, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (ldMgr *LogicalDeviceManager) listLogicalDevices() (*voltha.LogicalDevices, error) {
	log.Debug("listLogicalDevices-start")
	result := &voltha.LogicalDevices{}
	ldMgr.lockLogicalDeviceAgentsMap.Lock()
	defer ldMgr.lockLogicalDeviceAgentsMap.Unlock()
	for _, agent := range ldMgr.logicalDeviceAgents {
		logicalDevice := ldMgr.localDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
		if logicalDevice != nil {
			cloned := reflect.ValueOf(logicalDevice).Elem().Interface().(voltha.LogicalDevice)
			result.Items = append(result.Items, &cloned)
		}
	}
	return result, nil
}

func (ldMgr *LogicalDeviceManager) CreateLogicalDevice(ctx context.Context, device *voltha.Device) (*string, error) {
	log.Infow("creating-logical-device-start", log.Fields{"deviceId": device.Id})
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
	log.Debugw("setting-logical-device-id", log.Fields{"logicaldeviceId": id})

	agent := NewLogicalDeviceAgent(id, device, ldMgr, ldMgr.deviceMgr, ldMgr.localDataProxy)
	ldMgr.addLogicalDeviceAgentToMap(agent)
	go agent.Start(ctx)

	log.Info("creating-logical-device-ends")
	return &id, nil
}

func (ldMgr *LogicalDeviceManager) AddUNILogicalPort(ctx context.Context, childDevice *voltha.Device) error {
	log.Infow("AddUNILogicalPort-start", log.Fields{"deviceId": childDevice.Id})
	// Sanity check
	if childDevice.Root {
		return errors.New("Device-root")
	}

	// Get the logical device id parent device
	parentId := childDevice.ParentId
	logDeviceId := ldMgr.deviceMgr.GetParentDeviceId(parentId)

	log.Infow("AddUNILogicalPort", log.Fields{"logDeviceId": logDeviceId, "parentId": parentId})

	if agent := ldMgr.getLogicalDeviceAgent(*logDeviceId); agent != nil {
		return agent.addUNILogicalPort(ctx, childDevice, childDevice.ProxyAddress.ChannelId)
	}
	return status.Errorf(codes.NotFound, "%s", childDevice.Id)
}
