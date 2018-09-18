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
	"github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"runtime"
	"sync"
)

type DeviceManager struct {
	deviceAgents        map[string]*DeviceAgent
	adapterProxy        *AdapterProxy
	logicalDeviceMgr    *LogicalDeviceManager
	kafkaProxy          *kafka.KafkaMessagingProxy
	stateTransitions    *TransitionMap
	localDataProxy      *model.Proxy
	exitChannel         chan int
	lockDeviceAgentsMap sync.RWMutex
}

func NewDeviceManager(kafkaProxy *kafka.KafkaMessagingProxy, ldProxy *model.Proxy) *DeviceManager {
	var deviceMgr DeviceManager
	deviceMgr.exitChannel = make(chan int, 1)
	deviceMgr.deviceAgents = make(map[string]*DeviceAgent)
	deviceMgr.adapterProxy = NewAdapterProxy(kafkaProxy)
	deviceMgr.kafkaProxy = kafkaProxy
	deviceMgr.localDataProxy = ldProxy
	deviceMgr.lockDeviceAgentsMap = sync.RWMutex{}
	return &deviceMgr
}

func (dMgr *DeviceManager) Start(ctx context.Context, logicalDeviceMgr *LogicalDeviceManager) {
	log.Info("starting-device-manager")
	dMgr.logicalDeviceMgr = logicalDeviceMgr
	dMgr.stateTransitions = NewTransitionMap(dMgr)
	log.Info("device-manager-started")
}

func (dMgr *DeviceManager) Stop(ctx context.Context) {
	log.Info("stopping-device-manager")
	dMgr.exitChannel <- 1
	log.Info("device-manager-stopped")
}

func sendResponse(ctx context.Context, ch chan interface{}, result interface{}) {
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

func (dMgr *DeviceManager) addDeviceAgentToMap(agent *DeviceAgent) {
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	if _, exist := dMgr.deviceAgents[agent.deviceId]; !exist {
		dMgr.deviceAgents[agent.deviceId] = agent
	}
}

func (dMgr *DeviceManager) getDeviceAgent(deviceId string) *DeviceAgent {
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	if agent, ok := dMgr.deviceAgents[deviceId]; ok {
		return agent
	}
	return nil
}

func (dMgr *DeviceManager) createDevice(ctx context.Context, device *voltha.Device, ch chan interface{}) {
	log.Debugw("createDevice-start", log.Fields{"device": device, "aproxy": dMgr.adapterProxy})

	// Create and start a device agent for that device
	agent := newDeviceAgent(dMgr.adapterProxy, device, dMgr, dMgr.localDataProxy)
	dMgr.addDeviceAgentToMap(agent)
	agent.start(ctx)

	sendResponse(ctx, ch, nil)
}

func (dMgr *DeviceManager) enableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	log.Debugw("enableDevice-start", log.Fields{"deviceid": id})

	var res interface{}
	if agent := dMgr.getDeviceAgent(id.Id); agent != nil {
		res = agent.enableDevice(ctx)
		log.Debugw("EnableDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}

	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) getDevice(id string) (*voltha.Device, error) {
	log.Debugw("getDevice-start", log.Fields{"deviceid": id})

	if device := dMgr.localDataProxy.Get("/devices/"+id, 1, false, ""); device == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id)
	} else {
		cloned := reflect.ValueOf(device).Elem().Interface().(voltha.Device)
		return &cloned, nil
	}
}

func (dMgr *DeviceManager) ListDevices() (*voltha.Devices, error) {
	log.Debug("ListDevices-start")
	result := &voltha.Devices{}
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	for _, agent := range dMgr.deviceAgents {
		if device := dMgr.localDataProxy.Get("/devices/"+agent.deviceId, 1, false, ""); device != nil {
			cloned := reflect.ValueOf(device).Elem().Interface().(voltha.Device)
			result.Items = append(result.Items, &cloned)
		}
	}
	return result, nil
}

func (dMgr *DeviceManager) updateDevice(device *voltha.Device) error {
	log.Debugw("updateDevice-start", log.Fields{"deviceid": device.Id, "device": device})

	if agent := dMgr.getDeviceAgent(device.Id); agent != nil {
		return agent.updateDevice(device)
	}
	return status.Errorf(codes.NotFound, "%s", device.Id)
}

