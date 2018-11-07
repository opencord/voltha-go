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
package core

import (
	"context"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	ofp "github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	fd "github.com/opencord/voltha-go/rw_core/flow_decomposition"
	"github.com/opencord/voltha-go/rw_core/graph"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"sync"
)

type LogicalDeviceAgent struct {
	logicalDeviceId   string
	lastData          *voltha.LogicalDevice
	rootDeviceId      string
	deviceMgr         *DeviceManager
	ldeviceMgr        *LogicalDeviceManager
	clusterDataProxy  *model.Proxy
	exitChannel       chan int
	deviceGraph       *graph.DeviceGraph
	DefaultFlowRules  *fu.DeviceRules
	flowProxy         *model.Proxy
	groupProxy        *model.Proxy
	lockLogicalDevice sync.RWMutex
	flowDecomposer    *fd.FlowDecomposer
}

func newLogicalDeviceAgent(id string, device *voltha.Device, ldeviceMgr *LogicalDeviceManager, deviceMgr *DeviceManager,
	cdProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceId = id
	agent.rootDeviceId = device.Id
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.flowDecomposer = fd.NewFlowDecomposer(agent.deviceMgr)
	agent.lockLogicalDevice = sync.RWMutex{}
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	//Build the logical device based on information retrieved from the device adapter
	var switchCap *ca.SwitchCapability
	var err error
	if switchCap, err = agent.deviceMgr.getSwitchCapability(ctx, agent.rootDeviceId); err != nil {
		log.Errorw("error-creating-logical-device", log.Fields{"error": err})
		return err
	}
	ld := &voltha.LogicalDevice{Id: agent.logicalDeviceId, RootDeviceId: agent.rootDeviceId}
	ld.Desc = (proto.Clone(switchCap.Desc)).(*ofp.OfpDesc)
	ld.SwitchFeatures = (proto.Clone(switchCap.SwitchFeatures)).(*ofp.OfpSwitchFeatures)
	ld.Flows = &ofp.Flows{Items: nil}
	ld.FlowGroups = &ofp.FlowGroups{Items: nil}

	//Add logical ports to the logical device based on the number of NNI ports discovered
	//First get the default port capability - TODO:  each NNI port may have different capabilities,
	//hence. may need to extract the port by the NNI port id defined by the adapter during device
	//creation
	var nniPorts *voltha.Ports
	if nniPorts, err = agent.deviceMgr.getPorts(ctx, agent.rootDeviceId, voltha.Port_ETHERNET_NNI); err != nil {
		log.Errorw("error-creating-logical-port", log.Fields{"error": err})
	}
	var portCap *ca.PortCapability
	for _, port := range nniPorts.Items {
		log.Infow("!!!!!!!NNI PORTS", log.Fields{"NNI": port})
		if portCap, err = agent.deviceMgr.getPortCapability(ctx, agent.rootDeviceId, port.PortNo); err != nil {
			log.Errorw("error-creating-logical-device", log.Fields{"error": err})
			return err
		}
		portCap.Port.RootPort = true
		lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
		lp.DeviceId = agent.rootDeviceId
		lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
		lp.OfpPort.PortNo = port.PortNo
		lp.OfpPort.Name = portCap.Port.Id
		lp.DevicePortNo = port.PortNo
		ld.Ports = append(ld.Ports, lp)
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Save the logical device
	if added := agent.clusterDataProxy.Add("/logical_devices", ld, ""); added == nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-created", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}

	agent.flowProxy = agent.clusterDataProxy.Root.GetProxy(
		fmt.Sprintf("/logical_devices/%s/flows", agent.logicalDeviceId),
		false)
	agent.groupProxy = agent.clusterDataProxy.Root.GetProxy(
		fmt.Sprintf("/logical_devices/%s/flow_groups", agent.logicalDeviceId),
		false)

	agent.flowProxy.RegisterCallback(model.POST_UPDATE, agent.flowTableUpdated)
	//agent.groupProxy.RegisterCallback(model.POST_UPDATE, agent.groupTableUpdated)

	return nil
}

// stop stops the logical devuce agent.  This removes the logical device from the data model.
func (agent *LogicalDeviceAgent) stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	//Remove the logical device from the model
	if removed := agent.clusterDataProxy.Remove("/logical_devices/"+agent.logicalDeviceId, ""); removed == nil {
		log.Errorw("failed-to-remove-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-removed", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}

// GetLogicalDevice locks the logical device model and then retrieves the latest logical device information
func (agent *LogicalDeviceAgent) GetLogicalDevice() (*voltha.LogicalDevice, error) {
	log.Debug("GetLogicalDevice")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cloned := proto.Clone(lDevice).(*voltha.LogicalDevice)
		return cloned, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDevicePorts() (*voltha.LogicalPorts, error) {
	log.Debug("!!!!!ListLogicalDevicePorts")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		lPorts := make([]*voltha.LogicalPort, 0)
		for _, port := range lDevice.Ports {
			lPorts = append(lPorts, proto.Clone(port).(*voltha.LogicalPort))
		}
		return &voltha.LogicalPorts{Items: lPorts}, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// listFlows locks the logical device model and then retrieves the latest flow information
func (agent *LogicalDeviceAgent) listFlows() []*ofp.OfpFlowStats {
	log.Debug("listFlows")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.Flows.Items
	}
	return nil
}

// listFlowGroups locks the logical device model and then retrieves the latest flow groups information
func (agent *LogicalDeviceAgent) listFlowGroups() []*ofp.OfpGroupEntry {
	log.Debug("listFlowGroups")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.FlowGroups.Items
	}
	return nil
}

// getLogicalDeviceWithoutLock retrieves a logical device from the model without locking it.   This is used only by
// functions that have already acquired the logical device lock to the model
func (agent *LogicalDeviceAgent) getLogicalDeviceWithoutLock() (*voltha.LogicalDevice, error) {
	log.Debug("getLogicalDeviceWithoutLock")
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cloned := proto.Clone(lDevice).(*voltha.LogicalDevice)
		return cloned, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// addUNILogicalPort creates a UNI port on the logical device that represents a child device
func (agent *LogicalDeviceAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device) error {
	log.Infow("addUNILogicalPort-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// Build the logical device based on information retrieved from the device adapter
	var portCap *ca.PortCapability
	var err error

	//Get UNI port number
	var uniPort uint32
	for _, port := range childDevice.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			uniPort = port.PortNo
		}
	}
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, childDevice.Id, uniPort); err != nil {
		log.Errorw("error-creating-logical-port", log.Fields{"error": err})
		return err
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get stored logical device
	if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		return status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		log.Infow("!!!!!!!!!!!ADDING-UNI", log.Fields{"deviceId": childDevice.Id})
		cloned := proto.Clone(ldevice).(*voltha.LogicalDevice)
		portCap.Port.RootPort = false
		//TODO: For now use the channel id assigned by the OLT as logical port number
		lPortNo := childDevice.ProxyAddress.ChannelId
		portCap.Port.Id = fmt.Sprintf("uni-%d", lPortNo)
		portCap.Port.OfpPort.PortNo = lPortNo
		portCap.Port.OfpPort.Name = portCap.Port.Id
		portCap.Port.DeviceId = childDevice.Id
		portCap.Port.DevicePortNo = uniPort
		lp := proto.Clone(portCap.Port).(*voltha.LogicalPort)
		lp.DeviceId = childDevice.Id
		cloned.Ports = append(cloned.Ports, lp)
		return agent.updateLogicalDeviceWithoutLock(cloned)
	}
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceWithoutLock(logicalDevice *voltha.LogicalDevice) error {
	cloned := proto.Clone(logicalDevice).(*voltha.LogicalDevice)
	afterUpdate := agent.clusterDataProxy.Update("/logical_devices/"+agent.logicalDeviceId, cloned, false, "")
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device:%s", agent.logicalDeviceId)
	}
	return nil
}

//updateFlowTable updates the flow table of that logical device
func (agent *LogicalDeviceAgent) updateFlowTable(ctx context.Context, flow *ofp.OfpFlowMod) error {
	log.Debug("updateFlowTable")
	if flow == nil {
		return nil
	}
	switch flow.GetCommand() {
	case ofp.OfpFlowModCommand_OFPFC_ADD:
		return agent.flowAdd(flow)
	case ofp.OfpFlowModCommand_OFPFC_DELETE:
		return agent.flowDelete(flow)
	case ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT:
		return agent.flowDeleteStrict(flow)
	case ofp.OfpFlowModCommand_OFPFC_MODIFY:
		return agent.flowModify(flow)
	case ofp.OfpFlowModCommand_OFPFC_MODIFY_STRICT:
		return agent.flowModifyStrict(flow)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceId, flow.GetCommand())
}

//updateGroupTable updates the group table of that logical device
func (agent *LogicalDeviceAgent) updateGroupTable(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	log.Debug("updateGroupTable")
	if groupMod == nil {
		return nil
	}
	switch groupMod.GetCommand() {
	case ofp.OfpGroupModCommand_OFPGC_ADD:
		return agent.groupAdd(groupMod)
	case ofp.OfpGroupModCommand_OFPGC_DELETE:
		return agent.groupDelete(groupMod)
	case ofp.OfpGroupModCommand_OFPGC_MODIFY:
		return agent.groupModify(groupMod)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceId, groupMod.GetCommand())
}

//updateFlowsWithoutLock updates the flows in the logical device without locking the logical device.  This function
//must only be called by a function that is holding the lock on the logical device
func (agent *LogicalDeviceAgent) updateFlowsWithoutLock(flows []*ofp.OfpFlowStats) error {
	if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		return status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		flowsCloned := make([]*ofp.OfpFlowStats, len(flows))
		copy(flowsCloned, flows)
		ldevice.Flows.Items = flowsCloned
		return agent.updateLogicalDeviceWithoutLock(ldevice)
	}
}

