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
package core

import (
	"context"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

func makeModelProxyManagerObj() *ModelProxyManager {
	cdRoot := model.NewRoot(&voltha.Voltha{}, nil)
	cdProxy := cdRoot.CreateProxy(context.Background(), "/", false)
	mpMgr := newModelProxyManager(cdProxy)
	return mpMgr
}

func TestNewModelProxyManager(t *testing.T) {
	type args struct {
		clusterDataProxy *model.Proxy
	}
	tests := []struct {
		name string
		args args
		want *ModelProxyManager
	}{
		{"NewModelProxyManager", args{&model.Proxy{}}, &ModelProxyManager{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newModelProxyManager(tt.args.clusterDataProxy); reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
				t.Errorf("newModelProxy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetVoltha(t *testing.T) {
	wantResult := &voltha.Voltha{}
	mpMgr := makeModelProxyManagerObj()
	result, err := mpMgr.GetVoltha(context.Background())
	if reflect.TypeOf(result) != reflect.TypeOf(wantResult) {
		t.Errorf("GetVoltha() = %v, want %v", result, wantResult)
	}
	assert.NotNil(t, result)
	assert.Nil(t, err)
}

func TestListCoreInstances(t *testing.T) {
	wantResult := &voltha.CoreInstances{}
	mpMgr := makeModelProxyManagerObj()
	result, err := mpMgr.ListCoreInstances(context.Background())
	if reflect.TypeOf(result) != reflect.TypeOf(wantResult) {
		t.Errorf("ListCoreInstances() = %v, want %v", result, wantResult)
	}
	assert.Nil(t, result.Items)
	assert.NotNil(t, err)
}

func TestGetCoreInstance(t *testing.T) {
	wantResult := &voltha.CoreInstance{}
	mpMgr := makeModelProxyManagerObj()
	result, err := mpMgr.GetCoreInstance(context.Background(), "id")
	if reflect.TypeOf(result) != reflect.TypeOf(wantResult) {
		t.Errorf("GetCoreInstance() = %v, want %v", result, wantResult)
	}
	assert.NotNil(t, err)
}

func TestListAdapters(t *testing.T) {
	wantResult := &voltha.Adapters{
		Items: []*voltha.Adapter{
			{
				Id: "id",
			},
		},
	}

	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.ListAdapters(context.Background())
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("ListAdapters() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0.Items)
	assert.Nil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/adapters", &voltha.Adapter{Id: "id"}, ""); added == nil {
		t.Error("Failed to add adapter")
	}
	result1, err1 := mpMgr.ListAdapters(context.Background())
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListAdapters() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1.Items)
	assert.Nil(t, err1)
	assert.Equal(t, wantResult, result1)
}

func TestListDeviceTypes(t *testing.T) {
	wantResult := &voltha.DeviceTypes{
		Items: []*voltha.DeviceType{
			{
				Id: "id",
			},
		},
	}

	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.ListDeviceTypes(context.Background())
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("ListDeviceTypes() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0.Items)
	assert.Nil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/device_types", &voltha.DeviceType{Id: "id"}, ""); added == nil {
		t.Error("Failed to add device type")
	}
	result1, err1 := mpMgr.ListDeviceTypes(context.Background())
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListDeviceTypes() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1.Items)
	assert.Nil(t, err1)
	assert.Equal(t, wantResult, result1)
}

func TestGetDeviceType(t *testing.T) {
	wantResult := &voltha.DeviceType{}
	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.GetDeviceType(context.Background(), "id")
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("GetDeviceType() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0)
	assert.NotNil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/device_types", &voltha.DeviceType{Id: "id"}, ""); added == nil {
		t.Error("Failed to add device type")
	}
	result1, err1 := mpMgr.GetDeviceType(context.Background(), "id")
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("GetDeviceType() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1)
	assert.Nil(t, err1)
	assert.Equal(t, "id", result1.Id)
}

func TestListDeviceGroups(t *testing.T) {
	wantResult := &voltha.DeviceGroups{
		Items: []*voltha.DeviceGroup{
			{
				Id: "id",
			},
		},
	}

	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.ListDeviceGroups(context.Background())
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("ListDeviceGroups() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0.Items)
	assert.Nil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/device_groups", &voltha.DeviceGroup{Id: "id"}, ""); added == nil {
		t.Error("Failed to add device group")
	}
	result1, err1 := mpMgr.ListDeviceGroups(context.Background())
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListDeviceGroups() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1.Items)
	assert.Nil(t, err1)
	assert.Equal(t, wantResult, result1)
}

func TestGetDeviceGroup(t *testing.T) {
	wantResult := &voltha.DeviceGroup{}
	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.GetDeviceGroup(context.Background(), "id")
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("GetDeviceGroup() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0)
	assert.NotNil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/device_groups", &voltha.DeviceGroup{Id: "id"}, ""); added == nil {
		t.Error("Failed to add device group")
	}
	result1, err1 := mpMgr.GetDeviceGroup(context.Background(), "id")
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("GetDeviceGroup() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1)
	assert.Nil(t, err1)
	assert.Equal(t, "id", result1.Id)
}

func TestListEventFilters(t *testing.T) {
	wantResult := &voltha.EventFilters{
		Filters: []*voltha.EventFilter{
			{
				Id: "id",
			},
		},
	}

	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.ListEventFilters(context.Background())
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("ListEventFilters() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0.Filters)
	assert.Nil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/event_filters", &voltha.EventFilter{Id: "id"}, ""); added == nil {
		t.Error("Failed to add event filter")
	}
	result1, err1 := mpMgr.ListEventFilters(context.Background())
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("ListEventFilters() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1.Filters)
	assert.Nil(t, err1)
	assert.Equal(t, wantResult, result1)
}

func TestGetEventFilter(t *testing.T) {
	/*
        wantResult := &voltha.EventFilters{}
	mpMgr := makeModelProxyManagerObj()

	// Case 1: Not Found
	result0, err0 := mpMgr.GetEventFilter(context.Background(), "id")
	if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
		t.Errorf("GetEventFilter() = %v, want %v", result0, wantResult)
	}
	assert.Nil(t, result0)
	assert.NotNil(t, err0)

	// Case 2: Found
	if added := mpMgr.clusterDataProxy.Add(context.Background(), "/event_filters", &voltha.EventFilter{Id: "id"}, ""); added == nil {
		t.Error("Failed to add event filter")
	}
	result1, err1 := mpMgr.GetEventFilter(context.Background(), "id")
	if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
		t.Errorf("GetEventFilter() = %v, want %v", result1, wantResult)
	}
	assert.NotNil(t, result1)
	assert.Nil(t, err1)
	assert.Equal(t, "id", result1.Filters[0].Id)
        */
        wantResult := &voltha.EventFilters{
                Filters: []*voltha.EventFilter{
                        {
                                Id: "id",
                        },
                },
        }

        mpMgr := makeModelProxyManagerObj()

        // Case 1: Not Found
        result0, err0 := mpMgr.GetEventFilter(context.Background(), "id")
        if reflect.TypeOf(result0) != reflect.TypeOf(wantResult) {
                t.Errorf("GetEventFilters() = %v, want %v", result0, wantResult)
        }
        assert.Nil(t, result0)
        assert.Nil(t, err0)

        // Case 2: Found
        if added := mpMgr.clusterDataProxy.Add(context.Background(), "/event_filters", &voltha.EventFilter{Id: "id"}, ""); added == nil {
                t.Error("Failed to add event filter")
        }
        result1, err1 := mpMgr.ListEventFilters(context.Background())
        if reflect.TypeOf(result1) != reflect.TypeOf(wantResult) {
                t.Errorf("GetEventFilters() = %v, want %v", result1, wantResult)
        }
        assert.NotNil(t, result1)
        assert.Nil(t, err1)
        assert.Equal(t, wantResult, result1)

}
