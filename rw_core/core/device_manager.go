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
	"github.com/opencord/voltha-go/common/probe"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-go/rw_core/utils"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"runtime"
	"sync"
)

type DeviceManager struct {
	deviceAgents            sync.Map
	rootDevices             map[string]bool
	lockRootDeviceMap       sync.RWMutex
	core                    *Core
	adapterProxy            *AdapterProxy
	adapterMgr              *AdapterManager
	logicalDeviceMgr        *LogicalDeviceManager
	kafkaICProxy            *kafka.InterContainerProxy
	stateTransitions        *TransitionMap
	clusterDataProxy        *model.Proxy
	coreInstanceId          string
	exitChannel             chan int
	defaultTimeout          int64
	devicesLoadingLock      sync.RWMutex
	deviceLoadingInProgress map[string][]chan int
}

func newDeviceManager(core *Core) *DeviceManager {
	var deviceMgr DeviceManager
	deviceMgr.core = core
	deviceMgr.exitChannel = make(chan int, 1)
	deviceMgr.rootDevices = make(map[string]bool)
	deviceMgr.kafkaICProxy = core.kmp
	deviceMgr.adapterProxy = NewAdapterProxy(core.kmp, core.config.CorePairTopic)
	deviceMgr.coreInstanceId = core.instanceId
	deviceMgr.clusterDataProxy = core.clusterDataProxy
	deviceMgr.adapterMgr = core.adapterMgr
	deviceMgr.lockRootDeviceMap = sync.RWMutex{}
	deviceMgr.defaultTimeout = core.config.DefaultCoreTimeout
	deviceMgr.devicesLoadingLock = sync.RWMutex{}
	deviceMgr.deviceLoadingInProgress = make(map[string][]chan int)
	return &deviceMgr
}

func (dMgr *DeviceManager) start(ctx context.Context, logicalDeviceMgr *LogicalDeviceManager) {
	log.Info("starting-device-manager")
	dMgr.logicalDeviceMgr = logicalDeviceMgr
	dMgr.stateTransitions = NewTransitionMap(dMgr)
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
	dMgr.lockRootDeviceMap.Lock()
	defer dMgr.lockRootDeviceMap.Unlock()
	dMgr.rootDevices[agent.deviceId] = agent.isRootdevice

}

func (dMgr *DeviceManager) deleteDeviceAgentFromMap(agent *DeviceAgent) {
	dMgr.deviceAgents.Delete(agent.deviceId)
	dMgr.lockRootDeviceMap.Lock()
	defer dMgr.lockRootDeviceMap.Unlock()
	delete(dMgr.rootDevices, agent.deviceId)
}