//updateFlowGroupsWithoutLock updates the flows in the logical device without locking the logical device.  This function
//must only be called by a function that is holding the lock on the logical device
func (agent *LogicalDeviceAgent) updateFlowGroupsWithoutLock(groups []*ofp.OfpGroupEntry) error {
	if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		return status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		groupsCloned := make([]*ofp.OfpGroupEntry, len(groups))
		copy(groupsCloned, groups)
		ldevice.FlowGroups.Items = groupsCloned
		return agent.updateLogicalDeviceWithoutLock(ldevice)
	}
}

//flowAdd adds a flow to the flow table of that logical device
func (agent *LogicalDeviceAgent) flowAdd(mod *ofp.OfpFlowMod) error {
	log.Debug("flowAdd")
	if mod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}

	var flows []*ofp.OfpFlowStats
	if lDevice.Flows != nil && lDevice.Flows.Items != nil {
		flows = lDevice.Flows.Items
	}

	oldData := proto.Clone(lDevice.Flows).(*voltha.Flows)
	changed := false
	checkOverlap := (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_CHECK_OVERLAP)) != 0
	if checkOverlap {
		if overlapped := fu.FindOverlappingFlows(flows, mod); len(overlapped) != 0 {
			//	TODO:  should this error be notified other than being logged?
			log.Warnw("overlapped-flows", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
		} else {
			//	Add flow
			flow := fd.FlowStatsEntryFromFlowModMessage(mod)
			flows = append(flows, flow)
			changed = true
		}
	} else {
		flow := fd.FlowStatsEntryFromFlowModMessage(mod)
		idx := fu.FindFlows(flows, flow)
		if idx >= 0 {
			oldFlow := flows[idx]
			if (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_RESET_COUNTS)) != 0 {
				flow.ByteCount = oldFlow.ByteCount
				flow.PacketCount = oldFlow.PacketCount
			}
			flows[idx] = flow
		} else {
			flows = append(flows, flow)
		}
		changed = true
	}
	if changed {
		//	Update model
		if lDevice.Flows == nil {
			lDevice.Flows = &ofp.Flows{}
		}
		lDevice.Flows.Items = flows
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
	// For now, force the callback to occur
	go agent.flowTableUpdated(oldData, lDevice.Flows)
	return nil
}

