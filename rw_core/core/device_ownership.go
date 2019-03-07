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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-protos/go/voltha"
	"github.com/opencord/voltha-go/rw_core/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
	"time"
)

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
}

type ownership struct {
	id    string
	owned bool
	chnl  chan int
}

type DeviceOwnership struct {
	instanceId         string
	exitChannel        chan int
	kvClient           kvstore.Client
	reservationTimeout int64 // Duration in seconds
	ownershipPrefix    string
	deviceMgr          *DeviceManager
	logicalDeviceMgr   *LogicalDeviceManager
	deviceMap          map[string]*ownership
	deviceMapLock      *sync.RWMutex
	deviceToKeyMap     map[string]string
	deviceToKeyMapLock *sync.RWMutex
}

func NewDeviceOwnership(id string, kvClient kvstore.Client, deviceMgr *DeviceManager, logicalDeviceMgr *LogicalDeviceManager, ownershipPrefix string, reservationTimeout int64) *DeviceOwnership {
	var deviceOwnership DeviceOwnership
	deviceOwnership.instanceId = id
	deviceOwnership.exitChannel = make(chan int, 1)
	deviceOwnership.kvClient = kvClient
	deviceOwnership.deviceMgr = deviceMgr
	deviceOwnership.logicalDeviceMgr = logicalDeviceMgr
	deviceOwnership.ownershipPrefix = ownershipPrefix
	deviceOwnership.reservationTimeout = reservationTimeout
	deviceOwnership.deviceMap = make(map[string]*ownership)
	deviceOwnership.deviceMapLock = &sync.RWMutex{}
	deviceOwnership.deviceToKeyMap = make(map[string]string)
	deviceOwnership.deviceToKeyMapLock = &sync.RWMutex{}
	return &deviceOwnership
}

func (da *DeviceOwnership) Start(ctx context.Context) {
	log.Info("starting-deviceOwnership", log.Fields{"instanceId": da.instanceId})
	log.Info("deviceOwnership-started")
}

func (da *DeviceOwnership) Stop(ctx context.Context) {
	log.Info("stopping-deviceOwnership")
	da.exitChannel <- 1
	// Need to flush all device reservations
	da.abandonAllDevices()
	log.Info("deviceOwnership-stopped")
}

func (da *DeviceOwnership) tryToReserveKey(id string) bool {
	var currOwner string
	//Try to reserve the key
	kvKey := fmt.Sprintf("%s_%s", da.ownershipPrefix, id)
	value, err := da.kvClient.Reserve(kvKey, da.instanceId, da.reservationTimeout)
	if err != nil {
		log.Errorw("error", log.Fields{"error": err, "id": id, "instanceId": da.instanceId})
	}
	if value != nil {
		if currOwner, err = kvstore.ToString(value); err != nil {
			log.Error("unexpected-owner-type")
		}
		return currOwner == da.instanceId
	}
	return false
}

func (da *DeviceOwnership) renewReservation(id string) bool {
	// Try to reserve the key
	kvKey := fmt.Sprintf("%s_%s", da.ownershipPrefix, id)
	if err := da.kvClient.RenewReservation(kvKey); err != nil {
		log.Errorw("reservation-renewal-error", log.Fields{"error": err, "instance": da.instanceId})
		return false
	}
	return true
}

