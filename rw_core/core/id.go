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
package core

import (
	"crypto/rand"
	"encoding/hex"
	m "math/rand"
)

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func CreateDeviceId() string {
	// deviceId is 16 hex long - lower 12 hex is the device id.  For now just generate the 12 hex
	val, _ := randomHex(12)
	return val
}

func CreateLogicalDeviceId() string {
	// Not used - logical device ID represents the MAC address of the OLT
	// logical device id is 16 hex long - lower 12 hex is the logical device id.  For now just generate the 12 hex
	val, _ := randomHex(12)
	return val
}

func createLogicalPortId() uint32 {
	//	A logical port is a uint32
	return m.Uint32()
}
