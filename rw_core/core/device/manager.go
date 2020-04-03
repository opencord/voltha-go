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

package device

import (
	"context"
	"errors"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/remote"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Manager represent device manager attributes
type Manager struct {
	deviceAgents            sync.Map
	rootDevices             map[string]bool
	lockRootDeviceMap       sync.RWMutex
	adapterProxy            *remote.AdapterProxy
	adapterMgr              *adapter.Manager
	logicalDeviceMgr        *LogicalManager
	kafkaICProxy            kafka.InterContainerProxy
	stateTransitions        *TransitionMap
	clusterDataProxy        *model.Proxy
	coreInstanceID          string
	exitChannel             chan int
	defaultTimeout          time.Duration
	devicesLoadingLock      sync.RWMutex
	deviceLoadingInProgress map[string][]chan int
}

func NewDeviceManagers(proxy *model.Proxy, adapterMgr *adapter.Manager, kmp kafka.InterContainerProxy, corePairTopic, coreInstanceID string, defaultCoreTimeout time.Duration) (*Manager, *LogicalManager) {
	deviceMgr := &Manager{
		exitChannel:             make(chan int, 1),
		rootDevices:             make(map[string]bool),
		kafkaICProxy:            kmp,
		adapterProxy:            remote.NewAdapterProxy(kmp, corePairTopic),
		coreInstanceID:          coreInstanceID,
		clusterDataProxy:        proxy,
		adapterMgr:              adapterMgr,
		defaultTimeout:          defaultCoreTimeout * time.Millisecond,
		deviceLoadingInProgress: make(map[string][]chan int),
	}
	logicalDeviceMgr := &LogicalManager{
		exitChannel:                    make(chan int, 1),
		deviceMgr:                      deviceMgr,
		kafkaICProxy:                   kmp,
		clusterDataProxy:               proxy,
		defaultTimeout:                 defaultCoreTimeout,
		logicalDeviceLoadingInProgress: make(map[string][]chan int),
	}
	deviceMgr.logicalDeviceMgr = logicalDeviceMgr

	adapterMgr.SetAdapterRestartedCallback(deviceMgr.adapterRestarted)

	return deviceMgr, logicalDeviceMgr
}

func (dMgr *Manager) Start(ctx context.Context) {
	logger.Info("starting-device-manager")
	dMgr.stateTransitions = NewTransitionMap(dMgr)
	probe.UpdateStatusFromContext(ctx, "device-manager", probe.ServiceStatusRunning)
	logger.Info("device-manager-started")
}

func (dMgr *Manager) Stop(ctx context.Context) {
	logger.Info("stopping-device-manager")
	dMgr.exitChannel <- 1
	probe.UpdateStatusFromContext(ctx, "device-manager", probe.ServiceStatusStopped)
	logger.Info("device-manager-stopped")
}

func sendResponse(ctx context.Context, ch chan interface{}, result interface{}) {
	if ctx.Err() == nil {
		// Returned response only of the ctx has not been cancelled/timeout/etc
		// Channel is automatically closed when a context is Done
		ch <- result
		logger.Debugw("sendResponse", log.Fields{"result": result})
	} else {
		// Should the transaction be reverted back?
		logger.Debugw("sendResponse-context-error", log.Fields{"context-error": ctx.Err()})
	}
}

func (dMgr *Manager) addDeviceAgentToMap(agent *Agent) {
	if _, exist := dMgr.deviceAgents.Load(agent.deviceID); !exist {
		dMgr.deviceAgents.Store(agent.deviceID, agent)
	}
	dMgr.lockRootDeviceMap.Lock()
	defer dMgr.lockRootDeviceMap.Unlock()
	dMgr.rootDevices[agent.deviceID] = agent.isRootdevice

}

func (dMgr *Manager) deleteDeviceAgentFromMap(agent *Agent) {
	dMgr.deviceAgents.Delete(agent.deviceID)
	dMgr.lockRootDeviceMap.Lock()
	defer dMgr.lockRootDeviceMap.Unlock()
	delete(dMgr.rootDevices, agent.deviceID)
}

// getDeviceAgent returns the agent managing the device.  If the device is not in memory, it will loads it, if it exists
func (dMgr *Manager) getDeviceAgent(ctx context.Context, deviceID string) *Agent {
	agent, ok := dMgr.deviceAgents.Load(deviceID)
	if ok {
		return agent.(*Agent)
	}
	// Try to load into memory - loading will also create the device agent and set the device ownership
	err := dMgr.load(ctx, deviceID)
	if err == nil {
		agent, ok = dMgr.deviceAgents.Load(deviceID)
		if !ok {
			return nil
		}
		return agent.(*Agent)
	}
	//TODO: Change the return params to return an error as well
	logger.Errorw("loading-device-failed", log.Fields{"deviceId": deviceID, "error": err})
	return nil
}

// listDeviceIdsFromMap returns the list of device IDs that are in memory
func (dMgr *Manager) listDeviceIdsFromMap() *voltha.IDs {
	result := &voltha.IDs{Items: make([]*voltha.ID, 0)}

	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		result.Items = append(result.Items, &voltha.ID{Id: key.(string)})
		return true
	})

	return result
}

