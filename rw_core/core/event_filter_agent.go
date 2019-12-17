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
	"context"
	"errors"
	"github.com/hashicorp/go-uuid"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
)

type EventFilterAgent struct {
	filters     map[string]map[string]*voltha.EventFilter
	deviceMgr   *DeviceManager
	modelProxy  *model.Proxy
	filterProxy *model.Proxy
}

func NewEventFilterAgent(deviceMgr *DeviceManager, modelProxy *model.Proxy) *EventFilterAgent {
	var evenFilterAgent EventFilterAgent
	evenFilterAgent.deviceMgr = deviceMgr
	evenFilterAgent.modelProxy = modelProxy
	evenFilterAgent.filters = make(map[string]map[string]*voltha.EventFilter)
	return &evenFilterAgent
}

func (efa *EventFilterAgent) start(ctx context.Context) error{
	log.Debug("starting-event-filter-agent")
	var err error
	efa.filterProxy,err = efa.modelProxy.CreateProxy(context.Background(), "/event_filters", false)
	if err != nil {
		log.Debug("failed-to-create-proxy")
		return err
	}
	return nil
}

func (efa *EventFilterAgent) CreateEventFilter(filter *voltha.EventFilter) (*voltha.EventFilter, error) {
	var agent *DeviceAgent
	if filter.DeviceId == "" || filter.EventType == "" {
		log.Errorw("device-id-or-event-type-is-missing", log.Fields{"filter": filter})
		return nil, errors.New("device-id-or-event-type-is-missing")
	}
	filter.Id, _ = uuid.GenerateUUID()
	if types, ok := efa.filters[filter.DeviceId]; ok {
		if _, ok := types[filter.EventType]; ok {
			log.Error("filter-already-present-in-db")
			return nil, errors.New("filter-already-present")
		}
	}
	if agent = efa.deviceMgr.getDeviceAgent(filter.DeviceId); agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", filter.DeviceId)
	}
	if err := agent.adapterProxy.SuppressEvent(filter); err != nil {
		log.Error("create-request-not-sent-to-adapter")
		return nil, err
	}
	if _, err := efa.modelProxy.Add(context.TODO(), "/event_filters", filter, ""); err != nil {
		log.Errorw("failed-to-add-filter-to-the-data-model", log.Fields{"error": err, "filter": filter})
	}
	if deviceFilters := efa.filters[filter.DeviceId]; deviceFilters != nil {
		deviceFilters[filter.EventType] = filter
	} else {
		types := make(map[string]*voltha.EventFilter)
		types[filter.EventType] = filter
		efa.filters[filter.DeviceId] = types
	}
	log.Info("filter-created-successfully", log.Fields{"filter": filter})
	return filter, nil
}

func (efa *EventFilterAgent) UpdateEventFilter(newFilter *voltha.EventFilter) (*voltha.EventFilter, error) {
	// Check if device ID and event type is not missing
	if newFilter.DeviceId == "" || newFilter.EventType == "" {
		log.Errorw("device-id-or-event-type-is-missing", log.Fields{"filter": newFilter})
		return nil, errors.New("device-id-or-event-type-is-missing")
	}
	// Check if the filter is present for the device
	if types, ok := efa.filters[newFilter.DeviceId]; !ok {
		if _, ok := types[newFilter.EventType]; !ok {
			log.Error("filter-not-present-in-db")
			return nil, errors.New("filter-not-found-in-db")
		}
	}
	// Fetch the old filter
	oldFilter := efa.filters[newFilter.DeviceId][newFilter.EventType]
	// Checking if there are any rules to compare if no rules are supplied then
	// use the old rules. Otherwise compare the rules if the rules match then return error
	if len(newFilter.Rules) != 0 {
		log.Debug("comparing-rules")
		oldRule := make(map[voltha.EventFilterRuleKey_EventFilterRuleType]string)
		newRule := make(map[voltha.EventFilterRuleKey_EventFilterRuleType]string)
		for _, rule := range newFilter.Rules {
			newRule[rule.Key] = rule.Value
		}
		for _, rule := range oldFilter.Rules {
			oldRule[rule.Key] = rule.Value
		}
		if reflect.DeepEqual(newRule, oldRule) {
			log.Error("filter-rules-are-same")
			return nil, errors.New("filter-rules-are-same")
		}
	} else if len(newFilter.Rules) == 0 {
		if newFilter.Enable == oldFilter.Enable {
			log.Error("filter-rules-are-same")
			return nil, errors.New("filter-rules-are-same")
		}
		newFilter.Rules = oldFilter.Rules
	}
	newFilter.Id = oldFilter.Id
	// Inform the adapter about the change
	if agent := efa.deviceMgr.getDeviceAgent(newFilter.DeviceId); agent != nil {
		if err := agent.adapterProxy.UnSuppressEvent(newFilter); err != nil {
			log.Error("create-request-not-sent-to-adapter")
			return nil, err
		}
		if err := agent.adapterProxy.SuppressEvent(newFilter); err != nil {
			log.Error("create-request-not-sent-to-adapter")
			return nil, err
		}
	} else {
		return nil, status.Errorf(codes.NotFound, "%s", newFilter.DeviceId)
	}
	// update the filter map
	if _, err := efa.modelProxy.Update(context.TODO(), "/event_filter/"+newFilter.Id, newFilter, false, ""); err != nil {
		log.Errorw("failed-to-update-filter-in-data-model", log.Fields{"error": err})
	}
	efa.filters[newFilter.DeviceId][newFilter.EventType] = newFilter
	log.Info("filter-updated-successfully")
	return newFilter, nil
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
	found := false
	for _, device := range efa.filters {
		for event, filter := range device {
			if filter.Id == filterID.Id {
				found = true
				if agent := efa.deviceMgr.getDeviceAgent(filter.DeviceId); agent != nil {
					if err := agent.adapterProxy.UnSuppressEvent(filter); err != nil {
						log.Error("delete-single-filter-request-not-sent-to-adapter")
						return err
					}
					if _, err := efa.modelProxy.Remove(context.TODO(), "/event_filters/"+filterID.Id, ""); err != nil {
						log.Errorw("failed-to-delete-from-data-model", log.Fields{"error": err})
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
	var agent *DeviceAgent
	found := false
	for id, device := range efa.filters {
		if id == deviceID.Id {
			found = true
			if agent = efa.deviceMgr.getDeviceAgent(id); agent == nil {
				return errors.New("not-found")
			}
			for _, filter := range device {
				if err := agent.adapterProxy.UnSuppressEvent(filter); err != nil {
					log.Error("create-request-not-sent-to-adapter")
					return err
				}
				if _, err := efa.modelProxy.Remove(context.TODO(), "/event_filters/"+filter.Id, ""); err != nil {
					log.Errorw("failed-to-delete-from-data-model", log.Fields{"error": err})
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

func (efa *EventFilterAgent) GetEventFilter(deviceID string) (*voltha.EventFilters, error) {
	var filters voltha.EventFilters
	if device, ok := efa.filters[deviceID]; ok {
		for _, filter := range device {
			filters.Filters = append(filters.Filters, filter)
		}
		return &filters, nil
	}
	return nil, errors.New("device-not-present")
}
