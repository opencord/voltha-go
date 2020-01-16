/*
 * Copyright 2019-present Open Networking Foundation

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

package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func init() {
	_, err := log.AddPackage(log.JSON, log.WarnLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

type ownership struct {
	id    string
	owned bool
	chnl  chan int
}

// DeviceOwnership represent device ownership attributes
type DeviceOwnership struct {
	instanceID         string
	exitChannel        chan int
	kvClient           kvstore.Client
	reservationTimeout int64 // Duration in seconds
	ownershipPrefix    string
	deviceMgr          *DeviceManager
	logicalDeviceMgr   *LogicalDeviceManager
	deviceMap          map[string]*ownership
	deviceMapLock      sync.RWMutex
	deviceToKeyMap     map[string]string
	deviceToKeyMapLock sync.RWMutex
	ownershipLock      sync.RWMutex
}

// NewDeviceOwnership creates device ownership instance
func NewDeviceOwnership(id string, kvClient kvstore.Client, deviceMgr *DeviceManager, logicalDeviceMgr *LogicalDeviceManager, ownershipPrefix string, reservationTimeout int64) *DeviceOwnership {
	var deviceOwnership DeviceOwnership
	deviceOwnership.instanceID = id
	deviceOwnership.exitChannel = make(chan int, 1)
	deviceOwnership.kvClient = kvClient
	deviceOwnership.deviceMgr = deviceMgr
	deviceOwnership.logicalDeviceMgr = logicalDeviceMgr
	deviceOwnership.ownershipPrefix = ownershipPrefix
	deviceOwnership.reservationTimeout = reservationTimeout
	deviceOwnership.deviceMap = make(map[string]*ownership)
	deviceOwnership.deviceMapLock = sync.RWMutex{}
	deviceOwnership.deviceToKeyMap = make(map[string]string)
	deviceOwnership.deviceToKeyMapLock = sync.RWMutex{}
	deviceOwnership.ownershipLock = sync.RWMutex{}
	return &deviceOwnership
}

// Start starts device device ownership
func (da *DeviceOwnership) Start(ctx context.Context) {
	log.Info("starting-deviceOwnership", log.Fields{"instanceId": da.instanceID})
	log.Info("deviceOwnership-started")
}

// Stop stops device ownership
func (da *DeviceOwnership) Stop(ctx context.Context) {
	log.Info("stopping-deviceOwnership")
	da.exitChannel <- 1
	// Need to flush all device reservations
	da.abandonAllDevices()
	log.Info("deviceOwnership-stopped")
}

func (da *DeviceOwnership) tryToReserveKey(ctx context.Context, id string) bool {
	var currOwner string
	//Try to reserve the key
	kvKey := fmt.Sprintf("%s_%s", da.ownershipPrefix, id)
	value, err := da.kvClient.Reserve(ctx, kvKey, da.instanceID, da.reservationTimeout)
	if err != nil {
		log.Errorw("error", log.Fields{"error": err, "id": id, "instanceId": da.instanceID})
	}
	if value != nil {
		if currOwner, err = kvstore.ToString(value); err != nil {
			log.Error("unexpected-owner-type")
		}
		return currOwner == da.instanceID
	}
	return false
}

func (da *DeviceOwnership) renewReservation(ctx context.Context, id string) bool {
	// Try to reserve the key
	kvKey := fmt.Sprintf("%s_%s", da.ownershipPrefix, id)
	if err := da.kvClient.RenewReservation(ctx, kvKey); err != nil {
		log.Errorw("reservation-renewal-error", log.Fields{"error": err, "instance": da.instanceID})
		return false
	}
	return true
}

func (da *DeviceOwnership) monitorOwnership(ctx context.Context, id string, chnl chan int) {
	log.Debugw("start-device-monitoring", log.Fields{"id": id})
	op := "starting"
	exit := false
	ticker := time.NewTicker(time.Duration(da.reservationTimeout) / 3 * time.Second)
	for {
		select {
		case <-da.exitChannel:
			log.Debugw("closing-monitoring", log.Fields{"Id": id})
			exit = true
		case <-ticker.C:
			log.Debugw(fmt.Sprintf("%s-reservation", op), log.Fields{"Id": id})
		case <-chnl:
			log.Debugw("closing-device-monitoring", log.Fields{"Id": id})
			exit = true
		}
		if exit {
			log.Infow("exiting-device-monitoring", log.Fields{"Id": id})
			ticker.Stop()
			break
		}
		deviceOwned, ownedByMe := da.getOwnership(id)
		if deviceOwned && ownedByMe {
			// Device owned; renew reservation
			op = "renew"
			if da.renewReservation(ctx, id) {
				log.Debugw("reservation-renewed", log.Fields{"id": id, "instanceId": da.instanceID})
			} else {
				log.Debugw("reservation-not-renewed", log.Fields{"id": id, "instanceId": da.instanceID})
			}
		} else {
			// Device not owned or not owned by me; try to seize ownership
			op = "retry"
			if err := da.setOwnership(id, da.tryToReserveKey(ctx, id)); err != nil {
				log.Errorw("unexpected-error", log.Fields{"error": err})
			}
		}
	}
	log.Debugw("device-monitoring-stopped", log.Fields{"id": id})
}

func (da *DeviceOwnership) getOwnership(id string) (bool, bool) {
	da.deviceMapLock.RLock()
	defer da.deviceMapLock.RUnlock()
	if val, exist := da.deviceMap[id]; exist {
		return true, val.owned
	}
	return false, false
}

func (da *DeviceOwnership) setOwnership(id string, owner bool) error {
	da.deviceMapLock.Lock()
	defer da.deviceMapLock.Unlock()
	if _, exist := da.deviceMap[id]; exist {
		if da.deviceMap[id].owned != owner {
			log.Debugw("ownership-changed", log.Fields{"Id": id, "owner": owner})
		}
		da.deviceMap[id].owned = owner
		return nil
	}
	return status.Error(codes.NotFound, fmt.Sprintf("id-inexistent-%s", id))
}

// GetAllDeviceIdsOwnedByMe returns all the deviceIds (root device Ids) that is managed by this Core
func (da *DeviceOwnership) GetAllDeviceIdsOwnedByMe() []string {
	deviceIds := []string{}
	da.deviceMapLock.Lock()
	defer da.deviceMapLock.Unlock()
	for _, ownership := range da.deviceMap {
		if ownership.owned {
			deviceIds = append(deviceIds, ownership.id)
		}
	}
	return deviceIds
}

// OwnedByMe returns whether this Core instance active owns this device.   This function will automatically
// trigger the process to monitor the device and update the device ownership regularly.
func (da *DeviceOwnership) OwnedByMe(ctx context.Context, id interface{}) (bool, error) {
	// Retrieve the ownership key based on the id
	var ownershipKey string
	var err error
	var idStr string
	var cache bool
	if ownershipKey, idStr, cache, err = da.getOwnershipKey(ctx, id); err != nil {
		log.Warnw("no-ownershipkey", log.Fields{"error": err})
		return false, err
	}

	// Update the deviceToKey map, if not from cache
	if !cache {
		da.deviceToKeyMapLock.Lock()
		da.deviceToKeyMap[idStr] = ownershipKey
		da.deviceToKeyMapLock.Unlock()
	}

	// Add a lock to prevent creation of two separate monitoring routines for the same device. When a NB request for a
	// device not in memory is received this results in this function being called in rapid succession, once when
	// loading the device and once when handling the NB request.
	da.ownershipLock.Lock()
	defer da.ownershipLock.Unlock()

	deviceOwned, ownedByMe := da.getOwnership(ownershipKey)
	if deviceOwned {
		log.Debugw("ownership", log.Fields{"Id": ownershipKey, "owned": ownedByMe})
		return ownedByMe, nil
	}
	// Not owned by me or maybe nobody else.  Try to reserve it
	reservedByMe := da.tryToReserveKey(ctx, ownershipKey)
	myChnl := make(chan int)

	da.deviceMapLock.Lock()
	da.deviceMap[ownershipKey] = &ownership{
		id:    ownershipKey,
		owned: reservedByMe,
		chnl:  myChnl}
	da.deviceMapLock.Unlock()

	log.Debugw("set-new-ownership", log.Fields{"Id": ownershipKey, "owned": reservedByMe})
	go da.monitorOwnership(context.Background(), ownershipKey, myChnl)
	return reservedByMe, nil
}

//AbandonDevice must be invoked whenever a device is deleted from the Core
func (da *DeviceOwnership) AbandonDevice(id string) error {
	if id == "" {
		return status.Error(codes.FailedPrecondition, "id-nil")
	}
	da.deviceMapLock.Lock()
	defer da.deviceMapLock.Unlock()
	o, exist := da.deviceMap[id]
	if exist { // id is ownership key
		// Need to clean up all deviceToKeyMap entries using this device as key
		da.deviceToKeyMapLock.Lock()
		defer da.deviceToKeyMapLock.Unlock()
		for k, v := range da.deviceToKeyMap {
			if id == v {
				delete(da.deviceToKeyMap, k)
			}
		}
		// Remove the device reference from the deviceMap
		delete(da.deviceMap, id)

		// Stop the Go routine monitoring the device
		close(o.chnl)
		delete(da.deviceMap, id)
		log.Debugw("abandoning-device", log.Fields{"Id": id})
		return nil
	}
	// id is not ownership key
	da.deleteDeviceKey(id)
	return nil
}

//abandonAllDevices must be invoked whenever a device is deleted from the Core
func (da *DeviceOwnership) abandonAllDevices() {
	da.deviceMapLock.Lock()
	defer da.deviceMapLock.Unlock()
	da.deviceToKeyMapLock.Lock()
	defer da.deviceToKeyMapLock.Unlock()
	for k := range da.deviceToKeyMap {
		delete(da.deviceToKeyMap, k)
	}
	for _, val := range da.deviceMap {
		close(val.chnl)
	}
}

func (da *DeviceOwnership) deleteDeviceKey(id string) {
	da.deviceToKeyMapLock.Lock()
	defer da.deviceToKeyMapLock.Unlock()
	if _, exist := da.deviceToKeyMap[id]; exist {
		delete(da.deviceToKeyMap, id)
	}
}

// getOwnershipKey returns the ownership key that the id param uses.   Ownership key is the parent
// device Id of a child device or the rootdevice of a logical device.   This function also returns the
// id in string format of the id param via the ref output as well as if the data was retrieved from cache
func (da *DeviceOwnership) getOwnershipKey(ctx context.Context, id interface{}) (ownershipKey string, ref string, cached bool, err error) {

	if id == nil {
		return "", "", false, status.Error(codes.InvalidArgument, "nil-id")
	}
	da.deviceToKeyMapLock.RLock()
	defer da.deviceToKeyMapLock.RUnlock()
	var device *voltha.Device
	var lDevice *voltha.LogicalDevice
	// The id can either be a device Id or a logical device id.
	if dID, ok := id.(*utils.DeviceID); ok {
		// Use cache if present
		if val, exist := da.deviceToKeyMap[dID.ID]; exist {
			return val, dID.ID, true, nil
		}
		if device, _ = da.deviceMgr.GetDevice(ctx, dID.ID); device == nil {
			return "", dID.ID, false, status.Errorf(codes.NotFound, "id-absent-%s", dID)
		}
		if device.Root {
			return device.Id, dID.ID, false, nil
		}
		return device.ParentId, dID.ID, false, nil
	} else if ldID, ok := id.(*utils.LogicalDeviceID); ok {
		// Use cache if present
		if val, exist := da.deviceToKeyMap[ldID.ID]; exist {
			return val, ldID.ID, true, nil
		}
		if lDevice, _ = da.logicalDeviceMgr.getLogicalDevice(ctx, ldID.ID); lDevice == nil {
			return "", ldID.ID, false, status.Errorf(codes.NotFound, "id-absent-%s", dID)
		}
		return lDevice.RootDeviceId, ldID.ID, false, nil
	}
	return "", "", false, status.Error(codes.NotFound, fmt.Sprintf("id-%v", id))
}
