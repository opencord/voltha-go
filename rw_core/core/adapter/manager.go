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

package adapter

import (
	"context"
	"fmt"
	"sync"
	"time"

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

// Manager represents adapter manager attributes
type Manager struct {
	adapterAgents               map[string]*agent
	deviceTypeToAdapterMap      map[string]string
	clusterDataProxy            *model.Proxy
	onAdapterRestart            adapterRestartedHandler
	coreInstanceID              string
	exitChannel                 chan int
	lockAdaptersMap             sync.RWMutex
	lockdDeviceTypeToAdapterMap sync.RWMutex
}

func NewAdapterManager(cdProxy *model.Proxy, coreInstanceID string, kafkaClient kafka.Client) *Manager {
	aMgr := &Manager{
		exitChannel:            make(chan int, 1),
		coreInstanceID:         coreInstanceID,
		clusterDataProxy:       cdProxy,
		adapterAgents:          make(map[string]*agent),
		deviceTypeToAdapterMap: make(map[string]string),
	}
	kafkaClient.SubscribeForMetadata(aMgr.updateLastAdapterCommunication)
	return aMgr
}

// an interface type for callbacks
// if more than one callback is required, this should be converted to a proper interface
type adapterRestartedHandler func(ctx context.Context, adapter *voltha.Adapter) error

func (aMgr *Manager) SetAdapterRestartedCallback(onAdapterRestart adapterRestartedHandler) {
	aMgr.onAdapterRestart = onAdapterRestart
}

func (aMgr *Manager) Start(ctx context.Context) error {
	logger.Info("starting-adapter-manager")

	// Load the existing adapterAgents and device types - this will also ensure the correct paths have been
	// created if there are no data in the dB to start
	err := aMgr.loadAdaptersAndDevicetypesInMemory()
	if err != nil {
		logger.Errorw("Failed-to-load-adapters-and-device-types-in-memeory", log.Fields{"error": err})
		return err
	}

	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusRunning)
	logger.Info("adapter-manager-started")
	return nil
}

//loadAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *Manager) loadAdaptersAndDevicetypesInMemory() error {
	// Load the adapters
	var adapters []*voltha.Adapter
	if err := aMgr.clusterDataProxy.List(context.Background(), "/adapters", &adapters); err != nil {
		logger.Errorw("Failed-to-list-adapters-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if len(adapters) != 0 {
		for _, adapter := range adapters {
			if err := aMgr.addAdapter(adapter, false); err != nil {
				logger.Errorw("failed to add adapter", log.Fields{"adapterId": adapter.Id})
			} else {
				logger.Debugw("adapter added successfully", log.Fields{"adapterId": adapter.Id})
			}
		}
	} else {
		logger.Debug("no-existing-adapter-found")
		//	No adapter data.   In order to have a proxy setup for that path let's create a fake adapter
		return aMgr.addAdapter(&voltha.Adapter{Id: SentinelAdapterID}, true)
	}

	// Load the device types
	var deviceTypes []*voltha.DeviceType
	if err := aMgr.clusterDataProxy.List(context.Background(), "/device_types", &deviceTypes); err != nil {
		logger.Errorw("Failed-to-list-device-types-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if len(deviceTypes) != 0 {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, dType := range deviceTypes {
			logger.Debugw("found-existing-device-types", log.Fields{"deviceTypes": dTypes})
			dTypes.Items = append(dTypes.Items, dType)
		}
		return aMgr.addDeviceTypes(dTypes, false)
	}

	logger.Debug("no-existing-device-type-found")
	//	No device types data.   In order to have a proxy setup for that path let's create a fake device type
	return aMgr.addDeviceTypes(&voltha.DeviceTypes{Items: []*voltha.DeviceType{{Id: SentinelDevicetypeID, Adapter: SentinelAdapterID}}}, true)
}

func (aMgr *Manager) updateLastAdapterCommunication(adapterID string, timestamp int64) {
	aMgr.lockAdaptersMap.RLock()
	adapterAgent, have := aMgr.adapterAgents[adapterID]
	aMgr.lockAdaptersMap.RUnlock()

	if have {
		adapterAgent.updateCommunicationTime(time.Unix(timestamp/1000, timestamp%1000*1000))
	}
}

