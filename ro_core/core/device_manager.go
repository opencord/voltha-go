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
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/probe"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type DeviceManager struct {
	deviceAgents     sync.Map
	logicalDeviceMgr *LogicalDeviceManager
	clusterDataProxy *model.Proxy
	coreInstanceId   string
	exitChannel      chan int
}

func newDeviceManager(cdProxy *model.Proxy, coreInstanceId string) *DeviceManager {
	var deviceMgr DeviceManager
	deviceMgr.exitChannel = make(chan int, 1)
	deviceMgr.coreInstanceId = coreInstanceId
	deviceMgr.clusterDataProxy = cdProxy
	return &deviceMgr
}

func (dMgr *DeviceManager) start(ctx context.Context, logicalDeviceMgr *LogicalDeviceManager) {
	log.Info("starting-device-manager")
	dMgr.logicalDeviceMgr = logicalDeviceMgr
	probe.UpdateStatusFromContext(ctx, "device-manager", probe.ServiceStatusRunning)
	log.Info("device-manager-started")
}

func (dMgr *DeviceManager) stop(ctx context.Context) {
	log.Info("stopping-device-manager")
	dMgr.exitChannel <- 1
	probe.UpdateStatusFromContext(ctx, "device-manager", probe.ServiceStatusStopped)
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
	if _, exist := dMgr.deviceAgents.Load(agent.deviceId); !exist {
		dMgr.deviceAgents.Store(agent.deviceId, agent)
	}
}

func (dMgr *DeviceManager) deleteDeviceAgentToMap(agent *DeviceAgent) {
	dMgr.deviceAgents.Delete(agent.deviceId)
}

func (dMgr *DeviceManager) getDeviceAgent(deviceId string) *DeviceAgent {
	if agent, ok := dMgr.deviceAgents.Load(deviceId); ok {
		return agent.(*DeviceAgent)
	} else {
		//	Try to load into memory - loading will also create the device agent
		if err := dMgr.load(deviceId); err == nil {
			if agent, ok = dMgr.deviceAgents.Load(deviceId); ok {
				return agent.(*DeviceAgent)
			}
		}
	}
	return nil
}

// listDeviceIdsFromMap returns the list of device IDs that are in memory
func (dMgr *DeviceManager) listDeviceIdsFromMap() *voltha.IDs {
	result := &voltha.IDs{Items: make([]*voltha.ID, 0)}
	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		result.Items = append(result.Items, &voltha.ID{Id: key.(string)})
		return true
	})
	return result
}