//flowDelete deletes a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowDelete(mod *ofp.OfpFlowMod) error {
	log.Debug("flowDelete")
	if mod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}
	flows := lDevice.Flows.Items

	//build a list of what to keep vs what to delete
	toKeep := make([]*ofp.OfpFlowStats, 0)
	for _, f := range flows {
		if !fu.FlowMatchesMod(f, mod) {
			toKeep = append(toKeep, f)
		}
	}

	//Update flows
	if len(toKeep) < len(flows) {
		lDevice.Flows.Items = toKeep
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}

	//TODO: send announcement on delete
	return nil
}

//flowStatsDelete deletes a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowStatsDelete(flow *ofp.OfpFlowStats) error {
	log.Debug("flowStatsDelete")
	if flow == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}
	flows := lDevice.Flows.Items

	//build a list of what to keep vs what to delete
	toKeep := make([]*ofp.OfpFlowStats, 0)
	for _, f := range flows {
		if !fu.FlowMatch(f, flow) {
			toKeep = append(toKeep, f)
		}
	}

	//Update flows
	if len(toKeep) < len(flows) {
		lDevice.Flows.Items = toKeep
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
	return nil
}

//flowDeleteStrict deletes a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowDeleteStrict(mod *ofp.OfpFlowMod) error {
	log.Debug("flowDeleteStrict")
	if mod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}
	flows := lDevice.Flows.Items
	changed := false
	flow := fd.FlowStatsEntryFromFlowModMessage(mod)
	idx := fu.FindFlows(flows, flow)
	if idx >= 0 {
		flows = append(flows[:idx], flows[idx+1:]...)
		changed = true
	} else {
		return errors.New(fmt.Sprintf("Cannot delete flow - %s", flow))
	}

	if changed {
		lDevice.Flows.Items = flows
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}

	return nil
}

