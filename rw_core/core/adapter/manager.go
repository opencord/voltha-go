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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/core_adapter"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Manager represents adapter manager attributes
type Manager struct {
	adapterAgents           map[string]*agent
	adapterEndpoints        map[Endpoint]*agent
	deviceTypes             map[string]*voltha.DeviceType
	adapterDbProxy          *model.Proxy
	deviceTypeDbProxy       *model.Proxy
	onAdapterRestart        vgrpc.RestartedHandler
	endpointMgr             EndpointManager
	lockAdapterAgentsMap    sync.RWMutex
	lockDeviceTypesMap      sync.RWMutex
	lockAdapterEndPointsMap sync.RWMutex
	liveProbeInterval       time.Duration
	coreEndpoint            string
}

// SetAdapterRestartedCallback is used to set the callback that needs to be invoked on an adapter restart
func (aMgr *Manager) SetAdapterRestartedCallback(onAdapterRestart vgrpc.RestartedHandler) {
	aMgr.onAdapterRestart = onAdapterRestart
}

func NewAdapterManager(
	coreEndpoint string,
	dbPath *model.Path,
	coreInstanceID string,
	backend *db.Backend,
	liveProbeInterval time.Duration,
) *Manager {
	return &Manager{
		adapterDbProxy:    dbPath.Proxy("adapters"),
		deviceTypeDbProxy: dbPath.Proxy("device_types"),
		deviceTypes:       make(map[string]*voltha.DeviceType),
		adapterAgents:     make(map[string]*agent),
		adapterEndpoints:  make(map[Endpoint]*agent),
		endpointMgr:       NewEndpointManager(backend),
		liveProbeInterval: liveProbeInterval,
		coreEndpoint:      coreEndpoint,
	}
}

func (aMgr *Manager) Start(ctx context.Context, serviceName string) {
	probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusPreparing)
	logger.Infow(ctx, "starting-service", log.Fields{"service": serviceName})

	// Load the existing adapterAgents and device types - this will also ensure the correct paths have been
	// created if there are no data in the dB to start
	err := aMgr.loadAdaptersAndDevicetypesInMemory(ctx)
	if err != nil {
		logger.Fatalw(ctx, "failed-to-load-adapters-and-device-types-in-memory", log.Fields{"service": serviceName, "error": err})
	}

	probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusRunning)
	logger.Infow(ctx, "service-started", log.Fields{"service": serviceName})
}

func (aMgr *Manager) Stop(ctx context.Context) {
	//	Stop all adapters
	aMgr.lockAdapterAgentsMap.RLock()
	defer aMgr.lockAdapterAgentsMap.RUnlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		adapterAgent.stop(ctx)
	}
}

func (aMgr *Manager) GetAdapterEndpoint(ctx context.Context, deviceID string, deviceType string) (string, error) {
	endPoint, err := aMgr.endpointMgr.GetEndpoint(ctx, deviceID, deviceType)
	if err != nil {
		return "", err
	}
	return string(endPoint), nil
}

func (aMgr *Manager) GetAdapterWithEndpoint(ctx context.Context, endPoint string) (*voltha.Adapter, error) {
	aMgr.lockAdapterEndPointsMap.RLock()
	agent, have := aMgr.adapterEndpoints[Endpoint(endPoint)]
	aMgr.lockAdapterEndPointsMap.RUnlock()

	if have {
		return agent.getAdapter(ctx), nil
	}

	return nil, errors.New("Not found")
}

func (aMgr *Manager) GetAdapterNameWithEndpoint(ctx context.Context, endPoint string) (string, error) {
	aMgr.lockAdapterEndPointsMap.RLock()
	agent, have := aMgr.adapterEndpoints[Endpoint(endPoint)]
	aMgr.lockAdapterEndPointsMap.RUnlock()

	if have {
		return agent.adapter.Id, nil
	}

	return "", errors.New("Not found")
}

func (aMgr *Manager) GetAdapterClient(_ context.Context, endpoint string) (adapter_service.AdapterServiceClient, error) {
	if endpoint == "" {
		return nil, errors.New("endpoint-cannot-be-empty")
	}
	aMgr.lockAdapterEndPointsMap.RLock()
	defer aMgr.lockAdapterEndPointsMap.RUnlock()

	if agent, have := aMgr.adapterEndpoints[Endpoint(endpoint)]; have {
		return agent.getClient()
	}

	return nil, fmt.Errorf("Endpoint-not-found-%s", endpoint)
}

