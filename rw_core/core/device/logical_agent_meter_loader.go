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

	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
)

// ListLogicalDeviceMeters returns logical device meters
func (agent *LogicalAgent) ListLogicalDeviceMeters(ctx context.Context) (*ofp.Meters, error) {
	logger.Debug("ListLogicalDeviceMeters")

	meterIDs := agent.meterLoader.ListMeters()
	ret := make([]*ofp.OfpMeterEntry, 0, len(meterIDs))
	for meterID := range meterIDs {
		if meterHandle, have := agent.meterLoader.LockMeter(meterID); have {
			ret = append(ret, meterHandle.GetReadOnly())
			meterHandle.Unlock()
		}
	}
	return &ofp.Meters{Items: ret}, nil
}

// GetMeterConfig returns meters which which are used by the given flows
func (agent *LogicalAgent) GetMeterConfig(flows map[uint64]*ofp.OfpFlowStats) (map[uint32]*ofp.OfpMeterConfig, error) {
	meters := make(map[uint32]*ofp.OfpMeterConfig)
	for _, flow := range flows {
		if flowMeterID := fu.GetMeterIdFromFlow(flow); flowMeterID != 0 {
			if _, have := meters[flowMeterID]; !have {
				// Meter is present in the flow, Get from logical device
				meterHandle, have := agent.meterLoader.LockMeter(flowMeterID)
				if !have {
					logger.Errorw("Meter-referred-by-flow-is-not-found-in-logicaldevice",
						log.Fields{"meterID": flowMeterID, "Available-meters": meters, "flow": *flow})
					return nil, fmt.Errorf("Meter-referred-by-flow-is-not-found-in-logicaldevice.MeterId-%d", flowMeterID)
				}

				meter := meterHandle.GetReadOnly()
				meters[flowMeterID] = meter.Config
				logger.Debugw("Found meter in logical device", log.Fields{"meterID": flowMeterID, "meter-band": meter.Config})

				meterHandle.Unlock()
			}
		}
	}
	logger.Debugw("meter-bands-for-flows", log.Fields{"flows": len(flows), "meters": meters})
	return meters, nil
}

func (agent *LogicalAgent) updateFlowCountOfMeterStats(ctx context.Context, modCommand *ofp.OfpFlowMod, flow *ofp.OfpFlowStats, revertUpdate bool) bool {
	flowCommand := modCommand.GetCommand()
	meterID := fu.GetMeterIdFromFlow(flow)
	logger.Debugw("Meter-id-in-flow-mod", log.Fields{"meterId": meterID})
	if meterID == 0 {
		logger.Debugw("No-meter-present-in-the-flow", log.Fields{"flow": *flow})
		return true
	}

	if flowCommand != ofp.OfpFlowModCommand_OFPFC_ADD && flowCommand != ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT {
		return true
	}

	meterHandle, have := agent.meterLoader.LockMeter(meterID)
	if !have {
		logger.Debugw("Meter-is-not-present-in-logical-device", log.Fields{"meterID": meterID})
		return true
	}
	defer meterHandle.Unlock()

	oldMeter := meterHandle.GetReadOnly()
	// avoiding using proto.Clone by only copying what have changed (this assumes that the oldMeter will never be modified)
	newStats := *oldMeter.Stats
	if flowCommand == ofp.OfpFlowModCommand_OFPFC_ADD {
		if revertUpdate {
			newStats.FlowCount--
		} else {
			newStats.FlowCount++
		}
	} else if flowCommand == ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT {
		if revertUpdate {
			newStats.FlowCount++
		} else {
			newStats.FlowCount--
		}
	}

	newMeter := &ofp.OfpMeterEntry{
		Config: oldMeter.Config,
		Stats:  &newStats,
	}
	if err := meterHandle.Update(ctx, newMeter); err != nil {
		logger.Debugw("unable-to-update-meter-in-db", log.Fields{"logicalDevice": agent.logicalDeviceID, "meterID": meterID})
		return false
	}

	logger.Debugw("updated-meter-flow-stats", log.Fields{"meterId": meterID})
	return true
}
