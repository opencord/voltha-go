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
	ic "github.com/opencord/voltha-protos/go/inter_container"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
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
	//lastData          *voltha.LogicalDevice
	rootDeviceId      string
	deviceMgr         *DeviceManager
	ldeviceMgr        *LogicalDeviceManager
	clusterDataProxy  *model.Proxy
	exitChannel       chan int
	deviceGraph       *graph.DeviceGraph
	DefaultFlowRules  *fu.DeviceRules
	flowProxy         *model.Proxy
	groupProxy        *model.Proxy
	ldProxy *model.Proxy
	portProxies map[string]*model.Proxy
	portProxiesLock sync.RWMutex
	lockLogicalDevice sync.RWMutex
	flowDecomposer    *fd.FlowDecomposer
}

func newLogicalDeviceAgent(id string, deviceId string, ldeviceMgr *LogicalDeviceManager,
	deviceMgr *DeviceManager,
	cdProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceId = id
	agent.rootDeviceId = deviceId
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.flowDecomposer = fd.NewFlowDecomposer(agent.deviceMgr)
	agent.lockLogicalDevice = sync.RWMutex{}
	agent.portProxies = make(map[string]*model.Proxy)
	agent.portProxiesLock = sync.RWMutex{}
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context, loadFromdB bool) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceId": agent.logicalDeviceId, "loadFromdB": loadFromdB})
	var ld *voltha.LogicalDevice
	if !loadFromdB {
		//Build the logical device based on information retrieved from the device adapter
		var switchCap *ic.SwitchCapability
		var err error
		if switchCap, err = agent.deviceMgr.getSwitchCapability(ctx, agent.rootDeviceId); err != nil {
			log.Errorw("error-creating-logical-device", log.Fields{"error": err})
			return err
		}
		ld = &voltha.LogicalDevice{Id: agent.logicalDeviceId, RootDeviceId: agent.rootDeviceId}

		// Create the datapath ID (uint64) using the logical device ID (based on the MAC Address)
		var datapathID uint64
		if datapathID, err = CreateDataPathId(agent.logicalDeviceId); err != nil {
			log.Errorw("error-creating-datapath-id", log.Fields{"error": err})
			return err
		}
		ld.DatapathId = datapathID
		ld.Desc = (proto.Clone(switchCap.Desc)).(*ofp.OfpDesc)
		log.Debugw("Switch-capability", log.Fields{"Desc": ld.Desc, "fromAd": switchCap.Desc})
		ld.SwitchFeatures = (proto.Clone(switchCap.SwitchFeatures)).(*ofp.OfpSwitchFeatures)
		ld.Flows = &ofp.Flows{Items: nil}
		ld.FlowGroups = &ofp.FlowGroups{Items: nil}

		agent.lockLogicalDevice.Lock()
		// Save the logical device
		if added := agent.clusterDataProxy.AddWithID("/logical_devices", ld.Id, ld, ""); added == nil {
			log.Errorw("failed-to-add-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
		} else {
			log.Debugw("logicaldevice-created", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
		}
		agent.lockLogicalDevice.Unlock()

		// TODO:  Set the NNI ports in a separate call once the port update issue is fixed.
		go agent.setupNNILogicalPorts(ctx, agent.rootDeviceId)
	} else {
		//	load from dB - the logical may not exist at this time.  On error, just return and the calling function
		// will destroy this agent.
		var err error
		if ld, err = agent.GetLogicalDevice(); err != nil {
			log.Warnw("failed-to-load-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
			return err
		}
		// Update the root device Id
		agent.rootDeviceId = ld.RootDeviceId
	}
	agent.lockLogicalDevice.Lock()

	agent.flowProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/flows", agent.logicalDeviceId),
		false)
	agent.groupProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/flow_groups", agent.logicalDeviceId),
		false)
	agent.ldProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/logical_devices/%s", agent.logicalDeviceId),
		false)

	agent.flowProxy.RegisterCallback(model.POST_UPDATE, agent.flowTableUpdated)
	agent.groupProxy.RegisterCallback(model.POST_UPDATE, agent.groupTableUpdated)

	// TODO:  Use a port proxy once the POST_ADD is fixed
	agent.ldProxy.RegisterCallback(model.POST_UPDATE, agent.portUpdated)

	agent.lockLogicalDevice.Unlock()

	return nil
}

