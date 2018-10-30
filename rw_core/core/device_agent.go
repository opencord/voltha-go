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
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/protos/core_adapter"
	ofp "github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	fu "github.com/opencord/voltha-go/rw_core/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"sync"
)

type DeviceAgent struct {
	deviceId         string
	lastData         *voltha.Device
	adapterProxy     *AdapterProxy
	deviceMgr        *DeviceManager
	clusterDataProxy *model.Proxy
	deviceProxy      *model.Proxy
	exitChannel      chan int
	flowProxy        *model.Proxy
	groupProxy       *model.Proxy
	lockDevice       sync.RWMutex
}

//newDeviceAgent creates a new device agent along as creating a unique ID for the device and set the device state to
//preprovisioning
func newDeviceAgent(ap *AdapterProxy, device *voltha.Device, deviceMgr *DeviceManager, cdProxy *model.Proxy) *DeviceAgent {
	var agent DeviceAgent
	agent.adapterProxy = ap
	cloned := (proto.Clone(device)).(*voltha.Device)
	cloned.Id = CreateDeviceId()
	cloned.AdminState = voltha.AdminState_PREPROVISIONED
	cloned.FlowGroups = &ofp.FlowGroups{Items: nil}
	cloned.Flows = &ofp.Flows{Items: nil}
	if !device.GetRoot() && device.ProxyAddress != nil {
		// Set the default vlan ID to the one specified by the parent adapter.  It can be
		// overwritten by the child adapter during a device update request
		cloned.Vlan = device.ProxyAddress.ChannelId
	}
	agent.deviceId = cloned.Id
	agent.lastData = cloned
	agent.deviceMgr = deviceMgr
	agent.exitChannel = make(chan int, 1)
	agent.clusterDataProxy = cdProxy
	agent.lockDevice = sync.RWMutex{}
	return &agent
}

// start save the device to the data model and registers for callbacks on that device
func (agent *DeviceAgent) start(ctx context.Context) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("starting-device-agent", log.Fields{"device": agent.lastData})
	// Add the initial device to the local model
	if added := agent.clusterDataProxy.Add("/devices", agent.lastData, ""); added == nil {
		log.Errorw("failed-to-add-device", log.Fields{"deviceId": agent.deviceId})
	}
	agent.deviceProxy = agent.clusterDataProxy.Root.GetProxy("/devices/"+agent.deviceId, false)
	agent.deviceProxy.RegisterCallback(model.POST_UPDATE, agent.processUpdate, nil)

	agent.flowProxy = agent.clusterDataProxy.Root.GetProxy(
		fmt.Sprintf("/devices/%s/flows", agent.deviceId),
		false)
	agent.groupProxy = agent.clusterDataProxy.Root.GetProxy(
		fmt.Sprintf("/devices/%s/flow_groups", agent.deviceId),
		false)

	agent.flowProxy.RegisterCallback(model.POST_UPDATE, agent.flowTableUpdated)
	//agent.groupProxy.RegisterCallback(model.POST_UPDATE, agent.groupTableUpdated)

	log.Debug("device-agent-started")
}

// stop stops the device agent.  Not much to do for now
func (agent *DeviceAgent) stop(ctx context.Context) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debug("stopping-device-agent")
	agent.exitChannel <- 1
	log.Debug("device-agent-stopped")
}

