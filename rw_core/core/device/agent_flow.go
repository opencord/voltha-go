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
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
)

// listDeviceFlows returns device flows
func (agent *Agent) listDeviceFlows() map[uint64]*ofp.OfpFlowStats {
	flowIDs := agent.flowLoader.List()
	flows := make(map[uint64]*ofp.OfpFlowStats, len(flowIDs))
	for flowID := range flowIDs {
		if flowHandle, have := agent.flowLoader.Lock(flowID); have {
			flows[flowID] = flowHandle.GetReadOnly()
			flowHandle.Unlock()
		}
	}
	return flows
}
