/*
 * Copyright 2018-2024 Open Networking Foundation (ONF) and the ONF Contributors

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
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) updatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	logger.Debugw(ctx, "update-pm-configs", log.Fields{"device-id": pmConfigs.Id})

	var rpce *voltha.RPCEvent
	defer func() {
		if rpce != nil {
			go agent.deviceMgr.SendRPCEvent(ctx, "RPC_ERROR_RAISE_EVENT", rpce,
				voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())
		}
	}()

	cloned, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}

	if !agent.proceedWithRequest(cloned) {
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Cannot complete operation as device deletion is in progress or reconciling is in progress/failed", cloned.Id)
	}

	// We need to send the response for the PM Config Updates in a synchronous manner to the adapter.
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": agent.adapterEndpoint,
			})
		return err
	}
	_, pmErr := client.UpdatePmConfig(ctx, &ca.PmConfigsInfo{
		DeviceId:  agent.deviceID,
		PmConfigs: pmConfigs,
	})

	if pmErr != nil {
		rpce = agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, pmErr.Error(), nil)
		return pmErr
	}
	// In case of no error for PM Config update, commit the new PM Config to DB.
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	// the Device properties might have changed due to other concurrent transactions on the device, so get latest copy
	cloned = agent.cloneDeviceWithoutLock()
	// commit new pm config
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)

	// Store back the device to DB and release lock
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		logger.Errorw(ctx, "error-updating-device-context-to-db", log.Fields{"device-id": agent.deviceID})
		rpce = agent.deviceMgr.NewRPCEvent(ctx, agent.deviceID, err.Error(), nil)
		return err
	}
	return nil
}

func (agent *Agent) initPmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "init-pm-configs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Cannot complete operation as device deletion is in progress or reconciling is in progress/failed", cloned.Id)
	}
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