// stop stops the logical devuce agent.  This removes the logical device from the data model.
func (agent *LogicalDeviceAgent) stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Unregister to teh callbacks
	if agent.flowProxy != nil {
		agent.flowProxy.UnregisterCallback(model.POST_UPDATE, agent.flowTableUpdated)
	}
	if agent.groupProxy != nil {
		agent.groupProxy.UnregisterCallback(model.POST_UPDATE, agent.groupTableUpdated)
	}
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
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDevicePorts() (*voltha.LogicalPorts, error) {
	log.Debug("!!!!!ListLogicalDevicePorts")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		lPorts := make([]*voltha.LogicalPort, 0)
		for _, port := range lDevice.Ports {
			lPorts = append(lPorts, port)
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
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
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
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.FlowGroups.Items
	}
	return nil
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceFlowsWithoutLock(flows *ofp.Flows) error {
	afterUpdate := agent.flowProxy.Update("/", flows, false, "")
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device-flows:%s", agent.logicalDeviceId)
	}
	return nil
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceFlowGroupsWithoutLock(flowGroups *ofp.FlowGroups) error {
	afterUpdate := agent.groupProxy.Update("/", flowGroups, false, "")
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device-flow-groups:%s", agent.logicalDeviceId)
	}
	return nil
}

// getLogicalDeviceWithoutLock retrieves a logical device from the model without locking it.   This is used only by
// functions that have already acquired the logical device lock to the model
func (agent *LogicalDeviceAgent) getLogicalDeviceWithoutLock() (*voltha.LogicalDevice, error) {
	log.Debug("getLogicalDeviceWithoutLock")
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		log.Debug("getLogicalDeviceWithoutLock", log.Fields{"ldevice": lDevice})
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}


func (agent *LogicalDeviceAgent)  addLogicalPort (device *voltha.Device, port *voltha.Port) error {
	if port.Type == voltha.Port_ETHERNET_NNI {
		if err := agent.addNNILogicalPort(device, port); err != nil {
			return err
		}
	} else if port.Type == voltha.Port_ETHERNET_UNI {
		if err :=  agent.addUNILogicalPort(device, port); err != nil {
			return err
		}
	} else {
		log.Debugw("invalid-port-type", log.Fields{"deviceId": device.Id, "port": port})
		return nil
	}
	go agent.setupDeviceGraph()
	return nil
}

