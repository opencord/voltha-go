/*Copyright 2019-present Open Networking Foundation

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
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
)

const (
	// KvstoreTimeout specifies the time out for KV Store Connection
	KvstoreTimeout = 5
	BasePathKvStore = "service/voltha/openolt"
	// FilterPath - <device-id>/event_filters/filter-type-<type-of-filter>
	FilterPath = "{%s}/event_filters/filter-type-%s"
)

type EventFilterAgent struct {
	filters   map[string]map[string]*voltha.EventFilter
	KVStore   *db.Backend
	deviceMgr *DeviceManager
}

func NewEventFilterAgent(host string, port int, storeType string, deviceMgr *DeviceManager) *EventFilterAgent {
	var evenFilterAgent EventFilterAgent
	var _ error
	evenFilterAgent.deviceMgr = deviceMgr
	filterStore := evenFilterAgent.reconcileFilters()
	evenFilterAgent.filters = filterStore
	evenFilterAgent.KVStore, _ = InitKVClient(host, port, storeType)
	return &evenFilterAgent
}

func InitKVClient(host string, port int, storeType string) (*db.Backend, error) {
	var kvClient kvstore.Client
	var err error
	addr := host + ":" + strconv.Itoa(port)
	switch storeType {
	case "consul":
		if kvClient, err = kvstore.NewConsulClient(addr, KvstoreTimeout); err != nil {
			log.Errorw("failed-to-create-kv-client", log.Fields{"error": err})
			return nil, err
		}
	case "etcd":
		if kvClient, err = kvstore.NewEtcdClient(addr, KvstoreTimeout); err != nil {
			log.Errorw("failed-to-create-kv-client", log.Fields{"error": err})
			return nil, err
		}
	default:
		log.Error("unsupported-kv-store")
		return nil, errors.New("unsupported-kv-store")
	}

	kvbackend := &db.Backend{
		Client:     kvClient,
		StoreType:  storeType,
		Host:       host,
		Port:       port,
		Timeout:    KvstoreTimeout,
		PathPrefix: BasePathKvStore}

	return kvbackend, nil
}

func (efa *EventFilterAgent) reconcileFilters() map[string]map[string]*voltha.EventFilter {
	// TODO: Code for reconciliation of event filters
	filterStore := make(map[string]map[string]*voltha.EventFilter)
	return filterStore
}

func (efa *EventFilterAgent) CreateEventFilter(filter *voltha.EventFilter) error {
	if filter.DeviceId == "" || filter.EventType == "" {
		log.Errorw("device-id-or-event-type-is-missing", log.Fields{"filter": filter})
		return errors.New("device-id-or-event-type-is-missing")
	}
	if types, ok := efa.filters[filter.DeviceId]; ok {
		if _, ok := types[filter.EventType]; ok {
			log.Error("filter-already-present-in-db")
			return errors.New("filter-already-present")
		}
	}
	// If the device has some filters assigned to it then add it to the list of filters
	// for that device.
	if types := efa.filters[filter.DeviceId]; types != nil {
		if agent := efa.deviceMgr.getDeviceAgent(filter.DeviceId); agent != nil {
			if err := agent.adapterProxy.SuppressEvent(filter); err != nil {
				log.Error("create-request-not-sent-to-adapter")
				return err
			}
		} else {
			return status.Errorf(codes.NotFound, "%s", filter.DeviceId)
		}
		types[filter.EventType] = filter
	} else {
		// If this is a new device then this part is executed.
		if agent := efa.deviceMgr.getDeviceAgent(filter.DeviceId); agent != nil {
			if err := agent.adapterProxy.SuppressEvent(filter); err != nil {
				log.Error("create-request-not-sent-to-adapter")
				return err
			}
		} else {
			return status.Errorf(codes.NotFound, "%s", filter.DeviceId)
		}
		types := make(map[string]*voltha.EventFilter)
		types[filter.EventType] = filter
		efa.filters[filter.DeviceId] = types
	}
	path := fmt.Sprintf(FilterPath, filter.DeviceId, filter.EventType)
	value, err := json.Marshal(filter)
	if err != nil {
		log.Error("failed to Marshal")
		return err
	}
	if err = efa.KVStore.Put(path, value); err != nil {
		log.Errorf("Failed to update resource %s", path)
		if agent := efa.deviceMgr.getDeviceAgent(filter.DeviceId); agent != nil {
			if err := agent.adapterProxy.UnSuppressEvent(filter); err != nil {
				log.Error("create-request-not-sent-to-adapter")
				return err
			}
			return err
		}
	}
	log.Info("filter-created-successfully", log.Fields{"filter": filter})
	return nil
}

func (efa *EventFilterAgent) UpdateEventFilter(newFilter *voltha.EventFilter) error {
	// Check if device ID and event type is not missing
	if newFilter.DeviceId == "" || newFilter.EventType == "" {
		log.Errorw("device-id-or-event-type-is-missing", log.Fields{"filter": newFilter})
		return errors.New("device-id-or-event-type-is-missing")
	}
	// Check if the filter is present for the device
	if types, ok := efa.filters[newFilter.DeviceId]; !ok {
		if _, ok := types[newFilter.EventType]; !ok {
			log.Error("filter-not-present-in-db")
			return errors.New("filter-not-found-in-db")
		}
	}
	// Fetch the old filter
	oldFilter := efa.filters[newFilter.DeviceId][newFilter.EventType]
	// Checking if there are any rules to compare if no rules are supplied then
	// use the old rules. Otherwise compare the rules if the rules match then return error
	if len(newFilter.Rules) != 0 {
		//Compare the filter rules
		diff := false
		keyMatch := false
		if len(oldFilter.Rules) == len(newFilter.Rules) {
			for _, newRule := range newFilter.Rules {
				for _, oldRule := range oldFilter.Rules {
					if newRule.Key == oldRule.Key {
						keyMatch = true
						if oldRule.Value != newRule.Value {
							diff = true
							break
						}
					}
					if !keyMatch {
						diff = true
						break
					}
				}
				if diff {
					break
				}
			}
			if !diff {
				log.Errorw("rules-are-same", log.Fields{"new-rules": newFilter.Rules, "old-rules": oldFilter.Rules})
				return errors.New("rules-are-same")
			}
		}
	} else {
		if newFilter.Enable == oldFilter.Enable {
			log.Errorw("filter-is-same", log.Fields{"new-filter": newFilter, "old-filter": oldFilter})
			return errors.New("filter-is-same")
		}
		newFilter.Rules = oldFilter.Rules
	}
	newFilter.Id = oldFilter.Id
	// Inform the adapter about the change
	if agent := efa.deviceMgr.getDeviceAgent(newFilter.DeviceId); agent != nil {
		if err := agent.adapterProxy.UnSuppressEvent(newFilter); err != nil {
			log.Error("create-request-not-sent-to-adapter")
			return err
		}
		if err := agent.adapterProxy.SuppressEvent(newFilter); err != nil {
			log.Error("create-request-not-sent-to-adapter")
			return err
		}
	} else {
		return status.Errorf(codes.NotFound, "%s", newFilter.DeviceId)
	}
	// Update in the KV store
	path := fmt.Sprintf(FilterPath, oldFilter.DeviceId, oldFilter.EventType)
	if err := efa.KVStore.Delete(path); err != nil {
		log.Errorw("failed-to-delete-filter-from-store", log.Fields{"path": path})
		return err
	}
	value, err := json.Marshal(newFilter)
	if err != nil {
		log.Error("failed to Marshal")
		return err
	}
	if err = efa.KVStore.Put(path, value); err != nil {
		log.Errorf("Failed to update resource %s", path)
		return err
	}
	// update the filter map
	efa.filters[newFilter.DeviceId][newFilter.EventType] = newFilter
	log.Info("filter-updated-successfully")
	return nil
}

func (efa *EventFilterAgent) DeleteEventFilter(filterInfo *voltha.EventFilter) error {
	var id common.ID
	if filterInfo.Id != "" {
		id.Id = filterInfo.Id
		if err := efa.deleteSingleFilter(&id); err != nil {
			log.Infow("delete-single-filter-failed", log.Fields{"filter-id": id.Id})
			return err
		}
		log.Infow("filter-deleted-successfully", log.Fields{"filter-id": id.Id})
	}
	if filterInfo.DeviceId != "" {
		id.Id = filterInfo.DeviceId
		if err := efa.deleteAllDeviceFilters(&id); err != nil {
			log.Infow("delete-all-device-filter-failed", log.Fields{"device-id": id.Id})
			return err
		}
		log.Infow("filters-deleted-successfully", log.Fields{"device-id": id.Id})
	}
	return nil
}

func (efa *EventFilterAgent) deleteSingleFilter(filterID *common.ID) error {
	// TODO: Delete from KV store
	found := false
	for _, device := range efa.filters {
		for event, filter := range device {
			if filter.Id == filterID.Id {
				found = true
				path := fmt.Sprintf(FilterPath, filter.DeviceId, filter.EventType)
				if err := efa.KVStore.Delete(path); err != nil {
					log.Errorw("failed-to-delete-filter-from-store", log.Fields{"path": path, "filter-id": filterID})
					return err
				}
				if agent := efa.deviceMgr.getDeviceAgent(filter.DeviceId); agent != nil {
					if err := agent.adapterProxy.UnSuppressEvent(filter); err != nil {
						log.Error("delete-single-filter-request-not-sent-to-adapter")
						return err
					}
				}
				delete(device, event)
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		log.Errorw("filter-not-found", log.Fields{"filter-id": filterID.Id})
		return errors.New("filter-not-found")
	}
	return nil
}

func (efa *EventFilterAgent) deleteAllDeviceFilters(deviceID *common.ID) error {
	// TODO: Delete from KV store
	var agent *DeviceAgent
	found := false
	for id, device := range efa.filters {
		if id == deviceID.Id {
			found = true
			if agent = efa.deviceMgr.getDeviceAgent(id); agent == nil {
				return errors.New("not-found")
			}
			for _, filter := range device {
				path := fmt.Sprintf(FilterPath, id, filter.EventType)
				if err := efa.KVStore.Delete(path); err != nil {
					log.Errorw("failed-to-delete-filter-from-store", log.Fields{"path": path, "device-id": id})
					return err
				}
				if err := agent.adapterProxy.UnSuppressEvent(filter); err != nil {
					log.Error("create-request-not-sent-to-adapter")
					return err
				}
			}
			delete(efa.filters, id)
			break
		}
	}
	if !found {
		log.Errorw("device-not-found", log.Fields{"filter-id": deviceID.Id})
		return errors.New("filter-not-found")
	}
	log.Infow("filters-for device-deleted-successfully", log.Fields{"device-id": deviceID.Id})
	return nil
}

func (efa *EventFilterAgent) ListEventFilter() *voltha.EventFilters {
	var filters voltha.EventFilters
	if len(efa.filters) == 0 {
		log.Infow("no-filters-present", log.Fields{"filters": efa.filters})
		return &filters
	}
	for _, filterTypes := range efa.filters {
		for _, filter := range filterTypes {
			filters.Filters = append(filters.Filters, filter)
		}
	}
	return &filters
}

func (efa *EventFilterAgent) GetEventFilter(deviceID *common.ID) (*voltha.EventFilters, error) {
	var filters voltha.EventFilters
	if device, ok := efa.filters[deviceID.Id]; ok {
		for _, filter := range device {
			filters.Filters = append(filters.Filters, filter)
		}
		return &filters, nil
	}
	return nil, errors.New("device-not-present")
}
