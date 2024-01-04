/*
 * Copyright 2018-2024 Open Networking Foundation (ONF) and the ONF Contributors

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

	fu "github.com/opencord/voltha-lib-go/v7/pkg/flows"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listLogicalDeviceMeters returns logical device meters
func (agent *LogicalAgent) listLogicalDeviceMeters(ctx context.Context) map[uint32]*ofp.OfpMeterEntry {
	meterIDs := agent.meterLoader.ListIDs()
	meters := make(map[uint32]*ofp.OfpMeterEntry, len(meterIDs))
	for meterID := range meterIDs {
		if meterHandle, have := agent.meterLoader.Lock(meterID); have {
			meters[meterID] = meterHandle.GetReadOnly()
			meterHandle.Unlock()
		}
	}
	logger.Debugw(ctx, "list-logical-device-meters", log.Fields{"logical-device-id": agent.logicalDeviceID, "num-meters": len(meters)})
	return meters
}

// updateMeterTable updates the meter table of that logical device
func (agent *LogicalAgent) updateMeterTable(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug(ctx, "updateMeterTable")
	if meterMod == nil {
		return nil
	}
	// This lock is necessary to ensure that logical-device-delete and meter operations are synchronized.
	// It was observed during tests that while meter cleanup was happening as part of logical-device delete,
	// ONOS was re-adding meters which became stale entries on the KV store.
	// It might look like a costly operation to lock the logical-device-agent for every meter operation,
	// but in practicality we have only handful number of meter profiles for a given deployment so we do
	// not expect too many meter operations.
	// We do not need such mechanism for flows, since flows are not stored on the KV store (it is only cached).
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.stopped {
		logger.Warnw(ctx, "logical-device-already-stopped-not-handling-meter", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return nil
	} else if _, err := agent.ldeviceMgr.getLogicalDeviceFromModel(ctx, agent.logicalDeviceID); err != nil {
		logger.Warnw(ctx, "logical-device-already-removed-not-handling-meter", log.Fields{"logical-device-id": agent.logicalDeviceID})
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
	if meterMod == nil {
		logger.Errorw(ctx, "failed-meterAdd-meterMod-is-nil", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return nil
	}
	logger.Debugw(ctx, "meterAdd", log.Fields{"metermod": *meterMod, "logical-device-id": agent.logicalDeviceID})

	meterEntry := fu.MeterEntryFromMeterMod(ctx, meterMod)

	meterHandle, created, err := agent.meterLoader.LockOrCreate(ctx, meterEntry)
	if err != nil {
		return err
	}
	defer meterHandle.Unlock()

	if created {
		logger.Debugw(ctx, "Meter-added-successfully", log.Fields{"Added-meter": meterEntry, "logical-device-id": agent.logicalDeviceID})
	} else {
		logger.Infow(ctx, "Meter-already-exists", log.Fields{"meter": *meterMod, "logical-device-id": agent.logicalDeviceID})
	}
	return nil
}

func (agent *LogicalAgent) meterDelete(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	if meterMod == nil {
		logger.Errorw(ctx, "failed-meterDelete-meterMod-is-nil", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return nil
	}
	logger.Debug(ctx, "meterDelete", log.Fields{"meterMod": *meterMod, "logical-device-id": agent.logicalDeviceID})

	meterHandle, have := agent.meterLoader.Lock(meterMod.MeterId)
	if !have {
		logger.Warnw(ctx, "meter-not-found", log.Fields{"meterID": meterMod.MeterId, "logical-device-id": agent.logicalDeviceID})
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

	logger.Debugw(ctx, "meterDelete-success", log.Fields{"meterID": meterMod.MeterId, "logical-device-id": agent.logicalDeviceID})
	return nil
}

func (agent *LogicalAgent) meterModify(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug(ctx, "meterModify")
	if meterMod == nil {
		logger.Errorw(ctx, "failed-meterModify-meterMod-is-nil", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return nil
	}

	meterHandle, have := agent.meterLoader.Lock(meterMod.MeterId)
	if !have {
		return fmt.Errorf("no-meter-to-modify: %d, logical-device-id: %s", meterMod.MeterId, agent.logicalDeviceID)
	}
	defer meterHandle.Unlock()

	oldMeter := meterHandle.GetReadOnly()
	newMeter := fu.MeterEntryFromMeterMod(ctx, meterMod)
	newMeter.Stats.FlowCount = oldMeter.Stats.FlowCount

	if err := meterHandle.Update(ctx, newMeter); err != nil {
		return err
	}
	logger.Debugw(ctx, "replaced-with-new-meter", log.Fields{"oldMeter": oldMeter, "newMeter": newMeter, "logical-device-id": agent.logicalDeviceID})
	return nil
}
