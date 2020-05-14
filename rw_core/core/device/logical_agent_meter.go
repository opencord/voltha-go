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
	"strconv"

	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// updateMeterTable updates the meter table of that logical device
func (agent *LogicalAgent) updateMeterTable(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("updateMeterTable")
	if meterMod == nil {
		return nil
	}
	switch meterMod.GetCommand() {
	case ofp.OfpMeterModCommand_OFPMC_ADD:
		return agent.meterAdd(ctx, meterMod)
	case ofp.OfpMeterModCommand_OFPMC_DELETE:
		return agent.meterDelete(ctx, meterMod)
	case ofp.OfpMeterModCommand_OFPMC_MODIFY:
		return agent.meterModify(ctx, meterMod)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, meterMod.GetCommand())
}

func (agent *LogicalAgent) meterAdd(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debugw("meterAdd", log.Fields{"metermod": *meterMod})
	if meterMod == nil {
		return nil
	}

	meterEntry := fu.MeterEntryFromMeterMod(meterMod)
	agent.meterLock.Lock()
	//check if the meter already exists or not
	_, ok := agent.meters[meterMod.MeterId]
	if ok {
		logger.Infow("Meter-already-exists", log.Fields{"meter": *meterMod})
		agent.meterLock.Unlock()
		return nil
	}

	mChunk := MeterChunk{
		meter: meterEntry,
	}
	//Add to map and acquire the per meter lock
	agent.meters[meterMod.MeterId] = &mChunk
	mChunk.lock.Lock()
	defer mChunk.lock.Unlock()
	agent.meterLock.Unlock()
	meterID := strconv.Itoa(int(meterMod.MeterId))
	if err := agent.clusterDataProxy.AddWithID(ctx, "meters/"+agent.logicalDeviceID, meterID, meterEntry); err != nil {
		logger.Errorw("failed-adding-meter", log.Fields{"deviceID": agent.logicalDeviceID, "meterID": meterID, "err": err})
		//Revert the map
		agent.meterLock.Lock()
		delete(agent.meters, meterMod.MeterId)
		agent.meterLock.Unlock()
		return err
	}

	logger.Debugw("Meter-added-successfully", log.Fields{"Added-meter": meterEntry})
	return nil
}

func (agent *LogicalAgent) meterDelete(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("meterDelete", log.Fields{"meterMod": *meterMod})
	if meterMod == nil {
		return nil
	}
	agent.meterLock.RLock()
	meterChunk, ok := agent.meters[meterMod.MeterId]
	agent.meterLock.RUnlock()
	if ok {
		//Dont let anyone to do any changes to this meter until this is done.
		//And wait if someone else is already making modifications. Do this with per meter lock.
		meterChunk.lock.Lock()
		defer meterChunk.lock.Unlock()
		if err := agent.deleteFlowsOfMeter(ctx, meterMod.MeterId); err != nil {
			return err
		}
		//remove from the store and cache
		if err := agent.removeLogicalDeviceMeter(ctx, meterMod.MeterId); err != nil {
			return err
		}
		logger.Debugw("meterDelete-success", log.Fields{"meterID": meterMod.MeterId})
	} else {
		logger.Warnw("meter-not-found", log.Fields{"meterID": meterMod.MeterId})
	}
	return nil
}

func (agent *LogicalAgent) meterModify(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("meterModify")
	if meterMod == nil {
		return nil
	}
	newMeter := fu.MeterEntryFromMeterMod(meterMod)
	agent.meterLock.RLock()
	meterChunk, ok := agent.meters[newMeter.Config.MeterId]
	agent.meterLock.RUnlock()
	if !ok {
		return fmt.Errorf("no-meter-to-modify:%d", newMeter.Config.MeterId)
	}
	//Release the map lock and syncronize per meter
	meterChunk.lock.Lock()
	defer meterChunk.lock.Unlock()
	oldMeter := meterChunk.meter
	newMeter.Stats.FlowCount = oldMeter.Stats.FlowCount

	if err := agent.updateLogicalDeviceMeter(ctx, newMeter, meterChunk); err != nil {
		logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "meterID": newMeter.Config.MeterId})
		return err
	}
	logger.Debugw("replaced-with-new-meter", log.Fields{"oldMeter": oldMeter, "newMeter": newMeter})
	return nil
}