//flowModify modifies a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowModify(mod *ofp.OfpFlowMod) error {
	return errors.New("flowModify not implemented")
}

//flowModifyStrict deletes a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowModifyStrict(mod *ofp.OfpFlowMod) error {
	return errors.New("flowModifyStrict not implemented")
}

func (agent *LogicalDeviceAgent) groupAdd(groupMod *ofp.OfpGroupMod) error {
	log.Debug("groupAdd")
	if groupMod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}
	groups := lDevice.FlowGroups.Items
	oldData := proto.Clone(lDevice.FlowGroups).(*voltha.FlowGroups)
	if fu.FindGroup(groups, groupMod.GroupId) == -1 {
		groups = append(groups, fd.GroupEntryFromGroupMod(groupMod))
		lDevice.FlowGroups.Items = groups
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	} else {
		return errors.New(fmt.Sprintf("Groups %d already present", groupMod.GroupId))
	}
	// For now, force the callback to occur
	go agent.groupTableUpdated(oldData, lDevice.FlowGroups)
	return nil
}

func (agent *LogicalDeviceAgent) groupDelete(groupMod *ofp.OfpGroupMod) error {
	log.Debug("groupDelete")
	if groupMod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}
	groups := lDevice.FlowGroups.Items
	flows := lDevice.Flows.Items
	groupsChanged := false
	flowsChanged := false
	groupId := groupMod.GroupId
	if groupId == uint32(ofp.OfpGroup_OFPG_ALL) {
		//TODO we must delete all flows that point to this group and
		//signal controller as requested by flow's flag
		groups = []*ofp.OfpGroupEntry{}
		groupsChanged = true
	} else {
		if idx := fu.FindGroup(groups, groupId); idx == -1 {
			return nil // Valid case
		} else {
			flowsChanged, flows = fu.FlowsDeleteByGroupId(flows, groupId)
			groups = append(groups[:idx], groups[idx+1:]...)
			groupsChanged = true
		}
	}
	if groupsChanged || flowsChanged {
		lDevice.FlowGroups.Items = groups
		lDevice.Flows.Items = flows
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
	return nil
}

