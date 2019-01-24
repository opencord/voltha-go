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
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	ic "github.com/opencord/voltha-go/protos/inter_container"
	ofp "github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type DeviceAgent struct {
	deviceId         string
	deviceType       string
	lastData         *voltha.Device
	adapterProxy     *AdapterProxy
	deviceMgr        *DeviceManager
	clusterDataProxy *model.Proxy
	deviceProxy      *model.Proxy
	exitChannel      chan int
	flowProxy        *model.Proxy
	groupProxy       *model.Proxy
	lockDevice       sync.RWMutex
}

//newDeviceAgent creates a new device agent along as creating a unique ID for the device and set the device state to
//preprovisioning
func newDeviceAgent(ap *AdapterProxy, device *voltha.Device, deviceMgr *DeviceManager, cdProxy *model.Proxy) *DeviceAgent {
	var agent DeviceAgent
	agent.adapterProxy = ap
	cloned := (proto.Clone(device)).(*voltha.Device)
	if cloned.Id == "" {
		cloned.Id = CreateDeviceId()
	}
	cloned.AdminState = voltha.AdminState_PREPROVISIONED
	cloned.FlowGroups = &ofp.FlowGroups{Items: nil}
	cloned.Flows = &ofp.Flows{Items: nil}
	if !device.GetRoot() && device.ProxyAddress != nil {
		// Set the default vlan ID to the one specified by the parent adapter.  It can be
		// overwritten by the child adapter during a device update request
		cloned.Vlan = device.ProxyAddress.ChannelId
	}
	agent.deviceId = cloned.Id
	agent.deviceType = cloned.Type
	agent.lastData = cloned
	agent.deviceMgr = deviceMgr
	agent.exitChannel = make(chan int, 1)
	agent.clusterDataProxy = cdProxy
	agent.lockDevice = sync.RWMutex{}
	return &agent
}

// start save the device to the data model and registers for callbacks on that device
func (agent *DeviceAgent) start(ctx context.Context) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("starting-device-agent", log.Fields{"device": agent.lastData})
	// Add the initial device to the local model
	if added := agent.clusterDataProxy.AddWithID("/devices", agent.deviceId, agent.lastData, ""); added == nil {
		log.Errorw("failed-to-add-device", log.Fields{"deviceId": agent.deviceId})
	}
	agent.deviceProxy = agent.clusterDataProxy.Root.CreateProxy("/devices/"+agent.deviceId, false)

	agent.flowProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/devices/%s/flows", agent.deviceId),
		false)
	agent.groupProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/devices/%s/flow_groups", agent.deviceId),
		false)

	log.Debug("device-agent-started")
}

// stop stops the device agent.  Not much to do for now
func (agent *DeviceAgent) stop(ctx context.Context) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debug("stopping-device-agent")
	agent.exitChannel <- 1
	log.Debug("device-agent-stopped")
}

