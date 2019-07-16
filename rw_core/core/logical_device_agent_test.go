/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
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
package core

import (
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLogicalDeviceAgent_diff_nochange_1(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{}
	updatedLogicalPorts := []*voltha.LogicalPort{}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
}

func TestLogicalDeviceAgent_diff_nochange_2(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
}

func TestLogicalDeviceAgent_diff_add(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 2, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
	assert.Equal(t, updatedLogicalPorts[0], newPorts[0])
	assert.Equal(t, updatedLogicalPorts[1], newPorts[1])
}

func TestLogicalDeviceAgent_diff_delete(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 0, len(changedPorts))
	assert.Equal(t, 1, len(deletedPorts))
	assert.Equal(t, currentLogicalPorts[0], deletedPorts[0])
}

func TestLogicalDeviceAgent_diff_changed(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 0, len(newPorts))
	assert.Equal(t, 2, len(changedPorts))
	assert.Equal(t, 0, len(deletedPorts))
	assert.Equal(t, updatedLogicalPorts[0], changedPorts[0])
	assert.Equal(t, updatedLogicalPorts[1], changedPorts[1])
}

func TestLogicalDeviceAgent_diff_mix(t *testing.T) {
	currentLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 1,
				State:  1,
			},
		},
		{
			Id:           "1233",
			DeviceId:     "d1234",
			DevicePortNo: 3,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 3,
				Name:   "port3",
				Config: 1,
				State:  1,
			},
		},
	}
	updatedLogicalPorts := []*voltha.LogicalPort{
		{
			Id:           "1231",
			DeviceId:     "d1234",
			DevicePortNo: 1,
			RootPort:     true,
			OfpPort: &ofp.OfpPort{
				PortNo: 1,
				Name:   "port1",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1232",
			DeviceId:     "d1234",
			DevicePortNo: 2,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 2,
				Name:   "port2",
				Config: 4,
				State:  4,
			},
		},
		{
			Id:           "1234",
			DeviceId:     "d1234",
			DevicePortNo: 4,
			RootPort:     false,
			OfpPort: &ofp.OfpPort{
				PortNo: 4,
				Name:   "port4",
				Config: 4,
				State:  4,
			},
		},
	}
	newPorts, changedPorts, deletedPorts := diff(currentLogicalPorts, updatedLogicalPorts)
	assert.Equal(t, 1, len(newPorts))
	assert.Equal(t, 2, len(changedPorts))
	assert.Equal(t, 1, len(deletedPorts))
	assert.Equal(t, updatedLogicalPorts[0], changedPorts[0])
	assert.Equal(t, updatedLogicalPorts[1], changedPorts[1])
	assert.Equal(t, currentLogicalPorts[2], deletedPorts[0])
}
