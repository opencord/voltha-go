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
package model

import (
	"github.com/opencord/voltha-go/common/log"
)

func init() {
	log.AddPackage(log.JSON, log.InfoLevel, log.Fields{"instanceId": "DB_MODEL"})
	defer log.CleanUp()
}

const (
	// period to determine when data requires a refresh (in milliseconds)
	// TODO: make this configurable?
	DataRefreshPeriod int64 = 5000

	// Attribute used to store a timestamp in the context object
	RequestTimestamp = "request-timestamp"

	// Time limit for a KV path reservation (in seconds)
	ReservationTTL int64 = 180
)