func (agent *LogicalDeviceAgent) groupModify(groupMod *ofp.OfpGroupMod) error {
	log.Debug("groupModify")
	if groupMod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return errors.New(fmt.Sprintf("no-logical-device-present:%s", agent.logicalDeviceId))
	}
	groups := lDevice.FlowGroups.Items
	groupsChanged := false
	groupId := groupMod.GroupId
	if idx := fu.FindGroup(groups, groupId); idx == -1 {
		return errors.New(fmt.Sprintf("group-absent:%s", groupId))
	} else {
		//replace existing group entry with new group definition
		groupEntry := fd.GroupEntryFromGroupMod(groupMod)
		groups[idx] = groupEntry
		groupsChanged = true
	}
	if groupsChanged {
		lDevice.FlowGroups.Items = groups
		if err := agent.updateLogicalDeviceWithoutLock(lDevice); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
	return nil
}

// deleteLogicalPort removes the logical port
func (agent *LogicalDeviceAgent) deleteLogicalPort(lPort *voltha.LogicalPort) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "logicalPortId": lPort.Id})
		return nil
	}
	index := -1
	for i, logicalPort := range logicaldevice.Ports {
		if logicalPort.Id == lPort.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		copy(logicaldevice.Ports[index:], logicaldevice.Ports[index+1:])
		logicaldevice.Ports[len(logicaldevice.Ports)-1] = nil
		logicaldevice.Ports = logicaldevice.Ports[:len(logicaldevice.Ports)-1]
		log.Debugw("logical-port-deleted", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return agent.updateLogicalDeviceWithoutLock(logicaldevice)
	}
	return nil
}

// enableLogicalPort enables the logical port
func (agent *LogicalDeviceAgent) enableLogicalPort(lPort *voltha.LogicalPort) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "logicalPortId": lPort.Id})
		return nil
	}
	index := -1
	for i, logicalPort := range logicaldevice.Ports {
		if logicalPort.Id == lPort.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		logicaldevice.Ports[index].OfpPort.Config = logicaldevice.Ports[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDeviceWithoutLock(logicaldevice)
	}
	//TODO:  Trigger subsequent actions on the device
	return nil
}

// disableLogicalPort disabled the logical port
func (agent *LogicalDeviceAgent) disableLogicalPort(lPort *voltha.LogicalPort) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "logicalPortId": lPort.Id})
		return nil
	}
	index := -1
	for i, logicalPort := range logicaldevice.Ports {
		if logicalPort.Id == lPort.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		logicaldevice.Ports[index].OfpPort.Config = (logicaldevice.Ports[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)) | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDeviceWithoutLock(logicaldevice)
	}
	//TODO:  Trigger subsequent actions on the device
	return nil
}

func isNNIPort(portNo uint32, nniPortsNo []uint32) bool {
	for _, pNo := range nniPortsNo {
		if pNo == portNo {
			return true
		}
	}
	return false
}

func (agent *LogicalDeviceAgent) getPreCalculatedRoute(ingress, egress uint32) []graph.RouteHop {
	log.Debugw("ROUTE", log.Fields{"len": len(agent.deviceGraph.Routes)})
	for routeLink, route := range agent.deviceGraph.Routes {
		log.Debugw("ROUTELINKS", log.Fields{"ingress": ingress, "egress": egress, "routelink": routeLink})
		if ingress == routeLink.Ingress && egress == routeLink.Egress {
			return route
		}
	}
	log.Warnw("no-route", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "ingress": ingress, "egress": egress})
	return nil
}

