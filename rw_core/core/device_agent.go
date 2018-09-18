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
	"github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
)

type DeviceAgent struct {
	deviceId       string
	lastData       *voltha.Device
	adapterProxy   *AdapterProxy
	deviceMgr      *DeviceManager
	localDataProxy *model.Proxy
	exitChannel    chan int
}

func newDeviceAgent(ap *AdapterProxy, device *voltha.Device, deviceMgr *DeviceManager, ldProxy *model.Proxy) *DeviceAgent {
	var agent DeviceAgent
	device.Id = CreateDeviceId()
	agent.deviceId = device.Id
	agent.adapterProxy = ap
	agent.lastData = device
	agent.deviceMgr = deviceMgr
	agent.exitChannel = make(chan int, 1)
	agent.localDataProxy = ldProxy
	return &agent
}

func (agent *DeviceAgent) start(ctx context.Context) {
	log.Debugw("starting-device-agent", log.Fields{"device": agent.lastData})
	// Add the initial device to the local model
	if added := agent.localDataProxy.Add("/devices", agent.lastData, ""); added == nil {
		log.Errorw("failed-to-add-device", log.Fields{"deviceId": agent.deviceId})
	}
	log.Debug("device-agent-started")
}

func (agent *DeviceAgent) Stop(ctx context.Context) {
	log.Debug("stopping-device-agent")
	agent.exitChannel <- 1
	log.Debug("device-agent-stopped")
}

func (agent *DeviceAgent) enableDevice(ctx context.Context) error {
	log.Debugw("enableDevice", log.Fields{"id": agent.lastData.Id, "device": agent.lastData})
	// Update the device status
	if device, err := agent.deviceMgr.getDevice(agent.deviceId); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		cloned := reflect.ValueOf(device).Elem().Interface().(voltha.Device)
		cloned.AdminState = voltha.AdminState_ENABLED
		cloned.OperStatus = voltha.OperStatus_ACTIVATING
		if afterUpdate := agent.localDataProxy.Update("/devices/"+agent.deviceId, &cloned, false, ""); afterUpdate == nil {
			return status.Errorf(codes.Internal, "failed-update-device:%s", agent.deviceId)
		} else {
			if err := agent.adapterProxy.AdoptDevice(ctx, &cloned); err != nil {
				log.Debugw("enableDevice-error", log.Fields{"id": agent.lastData.Id, "error": err})
				return err
			}
			agent.lastData = &cloned
		}
	}
	return nil
}

func (agent *DeviceAgent) getNNIPorts(ctx context.Context) *voltha.Ports {
	log.Debugw("getNNIPorts", log.Fields{"id": agent.deviceId})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.getDevice(agent.deviceId); device != nil {
		for _, port := range device.Ports {
			if port.Type == voltha.Port_ETHERNET_NNI {
				ports.Items = append(ports.Items, port)
			}
		}
	}
	return ports
}

func (agent *DeviceAgent) getSwitchCapability(ctx context.Context) (*core_adapter.SwitchCapability, error) {
	log.Debugw("getSwitchCapability", log.Fields{"deviceId": agent.deviceId})
	if device, err := agent.deviceMgr.getDevice(agent.deviceId); device == nil {
		return nil, err
	} else {
		var switchCap *core_adapter.SwitchCapability
		var err error
		if switchCap, err = agent.adapterProxy.GetOfpDeviceInfo(ctx, device); err != nil {
			log.Debugw("getSwitchCapability-error", log.Fields{"id": device.Id, "error": err})
			return nil, err
		}
		return switchCap, nil
	}
}

func (agent *DeviceAgent) getPortCapability(ctx context.Context, portNo uint32) (*core_adapter.PortCapability, error) {
	log.Debugw("getPortCapability", log.Fields{"deviceId": agent.deviceId})
	if device, err := agent.deviceMgr.getDevice(agent.deviceId); device == nil {
		return nil, err
	} else {
		var portCap *core_adapter.PortCapability
		var err error
		if portCap, err = agent.adapterProxy.GetOfpPortInfo(ctx, device, portNo); err != nil {
			log.Debugw("getPortCapability-error", log.Fields{"id": device.Id, "error": err})
			return nil, err
		}
		return portCap, nil
	}
}