// GetDevice retrieves the latest device information from the data model
func (agent *DeviceAgent) getDevice() (*voltha.Device, error) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	if device := agent.clusterDataProxy.Get("/devices/"+agent.deviceId, 1, false, ""); device != nil {
		if d, ok := device.(*voltha.Device); ok {
			cloned := proto.Clone(d).(*voltha.Device)
			return cloned, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// getDeviceWithoutLock is a helper function to be used ONLY by any device agent function AFTER it has acquired the device lock.
// This function is meant so that we do not have duplicate code all over the device agent functions
func (agent *DeviceAgent) getDeviceWithoutLock() (*voltha.Device, error) {
	if device := agent.clusterDataProxy.Get("/devices/"+agent.deviceId, 1, false, ""); device != nil {
		if d, ok := device.(*voltha.Device); ok {
			cloned := proto.Clone(d).(*voltha.Device)
			return cloned, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "device-%s", agent.deviceId)
}

// enableDevice activates a preprovisioned or disable device
func (agent *DeviceAgent) enableDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("enableDevice", log.Fields{"id": agent.deviceId})
	if device, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		if device.AdminState == voltha.AdminState_ENABLED {
			log.Debugw("device-already-enabled", log.Fields{"id": agent.deviceId})
			//TODO:  Needs customized error message
			return nil
		}
		//TODO: if parent device is disabled then do not enable device
		// Verify whether we need to adopt the device the first time
		// TODO: A state machine for these state transitions would be better (we just have to handle
		// a limited set of states now or it may be an overkill)
		if device.AdminState == voltha.AdminState_PREPROVISIONED {
			// First send the request to an Adapter and wait for a response
			if err := agent.adapterProxy.AdoptDevice(ctx, device); err != nil {
				log.Debugw("adoptDevice-error", log.Fields{"id": agent.lastData.Id, "error": err})
				return err
			}
		} else {
			// First send the request to an Adapter and wait for a response
			if err := agent.adapterProxy.ReEnableDevice(ctx, device); err != nil {
				log.Debugw("renableDevice-error", log.Fields{"id": agent.lastData.Id, "error": err})
				return err
			}
		}
		// Received an Ack (no error found above).  Now update the device in the model to the expected state
		cloned := proto.Clone(device).(*voltha.Device)
		cloned.AdminState = voltha.AdminState_ENABLED
		cloned.OperStatus = voltha.OperStatus_ACTIVATING
		if afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, ""); afterUpdate == nil {
			return status.Errorf(codes.Internal, "failed-update-device:%s", agent.deviceId)
		}
	}
	return nil
}

func (agent *DeviceAgent) updateFlows(flows []*ofp.OfpFlowStats) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("updateFlows", log.Fields{"deviceId": agent.deviceId, "flows": flows})
	var oldData *voltha.Flows
	if storedData, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		oldData = proto.Clone(storedData.Flows).(*voltha.Flows)
		log.Debugw("updateFlows", log.Fields{"deviceId": agent.deviceId, "flows": flows, "old": oldData})
		// store the changed data
		storedData.Flows.Items = flows
		afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, storedData, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}

		// For now, force the callback to occur
		go agent.flowTableUpdated(oldData, &ofp.Flows{Items: flows})
		return nil
	}
}

func (agent *DeviceAgent) updateGroups(groups []*ofp.OfpGroupEntry) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	var oldData *voltha.FlowGroups
	log.Debugw("updateGroups", log.Fields{"deviceId": agent.deviceId, "groups": groups})
	if storedData, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		oldData = proto.Clone(storedData.FlowGroups).(*voltha.FlowGroups)
		// store the changed data
		storedData.FlowGroups.Items = groups
		afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, storedData, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}

		// For now, force the callback to occur
		go agent.groupTableUpdated(oldData, &ofp.FlowGroups{Items: groups})
		return nil
	}
}

