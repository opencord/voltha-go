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

type DeviceManager struct {
	deviceAgents        map[string]*DeviceAgent
	logicalDeviceMgr    *LogicalDeviceManager
	clusterDataProxy    *model.Proxy
	coreInstanceId      string
	exitChannel         chan int
	lockDeviceAgentsMap sync.RWMutex
}

func newDeviceManager(cdProxy *model.Proxy, coreInstanceId string) *DeviceManager {
	var deviceMgr DeviceManager
	deviceMgr.exitChannel = make(chan int, 1)
	deviceMgr.deviceAgents = make(map[string]*DeviceAgent)
	deviceMgr.coreInstanceId = coreInstanceId
	deviceMgr.clusterDataProxy = cdProxy
	deviceMgr.lockDeviceAgentsMap = sync.RWMutex{}
	return &deviceMgr
}

func (dMgr *DeviceManager) start(ctx context.Context, logicalDeviceMgr *LogicalDeviceManager) {
	log.Info("starting-device-manager")
	dMgr.logicalDeviceMgr = logicalDeviceMgr
	log.Info("device-manager-started")
}

func (dMgr *DeviceManager) stop(ctx context.Context) {
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

func (dMgr *DeviceManager) deleteDeviceAgentToMap(agent *DeviceAgent) {
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	delete(dMgr.deviceAgents, agent.deviceId)
}

func (dMgr *DeviceManager) getDeviceAgent(deviceId string) *DeviceAgent {
	// TODO If the device is not in memory it needs to be loaded first
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	if agent, ok := dMgr.deviceAgents[deviceId]; ok {
		return agent
	}
	return nil
}

func (dMgr *DeviceManager) listDeviceIdsFromMap() *voltha.IDs {
	dMgr.lockDeviceAgentsMap.Lock()
	defer dMgr.lockDeviceAgentsMap.Unlock()
	result := &voltha.IDs{Items: make([]*voltha.ID, 0)}
	for key, _ := range dMgr.deviceAgents {
		result.Items = append(result.Items, &voltha.ID{Id: key})
	}
	return result
}

func (dMgr *DeviceManager) GetDevice(id string) (*voltha.Device, error) {
	log.Debugw("GetDevice", log.Fields{"deviceid": id})
	if agent := dMgr.getDeviceAgent(id); agent != nil {
		return agent.getDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (dMgr *DeviceManager) IsRootDevice(id string) (bool, error) {
	device, err := dMgr.GetDevice(id)
	if err != nil {
		return false, err
	}
	return device.Root, nil
}

// GetDevice retrieves the latest device information from the data model
func (dMgr *DeviceManager) ListDevices() (*voltha.Devices, error) {
	log.Debug("ListDevices")
	result := &voltha.Devices{}
	if devices := dMgr.clusterDataProxy.Get("/devices", 0, false, ""); devices != nil {
		for _, device := range devices.([]interface{}) {
			if agent := dMgr.getDeviceAgent(device.(*voltha.Device).Id); agent == nil {
				agent = newDeviceAgent(device.(*voltha.Device), dMgr, dMgr.clusterDataProxy)
				dMgr.addDeviceAgentToMap(agent)
				agent.start(nil)
			}
			result.Items = append(result.Items, device.(*voltha.Device))
		}
	}
	return result, nil
}

// ListDeviceIds retrieves the latest device IDs information from the data model (memory data only)
func (dMgr *DeviceManager) ListDeviceIds() (*voltha.IDs, error) {
	log.Debug("ListDeviceIDs")
	// Report only device IDs that are in the device agent map
	return dMgr.listDeviceIdsFromMap(), nil
}

//ReconcileDevices is a request to a voltha core to managed a list of devices based on their IDs
func (dMgr *DeviceManager) ReconcileDevices(ctx context.Context, ids *voltha.IDs, ch chan interface{}) {
	log.Debug("ReconcileDevices")
	var res interface{}
	if ids != nil {
		toReconcile := len(ids.Items)
		reconciled := 0
		for _, id := range ids.Items {
			//	 Act on the device only if its not present in the agent map
			if agent := dMgr.getDeviceAgent(id.Id); agent == nil {
				//	Device Id not in memory
				log.Debugw("reconciling-device", log.Fields{"id": id.Id})
				// Load device from model
				if device := dMgr.clusterDataProxy.Get("/devices/"+id.Id, 0, false, ""); device != nil {
					agent = newDeviceAgent(device.(*voltha.Device), dMgr, dMgr.clusterDataProxy)
					dMgr.addDeviceAgentToMap(agent)
					agent.start(nil)
					reconciled += 1
				} else {
					log.Warnw("device-inexistent", log.Fields{"id": id.Id})
				}
			} else {
				reconciled += 1
			}
		}
		if toReconcile != reconciled {
			res = status.Errorf(codes.DataLoss, "less-device-reconciled:%d/%d", reconciled, toReconcile)
		}
	} else {
		res = status.Errorf(codes.InvalidArgument, "empty-list-of-ids")
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) getPorts(ctx context.Context, deviceId string, portType voltha.Port_PortType) (*voltha.Ports, error) {
	log.Debugw("getPorts", log.Fields{"deviceid": deviceId, "portType": portType})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.getPorts(ctx, portType), nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) ListDevicePorts(ctx context.Context, deviceId string) (*voltha.Ports, error) {
	log.Debugw("ListDevicePorts", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.ListDevicePorts(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) ListDevicePmConfigs(ctx context.Context, deviceId string) (*voltha.PmConfigs, error) {
	log.Debugw("ListDevicePmConfigs", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.ListDevicePmConfigs(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) ListDeviceFlows(ctx context.Context, deviceId string) (*voltha.Flows, error) {
	log.Debugw("ListDeviceFlows", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.ListDeviceFlows(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) ListDeviceFlowGroups(ctx context.Context, deviceId string) (*voltha.FlowGroups, error) {
	log.Debugw("ListDeviceFlowGroups", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.ListDeviceFlowGroups(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) GetImageDownloadStatus(ctx context.Context, deviceId string, imageName string) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownloadStatus", log.Fields{"deviceid": deviceId, "imagename": imageName})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.GetImageDownloadStatus(ctx, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) GetImageDownload(ctx context.Context, deviceId string, imageName string) ( *voltha.ImageDownload, error) {
	log.Debugw("GetImageDownload", log.Fields{"deviceid": deviceId, "imagename": imageName})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.GetImageDownload(ctx, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) ListImageDownloads(ctx context.Context, deviceId string) ( *voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.ListImageDownloads(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) GetImages(ctx context.Context, deviceId string) ( *voltha.Images, error) {
	log.Debugw("GetImages", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.GetImages(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) getParentDevice(childDevice *voltha.Device) *voltha.Device {
	//	Sanity check
	if childDevice.Root {
		// childDevice is the parent device
		return childDevice
	}
	parentDevice, _ := dMgr.GetDevice(childDevice.ParentId)
	return parentDevice
}