func (agent *LogicalDeviceAgent) GetRoute(ingressPortNo uint32, egressPortNo uint32) []graph.RouteHop {
	log.Debugw("getting-route", log.Fields{"ingress-port": ingressPortNo, "egress-port": egressPortNo})
	// Get the updated logical device
	var ld *ca.LogicalDevice
	routes := make([]graph.RouteHop, 0)
	var err error
	if ld, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		return nil
	}
	nniLogicalPortsNo := make([]uint32, 0)
	for _, logicalPort := range ld.Ports {
		if logicalPort.RootPort {
			nniLogicalPortsNo = append(nniLogicalPortsNo, logicalPort.OfpPort.PortNo)
		}
	}
	if len(nniLogicalPortsNo) == 0 {
		log.Errorw("no-nni-ports", log.Fields{"LogicalDeviceId": ld.Id})
		return nil
	}
	// Note: A port value of 0 is equivalent to a nil port

	//	Consider different possibilities
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		log.Debugw("controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
		if isNNIPort(ingressPortNo, nniLogicalPortsNo) {
			log.Debug("returning-half-route")
			//This is a trap on the NNI Port
			//Return a 'half' route to make the flow decomposer logic happy
			for routeLink, route := range agent.deviceGraph.Routes {
				if isNNIPort(routeLink.Egress, nniLogicalPortsNo) {
					routes = append(routes, graph.RouteHop{}) // first hop is set to empty
					routes = append(routes, route[1])
					return routes
				}
			}
			log.Warnw("no-upstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
			return nil
		}
		//treat it as if the output port is the first NNI of the OLT
		egressPortNo = nniLogicalPortsNo[0]
	}
	//If ingress port is not specified (nil), it may be a wildcarded
	//route if egress port is OFPP_CONTROLLER or a nni logical port,
	//in which case we need to create a half-route where only the egress
	//hop is filled, the first hop is nil
	if ingressPortNo == 0 && isNNIPort(egressPortNo, nniLogicalPortsNo) {
		// We can use the 2nd hop of any upstream route, so just find the first upstream:
		for routeLink, route := range agent.deviceGraph.Routes {
			if isNNIPort(routeLink.Egress, nniLogicalPortsNo) {
				routes = append(routes, graph.RouteHop{}) // first hop is set to empty
				routes = append(routes, route[1])
				return routes
			}
		}
		log.Warnw("no-upstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
		return nil
	}
	//If egress port is not specified (nil), we can also can return a "half" route
	if egressPortNo == 0 {
		for routeLink, route := range agent.deviceGraph.Routes {
			if routeLink.Ingress == ingressPortNo {
				routes = append(routes, route[0])
				routes = append(routes, graph.RouteHop{})
				return routes
			}
		}
		log.Warnw("no-downstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
		return nil
	}

	//	Return the pre-calculated route
	return agent.getPreCalculatedRoute(ingressPortNo, egressPortNo)
}

// updateRoutes updates the device routes whenever there is a device or port changes relevant to this
// logical device.   TODO: Add more heuristics to this process to update the routes where a change has occurred
// instead of rebuilding the entire set of routes
func (agent *LogicalDeviceAgent) updateRoutes() {
	if ld, err := agent.GetLogicalDevice(); err == nil {
		agent.deviceGraph.ComputeRoutes(ld.Ports)
	}
}

func (agent *LogicalDeviceAgent) rootDeviceDefaultRules() *fu.FlowsAndGroups {
	return fu.NewFlowsAndGroups()
}

func (agent *LogicalDeviceAgent) leafDeviceDefaultRules(deviceId string) *fu.FlowsAndGroups {
	fg := fu.NewFlowsAndGroups()
	var device *voltha.Device
	var err error
	if device, err = agent.deviceMgr.GetDevice(deviceId); err != nil {
		return fg
	}
	//set the upstream and downstream ports
	upstreamPorts := make([]*voltha.Port, 0)
	downstreamPorts := make([]*voltha.Port, 0)
	for _, port := range device.Ports {
		if port.Type == voltha.Port_PON_ONU || port.Type == voltha.Port_VENET_ONU {
			upstreamPorts = append(upstreamPorts, port)
		} else if port.Type == voltha.Port_ETHERNET_UNI {
			downstreamPorts = append(downstreamPorts, port)
		}
	}
	//it is possible that the downstream ports are not created, but the flow_decomposition has already
	//kicked in. In such scenarios, cut short the processing and return.
	if len(downstreamPorts) == 0 {
		return fg
	}
	// set up the default flows
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fd.InPort(downstreamPorts[0].PortNo),
			fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fd.SetField(fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | device.Vlan)),
			fd.Output(upstreamPorts[0].PortNo),
		},
	}
	fg.AddFlow(fd.MkFlowStat(fa))

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fd.InPort(downstreamPorts[0].PortNo),
			fd.VlanVid(0),
		},
		Actions: []*ofp.OfpAction{
			fd.PushVlan(0x8100),
			fd.SetField(fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | device.Vlan)),
			fd.Output(upstreamPorts[0].PortNo),
		},
	}
	fg.AddFlow(fd.MkFlowStat(fa))

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fd.InPort(upstreamPorts[0].PortNo),
			fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | device.Vlan),
		},
		Actions: []*ofp.OfpAction{
			fd.SetField(fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0)),
			fd.Output(downstreamPorts[0].PortNo),
		},
	}
	fg.AddFlow(fd.MkFlowStat(fa))

	return fg
}