func (agent *DeviceAgent) updateDevice(device *voltha.Device) error {
	log.Debugw("updateDevice", log.Fields{"deviceId": device.Id})
	// Get the dev info from the model
	if storedData, err := agent.deviceMgr.getDevice(device.Id); err != nil {
		return status.Errorf(codes.NotFound, "%s", device.Id)
	} else {
		// store the changed data
		cloned := (proto.Clone(device)).(*voltha.Device)
		afterUpdate := agent.localDataProxy.Update("/devices/"+device.Id, cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", device.Id)
		}
		// Perform the state transition
		if err := agent.deviceMgr.processTransition(storedData, cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": device.Id, "error": err})
			return err
		}
		return nil
	}
}

func (agent *DeviceAgent) updateDeviceState(operState *core_adapter.IntType, connState *core_adapter.IntType) error {
	// Work only on latest data
	if storeDevice, err := agent.deviceMgr.getDevice(agent.deviceId); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := reflect.ValueOf(storeDevice).Elem().Interface().(voltha.Device)
		if operState != nil {
			cloned.OperStatus = voltha.OperStatus_OperStatus(operState.Val)
		}
		if connState != nil {
			cloned.ConnectStatus = voltha.ConnectStatus_ConnectStatus(connState.Val)
		}
		log.Debugw("DeviceStateUpdate-device", log.Fields{"device": cloned})
		// Store the device
		if afterUpdate := agent.localDataProxy.Update("/devices/"+agent.deviceId, &cloned, false, ""); afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		// Perform the state transition
		if err := agent.deviceMgr.processTransition(storeDevice, &cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": agent.deviceId, "error": err})
			return err
		}
		return nil
	}
}

func (agent *DeviceAgent) updatePmConfigs(pmConfigs *voltha.PmConfigs) error {
	log.Debug("updatePmConfigs")
	// Work only on latest data
	if storeDevice, err := agent.deviceMgr.getDevice(agent.deviceId); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := reflect.ValueOf(storeDevice).Elem().Interface().(voltha.Device)
		cp := proto.Clone(pmConfigs)
		cloned.PmConfigs = cp.(*voltha.PmConfigs)
		// Store the device
		afterUpdate := agent.localDataProxy.Update("/devices/"+agent.deviceId, &cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		return nil
	}
}

func (agent *DeviceAgent) addPort(port *voltha.Port) error {
	log.Debug("addPort")
	// Work only on latest data
	if storeDevice, err := agent.deviceMgr.getDevice(agent.deviceId); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := reflect.ValueOf(storeDevice).Elem().Interface().(voltha.Device)
		if cloned.Ports == nil {
			//	First port
			cloned.Ports = make([]*voltha.Port, 0)
		}
		cp := proto.Clone(port)
		cloned.Ports = append(cloned.Ports, cp.(*voltha.Port))
		// Store the device
		afterUpdate := agent.localDataProxy.Update("/devices/"+agent.deviceId, &cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		return nil
	}
}

// TODO: A generic device update by attribute
func (agent *DeviceAgent) updateDeviceAttribute(name string, value interface{}) {
	if value == nil {
		return
	}
	var storeDevice *voltha.Device
	var err error
	if storeDevice, err = agent.deviceMgr.getDevice(agent.deviceId); err != nil {
		return
	}
	updated := false
	s := reflect.ValueOf(storeDevice).Elem()
	if s.Kind() == reflect.Struct {
		// exported field
		f := s.FieldByName(name)
		if f.IsValid() && f.CanSet() {
			switch f.Kind() {
			case reflect.String:
				f.SetString(value.(string))
				updated = true
			case reflect.Uint32:
				f.SetUint(uint64(value.(uint32)))
				updated = true
			case reflect.Bool:
				f.SetBool(value.(bool))
				updated = true
			}
		}
	}
	log.Debugw("update-field-status", log.Fields{"device": storeDevice, "name": name, "updated": updated})
	//	Save the data
	cloned := reflect.ValueOf(storeDevice).Elem().Interface().(voltha.Device)
	if afterUpdate := agent.localDataProxy.Update("/devices/"+agent.deviceId, &cloned, false, ""); afterUpdate == nil {
		log.Warnw("attribute-update-failed", log.Fields{"attribute": name, "value": value})
	}
	return
}
