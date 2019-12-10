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
	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/stretchr/testify/assert"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fields struct {
	mutex        sync.RWMutex
	Root         *root
	Config       *DataRevision
	childrenLock sync.RWMutex
	Children     map[string][]Revision
	Hash         string
	Branch       *Branch
	WeakRef      string
	Name         string
	lastUpdate   time.Time
}

func testObject(testot *fields) *NonPersistedRevision {
	return &NonPersistedRevision{
		mutex:        testot.mutex,
		Root:         testot.Root,
		Config:       testot.Config,
		childrenLock: testot.childrenLock,
		Children:     testot.Children,
		Hash:         testot.Hash,
		Branch:       testot.Branch,
		WeakRef:      testot.WeakRef,
		Name:         testot.Name,
		lastUpdate:   testot.lastUpdate,
	}
}

func TestGetRevCache(t *testing.T) {
	var revcacheInst *revCacheSingleton
	revcacheInst = GetRevCache()
	assert.NotEqual(t, revcacheInst, nil)
}

var (
	Persisted_Device = &voltha.Device{
		Id:              "Config-SomeNode-01-new-test",
		Type:            "simulated_olt",
		Root:            true,
		ParentId:        "",
		ParentPortNo:    0,
		Vendor:          "voltha-test",
		Model:           "GetLatest-voltha-simulated-olt",
		HardwareVersion: "1.0.0",
		FirmwareVersion: "1.0.0",
		Images:          &voltha.Images{},
		SerialNumber:    "abcdef-123456",
		VendorId:        "DEADBEEF-INC",
		Adapter:         "simulated_olt",
		Vlan:            1234,
		Address:         &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		ExtraArgs:       "",
		ProxyAddress:    &voltha.Device_ProxyAddress{},
		AdminState:      voltha.AdminState_PREPROVISIONED,
		OperStatus:      common.OperStatus_ACTIVE,
		Reason:          "",
		ConnectStatus:   common.ConnectStatus_REACHABLE,
		Custom:          &any.Any{},
		Ports:           TestNode_Port,
		Flows:           &openflow_13.Flows{},
		FlowGroups:      &openflow_13.FlowGroups{},
		PmConfigs:       &voltha.PmConfigs{},
		ImageDownloads:  []*voltha.ImageDownload{},
	}

	Persisted_Data = Persisted_Device

	Persisted_Root = &root{RevisionClass: reflect.TypeOf(NonPersistedRevision{})}
)

func TestNewNonPersistedRevision(t *testing.T) {
	tests := []struct {
		name   string
		fields *fields
	}{
		{"NonPRevisiov", &fields{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rev := NewNonPersistedRevision(Persisted_Root, tt.fields.Branch, Persisted_Data, tt.fields.Children); rev == nil {
				t.Error("Failed to get Revision")
			} else {
				t.Logf("Revision type reflected")
			}

		})
	}
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name   string
		fields *fields
	}{
		{"NonPRevisiov", &fields{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			npr := testObject(tt.fields)
			rev := npr.GetConfig()
			assert.NotEqual(t, rev, nil)
			npr.SetConfig(rev)
		})
	}
}

func TestGetAllChildren(t *testing.T) {
	tests := []struct {
		name   string
		fields *fields
	}{
		{"NonPRevisiov", &fields{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			npr := testObject(tt.fields)
			rev := npr.GetAllChildren()
			assert.NotEqual(t, rev, nil)
			npr.SetAllChildren(rev)
		})
	}
}

func TestGetName(t *testing.T) {
	tests := []struct {
		name   string
		fields *fields
	}{
		{"NonPRevisiov", &fields{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			npr := testObject(tt.fields)
			rev := npr.GetName()
			assert.NotEqual(t, rev, nil)
			npr.SetName(rev)
			child := npr.GetChildren(rev)
			assert.NotEqual(t, child, nil)
		})
	}
}
