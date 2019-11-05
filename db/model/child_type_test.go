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
	"github.com/opencord/voltha-protos/go/voltha"
	"reflect"
	"testing"
)

// Dissect a proto message by extracting all the children fields
func TestChildType_01_Device_Proto_ChildrenFields(t *testing.T) {
	var cls *voltha.Device

	t.Logf("Extracting children fields from proto type: %s", reflect.TypeOf(cls))
	names := ChildrenFields(cls)
	t.Logf("Extracting children field names: %+v", names)

	expectedKeys := []string{"ports", "flows", "flow_groups", "image_downloads", "pm_configs"}
	for _, key := range expectedKeys {
		if _, exists := names[key]; !exists {
			t.Errorf("Missing key:%s from class type:%s", key, reflect.TypeOf(cls))
		}
	}
}

// Verify that the cache contains an entry for types on which ChildrenFields was performed
func TestChildType_02_Cache_Keys(t *testing.T) {
	if _, exists := getChildTypes().Cache[reflect.TypeOf(&voltha.Device{}).String()]; !exists {
		t.Errorf("getChildTypeCache().Cache should have an entry of type: %+v\n", reflect.TypeOf(&voltha.Device{}).String())
	}
	for k := range getChildTypes().Cache {
		t.Logf("getChildTypeCache().Cache Key:%+v\n", k)
	}
}
