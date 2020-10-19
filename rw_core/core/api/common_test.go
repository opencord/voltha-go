/*
 * Copyright 2019-present Open Networking Foundation
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
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/metadata"
)

const (
	volthaSerialNumberKey = "voltha_serial_number"
	retryInterval         = 50 * time.Millisecond
)

var (
	coreInCompeteMode bool
)

type isLogicalDeviceConditionSatisfied func(ld *voltha.LogicalDevice) bool
type isLogicalDevicePortsConditionSatisfied func(ports []*voltha.LogicalPort) bool
type isDeviceConditionSatisfied func(ld *voltha.Device) bool
type isDevicePortsConditionSatisfied func(ports *voltha.Ports) bool
type isDevicesConditionSatisfied func(ds *voltha.Devices) bool
type isLogicalDevicesConditionSatisfied func(lds *voltha.LogicalDevices) bool
type isConditionSatisfied func() bool

func init() {
	//Default mode is two rw-core running in a pair of competing cores
	coreInCompeteMode = true
}

func setCoreCompeteMode(mode bool) {
	coreInCompeteMode = mode
}

func getContext() context.Context {
	if coreInCompeteMode {
		return metadata.NewIncomingContext(context.Background(), metadata.Pairs(volthaSerialNumberKey, uuid.New().String()))
	}
	return context.Background()
}

func waitUntilDeviceReadiness(deviceID string,
	timeout time.Duration,
	verificationFunction isDeviceConditionSatisfied,
	nbi *NBIHandler) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			device, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: deviceID})
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
	nbi *NBIHandler) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			ports, _ := nbi.ListDevicePorts(getContext(), &voltha.ID{Id: deviceID})
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
	nbi *NBIHandler,
	verificationFunction isLogicalDeviceConditionSatisfied,
) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			// Get the logical device from the olt device
			d, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDeviceID})
			if d != nil && d.ParentId != "" {
				ld, _ := nbi.GetLogicalDevice(getContext(), &voltha.ID{Id: d.ParentId})
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
	nbi *NBIHandler,
	verificationFunction isLogicalDevicePortsConditionSatisfied,
) error {
	ch := make(chan int, 1)
	done := false
	go func() {
		for {
			// Get the logical device from the olt device
			d, _ := nbi.GetDevice(getContext(), &voltha.ID{Id: oltDeviceID})
			if d != nil && d.ParentId != "" {
				ports, err := nbi.ListLogicalDevicePorts(getContext(), &voltha.ID{Id: d.ParentId})
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

func waitUntilConditionForDevices(timeout time.Duration, nbi *NBIHandler, verificationFunction isDevicesConditionSatisfied) error {
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

func waitUntilConditionForLogicalDevices(timeout time.Duration, nbi *NBIHandler, verificationFunction isLogicalDevicesConditionSatisfied) error {
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

func waitUntilCondition(timeout time.Duration, nbi *NBIHandler, verificationFunction isConditionSatisfied) error {
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