// getDeviceAgent returns the agent managing the device.  If the device is not in memory, it will loads it, if it exists
func (dMgr *DeviceManager) getDeviceAgent(deviceId string) *DeviceAgent {
	if agent, ok := dMgr.deviceAgents.Load(deviceId); ok {
		return agent.(*DeviceAgent)
	} else {
		//	Try to load into memory - loading will also create the device agent and set the device ownership
		if err := dMgr.load(deviceId); err == nil {
			if agent, ok = dMgr.deviceAgents.Load(deviceId); !ok {
				return nil
			} else {
				// Register this device for ownership tracking
				go dMgr.core.deviceOwnership.OwnedByMe(&utils.DeviceID{Id: deviceId})
				return agent.(*DeviceAgent)
			}
		} else {
			//TODO: Change the return params to return an error as well
			log.Errorw("loading-device-failed", log.Fields{"deviceId": deviceId, "error": err})
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

func (dMgr *DeviceManager) createDevice(ctx context.Context, device *voltha.Device, ch chan interface{}) {
	log.Debugw("createDevice", log.Fields{"device": device, "aproxy": dMgr.adapterProxy})

	// Ensure this device is set as root
	device.Root = true
	// Create and start a device agent for that device
	agent := newDeviceAgent(dMgr.adapterProxy, device, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
	dMgr.addDeviceAgentToMap(agent)
	agent.start(ctx, false)

	sendResponse(ctx, ch, agent.lastData)
}

func (dMgr *DeviceManager) enableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	log.Debugw("enableDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(id.Id); agent != nil {
		res = agent.enableDevice(ctx)
		log.Debugw("EnableDevice-result", log.Fields{"result": res})
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

func (dMgr *DeviceManager) rebootDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	log.Debugw("rebootDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(id.Id); agent != nil {
		res = agent.rebootDevice(ctx)
		log.Debugw("rebootDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) deleteDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	log.Debugw("deleteDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(id.Id); agent != nil {
		res = agent.deleteDevice(ctx)
		log.Debugw("deleteDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendResponse(ctx, ch, res)
}

// stopManagingDevice stops the management of the device as well as any of its reference device and logical device.
// This function is called only in the Core that does not own this device.  In the Core that owns this device then a
// deletion deletion also includes removal of any reference of this device.
func (dMgr *DeviceManager) stopManagingDevice(id string) {
	log.Infow("stopManagingDevice", log.Fields{"deviceId": id})
	if dMgr.IsDeviceInCache(id) { // Proceed only if an agent is present for this device
		if root, _ := dMgr.IsRootDevice(id); root == true {
			// stop managing the logical device
			ldeviceId := dMgr.logicalDeviceMgr.stopManagingLogicalDeviceWithDeviceId(id)
			if ldeviceId != "" { // Can happen if logical device agent was already stopped
				dMgr.core.deviceOwnership.AbandonDevice(ldeviceId)
			}
			// stop managing the child devices
			childDeviceIds := dMgr.getAllDeviceIdsWithDeviceParentId(id)
			for _, cId := range childDeviceIds {
				dMgr.stopManagingDevice(cId)
			}
		}
		if agent := dMgr.getDeviceAgent(id); agent != nil {
			agent.stop(nil)
			dMgr.deleteDeviceAgentFromMap(agent)
			// Abandon the device ownership
			dMgr.core.deviceOwnership.AbandonDevice(id)
		}
	}
}

func (dMgr *DeviceManager) RunPostDeviceDelete(cDevice *voltha.Device) error {
	log.Infow("RunPostDeviceDelete", log.Fields{"deviceId": cDevice.Id})
	dMgr.stopManagingDevice(cDevice.Id)
	return nil
}

// GetDevice will returns a device, either from memory or from the dB, if present
func (dMgr *DeviceManager) GetDevice(id string) (*voltha.Device, error) {
	log.Debugw("GetDevice", log.Fields{"deviceid": id})
	if agent := dMgr.getDeviceAgent(id); agent != nil {
		return agent.getDevice()
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (dMgr *DeviceManager) GetChildDevice(parentDeviceId string, serialNumber string, onuId int64, parentPortNo int64) (*voltha.Device, error) {
	log.Debugw("GetChildDevice", log.Fields{"parentDeviceid": parentDeviceId, "serialNumber": serialNumber,
		"parentPortNo": parentPortNo, "onuId": onuId})

	var parentDevice *voltha.Device
	var err error
	if parentDevice, err = dMgr.GetDevice(parentDeviceId); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	var childDeviceIds []string
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-devices", log.Fields{"parentDeviceId": parentDevice.Id, "serialNumber": serialNumber, "onuId": onuId})
		return nil, status.Errorf(codes.NotFound, "%s", parentDeviceId)
	}

	var foundChildDevice *voltha.Device
	for _, childDeviceId := range childDeviceIds {
		found := false
		if searchDevice, err := dMgr.GetDevice(childDeviceId); err == nil {

			foundOnuId := false
			if searchDevice.ProxyAddress.OnuId == uint32(onuId) {
				if searchDevice.ParentPortNo == uint32(parentPortNo) {
					log.Debugw("found-child-by-onuid", log.Fields{"parentDeviceId": parentDevice.Id, "onuId": onuId})
					foundOnuId = true
				}
			}

			foundSerialNumber := false
			if searchDevice.SerialNumber == serialNumber {
				log.Debugw("found-child-by-serialnumber", log.Fields{"parentDeviceId": parentDevice.Id, "serialNumber": serialNumber})
				foundSerialNumber = true
			}

			// if both onuId and serialNumber are provided both must be true for the device to be found
			// otherwise whichever one found a match is good enough
			if onuId > 0 && serialNumber != "" {
				found = foundOnuId && foundSerialNumber
			} else {
				found = foundOnuId || foundSerialNumber
			}

			if found == true {
				foundChildDevice = searchDevice
				break
			}
		}
	}

	if foundChildDevice != nil {
		log.Debugw("child-device-found", log.Fields{"parentDeviceId": parentDevice.Id, "foundChildDevice": foundChildDevice})
		return foundChildDevice, nil
	}

	log.Warnw("child-device-not-found", log.Fields{"parentDeviceId": parentDevice.Id,
		"serialNumber": serialNumber, "onuId": onuId, "parentPortNo": parentPortNo})
	return nil, status.Errorf(codes.NotFound, "%s", parentDeviceId)
}

func (dMgr *DeviceManager) GetChildDeviceWithProxyAddress(proxyAddress *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	log.Debugw("GetChildDeviceWithProxyAddress", log.Fields{"proxyAddress": proxyAddress})

	var parentDevice *voltha.Device
	var err error
	if parentDevice, err = dMgr.GetDevice(proxyAddress.DeviceId); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	var childDeviceIds []string
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-devices", log.Fields{"parentDeviceId": parentDevice.Id})
		return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
	}

	var foundChildDevice *voltha.Device
	for _, childDeviceId := range childDeviceIds {
		if searchDevice, err := dMgr.GetDevice(childDeviceId); err == nil {
			if searchDevice.ProxyAddress == proxyAddress {
				foundChildDevice = searchDevice
				break
			}
		}
	}

	if foundChildDevice != nil {
		log.Debugw("child-device-found", log.Fields{"proxyAddress": proxyAddress})
		return foundChildDevice, nil
	}

	log.Warnw("child-device-not-found", log.Fields{"proxyAddress": proxyAddress})
	return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
}

func (dMgr *DeviceManager) IsDeviceInCache(id string) bool {
	_, exist := dMgr.deviceAgents.Load(id)
	return exist
}

func (dMgr *DeviceManager) IsRootDevice(id string) (bool, error) {
	dMgr.lockRootDeviceMap.RLock()
	defer dMgr.lockRootDeviceMap.RUnlock()
	if exist := dMgr.rootDevices[id]; exist {
		return dMgr.rootDevices[id], nil
	}
	return false, nil
}

// ListDevices retrieves the latest devices from the data model
func (dMgr *DeviceManager) ListDevices() (*voltha.Devices, error) {
	log.Debug("ListDevices")
	result := &voltha.Devices{}
	if devices := dMgr.clusterDataProxy.List(context.Background(), "/devices", 0, false, ""); devices != nil {
		for _, device := range devices.([]interface{}) {
			// If device is not in memory then set it up
			if !dMgr.IsDeviceInCache(device.(*voltha.Device).Id) {
				log.Debugw("loading-device-from-Model", log.Fields{"id": device.(*voltha.Device).Id})
				agent := newDeviceAgent(dMgr.adapterProxy, device.(*voltha.Device), dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
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
	log.Debugw("ListDevices-end", log.Fields{"len": len(result.Items)})
	return result, nil
}

//getDeviceFromModelretrieves the device data from the model.
func (dMgr *DeviceManager) getDeviceFromModel(deviceId string) (*voltha.Device, error) {
	if device := dMgr.clusterDataProxy.Get(context.Background(), "/devices/"+deviceId, 0, false, ""); device != nil {
		if d, ok := device.(*voltha.Device); ok {
			return d, nil
		}
	}
	return nil, status.Error(codes.NotFound, deviceId)
}

// loadDevice loads the deviceId in memory, if not present
func (dMgr *DeviceManager) loadDevice(deviceId string) (*DeviceAgent, error) {
	if deviceId == "" {
		return nil, status.Error(codes.InvalidArgument, "deviceId empty")
	}
	var err error
	var device *voltha.Device
	dMgr.devicesLoadingLock.Lock()
	if _, exist := dMgr.deviceLoadingInProgress[deviceId]; !exist {
		if !dMgr.IsDeviceInCache(deviceId) {
			dMgr.deviceLoadingInProgress[deviceId] = []chan int{make(chan int, 1)}
			dMgr.devicesLoadingLock.Unlock()
			// Proceed with the loading only if the device exist in the Model (could have been deleted)
			if device, err = dMgr.getDeviceFromModel(deviceId); err == nil {
				log.Debugw("loading-device", log.Fields{"deviceId": deviceId})
				agent := newDeviceAgent(dMgr.adapterProxy, device, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
				if err = agent.start(nil, true); err != nil {
					log.Warnw("Failure loading device", log.Fields{"deviceId": deviceId, "error": err})
					agent.stop(nil)
				} else {
					dMgr.addDeviceAgentToMap(agent)
				}
			} else {
				log.Debugw("Device not in model", log.Fields{"deviceId": deviceId})
			}
			// announce completion of task to any number of waiting channels
			dMgr.devicesLoadingLock.Lock()
			if v, ok := dMgr.deviceLoadingInProgress[deviceId]; ok {
				for _, ch := range v {
					close(ch)
				}
				delete(dMgr.deviceLoadingInProgress, deviceId)
			}
			dMgr.devicesLoadingLock.Unlock()
		} else {
			dMgr.devicesLoadingLock.Unlock()
		}
	} else {
		ch := make(chan int, 1)
		dMgr.deviceLoadingInProgress[deviceId] = append(dMgr.deviceLoadingInProgress[deviceId], ch)
		dMgr.devicesLoadingLock.Unlock()
		//	Wait for the channel to be closed, implying the process loading this device is done.
		<-ch
	}
	if agent, ok := dMgr.deviceAgents.Load(deviceId); ok {
		return agent.(*DeviceAgent), nil
	}
	return nil, status.Errorf(codes.Aborted, "Error loading device %s", deviceId)
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
					log.Warnw("failure-loading-device", log.Fields{"deviceId": childDeviceId, "error": err})
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

//ReconcileDevices is a request to a voltha core to update its list of managed devices.  This will
//trigger loading the devices along with their children and parent in memory
func (dMgr *DeviceManager) ReconcileDevices(ctx context.Context, ids *voltha.IDs, ch chan interface{}) {
	log.Debugw("ReconcileDevices", log.Fields{"numDevices": len(ids.Items)})
	var res interface{}
	if ids != nil && len(ids.Items) != 0 {
		toReconcile := len(ids.Items)
		reconciled := 0
		var err error
		for _, id := range ids.Items {
			if err = dMgr.load(id.Id); err != nil {
				log.Warnw("failure-reconciling-device", log.Fields{"deviceId": id.Id, "error": err})
			} else {
				reconciled += 1
			}
		}
		if toReconcile != reconciled {
			res = status.Errorf(codes.DataLoss, "less-device-reconciled-than-requested:%d/%d", reconciled, toReconcile)
		}
	} else {
		res = status.Errorf(codes.InvalidArgument, "empty-list-of-ids")
	}
	sendResponse(ctx, ch, res)
}

// isOkToReconcile validates whether a device is in the correct status to be reconciled
func isOkToReconcile(device *voltha.Device) bool {
	if device == nil {
		return false
	}
	return device.AdminState != voltha.AdminState_PREPROVISIONED && device.AdminState != voltha.AdminState_DELETED
}

// adapterRestarted is invoked whenever an adapter is restarted
func (dMgr *DeviceManager) adapterRestarted(adapter *voltha.Adapter) error {
	log.Debugw("adapter-restarted", log.Fields{"adapter": adapter.Id})

	// Let's reconcile the device managed by this Core only
	rootDeviceIds := dMgr.core.deviceOwnership.GetAllDeviceIdsOwnedByMe()
	if len(rootDeviceIds) == 0 {
		log.Debugw("nothing-to-reconcile", log.Fields{"adapterId": adapter.Id})
		return nil
	}

	chnlsList := make([]chan interface{}, 0)
	for _, rootDeviceId := range rootDeviceIds {
		if rootDevice, _ := dMgr.getDeviceFromModel(rootDeviceId); rootDevice != nil {
			if rootDevice.Adapter == adapter.Id {
				if isOkToReconcile(rootDevice) {
					log.Debugw("reconciling-root-device", log.Fields{"rootId": rootDevice.Id})
					chnlsList = dMgr.sendReconcileDeviceRequest(rootDevice, chnlsList)
				} else {
					log.Debugw("not-reconciling-root-device", log.Fields{"rootId": rootDevice.Id, "state": rootDevice.AdminState})
				}
			} else { // Should we be reconciling the root's children instead?
			childManagedByAdapter:
				for _, port := range rootDevice.Ports {
					for _, peer := range port.Peers {
						if childDevice, _ := dMgr.getDeviceFromModel(peer.DeviceId); childDevice != nil {
							if childDevice.Adapter == adapter.Id {
								if isOkToReconcile(childDevice) {
									log.Debugw("reconciling-child-device", log.Fields{"childId": childDevice.Id})
									chnlsList = dMgr.sendReconcileDeviceRequest(childDevice, chnlsList)
								} else {
									log.Debugw("not-reconciling-child-device", log.Fields{"childId": childDevice.Id, "state": childDevice.AdminState})
								}
							} else {
								// All child devices under a parent device are typically managed by the same adapter type.
								// Therefore we only need to check whether the first device we retrieved is managed by that adapter
								break childManagedByAdapter
							}
						}
					}
				}
			}
		}
	}
	if len(chnlsList) > 0 {
		// Wait for completion
		if res := utils.WaitForNilOrErrorResponses(dMgr.defaultTimeout, chnlsList...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	} else {
		log.Debugw("no-managed-device-to-reconcile", log.Fields{"adapterId": adapter.Id})
	}
	return nil
}

func (dMgr *DeviceManager) sendReconcileDeviceRequest(device *voltha.Device, chnlsList []chan interface{}) []chan interface{} {
	// Send a reconcile request to the adapter. Since this Core may not be managing this device then there is no
	// point of creating a device agent (if the device is not being managed by this Core) before sending the request
	// to the adapter.   We will therefore bypass the adapter adapter and send the request directly to teh adapter via
	// the adapter_proxy.
	ch := make(chan interface{})
	chnlsList = append(chnlsList, ch)
	go func(device *voltha.Device) {
		if err := dMgr.adapterProxy.ReconcileDevice(context.Background(), device); err != nil {
			log.Errorw("reconcile-request-failed", log.Fields{"deviceId": device.Id, "error": err})
			ch <- status.Errorf(codes.Internal, "device: %s", device.Id)
		}
		ch <- nil
	}(device)

	return chnlsList
}

func (dMgr *DeviceManager) reconcileChildDevices(parentDeviceId string) error {
	if parentDevice, _ := dMgr.getDeviceFromModel(parentDeviceId); parentDevice != nil {
		chnlsList := make([]chan interface{}, 0)
		for _, port := range parentDevice.Ports {
			for _, peer := range port.Peers {
				if childDevice, _ := dMgr.getDeviceFromModel(peer.DeviceId); childDevice != nil {
					chnlsList = dMgr.sendReconcileDeviceRequest(childDevice, chnlsList)
				}
			}
		}
		// Wait for completion
		if res := utils.WaitForNilOrErrorResponses(dMgr.defaultTimeout, chnlsList...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	}
	return nil
}

func (dMgr *DeviceManager) updateDeviceUsingAdapterData(device *voltha.Device) error {
	log.Debugw("updateDeviceUsingAdapterData", log.Fields{"deviceid": device.Id, "device": device})
	if agent := dMgr.getDeviceAgent(device.Id); agent != nil {
		return agent.updateDeviceUsingAdapterData(device)
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
		// Notify the logical device manager to setup a logical port, if needed.  If the added port is an NNI or UNI
		// then a logical port will be added to the logical device and the device graph generated.  If the port is a
		// PON port then only the device graph will be generated.
		if device, err := dMgr.GetDevice(deviceId); err == nil {
			go dMgr.logicalDeviceMgr.updateLogicalPort(device, port)
		} else {
			log.Errorw("failed-to-retrieve-device", log.Fields{"deviceId": deviceId})
			return err
		}
		return nil
	} else {
		return status.Errorf(codes.NotFound, "%s", deviceId)
	}
}

func (dMgr *DeviceManager) deletePeerPorts(fromDeviceId string, deviceId string) error {
	log.Debugw("deletePeerPorts", log.Fields{"fromDeviceId": fromDeviceId, "deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(fromDeviceId); agent != nil {
		return agent.deletePeerPorts(deviceId)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) addFlowsAndGroups(deviceId string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	log.Debugw("addFlowsAndGroups", log.Fields{"deviceid": deviceId, "flowMetadata": flowMetadata})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.addFlowsAndGroups(flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) deleteFlowsAndGroups(deviceId string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	log.Debugw("deleteFlowsAndGroups", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.deleteFlowsAndGroups(flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) updateFlowsAndGroups(deviceId string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	log.Debugw("updateFlowsAndGroups", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updateFlowsAndGroups(flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

// updatePmConfigs updates the PM configs.  This is executed when the northbound gRPC API is invoked, typically
// following a user action
func (dMgr *DeviceManager) updatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs, ch chan interface{}) {
	var res interface{}
	if pmConfigs.Id == "" {
		res = status.Errorf(codes.FailedPrecondition, "invalid-device-Id")
	} else if agent := dMgr.getDeviceAgent(pmConfigs.Id); agent != nil {
		res = agent.updatePmConfigs(ctx, pmConfigs)
	} else {
		res = status.Errorf(codes.NotFound, "%s", pmConfigs.Id)
	}
	sendResponse(ctx, ch, res)
}

// initPmConfigs initialize the pm configs as defined by the adapter.
func (dMgr *DeviceManager) initPmConfigs(deviceId string, pmConfigs *voltha.PmConfigs) error {
	if pmConfigs.Id == "" {
		return status.Errorf(codes.FailedPrecondition, "invalid-device-Id")
	}
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.initPmConfigs(pmConfigs)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) listPmConfigs(ctx context.Context, deviceId string) (*voltha.PmConfigs, error) {
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.listPmConfigs(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) getSwitchCapability(ctx context.Context, deviceId string) (*ic.SwitchCapability, error) {
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

func (dMgr *DeviceManager) getPortCapability(ctx context.Context, deviceId string, portNo uint32) (*ic.PortCapability, error) {
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

func (dMgr *DeviceManager) updateChildrenStatus(deviceId string, operStatus voltha.OperStatus_OperStatus, connStatus voltha.ConnectStatus_ConnectStatus) error {
	log.Debugw("updateChildrenStatus", log.Fields{"parentDeviceid": deviceId, "operStatus": operStatus, "connStatus": connStatus})
	var parentDevice *voltha.Device
	var err error
	if parentDevice, err = dMgr.GetDevice(deviceId); err != nil {
		return status.Errorf(codes.Aborted, "%s", err.Error())
	}
	var childDeviceIds []string
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-device", log.Fields{"parentDeviceId": parentDevice.Id})
	}
	for _, childDeviceId := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(childDeviceId); agent != nil {
			if err = agent.updateDeviceStatus(operStatus, connStatus); err != nil {
				return status.Errorf(codes.Aborted, "childDevice:%s, error:%s", childDeviceId, err.Error())
			}
		}
	}
	return nil
}

func (dMgr *DeviceManager) updatePortState(deviceId string, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_OperStatus) error {
	log.Debugw("updatePortState", log.Fields{"deviceid": deviceId, "portType": portType, "portNo": portNo, "operStatus": operStatus})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.updatePortState(portType, portNo, operStatus)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) deleteAllPorts(deviceId string) error {
	log.Debugw("DeleteAllPorts", log.Fields{"deviceid": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		if err := agent.deleteAllPorts(); err != nil {
			return err
		}
		// Notify the logical device manager to remove all logical ports, if needed.
		// At this stage the device itself may gave been deleted already at a deleteAllPorts
		// typically is part of a device deletion phase.
		if device, err := dMgr.GetDevice(deviceId); err == nil {
			go dMgr.logicalDeviceMgr.deleteAllLogicalPorts(device)
		} else {
			log.Warnw("failed-to-retrieve-device", log.Fields{"deviceId": deviceId})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

//updatePortsState updates all ports on the device
func (dMgr *DeviceManager) updatePortsState(deviceId string, state voltha.OperStatus_OperStatus) error {
	log.Debugw("updatePortsState", log.Fields{"deviceid": deviceId})

	var adminState voltha.AdminState_AdminState
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		switch state {
		case voltha.OperStatus_ACTIVE:
			adminState = voltha.AdminState_ENABLED
			if err := agent.enablePorts(); err != nil {
				log.Warnw("enable-all-ports-failed", log.Fields{"deviceId": deviceId, "error": err})
				return err
			}
		case voltha.OperStatus_UNKNOWN:
			adminState = voltha.AdminState_DISABLED
			if err := agent.disablePorts(); err != nil {
				log.Warnw("disable-all-ports-failed", log.Fields{"deviceId": deviceId, "error": err})
				return err
			}
		default:
			return status.Error(codes.Unimplemented, "state-change-not-implemented")
		}
		// Notify the logical device about the state change
		if device, err := dMgr.GetDevice(deviceId); err != nil {
			log.Warnw("non-existent-device", log.Fields{"deviceId": deviceId, "error": err})
			return err
		} else {
			if err := dMgr.logicalDeviceMgr.updatePortsState(device, adminState); err != nil {
				log.Warnw("failed-updating-ports-state", log.Fields{"deviceId": deviceId, "error": err})
				return err
			}
			return nil
		}
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) childDeviceDetected(parentDeviceId string, parentPortNo int64, deviceType string,
	channelId int64, vendorId string, serialNumber string, onuId int64) (*voltha.Device, error) {
	log.Debugw("childDeviceDetected", log.Fields{"parentDeviceId": parentDeviceId, "parentPortNo": parentPortNo, "deviceType": deviceType, "channelId": channelId, "vendorId": vendorId, "serialNumber": serialNumber, "onuId": onuId})

	if deviceType == "" && vendorId != "" {
		log.Debug("device-type-is-nil-fetching-device-type")
		if deviceTypesIf := dMgr.adapterMgr.clusterDataProxy.List(context.Background(), "/device_types", 0, false, ""); deviceTypesIf != nil {
		OLoop:
			for _, deviceTypeIf := range deviceTypesIf.([]interface{}) {
				if dType, ok := deviceTypeIf.(*voltha.DeviceType); ok {
					for _, v := range dType.VendorIds {
						if v == vendorId {
							deviceType = dType.Adapter
							break OLoop
						}
					}
				}
			}
		}
	}
	//if no match found for the vendorid,report adapter with the custom error message
	if deviceType == "" {
		log.Errorw("failed-to-fetch-adapter-name ", log.Fields{"vendorId": vendorId})
		return nil, status.Errorf(codes.NotFound, "%s", vendorId)
	}

	// Create the ONU device
	childDevice := &voltha.Device{}
	childDevice.Type = deviceType
	childDevice.ParentId = parentDeviceId
	childDevice.ParentPortNo = uint32(parentPortNo)
	childDevice.VendorId = vendorId
	childDevice.SerialNumber = serialNumber
	childDevice.Root = false

	//Get parent device type
	parent, err := dMgr.GetDevice(parentDeviceId)
	if err != nil {
		log.Error("no-parent-found", log.Fields{"parentId": parentDeviceId})
		return nil, status.Errorf(codes.NotFound, "%s", parentDeviceId)
	}

	if device, err := dMgr.GetChildDevice(parentDeviceId, serialNumber, onuId, parentPortNo); err == nil {
		log.Warnw("child-device-exists", log.Fields{"parentId": parentDeviceId, "serialNumber": serialNumber})
		return device, status.Errorf(codes.AlreadyExists, "%s", serialNumber)
	}

	childDevice.ProxyAddress = &voltha.Device_ProxyAddress{DeviceId: parentDeviceId, DeviceType: parent.Type, ChannelId: uint32(channelId), OnuId: uint32(onuId)}

	// Create and start a device agent for that device
	agent := newDeviceAgent(dMgr.adapterProxy, childDevice, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
	dMgr.addDeviceAgentToMap(agent)
	agent.start(nil, false)

	// Activate the child device
	if agent := dMgr.getDeviceAgent(agent.deviceId); agent != nil {
		go agent.enableDevice(nil)
	}

	// Publish on the messaging bus that we have discovered new devices
	go dMgr.kafkaICProxy.DeviceDiscovered(agent.deviceId, deviceType, parentDeviceId, dMgr.coreInstanceId)

	return agent.lastData, nil
}

func (dMgr *DeviceManager) processTransition(previous *voltha.Device, current *voltha.Device) error {
	// This will be triggered on every update to the device.
	handlers := dMgr.stateTransitions.GetTransitionHandler(previous, current)
	if handlers == nil {
		log.Debugw("no-op-transition", log.Fields{"deviceId": current.Id})
		return nil
	}
	log.Debugw("handler-found", log.Fields{"num-handlers": len(handlers), "isParent": current.Root, "current-data": current})
	for _, handler := range handlers {
		log.Debugw("running-handler", log.Fields{"handler": funcName(handler)})
		if err := handler(current); err != nil {
			log.Warnw("handler-failed", log.Fields{"handler": funcName(handler), "error": err})
			return err
		}
	}
	return nil
}

func (dMgr *DeviceManager) packetOut(deviceId string, outPort uint32, packet *ofp.OfpPacketOut) error {
	log.Debugw("packetOut", log.Fields{"deviceId": deviceId, "outPort": outPort})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.packetOut(outPort, packet)
	}
	return status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) PacketIn(deviceId string, port uint32, transactionId string, packet []byte) error {
	log.Debugw("PacketIn", log.Fields{"deviceId": deviceId, "port": port})
	// Get the logical device Id based on the deviceId
	var device *voltha.Device
	var err error
	if device, err = dMgr.GetDevice(deviceId); err != nil {
		log.Errorw("device-not-found", log.Fields{"deviceId": deviceId})
		return err
	}
	if !device.Root {
		log.Errorw("device-not-root", log.Fields{"deviceId": deviceId})
		return status.Errorf(codes.FailedPrecondition, "%s", deviceId)
	}

	if err := dMgr.logicalDeviceMgr.packetIn(device.ParentId, port, transactionId, packet); err != nil {
		return err
	}
	return nil
}

func (dMgr *DeviceManager) CreateLogicalDevice(cDevice *voltha.Device) error {
	log.Info("CreateLogicalDevice")
	// Verify whether the logical device has already been created
	if cDevice.ParentId != "" {
		log.Debugw("Parent device already exist.", log.Fields{"deviceId": cDevice.Id, "logicalDeviceId": cDevice.Id})
		return nil
	}
	var logicalId *string
	var err error
	if logicalId, err = dMgr.logicalDeviceMgr.createLogicalDevice(nil, cDevice); err != nil {
		log.Warnw("createlogical-device-error", log.Fields{"device": cDevice})
		return err
	}
	// Update the parent device with the logical id
	dMgr.UpdateDeviceAttribute(cDevice.Id, "ParentId", *logicalId)
	return nil
}

func (dMgr *DeviceManager) DeleteLogicalDevice(cDevice *voltha.Device) error {
	log.Info("DeleteLogicalDevice")
	var err error
	if err = dMgr.logicalDeviceMgr.deleteLogicalDevice(nil, cDevice); err != nil {
		log.Warnw("deleteLogical-device-error", log.Fields{"deviceId": cDevice.Id})
		return err
	}
	// Remove the logical device Id from the parent device
	logicalId := ""
	dMgr.UpdateDeviceAttribute(cDevice.Id, "ParentId", logicalId)
	return nil
}

func (dMgr *DeviceManager) DeleteLogicalPort(device *voltha.Device) error {
	log.Info("deleteLogicalPort")
	var err error
	// Get the logical port associated with this device
	var lPortId *voltha.LogicalPortId
	if lPortId, err = dMgr.logicalDeviceMgr.getLogicalPortId(device); err != nil {
		log.Warnw("getLogical-port-error", log.Fields{"deviceId": device.Id, "error": err})
		return err
	}
	if err = dMgr.logicalDeviceMgr.deleteLogicalPort(nil, lPortId); err != nil {
		log.Warnw("deleteLogical-port-error", log.Fields{"deviceId": device.Id})
		return err
	}
	return nil
}

func (dMgr *DeviceManager) DeleteLogicalPorts(device *voltha.Device) error {
	log.Info("deleteLogicalPorts")
	if err := dMgr.logicalDeviceMgr.deleteLogicalPorts(device.Id); err != nil {
		log.Warnw("deleteLogical-ports-error", log.Fields{"deviceId": device.Id})
		return err
	}
	return nil
}

func (dMgr *DeviceManager) MarkChildDevicesAsUnReachable(device *voltha.Device) error {
	log.Info("MarkChildDevicesAsUnReachable")
	// Set the connection status to unreachable
	connStatus := voltha.ConnectStatus_UNREACHABLE
	// Do not set the operational status.  Setting it to -1 will do the trick
	operStatus := voltha.OperStatus_OperStatus(-1)
	if err := dMgr.updateChildrenStatus(device.Id, operStatus, connStatus); err != nil {
		log.Warnw("deleteLogical-ports-error", log.Fields{"deviceId": device.Id})
		return err
	}
	return nil
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

//childDevicesLost is invoked by an adapter to indicate that a parent device is in a state (Disabled) where it
//cannot manage the child devices.  This will trigger the Core to disable all the child devices.
func (dMgr *DeviceManager) childDevicesLost(parentDeviceId string) error {
	log.Debug("childDevicesLost")
	var err error
	var parentDevice *voltha.Device
	if parentDevice, err = dMgr.GetDevice(parentDeviceId); err != nil {
		log.Warnw("failed-getting-device", log.Fields{"deviceId": parentDeviceId, "error": err})
		return err
	}
	return dMgr.DisableAllChildDevices(parentDevice)
}

//childDevicesDetected is invoked by an adapter when child devices are found, typically after after a
// disable/enable sequence.  This will trigger the Core to Enable all the child devices of that parent.
func (dMgr *DeviceManager) childDevicesDetected(parentDeviceId string) error {
	log.Debug("childDevicesDetected")
	var err error
	var parentDevice *voltha.Device
	var childDeviceIds []string

	if parentDevice, err = dMgr.GetDevice(parentDeviceId); err != nil {
		log.Warnw("failed-getting-device", log.Fields{"deviceId": parentDeviceId, "error": err})
		return err
	}

	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-device", log.Fields{"parentDeviceId": parentDevice.Id})
	}
	allChildEnableRequestSent := true
	for _, childDeviceId := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(childDeviceId); agent != nil {
			// Run the children re-registration in its own routine
			go agent.enableDevice(nil)
		} else {
			err = status.Errorf(codes.Unavailable, "no agent for child device %s", childDeviceId)
			log.Errorw("no-child-device-agent", log.Fields{"parentDeviceId": parentDevice.Id, "childId": childDeviceId})
			allChildEnableRequestSent = false
		}
	}
	if !allChildEnableRequestSent {
		return err
	}
	return nil
}

/*
All the functions below are callback functions where they are invoked with the latest and previous data.  We can
therefore use the data as is without trying to get the latest from the model.
*/

//DisableAllChildDevices is invoked as a callback when the parent device is disabled
func (dMgr *DeviceManager) DisableAllChildDevices(parentDevice *voltha.Device) error {
	log.Debug("DisableAllChildDevices")
	var childDeviceIds []string
	var err error
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-device", log.Fields{"parentDeviceId": parentDevice.Id})
	}
	allChildDisable := true
	for _, childDeviceId := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(childDeviceId); agent != nil {
			if err = agent.disableDevice(nil); err != nil {
				log.Errorw("failure-disable-device", log.Fields{"deviceId": childDeviceId, "error": err.Error()})
				allChildDisable = false
			}
		}
	}
	if !allChildDisable {
		return err
	}
	return nil
}

//DeleteAllChildDevices is invoked as a callback when the parent device is deleted
func (dMgr *DeviceManager) DeleteAllChildDevices(parentDevice *voltha.Device) error {
	log.Debug("DeleteAllChildDevices")
	var childDeviceIds []string
	var err error
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		log.Debugw("no-child-device", log.Fields{"parentDeviceId": parentDevice.Id})
	}
	allChildDeleted := true
	for _, childDeviceId := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(childDeviceId); agent != nil {
			if err = agent.deleteDevice(nil); err != nil {
				log.Errorw("failure-delete-device", log.Fields{"deviceId": childDeviceId, "error": err.Error()})
				allChildDeleted = false
			} else {
				agent.stop(nil)
				dMgr.deleteDeviceAgentFromMap(agent)
			}
		}
	}
	if !allChildDeleted {
		return err
	}
	return nil
}

//getAllDeviceIdsWithDeviceParentId returns the list of device Ids which has id as parent Id.  This function uses the
// data from the agent instead of using the data from the parent device as that data would disappear from a parent
// device during a delete device operation.
func (dMgr *DeviceManager) getAllDeviceIdsWithDeviceParentId(id string) []string {
	log.Debugw("getAllAgentsWithDeviceParentId", log.Fields{"parentDeviceId": id})
	deviceIds := make([]string, 0)
	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		agent := value.(*DeviceAgent)
		if agent.parentId == id {
			deviceIds = append(deviceIds, key.(string))
		}
		return true
	})
	return deviceIds
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
		log.Debugw("returning-getAllChildDeviceIds", log.Fields{"parentDeviceId": parentDevice.Id, "childDeviceIds": childDeviceIds})
	}
	return childDeviceIds, nil
}

//getAllChildDevices is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *DeviceManager) getAllChildDevices(parentDeviceId string) (*voltha.Devices, error) {
	log.Debugw("getAllChildDevices", log.Fields{"parentDeviceId": parentDeviceId})
	if parentDevice, err := dMgr.GetDevice(parentDeviceId); err == nil {
		childDevices := make([]*voltha.Device, 0)
		if childDeviceIds, er := dMgr.getAllChildDeviceIds(parentDevice); er == nil {
			for _, deviceId := range childDeviceIds {
				if d, e := dMgr.GetDevice(deviceId); e == nil && d != nil {
					childDevices = append(childDevices, d)
				}
			}
		}
		return &voltha.Devices{Items: childDevices}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", parentDeviceId)
}

func (dMgr *DeviceManager) SetupUNILogicalPorts(cDevice *voltha.Device) error {
	log.Info("addUNILogicalPort")
	if err := dMgr.logicalDeviceMgr.setupUNILogicalPorts(nil, cDevice); err != nil {
		log.Warnw("addUNILogicalPort-error", log.Fields{"device": cDevice, "err": err})
		return err
	}
	return nil
}

func (dMgr *DeviceManager) downloadImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	log.Debugw("downloadImage", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(img.Id); agent != nil {
		if res, err = agent.downloadImage(ctx, img); err != nil {
			log.Debugw("downloadImage-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) cancelImageDownload(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	log.Debugw("cancelImageDownload", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(img.Id); agent != nil {
		if res, err = agent.cancelImageDownload(ctx, img); err != nil {
			log.Debugw("cancelImageDownload-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) activateImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	log.Debugw("activateImage", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(img.Id); agent != nil {
		if res, err = agent.activateImage(ctx, img); err != nil {
			log.Debugw("activateImage-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) revertImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	log.Debugw("revertImage", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(img.Id); agent != nil {
		if res, err = agent.revertImage(ctx, img); err != nil {
			log.Debugw("revertImage-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) getImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	log.Debugw("getImageDownloadStatus", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(img.Id); agent != nil {
		if res, err = agent.getImageDownloadStatus(ctx, img); err != nil {
			log.Debugw("getImageDownloadStatus-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *DeviceManager) updateImageDownload(deviceId string, img *voltha.ImageDownload) error {
	log.Debugw("updateImageDownload", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		if err := agent.updateImageDownload(img); err != nil {
			log.Debugw("updateImageDownload-failed", log.Fields{"err": err, "imageName": img.Name})
			return err
		}
	} else {
		return status.Errorf(codes.NotFound, "%s", img.Id)
	}
	return nil
}

func (dMgr *DeviceManager) getImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	log.Debugw("getImageDownload", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	if agent := dMgr.getDeviceAgent(img.Id); agent != nil {
		return agent.getImageDownload(ctx, img)
	}
	return nil, status.Errorf(codes.NotFound, "%s", img.Id)
}

func (dMgr *DeviceManager) listImageDownloads(ctx context.Context, deviceId string) (*voltha.ImageDownloads, error) {
	log.Debugw("listImageDownloads", log.Fields{"deviceId": deviceId})
	if agent := dMgr.getDeviceAgent(deviceId); agent != nil {
		return agent.listImageDownloads(ctx, deviceId)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceId)
}

func (dMgr *DeviceManager) SetAdminStateToEnable(cDevice *voltha.Device) error {
	log.Info("SetAdminStateToEnable")
	if agent := dMgr.getDeviceAgent(cDevice.Id); agent != nil {
		return agent.updateAdminState(voltha.AdminState_ENABLED)
	}
	return status.Errorf(codes.NotFound, "%s", cDevice.Id)
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
	return errors.New("transition-not-allowed")
}

func (dMgr *DeviceManager) NotifyInvalidTransition(pcDevice *voltha.Device) error {
	log.Errorw("NotifyInvalidTransition", log.Fields{
		"device":     pcDevice.Id,
		"adminState": pcDevice.AdminState,
		"operState":  pcDevice.OperStatus,
		"connState":  pcDevice.ConnectStatus,
	})
	//TODO: notify over kafka?
	return nil
}

func funcName(f interface{}) string {
	p := reflect.ValueOf(f).Pointer()
	rf := runtime.FuncForPC(p)
	return rf.Name()
}

func (dMgr *DeviceManager) UpdateDeviceAttribute(deviceId string, attribute string, value interface{}) {
	if agent, ok := dMgr.deviceAgents.Load(deviceId); ok {
		agent.(*DeviceAgent).updateDeviceAttribute(attribute, value)
	}
}

func (dMgr *DeviceManager) GetParentDeviceId(deviceId string) string {
	if device, _ := dMgr.GetDevice(deviceId); device != nil {
		log.Infow("GetParentDeviceId", log.Fields{"deviceId": device.Id, "parentId": device.ParentId})
		return device.ParentId
	}
	return ""
}

func (dMgr *DeviceManager) simulateAlarm(ctx context.Context, simulatereq *voltha.SimulateAlarmRequest, ch chan interface{}) {
	log.Debugw("simulateAlarm", log.Fields{"id": simulatereq.Id, "Indicator": simulatereq.Indicator, "IntfId": simulatereq.IntfId,
		"PortTypeName": simulatereq.PortTypeName, "OnuDeviceId": simulatereq.OnuDeviceId, "InverseBitErrorRate": simulatereq.InverseBitErrorRate,
		"Drift": simulatereq.Drift, "NewEqd": simulatereq.NewEqd, "OnuSerialNumber": simulatereq.OnuSerialNumber, "Operation": simulatereq.Operation})
	var res interface{}
	if agent := dMgr.getDeviceAgent(simulatereq.Id); agent != nil {
		res = agent.simulateAlarm(ctx, simulatereq)
		log.Debugw("SimulateAlarm-result", log.Fields{"result": res})
	}
	//TODO CLI always get successful response
	sendResponse(ctx, ch, res)
}
