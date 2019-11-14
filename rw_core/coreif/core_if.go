/*
 * Copyright 2018-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
Defines a DeviceManager Interface - Used for unit testing of the flow decomposer only at this
time.
*/

package coreif

import (
	"context"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/kafka"
)

// Core represent core methods
type Core interface {
	Start(ctx context.Context)
	Stop(ctx context.Context)
	GetKafkaInterContainerProxy() *kafka.InterContainerProxy
	GetConfig() *config.RWCoreFlags
	GetInstanceId() string
	GetClusterDataProxy() *model.Proxy
	GetAdapterManager() AdapterManager
	StartGRPCService(ctx context.Context)
	GetDeviceManager() DeviceManager
	GetLogicalDeviceManager() LogicalDeviceManager
	GetDeviceOwnerShip() DeviceOwnership
}
