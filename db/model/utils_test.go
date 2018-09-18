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
	"github.com/opencord/voltha-go/protos/voltha"
	"reflect"
	"testing"
)

func Test_Utils_Clone(t *testing.T) {
	a := &voltha.Device{
		Id:              "abcde",
		FirmwareVersion: "someversion",
	}
	b := &voltha.Device{}
	Clone(reflect.ValueOf(a).Interface(), b)
	t.Logf("A: %+v, B: %+v", a, b)
	b.Id = "12345"
	t.Logf("A: %+v, B: %+v", a, b)

	var c *voltha.Device
	c = Clone2(a).(*voltha.Device)
	t.Logf("A: %+v, C: %+v", a, c)
	c.Id = "12345"
	t.Logf("A: %+v, C: %+v", a, c)
}