//disableDevice disable a device
func (agent *DeviceAgent) disableDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	//defer agent.lockDevice.Unlock()
	log.Debugw("disableDevice", log.Fields{"id": agent.deviceId})
	// Get the most up to date the device info
	if device, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		if device.AdminState == voltha.AdminState_DISABLED {
			log.Debugw("device-already-disabled", log.Fields{"id": agent.deviceId})
			//TODO:  Needs customized error message
			agent.lockDevice.Unlock()
			return nil
		}
		// First send the request to an Adapter and wait for a response
		if err := agent.adapterProxy.DisableDevice(ctx, device); err != nil {
			log.Debugw("disableDevice-error", log.Fields{"id": agent.lastData.Id, "error": err})
			agent.lockDevice.Unlock()
			return err
		}
		// Received an Ack (no error found above).  Now update the device in the model to the expected state
		cloned := proto.Clone(device).(*voltha.Device)
		cloned.AdminState = voltha.AdminState_DISABLED
		// Set the state of all ports on that device to disable
		for _, port := range cloned.Ports {
			port.AdminState = voltha.AdminState_DISABLED
			port.OperStatus = voltha.OperStatus_UNKNOWN
		}
		if afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, ""); afterUpdate == nil {
			agent.lockDevice.Unlock()
			return status.Errorf(codes.Internal, "failed-update-device:%s", agent.deviceId)
		}
		agent.lockDevice.Unlock()
		//TODO: callback will be invoked to handle this state change
		//For now force the state transition to happen
		if err := agent.deviceMgr.processTransition(device, cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": device.Id, "error": err})
			return err
		}
	}
	return nil
}

func (agent *DeviceAgent) rebootDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("rebootDevice", log.Fields{"id": agent.deviceId})
	// Get the most up to date the device info
	if device, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		if device.AdminState != voltha.AdminState_DISABLED {
			log.Debugw("device-not-disabled", log.Fields{"id": agent.deviceId})
			//TODO:  Needs customized error message
			return status.Errorf(codes.FailedPrecondition, "deviceId:%s, expected-admin-state:%s", agent.deviceId, voltha.AdminState_DISABLED)
		}
		// First send the request to an Adapter and wait for a response
		if err := agent.adapterProxy.RebootDevice(ctx, device); err != nil {
			log.Debugw("rebootDevice-error", log.Fields{"id": agent.lastData.Id, "error": err})
			return err
		}
	}
	return nil
}

func (agent *DeviceAgent) deleteDevice(ctx context.Context) error {
	agent.lockDevice.Lock()
	log.Debugw("deleteDevice", log.Fields{"id": agent.deviceId})
	// Get the most up to date the device info
	if device, err := agent.getDeviceWithoutLock(); err != nil {
		agent.lockDevice.Unlock()
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		if device.AdminState != voltha.AdminState_DISABLED {
			log.Debugw("device-not-disabled", log.Fields{"id": agent.deviceId})
			//TODO:  Needs customized error message
			agent.lockDevice.Unlock()
			return status.Errorf(codes.FailedPrecondition, "deviceId:%s, expected-admin-state:%s", agent.deviceId, voltha.AdminState_DISABLED)
		}
		// Send the request to an Adapter and wait for a response
		if err := agent.adapterProxy.DeleteDevice(ctx, device); err != nil {
			log.Debugw("deleteDevice-error", log.Fields{"id": agent.lastData.Id, "error": err})
			agent.lockDevice.Unlock()
			return err
		}
		//	Set the device Admin state to DELETED in order to trigger the callback to delete
		// child devices, if any
		// Received an Ack (no error found above).  Now update the device in the model to the expected state
		cloned := proto.Clone(device).(*voltha.Device)
		cloned.AdminState = voltha.AdminState_DELETED
		if afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, ""); afterUpdate == nil {
			agent.lockDevice.Unlock()
			return status.Errorf(codes.Internal, "failed-update-device:%s", agent.deviceId)
		}
		agent.lockDevice.Unlock()
		//TODO: callback will be invoked to handle this state change
		//For now force the state transition to happen
		if err := agent.deviceMgr.processTransition(device, cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": device.Id, "error": err})
			return err
		}

	}
	return nil
}

// getPorts retrieves the ports information of the device based on the port type.
func (agent *DeviceAgent) getPorts(ctx context.Context, portType voltha.Port_PortType) *voltha.Ports {
	log.Debugw("getPorts", log.Fields{"id": agent.deviceId, "portType": portType})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.GetDevice(agent.deviceId); device != nil {
		for _, port := range device.Ports {
			if port.Type == portType {
				ports.Items = append(ports.Items, port)
			}
		}
	}
	return ports
}

