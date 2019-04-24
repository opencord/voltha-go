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
  Defines a logicalDeviceAgent Interface - Used for unit testing of the flow decomposer only at this
 time.
*/
package coreIf

import (
	"github.com/opencord/voltha-go/rw_core/graph"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-protos/go/voltha"
)

// LogicalAgent represents a generic agent
type LogicalDeviceAgent interface {
	GetLogicalDevice() (*voltha.LogicalDevice, error)
	GetDeviceGraph() *graph.DeviceGraph
	GetAllDefaultRules() *utils.DeviceRules
	GetWildcardInputPorts(excludePort ...uint32) []uint32
	GetRoute(ingressPortNo uint32, egressPortNo uint32) []graph.RouteHop
}