func (dMgr *Manager) CreateDevice(ctx context.Context, device *voltha.Device, ch chan interface{}) {
	deviceExist, err := dMgr.isParentDeviceExist(ctx, device)
	if err != nil {
		logger.Errorf("Failed to fetch parent device info")
		sendResponse(ctx, ch, err)
		return
	}
	if deviceExist {
		logger.Errorf("Device is Pre-provisioned already with same IP-Port or MAC Address")
		sendResponse(ctx, ch, errors.New("Device is already pre-provisioned"))
		return
	}
	logger.Debugw("CreateDevice", log.Fields{"device": device, "aproxy": dMgr.adapterProxy})

	// Ensure this device is set as root
	device.Root = true
	// Create and start a device agent for that device
	agent := newAgent(dMgr.adapterProxy, device, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
	device, err = agent.start(ctx, device)
	if err != nil {
		logger.Errorw("Fail-to-start-device", log.Fields{"device-id": agent.deviceID, "error": err})
		sendResponse(ctx, ch, err)
		return
	}
	dMgr.addDeviceAgentToMap(agent)

	sendResponse(ctx, ch, device)
}

func (dMgr *Manager) EnableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	logger.Debugw("EnableDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, id.Id); agent != nil {
		res = agent.enableDevice(ctx)
		logger.Debugw("EnableDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}

	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) DisableDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	logger.Debugw("DisableDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, id.Id); agent != nil {
		res = agent.disableDevice(ctx)
		logger.Debugw("DisableDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}

	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) RebootDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	logger.Debugw("RebootDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, id.Id); agent != nil {
		res = agent.rebootDevice(ctx)
		logger.Debugw("RebootDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) DeleteDevice(ctx context.Context, id *voltha.ID, ch chan interface{}) {
	logger.Debugw("DeleteDevice", log.Fields{"deviceid": id})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, id.Id); agent != nil {
		res = agent.deleteDevice(ctx)
		logger.Debugw("DeleteDevice-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", id.Id)
	}
	sendResponse(ctx, ch, res)
}

// stopManagingDevice stops the management of the device as well as any of its reference device and logical device.
// This function is called only in the Core that does not own this device.  In the Core that owns this device then a
// deletion deletion also includes removal of any reference of this device.
func (dMgr *Manager) stopManagingDevice(ctx context.Context, id string) {
	logger.Infow("stopManagingDevice", log.Fields{"deviceId": id})
	if dMgr.IsDeviceInCache(id) { // Proceed only if an agent is present for this device
		if root, _ := dMgr.IsRootDevice(id); root {
			// stop managing the logical device
			_ = dMgr.logicalDeviceMgr.stopManagingLogicalDeviceWithDeviceID(ctx, id)
		}
		if agent := dMgr.getDeviceAgent(ctx, id); agent != nil {
			if err := agent.stop(ctx); err != nil {
				logger.Warnw("unable-to-stop-device-agent", log.Fields{"device-id": agent.deviceID, "error": err})
			}
			dMgr.deleteDeviceAgentFromMap(agent)
		}
	}
}

// RunPostDeviceDelete removes any reference of this device
func (dMgr *Manager) RunPostDeviceDelete(ctx context.Context, cDevice *voltha.Device) error {
	logger.Infow("RunPostDeviceDelete", log.Fields{"deviceId": cDevice.Id})
	dMgr.stopManagingDevice(ctx, cDevice.Id)
	return nil
}

// GetDevice will returns a device, either from memory or from the dB, if present
func (dMgr *Manager) GetDevice(ctx context.Context, id string) (*voltha.Device, error) {
	logger.Debugw("GetDevice", log.Fields{"deviceid": id})
	if agent := dMgr.getDeviceAgent(ctx, id); agent != nil {
		return agent.getDevice(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

// GetChildDevice will return a device, either from memory or from the dB, if present
func (dMgr *Manager) GetChildDevice(ctx context.Context, parentDeviceID string, serialNumber string, onuID int64, parentPortNo int64) (*voltha.Device, error) {
	logger.Debugw("GetChildDevice", log.Fields{"parentDeviceid": parentDeviceID, "serialNumber": serialNumber,
		"parentPortNo": parentPortNo, "onuId": onuID})

	var parentDevice *voltha.Device
	var err error
	if parentDevice, err = dMgr.GetDevice(ctx, parentDeviceID); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	var childDeviceIds []string
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if len(childDeviceIds) == 0 {
		logger.Debugw("no-child-devices", log.Fields{"parentDeviceId": parentDevice.Id, "serialNumber": serialNumber, "onuId": onuID})
		return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
	}

	var foundChildDevice *voltha.Device
	for _, childDeviceID := range childDeviceIds {
		var found bool
		if searchDevice, err := dMgr.GetDevice(ctx, childDeviceID); err == nil {

			foundOnuID := false
			if searchDevice.ProxyAddress.OnuId == uint32(onuID) {
				if searchDevice.ParentPortNo == uint32(parentPortNo) {
					logger.Debugw("found-child-by-onuid", log.Fields{"parentDeviceId": parentDevice.Id, "onuId": onuID})
					foundOnuID = true
				}
			}

			foundSerialNumber := false
			if searchDevice.SerialNumber == serialNumber {
				logger.Debugw("found-child-by-serialnumber", log.Fields{"parentDeviceId": parentDevice.Id, "serialNumber": serialNumber})
				foundSerialNumber = true
			}

			// if both onuId and serialNumber are provided both must be true for the device to be found
			// otherwise whichever one found a match is good enough
			if onuID > 0 && serialNumber != "" {
				found = foundOnuID && foundSerialNumber
			} else {
				found = foundOnuID || foundSerialNumber
			}

			if found {
				foundChildDevice = searchDevice
				break
			}
		}
	}

	if foundChildDevice != nil {
		logger.Debugw("child-device-found", log.Fields{"parentDeviceId": parentDevice.Id, "foundChildDevice": foundChildDevice})
		return foundChildDevice, nil
	}

	logger.Warnw("child-device-not-found", log.Fields{"parentDeviceId": parentDevice.Id,
		"serialNumber": serialNumber, "onuId": onuID, "parentPortNo": parentPortNo})
	return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
}

// GetChildDeviceWithProxyAddress will return a device based on proxy address
func (dMgr *Manager) GetChildDeviceWithProxyAddress(ctx context.Context, proxyAddress *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	logger.Debugw("GetChildDeviceWithProxyAddress", log.Fields{"proxyAddress": proxyAddress})

	var parentDevice *voltha.Device
	var err error
	if parentDevice, err = dMgr.GetDevice(ctx, proxyAddress.DeviceId); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	var childDeviceIds []string
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if len(childDeviceIds) == 0 {
		logger.Debugw("no-child-devices", log.Fields{"parentDeviceId": parentDevice.Id})
		return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
	}

	var foundChildDevice *voltha.Device
	for _, childDeviceID := range childDeviceIds {
		if searchDevice, err := dMgr.GetDevice(ctx, childDeviceID); err == nil {
			if searchDevice.ProxyAddress == proxyAddress {
				foundChildDevice = searchDevice
				break
			}
		}
	}

	if foundChildDevice != nil {
		logger.Debugw("child-device-found", log.Fields{"proxyAddress": proxyAddress})
		return foundChildDevice, nil
	}

	logger.Warnw("child-device-not-found", log.Fields{"proxyAddress": proxyAddress})
	return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
}

// IsDeviceInCache returns true if device is found in the map
func (dMgr *Manager) IsDeviceInCache(id string) bool {
	_, exist := dMgr.deviceAgents.Load(id)
	return exist
}

// IsRootDevice returns true if root device is found in the map
func (dMgr *Manager) IsRootDevice(id string) (bool, error) {
	dMgr.lockRootDeviceMap.RLock()
	defer dMgr.lockRootDeviceMap.RUnlock()
	if exist := dMgr.rootDevices[id]; exist {
		return dMgr.rootDevices[id], nil
	}
	return false, nil
}

// ListDevices retrieves the latest devices from the data model
func (dMgr *Manager) ListDevices(ctx context.Context) (*voltha.Devices, error) {
	logger.Debug("ListDevices")
	result := &voltha.Devices{}

	var devices []*voltha.Device
	if err := dMgr.clusterDataProxy.List(ctx, "devices", &devices); err != nil {
		logger.Errorw("failed-to-list-devices-from-cluster-proxy", log.Fields{"error": err})
		return nil, err
	}

	for _, device := range devices {
		// If device is not in memory then set it up
		if !dMgr.IsDeviceInCache(device.Id) {
			logger.Debugw("loading-device-from-Model", log.Fields{"id": device.Id})
			agent := newAgent(dMgr.adapterProxy, device, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
			if _, err := agent.start(ctx, nil); err != nil {
				logger.Warnw("failure-starting-agent", log.Fields{"deviceId": device.Id})
			} else {
				dMgr.addDeviceAgentToMap(agent)
			}
		}
		result.Items = append(result.Items, device)
	}
	logger.Debugw("ListDevices-end", log.Fields{"len": len(result.Items)})
	return result, nil
}

//isParentDeviceExist checks whether device is already preprovisioned.
func (dMgr *Manager) isParentDeviceExist(ctx context.Context, newDevice *voltha.Device) (bool, error) {
	hostPort := newDevice.GetHostAndPort()
	var devices []*voltha.Device
	if err := dMgr.clusterDataProxy.List(ctx, "devices", &devices); err != nil {
		logger.Errorw("Failed to list devices from cluster data proxy", log.Fields{"error": err})
		return false, err
	}
	for _, device := range devices {
		if !device.Root {
			continue
		}
		if hostPort != "" && hostPort == device.GetHostAndPort() && device.AdminState != voltha.AdminState_DELETED {
			return true, nil
		}
		if newDevice.MacAddress != "" && newDevice.MacAddress == device.MacAddress && device.AdminState != voltha.AdminState_DELETED {
			return true, nil
		}
	}
	return false, nil
}

//getDeviceFromModelretrieves the device data from the model.
func (dMgr *Manager) getDeviceFromModel(ctx context.Context, deviceID string) (*voltha.Device, error) {
	device := &voltha.Device{}
	if have, err := dMgr.clusterDataProxy.Get(ctx, "devices/"+deviceID, device); err != nil {
		logger.Errorw("failed-to-get-device-info-from-cluster-proxy", log.Fields{"error": err})
		return nil, err
	} else if !have {
		return nil, status.Error(codes.NotFound, deviceID)
	}

	return device, nil
}

// loadDevice loads the deviceID in memory, if not present
func (dMgr *Manager) loadDevice(ctx context.Context, deviceID string) (*Agent, error) {
	if deviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "deviceId empty")
	}
	var err error
	var device *voltha.Device
	dMgr.devicesLoadingLock.Lock()
	if _, exist := dMgr.deviceLoadingInProgress[deviceID]; !exist {
		if !dMgr.IsDeviceInCache(deviceID) {
			dMgr.deviceLoadingInProgress[deviceID] = []chan int{make(chan int, 1)}
			dMgr.devicesLoadingLock.Unlock()
			// Proceed with the loading only if the device exist in the Model (could have been deleted)
			if device, err = dMgr.getDeviceFromModel(ctx, deviceID); err == nil {
				logger.Debugw("loading-device", log.Fields{"deviceId": deviceID})
				agent := newAgent(dMgr.adapterProxy, device, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
				if _, err = agent.start(ctx, nil); err != nil {
					logger.Warnw("Failure loading device", log.Fields{"deviceId": deviceID, "error": err})
				} else {
					dMgr.addDeviceAgentToMap(agent)
				}
			} else {
				logger.Debugw("Device not in model", log.Fields{"deviceId": deviceID})
			}
			// announce completion of task to any number of waiting channels
			dMgr.devicesLoadingLock.Lock()
			if v, ok := dMgr.deviceLoadingInProgress[deviceID]; ok {
				for _, ch := range v {
					close(ch)
				}
				delete(dMgr.deviceLoadingInProgress, deviceID)
			}
			dMgr.devicesLoadingLock.Unlock()
		} else {
			dMgr.devicesLoadingLock.Unlock()
		}
	} else {
		ch := make(chan int, 1)
		dMgr.deviceLoadingInProgress[deviceID] = append(dMgr.deviceLoadingInProgress[deviceID], ch)
		dMgr.devicesLoadingLock.Unlock()
		//	Wait for the channel to be closed, implying the process loading this device is done.
		<-ch
	}
	if agent, ok := dMgr.deviceAgents.Load(deviceID); ok {
		return agent.(*Agent), nil
	}
	return nil, status.Errorf(codes.Aborted, "Error loading device %s", deviceID)
}

// loadRootDeviceParentAndChildren loads the children and parents of a root device in memory
func (dMgr *Manager) loadRootDeviceParentAndChildren(ctx context.Context, device *voltha.Device) error {
	logger.Debugw("loading-parent-and-children", log.Fields{"deviceId": device.Id})
	if device.Root {
		// Scenario A
		if device.ParentId != "" {
			//	 Load logical device if needed.
			if err := dMgr.logicalDeviceMgr.load(ctx, device.ParentId); err != nil {
				logger.Warnw("failure-loading-logical-device", log.Fields{"lDeviceId": device.ParentId})
			}
		} else {
			logger.Debugw("no-parent-to-load", log.Fields{"deviceId": device.Id})
		}
		//	Load all child devices, if needed
		if childDeviceIds, err := dMgr.getAllChildDeviceIds(device); err == nil {
			for _, childDeviceID := range childDeviceIds {
				if _, err := dMgr.loadDevice(ctx, childDeviceID); err != nil {
					logger.Warnw("failure-loading-device", log.Fields{"deviceId": childDeviceID, "error": err})
					return err
				}
			}
			logger.Debugw("loaded-children", log.Fields{"deviceId": device.Id, "numChildren": len(childDeviceIds)})
		} else {
			logger.Debugw("no-child-to-load", log.Fields{"deviceId": device.Id})
		}
	}
	return nil
}

// load loads the deviceId in memory, if not present, and also loads its accompanying parents and children.  Loading
// in memory is for improved performance.  It is not imperative that a device needs to be in memory when a request
// acting on the device is received by the core. In such a scenario, the Core will load the device in memory first
// and the proceed with the request.
func (dMgr *Manager) load(ctx context.Context, deviceID string) error {
	logger.Debug("load...")
	// First load the device - this may fail in case the device was deleted intentionally by the other core
	var dAgent *Agent
	var err error
	if dAgent, err = dMgr.loadDevice(ctx, deviceID); err != nil {
		return err
	}
	// Get the loaded device details
	device, err := dAgent.getDevice(ctx)
	if err != nil {
		return err
	}

	// If the device is in Pre-provisioning or deleted state stop here
	if device.AdminState == voltha.AdminState_PREPROVISIONED || device.AdminState == voltha.AdminState_DELETED {
		return nil
	}

	// Now we face two scenarios
	if device.Root {
		// Load all children as well as the parent of this device (logical_device)
		if err := dMgr.loadRootDeviceParentAndChildren(ctx, device); err != nil {
			logger.Warnw("failure-loading-device-parent-and-children", log.Fields{"deviceId": deviceID})
			return err
		}
		logger.Debugw("successfully-loaded-parent-and-children", log.Fields{"deviceId": deviceID})
	} else {
		//	Scenario B - use the parentId of that device (root device) to trigger the loading
		if device.ParentId != "" {
			return dMgr.load(ctx, device.ParentId)
		}
	}
	return nil
}

// ListDeviceIds retrieves the latest device IDs information from the data model (memory data only)
func (dMgr *Manager) ListDeviceIds() (*voltha.IDs, error) {
	logger.Debug("ListDeviceIDs")
	// Report only device IDs that are in the device agent map
	return dMgr.listDeviceIdsFromMap(), nil
}

//ReconcileDevices is a request to a voltha core to update its list of managed devices.  This will
//trigger loading the devices along with their children and parent in memory
func (dMgr *Manager) ReconcileDevices(ctx context.Context, ids *voltha.IDs, ch chan interface{}) {
	logger.Debugw("ReconcileDevices", log.Fields{"numDevices": len(ids.Items)})
	var res interface{}
	if ids != nil && len(ids.Items) != 0 {
		toReconcile := len(ids.Items)
		reconciled := 0
		var err error
		for _, id := range ids.Items {
			if err = dMgr.load(ctx, id.Id); err != nil {
				logger.Warnw("failure-reconciling-device", log.Fields{"deviceId": id.Id, "error": err})
			} else {
				reconciled++
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
func (dMgr *Manager) adapterRestarted(ctx context.Context, adapter *voltha.Adapter) error {
	logger.Debugw("adapter-restarted", log.Fields{"adapter": adapter.Id})

	// Let's reconcile the device managed by this Core only
	if len(dMgr.rootDevices) == 0 {
		logger.Debugw("nothing-to-reconcile", log.Fields{"adapterId": adapter.Id})
		return nil
	}

	responses := make([]utils.Response, 0)
	for rootDeviceID := range dMgr.rootDevices {
		if rootDevice, _ := dMgr.getDeviceFromModel(ctx, rootDeviceID); rootDevice != nil {
			if rootDevice.Adapter == adapter.Id {
				if isOkToReconcile(rootDevice) {
					logger.Debugw("reconciling-root-device", log.Fields{"rootId": rootDevice.Id})
					responses = append(responses, dMgr.sendReconcileDeviceRequest(ctx, rootDevice))
				} else {
					logger.Debugw("not-reconciling-root-device", log.Fields{"rootId": rootDevice.Id, "state": rootDevice.AdminState})
				}
			} else { // Should we be reconciling the root's children instead?
			childManagedByAdapter:
				for _, port := range rootDevice.Ports {
					for _, peer := range port.Peers {
						if childDevice, _ := dMgr.getDeviceFromModel(ctx, peer.DeviceId); childDevice != nil {
							if childDevice.Adapter == adapter.Id {
								if isOkToReconcile(childDevice) {
									logger.Debugw("reconciling-child-device", log.Fields{"childId": childDevice.Id})
									responses = append(responses, dMgr.sendReconcileDeviceRequest(ctx, childDevice))
								} else {
									logger.Debugw("not-reconciling-child-device", log.Fields{"childId": childDevice.Id, "state": childDevice.AdminState})
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
	if len(responses) > 0 {
		// Wait for completion
		if res := utils.WaitForNilOrErrorResponses(dMgr.defaultTimeout, responses...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	} else {
		logger.Debugw("no-managed-device-to-reconcile", log.Fields{"adapterId": adapter.Id})
	}
	return nil
}

func (dMgr *Manager) sendReconcileDeviceRequest(ctx context.Context, device *voltha.Device) utils.Response {
	// Send a reconcile request to the adapter. Since this Core may not be managing this device then there is no
	// point of creating a device agent (if the device is not being managed by this Core) before sending the request
	// to the adapter.   We will therefore bypass the adapter adapter and send the request directly to the adapter via
	// the adapter proxy.
	response := utils.NewResponse()
	ch, err := dMgr.adapterProxy.ReconcileDevice(ctx, device)
	if err != nil {
		response.Error(err)
	}
	// Wait for adapter response in its own routine
	go func() {
		resp, ok := <-ch
		if !ok {
			response.Error(status.Errorf(codes.Aborted, "channel-closed-device: %s", device.Id))
		} else if resp.Err != nil {
			response.Error(resp.Err)
		}
		response.Done()
	}()
	return response
}

func (dMgr *Manager) ReconcileChildDevices(ctx context.Context, parentDeviceID string) error {
	if parentDevice, _ := dMgr.getDeviceFromModel(ctx, parentDeviceID); parentDevice != nil {
		responses := make([]utils.Response, 0)
		for _, port := range parentDevice.Ports {
			for _, peer := range port.Peers {
				if childDevice, _ := dMgr.getDeviceFromModel(ctx, peer.DeviceId); childDevice != nil {
					responses = append(responses, dMgr.sendReconcileDeviceRequest(ctx, childDevice))
				}
			}
		}
		// Wait for completion
		if res := utils.WaitForNilOrErrorResponses(dMgr.defaultTimeout, responses...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	}
	return nil
}

func (dMgr *Manager) UpdateDeviceUsingAdapterData(ctx context.Context, device *voltha.Device) error {
	logger.Debugw("UpdateDeviceUsingAdapterData", log.Fields{"deviceid": device.Id, "device": device})
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		return agent.updateDeviceUsingAdapterData(ctx, device)
	}
	return status.Errorf(codes.NotFound, "%s", device.Id)
}

func (dMgr *Manager) AddPort(ctx context.Context, deviceID string, port *voltha.Port) error {
	agent := dMgr.getDeviceAgent(ctx, deviceID)
	if agent != nil {
		if err := agent.addPort(ctx, port); err != nil {
			return err
		}
		//	Setup peer ports
		meAsPeer := &voltha.Port_PeerPort{DeviceId: deviceID, PortNo: port.PortNo}
		for _, peerPort := range port.Peers {
			if agent := dMgr.getDeviceAgent(ctx, peerPort.DeviceId); agent != nil {
				if err := agent.addPeerPort(ctx, meAsPeer); err != nil {
					logger.Errorw("failed-to-add-peer", log.Fields{"peer-device-id": peerPort.DeviceId})
					return err
				}
			}
		}
		// Notify the logical device manager to setup a logical port, if needed.  If the added port is an NNI or UNI
		// then a logical port will be added to the logical device and the device graph generated.  If the port is a
		// PON port then only the device graph will be generated.
		if device, err := dMgr.GetDevice(ctx, deviceID); err == nil {
			go func() {
				err = dMgr.logicalDeviceMgr.updateLogicalPort(context.Background(), device, port)
				if err != nil {
					logger.Errorw("unable-to-update-logical-port", log.Fields{"error": err})
				}
			}()
		} else {
			logger.Errorw("failed-to-retrieve-device", log.Fields{"deviceId": deviceID})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) addFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	logger.Debugw("addFlowsAndGroups", log.Fields{"deviceid": deviceID, "groups:": groups, "flowMetadata": flowMetadata})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.addFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) deleteFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	logger.Debugw("deleteFlowsAndGroups", log.Fields{"deviceid": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.deleteFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) updateFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	logger.Debugw("updateFlowsAndGroups", log.Fields{"deviceid": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// UpdatePmConfigs updates the PM configs.  This is executed when the northbound gRPC API is invoked, typically
// following a user action
func (dMgr *Manager) UpdatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs, ch chan interface{}) {
	var res interface{}
	if pmConfigs.Id == "" {
		res = status.Errorf(codes.FailedPrecondition, "invalid-device-Id")
	} else if agent := dMgr.getDeviceAgent(ctx, pmConfigs.Id); agent != nil {
		res = agent.updatePmConfigs(ctx, pmConfigs)
	} else {
		res = status.Errorf(codes.NotFound, "%s", pmConfigs.Id)
	}
	sendResponse(ctx, ch, res)
}

// InitPmConfigs initialize the pm configs as defined by the adapter.
func (dMgr *Manager) InitPmConfigs(ctx context.Context, deviceID string, pmConfigs *voltha.PmConfigs) error {
	if pmConfigs.Id == "" {
		return status.Errorf(codes.FailedPrecondition, "invalid-device-Id")
	}
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.initPmConfigs(ctx, pmConfigs)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) ListPmConfigs(ctx context.Context, deviceID string) (*voltha.PmConfigs, error) {
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.listPmConfigs(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) getSwitchCapability(ctx context.Context, deviceID string) (*ic.SwitchCapability, error) {
	logger.Debugw("getSwitchCapability", log.Fields{"deviceid": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.getSwitchCapability(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) GetPorts(ctx context.Context, deviceID string, portType voltha.Port_PortType) (*voltha.Ports, error) {
	logger.Debugw("GetPorts", log.Fields{"deviceid": deviceID, "portType": portType})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.getPorts(ctx, portType), nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) getPortCapability(ctx context.Context, deviceID string, portNo uint32) (*ic.PortCapability, error) {
	logger.Debugw("getPortCapability", log.Fields{"deviceid": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.getPortCapability(ctx, portNo)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) UpdateDeviceStatus(ctx context.Context, deviceID string, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	logger.Debugw("UpdateDeviceStatus", log.Fields{"deviceid": deviceID, "operStatus": operStatus, "connStatus": connStatus})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateDeviceStatus(ctx, operStatus, connStatus)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) UpdateChildrenStatus(ctx context.Context, deviceID string, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	logger.Debugw("UpdateChildrenStatus", log.Fields{"parentDeviceid": deviceID, "operStatus": operStatus, "connStatus": connStatus})
	var parentDevice *voltha.Device
	var err error
	if parentDevice, err = dMgr.GetDevice(ctx, deviceID); err != nil {
		return status.Errorf(codes.Aborted, "%s", err.Error())
	}
	var childDeviceIds []string
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if len(childDeviceIds) == 0 {
		logger.Debugw("no-child-device", log.Fields{"parentDeviceId": parentDevice.Id})
	}
	for _, childDeviceID := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err = agent.updateDeviceStatus(ctx, operStatus, connStatus); err != nil {
				return status.Errorf(codes.Aborted, "childDevice:%s, error:%s", childDeviceID, err.Error())
			}
		}
	}
	return nil
}

func (dMgr *Manager) UpdatePortState(ctx context.Context, deviceID string, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Debugw("UpdatePortState", log.Fields{"deviceid": deviceID, "portType": portType, "portNo": portNo, "operStatus": operStatus})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if err := agent.updatePortState(ctx, portType, portNo, operStatus); err != nil {
			logger.Errorw("updating-port-state-failed", log.Fields{"deviceid": deviceID, "portNo": portNo, "error": err})
			return err
		}
		// Notify the logical device manager to change the port state
		// Do this for NNI and UNIs only. PON ports are not known by logical device
		if portType == voltha.Port_ETHERNET_NNI || portType == voltha.Port_ETHERNET_UNI {
			go func() {
				err := dMgr.logicalDeviceMgr.updatePortState(context.Background(), deviceID, portNo, operStatus)
				if err != nil {
					// While we want to handle (catch) and log when
					// an update to a port was not able to be
					// propagated to the logical port, we can report
					// it as a warning and not an error because it
					// doesn't stop or modify processing.
					// TODO: VOL-2707
					logger.Warnw("unable-to-update-logical-port-state", log.Fields{"error": err})
				}
			}()
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) DeleteAllPorts(ctx context.Context, deviceID string) error {
	logger.Debugw("DeleteAllPorts", log.Fields{"deviceid": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if err := agent.deleteAllPorts(ctx); err != nil {
			return err
		}
		// Notify the logical device manager to remove all logical ports, if needed.
		// At this stage the device itself may gave been deleted already at a DeleteAllPorts
		// typically is part of a device deletion phase.
		if device, err := dMgr.GetDevice(ctx, deviceID); err == nil {
			go func() {
				err = dMgr.logicalDeviceMgr.deleteAllLogicalPorts(ctx, device)
				if err != nil {
					logger.Errorw("unable-to-delete-logical-ports", log.Fields{"error": err})
				}
			}()
		} else {
			logger.Warnw("failed-to-retrieve-device", log.Fields{"deviceId": deviceID})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

//UpdatePortsState updates all ports on the device
func (dMgr *Manager) UpdatePortsState(ctx context.Context, deviceID string, state voltha.OperStatus_Types) error {
	logger.Debugw("UpdatePortsState", log.Fields{"deviceid": deviceID})

	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		switch state {
		case voltha.OperStatus_ACTIVE:
			if err := agent.updatePortsOperState(ctx, state); err != nil {
				logger.Warnw("updatePortsOperState-failed", log.Fields{"deviceId": deviceID, "error": err})
				return err
			}
		case voltha.OperStatus_UNKNOWN:
			if err := agent.updatePortsOperState(ctx, state); err != nil {
				logger.Warnw("updatePortsOperState-failed", log.Fields{"deviceId": deviceID, "error": err})
				return err
			}
		default:
			return status.Error(codes.Unimplemented, "state-change-not-implemented")
		}
		// Notify the logical device about the state change
		device, err := dMgr.GetDevice(ctx, deviceID)
		if err != nil {
			logger.Warnw("non-existent-device", log.Fields{"deviceId": deviceID, "error": err})
			return err
		}
		if err := dMgr.logicalDeviceMgr.updatePortsState(ctx, device, state); err != nil {
			logger.Warnw("failed-updating-ports-state", log.Fields{"deviceId": deviceID, "error": err})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) ChildDeviceDetected(ctx context.Context, parentDeviceID string, parentPortNo int64, deviceType string,
	channelID int64, vendorID string, serialNumber string, onuID int64) (*voltha.Device, error) {
	logger.Debugw("ChildDeviceDetected", log.Fields{"parentDeviceId": parentDeviceID, "parentPortNo": parentPortNo, "deviceType": deviceType, "channelId": channelID, "vendorId": vendorID, "serialNumber": serialNumber, "onuId": onuID})

	if deviceType == "" && vendorID != "" {
		logger.Debug("device-type-is-nil-fetching-device-type")
	OLoop:
		for _, dType := range dMgr.adapterMgr.ListDeviceTypes() {
			for _, v := range dType.VendorIds {
				if v == vendorID {
					deviceType = dType.Adapter
					break OLoop
				}
			}
		}
	}
	//if no match found for the vendorid,report adapter with the custom error message
	if deviceType == "" {
		logger.Errorw("failed-to-fetch-adapter-name ", log.Fields{"vendorId": vendorID})
		return nil, status.Errorf(codes.NotFound, "%s", vendorID)
	}

	// Create the ONU device
	childDevice := &voltha.Device{}
	childDevice.Type = deviceType
	childDevice.ParentId = parentDeviceID
	childDevice.ParentPortNo = uint32(parentPortNo)
	childDevice.VendorId = vendorID
	childDevice.SerialNumber = serialNumber
	childDevice.Root = false

	// Get parent device type
	pAgent := dMgr.getDeviceAgent(ctx, parentDeviceID)
	if pAgent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
	}
	if pAgent.deviceType == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "device Type not set %s", parentDeviceID)
	}

	if device, err := dMgr.GetChildDevice(ctx, parentDeviceID, serialNumber, onuID, parentPortNo); err == nil {
		logger.Warnw("child-device-exists", log.Fields{"parentId": parentDeviceID, "serialNumber": serialNumber})
		return device, status.Errorf(codes.AlreadyExists, "%s", serialNumber)
	}

	childDevice.ProxyAddress = &voltha.Device_ProxyAddress{DeviceId: parentDeviceID, DeviceType: pAgent.deviceType, ChannelId: uint32(channelID), OnuId: uint32(onuID)}

	// Create and start a device agent for that device
	agent := newAgent(dMgr.adapterProxy, childDevice, dMgr, dMgr.clusterDataProxy, dMgr.defaultTimeout)
	childDevice, err := agent.start(ctx, childDevice)
	if err != nil {
		logger.Errorw("error-starting-child-device", log.Fields{"parent-device-id": childDevice.ParentId, "child-device-id": agent.deviceID, "error": err})
		return nil, err
	}
	dMgr.addDeviceAgentToMap(agent)

	// Activate the child device
	if agent = dMgr.getDeviceAgent(ctx, agent.deviceID); agent != nil {
		go func() {
			err := agent.enableDevice(context.Background())
			if err != nil {
				logger.Errorw("unable-to-enable-device", log.Fields{"error": err})
			}
		}()
	}

	// Publish on the messaging bus that we have discovered new devices
	go func() {
		err := dMgr.kafkaICProxy.DeviceDiscovered(agent.deviceID, deviceType, parentDeviceID, dMgr.coreInstanceID)
		if err != nil {
			logger.Errorw("unable-to-discover-the-device", log.Fields{"error": err})
		}
	}()

	return childDevice, nil
}

func (dMgr *Manager) processTransition(ctx context.Context, device *voltha.Device, previousState *deviceState) error {
	// This will be triggered on every state update
	logger.Debugw("state-transition", log.Fields{
		"device":           device.Id,
		"prev-admin-state": previousState.Admin,
		"prev-oper-state":  previousState.Operational,
		"prev-conn-state":  previousState.Connection,
		"curr-admin-state": device.AdminState,
		"curr-oper-state":  device.OperStatus,
		"curr-conn-state":  device.ConnectStatus,
	})
	handlers := dMgr.stateTransitions.GetTransitionHandler(device, previousState)
	if handlers == nil {
		logger.Debugw("no-op-transition", log.Fields{"deviceId": device.Id})
		return nil
	}
	logger.Debugw("handler-found", log.Fields{"num-expectedHandlers": len(handlers), "isParent": device.Root, "current-data": device, "previous-state": previousState})
	for _, handler := range handlers {
		logger.Debugw("running-handler", log.Fields{"handler": funcName(handler)})
		if err := handler(ctx, device); err != nil {
			logger.Warnw("handler-failed", log.Fields{"handler": funcName(handler), "error": err})
			return err
		}
	}
	return nil
}

func (dMgr *Manager) packetOut(ctx context.Context, deviceID string, outPort uint32, packet *ofp.OfpPacketOut) error {
	logger.Debugw("packetOut", log.Fields{"deviceId": deviceID, "outPort": outPort})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.packetOut(ctx, outPort, packet)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// PacketIn receives packet from adapter
func (dMgr *Manager) PacketIn(ctx context.Context, deviceID string, port uint32, transactionID string, packet []byte) error {
	logger.Debugw("PacketIn", log.Fields{"deviceId": deviceID, "port": port})
	// Get the logical device Id based on the deviceId
	var device *voltha.Device
	var err error
	if device, err = dMgr.GetDevice(ctx, deviceID); err != nil {
		logger.Errorw("device-not-found", log.Fields{"deviceId": deviceID})
		return err
	}
	if !device.Root {
		logger.Errorw("device-not-root", log.Fields{"deviceId": deviceID})
		return status.Errorf(codes.FailedPrecondition, "%s", deviceID)
	}

	if err := dMgr.logicalDeviceMgr.packetIn(ctx, device.ParentId, port, transactionID, packet); err != nil {
		return err
	}
	return nil
}

func (dMgr *Manager) setParentID(ctx context.Context, device *voltha.Device, parentID string) error {
	logger.Debugw("setParentId", log.Fields{"deviceId": device.Id, "parentId": parentID})
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		return agent.setParentID(ctx, device, parentID)
	}
	return status.Errorf(codes.NotFound, "%s", device.Id)
}

// CreateLogicalDevice creates logical device in core
func (dMgr *Manager) CreateLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info("CreateLogicalDevice")
	// Verify whether the logical device has already been created
	if cDevice.ParentId != "" {
		logger.Debugw("Parent device already exist.", log.Fields{"deviceId": cDevice.Id, "logicalDeviceId": cDevice.Id})
		return nil
	}
	var err error
	if _, err = dMgr.logicalDeviceMgr.createLogicalDevice(ctx, cDevice); err != nil {
		logger.Warnw("createlogical-device-error", log.Fields{"device": cDevice})
		return err
	}
	return nil
}

// DeleteLogicalDevice deletes logical device from core
func (dMgr *Manager) DeleteLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info("DeleteLogicalDevice")
	var err error
	if err = dMgr.logicalDeviceMgr.deleteLogicalDevice(ctx, cDevice); err != nil {
		logger.Warnw("deleteLogical-device-error", log.Fields{"deviceId": cDevice.Id})
		return err
	}
	// Remove the logical device Id from the parent device
	logicalID := ""
	dMgr.UpdateDeviceAttribute(ctx, cDevice.Id, "ParentId", logicalID)
	return nil
}

// DeleteLogicalPort removes the logical port associated with a device
func (dMgr *Manager) DeleteLogicalPort(ctx context.Context, device *voltha.Device) error {
	logger.Info("deleteLogicalPort")
	var err error
	// Get the logical port associated with this device
	var lPortID *voltha.LogicalPortId
	if lPortID, err = dMgr.logicalDeviceMgr.getLogicalPortID(ctx, device); err != nil {
		logger.Warnw("getLogical-port-error", log.Fields{"deviceId": device.Id, "error": err})
		return err
	}
	if err = dMgr.logicalDeviceMgr.deleteLogicalPort(ctx, lPortID); err != nil {
		logger.Warnw("deleteLogical-port-error", log.Fields{"deviceId": device.Id})
		return err
	}
	return nil
}

// DeleteLogicalPorts removes the logical ports associated with that deviceId
func (dMgr *Manager) DeleteLogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	logger.Debugw("delete-all-logical-ports", log.Fields{"device-id": cDevice.Id})
	if err := dMgr.logicalDeviceMgr.deleteLogicalPorts(ctx, cDevice.Id); err != nil {
		// Just log the error.   The logical device or port may already have been deleted before this callback is invoked.
		logger.Warnw("deleteLogical-ports-error", log.Fields{"device-id": cDevice.Id, "error": err})
	}
	return nil
}

func (dMgr *Manager) getParentDevice(ctx context.Context, childDevice *voltha.Device) *voltha.Device {
	//	Sanity check
	if childDevice.Root {
		// childDevice is the parent device
		return childDevice
	}
	parentDevice, _ := dMgr.GetDevice(ctx, childDevice.ParentId)
	return parentDevice
}

//ChildDevicesLost is invoked by an adapter to indicate that a parent device is in a state (Disabled) where it
//cannot manage the child devices.  This will trigger the Core to disable all the child devices.
func (dMgr *Manager) ChildDevicesLost(ctx context.Context, parentDeviceID string) error {
	logger.Debug("ChildDevicesLost")
	var err error
	var parentDevice *voltha.Device
	if parentDevice, err = dMgr.GetDevice(ctx, parentDeviceID); err != nil {
		logger.Warnw("failed-getting-device", log.Fields{"deviceId": parentDeviceID, "error": err})
		return err
	}
	return dMgr.DisableAllChildDevices(ctx, parentDevice)
}

//ChildDevicesDetected is invoked by an adapter when child devices are found, typically after after a
// disable/enable sequence.  This will trigger the Core to Enable all the child devices of that parent.
func (dMgr *Manager) ChildDevicesDetected(ctx context.Context, parentDeviceID string) error {
	logger.Debug("ChildDevicesDetected")
	var err error
	var parentDevice *voltha.Device
	var childDeviceIds []string

	if parentDevice, err = dMgr.GetDevice(ctx, parentDeviceID); err != nil {
		logger.Warnw("failed-getting-device", log.Fields{"deviceId": parentDeviceID, "error": err})
		return err
	}

	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		logger.Debugw("no-child-device", log.Fields{"parentDeviceId": parentDevice.Id})
	}
	allChildEnableRequestSent := true
	for _, childDeviceID := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			// Run the children re-registration in its own routine
			go func() {
				err = agent.enableDevice(ctx)
				if err != nil {
					logger.Errorw("unable-to-enable-device", log.Fields{"error": err})
				}
			}()
		} else {
			err = status.Errorf(codes.Unavailable, "no agent for child device %s", childDeviceID)
			logger.Errorw("no-child-device-agent", log.Fields{"parentDeviceId": parentDevice.Id, "childId": childDeviceID})
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
func (dMgr *Manager) DisableAllChildDevices(ctx context.Context, parentCurrDevice *voltha.Device) error {
	logger.Debug("DisableAllChildDevices")
	var childDeviceIds []string
	var err error
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentCurrDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", parentCurrDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		logger.Debugw("no-child-device", log.Fields{"parentDeviceId": parentCurrDevice.Id})
	}
	for _, childDeviceID := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err = agent.disableDevice(ctx); err != nil {
				// Just log the error - this error happens only if the child device was already in deleted state.
				logger.Errorw("failure-disable-device", log.Fields{"deviceId": childDeviceID, "error": err.Error()})
			}
		}
	}
	return nil
}

//DeleteAllChildDevices is invoked as a callback when the parent device is deleted
func (dMgr *Manager) DeleteAllChildDevices(ctx context.Context, parentCurrDevice *voltha.Device) error {
	logger.Debug("DeleteAllChildDevices")
	var childDeviceIds []string
	var err error
	if childDeviceIds, err = dMgr.getAllChildDeviceIds(parentCurrDevice); err != nil {
		return status.Errorf(codes.NotFound, "%s", parentCurrDevice.Id)
	}
	if len(childDeviceIds) == 0 {
		logger.Debugw("no-child-device", log.Fields{"parentDeviceId": parentCurrDevice.Id})
	}
	for _, childDeviceID := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err = agent.deleteDevice(ctx); err != nil {
				logger.Warnw("failure-delete-device", log.Fields{"deviceId": childDeviceID, "error": err.Error()})
			}
			// No further action is required here.  The deleteDevice will change the device state where the resulting
			// callback will take care of cleaning the child device agent.
		}
	}
	return nil
}

//DeleteAllUNILogicalPorts is invoked as a callback when the parent device is deleted
func (dMgr *Manager) DeleteAllUNILogicalPorts(ctx context.Context, curr *voltha.Device) error {
	logger.Debugw("delete-all-uni-logical-ports", log.Fields{"parent-device-id": curr.Id})
	if err := dMgr.logicalDeviceMgr.deleteAllUNILogicalPorts(ctx, curr); err != nil {
		// Just log the error and let the remaining pipeline proceed - ports may already have been deleted
		logger.Warnw("delete-all-uni-logical-ports-failed", log.Fields{"parent-device-id": curr.Id, "error": err})
	}
	return nil
}

//DeleteAllLogicalPorts is invoked as a callback when the parent device's connection status moves to UNREACHABLE
func (dMgr *Manager) DeleteAllLogicalPorts(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw("delete-all-logical-ports", log.Fields{"parent-device-id": parentDevice.Id})
	if err := dMgr.logicalDeviceMgr.deleteAllLogicalPorts(ctx, parentDevice); err != nil {
		// Just log error as logical device may already have been deleted
		logger.Warnw("delete-all-logical-ports-fail", log.Fields{"parent-device-id": parentDevice.Id, "error": err})
	}
	return nil
}

//DeleteAllDeviceFlows is invoked as a callback when the parent device's connection status moves to UNREACHABLE
func (dMgr *Manager) DeleteAllDeviceFlows(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw("delete-all-device-flows", log.Fields{"parent-device-id": parentDevice.Id})
	if agent := dMgr.getDeviceAgent(ctx, parentDevice.Id); agent != nil {
		if err := agent.deleteAllFlows(ctx); err != nil {
			logger.Errorw("error-deleting-all-device-flows", log.Fields{"parent-device-id": parentDevice.Id})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
}

//getAllChildDeviceIds is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *Manager) getAllChildDeviceIds(parentDevice *voltha.Device) ([]string, error) {
	logger.Debugw("getAllChildDeviceIds", log.Fields{"parentDeviceId": parentDevice.Id})
	childDeviceIds := make([]string, 0)
	if parentDevice != nil {
		for _, port := range parentDevice.Ports {
			for _, peer := range port.Peers {
				childDeviceIds = append(childDeviceIds, peer.DeviceId)
			}
		}
		logger.Debugw("returning-getAllChildDeviceIds", log.Fields{"parentDeviceId": parentDevice.Id, "childDeviceIds": childDeviceIds})
	}
	return childDeviceIds, nil
}

//GetAllChildDevices is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *Manager) GetAllChildDevices(ctx context.Context, parentDeviceID string) (*voltha.Devices, error) {
	logger.Debugw("GetAllChildDevices", log.Fields{"parentDeviceId": parentDeviceID})
	if parentDevice, err := dMgr.GetDevice(ctx, parentDeviceID); err == nil {
		childDevices := make([]*voltha.Device, 0)
		if childDeviceIds, er := dMgr.getAllChildDeviceIds(parentDevice); er == nil {
			for _, deviceID := range childDeviceIds {
				if d, e := dMgr.GetDevice(ctx, deviceID); e == nil && d != nil {
					childDevices = append(childDevices, d)
				}
			}
		}
		return &voltha.Devices{Items: childDevices}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
}

// SetupUNILogicalPorts creates UNI ports on the logical device that represents a child UNI interface
func (dMgr *Manager) SetupUNILogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info("addUNILogicalPort")
	if err := dMgr.logicalDeviceMgr.setupUNILogicalPorts(ctx, cDevice); err != nil {
		logger.Warnw("addUNILogicalPort-error", log.Fields{"device": cDevice, "err": err})
		return err
	}
	return nil
}

func (dMgr *Manager) DownloadImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	logger.Debugw("DownloadImage", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		if res, err = agent.downloadImage(ctx, img); err != nil {
			logger.Debugw("DownloadImage-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	logger.Debugw("CancelImageDownload", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		if res, err = agent.cancelImageDownload(ctx, img); err != nil {
			logger.Debugw("CancelImageDownload-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) ActivateImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	logger.Debugw("ActivateImage", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		if res, err = agent.activateImage(ctx, img); err != nil {
			logger.Debugw("ActivateImage-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) RevertImage(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	logger.Debugw("RevertImage", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		if res, err = agent.revertImage(ctx, img); err != nil {
			logger.Debugw("RevertImage-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload, ch chan interface{}) {
	logger.Debugw("GetImageDownloadStatus", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	var res interface{}
	var err error
	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		if res, err = agent.getImageDownloadStatus(ctx, img); err != nil {
			logger.Debugw("GetImageDownloadStatus-failed", log.Fields{"err": err, "imageName": img.Name})
			res = err
		}
	} else {
		res = status.Errorf(codes.NotFound, "%s", img.Id)
	}
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) UpdateImageDownload(ctx context.Context, deviceID string, img *voltha.ImageDownload) error {
	logger.Debugw("UpdateImageDownload", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if err := agent.updateImageDownload(ctx, img); err != nil {
			logger.Debugw("UpdateImageDownload-failed", log.Fields{"err": err, "imageName": img.Name})
			return err
		}
	} else {
		return status.Errorf(codes.NotFound, "%s", img.Id)
	}
	return nil
}

func (dMgr *Manager) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw("GetImageDownload", log.Fields{"deviceid": img.Id, "imageName": img.Name})
	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		return agent.getImageDownload(ctx, img)
	}
	return nil, status.Errorf(codes.NotFound, "%s", img.Id)
}

func (dMgr *Manager) ListImageDownloads(ctx context.Context, deviceID string) (*voltha.ImageDownloads, error) {
	logger.Debugw("ListImageDownloads", log.Fields{"deviceID": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.listImageDownloads(ctx, deviceID)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) NotifyInvalidTransition(_ context.Context, device *voltha.Device) error {
	logger.Errorw("NotifyInvalidTransition", log.Fields{
		"device":           device.Id,
		"curr-admin-state": device.AdminState,
		"curr-oper-state":  device.OperStatus,
		"curr-conn-state":  device.ConnectStatus,
	})
	//TODO: notify over kafka?
	return nil
}

func funcName(f interface{}) string {
	p := reflect.ValueOf(f).Pointer()
	rf := runtime.FuncForPC(p)
	return rf.Name()
}

// UpdateDeviceAttribute updates value of particular device attribute
func (dMgr *Manager) UpdateDeviceAttribute(ctx context.Context, deviceID string, attribute string, value interface{}) {
	if agent, ok := dMgr.deviceAgents.Load(deviceID); ok {
		agent.(*Agent).updateDeviceAttribute(ctx, attribute, value)
	}
}

// GetParentDeviceID returns parent device id, either from memory or from the dB, if present
func (dMgr *Manager) GetParentDeviceID(ctx context.Context, deviceID string) string {
	if device, _ := dMgr.GetDevice(ctx, deviceID); device != nil {
		logger.Infow("GetParentDeviceId", log.Fields{"deviceId": device.Id, "parentId": device.ParentId})
		return device.ParentId
	}
	return ""
}

func (dMgr *Manager) SimulateAlarm(ctx context.Context, simulatereq *voltha.SimulateAlarmRequest, ch chan interface{}) {
	logger.Debugw("SimulateAlarm", log.Fields{"id": simulatereq.Id, "Indicator": simulatereq.Indicator, "IntfId": simulatereq.IntfId,
		"PortTypeName": simulatereq.PortTypeName, "OnuDeviceId": simulatereq.OnuDeviceId, "InverseBitErrorRate": simulatereq.InverseBitErrorRate,
		"Drift": simulatereq.Drift, "NewEqd": simulatereq.NewEqd, "OnuSerialNumber": simulatereq.OnuSerialNumber, "Operation": simulatereq.Operation})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, simulatereq.Id); agent != nil {
		res = agent.simulateAlarm(ctx, simulatereq)
		logger.Debugw("SimulateAlarm-result", log.Fields{"result": res})
	}
	//TODO CLI always get successful response
	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) UpdateDeviceReason(ctx context.Context, deviceID string, reason string) error {
	logger.Debugw("UpdateDeviceReason", log.Fields{"deviceid": deviceID, "reason": reason})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateDeviceReason(ctx, reason)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) EnablePort(ctx context.Context, port *voltha.Port, ch chan interface{}) {
	logger.Debugw("EnablePort", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, port.DeviceId); agent != nil {
		res = agent.enablePort(ctx, port)
		logger.Debugw("EnablePort-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", port.DeviceId)
	}

	sendResponse(ctx, ch, res)
}

func (dMgr *Manager) DisablePort(ctx context.Context, port *voltha.Port, ch chan interface{}) {
	logger.Debugw("DisablePort", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	var res interface{}
	if agent := dMgr.getDeviceAgent(ctx, port.DeviceId); agent != nil {
		res = agent.disablePort(ctx, port)
		logger.Debugw("DisablePort-result", log.Fields{"result": res})
	} else {
		res = status.Errorf(codes.NotFound, "%s", port.DeviceId)
	}

	sendResponse(ctx, ch, res)
}

// ChildDeviceLost  calls parent adapter to delete child device and all its references
func (dMgr *Manager) ChildDeviceLost(ctx context.Context, curr *voltha.Device) error {
	logger.Debugw("childDeviceLost", log.Fields{"child-device-id": curr.Id, "parent-device-id": curr.ParentId})
	if parentAgent := dMgr.getDeviceAgent(ctx, curr.ParentId); parentAgent != nil {
		if err := parentAgent.ChildDeviceLost(ctx, curr); err != nil {
			// Just log the message and let the remaining pipeline proceed.
			logger.Warnw("childDeviceLost", log.Fields{"child-device-id": curr.Id, "parent-device-id": curr.ParentId, "error": err})
		}
	}
	// Do not return an error as parent device may also have been deleted.  Let the remaining pipeline proceed.
	return nil
}

func (dMgr *Manager) StartOmciTest(ctx context.Context, omcitestrequest *voltha.OmciTestRequest) (*voltha.TestResponse, error) {
	logger.Debugw("Omci_test_Request", log.Fields{"device-id": omcitestrequest.Id, "uuid": omcitestrequest.Uuid})
	if agent := dMgr.getDeviceAgent(ctx, omcitestrequest.Id); agent != nil {
		res, err := agent.startOmciTest(ctx, omcitestrequest)
		if err != nil {
			return nil, err
		}
		logger.Debugw("Omci_test_Response_result-device-magnager", log.Fields{"result": res})
		return res, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", omcitestrequest.Id)
}
