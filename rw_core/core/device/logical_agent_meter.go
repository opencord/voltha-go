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

	fu "github.com/opencord/voltha-lib-go/v4/pkg/flows"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listLogicalDeviceMeters returns logical device meters
func (agent *LogicalAgent) listLogicalDeviceMeters() map[uint32]*ofp.OfpMeterEntry {
	meterIDs := agent.meterLoader.ListIDs()
	meters := make(map[uint32]*ofp.OfpMeterEntry, len(meterIDs))
	for meterID := range meterIDs {
		if meterHandle, have := agent.meterLoader.Lock(meterID); have {
			meters[meterID] = meterHandle.GetReadOnly()
			meterHandle.Unlock()
		}
	}
	return meters
}

// updateMeterTable updates the meter table of that logical device
func (agent *LogicalAgent) updateMeterTable(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug(ctx, "updateMeterTable")
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
		"unhandled-command: logical-device-id:%s, command:%s", agent.logicalDeviceID, meterMod.GetCommand())
}

func (agent *LogicalAgent) meterAdd(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debugw(ctx, "meterAdd", log.Fields{"metermod": *meterMod})
	if meterMod == nil {
		return nil
	}

	meterEntry := fu.MeterEntryFromMeterMod(ctx, meterMod)

	meterHandle, created, err := agent.meterLoader.LockOrCreate(ctx, meterEntry)
	if err != nil {
		return err
	}
	defer meterHandle.Unlock()

	if created {
		logger.Debugw(ctx, "Meter-added-successfully", log.Fields{"Added-meter": meterEntry})
	} else {
		logger.Infow(ctx, "Meter-already-exists", log.Fields{"meter": *meterMod})
	}
	return nil
}

func (agent *LogicalAgent) meterDelete(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug(ctx, "meterDelete", log.Fields{"meterMod": *meterMod})
	if meterMod == nil {
		return nil
	}

	meterHandle, have := agent.meterLoader.Lock(meterMod.MeterId)
	if !have {
		logger.Warnw(ctx, "meter-not-found", log.Fields{"meterID": meterMod.MeterId})
		return nil
	}
	defer meterHandle.Unlock()

	//TODO: A meter lock is held here while flow lock(s) are acquired, if this is done in opposite order anywhere
	//      there's potential for deadlock.
	if err := agent.deleteFlowsHavingMeter(ctx, meterMod.MeterId); err != nil {
		return err
	}

	if err := meterHandle.Delete(ctx); err != nil {
		return err
	}

	logger.Debugw(ctx, "meterDelete-success", log.Fields{"meterID": meterMod.MeterId})
	return nil
}

func (agent *LogicalAgent) meterModify(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug(ctx, "meterModify")
	if meterMod == nil {
		return nil
	}

	meterHandle, have := agent.meterLoader.Lock(meterMod.MeterId)
	if !have {
		return fmt.Errorf("no-meter-to-modify: %d", meterMod.MeterId)
	}
	defer meterHandle.Unlock()

	oldMeter := meterHandle.GetReadOnly()
	newMeter := fu.MeterEntryFromMeterMod(ctx, meterMod)
	newMeter.Stats.FlowCount = oldMeter.Stats.FlowCount

	if err := meterHandle.Update(ctx, newMeter); err != nil {
		return err
	}
	logger.Debugw(ctx, "replaced-with-new-meter", log.Fields{"oldMeter": oldMeter, "newMeter": newMeter})
	return nil
}
