/*
* Copyright 2021-2024 Open Networking Foundation (ONF) and the ONF Contributors

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

	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/core"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateLogicalDevice creates logical device in core
func (dMgr *Manager) CreateLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info(ctx, "create-logical-device")
	// Verify whether the logical device has already been created
	if cDevice.ParentId != "" {
		logger.Debugw(ctx, "parent-device-already-exist", log.Fields{"device-id": cDevice.Id, "logical-device-id": cDevice.Id})
		return nil
	}
	var err error
	if _, err = dMgr.logicalDeviceMgr.createLogicalDevice(ctx, cDevice); err != nil {
		logger.Warnw(ctx, "create-logical-device-error", log.Fields{"device": cDevice})
		return err
	}
	return nil
}

// DeleteLogicalDevice deletes logical device from core
func (dMgr *Manager) DeleteLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info(ctx, "delete-logical-device")
	if err := dMgr.logicalDeviceMgr.deleteLogicalDevice(ctx, cDevice); err != nil {
		return err
	}
	// Remove the logical device Id from the parent device
	logicalID := ""
	dMgr.UpdateDeviceAttribute(ctx, cDevice.Id, "ParentId", logicalID)
	return nil
}

// DeleteLogicalPorts removes the logical ports associated with that deviceId
func (dMgr *Manager) DeleteLogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-logical-ports", log.Fields{"device-id": cDevice.Id})
	if err := dMgr.logicalDeviceMgr.deleteLogicalPorts(ctx, cDevice.Id); err != nil {
		// Just log the error.   The logical device or port may already have been deleted before this callback is invoked.
		logger.Warnw(ctx, "delete-logical-ports-error", log.Fields{"device-id": cDevice.Id, "error": err})
	}
	return nil
}

// SetupUNILogicalPorts creates UNI ports on the logical device that represents a child UNI interface
func (dMgr *Manager) SetupUNILogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info(ctx, "setup-uni-logical-ports")
	cDevicePorts, err := dMgr.listDevicePorts(ctx, cDevice.Id)
	if err != nil {
		return err
	}
	if err := dMgr.logicalDeviceMgr.setupUNILogicalPorts(ctx, cDevice, cDevicePorts); err != nil {
		logger.Warnw(ctx, "setup-uni-logical-ports-error", log.Fields{"device": cDevice, "err": err})
		return err
	}
	return nil
}

// RunPostDeviceDelete removes any reference of this device
func (dMgr *Manager) RunPostDeviceDelete(ctx context.Context, cDevice *voltha.Device) error {
	logger.Infow(ctx, "run-post-device-delete", log.Fields{"device-id": cDevice.Id})
	// Deleting the logical device
	logger.Debugw(ctx, "delete-logical-device", log.Fields{"device-id": cDevice.Id})
	if dMgr.logicalDeviceMgr != nil && cDevice.Root {
		if err := dMgr.logicalDeviceMgr.deleteLogicalDevice(ctx, cDevice); err != nil {
			logger.Warnw(ctx, "failure-delete-logical-device", log.Fields{"device-id": cDevice.Id, "error": err.Error()})
		}
		// Remove the logical device Id from the parent device
		logicalID := ""
		dMgr.UpdateDeviceAttribute(ctx, cDevice.Id, "ParentId", logicalID)
	}
	if agent := dMgr.getDeviceAgent(ctx, cDevice.Id); agent != nil {
		logger.Debugw(ctx, "invoking-delete-device-and-ports", log.Fields{"device-id": cDevice.Id})
		// Delete ports
		if err := agent.deleteAllPorts(ctx); err != nil {
			logger.Warnw(ctx, "failure-delete-device-ports", log.Fields{"device-id": cDevice.Id, "error": err.Error()})
		}
	}
	dMgr.stopManagingDevice(ctx, cDevice.Id)
	return nil
}

// DeleteAllLogicalPorts is invoked as a callback when the parent device's connection status moves to UNREACHABLE
func (dMgr *Manager) DeleteAllLogicalPorts(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-logical-ports", log.Fields{"parent-device-id": parentDevice.Id})
	if err := dMgr.logicalDeviceMgr.deleteAllLogicalPorts(ctx, parentDevice); err != nil {
		// Just log error as logical device may already have been deleted
		logger.Warnw(ctx, "delete-all-logical-ports-fail", log.Fields{"parent-device-id": parentDevice.Id, "error": err})
	}
	return nil
}

// DeleteAllChildDevices is invoked as a callback when the parent device is deleted.  The force
// delete option is used in this callback because if the child device is in reconcile state then
// a delete request with no force option would not be sent to the child adapter, hence leaving the
// system in an unknown state.  See https://jira.opencord.org/browse/VOL-4421 for more details.
func (dMgr *Manager) DeleteAllChildDevices(ctx context.Context, parentCurrDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-child-devices", log.Fields{"parent-device-id": parentCurrDevice.Id})

	agent := dMgr.getDeviceAgent(ctx, parentCurrDevice.Id)
	if agent == nil {
		return status.Errorf(codes.NotFound, "%s", parentCurrDevice.Id)
	}
	// The retry mechanism ensures that failed child device deletions are re-attempted.
	// We continue retrying the deletion process until all child devices are successfully deleted.
	childDeviceIDs := dMgr.getAllChildDeviceIds(ctx, parentCurrDevice.Id)
	if len(childDeviceIDs) == 0 {
		logger.Debugw(ctx, "no-child-devices-to-delete", log.Fields{"parent-device-id": parentCurrDevice.Id})
		return nil
	}

	retryNeeded := true

	// Keep retrying until all devices are deleted
	for retryNeeded {
		retryNeeded = false

		for childDeviceID := range childDeviceIDs {
			if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
				logger.Debugw(ctx, "invoking-delete-device", log.Fields{"device-id": childDeviceID, "parent-device-id": parentCurrDevice.Id})

				// Attempt to delete the device forcefully. If it fails, we mark it for retry.
				if err := agent.deleteDeviceForce(ctx); err != nil {
					logger.Warnw(ctx, "delete-device-force-failed", log.Fields{
						"device-id":        childDeviceID,
						"parent-device-id": parentCurrDevice.Id,
						"error":            err,
					})

					// On failure, mark the device as DELETE_FAILED to trigger a retry.
					if err = agent.updateTransientState(ctx, core.DeviceTransientState_DELETE_FAILED); err != nil {
						logger.Warnw(ctx, "failed-updating-transient-state", log.Fields{
							"device-id":        childDeviceID,
							"parent-device-id": parentCurrDevice.Id,
							"error":            err,
						})
					}

					retryNeeded = true
				} else {
					// On successful deletion, remove the child device ID from the map.
					delete(childDeviceIDs, childDeviceID)
					logger.Debugw(ctx, "child-device-deleted", log.Fields{"device-id": childDeviceID})
				}
			}
		}
	}

	logger.Debugw(ctx, "all-child-devices-deleted", log.Fields{"parent-device-id": parentCurrDevice.Id})
	return nil
}

// DeleteAllDeviceFlows is invoked as a callback when the parent device's connection status moves to UNREACHABLE
func (dMgr *Manager) DeleteAllDeviceFlows(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-device-flows", log.Fields{"parent-device-id": parentDevice.Id})
	if agent := dMgr.getDeviceAgent(ctx, parentDevice.Id); agent != nil {
		if err := agent.deleteAllFlows(ctx); err != nil {
			logger.Errorw(ctx, "error-deleting-all-device-flows", log.Fields{"parent-device-id": parentDevice.Id})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
}

// ChildDeviceLost  calls parent adapter to delete child device and all its references
func (dMgr *Manager) ChildDeviceLost(ctx context.Context, curr *voltha.Device) error {
	logger.Debugw(ctx, "child-device-lost", log.Fields{"child-device-id": curr.Id, "parent-device-id": curr.ParentId})
	if parentAgent := dMgr.getDeviceAgent(ctx, curr.ParentId); parentAgent != nil {
		if err := parentAgent.ChildDeviceLost(ctx, curr); err != nil {
			// Just log the message and let the remaining pipeline proceed.
			logger.Warnw(ctx, "childDeviceLost", log.Fields{"child-device-id": curr.Id, "parent-device-id": curr.ParentId, "error": err})
		}
	}
	// Do not return an error as parent device may also have been deleted.  Let the remaining pipeline proceed.
	return nil
}

func (dMgr *Manager) DeleteAllLogicalMeters(ctx context.Context, cDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-logical-device-meters", log.Fields{"device-id": cDevice.Id})
	// Get logical device id
	ldID, err := dMgr.logicalDeviceMgr.getLogicalDeviceIDFromDeviceID(ctx, cDevice.Id)
	if err != nil {
		return err
	}
	if err := dMgr.logicalDeviceMgr.deleteAllLogicalMetersForLogicalDevice(ctx, *ldID); err != nil {
		// Just log the error.   The logical device or port may already have been deleted before this callback is invoked.
		logger.Warnw(ctx, "delete-logical-meters-error", log.Fields{"device-id": cDevice.Id, "error": err})
	}
	return nil

}

// ClearTransientState updates the parent device transient state to none
func (dMgr *Manager) ClearDeviceTransientState(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw(ctx, "clear-device-transient-state", log.Fields{"parent-device-id": parentDevice.Id})
	if agent := dMgr.getDeviceAgent(ctx, parentDevice.Id); agent != nil {
		// Update the transient state to none if the device is root and oper status is active and transient state is olt reboot in progress
		if err := agent.updateTransientState(ctx, core.DeviceTransientState_NONE); err != nil {
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
}