// setupNNILogicalPorts creates an NNI port on the logical device that represents an NNI interface on a root device
func (agent *LogicalDeviceAgent) setupNNILogicalPorts(ctx context.Context, deviceId string) error {
	log.Infow("setupNNILogicalPorts-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// Build the logical device based on information retrieved from the device adapter
	//var portCap *ic.PortCapability
	var err error

	var device *voltha.Device
	if device, err = agent.deviceMgr.GetDevice(deviceId); err != nil {
		log.Errorw("error-retrieving-device", log.Fields{"error": err, "deviceId": device.Id})
		return err
	}

	//Get UNI port number
	//var uniPort uint32
	changesMade := false
	for _, port := range device.Ports {
		if port.Type == voltha.Port_ETHERNET_NNI {
			if err = agent.addNNILogicalPort(device, port); err != nil {
				log.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			} else {
				changesMade = true
			}
			//uniPort = port.PortNo
		}
	}
	if changesMade {
		go agent.setupDeviceGraph()
	}
	return err
}


// setupUNILogicalPorts creates a UNI port on the logical device that represents a child UNI interface
func (agent *LogicalDeviceAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	log.Infow("setupUNILogicalPort-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// Build the logical device based on information retrieved from the device adapter
	//var portCap *ic.PortCapability
	var err error

	//Get UNI port number
	//var uniPort uint32
	changesMade := false
	for _, port := range childDevice.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			if err = agent.addUNILogicalPort(childDevice, port); err != nil {
				log.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			} else {
				changesMade = true
			}
			//uniPort = port.PortNo
		}
	}
	if changesMade {
		go agent.setupDeviceGraph()
	}
	//if portCap, err = agent.deviceMgr.getPortCapability(ctx, childDevice.Id, uniPort); err != nil {
	//	log.Errorw("error-creating-logical-port", log.Fields{"error": err})
	//	return err
	//}
	//agent.lockLogicalDevice.Lock()
	//defer agent.lockLogicalDevice.Unlock()
	//// Get stored logical device
	//if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
	//	return status.Error(codes.NotFound, agent.logicalDeviceId)
	//} else {
	//	log.Debugw("adding-uni", log.Fields{"deviceId": childDevice.Id})
	//	portCap.Port.RootPort = false
	//	//TODO: For now use the channel id assigned by the OLT as logical port number
	//	lPortNo := childDevice.ProxyAddress.ChannelId
	//	portCap.Port.Id = fmt.Sprintf("uni-%d", lPortNo)
	//	portCap.Port.OfpPort.PortNo = lPortNo
	//	portCap.Port.OfpPort.Name = portCap.Port.Id
	//	portCap.Port.DeviceId = childDevice.Id
	//	portCap.Port.DevicePortNo = uniPort
	//	portCap.Port.DeviceId = childDevice.Id
	//
	//	ldevice.Ports = append(ldevice.Ports, portCap.Port)
	//	return agent.updateLogicalDeviceWithoutLock(ldevice)
	//}
	return err
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceWithoutLock(logicalDevice *voltha.LogicalDevice) error {
	afterUpdate := agent.clusterDataProxy.Update("/logical_devices/"+agent.logicalDeviceId, logicalDevice, false, "")
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device:%s", agent.logicalDeviceId)
	}
	//if a, ok := afterUpdate.(*voltha.LogicalDevice); ok {
	//	log.Debugw("AFTER UPDATE", log.Fields{"logical": a})
	//}
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

//updateFlowGroupsWithoutLock updates the flows in the logical device without locking the logical device.  This function
//must only be called by a function that is holding the lock on the logical device
func (agent *LogicalDeviceAgent) updateFlowGroupsWithoutLock(groups []*ofp.OfpGroupEntry) error {
	groupsCloned := make([]*ofp.OfpGroupEntry, len(groups))
	copy(groupsCloned, groups)
	if afterUpdate := agent.groupProxy.Update("/", groupsCloned, true, ""); afterUpdate == nil {
		return errors.New(fmt.Sprintf("update-flow-group-failed:%s", agent.logicalDeviceId))
	}
	return nil
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

	//oldData := proto.Clone(lDevice.Flows).(*voltha.Flows)
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
		flowsToUpdate := &ofp.Flows{}
		if lDevice.Flows != nil {
			flowsToUpdate = &ofp.Flows{Items: flows}
		}
		if err := agent.updateLogicalDeviceFlowsWithoutLock(flowsToUpdate); err != nil {
			log.Errorw("Cannot-update-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
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
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: toKeep}); err != nil {
			log.Errorw("Cannot-update-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
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
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: toKeep}); err != nil {
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
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: flows}); err != nil {
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
	if fu.FindGroup(groups, groupMod.GroupId) == -1 {
		groups = append(groups, fd.GroupEntryFromGroupMod(groupMod))
		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(&ofp.FlowGroups{Items: groups}); err != nil {
			log.Errorw("Cannot-update-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	} else {
		return errors.New(fmt.Sprintf("Groups %d already present", groupMod.GroupId))
	}
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
	if groupsChanged {
		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(&ofp.FlowGroups{Items: groups}); err != nil {
			log.Errorw("Cannot-update-group", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
	if flowsChanged {
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: flows}); err != nil {
			log.Errorw("Cannot-update-flow", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
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
		return errors.New(fmt.Sprintf("group-absent:%d", groupId))
	} else {
		//replace existing group entry with new group definition
		groupEntry := fd.GroupEntryFromGroupMod(groupMod)
		groups[idx] = groupEntry
		groupsChanged = true
	}
	if groupsChanged {
		//lDevice.FlowGroups.Items = groups
		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(&ofp.FlowGroups{Items: groups}); err != nil {
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
	var ld *ic.LogicalDevice
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
	if len(downstreamPorts) == 0 || len(upstreamPorts) == 0{
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
	for deviceId := range deviceNodeIds {
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

	var groups *ofp.FlowGroups
	lDevice, _ := agent.getLogicalDeviceWithoutLock()
	groups = lDevice.FlowGroups
	log.Debugw("flowsinfo", log.Fields{"flows": latestData, "groups": groups})
	deviceRules := agent.flowDecomposer.DecomposeRules(agent, *latestData, *groups)
	log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

	var err error
	for deviceId, value := range deviceRules.GetRules() {
		if err = agent.deviceMgr.updateFlows(deviceId, value.ListFlows()); err != nil {
			log.Error("update-flows-failed", log.Fields{"deviceID":deviceId})
		}
		if err = agent.deviceMgr.updateGroups(deviceId, value.ListGroups()); err != nil {
			log.Error("update-groups-failed", log.Fields{"deviceID":deviceId})
		}
	}

	return nil
}

func (agent *LogicalDeviceAgent) groupTableUpdated(args ...interface{}) interface{} {
	log.Debugw("groupTableUpdated-callback", log.Fields{"argsLen": len(args)})

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

	var flows *ofp.Flows
	lDevice, _ := agent.getLogicalDeviceWithoutLock()
	flows = lDevice.Flows
	log.Debugw("groupsinfo", log.Fields{"groups": latestData, "flows": flows})
	deviceRules := agent.flowDecomposer.DecomposeRules(agent, *flows, *latestData)
	log.Debugw("rules", log.Fields{"rules": deviceRules.String()})
	var err error
	for deviceId, value := range deviceRules.GetRules() {
		if err = agent.deviceMgr.updateFlows(deviceId, value.ListFlows()); err != nil {
			log.Error("update-flows-failed", log.Fields{"deviceID":deviceId})
		}
		if err = agent.deviceMgr.updateGroups(deviceId, value.ListGroups()); err != nil {
			log.Error("update-groups-failed", log.Fields{"deviceID":deviceId})
		}

	}
	return nil
}

// portAdded is a callback invoked when a port is added to the logical device.
// TODO: To use when POST_ADD is fixed.
func (agent *LogicalDeviceAgent) portAdded(args ...interface{}) interface{} {
	log.Debugw("portAdded-callback", log.Fields{"argsLen": len(args)})

	var port *voltha.LogicalPort

	// Sanity check
	if args[0] != nil {
		log.Warnw("previous-data-not-nil", log.Fields{"args0": args[0]})
	}
	var ok bool
	if port, ok = args[1].(*voltha.LogicalPort); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	// Set the proxy and callback for that port
	agent.portProxiesLock.Lock()
	agent.portProxies[port.Id] = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/ports/%s", agent.logicalDeviceId, port.Id),
		false)
	agent.portProxies[port.Id].RegisterCallback(model.POST_UPDATE, agent.portUpdated)
	agent.portProxiesLock.Unlock()

	// Send the port change event to the OF controller
	agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
		&ofp.OfpPortStatus{Reason:ofp.OfpPortReason_OFPPR_ADD, Desc:port.OfpPort})

	return nil
}

// portRemoved is a callback invoked when a port is removed from the logical device.
// TODO: To use when POST_ADD is fixed.
func (agent *LogicalDeviceAgent) portRemoved(args ...interface{}) interface{} {
	log.Debugw("portRemoved-callback", log.Fields{"argsLen": len(args)})

	var port *voltha.LogicalPort

	// Sanity check
	if args[1] != nil {
		log.Warnw("data-not-nil", log.Fields{"args1": args[1]})
	}
	var ok bool
	if port, ok = args[0].(*voltha.LogicalPort); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}

	// Remove the proxy and callback for that port
	agent.portProxiesLock.Lock()
	agent.portProxies[port.Id].UnregisterCallback(model.POST_UPDATE, agent.portUpdated)
	delete(agent.portProxies, port.Id)
	agent.portProxiesLock.Unlock()

	// Send the port change event to the OF controller
	agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
		&ofp.OfpPortStatus{Reason:ofp.OfpPortReason_OFPPR_DELETE, Desc:port.OfpPort})

	return nil
}

// diff go over two lists of logical ports and return what's new, what's changed and what's removed.
func diff(oldList , newList []*voltha.LogicalPort) (newPorts, changedPorts, deletedPorts []*voltha.LogicalPort) {
	newPorts = make([]*voltha.LogicalPort, 0)
	changedPorts = make([]*voltha.LogicalPort, 0)
	deletedPorts = make([]*voltha.LogicalPort, 0)
	for _, o := range oldList {
		found := false
		changed := false
		for _, n := range newList {
			if o.Id == n.Id {
				changed = !reflect.DeepEqual(o, n)
				found = true
				break
			}
		}
		if !found {
			deletedPorts = append(deletedPorts, o)
		}
		if changed {
			changedPorts = append(changedPorts, o)
		}
	}
	for _, n := range newList {
		found := false
		for _, o := range oldList {
			if o.Id == n.Id {
				found = true
				break
			}
		}
		if !found {
			newPorts = append(newPorts, n)
		}
	}
	return
}

// portUpdated is invoked when a port is updated on the logical device.  Until
// the POST_ADD notification is fixed, we will use the logical device to
// update that data.
func (agent *LogicalDeviceAgent) portUpdated(args ...interface{}) interface{} {
	log.Debugw("portUpdated-callback", log.Fields{"argsLen": len(args)})

	var oldLD *voltha.LogicalDevice
	var newlD *voltha.LogicalDevice

	var ok bool
	if oldLD, ok = args[0].(*voltha.LogicalDevice); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}
	if newlD, ok = args[1].(*voltha.LogicalDevice); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	if reflect.DeepEqual(oldLD.Ports, newlD.Ports) {
		log.Debug("ports-have-not-changed")
		return nil
	}

	// Get the difference between the two list
	newPorts, changedPorts, deletedPorts := diff(oldLD.Ports, newlD.Ports)

	// Send the port change events to the OF controller
	for _, new := range newPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
			&ofp.OfpPortStatus{Reason:ofp.OfpPortReason_OFPPR_ADD, Desc:new.OfpPort})
	}
	for _, change := range changedPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
			&ofp.OfpPortStatus{Reason:ofp.OfpPortReason_OFPPR_MODIFY, Desc:change.OfpPort})
	}
	for _, del := range deletedPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
			&ofp.OfpPortStatus{Reason:ofp.OfpPortReason_OFPPR_DELETE, Desc:del.OfpPort})
	}

	return nil
}


