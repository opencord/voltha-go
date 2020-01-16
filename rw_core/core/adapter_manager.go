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
	"fmt"
	"reflect"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/probe"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

// sentinel constants
const (
	SentinelAdapterID    = "adapter_sentinel"
	SentinelDevicetypeID = "device_type_sentinel"
)

// AdapterAgent represents adapter agent
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

// AdapterManager represents adapter manager attributes
type AdapterManager struct {
	adapterAgents               map[string]*AdapterAgent
	deviceTypeToAdapterMap      map[string]string
	clusterDataProxy            *model.Proxy
	adapterProxy                *model.Proxy
	deviceTypeProxy             *model.Proxy
	deviceMgr                   *DeviceManager
	coreInstanceID              string
	exitChannel                 chan int
	lockAdaptersMap             sync.RWMutex
	lockdDeviceTypeToAdapterMap sync.RWMutex
}

func newAdapterManager(cdProxy *model.Proxy, coreInstanceID string, deviceMgr *DeviceManager) *AdapterManager {
	var adapterMgr AdapterManager
	adapterMgr.exitChannel = make(chan int, 1)
	adapterMgr.coreInstanceID = coreInstanceID
	adapterMgr.clusterDataProxy = cdProxy
	adapterMgr.adapterAgents = make(map[string]*AdapterAgent)
	adapterMgr.deviceTypeToAdapterMap = make(map[string]string)
	adapterMgr.lockAdaptersMap = sync.RWMutex{}
	adapterMgr.lockdDeviceTypeToAdapterMap = sync.RWMutex{}
	adapterMgr.deviceMgr = deviceMgr
	return &adapterMgr
}

func (aMgr *AdapterManager) start(ctx context.Context) error {
	log.Info("starting-adapter-manager")

	// Load the existing adapterAgents and device types - this will also ensure the correct paths have been
	// created if there are no data in the dB to start
	err := aMgr.loadAdaptersAndDevicetypesInMemory()
	if err != nil {
		log.Errorw("Failed-to-load-adapters-and-device-types-in-memeory", log.Fields{"error": err})
		return err
	}

	//// Create the proxies
	aMgr.adapterProxy, err = aMgr.clusterDataProxy.CreateProxy(context.Background(), "/adapters", false)
	if err != nil {
		log.Errorw("Failed-to-create-adapter-proxy", log.Fields{"error": err})
		return err
	}
	aMgr.deviceTypeProxy, err = aMgr.clusterDataProxy.CreateProxy(context.Background(), "/device_types", false)
	if err != nil {
		log.Errorw("Failed-to-create-device-proxy", log.Fields{"error": err})
		return err
	}

	// Register the callbacks
	aMgr.adapterProxy.RegisterCallback(model.POST_UPDATE, aMgr.adapterUpdated)
	aMgr.deviceTypeProxy.RegisterCallback(model.POST_UPDATE, aMgr.deviceTypesUpdated)
	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusRunning)
	log.Info("adapter-manager-started")
	return nil
}

//loadAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *AdapterManager) loadAdaptersAndDevicetypesInMemory() error {
	// Load the adapters
	adaptersIf, err := aMgr.clusterDataProxy.List(context.Background(), "/adapters", 0, false, "")
	if err != nil {
		log.Errorw("Failed-to-list-adapters-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if adaptersIf != nil {
		for _, adapterIf := range adaptersIf.([]interface{}) {
			if adapter, ok := adapterIf.(*voltha.Adapter); ok {
				log.Debugw("found-existing-adapter", log.Fields{"adapterId": adapter.Id})
				return aMgr.addAdapter(adapter, false)
			}
		}
	} else {
		log.Debug("no-existing-adapter-found")
		//	No adapter data.   In order to have a proxy setup for that path let's create a fake adapter
		return aMgr.addAdapter(&voltha.Adapter{Id: SentinelAdapterID}, true)
	}

	// Load the device types
	deviceTypesIf, err := aMgr.clusterDataProxy.List(context.Background(), "/device_types", 0, false, "")
	if err != nil {
		log.Errorw("Failed-to-list-device-types-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if deviceTypesIf != nil {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, deviceTypeIf := range deviceTypesIf.([]interface{}) {
			if dType, ok := deviceTypeIf.(*voltha.DeviceType); ok {
				log.Debugw("found-existing-device-types", log.Fields{"deviceTypes": dTypes})
				dTypes.Items = append(dTypes.Items, dType)
			}
		}
		return aMgr.addDeviceTypes(dTypes, false)
	}

	log.Debug("no-existing-device-type-found")
	//	No device types data.   In order to have a proxy setup for that path let's create a fake device type
	return aMgr.addDeviceTypes(&voltha.DeviceTypes{Items: []*voltha.DeviceType{{Id: SentinelDevicetypeID, Adapter: SentinelAdapterID}}}, true)
}

//updateAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *AdapterManager) updateAdaptersAndDevicetypesInMemory(adapter *voltha.Adapter) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()

	if adapterAgent, ok := aMgr.adapterAgents[adapter.Id]; ok {
		if adapterAgent.getAdapter() != nil {
			// Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
			go func() {
				err := aMgr.deviceMgr.adapterRestarted(adapter)
				if err != nil {
					log.Errorw("unable-to-restart-adapter", log.Fields{"error": err})
				}
			}()
			return
		}
	}

	// Update the adapters
	adaptersIf, err := aMgr.clusterDataProxy.List(context.Background(), "/adapters", 0, false, "")
	if err != nil {
		log.Errorw("failed-to-list-adapters-from-cluster-proxy", log.Fields{"error": err})
		return
	}
	if adaptersIf != nil {
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
	deviceTypesIf, err := aMgr.clusterDataProxy.List(context.Background(), "/device_types", 0, false, "")
	if err != nil {
		log.Errorw("Failed-to-list-device-types-in-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	if deviceTypesIf != nil {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, deviceTypeIf := range deviceTypesIf.([]interface{}) {
			if dType, ok := deviceTypeIf.(*voltha.DeviceType); ok {
				log.Debugw("found-existing-device-types", log.Fields{"deviceTypes": dTypes})
				aMgr.updateDeviceTypeWithoutLock(dType)
			}
		}
	}
}

