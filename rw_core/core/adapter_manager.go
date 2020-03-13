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
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	"github.com/opencord/voltha-protos/v3/go/voltha"
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
	logger.Debugw("getAdapter", log.Fields{"adapter": aa.adapter})
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

// AdapterAgentKey is the key to the AdapterAgent map
type AdapterAgentKey struct {
	adapterID         string
	adapterInstanceID int32
}

// AdapterManager represents adapter manager attributes
type AdapterManager struct {
	adapterAgents               map[AdapterAgentKey]*AdapterAgent
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
		adapterAgents:          make(map[AdapterAgentKey]*AdapterAgent),
		deviceTypeToAdapterMap: make(map[string]string),
		deviceMgr:              deviceMgr,
	}
	// TODO discuss - this implementation assumes topic = adapterId
	//kafkaClient.SubscribeForMetadata(aMgr.updateLastAdapterCommunication)
	return aMgr
}

func (aMgr *AdapterManager) start(ctx context.Context) error {
	logger.Info("starting-adapter-manager")

	// Load the existing adapterAgents and device types - this will also ensure the correct paths have been
	// created if there are no data in the dB to start
	err := aMgr.loadAdaptersAndDevicetypesInMemory()
	if err != nil {
		logger.Errorw("Failed-to-load-adapters-and-device-types-in-memeory", log.Fields{"error": err})
		return err
	}

	//// Create the proxies
	aMgr.adapterProxy, err = aMgr.clusterDataProxy.CreateProxy(ctx, "/adapters", false)
	if err != nil {
		logger.Errorw("Failed-to-create-adapter-proxy", log.Fields{"error": err})
		return err
	}
	aMgr.deviceTypeProxy, err = aMgr.clusterDataProxy.CreateProxy(ctx, "/device_types", false)
	if err != nil {
		logger.Errorw("Failed-to-create-device-proxy", log.Fields{"error": err})
		return err
	}

	// Register the callbacks
	aMgr.adapterProxy.RegisterCallback(model.PostUpdate, aMgr.adapterUpdated)
	aMgr.deviceTypeProxy.RegisterCallback(model.PostUpdate, aMgr.deviceTypesUpdated)
	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusRunning)
	logger.Info("adapter-manager-started")
	return nil
}

//loadAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *AdapterManager) loadAdaptersAndDevicetypesInMemory() error {
	// Load the adapters
	adaptersIf, err := aMgr.clusterDataProxy.List(context.Background(), "/adapters", 0, false, "")
	if err != nil {
		logger.Errorw("Failed-to-list-adapters-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if adaptersIf != nil {
		for _, adapterIf := range adaptersIf.([]interface{}) {
			if adapter, ok := adapterIf.(*voltha.Adapter); ok {
				if err := aMgr.addAdapter(adapter, false); err != nil {
					logger.Errorw("failed to add adapter", log.Fields{"adapterId": adapter.Id})
				} else {
					logger.Debugw("adapter added successfully", log.Fields{"adapterId": adapter.Id})
				}
			}
		}
	} else {
		logger.Debug("no-existing-adapter-found")
		//	No adapter data.   In order to have a proxy setup for that path let's create a fake adapter
		return aMgr.addAdapter(&voltha.Adapter{Id: SentinelAdapterID}, true)
	}

	// Load the device types
	deviceTypesIf, err := aMgr.clusterDataProxy.List(context.Background(), "/device_types", 0, false, "")
	if err != nil {
		logger.Errorw("Failed-to-list-device-types-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if deviceTypesIf != nil {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, deviceTypeIf := range deviceTypesIf.([]interface{}) {
			if dType, ok := deviceTypeIf.(*voltha.DeviceType); ok {
				logger.Debugw("found-existing-device-types", log.Fields{"deviceTypes": dTypes})
				dTypes.Items = append(dTypes.Items, dType)
			} else {
				logger.Errorw("not an voltha device type", log.Fields{"interface": deviceTypeIf})
			}
		}
		return aMgr.addDeviceTypes(dTypes, false)
	}

	logger.Debug("no-existing-device-type-found")
	//	No device types data.   In order to have a proxy setup for that path let's create a fake device type
	return aMgr.addDeviceTypes(&voltha.DeviceTypes{Items: []*voltha.DeviceType{{Id: SentinelDevicetypeID, Adapter: SentinelAdapterID}}}, true)
}