// getSwitchCapability is a helper method that a logical device agent uses to retrieve the switch capability of a
// parent device
func (agent *DeviceAgent) getSwitchCapability(ctx context.Context) (*core_adapter.SwitchCapability, error) {
	log.Debugw("getSwitchCapability", log.Fields{"deviceId": agent.deviceId})
	if device, err := agent.deviceMgr.GetDevice(agent.deviceId); device == nil {
		return nil, err
	} else {
		var switchCap *core_adapter.SwitchCapability
		var err error
		if switchCap, err = agent.adapterProxy.GetOfpDeviceInfo(ctx, device); err != nil {
			log.Debugw("getSwitchCapability-error", log.Fields{"id": device.Id, "error": err})
			return nil, err
		}
		return switchCap, nil
	}
}

// getPortCapability is a helper method that a logical device agent uses to retrieve the port capability of a
// device
func (agent *DeviceAgent) getPortCapability(ctx context.Context, portNo uint32) (*core_adapter.PortCapability, error) {
	log.Debugw("getPortCapability", log.Fields{"deviceId": agent.deviceId})
	if device, err := agent.deviceMgr.GetDevice(agent.deviceId); device == nil {
		return nil, err
	} else {
		var portCap *core_adapter.PortCapability
		var err error
		if portCap, err = agent.adapterProxy.GetOfpPortInfo(ctx, device, portNo); err != nil {
			log.Debugw("getPortCapability-error", log.Fields{"id": device.Id, "error": err})
			return nil, err
		}
		return portCap, nil
	}
}

// TODO: implement when callback from the data model is ready
// processUpdate is a callback invoked whenever there is a change on the device manages by this device agent
func (agent *DeviceAgent) processUpdate(args ...interface{}) interface{} {
	log.Debug("!!!!!!!!!!!!!!!!!!!!!!!!!")
	log.Debugw("processUpdate", log.Fields{"deviceId": agent.deviceId, "args": args})
	return nil
}

func (agent *DeviceAgent) updateDevice(device *voltha.Device) error {
	agent.lockDevice.Lock()
	//defer agent.lockDevice.Unlock()
	log.Debugw("updateDevice", log.Fields{"deviceId": device.Id})
	// Get the dev info from the model
	if storedData, err := agent.getDeviceWithoutLock(); err != nil {
		agent.lockDevice.Unlock()
		return status.Errorf(codes.NotFound, "%s", device.Id)
	} else {
		// store the changed data
		cloned := proto.Clone(device).(*voltha.Device)
		afterUpdate := agent.clusterDataProxy.Update("/devices/"+device.Id, cloned, false, "")
		agent.lockDevice.Unlock()
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", device.Id)
		}
		// Perform the state transition
		if err := agent.deviceMgr.processTransition(storedData, cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": device.Id, "error": err})
			return err
		}
		return nil
	}
}

func (agent *DeviceAgent) updateDeviceStatus(operStatus voltha.OperStatus_OperStatus, connStatus voltha.ConnectStatus_ConnectStatus) error {
	agent.lockDevice.Lock()
	//defer agent.lockDevice.Unlock()
	// Work only on latest data
	if storeDevice, err := agent.getDeviceWithoutLock(); err != nil {
		agent.lockDevice.Unlock()
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := proto.Clone(storeDevice).(*voltha.Device)
		// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
		if s, ok := voltha.ConnectStatus_ConnectStatus_value[connStatus.String()]; ok {
			log.Debugw("updateDeviceStatus-conn", log.Fields{"ok": ok, "val": s})
			cloned.ConnectStatus = connStatus
		}
		if s, ok := voltha.OperStatus_OperStatus_value[operStatus.String()]; ok {
			log.Debugw("updateDeviceStatus-oper", log.Fields{"ok": ok, "val": s})
			cloned.OperStatus = operStatus
		}
		log.Debugw("updateDeviceStatus", log.Fields{"deviceId": cloned.Id, "operStatus": cloned.OperStatus, "connectStatus": cloned.ConnectStatus})
		// Store the device
		if afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, ""); afterUpdate == nil {
			agent.lockDevice.Unlock()
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		agent.lockDevice.Unlock()
		// Perform the state transition
		if err := agent.deviceMgr.processTransition(storeDevice, cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": agent.deviceId, "error": err})
			return err
		}
		return nil
	}
}