func (da *DeviceOwnership) MonitorOwnership(id string, chnl chan int) {
	op := "starting"
	exit := false
	ticker := time.NewTicker(time.Duration(da.reservationTimeout) / 3 * time.Second)
	for {
		select {
		case <-da.exitChannel:
			log.Infow("closing-monitoring", log.Fields{"Id": id})
			exit = true
		case <-ticker.C:
			log.Debugw(fmt.Sprintf("%s-reservation", op), log.Fields{"Id": id})
		case <-chnl:
			log.Infow("closing-device-monitoring", log.Fields{"Id": id})
			exit = true
		}
		if exit {
			ticker.Stop()
			break
		}
		deviceOwned, ownedByMe := da.getOwnership(id)
		if deviceOwned && ownedByMe {
			// Device owned; renew reservation
			op = "renew"
			if da.renewReservation(id) {
				log.Debugw("reservation-renewed", log.Fields{"id": id, "instanceId": da.instanceId})
			} else {
				log.Debugw("reservation-not-renewed", log.Fields{"id": id, "instanceId": da.instanceId})
			}
		} else {
			// Device not owned or not owned by me; try to seize ownership
			op = "retry"
			if err := da.setOwnership(id, da.tryToReserveKey(id)); err != nil {
				log.Errorw("unexpected-error", log.Fields{"error": err})
			}
		}
	}
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

// OwnedByMe returns where this Core instance active owns this device.   This function will automatically
// trigger the process to monitor the device and update the device ownership regularly.
func (da *DeviceOwnership) OwnedByMe(id interface{}) bool {
	// Retrieve the ownership key based on the id
	var ownershipKey string
	var err error
	if ownershipKey, err = da.getOwnershipKey(id); err != nil {
		log.Warnw("no-ownershipkey", log.Fields{"error": err})
		return false
	}

	deviceOwned, ownedByMe := da.getOwnership(ownershipKey)
	if deviceOwned {
		return ownedByMe
	}
	// Not owned by me or maybe anybody else.  Try to reserve it
	reservedByMe := da.tryToReserveKey(ownershipKey)
	myChnl := make(chan int)
	da.deviceMap[ownershipKey] = &ownership{id: ownershipKey, owned: reservedByMe, chnl: myChnl}
	log.Debugw("set-new-ownership", log.Fields{"Id": ownershipKey, "owned": reservedByMe})
	go da.MonitorOwnership(ownershipKey, myChnl)
	return reservedByMe
}

//AbandonDevice must be invoked whenever a device is deleted from the Core
func (da *DeviceOwnership) AbandonDevice(id string) error {
	da.deviceMapLock.Lock()
	defer da.deviceMapLock.Unlock()
	if o, exist := da.deviceMap[id]; exist {
		// Stop the Go routine monitoring the device
		close(o.chnl)
		delete(da.deviceMap, id)
		log.Debugw("abandoning-device", log.Fields{"Id": id})
		return nil
	}
	return status.Error(codes.NotFound, fmt.Sprintf("id-inexistent-%s", id))
}

//abandonAllDevices must be invoked whenever a device is deleted from the Core
func (da *DeviceOwnership) abandonAllDevices() {
	da.deviceMapLock.Lock()
	defer da.deviceMapLock.Unlock()
	for _, val := range da.deviceMap {
		close(val.chnl)
	}
}

func (da *DeviceOwnership) getDeviceKey(id string) (string, error) {
	da.deviceToKeyMapLock.RLock()
	defer da.deviceToKeyMapLock.RUnlock()
	if val, exist := da.deviceToKeyMap[id]; exist {
		return val, nil
	}
	return "", status.Error(codes.NotFound, fmt.Sprintf("not-present-%s", id))
}

func (da *DeviceOwnership) updateDeviceKey(id string, key string) error {
	da.deviceToKeyMapLock.Lock()
	defer da.deviceToKeyMapLock.Unlock()
	if _, exist := da.deviceToKeyMap[id]; exist {
		return status.Error(codes.AlreadyExists, fmt.Sprintf("already-present-%s", id))
	}
	da.deviceToKeyMap[id] = key
	return nil
}

func (da *DeviceOwnership) getOwnershipKey(id interface{}) (string, error) {
	if id == nil {
		return "", status.Error(codes.InvalidArgument, "nil-id")
	}
	var device *voltha.Device
	var lDevice *voltha.LogicalDevice
	// The id can either be a device Id or a logical device id.
	if dId, ok := id.(*utils.DeviceID); ok {
		// Use cache if present
		if val, err := da.getDeviceKey(dId.Id); err == nil {
			return val, nil
		}
		if device, _ = da.deviceMgr.GetDevice(dId.Id); device == nil {
			return "", status.Error(codes.NotFound, fmt.Sprintf("id-absent-%s", dId))
		}
		if device.Root {
			if err := da.updateDeviceKey(dId.Id, device.Id); err != nil {
				log.Warnw("Error-updating-cache", log.Fields{"id": dId.Id, "key": device.Id, "error": err})
			}
			return device.Id, nil
		} else {
			if err := da.updateDeviceKey(dId.Id, device.ParentId); err != nil {
				log.Warnw("Error-updating-cache", log.Fields{"id": dId.Id, "key": device.ParentId, "error": err})
			}
			return device.ParentId, nil
		}
	} else if ldId, ok := id.(*utils.LogicalDeviceID); ok {
		// Use cache if present
		if val, err := da.getDeviceKey(ldId.Id); err == nil {
			return val, nil
		}
		if lDevice, _ = da.logicalDeviceMgr.getLogicalDevice(ldId.Id); lDevice == nil {
			return "", status.Error(codes.NotFound, fmt.Sprintf("id-absent-%s", ldId))
		}
		if err := da.updateDeviceKey(ldId.Id, lDevice.RootDeviceId); err != nil {
			log.Warnw("Error-updating-cache", log.Fields{"id": ldId.Id, "key": lDevice.RootDeviceId, "error": err})
		}
		return lDevice.RootDeviceId, nil
	}
	return "", status.Error(codes.NotFound, fmt.Sprintf("id-%s", id))
}
