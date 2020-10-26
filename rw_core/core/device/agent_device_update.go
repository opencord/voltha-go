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
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/common"
)

/*type Update struct {
	StateChange string `protobuf:"bytes,6,opt,name=state_change,json=stateChange,proto3" json:"state_change,omitempty"`
	// Operation status
	Status *common.OperationResp `protobuf:"bytes,7,opt,name=status,proto3" json:"status,omitempty"`
	// A brief description to provide more context to this update
	Description          string   `protobuf:"bytes,8,opt,name=description,proto3" json:"description,omitempty"`
}*/

func (agent *Agent) logDeviceUpdate(ctx context.Context, operation string, prevState *common.AdminState_Types, status *common.OperationResp, desc *string) {
	logger.Debugw(ctx, "addDeviceUpdate", log.Fields{"device-id": agent.deviceID})

	var requestedBy string

	rb := ctx.Value("fromTopic")
	if rb != nil {
		requestedBy = rb.(string)
	}
	if requestedBy == "" {
		requestedBy = "NB"
	}

	logger.Infow(ctx, "logDeviceUpdate", log.Fields{"device-update": operation, "device-update-id": agent.deviceID,
		"requested-by": requestedBy, "state-change": agent.stateChangeString(prevState),
		"status": status.GetCode().String(), "description": desc})
}

func (agent *Agent) stateChangeString(prevState *common.AdminState_Types) string {
	device := agent.getDeviceReadOnlyWithoutLock()
	if prevState != nil && *prevState != device.AdminState {
		return fmt.Sprintf("%s->%s", *prevState, device.AdminState)
	}
	return ""
}
