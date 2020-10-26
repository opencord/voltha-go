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

package device

import (
	"context"
	"fmt"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opentracing/opentracing-go"
	jtracing "github.com/uber/jaeger-client-go"
	"time"

	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

// listDeviceUpdates retrieves the list of device update in whole lifecycle of device.
func (agent *Agent) listDeviceUpdates(ctx context.Context, filter *voltha.DeviceUpdateFilter) *voltha.DeviceUpdates {
	logger.Debugw(ctx, "listDeviceUpdates", log.Fields{"device-id": agent.deviceID})
	returnedUpdates := agent.updateLoader.Load(ctx)

	var updates []*voltha.DeviceUpdate
	for _, update := range returnedUpdates.Items {
		if (filter.OperationId != "" && filter.OperationId != update.OperationId) ||
			(filter.Operation != "" && filter.Operation != update.Operation) ||
			(filter.Status != nil && filter.Status.Code != update.Status.Code) ||
			(filter.RequestedBy != "" && filter.RequestedBy != update.RequestedBy) ||
			(filter.FromTimestamp != nil && (filter.FromTimestamp.Seconds > update.Timestamp.Seconds || filter.FromTimestamp.Nanos > update.Timestamp.Nanos ||
				filter.ToTimestamp.Seconds < update.Timestamp.Seconds || filter.ToTimestamp.Nanos < update.Timestamp.Nanos)) {
			continue
		}
		updates = append(updates, update)

	}
	return &voltha.DeviceUpdates{Items: updates}
}

func (agent *Agent) newDeviceUpdate(ctx context.Context, operation string, requestedBy string) *voltha.DeviceUpdate {
	logger.Debugw(ctx, "newDeviceUpdate", log.Fields{"device-id": agent.deviceID})
	var opID string
	if span := opentracing.SpanFromContext(ctx); span != nil {
		if jSpan, ok := span.(*jtracing.Span); ok {
			opID = fmt.Sprintf("%016x", jSpan.SpanContext().TraceID().Low) // Using Sprintf to avoid removal of leading 0s
		}
	}
	now := time.Now()
	update := &voltha.DeviceUpdate{
		DeviceId: agent.deviceID,
		Timestamp: &timestamp.Timestamp{
			Seconds: now.Unix(),
			Nanos:   int32(now.Nanosecond()),
		},
		Operation:   operation,
		OperationId: opID,
		RequestedBy: requestedBy,
		StateChange: &voltha.DeviceStatesChange{
			Previous: &voltha.DeviceState{},
			Current:  &voltha.DeviceState{},
		},
		Status: &common.OperationResp{
			Code: common.OperationResp_OPERATION_FAILURE,
		},
	}
	return update
}

func (agent *Agent) deviceUpdate(ctx context.Context, update *voltha.DeviceUpdate) {
	err := agent.addDeviceUpdate(ctx, update)
	if err != nil {
		logger.Errorw(ctx, "unable-to-add-device-update", log.Fields{"error": err, "device-id": agent.deviceID, "operation-id": update.OperationId})
	}
}

// addDeviceUpdate append the device update in kv store
func (agent *Agent) addDeviceUpdate(ctx context.Context, update *voltha.DeviceUpdate) error {
	logger.Debugw(ctx, "addDeviceUpdate", log.Fields{"device-id": agent.deviceID})

	deviceUpdateHandle := agent.updateLoader.Lock()
	defer deviceUpdateHandle.Unlock()

	return deviceUpdateHandle.Update(ctx, update)
}

func (agent *Agent) updateDeviceStateChange(prevState *voltha.DeviceState, du *voltha.DeviceUpdate) {
	device := agent.getDeviceReadOnlyWithoutLock()
	if prevState != nil && (prevState.AdminState != device.AdminState ||
		prevState.OperStatus != device.OperStatus ||
		prevState.ConnectStatus != device.ConnectStatus) {
		du.StateChange.Current = &voltha.DeviceState{
			AdminState:    device.AdminState,
			OperStatus:    device.OperStatus,
			ConnectStatus: device.ConnectStatus,
		}
		du.StateChange.Previous = prevState
	}
}

func (agent *Agent) getDeviceState() *voltha.DeviceState {
	device := agent.getDeviceReadOnlyWithoutLock()
	return &voltha.DeviceState{
		AdminState:    device.AdminState,
		OperStatus:    device.OperStatus,
		ConnectStatus: device.ConnectStatus,
	}
}
