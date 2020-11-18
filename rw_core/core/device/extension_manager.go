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
	"github.com/opencord/voltha-protos/v4/go/extension"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ExtensionManager struct {
	DeviceManager *Manager
}

func GetNewExtensionManager(deviceManager *Manager) *ExtensionManager {
	return &ExtensionManager{DeviceManager: deviceManager}
}

func (e ExtensionManager) GetExtValue(ctx context.Context, request *extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	log.EnrichSpan(ctx, log.Fields{"device-id": request.TargetId})

	logger.Debugw(ctx, "GetExtValue", log.Fields{"request": request})
	agent := e.DeviceManager.getDeviceAgent(ctx, request.TargetId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "target-id %s", request.TargetId)
	}

	response, err := agent.getSingleValue(ctx, request)
	if err != nil {
		logger.Errorw(ctx, "Fail-to-get-single-value", log.Fields{"device-id": agent.deviceID, "error": err})
		return nil, err
	}

	logger.Debugw(ctx, "GetExtValue response", log.Fields{"response": response})
	return response, nil
}

func (e ExtensionManager) SetExtValue(ctx context.Context, request *extension.SingleSetValueRequest) (*extension.SingleSetValueResponse, error) {
	log.EnrichSpan(ctx, log.Fields{"device-id": request.TargetId})

	logger.Debugw(ctx, "SetExtValue", log.Fields{"request": request})
	agent := e.DeviceManager.getDeviceAgent(ctx, request.TargetId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "target-id %s", request.TargetId)
	}

	response, err := agent.setSingleValue(ctx, request)
	if err != nil {
		logger.Errorw(ctx, "Fail-to-set-single-value", log.Fields{"device-id": agent.deviceID, "error": err})
		return nil, err
	}

	logger.Debugw(ctx, "SetExtValue response", log.Fields{"response": response})
	return response, nil
}
