/*
 * Copyright 2018-present Open Networking Foundation
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
package utils

import (
	"bytes"
	"github.com/cevaris/ordered_map"
	"github.com/gogo/protobuf/proto"
	ofp "github.com/opencord/voltha-go/protos/openflow_13"
)

type OfpFlowModArgs map[string]uint64

type FlowArgs struct {
	MatchFields []*ofp.OfpOxmOfbField
	Actions     []*ofp.OfpAction
	Command     *ofp.OfpFlowModCommand
	Priority    uint32
	KV          OfpFlowModArgs
}

type GroupArgs struct {
	GroupId uint32
	Buckets []*ofp.OfpBucket
	Command *ofp.OfpGroupModCommand
}

type FlowsAndGroups struct {
	Flows  *ordered_map.OrderedMap
	Groups *ordered_map.OrderedMap
}

func NewFlowsAndGroups() *FlowsAndGroups {
	var fg FlowsAndGroups
	fg.Flows = ordered_map.NewOrderedMap()
	fg.Groups = ordered_map.NewOrderedMap()
	return &fg
}

func (fg *FlowsAndGroups) Copy() *FlowsAndGroups {
	copyFG := NewFlowsAndGroups()
	iter := fg.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			copyFG.Flows.Set(kv.Key, proto.Clone(protoMsg))
		}
	}
	iter = fg.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			copyFG.Groups.Set(kv.Key, proto.Clone(protoMsg))
		}
	}
	return copyFG
}

func (fg *FlowsAndGroups) GetFlow(index int) *ofp.OfpFlowStats {
	iter := fg.Flows.IterFunc()
	pos := 0
	for kv, ok := iter(); ok; kv, ok = iter() {
		if pos == index {
			if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
				return protoMsg
			}
			return nil
		}
		pos += 1
	}
	return nil
}

func (fg *FlowsAndGroups) String() string {
	var buffer bytes.Buffer
	iter := fg.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			buffer.WriteString("\nFlow:\n")
			buffer.WriteString(proto.MarshalTextString(protoMsg))
			buffer.WriteString("\n")
		}
	}
	iter = fg.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			buffer.WriteString("\nGroup:\n")
			buffer.WriteString(proto.MarshalTextString(protoMsg))
			buffer.WriteString("\n")
		}
	}
	return buffer.String()
}

func (fg *FlowsAndGroups) AddFlow(flow *ofp.OfpFlowStats) {
	if fg.Flows == nil {
		fg.Flows = ordered_map.NewOrderedMap()
	}
	if fg.Groups == nil {
		fg.Groups = ordered_map.NewOrderedMap()
	}
	//Add flow only if absent
	if _, exist := fg.Flows.Get(flow.Id); !exist {
		fg.Flows.Set(flow.Id, flow)
	}
}

//AddFrom add flows and groups from the argument into this structure only if they do not already exist
func (fg *FlowsAndGroups) AddFrom(from *FlowsAndGroups) {
	iter := from.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			if _, exist := fg.Flows.Get(protoMsg.Id); !exist {
				fg.Flows.Set(protoMsg.Id, protoMsg)
			}
		}
	}
	iter = from.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			if _, exist := fg.Groups.Get(protoMsg.Stats.GroupId); !exist {
				fg.Groups.Set(protoMsg.Stats.GroupId, protoMsg)
			}
		}
	}
}

type DeviceRules struct {
	Rules map[string]*FlowsAndGroups
}

func NewDeviceRules() *DeviceRules {
	var dr DeviceRules
	dr.Rules = make(map[string]*FlowsAndGroups)
	return &dr
}

func (dr *DeviceRules) Copy() *DeviceRules {
	copyDR := NewDeviceRules()
	for key, val := range dr.Rules {
		copyDR.Rules[key] = val.Copy()
	}
	return copyDR
}

func (dr *DeviceRules) String() string {
	var buffer bytes.Buffer
	for key, value := range dr.Rules {
		buffer.WriteString("DeviceId:")
		buffer.WriteString(key)
		buffer.WriteString(value.String())
		buffer.WriteString("\n\n")
	}
	return buffer.String()
}

func (dr *DeviceRules) AddFlowsAndGroup(deviceId string, fg *FlowsAndGroups) {
	if _, ok := dr.Rules[deviceId]; !ok {
		dr.Rules[deviceId] = NewFlowsAndGroups()
	}
	dr.Rules[deviceId] = fg
}

// CreateEntryIfNotExist creates a new deviceId in the Map if it does not exist and assigns an
// empty FlowsAndGroups to it.  Otherwise, it does nothing.
func (dr *DeviceRules) CreateEntryIfNotExist(deviceId string) {
	if _, ok := dr.Rules[deviceId]; !ok {
		dr.Rules[deviceId] = NewFlowsAndGroups()
	}
}