func (aMgr *Manager) addAdapter(adapter *voltha.Adapter, saveToDb bool) error {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	logger.Debugw("adding-adapter", log.Fields{"adapter": adapter})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		clonedAdapter := (proto.Clone(adapter)).(*voltha.Adapter)
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(clonedAdapter, nil)
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			if have, err := aMgr.clusterDataProxy.Get(context.Background(), "/adapters/"+adapter.Id, &voltha.Adapter{}); err != nil {
				logger.Errorw("failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				if err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/adapters", adapter.Id, clonedAdapter); err != nil {
					logger.Errorw("failed-to-save-adapter-to-cluster-proxy", log.Fields{"error": err})
					return err
				}
				aMgr.updateAdapterWithoutLock(adapter)
				logger.Debugw("adapter-saved-to-KV-Store", log.Fields{"adapter": adapter})
			}
		}
	}
	return nil
}

func (aMgr *Manager) addDeviceTypes(deviceTypes *voltha.DeviceTypes, saveToDb bool) error {
	if deviceTypes == nil {
		return fmt.Errorf("no-device-type")
	}
	logger.Debugw("adding-device-types", log.Fields{"deviceTypes": deviceTypes})
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	for _, deviceType := range deviceTypes.Items {
		clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
		if adapterAgent, exist := aMgr.adapterAgents[clonedDType.Adapter]; exist {
			adapterAgent.updateDeviceType(clonedDType)
		} else {
			logger.Debugw("adapter-not-exist", log.Fields{"deviceTypes": deviceTypes, "adapterId": clonedDType.Adapter})
			aMgr.adapterAgents[clonedDType.Adapter] = newAdapterAgent(&voltha.Adapter{Id: clonedDType.Adapter}, deviceTypes)
		}
		aMgr.deviceTypeToAdapterMap[clonedDType.Id] = clonedDType.Adapter
	}
	if saveToDb {
		// Save the device types to the KV store as well
		for _, deviceType := range deviceTypes.Items {
			if have, err := aMgr.clusterDataProxy.Get(context.Background(), "/device_types/"+deviceType.Id, &voltha.DeviceType{}); err != nil {
				logger.Errorw("Failed-to--device-types-from-cluster-data-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				if err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/device_types", deviceType.Id, clonedDType); err != nil {
					logger.Errorw("Failed-to-add-device-types-to-cluster-data-proxy", log.Fields{"error": err})
					return err
				}
				aMgr.updateDeviceTypeWithoutLock(deviceType)
				logger.Debugw("device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
			}
		}
	}
	return nil
}

func (aMgr *Manager) ListAdapters(ctx context.Context) (*voltha.Adapters, error) {
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

func (aMgr *Manager) getAdapter(adapterID string) *voltha.Adapter {
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	if adapterAgent, ok := aMgr.adapterAgents[adapterID]; ok {
		return adapterAgent.getAdapter()
	}
	return nil
}

func (aMgr *Manager) updateAdapterWithoutLock(adapter *voltha.Adapter) {
	if adapterAgent, ok := aMgr.adapterAgents[adapter.Id]; ok {
		adapterAgent.updateAdapter(adapter)
	} else {
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(adapter, nil)
	}
}

func (aMgr *Manager) updateDeviceTypeWithoutLock(deviceType *voltha.DeviceType) {
	if adapterAgent, exist := aMgr.adapterAgents[deviceType.Adapter]; exist {
		adapterAgent.updateDeviceType(deviceType)
	} else {
		aMgr.adapterAgents[deviceType.Adapter] = newAdapterAgent(&voltha.Adapter{Id: deviceType.Adapter},
			&voltha.DeviceTypes{Items: []*voltha.DeviceType{deviceType}})
	}
	aMgr.deviceTypeToAdapterMap[deviceType.Id] = deviceType.Adapter
}

func (aMgr *Manager) RegisterAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) (*voltha.CoreInstance, error) {
	logger.Debugw("RegisterAdapter", log.Fields{"adapter": adapter, "deviceTypes": deviceTypes.Items})

	if aMgr.getAdapter(adapter.Id) != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		go func() {
			err := aMgr.onAdapterRestart(context.Background(), adapter)
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

	logger.Debugw("adapter-registered", log.Fields{"adapter": adapter.Id})

	return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
}

//GetAdapterName returns the name of the device adapter that service this device type
func (aMgr *Manager) GetAdapterName(deviceType string) (string, error) {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	if adapterID, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		return adapterID, nil
	}
	return "", fmt.Errorf("Adapter-not-registered-for-device-type %s", deviceType)
}

func (aMgr *Manager) ListDeviceTypes() []*voltha.DeviceType {
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
func (aMgr *Manager) GetDeviceType(deviceType string) *voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	if adapterID, exist := aMgr.deviceTypeToAdapterMap[deviceType]; exist {
		if adapterAgent := aMgr.adapterAgents[adapterID]; adapterAgent != nil {
			return adapterAgent.getDeviceType(deviceType)
		}
	}
	return nil
}
