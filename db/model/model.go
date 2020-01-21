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
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

func init() {
	if _, err := log.AddPackage(log.JSON, log.InfoLevel, log.Fields{"instanceId": "DB_MODEL"}); err != nil {
		log.Errorw("Unable to register package to the log map", log.Fields{"error": err})
	}
}

const (
	// DataRefreshPeriod is period to determine when data requires a refresh (in milliseconds)
	// TODO: make this configurable?
	DataRefreshPeriod int64 = 5000

	// RequestTimestamp attribute used to store a timestamp in the context object
	RequestTimestamp = "request-timestamp"

	// ReservationTTL is time limit for a KV path reservation (in seconds)
	ReservationTTL int64 = 180
)