// GetDevice will returns a device, either from memory or from the dB, if present
func (dMgr *DeviceManager) GetDevice(id string) (*voltha.Device, error) {
	log.Debugw("GetDevice", log.Fields{"deviceid": id})
	if agent := dMgr.getDeviceAgent(id); agent != nil {
		return agent.getDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (dMgr *DeviceManager) IsDeviceInCache(id string) bool {
	_, exist := dMgr.deviceAgents.Load(id)
	return exist
}

func (dMgr *DeviceManager) IsRootDevice(id string) (bool, error) {
	device, err := dMgr.GetDevice(id)
	if err != nil {
		return false, err
	}
	return device.Root, nil
}

// ListDevices retrieves the latest devices from the data model
func (dMgr *DeviceManager) ListDevices() (*voltha.Devices, error) {
	log.Debug("ListDevices")
	result := &voltha.Devices{}
	if devices, err := dMgr.clusterDataProxy.List(context.Background(), "/devices", 0, false, ""); err != nil {
		log.Errorw("failed-to-list-devices", log.Fields{"error": err})
		return nil, err
	} else if devices != nil {
		for _, device := range devices.([]interface{}) {
			// If device is not in memory then set it up
			if !dMgr.IsDeviceInCache(device.(*voltha.Device).Id) {
				agent := newDeviceAgent(device.(*voltha.Device), dMgr, dMgr.clusterDataProxy)
				if err := agent.start(nil, true); err != nil {
					log.Warnw("failure-starting-agent", log.Fields{"deviceId": device.(*voltha.Device).Id})
					agent.stop(nil)
				} else {
					dMgr.addDeviceAgentToMap(agent)
				}
			}
			result.Items = append(result.Items, device.(*voltha.Device))
		}
	}
	return result, nil
}

// loadDevice loads the deviceId in memory, if not present
func (dMgr *DeviceManager) loadDevice(deviceId string) (*DeviceAgent, error) {
	log.Debugw("loading-device", log.Fields{"deviceId": deviceId})
	// Sanity check
	if deviceId == "" {
		return nil, status.Error(codes.InvalidArgument, "deviceId empty")
	}
	if !dMgr.IsDeviceInCache(deviceId) {
		agent := newDeviceAgent(&voltha.Device{Id: deviceId}, dMgr, dMgr.clusterDataProxy)
		if err := agent.start(nil, true); err != nil {
			agent.stop(nil)
			return nil, err
		}
		dMgr.addDeviceAgentToMap(agent)
	}
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent, nil
	}
	return nil, status.Error(codes.NotFound, deviceId) // This should nto happen
}

// loadRootDeviceParentAndChildren loads the children and parents of a root device in memory
func (dMgr *DeviceManager) loadRootDeviceParentAndChildren(device *voltha.Device) error {
	log.Debugw("loading-parent-and-children", log.Fields{"deviceId": device.Id})
	if device.Root {
		// Scenario A
		if device.ParentId != "" {
			//	 Load logical device if needed.
			if err := dMgr.logicalDeviceMgr.load(device.ParentId); err != nil {
				log.Warnw("failure-loading-logical-device", log.Fields{"lDeviceId": device.ParentId})
			}
		} else {
			log.Debugw("no-parent-to-load", log.Fields{"deviceId": device.Id})
		}
		//	Load all child devices, if needed
		if childDeviceIds, err := dMgr.getAllChildDeviceIds(device); err == nil {
			for _, childDeviceId := range childDeviceIds {
				if _, err := dMgr.loadDevice(childDeviceId); err != nil {
					log.Warnw("failure-loading-device", log.Fields{"deviceId": childDeviceId})
					return err
				}
			}
			log.Debugw("loaded-children", log.Fields{"deviceId": device.Id, "numChildren": len(childDeviceIds)})
		} else {
			log.Debugw("no-child-to-load", log.Fields{"deviceId": device.Id})
		}
	}
	return nil
}

// load loads the deviceId in memory, if not present, and also loads its accompanying parents and children.  Loading
// in memory is for improved performance.  It is not imperative that a device needs to be in memory when a request
// acting on the device is received by the core. In such a scenario, the Core will load the device in memory first
// and the proceed with the request.
func (dMgr *DeviceManager) load(deviceId string) error {
	log.Debug("load...")
	// First load the device - this may fail in case the device was deleted intentionally by the other core
	var dAgent *DeviceAgent
	var err error
	if dAgent, err = dMgr.loadDevice(deviceId); err != nil {
		log.Warnw("failure-loading-device", log.Fields{"deviceId": deviceId})
		return err
	}
	// Get the loaded device details
	var device *voltha.Device
	if device, err = dAgent.getDevice(); err != nil {
		return err
	}

	// If the device is in Pre-provisioning or deleted state stop here
	if device.AdminState == voltha.AdminState_PREPROVISIONED || device.AdminState == voltha.AdminState_DELETED {
		return nil
	}

	// Now we face two scenarios
	if device.Root {
		// Load all children as well as the parent of this device (logical_device)
		if err := dMgr.loadRootDeviceParentAndChildren(device); err != nil {
			log.Warnw("failure-loading-device-parent-and-children", log.Fields{"deviceId": deviceId})
			return err
		}
		log.Debugw("successfully-loaded-parent-and-children", log.Fields{"deviceId": deviceId})
	} else {
		//	Scenario B - use the parentId of that device (root device) to trigger the loading
		if device.ParentId != "" {
			return dMgr.load(device.ParentId)
		}
	}
	return nil
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
			if !dMgr.IsDeviceInCache(id.Id) {
				//	Device Id not in memory
				log.Debugw("reconciling-device", log.Fields{"id": id.Id})
				// Load device from dB
				agent := newDeviceAgent(&voltha.Device{Id: id.Id}, dMgr, dMgr.clusterDataProxy)
				if err := agent.start(nil, true); err != nil {
					log.Warnw("failure-loading-device", log.Fields{"deviceId": id.Id})
					agent.stop(nil)
				} else {
					dMgr.addDeviceAgentToMap(agent)
					reconciled += 1
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

func (dMgr *DeviceManager) GetImageDownload(ctx context.Context, deviceId string, imageName string) (*voltha.ImageDownload, error) {
	log.Debugw("GetImageDownload", log.Fields{"deviceid": deviceId, "imagename": imageName})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.GetImageDownload(ctx, imageName)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) ListImageDownloads(ctx context.Context, deviceId string) (*voltha.ImageDownloads, error) {
	log.Debugw("ListImageDownloads", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.ListImageDownloads(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)

}

func (dMgr *DeviceManager) GetImages(ctx context.Context, deviceId string) (*voltha.Images, error) {
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

//getAllChildDeviceIds is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *DeviceManager) getAllChildDeviceIds(parentDevice *voltha.Device) ([]string, error) {
	log.Debugw("getAllChildDeviceIds", log.Fields{"parentDeviceId": parentDevice.Id})
	childDeviceIds := make([]string, 0)
	if parentDevice != nil {
		for _, port := range parentDevice.Ports {
			for _, peer := range port.Peers {
				childDeviceIds = append(childDeviceIds, peer.DeviceId)
			}
		}
	}
	return childDeviceIds, nil
}