func (aMgr *Manager) addAdapter(ctx context.Context, adapter *voltha.Adapter, saveToDb bool) error {
	aMgr.lockAdapterAgentsMap.Lock()
	aMgr.lockAdapterEndPointsMap.Lock()
	defer aMgr.lockAdapterEndPointsMap.Unlock()
	defer aMgr.lockAdapterAgentsMap.Unlock()
	logger.Debugw(ctx, "adding-adapter", log.Fields{"adapterId": adapter.Id, "vendor": adapter.Vendor,
		"currentReplica": adapter.CurrentReplica, "totalReplicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})
	if _, exist := aMgr.adapterAgents[adapter.Id]; !exist {
		if saveToDb {
			// Save the adapter to the KV store - first check if it already exist
			if have, err := aMgr.adapterDbProxy.Get(log.WithSpanFromContext(context.Background(), ctx), adapter.Id, &voltha.Adapter{}); err != nil {
				logger.Errorw(ctx, "failed-to-get-adapters-from-cluster-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				if err := aMgr.adapterDbProxy.Set(log.WithSpanFromContext(context.Background(), ctx), adapter.Id, adapter); err != nil {
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
		// Use a muted adapter restart handler which is invoked by the corresponding gRPC client on an adapter restart.
		// This handler just log the restart event.  The actual action taken following an adapter restart
		// will be done when an adapter re-registers itself.
		aMgr.adapterAgents[adapter.Id] = newAdapterAgent(aMgr.coreEndpoint, clonedAdapter, aMgr.mutedAdapterRestartedHandler, aMgr.liveProbeInterval)
		aMgr.adapterEndpoints[Endpoint(adapter.Endpoint)] = aMgr.adapterAgents[adapter.Id]
	}
	return nil
}

func (aMgr *Manager) addDeviceTypes(ctx context.Context, deviceTypes *voltha.DeviceTypes, saveToDb bool) error {
	if deviceTypes == nil {
		return fmt.Errorf("no-device-type")
	}
	logger.Debugw(ctx, "adding-device-types", log.Fields{"deviceTypes": deviceTypes})
	aMgr.lockAdapterAgentsMap.Lock()
	defer aMgr.lockAdapterAgentsMap.Unlock()
	aMgr.lockDeviceTypesMap.Lock()
	defer aMgr.lockDeviceTypesMap.Unlock()

	// create an in memory map to fetch the entire voltha.DeviceType from a device.Type string
	for _, deviceType := range deviceTypes.Items {
		aMgr.deviceTypes[deviceType.Id] = deviceType
	}

	if saveToDb {
		// Save the device types to the KV store
		for _, deviceType := range deviceTypes.Items {
			if have, err := aMgr.deviceTypeDbProxy.Get(log.WithSpanFromContext(context.Background(), ctx), deviceType.Id, &voltha.DeviceType{}); err != nil {
				logger.Errorw(ctx, "Failed-to--device-types-from-cluster-data-proxy", log.Fields{"error": err})
				return err
			} else if !have {
				//	Does not exist - save it
				clonedDType := (proto.Clone(deviceType)).(*voltha.DeviceType)
				if err := aMgr.deviceTypeDbProxy.Set(log.WithSpanFromContext(context.Background(), ctx), deviceType.Id, clonedDType); err != nil {
					logger.Errorw(ctx, "Failed-to-add-device-types-to-cluster-data-proxy", log.Fields{"error": err})
					return err
				}
				logger.Debugw(ctx, "device-type-saved-to-KV-Store", log.Fields{"deviceType": deviceType})
			}
		}
	}
	return nil
}

//loadAdaptersAndDevicetypesInMemory loads the existing set of adapters and device types in memory
func (aMgr *Manager) loadAdaptersAndDevicetypesInMemory(ctx context.Context) error {
	// Load the adapters
	var adapters []*voltha.Adapter
	if err := aMgr.adapterDbProxy.List(log.WithSpanFromContext(context.Background(), ctx), &adapters); err != nil {
		logger.Errorw(ctx, "Failed-to-list-adapters-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}

	logger.Debugw(ctx, "retrieved-adapters", log.Fields{"count": len(adapters)})

	if len(adapters) != 0 {
		for _, adapter := range adapters {
			if err := aMgr.addAdapter(ctx, adapter, false); err != nil {
				logger.Errorw(ctx, "failed-to-add-adapter", log.Fields{"adapterId": adapter.Id})
			} else {
				logger.Debugw(ctx, "adapter-added-successfully", log.Fields{"adapterId": adapter.Id})
			}
		}
	}

	// Load the device types
	var deviceTypes []*voltha.DeviceType
	if err := aMgr.deviceTypeDbProxy.List(log.WithSpanFromContext(context.Background(), ctx), &deviceTypes); err != nil {
		logger.Errorw(ctx, "Failed-to-list-device-types-from-cluster-data-proxy", log.Fields{"error": err})
		return err
	}

	logger.Debugw(ctx, "retrieved-devicetypes", log.Fields{"count": len(deviceTypes)})

	if len(deviceTypes) != 0 {
		dTypes := &voltha.DeviceTypes{Items: []*voltha.DeviceType{}}
		for _, dType := range deviceTypes {
			logger.Debugw(ctx, "found-existing-device-types", log.Fields{"deviceTypes": deviceTypes})
			dTypes.Items = append(dTypes.Items, dType)
		}
		if err := aMgr.addDeviceTypes(ctx, dTypes, false); err != nil {
			logger.Errorw(ctx, "failed-to-add-device-type", log.Fields{"deviceTypes": deviceTypes})
		} else {
			logger.Debugw(ctx, "device-type-added-successfully", log.Fields{"deviceTypes": deviceTypes})
		}
	}

	// Start the adapter agents - this will trigger the connection to the adapter
	aMgr.lockAdapterAgentsMap.RLock()
	defer aMgr.lockAdapterAgentsMap.RUnlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		subCtx := log.WithSpanFromContext(context.Background(), ctx)
		if err := adapterAgent.start(subCtx); err != nil {
			logger.Errorw(ctx, "failed-to-start-adapter", log.Fields{"adapter-endpoint": adapterAgent.adapterAPIEndPoint})
		}
	}

	logger.Debug(ctx, "no-existing-device-type-found")

	return nil
}

func (aMgr *Manager) RegisterAdapter(ctx context.Context, registration *core_adapter.AdapterRegistration) (*empty.Empty, error) {
	adapter := registration.Adapter
	deviceTypes := registration.DTypes
	logger.Infow(ctx, "RegisterAdapter", log.Fields{"adapter": adapter, "deviceTypes": deviceTypes.Items})

	if adapter.Type == "" {
		logger.Errorw(ctx, "adapter-not-specifying-type", log.Fields{
			"adapterId": adapter.Id,
			"vendor":    adapter.Vendor,
			"type":      adapter.Type,
		})
		return nil, status.Error(codes.InvalidArgument, "adapter-not-specifying-type")
	}

	if adpt, _ := aMgr.getAdapter(ctx, adapter.Id); adpt != nil {
		//	Already registered - Adapter may have restarted.  Trigger the reconcile process for that adapter
		logger.Warnw(ctx, "adapter-restarted", log.Fields{"adapter": adpt.Id, "endpoint": adpt.Endpoint})

		// First reset the adapter connection
		agt, err := aMgr.getAgent(ctx, adpt.Id)
		if err != nil {
			logger.Errorw(ctx, "no-adapter-agent", log.Fields{"error": err})
			return nil, err
		}
		agt.resetConnection(ctx)

		go func() {
			err := aMgr.onAdapterRestart(log.WithSpanFromContext(context.Background(), ctx), adpt.Endpoint)
			if err != nil {
				logger.Errorw(ctx, "unable-to-restart-adapter", log.Fields{"error": err})
			}
		}()
		return &empty.Empty{}, nil
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

	// Setup the endpoints for this adapter
	if err := aMgr.endpointMgr.RegisterAdapter(ctx, adapter, deviceTypes); err != nil {
		logger.Errorw(ctx, "failed-to-register-adapter", log.Fields{"error": err})
	}

	// Start adapter instance - this will trigger the connection to the adapter
	if agent, err := aMgr.getAgent(ctx, adapter.Id); agent != nil {
		subCtx := log.WithSpanFromContext(context.Background(), ctx)
		if err := agent.start(subCtx); err != nil {
			logger.Errorw(ctx, "failed-to-start-adapter", log.Fields{"error": err})
			return nil, err
		}
	} else {
		logger.Fatalw(ctx, "adapter-absent", log.Fields{"error": err, "adapter": adapter.Id})
	}

	return &empty.Empty{}, nil
}

func (aMgr *Manager) GetAdapterTypeByVendorID(vendorID string) (string, error) {
	aMgr.lockDeviceTypesMap.RLock()
	defer aMgr.lockDeviceTypesMap.RUnlock()
	for _, dType := range aMgr.deviceTypes {
		for _, v := range dType.VendorIds {
			if v == vendorID {
				return dType.AdapterType, nil
			}
		}
	}
	return "", fmt.Errorf("vendor id %s not found", vendorID)
}

// GetAdapterType returns the name of the device adapter that services this device type
func (aMgr *Manager) GetAdapterType(deviceType string) (string, error) {
	aMgr.lockDeviceTypesMap.Lock()
	defer aMgr.lockDeviceTypesMap.Unlock()
	for _, dt := range aMgr.deviceTypes {
		if deviceType == dt.Id {
			return dt.AdapterType, nil
		}
	}
	return "", fmt.Errorf("adapter-not-registered-for-device-type %s", deviceType)
}

// ListDeviceTypes returns all the device types known to the system
func (aMgr *Manager) ListDeviceTypes(ctx context.Context, _ *empty.Empty) (*voltha.DeviceTypes, error) {
	logger.Debug(ctx, "ListDeviceTypes")
	aMgr.lockDeviceTypesMap.Lock()
	defer aMgr.lockDeviceTypesMap.Unlock()

	deviceTypes := make([]*voltha.DeviceType, 0, len(aMgr.deviceTypes))
	for _, deviceType := range aMgr.deviceTypes {
		deviceTypes = append(deviceTypes, deviceType)
	}
	return &voltha.DeviceTypes{Items: deviceTypes}, nil
}

// GetDeviceType returns the device type proto definition given the name of the device type
func (aMgr *Manager) GetDeviceType(ctx context.Context, deviceType *common.ID) (*voltha.DeviceType, error) {
	logger.Debugw(ctx, "GetDeviceType", log.Fields{"typeid": deviceType.Id})
	aMgr.lockDeviceTypesMap.Lock()
	defer aMgr.lockDeviceTypesMap.Unlock()

	dType, exist := aMgr.deviceTypes[deviceType.Id]
	if !exist {
		return nil, status.Errorf(codes.NotFound, "device_type-%s", deviceType.Id)
	}
	return dType, nil
}

// ListAdapters returns the contents of all adapters known to the system
func (aMgr *Manager) ListAdapters(ctx context.Context, _ *empty.Empty) (*voltha.Adapters, error) {
	logger.Debug(ctx, "Listing adapters")
	result := &voltha.Adapters{Items: []*voltha.Adapter{}}
	aMgr.lockAdapterAgentsMap.RLock()
	defer aMgr.lockAdapterAgentsMap.RUnlock()
	for _, adapterAgent := range aMgr.adapterAgents {
		if a := adapterAgent.getAdapter(ctx); a != nil {
			result.Items = append(result.Items, (proto.Clone(a)).(*voltha.Adapter))
		}
	}
	logger.Debugw(ctx, "Listing adapters", log.Fields{"result": result})
	return result, nil
}

func (aMgr *Manager) getAgent(ctx context.Context, adapterID string) (*agent, error) {
	aMgr.lockAdapterAgentsMap.RLock()
	defer aMgr.lockAdapterAgentsMap.RUnlock()
	if adapterAgent, ok := aMgr.adapterAgents[adapterID]; ok {
		return adapterAgent, nil
	}
	return nil, errors.New("Not found")
}

func (aMgr *Manager) getAdapter(ctx context.Context, adapterID string) (*voltha.Adapter, error) {
	aMgr.lockAdapterAgentsMap.RLock()
	defer aMgr.lockAdapterAgentsMap.RUnlock()
	if adapterAgent, ok := aMgr.adapterAgents[adapterID]; ok {
		return adapterAgent.getAdapter(ctx), nil
	}
	return nil, errors.New("Not found")
}

// mutedAdapterRestartedHandler will be invoked by the grpc client on an adapter restart.
// Since the Adapter will re-register itself and that will trigger the reconcile process,
// therefore this handler does nothing, other than logging the event.
func (aMgr *Manager) mutedAdapterRestartedHandler(ctx context.Context, endpoint string) error {
	logger.Infow(ctx, "muted-adapter-restarted", log.Fields{"endpoint": endpoint})
	return nil
}

func (aMgr *Manager) WaitUntilConnectionsToAdaptersAreUp(ctx context.Context, connectionRetryInterval time.Duration) error {
	logger.Infow(ctx, "waiting-for-adapters-to-be-up", log.Fields{"retry-interval": connectionRetryInterval})
	for {
		aMgr.lockAdapterAgentsMap.Lock()
		numAdapters := len(aMgr.adapterAgents)
		if numAdapters == 0 {
			// No adapter registered yet
			aMgr.lockAdapterAgentsMap.Unlock()
			logger.Info(ctx, "no-adapter-registered")
			return nil
		}
		// A case of Core restart
		agentsUp := true
	adapterloop:
		for _, agt := range aMgr.adapterAgents {
			agentsUp = agentsUp && agt.IsConnectionUp()
			if !agentsUp {
				break adapterloop
			}
		}
		aMgr.lockAdapterAgentsMap.Unlock()
		if agentsUp {
			logger.Infow(ctx, "adapter-connections-ready", log.Fields{"adapter-count": numAdapters})
			return nil
		}
		logger.Warnw(ctx, "adapter-connections-not-ready", log.Fields{"adapter-count": numAdapters})
		select {
		case <-time.After(connectionRetryInterval):
			logger.Infow(ctx, "retrying-adapter-connections", log.Fields{"adapter-count": numAdapters})
			continue
		case <-ctx.Done():
			logger.Errorw(ctx, "context-timeout", log.Fields{"adapter-count": numAdapters, "err": ctx.Err()})
			return ctx.Err()
		}
	}
}
