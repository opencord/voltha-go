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
	fd "github.com/opencord/voltha-go/rw_core/flow_decomposition"
	"github.com/opencord/voltha-go/rw_core/graph"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	ofp "github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"sync"
)

type LogicalDeviceAgent struct {
	logicalDeviceId    string
	rootDeviceId       string
	deviceMgr          *DeviceManager
	ldeviceMgr         *LogicalDeviceManager
	clusterDataProxy   *model.Proxy
	exitChannel        chan int
	deviceGraph        *graph.DeviceGraph
	flowProxy          *model.Proxy
	groupProxy         *model.Proxy
	ldProxy            *model.Proxy
	portProxies        map[string]*model.Proxy
	portProxiesLock    sync.RWMutex
	lockLogicalDevice  sync.RWMutex
	logicalPortsNo     map[uint32]bool //value is true for NNI port
	lockLogicalPortsNo sync.RWMutex
	flowDecomposer     *fd.FlowDecomposer
	defaultTimeout     int64
}

func newLogicalDeviceAgent(id string, deviceId string, ldeviceMgr *LogicalDeviceManager,
	deviceMgr *DeviceManager,
	cdProxy *model.Proxy, timeout int64) *LogicalDeviceAgent {
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
	agent.lockLogicalPortsNo = sync.RWMutex{}
	agent.logicalPortsNo = make(map[uint32]bool)
	agent.defaultTimeout = timeout
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

		// TODO:  Set the logical ports in a separate call once the port update issue is fixed.
		go agent.setupLogicalPorts(ctx)

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

		// Setup the local list of logical ports
		agent.addLogicalPortsToMap(ld.Ports)

		// Setup the device graph
		agent.generateDeviceGraph()
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	agent.flowProxy = agent.clusterDataProxy.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/flows", agent.logicalDeviceId),
		false)
	agent.groupProxy = agent.clusterDataProxy.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/flow_groups", agent.logicalDeviceId),
		false)
	agent.ldProxy = agent.clusterDataProxy.CreateProxy(
		fmt.Sprintf("/logical_devices/%s", agent.logicalDeviceId),
		false)

	// TODO:  Use a port proxy once the POST_ADD is fixed
	if agent.ldProxy != nil {
		agent.ldProxy.RegisterCallback(model.POST_UPDATE, agent.portUpdated)
	} else {
		log.Errorw("logical-device-proxy-null", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return status.Error(codes.Internal, "logical-device-proxy-null")
	}

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
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDeviceFlows() (*ofp.Flows, error) {
	log.Debug("ListLogicalDeviceFlows")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cFlows := (proto.Clone(lDevice.Flows)).(*ofp.Flows)
		return cFlows, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDeviceFlowGroups() (*ofp.FlowGroups, error) {
	log.Debug("ListLogicalDeviceFlowGroups")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cFlowGroups := (proto.Clone(lDevice.FlowGroups)).(*ofp.FlowGroups)
		return cFlowGroups, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDevicePorts() (*voltha.LogicalPorts, error) {
	log.Debug("ListLogicalDevicePorts")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
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
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.Flows.Items
	}
	return nil
}

// listFlowGroups locks the logical device model and then retrieves the latest flow groups information
func (agent *LogicalDeviceAgent) listFlowGroups() []*ofp.OfpGroupEntry {
	log.Debug("listFlowGroups")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
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
		//log.Debug("getLogicalDeviceWithoutLock", log.Fields{"ldevice": lDevice})
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) updateLogicalPort(device *voltha.Device, port *voltha.Port) error {
	log.Debugw("updateLogicalPort", log.Fields{"deviceId": device.Id, "port": port})
	var err error
	if port.Type == voltha.Port_ETHERNET_NNI {
		if _, err = agent.addNNILogicalPort(device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, true)
	} else if port.Type == voltha.Port_ETHERNET_UNI {
		if _, err = agent.addUNILogicalPort(device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, false)
	} else {
		// Update the device graph to ensure all routes on the logical device have been calculated
		if err = agent.updateRoutes(device, port); err != nil {
			log.Errorw("failed-to-update-routes", log.Fields{"deviceId": device.Id, "port": port, "error": err})
			return err
		}
	}
	return nil
}

func (agent *LogicalDeviceAgent) addLogicalPort(device *voltha.Device, port *voltha.Port) error {
	log.Debugw("addLogicalPort", log.Fields{"deviceId": device.Id, "port": port})
	var err error
	if port.Type == voltha.Port_ETHERNET_NNI {
		if _, err = agent.addNNILogicalPort(device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, true)
	} else if port.Type == voltha.Port_ETHERNET_UNI {
		if _, err = agent.addUNILogicalPort(device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, false)
	} else {
		log.Debugw("invalid-port-type", log.Fields{"deviceId": device.Id, "port": port})
		return nil
	}
	return nil
}

// setupLogicalPorts is invoked once the logical device has been created and is ready to get ports
// added to it.  While the logical device was being created we could have received requests to add
// NNI and UNI ports which were discarded.  Now is the time to add them if needed
func (agent *LogicalDeviceAgent) setupLogicalPorts(ctx context.Context) error {
	log.Infow("setupLogicalPorts", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// First add any NNI ports which could have been missing
	if err := agent.setupNNILogicalPorts(nil, agent.rootDeviceId); err != nil {
		log.Errorw("error-setting-up-NNI-ports", log.Fields{"error": err, "deviceId": agent.rootDeviceId})
		return err
	}

	// Now, set up the UNI ports if needed.
	if children, err := agent.deviceMgr.getAllChildDevices(agent.rootDeviceId); err != nil {
		log.Errorw("error-getting-child-devices", log.Fields{"error": err, "deviceId": agent.rootDeviceId})
		return err
	} else {
		chnlsList := make([]chan interface{}, 0)
		for _, child := range children.Items {
			ch := make(chan interface{})
			chnlsList = append(chnlsList, ch)
			go func(device *voltha.Device, ch chan interface{}) {
				if err = agent.setupUNILogicalPorts(nil, device); err != nil {
					log.Error("setting-up-UNI-ports-failed", log.Fields{"deviceID": device.Id})
					ch <- status.Errorf(codes.Internal, "UNI-ports-setup-failed: %s", device.Id)
				}
				ch <- nil
			}(child, ch)
		}
		// Wait for completion
		if res := fu.WaitForNilOrErrorResponses(agent.defaultTimeout, chnlsList...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	}
	return nil
}

// setupNNILogicalPorts creates an NNI port on the logical device that represents an NNI interface on a root device
func (agent *LogicalDeviceAgent) setupNNILogicalPorts(ctx context.Context, deviceId string) error {
	log.Infow("setupNNILogicalPorts-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// Build the logical device based on information retrieved from the device adapter
	var err error

	var device *voltha.Device
	if device, err = agent.deviceMgr.GetDevice(deviceId); err != nil {
		log.Errorw("error-retrieving-device", log.Fields{"error": err, "deviceId": deviceId})
		return err
	}

	//Get UNI port number
	for _, port := range device.Ports {
		if port.Type == voltha.Port_ETHERNET_NNI {
			if _, err = agent.addNNILogicalPort(device, port); err != nil {
				log.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			agent.addLogicalPortToMap(port.PortNo, true)
		}
	}
	return err
}

// updatePortsState updates the ports state related to the device
func (agent *LogicalDeviceAgent) updatePortsState(device *voltha.Device, state voltha.AdminState_AdminState) error {
	log.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device info
	if ld, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Warnw("logical-device-unknown", log.Fields{"ldeviceId": agent.logicalDeviceId, "error": err})
		return err
	} else {
		cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
		for _, lport := range cloned.Ports {
			if lport.DeviceId == device.Id {
				switch state {
				case voltha.AdminState_ENABLED:
					lport.OfpPort.Config = lport.OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
					lport.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
				case voltha.AdminState_DISABLED:
					lport.OfpPort.Config = lport.OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
					lport.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
				default:
					log.Warnw("unsupported-state-change", log.Fields{"deviceId": device.Id, "state": state})
				}
			}
		}
		// Updating the logical device will trigger the poprt change events to be populated to the controller
		if err := agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
			log.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceId, "error": err})
			return err
		}
	}
	return nil
}

// setupUNILogicalPorts creates a UNI port on the logical device that represents a child UNI interface
func (agent *LogicalDeviceAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	log.Infow("setupUNILogicalPort", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	// Build the logical device based on information retrieved from the device adapter
	var err error

	//Get UNI port number
	for _, port := range childDevice.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			if _, err = agent.addUNILogicalPort(childDevice, port); err != nil {
				log.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			agent.addLogicalPortToMap(port.PortNo, false)
		}
	}
	return err
}

// deleteAllLogicalPorts deletes all logical ports associated with this device
func (agent *LogicalDeviceAgent) deleteAllLogicalPorts(device *voltha.Device) error {
	log.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device info
	if ld, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Warnw("logical-device-unknown", log.Fields{"ldeviceId": agent.logicalDeviceId, "error": err})
		return err
	} else {
		cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
		updateLogicalPorts := []*voltha.LogicalPort{}
		for _, lport := range cloned.Ports {
			if lport.DeviceId != device.Id {
				updateLogicalPorts = append(updateLogicalPorts, lport)
			}
		}
		if len(updateLogicalPorts) < len(cloned.Ports) {
			cloned.Ports = updateLogicalPorts
			// Updating the logical device will trigger the poprt change events to be populated to the controller
			if err := agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
				log.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceId, "error": err})
				return err
			}
		} else {
			log.Debugw("no-change-required", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		}
	}
	return nil
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceWithoutLock(logicalDevice *voltha.LogicalDevice) error {
	afterUpdate := agent.clusterDataProxy.Update("/logical_devices/"+agent.logicalDeviceId, logicalDevice, false, "")
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

	updatedFlows := make([]*ofp.OfpFlowStats, 0)
	//oldData := proto.Clone(lDevice.Flows).(*voltha.Flows)
	changed := false
	checkOverlap := (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_CHECK_OVERLAP)) != 0
	if checkOverlap {
		if overlapped := fu.FindOverlappingFlows(flows, mod); len(overlapped) != 0 {
			//	TODO:  should this error be notified other than being logged?
			log.Warnw("overlapped-flows", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
		} else {
			//	Add flow
			flow := fu.FlowStatsEntryFromFlowModMessage(mod)
			flows = append(flows, flow)
			updatedFlows = append(updatedFlows, flow)
			changed = true
		}
	} else {
		flow := fu.FlowStatsEntryFromFlowModMessage(mod)
		idx := fu.FindFlows(flows, flow)
		if idx >= 0 {
			oldFlow := flows[idx]
			if (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_RESET_COUNTS)) != 0 {
				flow.ByteCount = oldFlow.ByteCount
				flow.PacketCount = oldFlow.PacketCount
			}
			if !reflect.DeepEqual(oldFlow, flow) {
				flows[idx] = flow
				updatedFlows = append(updatedFlows, flow)
				changed = true
			}
		} else {
			flows = append(flows, flow)
			updatedFlows = append(updatedFlows, flow)
			changed = true
		}
	}
	if changed {
		// Launch a routine to decompose the flows
		if err := agent.decomposeAndSendFlows(&ofp.Flows{Items: updatedFlows}, lDevice.FlowGroups); err != nil {
			log.Errorw("decomposing-and-sending-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}

		//	Update model
		flowsToUpdate := &ofp.Flows{}
		if lDevice.Flows != nil {
			flowsToUpdate = &ofp.Flows{Items: flows}
		}
		if err := agent.updateLogicalDeviceFlowsWithoutLock(flowsToUpdate); err != nil {
			log.Errorw("db-flow-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
	}
	return nil
}

func (agent *LogicalDeviceAgent) decomposeAndSendFlows(flows *ofp.Flows, groups *ofp.FlowGroups) error {
	log.Debugw("decomposeAndSendFlows", log.Fields{"logicalDeviceID": agent.logicalDeviceId})

	deviceRules := agent.flowDecomposer.DecomposeRules(agent, *flows, *groups)
	log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

	chnlsList := make([]chan interface{}, 0)
	for deviceId, value := range deviceRules.GetRules() {
		ch := make(chan interface{})
		chnlsList = append(chnlsList, ch)
		go func(deviceId string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry) {
			if err := agent.deviceMgr.addFlowsAndGroups(deviceId, flows, groups); err != nil {
				log.Error("flow-update-failed", log.Fields{"deviceID": deviceId})
				ch <- status.Errorf(codes.Internal, "flow-update-failed: %s", deviceId)
			}
			ch <- nil
		}(deviceId, value.ListFlows(), value.ListGroups())
	}
	// Wait for completion
	if res := fu.WaitForNilOrErrorResponses(agent.defaultTimeout, chnlsList...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
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
	flowsToDelete := make([]*ofp.OfpFlowStats, 0)
	flows := lDevice.Flows.Items
	changed := false
	flow := fu.FlowStatsEntryFromFlowModMessage(mod)
	idx := fu.FindFlows(flows, flow)
	if idx >= 0 {
		flowsToDelete = append(flowsToDelete, flows[idx])
		flows = append(flows[:idx], flows[idx+1:]...)
		changed = true
	} else {
		return errors.New(fmt.Sprintf("Cannot delete flow - %s", flow))
	}

	if changed {
		// Decompose flows to be deleted and send to adapter
		if err := agent.decomposeAndSendFlows(&ofp.Flows{Items: flowsToDelete}, lDevice.FlowGroups); err != nil {
			log.Errorw("decomposing-and-sending-flows failed", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
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
		groups = append(groups, fu.GroupEntryFromGroupMod(groupMod))
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
		groupEntry := fu.GroupEntryFromGroupMod(groupMod)
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
		if err := agent.updateLogicalDeviceWithoutLock(logicaldevice); err != nil {
			log.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
			return err
		}
		// Reset the logical device graph
		go agent.generateDeviceGraph()
	}
	return nil
}

// deleteLogicalPorts removes the logical ports associated with that deviceId
func (agent *LogicalDeviceAgent) deleteLogicalPorts(deviceId string) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return nil
	}
	updatedLPorts := []*voltha.LogicalPort{}
	for _, logicalPort := range logicaldevice.Ports {
		if logicalPort.DeviceId != deviceId {
			updatedLPorts = append(updatedLPorts, logicalPort)
		}
	}
	logicaldevice.Ports = updatedLPorts
	log.Debugw("updated-logical-ports", log.Fields{"ports": updatedLPorts})
	if err := agent.updateLogicalDeviceWithoutLock(logicaldevice); err != nil {
		log.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
		return err
	}
	// Reset the logical device graph
	go agent.generateDeviceGraph()

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
	routes := make([]graph.RouteHop, 0)

	// Note: A port value of 0 is equivalent to a nil port

	//	Consider different possibilities
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		log.Debugw("controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
		if agent.isNNIPort(ingressPortNo) {
			log.Debug("returning-half-route")
			//This is a trap on the NNI Port
			if len(agent.deviceGraph.Routes) == 0 {
				// If there are no routes set (usually when the logical device has only NNI port(s), then just return an
				// internal route
				hop := graph.RouteHop{DeviceID: agent.rootDeviceId, Ingress: ingressPortNo, Egress: egressPortNo}
				routes = append(routes, hop)
				routes = append(routes, hop)
				return routes
			}
			//Return a 'half' route to make the flow decomposer logic happy
			for routeLink, route := range agent.deviceGraph.Routes {
				if agent.isNNIPort(routeLink.Egress) {
					routes = append(routes, graph.RouteHop{}) // first hop is set to empty
					routes = append(routes, route[1])
					return routes
				}
			}
			log.Warnw("no-upstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
			return nil
		}
		//treat it as if the output port is the first NNI of the OLT
		var err error
		if egressPortNo, err = agent.getFirstNNIPort(); err != nil {
			log.Warnw("no-nni-port", log.Fields{"error": err})
			return nil
		}
	}
	//If ingress port is not specified (nil), it may be a wildcarded
	//route if egress port is OFPP_CONTROLLER or a nni logical port,
	//in which case we need to create a half-route where only the egress
	//hop is filled, the first hop is nil
	if ingressPortNo == 0 && agent.isNNIPort(egressPortNo) {
		// We can use the 2nd hop of any upstream route, so just find the first upstream:
		for routeLink, route := range agent.deviceGraph.Routes {
			if agent.isNNIPort(routeLink.Egress) {
				routes = append(routes, graph.RouteHop{}) // first hop is set to empty
				routes = append(routes, route[1])
				return routes
			}
		}
		log.Warnw("no-upstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
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
		log.Warnw("no-downstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
		return nil
	}
	//	Return the pre-calculated route
	return agent.getPreCalculatedRoute(ingressPortNo, egressPortNo)
}

//GetWildcardInputPorts filters out the logical port number from the set of logical ports on the device and
//returns their port numbers.  This function is invoked only during flow decomposition where the lock on the logical
//device is already held.  Therefore it is safe to retrieve the logical device without lock.
func (agent *LogicalDeviceAgent) GetWildcardInputPorts(excludePort ...uint32) []uint32 {
	lPorts := make([]uint32, 0)
	var exclPort uint32
	if len(excludePort) == 1 {
		exclPort = excludePort[0]
	}
	if lDevice, _ := agent.getLogicalDeviceWithoutLock(); lDevice != nil {
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

//updateRoutes rebuilds the device graph if not done already
func (agent *LogicalDeviceAgent) updateRoutes(device *voltha.Device, port *voltha.Port) error {
	log.Debugf("updateRoutes", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "device": device.Id, "port": port})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	if agent.deviceGraph == nil {
		agent.deviceGraph = graph.NewDeviceGraph(agent.logicalDeviceId, agent.deviceMgr.GetDevice)
	}
	// Get all the logical ports on that logical device
	if lDevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("unknown-logical-device", log.Fields{"error": err, "logicalDeviceId": agent.logicalDeviceId})
		return err
	} else {
		//TODO:  Find a better way to refresh only missing routes
		agent.deviceGraph.ComputeRoutes(lDevice.Ports)
	}
	agent.deviceGraph.Print()
	return nil
}

//updateDeviceGraph updates the device graph if not done already and setup the default rules as well
func (agent *LogicalDeviceAgent) updateDeviceGraph(lp *voltha.LogicalPort) {
	log.Debugf("updateDeviceGraph", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	if agent.deviceGraph == nil {
		agent.deviceGraph = graph.NewDeviceGraph(agent.logicalDeviceId, agent.deviceMgr.GetDevice)
	}
	agent.deviceGraph.AddPort(lp)
	agent.deviceGraph.Print()
}

//generateDeviceGraph regenerates the device graph
func (agent *LogicalDeviceAgent) generateDeviceGraph() {
	log.Debugf("generateDeviceGraph", log.Fields{"logicalDeviceId": agent.logicalDeviceId})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device
	if ld, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("logical-device-not-present", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "error": err})
	} else {
		log.Debugw("generating-graph", log.Fields{"lDeviceId": agent.logicalDeviceId, "deviceGraph": agent.deviceGraph, "lPorts": len(ld.Ports)})
		if agent.deviceGraph == nil {
			agent.deviceGraph = graph.NewDeviceGraph(agent.logicalDeviceId, agent.deviceMgr.GetDevice)
		}
		agent.deviceGraph.ComputeRoutes(ld.Ports)
		agent.deviceGraph.Print()
	}
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
	agent.portProxies[port.Id] = agent.clusterDataProxy.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/ports/%s", agent.logicalDeviceId, port.Id),
		false)
	agent.portProxies[port.Id].RegisterCallback(model.POST_UPDATE, agent.portUpdated)
	agent.portProxiesLock.Unlock()

	// Send the port change event to the OF controller
	agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
		&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_ADD, Desc: port.OfpPort})

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
		&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_DELETE, Desc: port.OfpPort})

	return nil
}

// diff go over two lists of logical ports and return what's new, what's changed and what's removed.
func diff(oldList, newList []*voltha.LogicalPort) (newPorts, changedPorts, deletedPorts []*voltha.LogicalPort) {
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
	for _, newP := range newPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_ADD, Desc: newP.OfpPort})
	}
	for _, change := range changedPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_MODIFY, Desc: change.OfpPort})
	}
	for _, del := range deletedPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceId,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_DELETE, Desc: del.OfpPort})
	}

	return nil
}