func (agent *DeviceAgent) updatePortState(portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_OperStatus) error {
	agent.lockDevice.Lock()
	//defer agent.lockDevice.Unlock()
	// Work only on latest data
	// TODO: Get list of ports from device directly instead of the entire device
	if storeDevice, err := agent.getDeviceWithoutLock(); err != nil {
		agent.lockDevice.Unlock()
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := proto.Clone(storeDevice).(*voltha.Device)
		// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
		if _, ok := voltha.Port_PortType_value[portType.String()]; !ok {
			agent.lockDevice.Unlock()
			return status.Errorf(codes.InvalidArgument, "%s", portType)
		}
		for _, port := range cloned.Ports {
			if port.Type == portType && port.PortNo == portNo {
				port.OperStatus = operStatus
				// Set the admin status to ENABLED if the operational status is ACTIVE
				// TODO: Set by northbound system?
				if operStatus == voltha.OperStatus_ACTIVE {
					port.AdminState = voltha.AdminState_ENABLED
				}
				break
			}
		}
		log.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
		// Store the device
		if afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, ""); afterUpdate == nil {
			agent.lockDevice.Unlock()
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		agent.lockDevice.Unlock()
		// Perform the state transition
		if err := agent.deviceMgr.processTransition(storeDevice, cloned); err != nil {
			log.Warnw("process-transition-error", log.Fields{"deviceid": agent.deviceId, "error": err})
			return err
		}
		return nil
	}
}

func (agent *DeviceAgent) updatePmConfigs(pmConfigs *voltha.PmConfigs) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debug("updatePmConfigs")
	// Work only on latest data
	if storeDevice, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := proto.Clone(storeDevice).(*voltha.Device)
		cloned.PmConfigs = proto.Clone(pmConfigs).(*voltha.PmConfigs)
		// Store the device
		afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		return nil
	}
}

func (agent *DeviceAgent) addPort(port *voltha.Port) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debugw("addPort", log.Fields{"deviceId": agent.deviceId})
	// Work only on latest data
	if storeDevice, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := proto.Clone(storeDevice).(*voltha.Device)
		if cloned.Ports == nil {
			//	First port
			log.Debugw("addPort-first-port-to-add", log.Fields{"deviceId": agent.deviceId})
			cloned.Ports = make([]*voltha.Port, 0)
		}
		cp := proto.Clone(port).(*voltha.Port)
		// Set the admin state of the port to ENABLE if the operational state is ACTIVE
		// TODO: Set by northbound system?
		if cp.OperStatus == voltha.OperStatus_ACTIVE {
			cp.AdminState = voltha.AdminState_ENABLED
		}
		cloned.Ports = append(cloned.Ports, cp)
		// Store the device
		afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		return nil
	}
}

