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

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) updatePmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "updatePmConfigs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	// Store the device
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return err
	}
	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.UpdatePmConfigs(subCtx, cloned, pmConfigs)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "updatePmConfigs", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) initPmConfigs(ctx context.Context, pmConfigs *voltha.PmConfigs) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "initPmConfigs", log.Fields{"device-id": pmConfigs.Id})

	cloned := agent.cloneDeviceWithoutLock()
	cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

func (agent *Agent) listPmConfigs(ctx context.Context) (*voltha.PmConfigs, error) {
	logger.Debugw(ctx, "listPmConfigs", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err)
	}
	return device.PmConfigs, nil
}