// addNNILogicalPort adds an NNI port to the logical device.  It returns a bool representing whether a port has been
// added and an eror in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalDeviceAgent) addNNILogicalPort(device *voltha.Device, port *voltha.Port) (bool, error) {
	log.Debugw("addNNILogicalPort", log.Fields{"NNI": port})
	if device.AdminState != voltha.AdminState_ENABLED || device.OperStatus != voltha.OperStatus_ACTIVE {
		log.Infow("device-not-ready", log.Fields{"deviceId": device.Id, "admin": device.AdminState, "oper": device.OperStatus})
		return false, nil
	}
	agent.lockLogicalDevice.RLock()
	if agent.portExist(device, port) {
		log.Debugw("port-already-exist", log.Fields{"port": port})
		agent.lockLogicalDevice.RUnlock()
		return false, nil
	}
	agent.lockLogicalDevice.RUnlock()

	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(nil, device.Id, port.PortNo); err != nil {
		log.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}

	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Double check again if this port has been already added since the getPortCapability could have taken a long time
	if agent.portExist(device, port) {
		log.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	portCap.Port.RootPort = true
	lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
	lp.DeviceId = device.Id
	lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
	lp.OfpPort.PortNo = port.PortNo
	lp.OfpPort.Name = lp.Id
	lp.DevicePortNo = port.PortNo

	var ld *voltha.LogicalDevice
	if ld, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("error-retrieving-logical-device", log.Fields{"error": err})
		return false, err
	}
	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	if cloned.Ports == nil {
		cloned.Ports = make([]*voltha.LogicalPort, 0)
	}
	cloned.Ports = append(cloned.Ports, lp)

	if err = agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
		log.Errorw("error-updating-logical-device", log.Fields{"error": err})
		return false, err
	}

	// Update the device graph with this new logical port
	clonedLP := (proto.Clone(lp)).(*voltha.LogicalPort)
	go agent.updateDeviceGraph(clonedLP)

	return true, nil
}

