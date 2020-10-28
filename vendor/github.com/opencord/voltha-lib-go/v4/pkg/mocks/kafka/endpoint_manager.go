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

package kafka

import (
	"context"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
)

type EndpointManager struct{}

func NewEndpointManager() kafka.EndpointManager {
	mock := &EndpointManager{}
	return mock
}

func (em *EndpointManager) GetEndpoint(ctx context.Context, deviceID string, serviceType string) (kafka.Endpoint, error) {
	// TODO add mocks call and args
	return kafka.Endpoint(serviceType), nil
}

func (em *EndpointManager) IsDeviceOwnedByService(ctx context.Context, deviceID string, serviceType string, replicaNumber int32) (bool, error) {
	// TODO add mocks call and args
	return true, nil
}

func (em *EndpointManager) GetReplicaAssignment(ctx context.Context, deviceID string, serviceType string) (kafka.ReplicaID, error) {
	return kafka.ReplicaID(1), nil
}
