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
	if GetInstance().ChildrenFieldsCache != nil || len(GetInstance().ChildrenFieldsCache) > 0 {
		t.Errorf("GetInstance().ChildrenFieldsCache should be empty: %+v\n", GetInstance().ChildrenFieldsCache)
	}
	t.Logf("GetInstance().ChildrenFieldsCache is empty - %+v\n", GetInstance().ChildrenFieldsCache)
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
	if GetInstance().ChildrenFieldsCache == nil || len(GetInstance().ChildrenFieldsCache) != 1 {
		t.Errorf("GetInstance().ChildrenFieldsCache should have one entry: %+v\n", GetInstance().ChildrenFieldsCache)
	}
	t.Logf("GetInstance().ChildrenFieldsCache has one entry: %+v\n", GetInstance().ChildrenFieldsCache)
}

/*

 */
func Test_ChildType_04_ChildrenFieldsCache_Keys(t *testing.T) {
	for k := range GetInstance().ChildrenFieldsCache {
		t.Logf("GetInstance().ChildrenFieldsCache Key:%+v\n", k)
	}
}