func (agent *LogicalDeviceAgent) portExist(device *voltha.Device, port *voltha.Port) bool {
	if ldevice, _ := agent.getLogicalDeviceWithoutLock(); ldevice != nil {
		for _, lPort := range ldevice.Ports {
			if lPort.DeviceId == device.Id && lPort.DevicePortNo == port.PortNo && lPort.Id == port.Label {
				return true
			}
		}
	}
	return false
}

// addUNILogicalPort adds an UNI port to the logical device.  It returns a bool representing whether a port has been
// added and an eror in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalDeviceAgent) addUNILogicalPort(childDevice *voltha.Device, port *voltha.Port) (bool, error) {
	log.Debugw("addUNILogicalPort", log.Fields{"port": port})
	if childDevice.AdminState != voltha.AdminState_ENABLED || childDevice.OperStatus != voltha.OperStatus_ACTIVE {
		log.Infow("device-not-ready", log.Fields{"deviceId": childDevice.Id, "admin": childDevice.AdminState, "oper": childDevice.OperStatus})
		return false, nil
	}
	agent.lockLogicalDevice.RLock()
	if agent.portExist(childDevice, port) {
		log.Debugw("port-already-exist", log.Fields{"port": port})
		agent.lockLogicalDevice.RUnlock()
		return false, nil
	}
	agent.lockLogicalDevice.RUnlock()
	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(nil, childDevice.Id, port.PortNo); err != nil {
		log.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Double check again if this port has been already added since the getPortCapability could have taken a long time
	if agent.portExist(childDevice, port) {
		log.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}
	// Get stored logical device
	if ldevice, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		return false, status.Error(codes.NotFound, agent.logicalDeviceId)
	} else {
		log.Debugw("adding-uni", log.Fields{"deviceId": childDevice.Id})
		portCap.Port.RootPort = false
		portCap.Port.Id = port.Label
		portCap.Port.OfpPort.PortNo = port.PortNo
		portCap.Port.DeviceId = childDevice.Id
		portCap.Port.DevicePortNo = port.PortNo
		cloned := (proto.Clone(ldevice)).(*voltha.LogicalDevice)
		if cloned.Ports == nil {
			cloned.Ports = make([]*voltha.LogicalPort, 0)
		}
		cloned.Ports = append(cloned.Ports, portCap.Port)
		if err := agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
			return false, err
		}
		// Update the device graph with this new logical port
		clonedLP := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
		go agent.updateDeviceGraph(clonedLP)
		return true, nil
	}
}

