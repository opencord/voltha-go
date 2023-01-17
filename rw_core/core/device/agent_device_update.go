/*
 * Copyright 2021-2023 Open Networking Foundation (ONF) and the ONF Contributors

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

	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
)

func (agent *Agent) logDeviceUpdate(ctx context.Context, prevState, currState *common.AdminState_Types, status *common.OperationResp, err error, desc string) {
	requestedBy := utils.GetEndpointMetadataFromContext(ctx)

	if requestedBy == "" {
		requestedBy = "NB"
	}

	rpc := utils.GetRPCMetadataFromContext(ctx)

	fields := log.Fields{"rpc": rpc, "device-id": agent.deviceID,
		"requested-by": requestedBy, "state-change": agent.stateChangeString(prevState, currState),
		"status": status.GetCode().String(), "description": desc, "error": err}

	if err != nil {
		logger.Errorw(ctx, "logDeviceUpdate-failed", fields)
		return
	}

	logger.Infow(ctx, "logDeviceUpdate-success", fields)
}

func (agent *Agent) stateChangeString(prevState *common.AdminState_Types, currState *common.AdminState_Types) string {
	if prevState != nil && currState != nil && *prevState != *currState {
		return fmt.Sprintf("%s->%s", *prevState, *currState)
	}
	return ""
}
