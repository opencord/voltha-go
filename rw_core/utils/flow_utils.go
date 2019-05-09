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
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"strings"
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

func (fg *FlowsAndGroups) ListFlows() []*ofp.OfpFlowStats {
	flows := make([]*ofp.OfpFlowStats, 0)
	iter := fg.Flows.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpFlowStats); isMsg {
			flows = append(flows, protoMsg)
		}
	}
	return flows
}

func (fg *FlowsAndGroups) ListGroups() []*ofp.OfpGroupEntry {
	groups := make([]*ofp.OfpGroupEntry, 0)
	iter := fg.Groups.IterFunc()
	for kv, ok := iter(); ok; kv, ok = iter() {
		if protoMsg, isMsg := kv.Value.(*ofp.OfpGroupEntry); isMsg {
			groups = append(groups, protoMsg)
		}
	}
	return groups
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
	if dr != nil {
		for key, val := range dr.Rules {
			if val != nil {
				copyDR.Rules[key] = val.Copy()
			}
		}
	}
	return copyDR
}

func (dr *DeviceRules) ClearFlows(deviceId string) {
	if _, exist := dr.Rules[deviceId]; exist {
		dr.Rules[deviceId].Flows = ordered_map.NewOrderedMap()
	}
}

func (dr *DeviceRules) FilterRules(deviceIds map[string]string) *DeviceRules {
	filteredDR := NewDeviceRules()
	for key, val := range dr.Rules {
		if _, exist := deviceIds[key]; exist {
			filteredDR.Rules[key] = val.Copy()
		}
	}
	return filteredDR
}

func (dr *DeviceRules) AddFlow(deviceId string, flow *ofp.OfpFlowStats) {
	if _, exist := dr.Rules[deviceId]; !exist {
		dr.Rules[deviceId] = NewFlowsAndGroups()
	}
	dr.Rules[deviceId].AddFlow(flow)
}

func (dr *DeviceRules) GetRules() map[string]*FlowsAndGroups {
	return dr.Rules
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

/*
 *  Common flow routines
 */

//FindOverlappingFlows return a list of overlapping flow(s) where mod is the flow request
func FindOverlappingFlows(flows []*ofp.OfpFlowStats, mod *ofp.OfpFlowMod) []*ofp.OfpFlowStats {
	return nil //TODO - complete implementation
}

// FindFlowById returns the index of the flow in the flows array if present. Otherwise, it returns -1
func FindFlowById(flows []*ofp.OfpFlowStats, flow *ofp.OfpFlowStats) int {
	for idx, f := range flows {
		if flow.Id == f.Id {
			return idx
		}
	}
	return -1
}

// FindFlows returns the index in flows where flow if present.  Otherwise, it returns -1
func FindFlows(flows []*ofp.OfpFlowStats, flow *ofp.OfpFlowStats) int {
	for idx, f := range flows {
		if FlowMatch(f, flow) {
			return idx
		}
	}
	return -1
}

//FlowMatch returns true if two flows matches on the following flow attributes:
//TableId, Priority, Flags, Cookie, Match
func FlowMatch(f1 *ofp.OfpFlowStats, f2 *ofp.OfpFlowStats) bool {
	keysMatter := []string{"TableId", "Priority", "Flags", "Cookie", "Match"}
	for _, key := range keysMatter {
		switch key {
		case "TableId":
			if f1.TableId != f2.TableId {
				return false
			}
		case "Priority":
			if f1.Priority != f2.Priority {
				return false
			}
		case "Flags":
			if f1.Flags != f2.Flags {
				return false
			}
		case "Cookie":
			if f1.Cookie != f2.Cookie {
				return false
			}
		case "Match":
			if strings.Compare(f1.Match.String(), f2.Match.String()) != 0 {
				return false
			}
		}
	}
	return true
}

//FlowMatchesMod returns True if given flow is "covered" by the wildcard flow_mod, taking into consideration of
//both exact matches as well as masks-based match fields if any. Otherwise return False
func FlowMatchesMod(flow *ofp.OfpFlowStats, mod *ofp.OfpFlowMod) bool {
	//Check if flow.cookie is covered by mod.cookie and mod.cookie_mask
	if (flow.Cookie & mod.CookieMask) != (mod.Cookie & mod.CookieMask) {
		return false
	}

	//Check if flow.table_id is covered by flow_mod.table_id
	if mod.TableId != uint32(ofp.OfpTable_OFPTT_ALL) && flow.TableId != mod.TableId {
		return false
	}

	//Check out_port
	if (mod.OutPort&0x7fffffff) != uint32(ofp.OfpPortNo_OFPP_ANY) && !FlowHasOutPort(flow, mod.OutPort) {
		return false
	}

	//	Check out_group
	if (mod.OutGroup&0x7fffffff) != uint32(ofp.OfpGroup_OFPG_ANY) && !FlowHasOutGroup(flow, mod.OutGroup) {
		return false
	}

	//Priority is ignored

	//Check match condition
	//If the flow_mod match field is empty, that is a special case and indicates the flow entry matches
	if (mod.Match == nil) || (mod.Match.OxmFields == nil) {
		//If we got this far and the match is empty in the flow spec, than the flow matches
		return true
	} // TODO : implement the flow match analysis
	return false

}

//FlowHasOutPort returns True if flow has a output command with the given out_port
func FlowHasOutPort(flow *ofp.OfpFlowStats, outPort uint32) bool {
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS) {
			if instruction.GetActions() == nil {
				return false
			}
			for _, action := range instruction.GetActions().Actions {
				if action.Type == ofp.OfpActionType_OFPAT_OUTPUT {
					if (action.GetOutput() != nil) && (action.GetOutput().Port == outPort) {
						return true
					}
				}

			}
		}
	}
	return false
}

//FlowHasOutGroup return True if flow has a output command with the given out_group
func FlowHasOutGroup(flow *ofp.OfpFlowStats, groupID uint32) bool {
	for _, instruction := range flow.Instructions {
		if instruction.Type == uint32(ofp.OfpInstructionType_OFPIT_APPLY_ACTIONS) {
			if instruction.GetActions() == nil {
				return false
			}
			for _, action := range instruction.GetActions().Actions {
				if action.Type == ofp.OfpActionType_OFPAT_GROUP {
					if (action.GetGroup() != nil) && (action.GetGroup().GroupId == groupID) {
						return true
					}
				}

			}
		}
	}
	return false
}

//FindGroup returns index of group if found, else returns -1
func FindGroup(groups []*ofp.OfpGroupEntry, groupId uint32) int {
	for idx, group := range groups {
		if group.Desc.GroupId == groupId {
			return idx
		}
	}
	return -1
}

func FlowsDeleteByGroupId(flows []*ofp.OfpFlowStats, groupId uint32) (bool, []*ofp.OfpFlowStats) {
	toKeep := make([]*ofp.OfpFlowStats, 0)

	for _, f := range flows {
		if !FlowHasOutGroup(f, groupId) {
			toKeep = append(toKeep, f)
		}
	}
	return len(toKeep) < len(flows), toKeep
}
