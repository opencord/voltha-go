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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

// Manager represents adapter manager attributes
type Manager struct {
	adapterAgents               map[string]*agent
	deviceTypes                 map[string]*voltha.DeviceType
	clusterDataProxy            *model.Proxy
	onAdapterRestart            adapterRestartedHandler
	coreInstanceID              string
	exitChannel                 chan int
	lockAdaptersMap             sync.RWMutex
	lockdDeviceTypeToAdapterMap sync.RWMutex
}

func NewAdapterManager(cdProxy *model.Proxy, coreInstanceID string, kafkaClient kafka.Client) *Manager {
	aMgr := &Manager{
		exitChannel:      make(chan int, 1),
		coreInstanceID:   coreInstanceID,
		clusterDataProxy: cdProxy,
		deviceTypes:      make(map[string]*voltha.DeviceType),
		adapterAgents:    make(map[string]*agent),
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
	if err := aMgr.clusterDataProxy.List(context.Background(), "adapters", &adapters); err != nil {
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
	}

	// Load the device types
	var deviceTypes []*voltha.DeviceType
	if err := aMgr.clusterDataProxy.List(context.Background(), "device_types", &deviceTypes); err != nil {
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

	return nil
}

func (aMgr *Manager) updateLastAdapterCommunication(adapterID string, timestamp time.Time) {
	aMgr.lockAdaptersMap.RLock()
	adapterAgent, have := aMgr.adapterAgents[adapterID]
	aMgr.lockAdaptersMap.RUnlock()

	if have {
		adapterAgent.updateCommunicationTime(timestamp)
	}
}

func (aMgr *Manager) addAdapter(adapter *voltha.Adapter, saveToDb bool) error {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	logger.Debugw("adding-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			if have, err := aMgr.clusterDataProxy.Get(context.Background(), "adapters/"+adapter.Id, &voltha.Adapter{}); err != nil {
				logger.Errorw("failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				if err := aMgr.clusterDataProxy.AddWithID(context.Background(), "adapters", adapter.Id, adapter); err != nil {
					logger.Errorw("failed-to-save-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
						"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "replica": adapter.CurrentReplica, "total": adapter.TotalReplicas})
					return err
				}
				logger.Debugw("adapter-saved-to-KV-Store", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
					"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "replica": adapter.CurrentReplica, "total": adapter.TotalReplicas})
			} else {
				log.Warnw("adding-adapter-already-in-KV-store", log.Fields{
					"adapterName":    adapter.Id,
					"adapterReplica": adapter.CurrentReplica,
				})
			}
		}
		clonedAdapter := (proto.Clone(adapter)).(*voltha.Adapter)
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(clonedAdapter)
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

	// create an in memory map to fetch the entire voltha.DeviceType from a device.Type string
	for _, deviceType := range deviceTypes.Items {
		aMgr.deviceTypes[deviceType.Id] = deviceType
	}

	if saveToDb {
		// Save the device types to the KV store
		for _, deviceType := range deviceTypes.Items {
			if have, err := aMgr.clusterDataProxy.Get(context.Background(), "device_types/"+deviceType.Id, &voltha.DeviceType{}); err != nil {
				logger.Errorw("Failed-to--device-types-from-cluster-data-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				if err := aMgr.clusterDataProxy.AddWithID(context.Background(), "device_types", deviceType.Id, clonedDType); err != nil {
					logger.Errorw("Failed-to-add-device-types-to-cluster-data-proxy", log.Fields{"error": err})
					return err
				}
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
			result.Items = append(result.Items, (proto.Clone(a)).(*voltha.Adapter))
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

func (aMgr *Manager) RegisterAdapter(adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) (*voltha.CoreInstance, error) {
	logger.Debugw("RegisterAdapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "deviceTypes": deviceTypes.Items})

	if adapter.Type == "" {
		log.Errorw("adapter-not-specifying-type", log.Fields{
			"adapterId": adapter.Id,
			"vendor":    adapter.Vendor,
			"type":      adapter.Type,
		})
		return nil, status.Error(codes.InvalidArgument, "adapter-not-specifying-type")
	}

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

	logger.Debugw("adapter-registered", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})

	return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
}

// GetAdapterType returns the name of the device adapter that service this device type
func (aMgr *Manager) GetAdapterType(deviceType string) (string, error) {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		if deviceType == adapterAgent.adapter.Type {
			return adapterAgent.adapter.Type, nil
		}
	}
	return "", fmt.Errorf("Adapter-not-registered-for-device-type %s", deviceType)
}

func (aMgr *Manager) ListDeviceTypes() []*voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	deviceTypes := make([]*voltha.DeviceType, 0, len(aMgr.deviceTypes))

	for _, deviceType := range aMgr.deviceTypes {
		deviceTypes = append(deviceTypes, deviceType)
	}

	return deviceTypes
}

// getDeviceType returns the device type proto definition given the name of the device type
func (aMgr *Manager) GetDeviceType(deviceType string) *voltha.DeviceType {
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	if deviceType, exist := aMgr.deviceTypes[deviceType]; exist {
		return deviceType
	}

	return nil
}
