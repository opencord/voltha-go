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
package common

import (
	"fmt"
	"math/rand"
	"time"
)

//GetRandomSerialNumber returns a serial number formatted as "HOST:PORT"
func GetRandomSerialNumber() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%d.%d.%d.%d:%d",
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(9000)+1000,
	)
}

//GetRandomMacAddress returns a random mac address
func GetRandomMacAddress() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
	)
}
