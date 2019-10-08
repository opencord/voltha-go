/*
 * Copyright 2019-present Open Networking Foundation

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
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/common/probe"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-protos/go/voltha"
	"reflect"
	"sync"
)

const (
	SENTINEL_ADAPTER_ID    = "adapter_sentinel"
	SENTINEL_DEVICETYPE_ID = "device_type_sentinel"
)

type AdapterAgent struct {
	adapter     *voltha.Adapter
	deviceTypes map[string]*voltha.DeviceType
	lock        sync.RWMutex
}

func newAdapterAgent(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) *AdapterAgent {
	var adapterAgent AdapterAgent
	adapterAgent.adapter = adapter
	adapterAgent.lock = sync.RWMutex{}
	adapterAgent.deviceTypes = make(map[string]*voltha.DeviceType)
	if deviceTypes != nil {
		for _, dType := range deviceTypes.Items {
			adapterAgent.deviceTypes[dType.Id] = dType
		}
	}
	return &adapterAgent
}

// Returns true if this device agent can handle this device Type
func (aa *AdapterAgent) handlesDeviceType(deviceType string) bool {
	aa.lock.RLock()
	defer aa.lock.RUnlock()
	_, exist := aa.deviceTypes[deviceType]
	return exist
}

func (aa *AdapterAgent) getDeviceType(deviceType string) *voltha.DeviceType {
	aa.lock.RLock()
	defer aa.lock.RUnlock()
	if _, exist := aa.deviceTypes[deviceType]; exist {
		return aa.deviceTypes[deviceType]
	}
	return nil
}

func (aa *AdapterAgent) getAdapter() *voltha.Adapter {
	aa.lock.RLock()
	defer aa.lock.RUnlock()
	log.Debugw("getAdapter", log.Fields{"adapter": aa.adapter})
	return aa.adapter
}

func (aa *AdapterAgent) updateAdapter(adapter *voltha.Adapter) {
	aa.lock.Lock()
	defer aa.lock.Unlock()
	aa.adapter = adapter
}

func (aa *AdapterAgent) updateDeviceType(deviceType *voltha.DeviceType) {
	aa.lock.Lock()
	defer aa.lock.Unlock()
	aa.deviceTypes[deviceType.Id] = deviceType
}

type AdapterManager struct {
	adapterAgents               map[string]*AdapterAgent
	deviceTypeToAdapterMap      map[string]string
	clusterDataProxy            *model.Proxy
	adapterProxy                *model.Proxy
	deviceTypeProxy             *model.Proxy
	deviceMgr                   *DeviceManager
	coreInstanceId              string
	exitChannel                 chan int
	lockAdaptersMap             sync.RWMutex
	lockdDeviceTypeToAdapterMap sync.RWMutex
}

func newAdapterManager(cdProxy *model.Proxy, coreInstanceId string, deviceMgr *DeviceManager) *AdapterManager {
	var adapterMgr AdapterManager
	adapterMgr.exitChannel = make(chan int, 1)
	adapterMgr.coreInstanceId = coreInstanceId
	adapterMgr.clusterDataProxy = cdProxy
	adapterMgr.adapterAgents = make(map[string]*AdapterAgent)
	adapterMgr.deviceTypeToAdapterMap = make(map[string]string)
	adapterMgr.lockAdaptersMap = sync.RWMutex{}
	adapterMgr.lockdDeviceTypeToAdapterMap = sync.RWMutex{}
	adapterMgr.deviceMgr = deviceMgr
	return &adapterMgr
}

func (aMgr *AdapterManager) start(ctx context.Context) {
	log.Info("starting-adapter-manager")

	// Load the existing adapterAgents and device types - this will also ensure the correct paths have been
	// created if there are no data in the dB to start
	aMgr.loadAdaptersAndDevicetypesInMemory()

	//// Create the proxies
	aMgr.adapterProxy = aMgr.clusterDataProxy.CreateProxy(context.Background(), "/adapters", false)
	aMgr.deviceTypeProxy = aMgr.clusterDataProxy.CreateProxy(context.Background(), "/device_types", false)

	// Register the callbacks
	aMgr.adapterProxy.RegisterCallback(model.POST_UPDATE, aMgr.adapterUpdated)
	aMgr.deviceTypeProxy.RegisterCallback(model.POST_UPDATE, aMgr.deviceTypesUpdated)
	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusRunning)
	log.Info("adapter-manager-started")
}

func (aMgr *AdapterManager) stop(ctx context.Context) {
	log.Info("stopping-device-manager")
	aMgr.exitChannel <- 1
	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusStopped)
	log.Info("device-manager-stopped")
}

//loadAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *AdapterManager) loadAdaptersAndDevicetypesInMemory() {
	// Load the adapters
	if adaptersIf := aMgr.clusterDataProxy.List(context.Background(), "/adapters", 0, false, ""); adaptersIf != nil {
		for _, adapterIf := range adaptersIf.([]interface{}) {
			if adapter, ok := adapterIf.(*voltha.Adapter); ok {
				log.Debugw("found-existing-adapter", log.Fields{"adapterId": adapter.Id})
				aMgr.addAdapter(adapter, false)
			}
		}
	} else {
		log.Debug("no-existing-adapter-found")
		//	No adapter data.   In order to have a proxy setup for that path let's create a fake adapter
		aMgr.addAdapter(&voltha.Adapter{Id: SENTINEL_ADAPTER_ID}, true)
	}

	// Load the device types
	if deviceTypesIf := aMgr.clusterDataProxy.List(context.Background(), "/device_types", 0, false, ""); deviceTypesIf != nil {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, deviceTypeIf := range deviceTypesIf.([]interface{}) {
			if dType, ok := deviceTypeIf.(*voltha.DeviceType); ok {
				log.Debugw("found-existing-device-types", log.Fields{"deviceTypes": dTypes})
				dTypes.Items = append(dTypes.Items, dType)
			}
		}
		aMgr.addDeviceTypes(dTypes, false)
	} else {
		log.Debug("no-existing-device-type-found")
		//	No device types data.   In order to have a proxy setup for that path let's create a fake device type
		aMgr.addDeviceTypes(&voltha.DeviceTypes{Items: []*voltha.DeviceType{{Id: SENTINEL_DEVICETYPE_ID, Adapter: SENTINEL_ADAPTER_ID}}}, true)
	}
}

//updateAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *AdapterManager) updateAdaptersAndDevicetypesInMemory(adapter *voltha.Adapter) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()

	if adapterAgent, ok := aMgr.adapterAgents[adapter.Id]; ok {
		if adapterAgent.getAdapter() != nil {
			// Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
			go aMgr.deviceMgr.adapterRestarted(adapter)
			return
		}
	}

	// Update the adapters
	if adaptersIf := aMgr.clusterDataProxy.List(context.Background(), "/adapters", 0, false, ""); adaptersIf != nil {
		for _, adapterIf := range adaptersIf.([]interface{}) {
			if adapter, ok := adapterIf.(*voltha.Adapter); ok {
				log.Debugw("found-existing-adapter", log.Fields{"adapterId": adapter.Id})
				aMgr.updateAdapterWithoutLock(adapter)
			}
		}
	}
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	// Update the device types
	if deviceTypesIf := aMgr.clusterDataProxy.List(context.Background(), "/device_types", 0, false, ""); deviceTypesIf != nil {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, deviceTypeIf := range deviceTypesIf.([]interface{}) {
			if dType, ok := deviceTypeIf.(*voltha.DeviceType); ok {
				log.Debugw("found-existing-device-types", log.Fields{"deviceTypes": dTypes})
				aMgr.updateDeviceTypeWithoutLock(dType)
			}
		}
	}
}

func (aMgr *AdapterManager) addAdapter(adapter *voltha.Adapter, saveToDb bool) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	log.Debugw("adding-adapter", log.Fields{"adapter": adapter})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		clonedAdapter := (proto.Clone(adapter)).(*voltha.Adapter)
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(clonedAdapter, nil)
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			if kvAdapter := aMgr.clusterDataProxy.Get(context.Background(), "/adapters/"+adapter.Id, 0, false, ""); kvAdapter == nil {
				if added := aMgr.clusterDataProxy.AddWithID(context.Background(), "/adapters", adapter.Id, clonedAdapter, ""); added == nil {
					//TODO:  Errors when saving to KV would require a separate go routine to be launched and try the saving again
					log.Errorw("failed-to-save-adapter", log.Fields{"adapter": adapter})
				} else {
					log.Debugw("adapter-saved-to-KV-Store", log.Fields{"adapter": adapter})
				}
			}
		}
	}
}

func (aMgr *AdapterManager) addDeviceTypes(deviceTypes *voltha.DeviceTypes, saveToDb bool) {
	if deviceTypes == nil {
		return
	}
	log.Debugw("adding-device-types", log.Fields{"deviceTypes": deviceTypes})
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	for _, deviceType := range deviceTypes.Items {
		clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
		if adapterAgent, exist := aMgr.adapterAgents[clonedDType.Adapter]; exist {
			adapterAgent.updateDeviceType(clonedDType)
		} else {
			log.Debugw("adapter-not-exist", log.Fields{"deviceTypes": deviceTypes, "adapterId": clonedDType.Adapter})
			aMgr.adapterAgents[clonedDType.Adapter] = newAdapterAgent(&voltha.Adapter{Id: clonedDType.Adapter}, deviceTypes)
		}
		aMgr.deviceTypeToAdapterMap[clonedDType.Id] = clonedDType.Adapter
	}
	if saveToDb {
		// Save the device types to the KV store as well
		for _, deviceType := range deviceTypes.Items {
			if dType := aMgr.clusterDataProxy.Get(context.Background(), "/device_types/"+deviceType.Id, 0, false, ""); dType == nil {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				if added := aMgr.clusterDataProxy.AddWithID(context.Background(), "/device_types", deviceType.Id, clonedDType, ""); added == nil {
					log.Errorw("failed-to-save-deviceType", log.Fields{"deviceType": deviceType})
				} else {
					log.Debugw("device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
				}
			}
		}
	}
}

func (aMgr *AdapterManager) listAdapters(ctx context.Context) (*voltha.Adapters, error) {
	result := &voltha.Adapters{Items: []*voltha.Adapter{}}
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		if a := adapterAgent.getAdapter(); a != nil {
			if a.Id != SENTINEL_ADAPTER_ID { // don't report the sentinel
				result.Items = append(result.Items, (proto.Clone(a)).(*voltha.Adapter))
			}
		}
	}
	return result, nil
}

func (aMgr *AdapterManager) deleteAdapter(adapterId string) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	delete(aMgr.adapterAgents, adapterId)
}

func (aMgr *AdapterManager) getAdapter(adapterId string) *voltha.Adapter {
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	if adapterAgent, ok := aMgr.adapterAgents[adapterId]; ok {
		return adapterAgent.getAdapter()
	}
	return nil
}

//updateAdapter updates an adapter if it exist.  Otherwise, it creates it.
func (aMgr *AdapterManager) updateAdapter(adapter *voltha.Adapter) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	aMgr.updateAdapterWithoutLock(adapter)
}

func (aMgr *AdapterManager) updateAdapterWithoutLock(adapter *voltha.Adapter) {
	if adapterAgent, ok := aMgr.adapterAgents[adapter.Id]; ok {
		adapterAgent.updateAdapter(adapter)
	} else {
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(adapter, nil)
	}
}

//updateDeviceType updates an adapter if it exist.  Otherwise, it creates it.
func (aMgr *AdapterManager) updateDeviceType(deviceType *voltha.DeviceType) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	aMgr.updateDeviceTypeWithoutLock(deviceType)
}

func (aMgr *AdapterManager) updateDeviceTypeWithoutLock(deviceType *voltha.DeviceType) {
	if adapterAgent, exist := aMgr.adapterAgents[deviceType.Adapter]; exist {
		adapterAgent.updateDeviceType(deviceType)
	} else {
		aMgr.adapterAgents[deviceType.Adapter] = newAdapterAgent(&voltha.Adapter{Id: deviceType.Adapter},
			&voltha.DeviceTypes{Items: []*voltha.DeviceType{deviceType}})
	}
	aMgr.deviceTypeToAdapterMap[deviceType.Id] = deviceType.Adapter
}

func (aMgr *AdapterManager) registerAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) *voltha.CoreInstance {
	log.Debugw("registerAdapter", log.Fields{"adapter": adapter, "deviceTypes": deviceTypes.Items})

	if aMgr.getAdapter(adapter.Id) != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		go aMgr.deviceMgr.adapterRestarted(adapter)
		return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceId}
	}
	// Save the adapter and the device types
	aMgr.addAdapter(adapter, true)
	aMgr.addDeviceTypes(deviceTypes, true)

	log.Debugw("adapter-registered", log.Fields{"adapter": adapter.Id})

	return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceId}
}

//getAdapterName returns the name of the device adapter that service this device type
func (aMgr *AdapterManager) getAdapterName(deviceType string) (string, error) {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	if adapterId, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		return adapterId, nil
	}
	return "", errors.New(fmt.Sprintf("Adapter-not-registered-for-device-type %s", deviceType))
}

// getDeviceType returns the device type proto definition given the name of the device type
func (aMgr *AdapterManager) getDeviceType(deviceType string) *voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	if adapterId, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		if adapterAgent, _ := aMgr.adapterAgents[adapterId]; adapterAgent != nil {
			return adapterAgent.getDeviceType(deviceType)
		}
	}
	return nil
}

//adapterUpdated is a callback invoked when an adapter change has been noticed
func (aMgr *AdapterManager) adapterUpdated(args ...interface{}) interface{} {
	log.Debugw("updateAdapter-callback", log.Fields{"argsLen": len(args)})

	var previousData *voltha.Adapters
	var latestData *voltha.Adapters

	var ok bool
	if previousData, ok = args[0].(*voltha.Adapters); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}
	if latestData, ok = args[1].(*voltha.Adapters); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	if previousData != nil && latestData != nil {
		if reflect.DeepEqual(previousData.Items, latestData.Items) {
			log.Debug("update-not-required")
			return nil
		}
	}

	if latestData != nil {
		for _, adapter := range latestData.Items {
			aMgr.updateAdapter(adapter)
		}
	}

	return nil
}

//deviceTypesUpdated is a callback invoked when a device type change has been noticed
func (aMgr *AdapterManager) deviceTypesUpdated(args ...interface{}) interface{} {
	log.Debugw("deviceTypesUpdated-callback", log.Fields{"argsLen": len(args)})

	var previousData *voltha.DeviceTypes
	var latestData *voltha.DeviceTypes

	var ok bool
	if previousData, ok = args[0].(*voltha.DeviceTypes); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}

	if latestData, ok = args[1].(*voltha.DeviceTypes); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	if previousData != nil && latestData != nil {
		if reflect.DeepEqual(previousData.Items, latestData.Items) {
			log.Debug("update-not-required")
			return nil
		}
	}

	if latestData != nil {
		for _, dType := range latestData.Items {
			aMgr.updateDeviceType(dType)
		}
	}
	return nil
}
