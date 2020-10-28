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

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-lib-go/v4/pkg/probe"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Manager represents adapter manager attributes
type Manager struct {
	adapterAgents               map[string]*agent
	deviceTypes                 map[string]*voltha.DeviceType
	adapterProxy                *model.Proxy
	deviceTypeProxy             *model.Proxy
	onAdapterRestart            adapterRestartedHandler
	coreInstanceID              string
	lockAdaptersMap             sync.RWMutex
	lockdDeviceTypeToAdapterMap sync.RWMutex
}

func NewAdapterManager(ctx context.Context, dbPath *model.Path, coreInstanceID string, kafkaClient kafka.Client) *Manager {
	aMgr := &Manager{
		coreInstanceID:  coreInstanceID,
		adapterProxy:    dbPath.Proxy("adapters"),
		deviceTypeProxy: dbPath.Proxy("device_types"),
		deviceTypes:     make(map[string]*voltha.DeviceType),
		adapterAgents:   make(map[string]*agent),
	}
	kafkaClient.SubscribeForMetadata(ctx, aMgr.updateLastAdapterCommunication)
	return aMgr
}

// an interface type for callbacks
// if more than one callback is required, this should be converted to a proper interface
type adapterRestartedHandler func(ctx context.Context, adapter *voltha.Adapter) error

func (aMgr *Manager) SetAdapterRestartedCallback(onAdapterRestart adapterRestartedHandler) {
	aMgr.onAdapterRestart = onAdapterRestart
}

func (aMgr *Manager) Start(ctx context.Context) {
	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusPreparing)
	logger.Info(ctx, "starting-adapter-manager")

	// Load the existing adapterAgents and device types - this will also ensure the correct paths have been
	// created if there are no data in the dB to start
	err := aMgr.loadAdaptersAndDevicetypesInMemory(ctx)
	if err != nil {
		logger.Fatalf(ctx, "failed-to-load-adapters-and-device-types-in-memory: %s", err)
	}

	probe.UpdateStatusFromContext(ctx, "adapter-manager", probe.ServiceStatusRunning)
	logger.Info(ctx, "adapter-manager-started")
}

//loadAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *Manager) loadAdaptersAndDevicetypesInMemory(ctx context.Context) error {
	// Load the adapters
	var adapters []*voltha.Adapter
	if err := aMgr.adapterProxy.List(log.WithSpanFromContext(context.Background(), ctx), &adapters); err != nil {
		logger.Errorw(ctx, "Failed-to-list-adapters-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if len(adapters) != 0 {
		for _, adapter := range adapters {
			if err := aMgr.addAdapter(ctx, adapter, false); err != nil {
				logger.Errorw(ctx, "failed to add adapter", log.Fields{"adapterId": adapter.Id})
			} else {
				logger.Debugw(ctx, "adapter added successfully", log.Fields{"adapterId": adapter.Id})
			}
		}
	}

	// Load the device types
	var deviceTypes []*voltha.DeviceType
	if err := aMgr.deviceTypeProxy.List(log.WithSpanFromContext(context.Background(), ctx), &deviceTypes); err != nil {
		logger.Errorw(ctx, "Failed-to-list-device-types-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}
	if len(deviceTypes) != 0 {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, dType := range deviceTypes {
			logger.Debugw(ctx, "found-existing-device-types", log.Fields{"deviceTypes": dTypes})
			dTypes.Items = append(dTypes.Items, dType)
		}
		return aMgr.addDeviceTypes(ctx, dTypes, false)
	}

	logger.Debug(ctx, "no-existing-device-type-found")

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

func (aMgr *Manager) addAdapter(ctx context.Context, adapter *voltha.Adapter, saveToDb bool) error {
	aMgr.lockAdaptersMap.Lock()
	defer aMgr.lockAdaptersMap.Unlock()
	logger.Debugw(ctx, "adding-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			if have, err := aMgr.adapterProxy.Get(log.WithSpanFromContext(context.Background(), ctx), adapter.Id, &voltha.Adapter{}); err != nil {
				logger.Errorw(ctx, "failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				if err := aMgr.adapterProxy.Set(log.WithSpanFromContext(context.Background(), ctx), adapter.Id, adapter); err != nil {
					logger.Errorw(ctx, "failed-to-save-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
						"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "replica": adapter.CurrentReplica, "total": adapter.TotalReplicas})
					return err
				}
				logger.Debugw(ctx, "adapter-saved-to-KV-Store", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
					"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "replica": adapter.CurrentReplica, "total": adapter.TotalReplicas})
			} else {
				logger.Warnw(ctx, "adding-adapter-already-in-KV-store", log.Fields{
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

func (aMgr *Manager) addDeviceTypes(ctx context.Context, deviceTypes *voltha.DeviceTypes, saveToDb bool) error {
	if deviceTypes == nil {
		return fmt.Errorf("no-device-type")
	}
	logger.Debugw(ctx, "adding-device-types", log.Fields{"deviceTypes": deviceTypes})
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
			if have, err := aMgr.deviceTypeProxy.Get(log.WithSpanFromContext(context.Background(), ctx), deviceType.Id, &voltha.DeviceType{}); err != nil {
				logger.Errorw(ctx, "Failed-to--device-types-from-cluster-data-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				if err := aMgr.deviceTypeProxy.Set(log.WithSpanFromContext(context.Background(), ctx), deviceType.Id, clonedDType); err != nil {
					logger.Errorw(ctx, "Failed-to-add-device-types-to-cluster-data-proxy", log.Fields{"error": err})
					return err
				}
				logger.Debugw(ctx, "device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
			}
		}
	}

	return nil
}

// ListAdapters returns the contents of all adapters known to the system
func (aMgr *Manager) ListAdapters(ctx context.Context, _ *empty.Empty) (*voltha.Adapters, error) {
	result := &voltha.Adapters{Items: []*voltha.Adapter{}}
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		if a := adapterAgent.getAdapter(ctx); a != nil {
			result.Items = append(result.Items, (proto.Clone(a)).(*voltha.Adapter))
		}
	}
	return result, nil
}

func (aMgr *Manager) getAdapter(ctx context.Context, adapterID string) *voltha.Adapter {
	aMgr.lockAdaptersMap.RLock()
	defer aMgr.lockAdaptersMap.RUnlock()
	if adapterAgent, ok := aMgr.adapterAgents[adapterID]; ok {
		return adapterAgent.getAdapter(ctx)
	}
	return nil
}

func (aMgr *Manager) RegisterAdapter(ctx context.Context, adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) (*voltha.CoreInstance, error) {
	logger.Debugw(ctx, "RegisterAdapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint, "deviceTypes": deviceTypes.Items})

	if adapter.Type == "" {
		logger.Errorw(ctx, "adapter-not-specifying-type", log.Fields{
			"adapterId": adapter.Id,
			"vendor":    adapter.Vendor,
			"type":      adapter.Type,
		})
		return nil, status.Error(codes.InvalidArgument, "adapter-not-specifying-type")
	}

	if aMgr.getAdapter(ctx, adapter.Id) != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		go func() {
			err := aMgr.onAdapterRestart(log.WithSpanFromContext(context.Background(), ctx), adapter)
			if err != nil {
				logger.Errorw(ctx, "unable-to-restart-adapter", log.Fields{"error": err})
			}
		}()
		return &voltha.CoreInstance{InstanceId: aMgr.coreInstanceID}, nil
	}
	// Save the adapter and the device types
	if err := aMgr.addAdapter(ctx, adapter, true); err != nil {
		logger.Errorw(ctx, "failed-to-add-adapter", log.Fields{"error": err})
		return nil, err
	}
	if err := aMgr.addDeviceTypes(ctx, deviceTypes, true); err != nil {
		logger.Errorw(ctx, "failed-to-add-device-types", log.Fields{"error": err})
		return nil, err
	}

	logger.Debugw(ctx, "adapter-registered", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
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
	return "", fmt.Errorf("adapter-not-registered-for-device-type %s", deviceType)
}

// ListDeviceTypes returns all the device types known to the system
func (aMgr *Manager) ListDeviceTypes(ctx context.Context, _ *empty.Empty) (*voltha.DeviceTypes, error) {
	logger.Debug(ctx, "ListDeviceTypes")
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	deviceTypes := make([]*voltha.DeviceType, 0, len(aMgr.deviceTypes))
	for _, deviceType := range aMgr.deviceTypes {
		deviceTypes = append(deviceTypes, deviceType)
	}
	return &voltha.DeviceTypes{Items: deviceTypes}, nil
}

// GetDeviceType returns the device type proto definition given the name of the device type
func (aMgr *Manager) GetDeviceType(ctx context.Context, deviceType *voltha.ID) (*voltha.DeviceType, error) {
	logger.Debugw(ctx, "GetDeviceType", log.Fields{"typeid": deviceType.Id})
	aMgr.lockdDeviceTypeToAdapterMap.Lock()
	defer aMgr.lockdDeviceTypeToAdapterMap.Unlock()

	dType, exist := aMgr.deviceTypes[deviceType.Id]
	if !exist {
		return nil, status.Errorf(codes.NotFound, "device_type-%s", deviceType.Id)
	}
	return dType, nil
}