// GetDevice retrieves the latest device information from the data model
func (agent *DeviceAgent) getDevice() (*voltha.Device, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	if device := agent.clusterDataProxy.Get("/devices/"+agent.deviceId, 1, false, ""); device != nil {
		if d, ok := device.(*voltha.Device); ok {
			cloned := proto.Clone(d).(*voltha.Device)
			return cloned, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// getDeviceWithoutLock is a helper function to be used ONLY by any device agent function AFTER it has acquired the device lock.
// This function is meant so that we do not have duplicate code all over the device agent functions
func (agent *DeviceAgent) getDeviceWithoutLock() (*voltha.Device, error) {
	if device := agent.clusterDataProxy.Get("/devices/"+agent.deviceId, 1, false, ""); device != nil {
		if d, ok := device.(*voltha.Device); ok {
			cloned := proto.Clone(d).(*voltha.Device)
			return cloned, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// getPorts retrieves the ports information of the device based on the port type.
func (agent *DeviceAgent) getPorts(ctx context.Context, portType voltha.Port_PortType) *voltha.Ports {
	log.Debugw("getPorts", log.Fields{"id": agent.deviceId, "portType": portType})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		for _, port := range device.Ports {
			if port.Type == portType {
				ports.Items = append(ports.Items, port)
			}
		}
	}
	return ports
}

// ListDevicePorts retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDevicePorts(ctx context.Context) (*voltha.Ports, error) {
	log.Debugw("ListDevicePorts", log.Fields{"id": agent.deviceId})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		for _, entry := range device.GetPorts() {
			ports.Items = append(ports.Items, entry)
		}
		return ports, nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// ListDevicePmConfigs retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDevicePmConfigs(ctx context.Context) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		return device.GetPmConfigs(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// ListDeviceFlows retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDeviceFlows(ctx context.Context) (*voltha.Flows, error) {
	log.Debugw("ListDeviceFlows", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		return device.GetFlows(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// ListDeviceFlows retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDeviceFlowGroups(ctx context.Context) (*voltha.FlowGroups, error) {
	log.Debugw("ListDeviceFlowGroups", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		return device.GetFlowGroups(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// GetImageDownloadStatus retrieves the download status of an image of a particular device.
func (agent *DeviceAgent) GetImageDownloadStatus(ctx context.Context, imageName string) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownloadStatus", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		for _, img := range device.GetImageDownloads() {
			if img.GetName() == imageName {
				return img, nil
			}
		}
		return nil, status.Errorf(codes.NotFound, "device-%s, image-%s", agent.deviceId, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// GetImageDownload retrieves the image download for a particular device.
func (agent *DeviceAgent) GetImageDownload(ctx context.Context, imageName string) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownload", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		for _, img := range device.GetImageDownloads() {
			if img.GetName() == imageName {
				return img, nil
			}
		}
		return nil, status.Errorf(codes.NotFound, "device-%s, image-%s", agent.deviceId, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// ListImageDownloads retrieves the image downloads for a particular device.
func (agent *DeviceAgent) ListImageDownloads(ctx context.Context) (*voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		return &voltha.ImageDownloads{Items: device.GetImageDownloads()}, nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// GetImages retrieves the list of images for a particular device.
func (agent *DeviceAgent) GetImages(ctx context.Context) (*voltha.Images, error) {
	log.Debugw("GetImages", log.Fields{"id": agent.deviceId})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		return device.GetImages(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// getSwitchCapability is a helper method that a logical device agent uses to retrieve the switch capability of a
// parent device
func (agent *DeviceAgent) getSwitchCapability(ctx context.Context) (*ic.SwitchCapability, error) {
	log.Debugw("getSwitchCapability", log.Fields{"deviceId": agent.deviceId})
	if device, err := agent.deviceMgr.GetDevice(agent.deviceId); device == nil {
		return nil, err
	} else {
		var switchCap *ic.SwitchCapability
		var err error
		if switchCap, err = agent.adapterProxy.GetOfpDeviceInfo(ctx, device); err != nil {
			log.Debugw("getSwitchCapability-error", log.Fields{"id": device.Id, "error": err})
			return nil, err
		}
		return switchCap, nil
	}
}

// getPortCapability is a helper method that a logical device agent uses to retrieve the port capability of a
// device
func (agent *DeviceAgent) getPortCapability(ctx context.Context, portNo uint32) (*ic.PortCapability, error) {
	log.Debugw("getPortCapability", log.Fields{"deviceId": agent.deviceId})
	if device, err := agent.deviceMgr.GetDevice(agent.deviceId); device == nil {
		return nil, err
	} else {
		var portCap *ic.PortCapability
		var err error
		if portCap, err = agent.adapterProxy.GetOfpPortInfo(ctx, device, portNo); err != nil {
			log.Debugw("getPortCapability-error", log.Fields{"id": device.Id, "error": err})
			return nil, err
		}
		return portCap, nil
	}
}