func (agent *DeviceAgent) addPeerPort(port *voltha.Port_PeerPort) error {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	log.Debug("addPeerPort")
	// Work only on latest data
	if storeDevice, err := agent.getDeviceWithoutLock(); err != nil {
		return status.Errorf(codes.NotFound, "%s", agent.deviceId)
	} else {
		// clone the device
		cloned := proto.Clone(storeDevice).(*voltha.Device)
		// Get the peer port on the device based on the port no
		for _, peerPort := range cloned.Ports {
			if peerPort.PortNo == port.PortNo { // found port
				cp := proto.Clone(port).(*voltha.Port_PeerPort)
				peerPort.Peers = append(peerPort.Peers, cp)
				log.Debugw("found-peer", log.Fields{"portNo": port.PortNo, "deviceId": agent.deviceId})
				break
			}
		}
		//To track an issue when adding peer-port.
		log.Debugw("before-peer-added", log.Fields{"device": cloned})
		// Store the device
		afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, "")
		if afterUpdate == nil {
			return status.Errorf(codes.Internal, "%s", agent.deviceId)
		}
		//To track an issue when adding peer-port.
		if d, ok := afterUpdate.(*voltha.Device); ok {
			log.Debugw("after-peer-added", log.Fields{"device": d})
		} else {
			log.Debug("after-peer-added-incorrect-type", log.Fields{"type": reflect.ValueOf(afterUpdate).Type()})
		}

		return nil
	}
}

//flowTableUpdated is the callback after flows have been updated in the model to push them
//to the adapters
func (agent *DeviceAgent) flowTableUpdated(args ...interface{}) interface{} {
	log.Debugw("flowTableUpdated-callback", log.Fields{"argsLen": len(args)})

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	var previousData *voltha.Flows
	var latestData *voltha.Flows

	var ok bool
	if previousData, ok = args[0].(*ofp.Flows); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}
	if latestData, ok = args[1].(*ofp.Flows); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	// Sanity check - should not happen as this is already handled in logical device agent
	if reflect.DeepEqual(previousData.Items, latestData.Items) {
		log.Debugw("flow-update-not-required", log.Fields{"previous": previousData.Items, "new": latestData.Items})
		return nil
	}

	var device *voltha.Device
	var err error
	if device, err = agent.getDeviceWithoutLock(); err != nil {
		log.Errorw("no-device", log.Fields{"id": agent.deviceId, "error": err})
		return nil
	}
	groups := device.FlowGroups

	// Send update to adapters
	// Check whether the device supports incremental flow changes
	// Assume false for test
	acceptsAddRemoveFlowUpdates := false
	if !acceptsAddRemoveFlowUpdates {
		if err := agent.adapterProxy.UpdateFlowsBulk(device, latestData, groups); err != nil {
			log.Debugw("update-flow-bulk-error", log.Fields{"id": agent.lastData.Id, "error": err})
			return err
		}
		return nil
	}
	// Incremental flow changes accepted
	var toAdd []*ofp.OfpFlowStats
	var toDelete []*ofp.OfpFlowStats

	for _, flow := range latestData.Items {
		if fu.FindFlowById(previousData.Items, flow) == -1 { // did not exist before
			toAdd = append(toAdd, flow)
		}
	}
	for _, flow := range previousData.Items {
		if fu.FindFlowById(latestData.Items, flow) == -1 { // does not exist now
			toDelete = append(toDelete, flow)
		}
	}
	flowChanges := &ofp.FlowChanges{
		ToAdd:    &voltha.Flows{Items: toAdd},
		ToRemove: &voltha.Flows{Items: toDelete},
	}
	// Send an empty group changes as it would be dealt with a call to groupTableUpdated
	groupChanges := &ofp.FlowGroupChanges{}

	// Send changes only
	if err := agent.adapterProxy.UpdateFlowsIncremental(device, flowChanges, groupChanges); err != nil {
		log.Debugw("update-flow-bulk-error", log.Fields{"id": agent.lastData.Id, "error": err})
		return err
	}

	return nil
}