func (agent *LogicalDeviceAgent) addNNILogicalPort (device *voltha.Device, port *voltha.Port)  error {
	log.Infow("addNNILogicalPort", log.Fields{"NNI": port})
	if agent.portExist(device, port) {
		log.Debugw("port-already-exist", log.Fields{"port": port})
		return nil
	}
	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(nil, device.Id, port.PortNo); err != nil {
		log.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return err
	}
	portCap.Port.RootPort = true
	lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
	lp.DeviceId = device.Id
	lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
	lp.OfpPort.PortNo = port.PortNo
	lp.OfpPort.Name = lp.Id
	lp.DevicePortNo = port.PortNo

	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	var ld *voltha.LogicalDevice
	if ld, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("error-retrieving-logical-device", log.Fields{"error": err})
		return err
	}
	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	if cloned.Ports == nil {
		cloned.Ports = make([]*voltha.LogicalPort, 0)
	}
	cloned.Ports = append(cloned.Ports, lp)

	if err = agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
		log.Errorw("error-updating-logical-device", log.Fields{"error": err})
		return err
	}
	return nil
}

func (agent *LogicalDeviceAgent) portExist (device *voltha.Device, port *voltha.Port) bool {
	if ldevice, _ := agent.GetLogicalDevice(); ldevice != nil {
		for _, lPort := range ldevice.Ports {
			if lPort.DeviceId == device.Id && lPort.DevicePortNo == port.PortNo {
				if lPort.OfpPort != nil && device.ProxyAddress != nil {
					return lPort.OfpPort.PortNo == device.ProxyAddress.ChannelId
				} else if lPort.OfpPort != nil || device.ProxyAddress != nil {
					return false
				}
				return true
			}
		}
	}
	return false
}