func (dMgr *DeviceManager) addPort(deviceId string, port *voltha.Port) error {
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.addPort(port)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) updatePmConfigs(deviceId string, pmConfigs *voltha.PmConfigs) error {
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updatePmConfigs(pmConfigs)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) getSwitchCapability(ctx context.Context, deviceId string) (*core_adapter.SwitchCapability, error) {
	log.Debugw("getSwitchCapability-start", log.Fields{"deviceid": deviceId})

	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getSwitchCapability(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) getNNIPorts(ctx context.Context, deviceId string) (*voltha.Ports, error) {
	log.Debugw("getNNIPorts-start", log.Fields{"deviceid": deviceId})

	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getNNIPorts(ctx), nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) getPortCapability(ctx context.Context, deviceId string, portNo uint32) (*core_adapter.PortCapability, error) {
	log.Debugw("getPortCapability-start", log.Fields{"deviceid": deviceId})

	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getPortCapability(ctx, portNo)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) updateDeviceState(deviceId string, operState *core_adapter.IntType, connState *core_adapter.IntType) error {
	log.Debugw("updateDeviceState-start", log.Fields{"deviceid": deviceId, "operState": operState, "connState": connState})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updateDeviceState(operState, connState)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) childDeviceDetected(parentDeviceId string, parentPortNo int64, deviceType string, channelId int64) error {
	log.Debugw("childDeviceDetected-start", log.Fields{"parentDeviceId": parentDeviceId})

	// Create the ONU device
	childDevice := &voltha.Device{}
	childDevice.Id = CreateDeviceId()
	childDevice.Type = deviceType
	childDevice.ParentId = parentDeviceId
	childDevice.ParentPortNo = uint32(parentPortNo)
	childDevice.Root = false
	childDevice.ProxyAddress = &voltha.Device_ProxyAddress{ChannelId: uint32(channelId)}

	// Create and start a device agent for that device
	agent := newDeviceAgent(dMgr.adapterProxy, childDevice, dMgr, dMgr.localDataProxy)
	dMgr.addDeviceAgentToMap(agent)
	agent.start(nil)

	// Activate the child device
	if agent := dMgr.getDeviceAgent(childDevice.Id); agent != nil {
		return agent.enableDevice(nil)
	}

	return nil
}

func (dMgr *DeviceManager) processTransition(previous *voltha.Device, current *voltha.Device) error {
	// This will be triggered on every update to the device.
	handler := dMgr.stateTransitions.GetTransitionHandler(previous, current)
	if handler != nil {
		log.Debugw("found-handler", log.Fields{"handler": funcName(handler)})
		return handler(previous, current)
	}
	log.Debugw("handler-not-found", log.Fields{"deviceId": current.Id})
	return nil
}

func (dMgr *DeviceManager) createLogicalDevice(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("createLogicalDevice")
	var logicalId *string
	var err error
	if logicalId, err = dMgr.logicalDeviceMgr.CreateLogicalDevice(nil, cDevice); err != nil {
		log.Warnw("createlogical-device-error", log.Fields{"device": cDevice})
		return err
	}
	// Update the parent device with the logical id
	dMgr.UpdateDeviceAttribute(cDevice.Id, "ParentId", *logicalId)
	return nil
}

func (dMgr *DeviceManager) addUNILogicalPort(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("addUNILogicalPort")
	if err := dMgr.logicalDeviceMgr.AddUNILogicalPort(nil, cDevice); err != nil {
		log.Warnw("addUNILogicalPort-error", log.Fields{"device": cDevice, "err": err})
		return err
	}
	return nil
}

func (dMgr *DeviceManager) activateDevice(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("activateDevice")
	return nil
}

func (dMgr *DeviceManager) disableDevice(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("disableDevice")
	return nil
}

func (dMgr *DeviceManager) abandonDevice(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("abandonDevice")
	return nil
}

func (dMgr *DeviceManager) reEnableDevice(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("reEnableDevice")
	return nil
}

func (dMgr *DeviceManager) noOp(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("noOp")
	return nil
}

func (dMgr *DeviceManager) notAllowed(pDevice *voltha.Device, cDevice *voltha.Device) error {
	log.Info("notAllowed")
	return errors.New("Transition-not-allowed")
}

func funcName(f interface{}) string {
	p := reflect.ValueOf(f).Pointer()
	rf := runtime.FuncForPC(p)
	return rf.Name()
}

func (dMgr *DeviceManager) UpdateDeviceAttribute(deviceId string, attribute string, value interface{}) {
	if agent, ok := dMgr.deviceAgents[deviceId]; ok {
		agent.updateDeviceAttribute(attribute, value)
	}
}

func (dMgr *DeviceManager) GetParentDeviceId(deviceId string) *string {
	if device, _ := dMgr.getDevice(deviceId); device != nil {
		log.Infow("GetParentDeviceId", log.Fields{"device": device})
		return &device.ParentId
	}
	return nil
}
