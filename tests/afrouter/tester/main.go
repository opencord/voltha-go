// +build integration

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
// The template for the tester.
// This template is filled in by the
// test driver based on the configuration.

package main

import (
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

func main() {
	// Setup logging
	/*
		if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
			log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
		}
	*/

	defer log.CleanUp()

	log.Info("Template runs!")
}
