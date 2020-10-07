/*
 * Copyright 2020-present Open Networking Foundation

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
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) getTransientState() voltha.DeviceTransientState_Types {
	transientStateHandle := agent.transientStateLoader.Lock()
	deviceTransientState := transientStateHandle.GetReadOnly()
	transientStateHandle.UnLock()
	return deviceTransientState
}

func (agent *Agent) updateTransientState(ctx context.Context, transientState voltha.DeviceTransientState_Types) error {
	// Update device transient state
	transientStateHandle := agent.transientStateLoader.Lock()
	if err := transientStateHandle.Update(ctx, transientState); err != nil {
		transientStateHandle.UnLock()
		return status.Errorf(codes.Internal, "failed-update-device-transient-state:%s: %s", agent.deviceID, err)
	}
	transientStateHandle.UnLock()
	return nil
}

func (agent *Agent) isDeletionInProgress() bool {
	deviceTransientState := agent.getTransientState()
	return deviceTransientState == voltha.DeviceTransientState_FORCE_DELETING ||
		deviceTransientState == voltha.DeviceTransientState_DELETING_FROM_ADAPTER ||
		deviceTransientState == voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE
}

func (agent *Agent) isStateDeleting(deviceTransientState voltha.DeviceTransientState_Types) bool {
	return deviceTransientState == voltha.DeviceTransientState_FORCE_DELETING ||
		deviceTransientState == voltha.DeviceTransientState_DELETING_FROM_ADAPTER ||
		deviceTransientState == voltha.DeviceTransientState_DELETING_POST_ADAPTER_RESPONSE
}
func (agent *Agent) deleteTransientState(ctx context.Context) error {
	transientStateHandle := agent.transientStateLoader.Lock()
	if err := transientStateHandle.Delete(ctx); err != nil {
		transientStateHandle.UnLock()
		return status.Errorf(codes.Internal, "failed-delete-device-transient-state:%s: %s", agent.deviceID, err)
	}
	transientStateHandle.UnLock()
	return nil
}
