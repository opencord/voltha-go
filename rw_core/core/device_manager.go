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
	"github.com/gogo/protobuf/proto"
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
	clusterDataProxy    *model.Proxy
	exitChannel         chan int
	lockDeviceAgentsMap sync.RWMutex
}

func NewDeviceManager(kafkaProxy *kafka.KafkaMessagingProxy, cdProxy *model.Proxy) *DeviceManager {
	var deviceMgr DeviceManager
	deviceMgr.exitChannel = make(chan int, 1)
	deviceMgr.deviceAgents = make(map[string]*DeviceAgent)
	deviceMgr.adapterProxy = NewAdapterProxy(kafkaProxy)
	deviceMgr.kafkaProxy = kafkaProxy
	deviceMgr.clusterDataProxy = cdProxy
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
	log.Debugw("createDevice", log.Fields{"device": device, "aproxy": dMgr.adapterProxy})

	// Create and start a device agent for that device
	agent := newDeviceAgent(dMgr.adapterProxy, device, dMgr, dMgr.clusterDataProxy)
	dMgr.addDeviceAgentToMap(agent)
	agent.start(ctx)

	sendResponse(ctx, ch, agent.lastData)
}

func (dMgr *DeviceManager) enableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	log.Debugw("enableDevice", log.Fields{"deviceid": id})

	var res interface{}
	if agent := dMgr.getDeviceAgent(id.Id); agent != nil {
		res = agent.enableDevice(ctx)
		log.Debugw("EnableDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}

	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) disableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	log.Debugw("disableDevice", log.Fields{"deviceid": id})

	var res interface{}
	if agent := dMgr.getDeviceAgent(id.Id); agent != nil {
		res = agent.disableDevice(ctx)
		log.Debugw("disableDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}

	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) getDevice(id string) (*voltha.Device, error) {
	log.Debugw("getDevice", log.Fields{"deviceid": id})
	if agent := dMgr.getDeviceAgent(id); agent != nil {
		return agent.getDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (dMgr *DeviceManager) ListDevices() (*voltha.Devices, error) {
	log.Debug("ListDevices")
	result := &voltha.Devices{}
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	for _, agent := range dMgr.deviceAgents {
		if device, err := agent.getDevice(); err == nil {
			cloned := proto.Clone(device).(*voltha.Device)
			result.Items = append(result.Items, cloned)
		}
	}
	return result, nil
}

func (dMgr *DeviceManager) updateDevice(device *voltha.Device) error {
	log.Debugw("updateDevice", log.Fields{"deviceid": device.Id, "device": device})

	if agent := dMgr.getDeviceAgent(device.Id); agent != nil {
		return agent.updateDevice(device)
	}
	return status.Errorf(codes.NotFound, "%s", device.Id)
}

func (dMgr *DeviceManager) addPort(deviceId string, port *voltha.Port) error {
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		if err := agent.addPort(port); err != nil {
			return err
		}
		//	Setup peer ports
		meAsPeer := &voltha.Port_PeerPort{DeviceId: deviceId, PortNo: port.PortNo}
		for _, peerPort := range port.Peers {
			if agent := dMgr.getDeviceAgent(peerPort.DeviceId); agent != nil {
				if err := agent.addPeerPort(meAsPeer); err != nil {
					log.Errorw("failed-to-add-peer", log.Fields{"peer-device-id": peerPort.DeviceId})
					return err
				}
			}
		}
		return nil
	} else {
		return status.Errorf(codes.NotFound, "%s", deviceId)
	}
}

func (dMgr *DeviceManager) updatePmConfigs(deviceId string, pmConfigs *voltha.PmConfigs) error {
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updatePmConfigs(pmConfigs)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) getSwitchCapability(ctx context.Context, deviceId string) (*core_adapter.SwitchCapability, error) {
	log.Debugw("getSwitchCapability", log.Fields{"deviceid": deviceId})

	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getSwitchCapability(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) getPorts(ctx context.Context, deviceId string, portType voltha.Port_PortType) (*voltha.Ports, error) {
	log.Debugw("getPorts", log.Fields{"deviceid": deviceId, "portType": portType})

	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getPorts(ctx, portType), nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) getPortCapability(ctx context.Context, deviceId string, portNo uint32) (*core_adapter.PortCapability, error) {
	log.Debugw("getPortCapability", log.Fields{"deviceid": deviceId})

	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getPortCapability(ctx, portNo)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) updateDeviceStatus(deviceId string, operStatus voltha.OperStatus_OperStatus, connStatus voltha.ConnectStatus_ConnectStatus) error {
	log.Debugw("updateDeviceStatus", log.Fields{"deviceid": deviceId, "operStatus": operStatus, "connStatus": connStatus})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updateDeviceStatus(operStatus, connStatus)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) updatePortState(deviceId string, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_OperStatus) error {
	log.Debugw("updatePortState", log.Fields{"deviceid": deviceId, "portType": portType, "portNo": portNo, "operStatus": operStatus})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updatePortState(portType, portNo, operStatus)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) childDeviceDetected(parentDeviceId string, parentPortNo int64, deviceType string, channelId int64) error {
	log.Debugw("childDeviceDetected", log.Fields{"parentDeviceId": parentDeviceId})

	// Create the ONU device
	childDevice := &voltha.Device{}
	childDevice.Type = deviceType
	childDevice.ParentId = parentDeviceId
	childDevice.ParentPortNo = uint32(parentPortNo)
	childDevice.Root = false
	childDevice.ProxyAddress = &voltha.Device_ProxyAddress{ChannelId: uint32(channelId)}

	// Create and start a device agent for that device
	agent := newDeviceAgent(dMgr.adapterProxy, childDevice, dMgr, dMgr.clusterDataProxy)
	dMgr.addDeviceAgentToMap(agent)
	agent.start(nil)

	// Activate the child device
	if agent := dMgr.getDeviceAgent(agent.deviceId); agent != nil {
		return agent.enableDevice(nil)
	}

	return nil
}

func (dMgr *DeviceManager) processTransition(previous *voltha.Device, current *voltha.Device) error {
	// This will be triggered on every update to the device.
	handlers := dMgr.stateTransitions.GetTransitionHandler(previous, current)
	if handlers == nil {
		log.Debugw("handlers-not-found", log.Fields{"deviceId": current.Id})
		return nil
	}
	for _, handler := range handlers {
		log.Debugw("running-handler", log.Fields{"handler": funcName(handler)})
		if err := handler(current); err != nil {
			return err
		}
	}
	//if handler != nil {
	//	log.Debugw("found-handlers", log.Fields{"handlers": funcName(handler)})
	//	return handler(current)
	//}
	return nil
}

func (dMgr *DeviceManager) createLogicalDevice(cDevice *voltha.Device) error {
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

func (dMgr *DeviceManager) deleteLogicalDevice(cDevice *voltha.Device) error {
	log.Info("deleteLogicalDevice")
	var err error
	if err = dMgr.logicalDeviceMgr.DeleteLogicalDevice(nil, cDevice); err != nil {
		log.Warnw("deleteLogical-device-error", log.Fields{"deviceId": cDevice.Id})
		return err
	}
	// Remove the logical device Id from the parent device
	logicalId := ""
	dMgr.UpdateDeviceAttribute(cDevice.Id, "ParentId", logicalId)
	return nil
}

func (dMgr *DeviceManager) deleteLogicalPort(cDevice *voltha.Device) error {
	log.Info("deleteLogicalPort")
	var err error
	if err = dMgr.logicalDeviceMgr.DeleteLogicalPort(nil, cDevice); err != nil {
		log.Warnw("deleteLogical-port-error", log.Fields{"deviceId": cDevice.Id})
		return err
	}
	//// Remove the logical device Id from the parent device
	//logicalId := ""
	//dMgr.UpdateDeviceAttribute(cDevice.Id, "ParentId", logicalId)
	return nil
}

func (dMgr *DeviceManager) getParentDevice(childDevice *voltha.Device) *voltha.Device {
	//	Sanity check
	if childDevice.Root {
		// childDevice is the parent device
		return childDevice
	}
	parentDevice, _ := dMgr.getDevice(childDevice.ParentId)
	return parentDevice
}

func (dMgr *DeviceManager) disableAllChildDevices(cDevice *voltha.Device) error {
	log.Debug("disableAllChildDevices")
	var childDeviceIds []string
	var err error
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(cDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", cDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-device", log.Fields{"deviceId": cDevice.Id})
	}
	for _, childDeviceId := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(childDeviceId); agent != nil {
			if err = agent.disableDevice(nil); err != nil {
				log.Errorw("failure-disable-device", log.Fields{"deviceId": childDeviceId, "error": err.Error()})
			}
		}
	}
	return nil
}

func (dMgr *DeviceManager) getAllChildDeviceIds(cDevice *voltha.Device) ([]string, error) {
	log.Info("getAllChildDeviceIds")
	// Get latest device info
	var device *voltha.Device
	var err error
	if device, err = dMgr.getDevice(cDevice.Id); err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", cDevice.Id)
	}
	childDeviceIds := make([]string, 0)
	for _, port := range device.Ports {
		for _, peer := range port.Peers {
			childDeviceIds = append(childDeviceIds, peer.DeviceId)
		}
	}
	return childDeviceIds, nil
}

func (dMgr *DeviceManager) addUNILogicalPort(cDevice *voltha.Device) error {
	log.Info("addUNILogicalPort")
	if err := dMgr.logicalDeviceMgr.AddUNILogicalPort(nil, cDevice); err != nil {
		log.Warnw("addUNILogicalPort-error", log.Fields{"device": cDevice, "err": err})
		return err
	}
	return nil
}

func (dMgr *DeviceManager) activateDevice(cDevice *voltha.Device) error {
	log.Info("activateDevice")
	return nil
}

func (dMgr *DeviceManager) disableDeviceHandler(cDevice *voltha.Device) error {
	log.Info("disableDevice-donothing")
	return nil
}

func (dMgr *DeviceManager) abandonDevice(cDevice *voltha.Device) error {
	log.Info("abandonDevice")
	return nil
}

func (dMgr *DeviceManager) reEnableDevice(cDevice *voltha.Device) error {
	log.Info("reEnableDevice")
	return nil
}

func (dMgr *DeviceManager) noOp(cDevice *voltha.Device) error {
	log.Info("noOp")
	return nil
}

func (dMgr *DeviceManager) notAllowed(pcDevice *voltha.Device) error {
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
		log.Infow("GetParentDeviceId", log.Fields{"deviceId": device.Id, "parentId": device.ParentId})
		return &device.ParentId
	}
	return nil
}
