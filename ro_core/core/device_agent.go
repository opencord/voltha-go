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
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DeviceAgent holds device specific information
type DeviceAgent struct {
	deviceID         string
	deviceType       string
	lastData         *voltha.Device
	deviceMgr        *DeviceManager
	clusterDataProxy *model.Proxy
	exitChannel      chan int
	lockDevice       sync.RWMutex
}

//newDeviceAgent creates a new device agent along as creating a unique ID for the device and set the device state to
//preprovisioning
func newDeviceAgent(device *voltha.Device, deviceMgr *DeviceManager, cdProxy *model.Proxy) *DeviceAgent {
	var agent DeviceAgent
	agent.deviceID = device.Id
	agent.deviceType = device.Type
	agent.lastData = device
	agent.deviceMgr = deviceMgr
	agent.exitChannel = make(chan int, 1)
	agent.clusterDataProxy = cdProxy
	agent.lockDevice = sync.RWMutex{}
	return &agent
}

// start save the device to the data model and registers for callbacks on that device
func (agent *DeviceAgent) start(ctx context.Context, loadFromDb bool) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("starting-device-agent", log.Fields{"device": agent.lastData})
	if loadFromDb {
		if device, err := agent.clusterDataProxy.Get(ctx, "/devices/"+agent.deviceID, 0, false, ""); err != nil {
			log.Errorw("failed-to-get-device", log.Fields{"error": err})
			return err
		} else if device != nil {
			if d, ok := device.(*voltha.Device); ok {
				agent.lastData = proto.Clone(d).(*voltha.Device)
			}
		} else {
			log.Errorw("failed-to-load-device", log.Fields{"deviceID": agent.deviceID})
			return status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
		}
		log.Debugw("device-loaded-from-dB", log.Fields{"device": agent.lastData})
	}
	log.Debug("device-agent-started")
	return nil
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
	if device, err := agent.clusterDataProxy.Get(context.Background(), "/devices/"+agent.deviceID, 0, false, ""); err != nil {
		log.Errorw("failed-to-get-device", log.Fields{"error": err})
		return nil, err
	} else if device != nil {
		if d, ok := device.(*voltha.Device); ok {
			cloned := proto.Clone(d).(*voltha.Device)
			return cloned, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// ListDevicePorts retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDevicePorts(ctx context.Context) (*voltha.Ports, error) {
	log.Debugw("ListDevicePorts", log.Fields{"id": agent.deviceID})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		ports.Items = append(ports.Items, device.GetPorts()...)
		return ports, nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// ListDevicePmConfigs retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDevicePmConfigs(ctx context.Context) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		return device.GetPmConfigs(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// ListDeviceFlows retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDeviceFlows(ctx context.Context) (*voltha.Flows, error) {
	log.Debugw("ListDeviceFlows", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		return device.GetFlows(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// ListDeviceFlowGroups retrieves the ports information for a particular device.
func (agent *DeviceAgent) ListDeviceFlowGroups(ctx context.Context) (*voltha.FlowGroups, error) {
	log.Debugw("ListDeviceFlowGroups", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		return device.GetFlowGroups(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// GetImageDownloadStatus retrieves the download status of an image of a particular device.
func (agent *DeviceAgent) GetImageDownloadStatus(ctx context.Context, imageName string) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownloadStatus", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		for _, img := range device.GetImageDownloads() {
			if img.GetName() == imageName {
				return img, nil
			}
		}
		return nil, status.Errorf(codes.NotFound, "device-%s, image-%s", agent.deviceID, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// GetImageDownload retrieves the image download for a particular device.
func (agent *DeviceAgent) GetImageDownload(ctx context.Context, imageName string) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownload", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		for _, img := range device.GetImageDownloads() {
			if img.GetName() == imageName {
				return img, nil
			}
		}
		return nil, status.Errorf(codes.NotFound, "device-%s, image-%s", agent.deviceID, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// ListImageDownloads retrieves the image downloads for a particular device.
func (agent *DeviceAgent) ListImageDownloads(ctx context.Context) (*voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		return &voltha.ImageDownloads{Items: device.GetImageDownloads()}, nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}

// GetImages retrieves the list of images for a particular device.
func (agent *DeviceAgent) GetImages(ctx context.Context) (*voltha.Images, error) {
	log.Debugw("GetImages", log.Fields{"id": agent.deviceID})
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceID); device != nil {
		return device.GetImages(), nil
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceID)
}