func (aMgr *AdapterManager) updateLastAdapterCommunication(adapterID string, currentReplica int32, timestamp int64) {
	aMgr.lockAdaptersMap.RLock()
	key := AdapterAgentKey{adapterID, currentReplica}
	adapterAgent, have := aMgr.adapterAgents[key]
	aMgr.lockAdaptersMap.RUnlock()

	if have {
		adapterAgent.updateCommunicationTime(time.Unix(timestamp/1000, timestamp%1000*1000))
	}
}

//updateAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *AdapterManager) updateAdaptersAndDevicetypesInMemory(ctx context.Context, adapter *voltha.Adapter) {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()

	key := AdapterAgentKey{adapter.Id, adapter.CurrentReplica}
	if adapterAgent, ok := aMgr.adapterAgents[key]; ok {
		if adapterAgent.getAdapter() != nil {
			// Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
			go func() {
				err := aMgr.deviceMgr.adapterRestarted(ctx, adapter)
				if err != nil {
					log.Errorw("unable-to-restart-adapter", log.Fields{"error": err})
				}
			}()
			return
		}
	}

	// Update the adapters
	adaptersIf, err := aMgr.clusterDataProxy.List(ctx, "/adapters", 0, false, "")
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
	logger.Debugw("adding-adapter", log.Fields{"adapter": adapter})
	key := AdapterAgentKey{adapter.Id, adapter.CurrentReplica}
	if _, exist := aMgr.adapterAgents[key]; !exist {
		clonedAdapter := (proto.Clone(adapter)).(*voltha.Adapter)
		aMgr.adapterAgents[key] = newAdapterAgent(clonedAdapter, nil)
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			id := fmt.Sprintf("%s/%d", adapter.Id, adapter.CurrentReplica)
			kvAdapter, err := aMgr.clusterDataProxy.Get(context.Background(), "/adapters/"+id, 0, false, "")
			if err != nil {
				logger.Errorw("failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			}
			if kvAdapter == nil {
				added, err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/adapters", id, clonedAdapter, "")
				if err != nil {
					logger.Errorw("failed-to-save-adapter-to-cluster-proxy", log.Fields{"error": err})
					return err
				}
				if added == nil {
					//TODO:  Errors when saving to KV would require a separate go routine to be launched and try the saving again
					logger.Errorw("failed-to-save-adapter", log.Fields{"adapter": adapter})
				} else {
					logger.Debugw("adapter-saved-to-KV-Store", log.Fields{"adapter": adapter})
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
	logger.Debugw("adding-device-types", log.Fields{"deviceTypes": deviceTypes})
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	// Update all the adapter agents, if none of them found then do not
	// create a new agent.
	for _, deviceType := range deviceTypes.Items {
		//clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
		for key := range aMgr.adapterAgents {
			if key.adapterID == deviceType.Adapter {
				aMgr.adapterAgents[key].updateDeviceType(deviceType)
			}
		}
		aMgr.deviceTypeToAdapterMap[deviceType.Id] = deviceType.Adapter
	}
	if saveToDb {
		// Save the device types to the KV store as well
		for _, deviceType := range deviceTypes.Items {
			dType, err := aMgr.clusterDataProxy.Get(context.Background(), "/device_types/"+deviceType.Id, 0, false, "")
			if err != nil {
				logger.Errorw("Failed-to-device-types-from-cluster-data-proxy", log.Fields{"error": err})
				return err
			}
			if dType == nil {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				added, err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/device_types", deviceType.Id, clonedDType, "")
				if err != nil {
					logger.Errorw("Failed-to-add-device-types-to-cluster-data-proxy", log.Fields{"error": err})
					return err
				}
				if added == nil {
					logger.Errorw("failed-to-save-deviceType", log.Fields{"deviceType": deviceType})
				} else {
					logger.Debugw("device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
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

func (aMgr *AdapterManager) getAdapter(adapterID string, currentReplica int32) *voltha.Adapter {
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	key := AdapterAgentKey{adapterID, currentReplica}
	if adapterAgent, ok := aMgr.adapterAgents[key]; ok {
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
	key := AdapterAgentKey{adapter.Id, adapter.CurrentReplica}
	if adapterAgent, ok := aMgr.adapterAgents[key]; ok {
		adapterAgent.updateAdapter(adapter)
	} else {
		aMgr.adapterAgents[key] = newAdapterAgent(adapter, nil)
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
	for key := range aMgr.adapterAgents {
		aMgr.adapterAgents[key].updateDeviceType(deviceType)
	}
	// TODO Discuss do we need to create an adapter agent when there is no registered adapter
	aMgr.deviceTypeToAdapterMap[deviceType.Id] = deviceType.Adapter
}

func (aMgr *AdapterManager) registerAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) (*voltha.CoreInstance, error) {
	logger.Debugw("registerAdapter", log.Fields{"adapter": adapter, "deviceTypes": deviceTypes.Items})

	if aMgr.getAdapter(adapter.Id, adapter.CurrentReplica) != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		go func() {
			err := aMgr.deviceMgr.adapterRestarted(context.Background(), adapter)
			if err != nil {
				logger.Errorw("unable-to-restart-adapter", log.Fields{"error": err})
			}
		}()
		return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
	}
	// Save the adapter and the device types
	if err := aMgr.addAdapter(adapter, true); err != nil {
		logger.Errorw("failed-to-add-adapter", log.Fields{"error": err})
		return nil, err
	}
	if err := aMgr.addDeviceTypes(deviceTypes, true); err != nil {
		logger.Errorw("failed-to-add-device-types", log.Fields{"error": err})
		return nil, err
	}

	logger.Debugw("adapter-registered", log.Fields{"adapterId": adapter.Id, "adapterCurrentReplica": adapter.CurrentReplica})

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

	// Iterate over all AdapterAgents and put all the device
	// types to a set to create a unique list
	deviceTypesSet := make(map[*voltha.DeviceType]struct{})
	var void struct{}
	for _, adapterAgent := range aMgr.adapterAgents {
		for _, deviceType := range adapterAgent.deviceTypes {
			deviceTypesSet[deviceType] = void
		}
	}
	// Convert deviceTypeSet to a list
	deviceTypes := []*voltha.DeviceType{}
	for deviceType := range deviceTypesSet {
		deviceTypes = append(deviceTypes, deviceType)
	}
	return deviceTypes
}

// getDeviceType returns the device type proto definition given the name of the device type
func (aMgr *AdapterManager) getDeviceType(deviceType string) *voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	if adapterID, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		for _, adapterAgent := range aMgr.adapterAgents {
			if adapterAgent.adapter.Id == adapterID {
				return adapterAgent.getDeviceType(deviceType)
			}
		}
	}
	return nil
}

//adapterUpdated is a callback invoked when an adapter change has been noticed
func (aMgr *AdapterManager) adapterUpdated(ctx context.Context, args ...interface{}) interface{} {
	logger.Debugw("updateAdapter-callback", log.Fields{"argsLen": len(args)})

	var previousData *voltha.Adapters
	var latestData *voltha.Adapters

	var ok bool
	if previousData, ok = args[0].(*voltha.Adapters); !ok {
		logger.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}
	if latestData, ok = args[1].(*voltha.Adapters); !ok {
		logger.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	if previousData != nil && latestData != nil {
		if reflect.DeepEqual(previousData.Items, latestData.Items) {
			logger.Debug("update-not-required")
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
	logger.Debugw("deviceTypesUpdated-callback", log.Fields{"argsLen": len(args)})

	var previousData *voltha.DeviceTypes
	var latestData *voltha.DeviceTypes

	var ok bool
	if previousData, ok = args[0].(*voltha.DeviceTypes); !ok {
		logger.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}

	if latestData, ok = args[1].(*voltha.DeviceTypes); !ok {
		logger.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	if previousData != nil && latestData != nil {
		if reflect.DeepEqual(previousData.Items, latestData.Items) {
			logger.Debug("update-not-required")
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