func (agent *LogicalDeviceAgent) addUNILogicalPort (childDevice *voltha.Device, port *voltha.Port)  error {
	log.Debugw("addUNILogicalPort", log.Fields{"port": port})
	if agent.portExist(childDevice, port) {
		log.Debugw("port-already-exist", log.Fields{"port": port})
		return nil
	}
	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(nil, childDevice.Id, port.PortNo); err != nil {
		log.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return err
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get stored logical device
	if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		return status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		log.Debugw("adding-uni", log.Fields{"deviceId": childDevice.Id})
		portCap.Port.RootPort = false
		//TODO: For now use the channel id assigned by the OLT as logical port number
		lPortNo := childDevice.ProxyAddress.ChannelId
		portCap.Port.Id = fmt.Sprintf("uni-%d", lPortNo)
		portCap.Port.OfpPort.PortNo = lPortNo
		portCap.Port.OfpPort.Name = portCap.Port.Id
		portCap.Port.DeviceId = childDevice.Id
		portCap.Port.DevicePortNo = port.PortNo
		cloned := (proto.Clone(ldevice)).(*voltha.LogicalDevice)
		if cloned.Ports == nil {
			cloned.Ports = make([]*voltha.LogicalPort, 0)
		}
		cloned.Ports = append(cloned.Ports, portCap.Port)
		return agent.updateLogicalDeviceWithoutLock(cloned)
	}
}

func (agent *LogicalDeviceAgent) packetOut(packet *ofp.OfpPacketOut) {
	log.Debugw("packet-out", log.Fields{"packet": packet.GetInPort()})
	outPort := fd.GetPacketOutPort(packet)
	//frame := packet.GetData()
	//TODO: Use a channel between the logical agent and the device agent
	if err := agent.deviceMgr.packetOut(agent.rootDeviceId, outPort, packet); err != nil {
		log.Error("packetout-failed", log.Fields{"logicalDeviceID":agent.rootDeviceId})
	}
}

func (agent *LogicalDeviceAgent) packetIn(port uint32, transactionId string, packet []byte) {
	log.Debugw("packet-in", log.Fields{"port": port, "packet": packet, "transactionId": transactionId})
	packetIn := fd.MkPacketIn(port, packet)
	agent.ldeviceMgr.grpcNbiHdlr.sendPacketIn(agent.logicalDeviceId, transactionId, packetIn)
	log.Debugw("sending-packet-in", log.Fields{"packet-in": packetIn})
}
