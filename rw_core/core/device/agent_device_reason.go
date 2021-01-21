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
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

// listDeviceUpdates retrieves the list of device reason in whole lifecycle of device.
func (agent *Agent) listDeviceReasons(ctx context.Context, deviceID string) *voltha.DeviceReasons {
	logger.Debugw(ctx, "listDeviceUpdates", log.Fields{"device-id": agent.deviceID})

	returnedReasons := agent.reasonLoader.Load(ctx)

	return &voltha.DeviceReasons{Items: returnedReasons.Items}
}

func (agent *Agent) UpdateDeviceReason(ctx context.Context, reason string) error {

	deviceReasonHandle := agent.reasonLoader.Lock()
	defer deviceReasonHandle.Unlock()

	return deviceReasonHandle.Update(ctx, &voltha.DeviceReason{
		DeviceId: agent.deviceID,
		Reason:   reason,
	})
}
