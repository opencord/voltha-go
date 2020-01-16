/*
 * Portions copyright 2019-present Open Networking Foundation
 * Original copyright 2019-present Ciena Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the"github.com/stretchr/testify/assert" "License");
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
package common

/*
 * This file has common code that is imported for all test cases, but
 * is not built into production binaries.
 */

import (
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

const (
	/*
	 * This sets the LogLevel of the Voltha logger. It's pinned to FatalLevel here, as we
	 * generally don't want to see logger output, even when running go test in verbose
	 * mode. Even "Error" level messages are expected to be output by some unit tests.
	 *
	 * If you are developing a unit test, and experiencing problems or wish additional
	 * debugging from Voltha, then changing this constant to log.DebugLevel may be
	 * useful.
	 */

	VOLTHA_LOGLEVEL = log.FatalLevel
)

// Unit test initialization. This init() function will be run once for all unit tests in afrouter
func init() {
	// Logger must be configured or bad things happen
	_, err := log.SetDefaultLogger(log.JSON, VOLTHA_LOGLEVEL, log.Fields{"instanceId": 1})
	if err != nil {
		panic(err)
	}

	_, err = log.AddPackage(log.JSON, VOLTHA_LOGLEVEL, nil)
	if err != nil {
		panic(err)
	}
}
