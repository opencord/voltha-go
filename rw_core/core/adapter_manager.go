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
	//deviceTypeToAdapterMap      map[string]string
	adapterAgents               map[string]*AdapterAgent
	deviceTypes                 map[string]*voltha.DeviceType
	clusterDataProxy            *model.Proxy
	deviceMgr                   *DeviceManager
	coreInstanceID              string
	exitChannel                 chan int
	lockAdaptersMap             sync.RWMutex
	lockdDeviceTypeToAdapterMap sync.RWMutex
}

func newAdapterManager(cdProxy *model.Proxy, coreInstanceID string, kafkaClient kafka.Client, deviceMgr *DeviceManager) *AdapterManager {
	aMgr := &AdapterManager{
		exitChannel:      make(chan int, 1),
		coreInstanceID:   coreInstanceID,
		clusterDataProxy: cdProxy,
		deviceTypes:      make(map[string]*voltha.DeviceType),
		adapterAgents:    make(map[string]*AdapterAgent),
		//deviceTypeToAdapterMap: make(map[string]string),
		deviceMgr: deviceMgr,
	}
	kafkaClient.SubscribeForMetadata(aMgr.updateLastAdapterCommunication)
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
	
	return nil
}

func (aMgr *AdapterManager) updateLastAdapterCommunication(adapterID string, timestamp int64) {
	aMgr.lockAdaptersMap.RLock()
	adapterAgent, have := aMgr.adapterAgents[adapterID]
	aMgr.lockAdaptersMap.RUnlock()

	if have {
		adapterAgent.updateCommunicationTime(time.Unix(timestamp/1000, timestamp%1000*1000))
	}
}

func (aMgr *AdapterManager) addAdapter(adapter *voltha.Adapter, saveToDb bool) error {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	logger.Debugw("adding-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			kvAdapter, err := aMgr.clusterDataProxy.Get(context.Background(), "/adapters/"+adapter.Id, 0, false, "")
			if err != nil {
				logger.Errorw("failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			}
			if kvAdapter == nil {
				added, err := aMgr.clusterDataProxy.AddWithID(context.Background(), "/adapters", adapter.Id, adapter, "")
				if err != nil {
					logger.Errorw("failed-to-save-adapter-to-cluster-proxy", log.Fields{"error": err})
					return err
				}
				if added == nil {
					//TODO:  Errors when saving to KV would require a separate go routine to be launched and try the saving again
					logger.Errorw("failed-to-save-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
						"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "replica": adapter.CurrentReplica, "total": adapter.TotalReplicas})
					return errors.New("failed-to-save-adapter")
				}
				logger.Debugw("adapter-saved-to-KV-Store", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
					"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "replica": adapter.CurrentReplica, "total": adapter.TotalReplicas})
			} else {
				log.Warnw("adding-adapter-already-in-KV-store", log.Fields{
					"kvAdapter":      kvAdapter,
					"adapterName":    adapter.Id,
					"adapterReplica": adapter.CurrentReplica,
				})
			}
		}
		clonedAdapter := (proto.Clone(adapter)).(*voltha.Adapter)
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(clonedAdapter, nil)
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

	// create an in memory map to fetch the entire voltha.DeviceType from a device.Type string
	for _, deviceType := range deviceTypes.Items {
		aMgr.deviceTypes[deviceType.Id] = deviceType
	}

	if saveToDb {
		// Save the device types to the KV store
		for _, deviceType := range deviceTypes.Items {
			dType, err := aMgr.clusterDataProxy.Get(context.Background(), "/device_types/"+deviceType.Id, 0, false, "")
			if err != nil {
				logger.Errorw("Failed-to--device-types-from-cluster-data-proxy", log.Fields{"error": err})
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
					return errors.New("failed-to-save-deviceType")
				}
				logger.Debugw("device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
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
			result.Items = append(result.Items, (proto.Clone(a)).(*voltha.Adapter))
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

func (aMgr *AdapterManager) registerAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) (*voltha.CoreInstance, error) {
	logger.Debugw("registerAdapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "deviceTypes": deviceTypes.Items})

	if aMgr.getAdapter(adapter.Id) != nil {
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

	logger.Debugw("adapter-registered", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})

	return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
}

// getAdapterType returns the name of the device adapter that service this device type
func (aMgr *AdapterManager) getAdapterType(deviceType string) (string, error) {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		if deviceType == adapterAgent.adapter.Type {
			return  adapterAgent.adapter.Type, nil
		}
	}
	return "", fmt.Errorf("Adapter-not-registered-for-device-type %s", deviceType)
}

func (aMgr *AdapterManager) listDeviceTypes() []*voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	deviceTypes := make([]*voltha.DeviceType, 0, len(aMgr.deviceTypes))

	for _, deviceType := range aMgr.deviceTypes {
		deviceTypes = append(deviceTypes, deviceType)
	}

	return deviceTypes
}

// getDeviceType returns the device type proto definition given the name of the device type
func (aMgr *AdapterManager) getDeviceType(deviceType string) *voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	if deviceType, exist := aMgr.deviceTypes[deviceType]; exist {
		return deviceType
	}

	return nil
}

