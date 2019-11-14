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
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	fd "github.com/opencord/voltha-go/rw_core/flowdecomposition"
	"github.com/opencord/voltha-go/rw_core/graph"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v2/pkg/flows"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	ic "github.com/opencord/voltha-protos/v2/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v2/go/openflow_13"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalDeviceAgent represent attributes of logical device agent
type LogicalDeviceAgent struct {
	logicalDeviceID    string
	rootDeviceID       string
	deviceMgr          *DeviceManager
	ldeviceMgr         *LogicalDeviceManager
	clusterDataProxy   *model.Proxy
	exitChannel        chan int
	deviceGraph        *graph.DeviceGraph
	flowProxy          *model.Proxy
	groupProxy         *model.Proxy
	meterProxy         *model.Proxy
	ldProxy            *model.Proxy
	portProxies        map[string]*model.Proxy
	portProxiesLock    sync.RWMutex
	lockLogicalDevice  sync.RWMutex
	lockDeviceGraph    sync.RWMutex
	logicalPortsNo     map[uint32]bool //value is true for NNI port
	lockLogicalPortsNo sync.RWMutex
	flowDecomposer     *fd.FlowDecomposer
	defaultTimeout     int64
}

func newLogicalDeviceAgent(id string, deviceID string, ldeviceMgr *LogicalDeviceManager,
	deviceMgr *DeviceManager,
	cdProxy *model.Proxy, timeout int64) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceID = id
	agent.rootDeviceID = deviceID
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.flowDecomposer = fd.NewFlowDecomposer(agent.deviceMgr)
	agent.lockLogicalDevice = sync.RWMutex{}
	agent.portProxies = make(map[string]*model.Proxy)
	agent.portProxiesLock = sync.RWMutex{}
	agent.lockLogicalPortsNo = sync.RWMutex{}
	agent.lockDeviceGraph = sync.RWMutex{}
	agent.logicalPortsNo = make(map[uint32]bool)
	agent.defaultTimeout = timeout
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context, loadFromdB bool) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceId": agent.logicalDeviceID, "loadFromdB": loadFromdB})
	var ld *voltha.LogicalDevice
	if !loadFromdB {
		//Build the logical device based on information retrieved from the device adapter
		var switchCap *ic.SwitchCapability
		var err error
		if switchCap, err = agent.deviceMgr.getSwitchCapability(ctx, agent.rootDeviceID); err != nil {
			log.Errorw("error-creating-logical-device", log.Fields{"error": err})
			return err
		}
		ld = &voltha.LogicalDevice{Id: agent.logicalDeviceID, RootDeviceId: agent.rootDeviceID}

		// Create the datapath ID (uint64) using the logical device ID (based on the MAC Address)
		var datapathID uint64
		if datapathID, err = CreateDataPathID(agent.logicalDeviceID); err != nil {
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
		if added := agent.clusterDataProxy.AddWithID(ctx, "/logical_devices", ld.Id, ld, ""); added == nil {
			log.Errorw("failed-to-add-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		} else {
			log.Debugw("logicaldevice-created", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		}
		agent.lockLogicalDevice.Unlock()

		// TODO:  Set the logical ports in a separate call once the port update issue is fixed.
		go func() {
			err := agent.setupLogicalPorts(ctx)
			if err != nil {
				log.Errorw("unable-to-setup-logical-ports", log.Fields{"error": err})
			}
		}()

	} else {
		//	load from dB - the logical may not exist at this time.  On error, just return and the calling function
		// will destroy this agent.
		var err error
		if ld, err = agent.GetLogicalDevice(); err != nil {
			log.Warnw("failed-to-load-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
			return err
		}

		// Update the root device Id
		agent.rootDeviceID = ld.RootDeviceId

		// Setup the local list of logical ports
		agent.addLogicalPortsToMap(ld.Ports)

	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	agent.flowProxy = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s/flows", agent.logicalDeviceID),
		false)
	agent.meterProxy = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s/meters", agent.logicalDeviceID),
		false)
	agent.groupProxy = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s/flow_groups", agent.logicalDeviceID),
		false)
	agent.ldProxy = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s", agent.logicalDeviceID),
		false)

	// TODO:  Use a port proxy once the POST_ADD is fixed
	if agent.ldProxy != nil {
		agent.ldProxy.RegisterCallback(model.POST_UPDATE, agent.portUpdated)
	} else {
		log.Errorw("logical-device-proxy-null", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return status.Error(codes.Internal, "logical-device-proxy-null")
	}

	// Setup the device graph - run it in its own routine
	if loadFromdB {
		go agent.generateDeviceGraph()
	}
	return nil
}

// stop stops the logical devuce agent.  This removes the logical device from the data model.
func (agent *LogicalDeviceAgent) stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	//Remove the logical device from the model
	if removed := agent.clusterDataProxy.Remove(ctx, "/logical_devices/"+agent.logicalDeviceID, ""); removed == nil {
		log.Errorw("failed-to-remove-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
	} else {
		log.Debugw("logicaldevice-removed", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
	}
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}

// GetLogicalDevice locks the logical device model and then retrieves the latest logical device information
func (agent *LogicalDeviceAgent) GetLogicalDevice() (*voltha.LogicalDevice, error) {
	log.Debug("GetLogicalDevice")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get(context.Background(), "/logical_devices/"+agent.logicalDeviceID, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDeviceFlows returns logical device flows
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlows() (*ofp.Flows, error) {
	log.Debug("ListLogicalDeviceFlows")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get(context.Background(), "/logical_devices/"+agent.logicalDeviceID, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cFlows := (proto.Clone(lDevice.Flows)).(*ofp.Flows)
		return cFlows, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDeviceMeters returns logical device meters
func (agent *LogicalDeviceAgent) ListLogicalDeviceMeters() (*ofp.Meters, error) {
	log.Debug("ListLogicalDeviceMeters")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get(context.Background(), "/logical_devices/"+agent.logicalDeviceID, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		if lDevice.Meters == nil {
			return &ofp.Meters{}, nil
		}
		cMeters := (proto.Clone(lDevice.Meters)).(*ofp.Meters)
		return cMeters, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlowGroups() (*ofp.FlowGroups, error) {
	log.Debug("ListLogicalDeviceFlowGroups")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get(context.Background(), "/logical_devices/"+agent.logicalDeviceID, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		cFlowGroups := (proto.Clone(lDevice.FlowGroups)).(*ofp.FlowGroups)
		return cFlowGroups, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDevicePorts returns logical device ports
func (agent *LogicalDeviceAgent) ListLogicalDevicePorts() (*voltha.LogicalPorts, error) {
	log.Debug("ListLogicalDevicePorts")
	agent.lockLogicalDevice.RLock()
	defer agent.lockLogicalDevice.RUnlock()
	logicalDevice := agent.clusterDataProxy.Get(context.Background(), "/logical_devices/"+agent.logicalDeviceID, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		lPorts := make([]*voltha.LogicalPort, 0)
		lPorts = append(lPorts, lDevice.Ports...)
		return &voltha.LogicalPorts{Items: lPorts}, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

//updateLogicalDeviceFlowsWithoutLock updates the logical device with the latest flows in the model.
func (agent *LogicalDeviceAgent) updateLogicalDeviceFlowsWithoutLock(flows *ofp.Flows) error {
	ld, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		return status.Errorf(codes.Internal, "logical-device-absent:%s", agent.logicalDeviceID)
	}
	log.Debugw("logical-device-before", log.Fields{"lports": len(ld.Ports)})
	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	cloned.Flows = flows

	if err = agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
		log.Errorw("error-updating-logical-device-with-flows", log.Fields{"error": err})
		return err
	}
	return nil
}

//updateLogicalDeviceMetersWithoutLock updates the logical device with the meters info
func (agent *LogicalDeviceAgent) updateLogicalDeviceMetersWithoutLock(meters *ofp.Meters) error {
	ld, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		return status.Errorf(codes.Internal, "logical-device-absent:%s", agent.logicalDeviceID)
	}
	log.Debugw("logical-device-before", log.Fields{"lports": len(ld.Ports)})
	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	cloned.Meters = meters

	if err = agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
		log.Errorw("error-updating-logical-device-with-meters", log.Fields{"error": err})
		return err
	}
	return nil
}

//updateLogicalDeviceFlowGroupsWithoutLock updates the logical device with the flow groups
func (agent *LogicalDeviceAgent) updateLogicalDeviceFlowGroupsWithoutLock(flowGroups *ofp.FlowGroups) error {
	ld, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		return status.Errorf(codes.Internal, "logical-device-absent:%s", agent.logicalDeviceID)
	}
	log.Debugw("logical-device-before", log.Fields{"lports": len(ld.Ports)})
	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	cloned.FlowGroups = flowGroups

	if err = agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
		log.Errorw("error-updating-logical-device-with-flowgroups", log.Fields{"error": err})
		return err
	}
	return nil
}

// getLogicalDeviceWithoutLock retrieves a logical device from the model without locking it.   This is used only by
// functions that have already acquired the logical device lock to the model
func (agent *LogicalDeviceAgent) getLogicalDeviceWithoutLock() (*voltha.LogicalDevice, error) {
	log.Debug("getLogicalDeviceWithoutLock")
	logicalDevice := agent.clusterDataProxy.Get(context.Background(), "/logical_devices/"+agent.logicalDeviceID, 0, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		//log.Debug("getLogicalDeviceWithoutLock", log.Fields{"ldevice": lDevice})
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
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

// setupLogicalPorts is invoked once the logical device has been created and is ready to get ports
// added to it.  While the logical device was being created we could have received requests to add
// NNI and UNI ports which were discarded.  Now is the time to add them if needed
func (agent *LogicalDeviceAgent) setupLogicalPorts(ctx context.Context) error {
	log.Infow("setupLogicalPorts", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// First add any NNI ports which could have been missing
	if err := agent.setupNNILogicalPorts(context.TODO(), agent.rootDeviceID); err != nil {
		log.Errorw("error-setting-up-NNI-ports", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}

	// Now, set up the UNI ports if needed.
	children, err := agent.deviceMgr.getAllChildDevices(agent.rootDeviceID)
	if err != nil {
		log.Errorw("error-getting-child-devices", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}
	responses := make([]coreutils.Response, 0)
	for _, child := range children.Items {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(child *voltha.Device) {
			if err = agent.setupUNILogicalPorts(context.TODO(), child); err != nil {
				log.Error("setting-up-UNI-ports-failed", log.Fields{"deviceID": child.Id})
				response.Error(status.Errorf(codes.Internal, "UNI-ports-setup-failed: %s", child.Id))
			}
			response.Done()
		}(child)
	}
	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, responses...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

// setupNNILogicalPorts creates an NNI port on the logical device that represents an NNI interface on a root device
func (agent *LogicalDeviceAgent) setupNNILogicalPorts(ctx context.Context, deviceID string) error {
	log.Infow("setupNNILogicalPorts-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error

	var device *voltha.Device
	if device, err = agent.deviceMgr.GetDevice(deviceID); err != nil {
		log.Errorw("error-retrieving-device", log.Fields{"error": err, "deviceId": deviceID})
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

// updatePortState updates the port state of the device
func (agent *LogicalDeviceAgent) updatePortState(deviceID string, portNo uint32, operStatus voltha.OperStatus_OperStatus) error {
	log.Infow("updatePortState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "portNo": portNo, "state": operStatus})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device info
	ld, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		log.Warnw("logical-device-unknown", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	for idx, lPort := range ld.Ports {
		if lPort.DeviceId == deviceID && lPort.DevicePortNo == portNo {
			cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
			if operStatus == voltha.OperStatus_ACTIVE {
				cloned.Ports[idx].OfpPort.Config = cloned.Ports[idx].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				cloned.Ports[idx].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				cloned.Ports[idx].OfpPort.Config = cloned.Ports[idx].OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				cloned.Ports[idx].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}
			// Update the logical device
			if err := agent.updateLogicalDeviceWithoutLock(cloned); err != nil {
				log.Errorw("error-updating-logical-device", log.Fields{"error": err})
				return err
			}
			return nil
		}
	}
	return status.Errorf(codes.NotFound, "port-%d-not-exist", portNo)
}

// updatePortsState updates the ports state related to the device
func (agent *LogicalDeviceAgent) updatePortsState(device *voltha.Device, state voltha.AdminState_AdminState) error {
	log.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device info
	ld, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		log.Warnw("logical-device-unknown", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
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
		log.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
}

// setupUNILogicalPorts creates a UNI port on the logical device that represents a child UNI interface
func (agent *LogicalDeviceAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	log.Infow("setupUNILogicalPort", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error
	var added bool
	//Get UNI port number
	for _, port := range childDevice.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			if added, err = agent.addUNILogicalPort(childDevice, port); err != nil {
				log.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			if added {
				agent.addLogicalPortToMap(port.PortNo, false)
			}
		}
	}
	return err
}

// deleteAllLogicalPorts deletes all logical ports associated with this device
func (agent *LogicalDeviceAgent) deleteAllLogicalPorts(device *voltha.Device) error {
	log.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device info
	ld, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		log.Warnw("logical-device-unknown", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
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
			log.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
			return err
		}
	} else {
		log.Debugw("no-change-required", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	}
	return nil
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceWithoutLock(logicalDevice *voltha.LogicalDevice) error {
	updateCtx := context.WithValue(context.Background(), model.RequestTimestamp, time.Now().UnixNano())
	afterUpdate := agent.clusterDataProxy.Update(updateCtx, "/logical_devices/"+agent.logicalDeviceID, logicalDevice, false, "")
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device:%s", agent.logicalDeviceID)
	}
	return nil
}

//generateDeviceGraphIfNeeded generates the device graph if the logical device has been updated since the last time
//that device graph was generated.
func (agent *LogicalDeviceAgent) generateDeviceGraphIfNeeded() error {
	ld, err := agent.GetLogicalDevice()
	if err != nil {
		log.Errorw("get-logical-device-error", log.Fields{"error": err})
		return err
	}
	agent.lockDeviceGraph.Lock()
	defer agent.lockDeviceGraph.Unlock()
	if agent.deviceGraph != nil && agent.deviceGraph.IsUpToDate(ld) {
		return nil
	}
	log.Debug("Generation of device graph required")
	agent.generateDeviceGraph()
	return nil
}

//updateFlowTable updates the flow table of that logical device
func (agent *LogicalDeviceAgent) updateFlowTable(ctx context.Context, flow *ofp.OfpFlowMod) error {
	log.Debug("updateFlowTable")
	if flow == nil {
		return nil
	}
	if err := agent.generateDeviceGraphIfNeeded(); err != nil {
		return err
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
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, flow.GetCommand())
}

//updateGroupTable updates the group table of that logical device
func (agent *LogicalDeviceAgent) updateGroupTable(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	log.Debug("updateGroupTable")
	if groupMod == nil {
		return nil
	}
	if err := agent.generateDeviceGraphIfNeeded(); err != nil {
		return err
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
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, groupMod.GetCommand())
}

// updateMeterTable updates the meter table of that logical device
func (agent *LogicalDeviceAgent) updateMeterTable(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	log.Debug("updateMeterTable")
	if meterMod == nil {
		return nil
	}
	if err := agent.generateDeviceGraphIfNeeded(); err != nil {
		return err
	}
	switch meterMod.GetCommand() {
	case ofp.OfpMeterModCommand_OFPMC_ADD:
		return agent.meterAdd(meterMod)
	case ofp.OfpMeterModCommand_OFPMC_DELETE:
		return agent.meterDelete(meterMod)
	case ofp.OfpMeterModCommand_OFPMC_MODIFY:
		return agent.meterModify(meterMod)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, meterMod.GetCommand())

}

func (agent *LogicalDeviceAgent) meterAdd(meterMod *ofp.OfpMeterMod) error {
	log.Debugw("meterAdd", log.Fields{"metermod": *meterMod})
	if meterMod == nil {
		return nil
	}
	log.Debug("Waiting for logical device lock!!")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	log.Debug("Acquired logical device lock")
	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}

	var meters []*ofp.OfpMeterEntry
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	log.Debugw("Available meters", log.Fields{"meters": meters})

	for _, meter := range meters {
		if meterMod.MeterId == meter.Config.MeterId {
			log.Infow("Meter-already-exists", log.Fields{"meter": *meterMod})
			return nil
		}
	}

	meterEntry := fu.MeterEntryFromMeterMod(meterMod)
	meters = append(meters, meterEntry)
	//Update model
	if err := agent.updateLogicalDeviceMetersWithoutLock(&ofp.Meters{Items: meters}); err != nil {
		log.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return err
	}
	log.Debugw("Meter-added-successfully", log.Fields{"Added-meter": meterEntry, "updated-meters": lDevice.Meters})
	return nil
}

func (agent *LogicalDeviceAgent) meterDelete(meterMod *ofp.OfpMeterMod) error {
	log.Debug("meterDelete", log.Fields{"meterMod": *meterMod})
	if meterMod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}

	var meters []*ofp.OfpMeterEntry
	var flows []*ofp.OfpFlowStats
	updatedFlows := make([]*ofp.OfpFlowStats, 0)
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}

	changedMeter := false
	changedFow := false
	log.Debugw("Available meters", log.Fields{"meters": meters})
	for index, meter := range meters {
		if meterMod.MeterId == meter.Config.MeterId {
			flows = lDevice.Flows.Items
			changedFow, updatedFlows = agent.getUpdatedFlowsAfterDeletebyMeterID(flows, meterMod.MeterId)
			meters = append(meters[:index], meters[index+1:]...)
			log.Debugw("Meter has been deleted", log.Fields{"meter": meter, "index": index})
			changedMeter = true
			break
		}
	}
	if changedMeter {
		//Update model
		metersToUpdate := &ofp.Meters{}
		if lDevice.Meters != nil {
			metersToUpdate = &ofp.Meters{Items: meters}
		}
		if err := agent.updateLogicalDeviceMetersWithoutLock(metersToUpdate); err != nil {
			log.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		log.Debug("Meter-deleted-from-DB-successfully", log.Fields{"updatedMeters": metersToUpdate, "no-of-meter": len(metersToUpdate.Items)})

	}
	if changedFow {
		//Update model
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: updatedFlows}); err != nil {
			log.Errorw("db-flow-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		log.Debug("Flows-associated-with-meter-deleted-from-DB-successfully",
			log.Fields{"updated-no-of-flows": len(updatedFlows), "meter": meterMod.MeterId})
	}
	log.Debugw("meterDelete success", log.Fields{"meterID": meterMod.MeterId})
	return nil
}

func (agent *LogicalDeviceAgent) meterModify(meterMod *ofp.OfpMeterMod) error {
	log.Debug("meterModify")
	if meterMod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}

	var meters []*ofp.OfpMeterEntry
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	changedMeter := false
	for index, meter := range meters {
		if meterMod.MeterId == meter.Config.MeterId {
			newmeterEntry := fu.MeterEntryFromMeterMod(meterMod)
			newmeterEntry.Stats.FlowCount = meter.Stats.FlowCount
			meters[index] = newmeterEntry
			changedMeter = true
			log.Debugw("Found meter, replaced with new meter", log.Fields{"old meter": meter, "new meter": newmeterEntry})
			break
		}
	}
	if changedMeter {
		//Update model
		metersToUpdate := &ofp.Meters{}
		if lDevice.Meters != nil {
			metersToUpdate = &ofp.Meters{Items: meters}
		}
		if err := agent.updateLogicalDeviceMetersWithoutLock(metersToUpdate); err != nil {
			log.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		log.Debugw("meter-updated-in-DB-successfully", log.Fields{"updated_meters": meters})
		return nil
	}

	log.Errorw("Meter not found ", log.Fields{"meter": meterMod})
	return fmt.Errorf("no-logical-device-present:%d", meterMod.MeterId)

}

func (agent *LogicalDeviceAgent) getUpdatedFlowsAfterDeletebyMeterID(flows []*ofp.OfpFlowStats, meterID uint32) (bool, []*ofp.OfpFlowStats) {
	log.Infow("Delete flows matching meter", log.Fields{"meter": meterID})
	changed := false
	//updatedFlows := make([]*ofp.OfpFlowStats, 0)
	for index := len(flows) - 1; index >= 0; index-- {
		if mID := fu.GetMeterIdFromFlow(flows[index]); mID != 0 && mID == meterID {
			log.Debugw("Flow to be deleted", log.Fields{"flow": flows[index], "index": index})
			flows = append(flows[:index], flows[index+1:]...)
			changed = true
		}
	}
	return changed, flows
}

func (agent *LogicalDeviceAgent) updateFlowCountOfMeterStats(modCommand *ofp.OfpFlowMod, meters []*ofp.OfpMeterEntry, flow *ofp.OfpFlowStats) bool {

	flowCommand := modCommand.GetCommand()
	meterID := fu.GetMeterIdFromFlow(flow)
	log.Debugw("Meter-id-in-flow-mod", log.Fields{"meterId": meterID})
	if meterID == 0 {
		log.Debugw("No meter present in the flow", log.Fields{"flow": *flow})
		return false
	}
	if meters == nil {
		log.Debug("No meters present in logical device")
		return false
	}
	changedMeter := false
	for _, meter := range meters {
		if meterID == meter.Config.MeterId { // Found meter in Logicaldevice
			if flowCommand == ofp.OfpFlowModCommand_OFPFC_ADD {
				meter.Stats.FlowCount++
				changedMeter = true
			} else if flowCommand == ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT {
				meter.Stats.FlowCount--
				changedMeter = true
			}
			log.Debugw("Found meter, updated meter flow stats", log.Fields{" meterId": meterID})
			break
		}
	}
	return changedMeter
}

//flowAdd adds a flow to the flow table of that logical device
func (agent *LogicalDeviceAgent) flowAdd(mod *ofp.OfpFlowMod) error {
	log.Debugw("flowAdd", log.Fields{"flow": mod})
	if mod == nil {
		return nil
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}

	var flows []*ofp.OfpFlowStats
	var meters []*ofp.OfpMeterEntry
	var flow *ofp.OfpFlowStats

	if lDevice.Flows != nil && lDevice.Flows.Items != nil {
		flows = lDevice.Flows.Items
	}

	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	updatedFlows := make([]*ofp.OfpFlowStats, 0)
	changed := false
	updated := false
	checkOverlap := (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_CHECK_OVERLAP)) != 0
	if checkOverlap {
		if overlapped := fu.FindOverlappingFlows(flows, mod); len(overlapped) != 0 {
			//	TODO:  should this error be notified other than being logged?
			log.Warnw("overlapped-flows", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		} else {
			//	Add flow
			flow = fu.FlowStatsEntryFromFlowModMessage(mod)
			flows = append(flows, flow)
			updatedFlows = append(updatedFlows, flow)
			changed = true
		}
	} else {
		flow = fu.FlowStatsEntryFromFlowModMessage(mod)
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
				updated = true
			}
		} else {
			flows = append(flows, flow)
			updatedFlows = append(updatedFlows, flow)
			changed = true
		}
	}
	log.Debugw("flowAdd-changed", log.Fields{"changed": changed})

	if changed {
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(updatedFlows, meters, &flowMetadata); err != nil { // This should never happen,meters should be installed before flow arrives
			log.Error("Meter-referred-in-flows-not-present")
			return err
		}
		deviceRules := agent.flowDecomposer.DecomposeRules(agent, ofp.Flows{Items: updatedFlows}, *lDevice.FlowGroups)
		log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.addDeviceFlowsAndGroups(deviceRules, &flowMetadata); err != nil {
			log.Errorw("failure-updating-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			return err
		}

		//	Update model
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: flows}); err != nil {
			log.Errorw("db-flow-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		if !updated {
			changedMeterStats := agent.updateFlowCountOfMeterStats(mod, meters, flow)
			metersToUpdate := &ofp.Meters{}
			if lDevice.Meters != nil {
				metersToUpdate = &ofp.Meters{Items: meters}
			}
			if changedMeterStats {
				//Update model
				if err := agent.updateLogicalDeviceMetersWithoutLock(metersToUpdate); err != nil {
					log.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
					return err
				}
				log.Debugw("meter-stats-updated-in-DB-successfully", log.Fields{"updated_meters": meters})

			}
		}

	}
	return nil
}

// GetMeterConfig returns meter config
func (agent *LogicalDeviceAgent) GetMeterConfig(flows []*ofp.OfpFlowStats, meters []*ofp.OfpMeterEntry, metadata *voltha.FlowMetadata) error {
	m := make(map[uint32]bool)
	for _, flow := range flows {
		if flowMeterID := fu.GetMeterIdFromFlow(flow); flowMeterID != 0 && !m[flowMeterID] {
			foundMeter := false
			// Meter is present in the flow , Get from logical device
			for _, meter := range meters {
				if flowMeterID == meter.Config.MeterId {
					metadata.Meters = append(metadata.Meters, meter.Config)
					log.Debugw("Found meter in logical device",
						log.Fields{"meterID": flowMeterID, "meter-band": meter.Config})
					m[flowMeterID] = true
					foundMeter = true
					break
				}
			}
			if !foundMeter {
				log.Errorw("Meter-referred-by-flow-is-not-found-in-logicaldevice",
					log.Fields{"meterID": flowMeterID, "Available-meters": meters, "flow": *flow})
				return errors.New("Meter-referred-by-flow-is-not-found-in-logicaldevice")
			}
		}
	}
	log.Debugw("meter-bands-for-flows", log.Fields{"flows": len(flows), "metadata": metadata})
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
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}

	var meters []*ofp.OfpMeterEntry
	var flows []*ofp.OfpFlowStats

	if lDevice.Flows != nil && lDevice.Flows.Items != nil {
		flows = lDevice.Flows.Items
	}

	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	//build a list of what to keep vs what to delete
	toKeep := make([]*ofp.OfpFlowStats, 0)
	toDelete := make([]*ofp.OfpFlowStats, 0)
	for _, f := range flows {
		// Check whether the flow and the flowmod matches
		if fu.FlowMatch(f, fu.FlowStatsEntryFromFlowModMessage(mod)) {
			toDelete = append(toDelete, f)
			continue
		}
		// Check wild card match
		if !fu.FlowMatchesMod(f, mod) {
			toKeep = append(toKeep, f)
		} else {
			toDelete = append(toDelete, f)
		}
	}

	log.Debugw("flowDelete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "toKeep": len(toKeep), "toDelete": toDelete})

	//Update flows
	if len(toDelete) > 0 {
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(toDelete, meters, &flowMetadata); err != nil { // This should never happen
			log.Error("Meter-referred-in-flows-not-present")
			return errors.New("Meter-referred-in-flows-not-present")
		}
		deviceRules := agent.flowDecomposer.DecomposeRules(agent, ofp.Flows{Items: toDelete}, ofp.FlowGroups{})
		log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.deleteDeviceFlowsAndGroups(deviceRules, &flowMetadata); err != nil {
			log.Errorw("failure-updating-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			return err
		}

		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: toKeep}); err != nil {
			log.Errorw("Cannot-update-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
	}

	//TODO: send announcement on delete
	return nil
}

func (agent *LogicalDeviceAgent) addDeviceFlowsAndGroups(deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) error {
	log.Debugw("addDeviceFlowsAndGroups", log.Fields{"logicalDeviceID": agent.logicalDeviceID, "deviceRules": deviceRules, "flowMetadata": flowMetadata})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			if err := agent.deviceMgr.addFlowsAndGroups(deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				log.Errorw("flow-add-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-add-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, responses...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

func (agent *LogicalDeviceAgent) deleteDeviceFlowsAndGroups(deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) error {
	log.Debugw("deleteDeviceFlowsAndGroups", log.Fields{"logicalDeviceID": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			if err := agent.deviceMgr.deleteFlowsAndGroups(deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				log.Error("flow-delete-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-delete-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, responses...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
	}
	return nil
}

func (agent *LogicalDeviceAgent) updateDeviceFlowsAndGroups(deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) error {
	log.Debugw("updateDeviceFlowsAndGroups", log.Fields{"logicalDeviceID": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			if err := agent.deviceMgr.updateFlowsAndGroups(deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				log.Error("flow-update-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-update-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	// Wait for completion
	if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, responses...); res != nil {
		return status.Errorf(codes.Aborted, "errors-%s", res)
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
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}
	var meters []*ofp.OfpMeterEntry
	var flows []*ofp.OfpFlowStats
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	if lDevice.Flows != nil && lDevice.Flows.Items != nil {
		flows = lDevice.Flows.Items
	}

	changedFlow := false
	changedMeter := false
	flow := fu.FlowStatsEntryFromFlowModMessage(mod)
	flowsToDelete := make([]*ofp.OfpFlowStats, 0)
	idx := fu.FindFlows(flows, flow)
	if idx >= 0 {
		changedMeter = agent.updateFlowCountOfMeterStats(mod, meters, flows[idx])
		flowsToDelete = append(flowsToDelete, flows[idx])
		flows = append(flows[:idx], flows[idx+1:]...)
		changedFlow = true
	} else {
		return fmt.Errorf("Cannot delete flow - %s", flow)
	}
	if changedMeter {
		//Update model
		metersToUpdate := &ofp.Meters{}
		if lDevice.Meters != nil {
			metersToUpdate = &ofp.Meters{Items: meters}
		}
		if err := agent.updateLogicalDeviceMetersWithoutLock(metersToUpdate); err != nil {
			log.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

	}
	if changedFlow {
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(flowsToDelete, meters, &flowMetadata); err != nil {
			log.Error("Meter-referred-in-flows-not-present")
			return err
		}
		deviceRules := agent.flowDecomposer.DecomposeRules(agent, ofp.Flows{Items: flowsToDelete}, ofp.FlowGroups{})
		log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.deleteDeviceFlowsAndGroups(deviceRules, &flowMetadata); err != nil {
			log.Errorw("failure-deleting-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			return err
		}

		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: flows}); err != nil {
			log.Errorw("Cannot-update-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
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
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}
	groups := lDevice.FlowGroups.Items
	if fu.FindGroup(groups, groupMod.GroupId) == -1 {
		groups = append(groups, fu.GroupEntryFromGroupMod(groupMod))

		deviceRules := agent.flowDecomposer.DecomposeRules(agent, *lDevice.Flows, ofp.FlowGroups{Items: groups})
		log.Debugw("rules", log.Fields{"rules": deviceRules.String()})
		if err := agent.addDeviceFlowsAndGroups(deviceRules, nil); err != nil {
			log.Errorw("failure-updating-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			return err
		}

		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(&ofp.FlowGroups{Items: groups}); err != nil {
			log.Errorw("Cannot-update-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
	} else {
		return fmt.Errorf("Groups %d already present", groupMod.GroupId)
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
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}
	groups := lDevice.FlowGroups.Items
	flows := lDevice.Flows.Items
	var groupsChanged bool
	flowsChanged := false
	groupID := groupMod.GroupId
	if groupID == uint32(ofp.OfpGroup_OFPG_ALL) {
		//TODO we must delete all flows that point to this group and
		//signal controller as requested by flow's flag
		groups = []*ofp.OfpGroupEntry{}
		groupsChanged = true
	} else {
		idx := fu.FindGroup(groups, groupID)
		if idx == -1 {
			return nil // Valid case
		}
		flowsChanged, flows = fu.FlowsDeleteByGroupId(flows, groupID)
		groups = append(groups[:idx], groups[idx+1:]...)
		groupsChanged = true
	}
	if flowsChanged || groupsChanged {
		deviceRules := agent.flowDecomposer.DecomposeRules(agent, ofp.Flows{Items: flows}, ofp.FlowGroups{Items: groups})
		log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.updateDeviceFlowsAndGroups(deviceRules, nil); err != nil {
			log.Errorw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			return err
		}
	}

	if groupsChanged {
		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(&ofp.FlowGroups{Items: groups}); err != nil {
			log.Errorw("Cannot-update-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
	}
	if flowsChanged {
		if err := agent.updateLogicalDeviceFlowsWithoutLock(&ofp.Flows{Items: flows}); err != nil {
			log.Errorw("Cannot-update-flow", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
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
		log.Errorw("no-logical-device-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return fmt.Errorf("no-logical-device-present:%s", agent.logicalDeviceID)
	}
	groups := lDevice.FlowGroups.Items
	var groupsChanged bool
	groupID := groupMod.GroupId
	idx := fu.FindGroup(groups, groupID)
	if idx == -1 {
		return fmt.Errorf("group-absent:%d", groupID)
	}
	//replace existing group entry with new group definition
	groupEntry := fu.GroupEntryFromGroupMod(groupMod)
	groups[idx] = groupEntry
	groupsChanged = true
	if groupsChanged {
		deviceRules := agent.flowDecomposer.DecomposeRules(agent, ofp.Flows{Items: lDevice.Flows.Items}, ofp.FlowGroups{Items: groups})
		log.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.updateDeviceFlowsAndGroups(deviceRules, nil); err != nil {
			log.Errorw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			return err
		}

		//lDevice.FlowGroups.Items = groups
		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(&ofp.FlowGroups{Items: groups}); err != nil {
			log.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
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
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "logicalPortId": lPort.Id})
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
		log.Debugw("logical-port-deleted", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		if err := agent.updateLogicalDeviceWithoutLock(logicaldevice); err != nil {
			log.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		// Reset the logical device graph
		go agent.generateDeviceGraph()
	}
	return nil
}

// deleteLogicalPorts removes the logical ports associated with that deviceId
func (agent *LogicalDeviceAgent) deleteLogicalPorts(deviceID string) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return nil
	}
	updatedLPorts := []*voltha.LogicalPort{}
	for _, logicalPort := range logicaldevice.Ports {
		if logicalPort.DeviceId != deviceID {
			updatedLPorts = append(updatedLPorts, logicalPort)
		}
	}
	logicaldevice.Ports = updatedLPorts
	log.Debugw("updated-logical-ports", log.Fields{"ports": updatedLPorts})
	if err := agent.updateLogicalDeviceWithoutLock(logicaldevice); err != nil {
		log.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return err
	}
	// Reset the logical device graph
	go agent.generateDeviceGraph()

	return nil
}

// enableLogicalPort enables the logical port
func (agent *LogicalDeviceAgent) enableLogicalPort(lPortID string) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "logicalPortId": lPortID})
		return nil
	}
	index := -1
	for i, logicalPort := range logicaldevice.Ports {
		if logicalPort.Id == lPortID {
			index = i
			break
		}
	}
	if index >= 0 {
		logicaldevice.Ports[index].OfpPort.Config = logicaldevice.Ports[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDeviceWithoutLock(logicaldevice)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

// disableLogicalPort disabled the logical port
func (agent *LogicalDeviceAgent) disableLogicalPort(lPortID string) error {
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()

	// Get the most up to date logical device
	var logicaldevice *voltha.LogicalDevice
	if logicaldevice, _ = agent.getLogicalDeviceWithoutLock(); logicaldevice == nil {
		log.Debugw("no-logical-device", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "logicalPortId": lPortID})
		return nil
	}
	index := -1
	for i, logicalPort := range logicaldevice.Ports {
		if logicalPort.Id == lPortID {
			index = i
			break
		}
	}
	if index >= 0 {
		logicaldevice.Ports[index].OfpPort.Config = (logicaldevice.Ports[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)) | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDeviceWithoutLock(logicaldevice)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

func (agent *LogicalDeviceAgent) getPreCalculatedRoute(ingress, egress uint32) []graph.RouteHop {
	log.Debugw("ROUTE", log.Fields{"len": len(agent.deviceGraph.Routes)})
	for routeLink, route := range agent.deviceGraph.Routes {
		log.Debugw("ROUTELINKS", log.Fields{"ingress": ingress, "egress": egress, "routelink": routeLink})
		if ingress == routeLink.Ingress && egress == routeLink.Egress {
			return route
		}
	}
	log.Warnw("no-route", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "ingress": ingress, "egress": egress})
	return nil
}

// GetRoute returns route
func (agent *LogicalDeviceAgent) GetRoute(ingressPortNo uint32, egressPortNo uint32) []graph.RouteHop {
	log.Debugw("getting-route", log.Fields{"ingress-port": ingressPortNo, "egress-port": egressPortNo})
	routes := make([]graph.RouteHop, 0)

	// Note: A port value of 0 is equivalent to a nil port

	//	Consider different possibilities
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		log.Debugw("controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
		if agent.isNNIPort(ingressPortNo) {
			//This is a trap on the NNI Port
			if len(agent.deviceGraph.Routes) == 0 {
				// If there are no routes set (usually when the logical device has only NNI port(s), then just return an
				// route with same IngressHop and EgressHop
				hop := graph.RouteHop{DeviceID: agent.rootDeviceID, Ingress: ingressPortNo, Egress: ingressPortNo}
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

// GetDeviceGraph returns device graph
func (agent *LogicalDeviceAgent) GetDeviceGraph() *graph.DeviceGraph {
	return agent.deviceGraph
}

//updateRoutes rebuilds the device graph if not done already
func (agent *LogicalDeviceAgent) updateRoutes(device *voltha.Device, port *voltha.Port) error {
	log.Debugf("updateRoutes", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "device": device.Id, "port": port})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	if agent.deviceGraph == nil {
		agent.deviceGraph = graph.NewDeviceGraph(agent.logicalDeviceID, agent.deviceMgr.GetDevice)
	}
	// Get all the logical ports on that logical device
	lDevice, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		log.Errorw("unknown-logical-device", log.Fields{"error": err, "logicalDeviceId": agent.logicalDeviceID})
		return err
	}
	//TODO:  Find a better way to refresh only missing routes
	agent.deviceGraph.ComputeRoutes(lDevice.Ports)
	agent.deviceGraph.Print()
	return nil
}

//updateDeviceGraph updates the device graph if not done already and setup the default rules as well
func (agent *LogicalDeviceAgent) updateDeviceGraph(lp *voltha.LogicalPort) {
	log.Debugf("updateDeviceGraph", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	if agent.deviceGraph == nil {
		agent.deviceGraph = graph.NewDeviceGraph(agent.logicalDeviceID, agent.deviceMgr.GetDevice)
	}
	agent.deviceGraph.AddPort(lp)
	agent.deviceGraph.Print()
}

//generateDeviceGraph regenerates the device graph
func (agent *LogicalDeviceAgent) generateDeviceGraph() {
	log.Debugw("generateDeviceGraph", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Get the latest logical device
	if ld, err := agent.getLogicalDeviceWithoutLock(); err != nil {
		log.Errorw("logical-device-not-present", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
	} else {
		log.Debugw("generating-graph", log.Fields{"lDeviceId": agent.logicalDeviceID, "lPorts": len(ld.Ports)})
		if agent.deviceGraph == nil {
			agent.deviceGraph = graph.NewDeviceGraph(agent.logicalDeviceID, agent.deviceMgr.GetDevice)
		}
		agent.deviceGraph.ComputeRoutes(ld.Ports)
		agent.deviceGraph.Print()
	}
}

// diff go over two lists of logical ports and return what's new, what's changed and what's removed.
func diff(oldList, newList []*voltha.LogicalPort) (newPorts, changedPorts, deletedPorts []*voltha.LogicalPort) {
	newPorts = make([]*voltha.LogicalPort, 0)
	changedPorts = make([]*voltha.LogicalPort, 0)
	deletedPorts = make([]*voltha.LogicalPort, 0)
	for _, o := range oldList {
		found := false
		for _, n := range newList {
			if o.Id == n.Id {
				found = true
				break
			}
		}
		if !found {
			deletedPorts = append(deletedPorts, o)
		}
	}
	for _, n := range newList {
		found := false
		changed := false
		for _, o := range oldList {
			if o.Id == n.Id {
				changed = !reflect.DeepEqual(o, n)
				found = true
				break
			}
		}
		if !found {
			newPorts = append(newPorts, n)
		}
		if changed {
			changedPorts = append(changedPorts, n)
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
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_ADD, Desc: newP.OfpPort})
	}
	for _, change := range changedPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_MODIFY, Desc: change.OfpPort})
	}
	for _, del := range deletedPorts {
		go agent.ldeviceMgr.grpcNbiHdlr.sendChangeEvent(agent.logicalDeviceID,
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
	if portCap, err = agent.deviceMgr.getPortCapability(context.TODO(), device.Id, port.PortNo); err != nil {
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
	if portCap, err = agent.deviceMgr.getPortCapability(context.TODO(), childDevice.Id, port.PortNo); err != nil {
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
	ldevice, err := agent.getLogicalDeviceWithoutLock()
	if err != nil {
		return false, status.Error(codes.NotFound, agent.logicalDeviceID)
	}
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

func (agent *LogicalDeviceAgent) packetOut(packet *ofp.OfpPacketOut) {
	log.Debugw("packet-out", log.Fields{
		"packet": hex.EncodeToString(packet.Data),
		"inPort": packet.GetInPort(),
	})
	outPort := fu.GetPacketOutPort(packet)
	//frame := packet.GetData()
	//TODO: Use a channel between the logical agent and the device agent
	if err := agent.deviceMgr.packetOut(agent.rootDeviceID, outPort, packet); err != nil {
		log.Error("packetout-failed", log.Fields{"logicalDeviceID": agent.rootDeviceID})
	}
}

func (agent *LogicalDeviceAgent) packetIn(port uint32, transactionID string, packet []byte) {
	log.Debugw("packet-in", log.Fields{
		"port":          port,
		"packet":        hex.EncodeToString(packet),
		"transactionId": transactionID,
	})
	packetIn := fu.MkPacketIn(port, packet)
	agent.ldeviceMgr.grpcNbiHdlr.sendPacketIn(agent.logicalDeviceID, transactionID, packetIn)
	log.Debugw("sending-packet-in", log.Fields{"packet": hex.EncodeToString(packetIn.Data)})
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