func (agent *LogicalDeviceAgent) packetOut(packet *ofp.OfpPacketOut) {
	log.Debugw("packet-out", log.Fields{"packet": packet.GetInPort()})
	outPort := fu.GetPacketOutPort(packet)
	//frame := packet.GetData()
	//TODO: Use a channel between the logical agent and the device agent
	if err := agent.deviceMgr.packetOut(agent.rootDeviceId, outPort, packet); err != nil {
		log.Error("packetout-failed", log.Fields{"logicalDeviceID": agent.rootDeviceId})
	}
}

func (agent *LogicalDeviceAgent) packetIn(port uint32, transactionId string, packet []byte) {
	log.Debugw("packet-in", log.Fields{"port": port, "packet": packet, "transactionId": transactionId})
	packetIn := fu.MkPacketIn(port, packet)
	agent.ldeviceMgr.grpcNbiHdlr.sendPacketIn(agent.logicalDeviceId, transactionId, packetIn)
	log.Debugw("sending-packet-in", log.Fields{"packet-in": packetIn})
}

func (agent *LogicalDeviceAgent) addLogicalPortToMap(portNo uint32, nniPort bool) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	if exist := agent.logicalPortsNo[portNo]; !exist {
		agent.logicalPortsNo[portNo] = nniPort
	}
}

func (agent *LogicalDeviceAgent) addLogicalPortsToMap(lps []*voltha.LogicalPort) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, lp := range lps {
		if exist := agent.logicalPortsNo[lp.DevicePortNo]; !exist {
			agent.logicalPortsNo[lp.DevicePortNo] = lp.RootPort
		}
	}
}

func (agent *LogicalDeviceAgent) deleteLogicalPortFromMap(portNo uint32) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	if exist := agent.logicalPortsNo[portNo]; exist {
		delete(agent.logicalPortsNo, portNo)
	}
}

func (agent *LogicalDeviceAgent) isNNIPort(portNo uint32) bool {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	if exist := agent.logicalPortsNo[portNo]; exist {
		return agent.logicalPortsNo[portNo]
	}
	return false
}

func (agent *LogicalDeviceAgent) getFirstNNIPort() (uint32, error) {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			return portNo, nil
		}
	}
	return 0, status.Error(codes.NotFound, "No NNI port found")
}