//groupTableUpdated is the callback after group table has been updated in the model to push them
//to the adapters
func (agent *DeviceAgent) groupTableUpdated(args ...interface{}) interface{} {
	log.Debugw("groupTableUpdated-callback", log.Fields{"argsLen": len(args)})

	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()

	var previousData *voltha.FlowGroups
	var latestData *voltha.FlowGroups

	var ok bool
	if previousData, ok = args[0].(*ofp.FlowGroups); !ok {
		log.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}
	if latestData, ok = args[1].(*ofp.FlowGroups); !ok {
		log.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	// Sanity check - should not happen as this is already handled in logical device agent
	if reflect.DeepEqual(previousData.Items, latestData.Items) {
		log.Debugw("group-table-update-not-required", log.Fields{"previous": previousData.Items, "new": latestData.Items})
		return nil
	}

	var device *voltha.Device
	var err error
	if device, err = agent.getDeviceWithoutLock(); err != nil {
		log.Errorw("no-device", log.Fields{"id": agent.deviceId, "error": err})
		return nil
	}
	flows := device.Flows

	// Send update to adapters
	// Check whether the device supports incremental flow changes
	// Assume false for test
	acceptsAddRemoveFlowUpdates := false
	if !acceptsAddRemoveFlowUpdates {
		if err := agent.adapterProxy.UpdateFlowsBulk(device, flows, latestData); err != nil {
			log.Debugw("update-flows-bulk-error", log.Fields{"id": agent.lastData.Id, "error": err})
			return err
		}
		return nil
	}

	// Incremental group changes accepted
	var toAdd []*ofp.OfpGroupEntry
	var toDelete []*ofp.OfpGroupEntry
	var toUpdate []*ofp.OfpGroupEntry

	for _, group := range latestData.Items {
		if idx := fu.FindGroup(previousData.Items, group.Desc.GroupId); idx == -1 { // did not exist before
			toAdd = append(toAdd, group)
		} else { // existed before
			if previousData.Items[idx].String() != group.String() { // there is a change
				toUpdate = append(toUpdate, group)
			}
		}
	}
	for _, group := range previousData.Items {
		if fu.FindGroup(latestData.Items, group.Desc.GroupId) == -1 { // does not exist now
			toDelete = append(toDelete, group)
		}
	}
	groupChanges := &ofp.FlowGroupChanges{
		ToAdd:    &voltha.FlowGroups{Items: toAdd},
		ToRemove: &voltha.FlowGroups{Items: toDelete},
		ToUpdate: &voltha.FlowGroups{Items: toUpdate},
	}
	// Send an empty flow changes as it should have been dealt with a call to flowTableUpdated
	flowChanges := &ofp.FlowChanges{}

	// Send changes only
	if err := agent.adapterProxy.UpdateFlowsIncremental(device, flowChanges, groupChanges); err != nil {
		log.Debugw("update-incremental-group-error", log.Fields{"id": agent.lastData.Id, "error": err})
		return err
	}
	return nil
}

// TODO: A generic device update by attribute
func (agent *DeviceAgent) updateDeviceAttribute(name string, value interface{}) {
	agent.lockDevice.Lock()
	defer agent.lockDevice.Unlock()
	if value == nil {
		return
	}
	var storeDevice *voltha.Device
	var err error
	if storeDevice, err = agent.getDeviceWithoutLock(); err != nil {
		return
	}
	updated := false
	s := reflect.ValueOf(storeDevice).Elem()
	if s.Kind() == reflect.Struct {
		// exported field
		f := s.FieldByName(name)
		if f.IsValid() && f.CanSet() {
			switch f.Kind() {
			case reflect.String:
				f.SetString(value.(string))
				updated = true
			case reflect.Uint32:
				f.SetUint(uint64(value.(uint32)))
				updated = true
			case reflect.Bool:
				f.SetBool(value.(bool))
				updated = true
			}
		}
	}
	log.Debugw("update-field-status", log.Fields{"deviceId": storeDevice.Id, "name": name, "updated": updated})
	//	Save the data
	cloned := proto.Clone(storeDevice).(*voltha.Device)
	if afterUpdate := agent.clusterDataProxy.Update("/devices/"+agent.deviceId, cloned, false, ""); afterUpdate == nil {
		log.Warnw("attribute-update-failed", log.Fields{"attribute": name, "value": value})
	}
	return
}