func (agent *LogicalDeviceAgent) generateDefaultRules() *fu.DeviceRules {
	rules := fu.NewDeviceRules()
	var ld *voltha.LogicalDevice
	var err error
	if ld, err = agent.GetLogicalDevice(); err != nil {
		log.Warnw("no-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
		return rules
	}

	deviceNodeIds := agent.deviceGraph.GetDeviceNodeIds()
	for deviceId, _ := range deviceNodeIds {
		if deviceId == ld.RootDeviceId {
			rules.AddFlowsAndGroup(deviceId, agent.rootDeviceDefaultRules())
		} else {
			rules.AddFlowsAndGroup(deviceId, agent.leafDeviceDefaultRules(deviceId))
		}
	}
	return rules
}

func (agent *LogicalDeviceAgent) GetAllDefaultRules() *fu.DeviceRules {
	// Get latest
	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.GetLogicalDevice(); err != nil {
		return fu.NewDeviceRules()
	}
	if agent.DefaultFlowRules == nil { // Nothing setup yet
		agent.deviceGraph = graph.NewDeviceGraph(agent.deviceMgr.GetDevice)
		agent.deviceGraph.ComputeRoutes(lDevice.Ports)
		agent.DefaultFlowRules = agent.generateDefaultRules()
	}
	return agent.DefaultFlowRules
}

func (agent *LogicalDeviceAgent) GetWildcardInputPorts(excludePort ...uint32) []uint32 {
	lPorts := make([]uint32, 0)
	var exclPort uint32
	if len(excludePort) == 1 {
		exclPort = excludePort[0]
	}
	if lDevice, _ := agent.GetLogicalDevice(); lDevice != nil {
		for _, port := range lDevice.Ports {
			if port.OfpPort.PortNo != exclPort {
				lPorts = append(lPorts, port.OfpPort.PortNo)
			}
		}
	}
	return lPorts
}

func (agent *LogicalDeviceAgent) GetDeviceGraph() *graph.DeviceGraph {
	return agent.deviceGraph
}

//setupDeviceGraph creates the device graph if not done already
func (agent *LogicalDeviceAgent) setupDeviceGraph() {
	if agent.deviceGraph == nil {
		agent.deviceGraph = graph.NewDeviceGraph(agent.deviceMgr.GetDevice)
		agent.updateRoutes()
	}
}

func (agent *LogicalDeviceAgent) flowTableUpdated(args ...interface{}) interface{} {
	log.Debugw("flowTableUpdated-callback", log.Fields{"argsLen": len(args)})

	//agent.lockLogicalDevice.Lock()
	//defer agent.lockLogicalDevice.Unlock()

	var previousData *ofp.Flows
	var latestData *ofp.Flows

	var ok bool
	if previousData, ok = args[0].(*ofp.Flows); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
	}
	if latestData, ok = args[1].(*ofp.Flows); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
	}

	if reflect.DeepEqual(previousData.Items, latestData.Items) {
		log.Debug("flow-update-not-required")
		return nil
	}

	// Ensure the device graph has been setup
	agent.setupDeviceGraph()

	var groups *ofp.FlowGroups
	lDevice, _ := agent.getLogicalDeviceWithoutLock()
	groups = lDevice.FlowGroups
	log.Debugw("flowsinfo", log.Fields{"flows": latestData, "groups": groups})
	//groupsIf := agent.groupProxy.Get("/", 1, false, "")
	//if groups, ok = groupsIf.(*ofp.FlowGroups); !ok {
	//	log.Errorw("cannot-retrieve-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "group": groupsIf})
	//	//return errors.New("cannot-retrieve-groups")
	//	groups = &ofp.FlowGroups{Items:nil}
	//}
	deviceRules := agent.flowDecomposer.DecomposeRules(agent, *latestData, *groups)
	log.Debugw("rules", log.Fields{"rules": deviceRules.String()})
	for deviceId, value := range deviceRules.GetRules() {
		agent.deviceMgr.updateFlows(deviceId, value.ListFlows())
		agent.deviceMgr.updateGroups(deviceId, value.ListGroups())
	}
	return nil
}

