/*
 * Copyright 2019-2024 Open Networking Foundation (ONF) and the ONF Contributors
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
package test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

var retryInterval = 50 * time.Millisecond

type isLogicalDeviceConditionSatisfied func(ld *voltha.LogicalDevice) bool
type isLogicalDevicePortsConditionSatisfied func(ports []*voltha.LogicalPort) bool
type isDeviceConditionSatisfied func(ld *voltha.Device) bool
type isDevicePortsConditionSatisfied func(ports *voltha.Ports) bool
type isDevicesConditionSatisfied func(ds *voltha.Devices) bool
type isLogicalDevicesConditionSatisfied func(lds *voltha.LogicalDevices) bool
type isConditionSatisfied func() bool

func getContext() context.Context {
	return context.Background()
}

func setRetryInterval(interval time.Duration) {
	retryInterval = interval
}

func waitUntilDeviceReadiness(deviceID string,
	timeout time.Duration,
	verificationFunction isDeviceConditionSatisfied,
	nbi voltha.VolthaServiceClient) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			device, _ := nbi.GetDevice(getContext(), &common.ID{Id: deviceID})
			if verificationFunction(device) {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("expected-states-not-reached-for-device%s", deviceID)
	}
}

func waitUntilDevicePortsReadiness(deviceID string,
	timeout time.Duration,
	verificationFunction isDevicePortsConditionSatisfied,
	nbi voltha.VolthaServiceClient) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			ports, _ := nbi.ListDevicePorts(getContext(), &common.ID{Id: deviceID})
			if verificationFunction(ports) {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("expected-states-not-reached-for-device%s", deviceID)
	}
}

func waitUntilLogicalDeviceReadiness(oltDeviceID string,
	timeout time.Duration,
	nbi voltha.VolthaServiceClient,
	verificationFunction isLogicalDeviceConditionSatisfied,
) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			// Get the logical device from the olt device
			d, _ := nbi.GetDevice(getContext(), &common.ID{Id: oltDeviceID})
			if d != nil && d.ParentId != "" {
				ld, _ := nbi.GetLogicalDevice(getContext(), &common.ID{Id: d.ParentId})
				if verificationFunction(ld) {
					ch <- 1
					break
				}
				if done {
					break
				}
			} else if d != nil && d.ParentId == "" { // case where logical device deleted
				if verificationFunction(nil) {
					ch <- 1
					break
				}
				if done {
					break
				}
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-for-logical-device-readiness%s", oltDeviceID)
	}
}

func waitUntilLogicalDevicePortsReadiness(oltDeviceID string,
	timeout time.Duration,
	nbi voltha.VolthaServiceClient,
	verificationFunction isLogicalDevicePortsConditionSatisfied,
) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			// Get the logical device from the olt device
			d, _ := nbi.GetDevice(getContext(), &common.ID{Id: oltDeviceID})
			if d != nil && d.ParentId != "" {
				ports, err := nbi.ListLogicalDevicePorts(getContext(), &common.ID{Id: d.ParentId})
				if err == nil && verificationFunction(ports.Items) {
					ch <- 1
					break
				}
				if done {
					break
				}
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-for-logical-device-readiness%s", oltDeviceID)
	}
}

func waitUntilConditionForDevices(timeout time.Duration, nbi voltha.VolthaServiceClient, verificationFunction isDevicesConditionSatisfied) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			devices, _ := nbi.ListDevices(getContext(), &empty.Empty{})
			if verificationFunction(devices) {
				ch <- 1
				break
			}
			if done {
				break
			}

			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-devices")
	}
}

func waitUntilConditionForLogicalDevices(timeout time.Duration, nbi voltha.VolthaServiceClient, verificationFunction isLogicalDevicesConditionSatisfied) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			lDevices, _ := nbi.ListLogicalDevices(getContext(), &empty.Empty{})
			if verificationFunction(lDevices) {
				ch <- 1
				break
			}
			if done {
				break
			}

			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-logical-devices")
	}
}

func waitUntilCondition(timeout time.Duration, verificationFunction isConditionSatisfied) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			if verificationFunction() {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-for-condition")
	}
}

func waitUntilDeviceIsRemoved(timeout time.Duration, nbi voltha.VolthaServiceClient, deviceID string) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			_, err := nbi.GetDevice(getContext(), &common.ID{Id: deviceID})
			if err != nil && strings.Contains(err.Error(), "NotFound") {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-for-condition")
	}
}

func cleanUpCreatedDevice(timeout time.Duration, nbi voltha.VolthaServiceClient, deviceID string) error {
	logger.Warnw(context.Background(), "cleanUpCreatedDevice", log.Fields{"device-id": deviceID})
	ch := make(chan int, 1)
	done := false
	go func() {
		// Force Remove the device - use a loop in case the initial delete fails
		for {
			logger.Debugw(context.Background(), "sending delete force ", log.Fields{"device-id": deviceID})
			var err error
			if _, err = nbi.ForceDeleteDevice(getContext(), &common.ID{Id: deviceID}); err != nil {
				logger.Debugw(context.Background(), "delete failed", log.Fields{"device-id": deviceID, "error": err})
				if strings.Contains(err.Error(), "NotFound") {
					logger.Debugw(context.Background(), "delete not found", log.Fields{"device-id": deviceID, "error": err})
					// ch <- 1
					break
				}
				time.Sleep(retryInterval)
				continue
			}
			logger.Debugw(context.Background(), "delete force no error", log.Fields{"device-id": deviceID, "error": err})
			break
		}
		logger.Debugw(context.Background(), "delete sent", log.Fields{"device-id": deviceID})
		for {
			_, err := nbi.GetDevice(getContext(), &common.ID{Id: deviceID})
			if err != nil && strings.Contains(err.Error(), "NotFound") {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-devices-cleanup")
	}
}

func cleanUpCreatedDevices(timeout time.Duration, nbi voltha.VolthaServiceClient, parentDeviceID string) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		// Force Remove the device - use a loop in case the initial delete fails
		for {
			if _, err := nbi.ForceDeleteDevice(getContext(), &common.ID{Id: parentDeviceID}); err != nil {
				if strings.Contains(err.Error(), "NotFound") {
					ch <- 1
					break
				}
				time.Sleep(retryInterval)
				continue
			}
			break
		}
		for {
			devices, _ := nbi.ListDevices(getContext(), &empty.Empty{})
			removed := devices == nil || len(devices.Items) == 0
			if !removed {
				removed = true
				for _, d := range devices.Items {
					if (d.Root && d.Id == parentDeviceID) || (!d.Root && d.ParentId == parentDeviceID) {
						removed = false
						break
					}
				}
			}
			if removed {
				ch <- 1
				break
			}
			if done {
				break
			}
			time.Sleep(retryInterval)
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-devices-cleanup")
	}
}

func cleanUpDevices(timeout time.Duration, nbi voltha.VolthaServiceClient, parentDeviceID string, verifyParentDeletionOnly bool) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		// Send a force delete to the parent device
		for {
			_, err := nbi.ForceDeleteDevice(getContext(), &common.ID{Id: parentDeviceID})
			if err == nil || strings.Contains(err.Error(), "NotFound") {
				break
			}
			time.Sleep(retryInterval)
			if done {
				return
			}
		}
		var err error
		for {
			if verifyParentDeletionOnly {
				_, err = nbi.GetDevice(getContext(), &common.ID{Id: parentDeviceID})
				if err != nil && strings.Contains(err.Error(), "NotFound") {
					ch <- 1
					break
				}
				time.Sleep(retryInterval)
				if done {
					return
				}
				continue
			}
			// verifyParentDeletionOnly is False => check children as well
			devices, _ := nbi.ListDevices(getContext(), &empty.Empty{})
			removed := devices == nil || len(devices.Items) == 0
			if !removed {
				removed = true
				for _, d := range devices.Items {
					if (d.Root && d.Id == parentDeviceID) || (!d.Root && d.ParentId == parentDeviceID) {
						removed = false
						break
					}
				}
			}
			if removed {
				ch <- 1
				break
			}
			time.Sleep(retryInterval)
			if done {
				break
			}
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-timer.C:
		done = true
		return fmt.Errorf("timeout-waiting-devices-cleanup")
	}
}

type ChangedEventListener struct {
	eventSubscriber   chan chan *ofp.ChangeEvent
	eventUnSubscriber chan chan *ofp.ChangeEvent
}

func NewChangedEventListener(bufferSize int) *ChangedEventListener {
	return &ChangedEventListener{
		eventSubscriber:   make(chan chan *ofp.ChangeEvent, bufferSize),
		eventUnSubscriber: make(chan chan *ofp.ChangeEvent, bufferSize),
	}
}

func (cel *ChangedEventListener) Start(ctx context.Context, coreEventsCh chan *ofp.ChangeEvent) {
	subs := map[chan *ofp.ChangeEvent]struct{}{}
	var subsLock sync.RWMutex
	for {
		select {
		case <-ctx.Done():
			logger.Debug(ctx, "closing-change-event-listener")
			subsLock.RLock()
			for msgCh := range subs {
				close(msgCh)
			}
			subsLock.RUnlock()
			return
		case eventCh := <-cel.eventSubscriber:
			subsLock.Lock()
			subs[eventCh] = struct{}{}
			subsLock.Unlock()
		case eventCh := <-cel.eventUnSubscriber:
			subsLock.Lock()
			close(eventCh)
			delete(subs, eventCh)
			subsLock.Unlock()
		case event := <-coreEventsCh:
			subsLock.RLock()
			for subscriber := range subs {
				select {
				case subscriber <- event:
				default:
				}
			}
			subsLock.RUnlock()
		}
	}
}

func (cel *ChangedEventListener) Subscribe(bufferSize int) chan *ofp.ChangeEvent {
	eventCh := make(chan *ofp.ChangeEvent, bufferSize)
	cel.eventSubscriber <- eventCh
	return eventCh
}

func (cel *ChangedEventListener) Unsubscribe(eventCh chan *ofp.ChangeEvent) {
	cel.eventUnSubscriber <- eventCh
}
