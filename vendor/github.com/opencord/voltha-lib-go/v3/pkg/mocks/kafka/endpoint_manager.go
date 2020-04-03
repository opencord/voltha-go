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
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
)

<<<<<<< HEAD
type EndpointManager struct{}

func NewEndpointManager() kafka.EndpointManager {
	mock := &EndpointManager{}
	return mock
}

func (em *EndpointManager) GetEndpoint(deviceID string, serviceType string) (kafka.Endpoint, error) {
	// TODO add mocks call and args
	return kafka.Endpoint(serviceType), nil
}

func (em *EndpointManager) IsDeviceOwnedByService(deviceID string, serviceType string, replicaNumber int32) (bool, error) {
=======
type endpointManagerMock struct {}

func NewEndpointManager() kafka.EndpointManager {
	mock := &endpointManagerMock{}
	return mock
}

func (em *endpointManagerMock) GetEndpoint(deviceID string, serviceType string) (kafka.Endpoint, error) {
	// TODO add mocks call and args
	return "mock-endpoint", nil
}

func (em *endpointManagerMock) IsDeviceOwnedByService(deviceID string, serviceType string, replicaNumber int32) (bool, error) {
>>>>>>> [VOL-2835] Using different topic per ONU device
	// TODO add mocks call and args
	return true, nil
}

<<<<<<< HEAD
func (em *EndpointManager) GetReplicaAssignment(deviceID string, serviceType string) (kafka.ReplicaID, error) {
	return kafka.ReplicaID(1), nil
}
=======
func (em *endpointManagerMock) getReplicaAssignment(deviceID string, serviceType string) (kafka.ReplicaID, error) {
	return kafka.ReplicaID(0), nil
}
>>>>>>> [VOL-2835] Using different topic per ONU device
