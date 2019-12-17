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
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
	"github.com/opencord/voltha-protos/v2/go/inter_container"
	"reflect"
	"sync"
	"time"

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

// updateCommunicationTime updates the message to the specified time.
// No attempt is made to save the time to the db, so only recent times are guaranteed to be accurate.
func (aa *AdapterAgent) updateCommunicationTime(new time.Time) {
	// only update if new time is not in the future, and either the old time is invalid or new time > old time
	if last, err := ptypes.Timestamp(aa.adapter.LastCommunication); !new.After(time.Now()) && (err != nil || new.After(last)) {
		timestamp, err := ptypes.TimestampProto(new)
		if err != nil {
			return // if the new time cannot be encoded, just ignore it
		}

		aa.lock.Lock()
		defer aa.lock.Unlock()
		aa.adapter.LastCommunication = timestamp
	}
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

func newAdapterManager(cdProxy *model.Proxy, coreInstanceID string, kafkaClient kafka.Client, deviceMgr *DeviceManager) *AdapterManager {
	aMgr := &AdapterManager{
		exitChannel:            make(chan int, 1),
		coreInstanceID:         coreInstanceID,
		clusterDataProxy:       cdProxy,
		adapterAgents:          make(map[string]*AdapterAgent),
		deviceTypeToAdapterMap: make(map[string]string),
		deviceMgr:              deviceMgr,
	}
	kafkaClient.SubscribeForMetadata(aMgr.updateLastAdapterCommunication)
	return aMgr
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
		aMgr.addAdapter(&voltha.Adapter{Id: SentinelAdapterID}, true)
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
		aMgr.addDeviceTypes(&voltha.DeviceTypes{Items: []*voltha.DeviceType{{Id: SentinelDevicetypeID, Adapter: SentinelAdapterID}}}, true)
	}
}

func (aMgr *AdapterManager) updateLastAdapterCommunication(header *inter_container.Header) {
	aMgr.lockAdaptersMap.RLock()
	adapterAgent, have := aMgr.adapterAgents[header.FromTopic]
	aMgr.lockAdaptersMap.RUnlock()

	if have {
		adapterAgent.updateCommunicationTime(time.Unix(header.Timestamp/1000, header.Timestamp%1000*1000))
	}
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

func (aMgr *AdapterManager) registerAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) *voltha.CoreInstance {
	log.Debugw("registerAdapter", log.Fields{"adapter": adapter, "deviceTypes": deviceTypes.Items})

	if aMgr.getAdapter(adapter.Id) != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		go func() {
			err := aMgr.deviceMgr.adapterRestarted(adapter)
			if err != nil {
				log.Errorw("unable-to-restart-adapter", log.Fields{"error": err})
			}
		}()
		return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}
	}
	// Save the adapter and the device types
	aMgr.addAdapter(adapter, true)
	aMgr.addDeviceTypes(deviceTypes, true)

	log.Debugw("adapter-registered", log.Fields{"adapter": adapter.Id})

	return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}
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