func (agent *LogicalDeviceAgent) groupTableUpdated(args ...interface{}) interface{} {
	log.Debugw("groupTableUpdated-callback", log.Fields{"argsLen": len(args)})

	//agent.lockLogicalDevice.Lock()
	//defer agent.lockLogicalDevice.Unlock()

	var previousData *ofp.FlowGroups
	var latestData *ofp.FlowGroups

	var ok bool
	if previousData, ok = args[0].(*ofp.FlowGroups); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
	}
	if latestData, ok = args[1].(*ofp.FlowGroups); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
	}

	if reflect.DeepEqual(previousData.Items, latestData.Items) {
		log.Debug("flow-update-not-required")
		return nil
	}

	// Ensure the device graph has been setup
	agent.setupDeviceGraph()

	var flows *ofp.Flows
	lDevice, _ := agent.getLogicalDeviceWithoutLock()
	flows = lDevice.Flows
	log.Debugw("groupsinfo", log.Fields{"groups": latestData, "flows": flows})
	//flowsIf := agent.flowProxy.Get("/", 1, false, "")
	//if flows, ok = flowsIf.(*ofp.Flows); !ok {
	//	log.Errorw("cannot-retrieve-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "flows": flows})
	//	//return errors.New("cannot-retrieve-groups")
	//	flows = &ofp.Flows{Items:nil}
	//}
	deviceRules := agent.flowDecomposer.DecomposeRules(agent, *flows, *latestData)
	log.Debugw("rules", log.Fields{"rules": deviceRules.String()})
	for deviceId, value := range deviceRules.GetRules() {
		agent.deviceMgr.updateFlows(deviceId, value.ListFlows())
		agent.deviceMgr.updateGroups(deviceId, value.ListGroups())
	}
	return nil
}

func (agent *LogicalDeviceAgent) packetOut(packet *ofp.OfpPacketOut ) {
	log.Debugw("packet-out", log.Fields{"packet": packet.GetInPort()})
	outPort := fd.GetPacketOutPort(packet)
	//frame := packet.GetData()
	//TODO: Use a channel between the logical agent and the device agent
	agent.deviceMgr.packetOut(agent.rootDeviceId, outPort, packet)
}


func (agent *LogicalDeviceAgent) packetIn(port uint32, packet []byte) {
	log.Debugw("packet-in", log.Fields{"port": port, "packet": packet})
	packet_in := fd.MkPacketIn(port, packet)
	log.Debugw("sending-packet-in", log.Fields{"packet-in": packet_in})
}

