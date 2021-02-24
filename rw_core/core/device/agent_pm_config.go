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
	"time"

	"github.com/gogo/protobuf/proto"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) updatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "update-pm-configs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.cloneDeviceWithoutLock()
	oldPmConfigs := proto.Clone(cloned.PmConfigs).(*voltha.PmConfigs) // back up old config
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	ch, pmErr := agent.adapterProxy.UpdatePmConfigs(subCtx, cloned, pmConfigs)
	if pmErr != nil {
		cancel()
		return pmErr
	}

	defer cancel()
	var rpce *voltha.RPCEvent
	defer func() {
		if rpce != nil {
			go agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce,
				voltha.EventCategory_COMMUNICATION, nil, time.Now().UnixNano())
		}
	}()
	// We need to send the response for the PM Config Updates in a synchronous manner to the caller.
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			pmErr = fmt.Errorf("response-channel-closed")
			rpce = agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, pmErr.Error(), nil)
		} else if rpcResponse.Err != nil {
			rpce = agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, rpcResponse.Err.Error(), nil)
			pmErr = rpcResponse.Err
		}
	case <-ctx.Done():
		rpce = agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, ctx.Err().Error(), nil)
		pmErr = ctx.Err()
	}
	// In case of any error in response, revert to old pm config
	if pmErr != nil {
		cloned.PmConfigs = oldPmConfigs
	}

	// Store back the device to DB
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		logger.Errorw(ctx, "error-updating-device-context-to-db", log.Fields{"device-id": agent.deviceID})
		rpce = agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
		return err
	}

	return pmErr
}

func (agent *Agent) initPmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "init-pm-configs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

func (agent *Agent) listPmConfigs(ctx context.Context) (*voltha.PmConfigs, error) {
	logger.Debugw(ctx, "list-pm-configs", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err)
	}
	return device.PmConfigs, nil
}
