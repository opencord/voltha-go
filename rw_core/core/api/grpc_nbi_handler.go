/*
 * Copyright 2018-present Open Networking Foundation

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

package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device"
	"github.com/opencord/voltha-lib-go/v7/pkg/version"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/omci"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

// APIHandler combines the partial API implementations in various components into a complete voltha implementation
type APIHandler struct {
	*device.Manager
	*device.LogicalManager
	adapterManager // *adapter.Manager
}

// avoid having multiple embedded types with the same name (`<package>.Manager`s conflict)
type adapterManager struct{ *adapter.Manager }

// NewAPIHandler creates API handler instance
func NewAPIHandler(deviceMgr *device.Manager, logicalDeviceMgr *device.LogicalManager, adapterMgr *adapter.Manager) *APIHandler {
	return &APIHandler{
		Manager:        deviceMgr,
		LogicalManager: logicalDeviceMgr,
		adapterManager: adapterManager{adapterMgr},
	}
}

// GetVoltha currently just returns version information
func (handler *APIHandler) GetVoltha(ctx context.Context, _ *empty.Empty) (*voltha.Voltha, error) {
	logger.Debug(ctx, "GetVoltha")
	/*
	 * For now, encode all the version information into a JSON object and
	 * pass that back as "version" so the client can get all the
	 * information associated with the version. Long term the API should
	 * better accommodate this, but for now this will work.
	 */
	data, err := json.Marshal(&version.VersionInfo)
	if err != nil {
		logger.Warnf(ctx, "Unable to encode version information as JSON: %s", err.Error())
		return &voltha.Voltha{Version: version.VersionInfo.Version}, nil
	}
	return &voltha.Voltha{Version: string(data)}, nil
}

var errUnimplemented = errors.New("unimplemented")

func (handler *APIHandler) ListCoreInstances(context.Context, *empty.Empty) (*voltha.CoreInstances, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) GetCoreInstance(context.Context, *voltha.ID) (*voltha.CoreInstance, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) CreateEventFilter(context.Context, *voltha.EventFilter) (*voltha.EventFilter, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) UpdateEventFilter(context.Context, *voltha.EventFilter) (*voltha.EventFilter, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) DeleteEventFilter(context.Context, *voltha.EventFilter) (*empty.Empty, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) GetEventFilter(context.Context, *voltha.ID) (*voltha.EventFilters, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) ListEventFilters(context.Context, *empty.Empty) (*voltha.EventFilters, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) SelfTest(context.Context, *voltha.ID) (*voltha.SelfTestResponse, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) GetAlarmDeviceData(context.Context, *common.ID) (*omci.AlarmDeviceData, error) {
	return nil, errUnimplemented
}
func (handler *APIHandler) GetMibDeviceData(context.Context, *common.ID) (*omci.MibDeviceData, error) {
	return nil, errUnimplemented
}
