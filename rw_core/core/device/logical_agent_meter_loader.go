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
	"sync"

	"github.com/gogo/protobuf/proto"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

//MeterChunk keeps a meter entry and its lock. The lock in the struct is used to syncronize the
//modifications for the related meter.
type MeterChunk struct {
	meter *ofp.OfpMeterEntry
	lock  sync.Mutex
}

func (agent *LogicalAgent) loadMeters(ctx context.Context) {
	agent.meterLock.Lock()
	defer agent.meterLock.Unlock()

	var meters []*ofp.OfpMeterEntry
	if err := agent.clusterDataProxy.List(ctx, "meters/"+agent.logicalDeviceID, &meters); err != nil {
		logger.Errorw("Failed-to-list-meters-from-proxy", log.Fields{"error": err})
		return
	}
	for _, meter := range meters {
		if meter.Config != nil {
			meterChunk := MeterChunk{
				meter: meter,
			}
			agent.meters[meter.Config.MeterId] = &meterChunk
		}
	}
}

//updateLogicalDeviceMeter updates meter info in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) updateLogicalDeviceMeter(ctx context.Context, meter *ofp.OfpMeterEntry, meterChunk *MeterChunk) error {
	path := fmt.Sprintf("meters/%s/%d", agent.logicalDeviceID, meter.Config.MeterId)
	if err := agent.clusterDataProxy.Update(ctx, path, meter); err != nil {
		logger.Errorw("error-updating-logical-device-with-meters", log.Fields{"error": err})
		return err
	}
	meterChunk.meter = meter
	return nil
}

//removeLogicalDeviceMeter deletes the meter from store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) removeLogicalDeviceMeter(ctx context.Context, meterID uint32) error {
	path := fmt.Sprintf("meters/%s/%d", agent.logicalDeviceID, meterID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-meter-from-store-%s", path)
	}
	agent.meterLock.Lock()
	defer agent.meterLock.Unlock()
	delete(agent.meters, meterID)
	return nil
}

// ListLogicalDeviceMeters returns logical device meters
func (agent *LogicalAgent) ListLogicalDeviceMeters(ctx context.Context) (*ofp.Meters, error) {
	logger.Debug("ListLogicalDeviceMeters")

	var meterEntries []*ofp.OfpMeterEntry
	agent.meterLock.RLock()
	defer agent.meterLock.RUnlock()
	for _, meterChunk := range agent.meters {
		meterEntries = append(meterEntries, (proto.Clone(meterChunk.meter)).(*ofp.OfpMeterEntry))
	}
	return &ofp.Meters{Items: meterEntries}, nil
}

// GetMeterConfig returns meter config
func (agent *LogicalAgent) GetMeterConfig(flows []*ofp.OfpFlowStats, meters []*ofp.OfpMeterEntry, metadata *voltha.FlowMetadata) error {
	m := make(map[uint32]bool)
	for _, flow := range flows {
		if flowMeterID := fu.GetMeterIdFromFlow(flow); flowMeterID != 0 && !m[flowMeterID] {
			foundMeter := false
			// Meter is present in the flow , Get from logical device
			for _, meter := range meters {
				if flowMeterID == meter.Config.MeterId {
					metadata.Meters = append(metadata.Meters, meter.Config)
					logger.Debugw("Found meter in logical device",
						log.Fields{"meterID": flowMeterID, "meter-band": meter.Config})
					m[flowMeterID] = true
					foundMeter = true
					break
				}
			}
			if !foundMeter {
				logger.Errorw("Meter-referred-by-flow-is-not-found-in-logicaldevice",
					log.Fields{"meterID": flowMeterID, "Available-meters": meters, "flow": *flow})
				return fmt.Errorf("Meter-referred-by-flow-is-not-found-in-logicaldevice.MeterId-%d", flowMeterID)
			}
		}
	}
	logger.Debugw("meter-bands-for-flows", log.Fields{"flows": len(flows), "metadata": metadata})
	return nil
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
	agent.meterLock.RLock()
	meterChunk, ok := agent.meters[meterID]
	agent.meterLock.RUnlock()
	if !ok {
		logger.Debugw("Meter-is-not-present-in-logical-device", log.Fields{"meterID": meterID})
		return true
	}

	//acquire the meter lock
	meterChunk.lock.Lock()
	defer meterChunk.lock.Unlock()

	if flowCommand == ofp.OfpFlowModCommand_OFPFC_ADD {
		if revertUpdate {
			meterChunk.meter.Stats.FlowCount--
		} else {
			meterChunk.meter.Stats.FlowCount++
		}
	} else if flowCommand == ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT {
		if revertUpdate {
			meterChunk.meter.Stats.FlowCount++
		} else {
			meterChunk.meter.Stats.FlowCount--
		}
	}

	//	Update store and cache
	if err := agent.updateLogicalDeviceMeter(ctx, meterChunk.meter, meterChunk); err != nil {
		logger.Debugw("unable-to-update-meter-in-db", log.Fields{"logicalDevice": agent.logicalDeviceID, "meterID": meterID})
		return false
	}

	logger.Debugw("updated-meter-flow-stats", log.Fields{"meterId": meterID})
	return true
}
