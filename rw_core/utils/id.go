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

package utils

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"

	"github.com/google/uuid"
)

// CreateDeviceID produces a device ID. The device ID is a UUID
func CreateDeviceID() string {
	return uuid.New().String()
}

// CreateLogicalDeviceID produces a logical device ID. The logical device ID is a UUID
func CreateLogicalDeviceID() string {
	return uuid.New().String()
}

// CreateLogicalPortID produces a random port ID for a logical device.
func CreateLogicalPortID() uint32 {
	//	A logical port is a uint32
	return rand.Uint32()
}

// CreateDataPathID creates uint64 pathid from string pathid
func CreateDataPathID(idInHexString string) (uint64, error) {
	if idInHexString == "" {
		return 0, errors.New("id-empty")
	}
	// First prepend 0x to the string
	newID := fmt.Sprintf("0x%s", idInHexString)
	d, err := strconv.ParseUint(newID, 0, 64)
	if err != nil {
		return 0, err
	}
	return d, nil
}
