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
	"testing"
)

/*

 */
func Test_ChildType_01_CacheIsEmpty(t *testing.T) {
	if getChildTypeCache().Cache != nil || len(getChildTypeCache().Cache) > 0 {
		t.Errorf("getChildTypeCache().Cache should be empty: %+v\n", getChildTypeCache().Cache)
	}
	t.Logf("getChildTypeCache().Cache is empty - %+v\n", getChildTypeCache().Cache)
}

/*

 */
func Test_ChildType_02_Device_Proto_ChildrenFields(t *testing.T) {

	var cls *voltha.Device
	//cls = &voltha.Device{Id:"testing-Config-rev-id"}

	names := ChildrenFields(cls)

	//tst := reflect.ValueOf(cls).Elem().FieldByName("ImageDownloads")
	//
	//t.Logf("############ Field by name : %+v\n", reflect.TypeOf(tst.Interface()))

	if names == nil || len(names) == 0 {
		t.Errorf("ChildrenFields failed to return names: %+v\n", names)
	}
	t.Logf("ChildrenFields returned names: %+v\n", names)
}

/*

 */
func Test_ChildType_03_CacheHasOneEntry(t *testing.T) {
	if getChildTypeCache().Cache == nil || len(getChildTypeCache().Cache) != 1 {
		t.Errorf("getChildTypeCache().Cache should have one entry: %+v\n", getChildTypeCache().Cache)
	}
	t.Logf("getChildTypeCache().Cache has one entry: %+v\n", getChildTypeCache().Cache)
}

/*

 */
func Test_ChildType_04_Cache_Keys(t *testing.T) {
	for k := range getChildTypeCache().Cache {
		t.Logf("getChildTypeCache().Cache Key:%+v\n", k)
	}
}
