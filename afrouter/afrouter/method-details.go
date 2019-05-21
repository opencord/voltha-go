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

package afrouter

import (
	"github.com/opencord/voltha-go/common/log"
	"regexp"
)

type methodDetails struct {
	all     string
	pkg     string
	service string
	method  string
}

// The compiled regex to extract the package/service/method
var mthdSlicer = regexp.MustCompile(`^/([a-zA-Z][a-zA-Z0-9]+)\.([a-zA-Z][a-zA-Z0-9]+)/([a-zA-Z][a-zA-Z0-9]+)`)

func newMethodDetails(fullMethodName string) methodDetails {
	// The full method name is structured as follows:
	// <package name>.<service>/<method>
	mthdSlice := mthdSlicer.FindStringSubmatch(fullMethodName)
	if mthdSlice == nil {
		log.Errorf("Faled to slice full method %s, result: %v", fullMethodName, mthdSlice)
	} else {
		log.Debugf("Sliced full method %s: %v", fullMethodName, mthdSlice)
	}
	return methodDetails{
		all:     mthdSlice[0],
		pkg:     mthdSlice[1],
		service: mthdSlice[2],
		method:  mthdSlice[3],
	}
}