func (aMgr *AdapterManager) addAdapter(adapter *voltha.Adapter, saveToDb bool) error {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	log.Debugw("adding-adapter", log.Fields{"adapter": adapter})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		clonedAdapter := (proto.Clone(adapter)).(*voltha.Adapter)
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(clonedAdapter, nil)
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			kvAdapter, err := aMgr.clusterDataProxy.Get(context.Background(), "/adapters/"+adapter.Id, 0, false, "")
			if err != nil {
				log.Errorw("failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			}
			if kvAdapter == nil {
				added, err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/adapters", adapter.Id, clonedAdapter, "")
				if err != nil {
					log.Errorw("failed-to-save-adapter-to-cluster-proxy", log.Fields{"error": err})
					return err
				}
				if added == nil {
					//TODO:  Errors when saving to KV would require a separate go routine to be launched and try the saving again
					log.Errorw("failed-to-save-adapter", log.Fields{"adapter": adapter})
				} else {
					log.Debugw("adapter-saved-to-KV-Store", log.Fields{"adapter": adapter})
				}
			}
		}
	}
	return nil
}

func (aMgr *AdapterManager) addDeviceTypes(deviceTypes *voltha.DeviceTypes, saveToDb bool) error {
	if deviceTypes == nil {
		return fmt.Errorf("no-device-type")
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
			dType, err := aMgr.clusterDataProxy.Get(context.Background(), "/device_types/"+deviceType.Id, 0, false, "")
			if err != nil {
				log.Errorw("Failed-to--device-types-from-cluster-data-proxy", log.Fields{"error": err})
				return err
			}
			if dType == nil {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				added, err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/device_types", deviceType.Id, clonedDType, "")
				if err != nil {
					log.Errorw("Failed-to-add-device-types-to-cluster-data-proxy", log.Fields{"error": err})
					return err
				}
				if added == nil {
					log.Errorw("failed-to-save-deviceType", log.Fields{"deviceType": deviceType})
				} else {
					log.Debugw("device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
				}
			}
		}
	}
	return nil
}

func (aMgr *AdapterManager) listAdapters(ctx context.Context) (*voltha.Adapters, error) {
	result := &voltha.Adapters{Items: []*voltha.Adapter{}}
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		if a := adapterAgent.getAdapter(); a != nil {
			if a.Id != SentinelAdapterID { // don't report the sentinel
				result.Items = append(result.Items, (proto.Clone(a)).(*voltha.Adapter))
			}
		}
	}
	return result, nil
}

func (aMgr *AdapterManager) getAdapter(adapterID string) *voltha.Adapter {
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	if adapterAgent, ok := aMgr.adapterAgents[adapterID]; ok {
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

func (aMgr *AdapterManager) registerAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) (*voltha.CoreInstance, error) {
	log.Debugw("registerAdapter", log.Fields{"adapter": adapter, "deviceTypes": deviceTypes.Items})

	if aMgr.getAdapter(adapter.Id) != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		go func() {
			err := aMgr.deviceMgr.adapterRestarted(adapter)
			if err != nil {
				log.Errorw("unable-to-restart-adapter", log.Fields{"error": err})
			}
		}()
		return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
	}
	// Save the adapter and the device types
	if err := aMgr.addAdapter(adapter, true); err != nil {
		log.Errorw("failed-to-add-adapter", log.Fields{"error": err})
		return nil, err
	}
	if err := aMgr.addDeviceTypes(deviceTypes, true); err != nil {
		log.Errorw("failed-to-add-device-types", log.Fields{"error": err})
		return nil, err
	}

	log.Debugw("adapter-registered", log.Fields{"adapter": adapter.Id})

	return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
}

//getAdapterName returns the name of the device adapter that service this device type
func (aMgr *AdapterManager) getAdapterName(deviceType string) (string, error) {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	if adapterID, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		return adapterID, nil
	}
	return "", fmt.Errorf("Adapter-not-registered-for-device-type %s", deviceType)
}

func (aMgr *AdapterManager) listDeviceTypes() []*voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	deviceTypes := make([]*voltha.DeviceType, 0, len(aMgr.deviceTypeToAdapterMap))
	for deviceTypeID, adapterID := range aMgr.deviceTypeToAdapterMap {
		if adapterAgent, have := aMgr.adapterAgents[adapterID]; have {
			if deviceType := adapterAgent.getDeviceType(deviceTypeID); deviceType != nil {
				if deviceType.Id != SentinelDevicetypeID { // don't report the sentinel
					deviceTypes = append(deviceTypes, deviceType)
				}
			}
		}
	}
	return deviceTypes
}

// getDeviceType returns the device type proto definition given the name of the device type
func (aMgr *AdapterManager) getDeviceType(deviceType string) *voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	if adapterID, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		if adapterAgent := aMgr.adapterAgents[adapterID]; adapterAgent != nil {
			return adapterAgent.getDeviceType(deviceType)
		}
	}
	return nil
}

//adapterUpdated is a callback invoked when an adapter change has been noticed
func (aMgr *AdapterManager) adapterUpdated(ctx context.Context, args ...interface{}) interface{} {
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
func (aMgr *AdapterManager) deviceTypesUpdated(ctx context.Context, args ...interface{}) interface{} {
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
