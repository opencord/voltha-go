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
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
)

// Core represent core methods
type Core interface {
	Start(ctx context.Context)
	Stop(ctx context.Context)
	GetKafkaInterContainerProxy(ctx context.Context) *kafka.InterContainerProxy
	GetConfig(ctx context.Context) *config.RWCoreFlags
	GetInstanceId(ctx context.Context) string
	GetClusterDataProxy(ctx context.Context) *model.Proxy
	GetAdapterManager(ctx context.Context) AdapterManager
	StartGRPCService(ctx context.Context)
	GetDeviceManager(ctx context.Context) DeviceManager
	GetLogicalDeviceManager(ctx context.Context) LogicalDeviceManager
}
