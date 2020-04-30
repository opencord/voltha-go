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

package device

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	fd "github.com/opencord/voltha-go/rw_core/flowdecomposition"
	"github.com/opencord/voltha-go/rw_core/route"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalAgent represent attributes of logical device agent
type LogicalAgent struct {
	logicalDeviceID    string
	serialNumber       string
	rootDeviceID       string
	deviceMgr          *Manager
	ldeviceMgr         *LogicalManager
	clusterDataProxy   *model.Proxy
	stopped            bool
	deviceRoutes       *route.DeviceRoutes
	lockDeviceRoutes   sync.RWMutex
	logicalPortsNo     map[uint32]bool //value is true for NNI port
	lockLogicalPortsNo sync.RWMutex
	flowDecomposer     *fd.FlowDecomposer
	defaultTimeout     time.Duration
	logicalDevice      *voltha.LogicalDevice
	requestQueue       *coreutils.RequestQueue
	startOnce          sync.Once
	stopOnce           sync.Once

	meters    map[uint32]*MeterChunk
	meterLock sync.RWMutex
	flows     map[uint64]*FlowChunk
	flowLock  sync.RWMutex
	groups    map[uint32]*GroupChunk
	groupLock sync.RWMutex
}

//MeterChunk keeps a meter entry and its lock. The lock in the struct is used to syncronize the
//modifications for the related meter.
type MeterChunk struct {
	meter *ofp.OfpMeterEntry
	lock  sync.Mutex
}

//FlowChunk keeps a flow and the lock for this flow. The lock in the struct is used to syncronize the
//modifications for the related flow.
type FlowChunk struct {
	flow *ofp.OfpFlowStats
	lock sync.Mutex
}

//GroupChunk keeps a group entry and its lock. The lock in the struct is used to syncronize the
//modifications for the related group.
type GroupChunk struct {
	group *ofp.OfpGroupEntry
	lock  sync.Mutex
}

func newLogicalDeviceAgent(id string, sn string, deviceID string, ldeviceMgr *LogicalManager,
	deviceMgr *Manager, cdProxy *model.Proxy, timeout time.Duration) *LogicalAgent {
	var agent LogicalAgent
	agent.logicalDeviceID = id
	agent.serialNumber = sn
	agent.rootDeviceID = deviceID
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.flowDecomposer = fd.NewFlowDecomposer(agent.deviceMgr)
	agent.logicalPortsNo = make(map[uint32]bool)
	agent.defaultTimeout = timeout
	agent.requestQueue = coreutils.NewRequestQueue()
	agent.meters = make(map[uint32]*MeterChunk)
	agent.flows = make(map[uint64]*FlowChunk)
	agent.groups = make(map[uint32]*GroupChunk)
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalAgent) start(ctx context.Context, loadFromDB bool) error {
	needToStart := false
	if agent.startOnce.Do(func() { needToStart = true }); !needToStart {
		return nil
	}

	logger.Infow("starting-logical_device-agent", log.Fields{"logical-device-id": agent.logicalDeviceID, "load-from-db": loadFromDB})

	var startSucceeded bool
	defer func() {
		if !startSucceeded {
			if err := agent.stop(ctx); err != nil {
				logger.Errorw("failed-to-cleanup-after-unsuccessful-start", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
			}
		}
	}()

	var ld *voltha.LogicalDevice
	if !loadFromDB {
		//Build the logical device based on information retrieved from the device adapter
		var switchCap *ic.SwitchCapability
		var err error
		if switchCap, err = agent.deviceMgr.getSwitchCapability(ctx, agent.rootDeviceID); err != nil {
			return err
		}
		ld = &voltha.LogicalDevice{Id: agent.logicalDeviceID, RootDeviceId: agent.rootDeviceID}

		// Create the datapath ID (uint64) using the logical device ID (based on the MAC Address)
		var datapathID uint64
		if datapathID, err = coreutils.CreateDataPathID(agent.serialNumber); err != nil {
			return err
		}
		ld.DatapathId = datapathID
		ld.Desc = (proto.Clone(switchCap.Desc)).(*ofp.OfpDesc)
		logger.Debugw("Switch-capability", log.Fields{"Desc": ld.Desc, "fromAd": switchCap.Desc})
		ld.SwitchFeatures = (proto.Clone(switchCap.SwitchFeatures)).(*ofp.OfpSwitchFeatures)
		ld.Flows = &ofp.Flows{Items: nil}
		ld.FlowGroups = &ofp.FlowGroups{Items: nil}
		ld.Ports = []*voltha.LogicalPort{}

		// Save the logical device
		if err := agent.clusterDataProxy.AddWithID(ctx, "logical_devices", ld.Id, ld); err != nil {
			logger.Errorw("failed-to-add-logical-device", log.Fields{"logical-device-id": agent.logicalDeviceID})
			return err
		}
		logger.Debugw("logicaldevice-created", log.Fields{"logical-device-id": agent.logicalDeviceID, "root-id": ld.RootDeviceId})

		agent.logicalDevice = proto.Clone(ld).(*voltha.LogicalDevice)

		// Setup the logicalports - internal processing, no need to propagate the client context
		go func() {
			err := agent.setupLogicalPorts(context.Background())
			if err != nil {
				logger.Errorw("unable-to-setup-logical-ports", log.Fields{"error": err})
			}
		}()
	} else {
		//	load from dB - the logical may not exist at this time.  On error, just return and the calling function
		// will destroy this agent.
		ld := &voltha.LogicalDevice{}
		have, err := agent.clusterDataProxy.Get(ctx, "logical_devices/"+agent.logicalDeviceID, ld)
		if err != nil {
			return err
		} else if !have {
			return status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
		}

		// Update the root device Id
		agent.rootDeviceID = ld.RootDeviceId

		// Update the last data
		agent.logicalDevice = proto.Clone(ld).(*voltha.LogicalDevice)

		// Setup the local list of logical ports
		agent.addLogicalPortsToMap(ld.Ports)
		// load the flows, meters and groups from KV to cache
		agent.loadFlows(ctx)
		agent.loadMeters(ctx)
		agent.loadGroups(ctx)
	}

	// Setup the device routes. Building routes may fail if the pre-conditions are not satisfied (e.g. no PON ports present)
	if loadFromDB {
		go func() {
			if err := agent.buildRoutes(context.Background()); err != nil {
				logger.Warn("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
			}
		}()
	}
	startSucceeded = true

	return nil
}

// stop stops the logical device agent.  This removes the logical device from the data model.
func (agent *LogicalAgent) stop(ctx context.Context) error {
	var returnErr error
	agent.stopOnce.Do(func() {
		logger.Info("stopping-logical_device-agent")

		if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
			// This should never happen - an error is returned only if the agent is stopped and an agent is only stopped once.
			returnErr = err
			return
		}
		defer agent.requestQueue.RequestComplete()

		//Remove the logical device from the model
		if err := agent.clusterDataProxy.Remove(ctx, "logical_devices/"+agent.logicalDeviceID); err != nil {
			returnErr = err
		} else {
			logger.Debugw("logicaldevice-removed", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		}

		agent.stopped = true

		logger.Info("logical_device-agent-stopped")
	})
	return returnErr
}

// GetLogicalDevice returns the latest logical device data
func (agent *LogicalAgent) GetLogicalDevice(ctx context.Context) (*voltha.LogicalDevice, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	return proto.Clone(agent.logicalDevice).(*voltha.LogicalDevice), nil
}

// ListLogicalDeviceFlows returns logical device flows
func (agent *LogicalAgent) ListLogicalDeviceFlows(ctx context.Context) (*ofp.Flows, error) {
	logger.Debug("ListLogicalDeviceFlows")
	var flowStats []*ofp.OfpFlowStats
	agent.flowLock.RLock()
	defer agent.flowLock.RUnlock()
	for _, flowChunk := range agent.flows {
		flowStats = append(flowStats, (proto.Clone(flowChunk.flow)).(*ofp.OfpFlowStats))
	}
	return &ofp.Flows{Items: flowStats}, nil
}

// ListLogicalDeviceMeters returns logical device meters
func (agent *LogicalAgent) ListLogicalDeviceMeters(ctx context.Context) (*ofp.Meters, error) {
	logger.Debug("ListLogicalDeviceMeters")

	var meterEntries []*ofp.OfpMeterEntry
	agent.meterLock.RLock()
	defer agent.meterLock.RUnlock()
	for _, meterChunk := range agent.meters {
		meterEntries = append(meterEntries, (proto.Clone(meterChunk.meter)).(*ofp.OfpMeterEntry))
	}
	return &ofp.Meters{Items: meterEntries}, nil
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (agent *LogicalAgent) ListLogicalDeviceFlowGroups(ctx context.Context) (*ofp.FlowGroups, error) {
	logger.Debug("ListLogicalDeviceFlowGroups")

	var groupEntries []*ofp.OfpGroupEntry
	agent.groupLock.RLock()
	defer agent.groupLock.RUnlock()
	for _, value := range agent.groups {
		groupEntries = append(groupEntries, (proto.Clone(value.group)).(*ofp.OfpGroupEntry))
	}
	return &ofp.FlowGroups{Items: groupEntries}, nil
}

// ListLogicalDevicePorts returns logical device ports
func (agent *LogicalAgent) ListLogicalDevicePorts(ctx context.Context) (*voltha.LogicalPorts, error) {
	logger.Debug("ListLogicalDevicePorts")
	logicalDevice, err := agent.GetLogicalDevice(ctx)
	if err != nil {
		return nil, err
	}
	if logicalDevice == nil {
		return &voltha.LogicalPorts{}, nil
	}
	lPorts := make([]*voltha.LogicalPort, 0)
	lPorts = append(lPorts, logicalDevice.Ports...)
	return &voltha.LogicalPorts{Items: lPorts}, nil
}

//updateLogicalDeviceFlow updates flow in the store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) updateLogicalDeviceFlow(ctx context.Context, flow *ofp.OfpFlowStats, flowChunk *FlowChunk) error {
	path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flow.Id)
	if err := agent.clusterDataProxy.Update(ctx, path, flow); err != nil {
		return status.Errorf(codes.Internal, "failed-update-flow:%s:%d %s", agent.logicalDeviceID, flow.Id, err)
	}
	flowChunk.flow = flow
	return nil
}

//removeLogicalDeviceFlow deletes the flow from store and cache.
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) removeLogicalDeviceFlow(ctx context.Context, flowID uint64) error {
	path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flowID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-flow-from-the-store-%s", path)
	}
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	delete(agent.flows, flowID)
	return nil
}

//updateLogicalDeviceMeter updates meter info in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) updateLogicalDeviceMeter(ctx context.Context, meter *ofp.OfpMeterEntry, meterChunk *MeterChunk) error {
	path := fmt.Sprintf("meters/%s/%d", agent.logicalDeviceID, meter.Config.MeterId)
	if err := agent.clusterDataProxy.Update(ctx, path, meter); err != nil {
		logger.Errorw("error-updating-logical-device-with-meters", log.Fields{"error": err})
		return err
	}
	meterChunk.meter = meter
	return nil
}

//removeLogicalDeviceMeter deletes the meter from store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) removeLogicalDeviceMeter(ctx context.Context, meterID uint32) error {
	path := fmt.Sprintf("meters/%s/%d", agent.logicalDeviceID, meterID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-meter-from-store-%s", path)
	}
	agent.meterLock.Lock()
	defer agent.meterLock.Unlock()
	delete(agent.meters, meterID)
	return nil
}

//updateLogicalDeviceFlowGroup updates the flow groups in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) updateLogicalDeviceFlowGroup(ctx context.Context, groupEntry *ofp.OfpGroupEntry, groupChunk *GroupChunk) error {
	path := fmt.Sprintf("groups/%s/%d", agent.logicalDeviceID, groupEntry.Desc.GroupId)
	if err := agent.clusterDataProxy.Update(ctx, path, groupEntry); err != nil {
		logger.Errorw("error-updating-logical-device-with-group", log.Fields{"error": err})
		return err
	}
	groupChunk.group = groupEntry
	return nil
}

//removeLogicalDeviceFlowGroup removes the flow groups in store and cache
//It is assumed that the chunk lock has been acquired before this function is called
func (agent *LogicalAgent) removeLogicalDeviceFlowGroup(ctx context.Context, groupID uint32) error {
	path := fmt.Sprintf("groups/%s/%d", agent.logicalDeviceID, groupID)
	if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
		return fmt.Errorf("couldnt-delete-group-from-store-%s", path)
	}
	agent.groupLock.Lock()
	defer agent.groupLock.Unlock()
	delete(agent.groups, groupID)
	return nil
}

// getLogicalDeviceWithoutLock returns a cloned logical device to a function that already holds the agent lock.
func (agent *LogicalAgent) getLogicalDeviceWithoutLock() *voltha.LogicalDevice {
	logger.Debug("getLogicalDeviceWithoutLock")
	return proto.Clone(agent.logicalDevice).(*voltha.LogicalDevice)
}

func (agent *LogicalAgent) updateLogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) error {
	logger.Debugw("updateLogicalPort", log.Fields{"deviceId": device.Id, "port": port})
	var err error
	if port.Type == voltha.Port_ETHERNET_NNI {
		if _, err = agent.addNNILogicalPort(ctx, device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, true)
	} else if port.Type == voltha.Port_ETHERNET_UNI {
		if _, err = agent.addUNILogicalPort(ctx, device, port); err != nil {
			return err
		}
		agent.addLogicalPortToMap(port.PortNo, false)
	} else {
		// Update the device routes to ensure all routes on the logical device have been calculated
		if err = agent.buildRoutes(ctx); err != nil {
			// Not an error - temporary state
			logger.Warnw("failed-to-update-routes", log.Fields{"device-id": device.Id, "port": port, "error": err})
		}
	}
	return nil
}

// setupLogicalPorts is invoked once the logical device has been created and is ready to get ports
// added to it.  While the logical device was being created we could have received requests to add
// NNI and UNI ports which were discarded.  Now is the time to add them if needed
func (agent *LogicalAgent) setupLogicalPorts(ctx context.Context) error {
	logger.Infow("setupLogicalPorts", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// First add any NNI ports which could have been missing
	if err := agent.setupNNILogicalPorts(ctx, agent.rootDeviceID); err != nil {
		logger.Errorw("error-setting-up-NNI-ports", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}

	// Now, set up the UNI ports if needed.
	children, err := agent.deviceMgr.GetAllChildDevices(ctx, agent.rootDeviceID)
	if err != nil {
		logger.Errorw("error-getting-child-devices", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}
	responses := make([]coreutils.Response, 0)
	for _, child := range children.Items {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(child *voltha.Device) {
			if err = agent.setupUNILogicalPorts(context.Background(), child); err != nil {
				logger.Error("setting-up-UNI-ports-failed", log.Fields{"deviceID": child.Id})
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
func (agent *LogicalAgent) setupNNILogicalPorts(ctx context.Context, deviceID string) error {
	logger.Infow("setupNNILogicalPorts-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error

	var device *voltha.Device
	if device, err = agent.deviceMgr.getDevice(ctx, deviceID); err != nil {
		logger.Errorw("error-retrieving-device", log.Fields{"error": err, "deviceId": deviceID})
		return err
	}

	//Get UNI port number
	for _, port := range device.Ports {
		if port.Type == voltha.Port_ETHERNET_NNI {
			if _, err = agent.addNNILogicalPort(ctx, device, port); err != nil {
				logger.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			agent.addLogicalPortToMap(port.PortNo, true)
		}
	}
	return err
}

// updatePortState updates the port state of the device
func (agent *LogicalAgent) updatePortState(ctx context.Context, deviceID string, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Infow("updatePortState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "portNo": portNo, "state": operStatus})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	original := agent.getLogicalDeviceWithoutLock()
	updatedPorts := clonePorts(original.Ports)
	for _, port := range updatedPorts {
		if port.DeviceId == deviceID && port.DevicePortNo == portNo {
			if operStatus == voltha.OperStatus_ACTIVE {
				port.OfpPort.Config = port.OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				port.OfpPort.Config = port.OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}
			// Update the logical device
			if err := agent.updateLogicalDevicePortsWithoutLock(ctx, original, updatedPorts); err != nil {
				logger.Errorw("error-updating-logical-device", log.Fields{"error": err})
				return err
			}
			return nil
		}
	}
	return status.Errorf(codes.NotFound, "port-%d-not-exist", portNo)
}

// updatePortsState updates the ports state related to the device
func (agent *LogicalAgent) updatePortsState(ctx context.Context, device *voltha.Device, state voltha.OperStatus_Types) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	original := agent.getLogicalDeviceWithoutLock()
	updatedPorts := clonePorts(original.Ports)
	for _, port := range updatedPorts {
		if port.DeviceId == device.Id {
			if state == voltha.OperStatus_ACTIVE {
				port.OfpPort.Config = port.OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				port.OfpPort.Config = port.OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				port.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}
		}
	}
	// Updating the logical device will trigger the poprt change events to be populated to the controller
	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, original, updatedPorts); err != nil {
		logger.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
}

// setupUNILogicalPorts creates a UNI port on the logical device that represents a child UNI interface
func (agent *LogicalAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
	logger.Infow("setupUNILogicalPort", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error
	var added bool
	//Get UNI port number
	for _, port := range childDevice.Ports {
		if port.Type == voltha.Port_ETHERNET_UNI {
			if added, err = agent.addUNILogicalPort(ctx, childDevice, port); err != nil {
				logger.Errorw("error-adding-UNI-port", log.Fields{"error": err})
			}
			if added {
				agent.addLogicalPortToMap(port.PortNo, false)
			}
		}
	}
	return err
}

// deleteAllLogicalPorts deletes all logical ports associated with this logical device
func (agent *LogicalAgent) deleteAllLogicalPorts(ctx context.Context) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	cloned := agent.getLogicalDeviceWithoutLock()

	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, cloned, []*voltha.LogicalPort{}); err != nil {
		logger.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
}

func clonePorts(ports []*voltha.LogicalPort) []*voltha.LogicalPort {
	return proto.Clone(&voltha.LogicalPorts{Items: ports}).(*voltha.LogicalPorts).Items
}

//updateLogicalDevicePortsWithoutLock updates the
func (agent *LogicalAgent) updateLogicalDevicePortsWithoutLock(ctx context.Context, device *voltha.LogicalDevice, newPorts []*voltha.LogicalPort) error {
	oldPorts := device.Ports
	device.Ports = newPorts
	if err := agent.updateLogicalDeviceWithoutLock(ctx, device); err != nil {
		return err
	}
	agent.portUpdated(oldPorts, newPorts)
	return nil
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalAgent) updateLogicalDeviceWithoutLock(ctx context.Context, logicalDevice *voltha.LogicalDevice) error {
	if agent.stopped {
		return fmt.Errorf("logical device agent stopped-%s", logicalDevice.Id)
	}

	updateCtx := context.WithValue(ctx, model.RequestTimestamp, time.Now().UnixNano())
	if err := agent.clusterDataProxy.Update(updateCtx, "logical_devices/"+agent.logicalDeviceID, logicalDevice); err != nil {
		logger.Errorw("failed-to-update-logical-devices-to-cluster-proxy", log.Fields{"error": err})
		return err
	}

	agent.logicalDevice = logicalDevice

	return nil
}

//generateDeviceRoutesIfNeeded generates the device routes if the logical device has been updated since the last time
//that device graph was generated.
func (agent *LogicalAgent) generateDeviceRoutesIfNeeded(ctx context.Context) error {
	agent.lockDeviceRoutes.Lock()
	defer agent.lockDeviceRoutes.Unlock()

	ld, err := agent.GetLogicalDevice(ctx)
	if err != nil {
		return err
	}

	if agent.deviceRoutes != nil && agent.deviceRoutes.IsUpToDate(ld) {
		return nil
	}
	logger.Debug("Generation of device route required")
	if err := agent.buildRoutes(ctx); err != nil {
		// No Route is not an error
		if !errors.Is(err, route.ErrNoRoute) {
			return err
		}
	}
	return nil
}

//updateFlowTable updates the flow table of that logical device
func (agent *LogicalAgent) updateFlowTable(ctx context.Context, flow *ofp.OfpFlowMod) error {
	logger.Debug("UpdateFlowTable")
	if flow == nil {
		return nil
	}

	if err := agent.generateDeviceRoutesIfNeeded(ctx); err != nil {
		return err
	}
	switch flow.GetCommand() {
	case ofp.OfpFlowModCommand_OFPFC_ADD:
		return agent.flowAdd(ctx, flow)
	case ofp.OfpFlowModCommand_OFPFC_DELETE:
		return agent.flowDelete(ctx, flow)
	case ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT:
		return agent.flowDeleteStrict(ctx, flow)
	case ofp.OfpFlowModCommand_OFPFC_MODIFY:
		return agent.flowModify(flow)
	case ofp.OfpFlowModCommand_OFPFC_MODIFY_STRICT:
		return agent.flowModifyStrict(flow)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, flow.GetCommand())
}

//updateGroupTable updates the group table of that logical device
func (agent *LogicalAgent) updateGroupTable(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("updateGroupTable")
	if groupMod == nil {
		return nil
	}

	if err := agent.generateDeviceRoutesIfNeeded(ctx); err != nil {
		return err
	}

	switch groupMod.GetCommand() {
	case ofp.OfpGroupModCommand_OFPGC_ADD:
		return agent.groupAdd(ctx, groupMod)
	case ofp.OfpGroupModCommand_OFPGC_DELETE:
		return agent.groupDelete(ctx, groupMod)
	case ofp.OfpGroupModCommand_OFPGC_MODIFY:
		return agent.groupModify(ctx, groupMod)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, groupMod.GetCommand())
}

// updateMeterTable updates the meter table of that logical device
func (agent *LogicalAgent) updateMeterTable(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("updateMeterTable")
	if meterMod == nil {
		return nil
	}
	switch meterMod.GetCommand() {
	case ofp.OfpMeterModCommand_OFPMC_ADD:
		return agent.meterAdd(ctx, meterMod)
	case ofp.OfpMeterModCommand_OFPMC_DELETE:
		return agent.meterDelete(ctx, meterMod)
	case ofp.OfpMeterModCommand_OFPMC_MODIFY:
		return agent.meterModify(ctx, meterMod)
	}
	return status.Errorf(codes.Internal,
		"unhandled-command: lDeviceId:%s, command:%s", agent.logicalDeviceID, meterMod.GetCommand())

}

func (agent *LogicalAgent) meterAdd(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debugw("meterAdd", log.Fields{"metermod": *meterMod})
	if meterMod == nil {
		return nil
	}

	meterEntry := fu.MeterEntryFromMeterMod(meterMod)
	agent.meterLock.Lock()
	//check if the meter already exists or not
	_, ok := agent.meters[meterMod.MeterId]
	if ok {
		logger.Infow("Meter-already-exists", log.Fields{"meter": *meterMod})
		agent.meterLock.Unlock()
		return nil
	}

	mChunk := MeterChunk{
		meter: meterEntry,
	}
	//Add to map and acquire the per meter lock
	agent.meters[meterMod.MeterId] = &mChunk
	mChunk.lock.Lock()
	defer mChunk.lock.Unlock()
	agent.meterLock.Unlock()
	meterID := strconv.Itoa(int(meterMod.MeterId))
	if err := agent.clusterDataProxy.AddWithID(ctx, "meters/"+agent.logicalDeviceID, meterID, meterEntry); err != nil {
		logger.Errorw("failed-adding-meter", log.Fields{"deviceID": agent.logicalDeviceID, "meterID": meterID, "err": err})
		//Revert the map
		agent.meterLock.Lock()
		delete(agent.meters, meterMod.MeterId)
		agent.meterLock.Unlock()
		return err
	}

	logger.Debugw("Meter-added-successfully", log.Fields{"Added-meter": meterEntry})
	return nil
}

func (agent *LogicalAgent) meterDelete(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("meterDelete", log.Fields{"meterMod": *meterMod})
	if meterMod == nil {
		return nil
	}
	agent.meterLock.RLock()
	meterChunk, ok := agent.meters[meterMod.MeterId]
	agent.meterLock.RUnlock()
	if ok {
		//Dont let anyone to do any changes to this meter until this is done.
		//And wait if someone else is already making modifications. Do this with per meter lock.
		meterChunk.lock.Lock()
		defer meterChunk.lock.Unlock()
		if err := agent.deleteFlowsOfMeter(ctx, meterMod.MeterId); err != nil {
			return err
		}
		//remove from the store and cache
		if err := agent.removeLogicalDeviceMeter(ctx, meterMod.MeterId); err != nil {
			return err
		}
		logger.Debugw("meterDelete-success", log.Fields{"meterID": meterMod.MeterId})
	} else {
		logger.Warnw("meter-not-found", log.Fields{"meterID": meterMod.MeterId})
	}
	return nil
}

func (agent *LogicalAgent) meterModify(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("meterModify")
	if meterMod == nil {
		return nil
	}
	newMeter := fu.MeterEntryFromMeterMod(meterMod)
	agent.meterLock.RLock()
	meterChunk, ok := agent.meters[newMeter.Config.MeterId]
	agent.meterLock.RUnlock()
	if !ok {
		return fmt.Errorf("no-meter-to-modify:%d", newMeter.Config.MeterId)
	}
	//Release the map lock and syncronize per meter
	meterChunk.lock.Lock()
	defer meterChunk.lock.Unlock()
	oldMeter := meterChunk.meter
	newMeter.Stats.FlowCount = oldMeter.Stats.FlowCount

	if err := agent.updateLogicalDeviceMeter(ctx, newMeter, meterChunk); err != nil {
		logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "meterID": newMeter.Config.MeterId})
		return err
	}
	logger.Debugw("replaced-with-new-meter", log.Fields{"oldMeter": oldMeter, "newMeter": newMeter})
	return nil

}

func (agent *LogicalAgent) deleteFlowsOfMeter(ctx context.Context, meterID uint32) error {
	logger.Infow("Delete-flows-matching-meter", log.Fields{"meter": meterID})
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	for flowID, flowChunk := range agent.flows {
		if mID := fu.GetMeterIdFromFlow(flowChunk.flow); mID != 0 && mID == meterID {
			logger.Debugw("Flow-to-be- deleted", log.Fields{"flow": flowChunk.flow})
			path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flowID)
			if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
				//TODO: Think on carrying on and deleting the remaining flows, instead of returning.
				//Anyways this returns an error to controller which possibly results with a re-deletion.
				//Then how can we handle the new deletion request(Same for group deletion)?
				return fmt.Errorf("couldnt-deleted-flow-from-store-%s", path)
			}
			delete(agent.flows, flowID)
		}
	}
	return nil
}

func (agent *LogicalAgent) updateFlowCountOfMeterStats(ctx context.Context, modCommand *ofp.OfpFlowMod, flow *ofp.OfpFlowStats, revertUpdate bool) bool {

	flowCommand := modCommand.GetCommand()
	meterID := fu.GetMeterIdFromFlow(flow)
	logger.Debugw("Meter-id-in-flow-mod", log.Fields{"meterId": meterID})
	if meterID == 0 {
		logger.Debugw("No-meter-present-in-the-flow", log.Fields{"flow": *flow})
		return true
	}

	if flowCommand != ofp.OfpFlowModCommand_OFPFC_ADD && flowCommand != ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT {
		return true
	}
	agent.meterLock.RLock()
	meterChunk, ok := agent.meters[meterID]
	agent.meterLock.RUnlock()
	if !ok {
		logger.Debugw("Meter-is-not-present-in-logical-device", log.Fields{"meterID": meterID})
		return true
	}

	//acquire the meter lock
	meterChunk.lock.Lock()
	defer meterChunk.lock.Unlock()

	if flowCommand == ofp.OfpFlowModCommand_OFPFC_ADD {
		if revertUpdate {
			meterChunk.meter.Stats.FlowCount--
		} else {
			meterChunk.meter.Stats.FlowCount++
		}
	} else if flowCommand == ofp.OfpFlowModCommand_OFPFC_DELETE_STRICT {
		if revertUpdate {
			meterChunk.meter.Stats.FlowCount++
		} else {
			meterChunk.meter.Stats.FlowCount--
		}
	}

	//	Update store and cache
	if err := agent.updateLogicalDeviceMeter(ctx, meterChunk.meter, meterChunk); err != nil {
		logger.Debugw("unable-to-update-meter-in-db", log.Fields{"logicalDevice": agent.logicalDeviceID, "meterID": meterID})
		return false
	}

	logger.Debugw("updated-meter-flow-stats", log.Fields{"meterId": meterID})
	return true
}

//flowAdd adds a flow to the flow table of that logical device
func (agent *LogicalAgent) flowAdd(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debugw("flowAdd", log.Fields{"flow": mod})
	if mod == nil {
		return nil
	}
	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		logger.Errorw("flowAdd-failed", log.Fields{"flowMod": mod, "err": err})
		return err
	}
	var updated bool
	var changed bool
	if changed, updated, err = agent.decomposeAndAdd(ctx, flow, mod); err != nil {
		logger.Errorw("flow-decompose-and-add-failed ", log.Fields{"flowMod": mod, "err": err})
		return err
	}
	if changed && !updated {
		if dbupdated := agent.updateFlowCountOfMeterStats(ctx, mod, flow, false); !dbupdated {
			return fmt.Errorf("couldnt-updated-flow-stats-%s", strconv.FormatUint(flow.Id, 10))
		}
	}
	return nil

}

func (agent *LogicalAgent) decomposeAndAdd(ctx context.Context, flow *ofp.OfpFlowStats, mod *ofp.OfpFlowMod) (bool, bool, error) {
	changed := false
	updated := false
	alreadyExist := true
	var flowToReplace *ofp.OfpFlowStats

	//if flow is not found in the map, create a new entry, otherwise get the existing one.
	agent.flowLock.Lock()
	flowChunk, ok := agent.flows[flow.Id]
	if !ok {
		flowChunk = &FlowChunk{
			flow: flow,
		}
		agent.flows[flow.Id] = flowChunk
		alreadyExist = false
		flowChunk.lock.Lock() //acquire chunk lock before releasing map lock
		defer flowChunk.lock.Unlock()
		agent.flowLock.Unlock()
	} else {
		agent.flowLock.Unlock() //release map lock before acquiring chunk lock
		flowChunk.lock.Lock()
		defer flowChunk.lock.Unlock()
	}

	if !alreadyExist {
		flowID := strconv.FormatUint(flow.Id, 10)
		if err := agent.clusterDataProxy.AddWithID(ctx, "logical_flows/"+agent.logicalDeviceID, flowID, flow); err != nil {
			logger.Errorw("failed-adding-flow-to-db", log.Fields{"deviceID": agent.logicalDeviceID, "flowID": flowID, "err": err})
			//Revert the map
			//TODO: Solve the condition:If we have two flow Adds of the same flow (at least same priority and match) in quick succession
			//then if the first one fails while the second one was waiting on the flowchunk, we will end up with an instance of flowChunk that is no longer in the map.
			agent.flowLock.Lock()
			delete(agent.flows, flow.Id)
			agent.flowLock.Unlock()
			return changed, updated, err
		}
	}
	flows := make([]*ofp.OfpFlowStats, 0)
	updatedFlows := make([]*ofp.OfpFlowStats, 0)
	checkOverlap := (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_CHECK_OVERLAP)) != 0
	if checkOverlap {
		if overlapped := fu.FindOverlappingFlows(flows, mod); len(overlapped) != 0 {
			//	TODO:  should this error be notified other than being logged?
			logger.Warnw("overlapped-flows", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		} else {
			//	Add flow
			changed = true
		}
	} else {
		if alreadyExist {
			flowToReplace = flowChunk.flow
			if (mod.Flags & uint32(ofp.OfpFlowModFlags_OFPFF_RESET_COUNTS)) != 0 {
				flow.ByteCount = flowToReplace.ByteCount
				flow.PacketCount = flowToReplace.PacketCount
			}
			if !proto.Equal(flowToReplace, flow) {
				changed = true
				updated = true
			}
		} else {
			changed = true
		}
	}
	logger.Debugw("flowAdd-changed", log.Fields{"changed": changed, "updated": updated})
	if changed {
		updatedFlows = append(updatedFlows, flow)
		var flowMetadata voltha.FlowMetadata
		lMeters, _ := agent.ListLogicalDeviceMeters(ctx)
		if err := agent.GetMeterConfig(updatedFlows, lMeters.Items, &flowMetadata); err != nil {
			logger.Error("Meter-referred-in-flow-not-present")
			return changed, updated, err
		}
		flowGroups, _ := agent.ListLogicalDeviceFlowGroups(ctx)
		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: updatedFlows}, *flowGroups)
		if err != nil {
			return changed, updated, err
		}

		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})
		//	Update store and cache
		if updated {
			if err := agent.updateLogicalDeviceFlow(ctx, flow, flowChunk); err != nil {
				return changed, updated, err
			}
		}
		respChannels := agent.addFlowsAndGroupsToDevices(ctx, deviceRules, &flowMetadata)
		// Create the go routines to wait
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChannels...); res != nil {
				logger.Infow("failed-to-add-flows-will-attempt-deletion", log.Fields{"errors": res, "logical-device-id": agent.logicalDeviceID})
				// Revert added flows
				if err := agent.revertAddedFlows(context.Background(), mod, flow, flowToReplace, deviceRules, &flowMetadata); err != nil {
					logger.Errorw("failure-to-delete-flows-after-failed-addition", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
				}
			}
		}()
	}
	return changed, updated, nil
}

// revertAddedFlows reverts flows after the flowAdd request has failed.  All flows corresponding to that flowAdd request
// will be reverted, both from the logical devices and the devices.
func (agent *LogicalAgent) revertAddedFlows(ctx context.Context, mod *ofp.OfpFlowMod, addedFlow *ofp.OfpFlowStats, replacedFlow *ofp.OfpFlowStats, deviceRules *fu.DeviceRules, metadata *voltha.FlowMetadata) error {
	logger.Debugw("revertFlowAdd", log.Fields{"added-flow": addedFlow, "replaced-flow": replacedFlow, "device-rules": deviceRules, "metadata": metadata})

	agent.flowLock.RLock()
	flowChunk, ok := agent.flows[addedFlow.Id]
	agent.flowLock.RUnlock()
	if !ok {
		// Not found - do nothing
		log.Debugw("flow-not-found", log.Fields{"added-flow": addedFlow})
		return nil
	}
	//Leave the map lock and syncronize per flow
	flowChunk.lock.Lock()
	defer flowChunk.lock.Unlock()

	if replacedFlow != nil {
		if err := agent.updateLogicalDeviceFlow(ctx, replacedFlow, flowChunk); err != nil {
			return err
		}
	} else {
		if err := agent.removeLogicalDeviceFlow(ctx, addedFlow.Id); err != nil {
			return err
		}
	}
	// Revert meters
	if changedMeterStats := agent.updateFlowCountOfMeterStats(ctx, mod, addedFlow, true); !changedMeterStats {
		return fmt.Errorf("Unable-to-revert-meterstats-for-flow-%s", strconv.FormatUint(addedFlow.Id, 10))
	}

	// Update the devices
	respChnls := agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, metadata)

	// Wait for the responses
	go func() {
		// Since this action is taken following an add failure, we may also receive a failure for the revert
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-reverting-added-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
		}
	}()

	return nil
}

// GetMeterConfig returns meter config
func (agent *LogicalAgent) GetMeterConfig(flows []*ofp.OfpFlowStats, meters []*ofp.OfpMeterEntry, metadata *voltha.FlowMetadata) error {
	m := make(map[uint32]bool)
	for _, flow := range flows {
		if flowMeterID := fu.GetMeterIdFromFlow(flow); flowMeterID != 0 && !m[flowMeterID] {
			foundMeter := false
			// Meter is present in the flow , Get from logical device
			for _, meter := range meters {
				if flowMeterID == meter.Config.MeterId {
					metadata.Meters = append(metadata.Meters, meter.Config)
					logger.Debugw("Found meter in logical device",
						log.Fields{"meterID": flowMeterID, "meter-band": meter.Config})
					m[flowMeterID] = true
					foundMeter = true
					break
				}
			}
			if !foundMeter {
				logger.Errorw("Meter-referred-by-flow-is-not-found-in-logicaldevice",
					log.Fields{"meterID": flowMeterID, "Available-meters": meters, "flow": *flow})
				return fmt.Errorf("Meter-referred-by-flow-is-not-found-in-logicaldevice.MeterId-%d", flowMeterID)
			}
		}
	}
	logger.Debugw("meter-bands-for-flows", log.Fields{"flows": len(flows), "metadata": metadata})
	return nil

}

//flowDelete deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowDelete(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debug("flowDelete")
	if mod == nil {
		return nil
	}

	fs, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}

	//build a list of what to delete
	toDelete := make([]*ofp.OfpFlowStats, 0)
	toDeleteChunks := make([]*FlowChunk, 0)
	//Lock the map to search the matched flows
	agent.flowLock.RLock()
	for _, f := range agent.flows {
		if fu.FlowMatch(f.flow, fs) {
			toDelete = append(toDelete, f.flow)
			toDeleteChunks = append(toDeleteChunks, f)
			continue
		}
		// Check wild card match
		if fu.FlowMatchesMod(f.flow, mod) {
			toDelete = append(toDelete, f.flow)
			toDeleteChunks = append(toDeleteChunks, f)
		}
	}
	agent.flowLock.RUnlock()
	//Delete the matched flows
	if len(toDelete) > 0 {
		logger.Debugw("flowDelete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "toDelete": len(toDelete)})
		var meters []*ofp.OfpMeterEntry
		var flowGroups []*ofp.OfpGroupEntry
		if ofpMeters, err := agent.ListLogicalDeviceMeters(ctx); err != nil {
			meters = ofpMeters.Items
		}

		if groups, err := agent.ListLogicalDeviceFlowGroups(ctx); err != nil {
			flowGroups = groups.Items
		}

		for _, fc := range toDeleteChunks {
			if err := agent.deleteFlowAndUpdateMeterStats(ctx, mod, fc); err != nil {
				return err
			}
		}
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(toDelete, meters, &flowMetadata); err != nil { // This should never happen
			logger.Error("Meter-referred-in-flows-not-present")
			return err
		}
		var respChnls []coreutils.Response
		var partialRoute bool
		var deviceRules *fu.DeviceRules
		deviceRules, err = agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: toDelete}, ofp.FlowGroups{Items: flowGroups})
		if err != nil {
			// A no route error means no route exists between the ports specified in the flow. This can happen when the
			// child device is deleted and a request to delete flows from the parent device is received
			if !errors.Is(err, route.ErrNoRoute) {
				logger.Errorw("unexpected-error-received", log.Fields{"flows-to-delete": toDelete, "error": err})
				return err
			}
			partialRoute = true
		}

		// Update the devices
		if partialRoute {
			respChnls = agent.deleteFlowsFromParentDevice(ctx, ofp.Flows{Items: toDelete}, &flowMetadata)
		} else {
			respChnls = agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &flowMetadata)
		}

		// Wait for the responses
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
				logger.Errorw("failure-updating-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
				// TODO: Revert the flow deletion
			}
		}()
	}
	//TODO: send announcement on delete
	return nil
}

func (agent *LogicalAgent) deleteFlowAndUpdateMeterStats(ctx context.Context, mod *ofp.OfpFlowMod, chunk *FlowChunk) error {
	chunk.lock.Lock()
	defer chunk.lock.Unlock()
	if changedMeter := agent.updateFlowCountOfMeterStats(ctx, mod, chunk.flow, false); !changedMeter {
		return fmt.Errorf("Cannot-delete-flow-%s. Meter-update-failed", chunk.flow)
	}
	// Update store and cache
	if err := agent.removeLogicalDeviceFlow(ctx, chunk.flow.Id); err != nil {
		return fmt.Errorf("Cannot-delete-flows-%s. Delete-from-store-failed", chunk.flow)
	}
	return nil
}

func (agent *LogicalAgent) addFlowsAndGroupsToDevices(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw("send-add-flows-to-device-manager", log.Fields{"logicalDeviceID": agent.logicalDeviceID, "deviceRules": deviceRules, "flowMetadata": flowMetadata})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			ctx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
			defer cancel()
			if err := agent.deviceMgr.addFlowsAndGroups(ctx, deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				logger.Errorw("flow-add-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-add-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	// Return responses (an array of channels) for the caller to wait for a response from the far end.
	return responses
}

func (agent *LogicalAgent) deleteFlowsAndGroupsFromDevices(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw("send-delete-flows-to-device-manager", log.Fields{"logicalDeviceID": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			ctx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
			defer cancel()
			if err := agent.deviceMgr.deleteFlowsAndGroups(ctx, deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				logger.Errorw("flow-delete-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-delete-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	return responses
}

func (agent *LogicalAgent) updateFlowsAndGroupsOfDevice(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw("send-update-flows-to-device-manager", log.Fields{"logicalDeviceID": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			ctx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
			defer cancel()
			if err := agent.deviceMgr.updateFlowsAndGroups(ctx, deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				logger.Errorw("flow-update-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-update-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	return responses
}

// getUNILogicalPortNo returns the UNI logical port number specified in the flow
func (agent *LogicalAgent) getUNILogicalPortNo(flow *ofp.OfpFlowStats) (uint32, error) {
	var uniPort uint32
	inPortNo := fu.GetInPort(flow)
	outPortNo := fu.GetOutPort(flow)
	if agent.isNNIPort(inPortNo) {
		uniPort = outPortNo
	} else if agent.isNNIPort(outPortNo) {
		uniPort = inPortNo
	}
	if uniPort != 0 {
		return uniPort, nil
	}
	return 0, status.Errorf(codes.NotFound, "no-uni-port: %v", flow)
}

func (agent *LogicalAgent) deleteFlowsFromParentDevice(ctx context.Context, flows ofp.Flows, metadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw("deleting-flows-from-parent-device", log.Fields{"logical-device-id": agent.logicalDeviceID, "flows": flows})
	responses := make([]coreutils.Response, 0)
	for _, flow := range flows.Items {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		uniPort, err := agent.getUNILogicalPortNo(flow)
		if err != nil {
			logger.Error("no-uni-port-in-flow", log.Fields{"deviceID": agent.rootDeviceID, "flow": flow, "error": err})
			response.Error(err)
			response.Done()
			continue
		}
		logger.Debugw("uni-port", log.Fields{"flows": flows, "uni-port": uniPort})
		go func(uniPort uint32, metadata *voltha.FlowMetadata) {
			ctx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
			defer cancel()
			if err := agent.deviceMgr.deleteParentFlows(ctx, agent.rootDeviceID, uniPort, metadata); err != nil {
				logger.Error("flow-delete-failed", log.Fields{"device-id": agent.rootDeviceID, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-delete-failed: %s %v", agent.rootDeviceID, err))
			}
			response.Done()
		}(uniPort, metadata)
	}
	return responses
}

//flowDeleteStrict deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowDeleteStrict(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debugw("flowDeleteStrict", log.Fields{"mod": mod})
	if mod == nil {
		return nil
	}

	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}
	logger.Debugw("flow-id-in-flow-delete-strict", log.Fields{"flowID": flow.Id})
	agent.flowLock.RLock()
	flowChunk, ok := agent.flows[flow.Id]
	agent.flowLock.RUnlock()
	if !ok {
		logger.Debugw("Skipping-flow-delete-strict-request. No-flow-found", log.Fields{"flowMod": mod})
		return nil
	}
	//Release the map lock and syncronize per flow
	flowChunk.lock.Lock()
	defer flowChunk.lock.Unlock()

	var meters []*ofp.OfpMeterEntry
	var flowGroups []*ofp.OfpGroupEntry
	if ofMeters, er := agent.ListLogicalDeviceMeters(ctx); er == nil {
		meters = ofMeters.Items
	}
	if ofGroups, er := agent.ListLogicalDeviceFlowGroups(ctx); er == nil {
		flowGroups = ofGroups.Items
	}
	if changedMeter := agent.updateFlowCountOfMeterStats(ctx, mod, flow, false); !changedMeter {
		return fmt.Errorf("Cannot delete flow - %s. Meter update failed", flow)
	}

	var flowMetadata voltha.FlowMetadata
	flowsToDelete := []*ofp.OfpFlowStats{flowChunk.flow}
	if err := agent.GetMeterConfig(flowsToDelete, meters, &flowMetadata); err != nil {
		logger.Error("meter-referred-in-flows-not-present")
		return err
	}
	var respChnls []coreutils.Response
	var partialRoute bool
	deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: flowsToDelete}, ofp.FlowGroups{Items: flowGroups})
	if err != nil {
		// A no route error means no route exists between the ports specified in the flow. This can happen when the
		// child device is deleted and a request to delete flows from the parent device is received
		if !errors.Is(err, route.ErrNoRoute) {
			logger.Errorw("unexpected-error-received", log.Fields{"flows-to-delete": flowsToDelete, "error": err})
			return err
		}
		partialRoute = true
	}

	// Update the model
	if err := agent.removeLogicalDeviceFlow(ctx, flow.Id); err != nil {
		return err
	}
	// Update the devices
	if partialRoute {
		respChnls = agent.deleteFlowsFromParentDevice(ctx, ofp.Flows{Items: flowsToDelete}, &flowMetadata)
	} else {
		respChnls = agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &flowMetadata)
	}

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-deleting-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()

	return nil
}

//flowModify modifies a flow from the flow table of that logical device
func (agent *LogicalAgent) flowModify(mod *ofp.OfpFlowMod) error {
	return errors.New("flowModify not implemented")
}

//flowModifyStrict deletes a flow from the flow table of that logical device
func (agent *LogicalAgent) flowModifyStrict(mod *ofp.OfpFlowMod) error {
	return errors.New("flowModifyStrict not implemented")
}

func (agent *LogicalAgent) groupAdd(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	if groupMod == nil {
		return nil
	}
	logger.Debugw("groupAdd", log.Fields{"GroupId": groupMod.GroupId})
	agent.groupLock.Lock()
	_, ok := agent.groups[groupMod.GroupId]
	if ok {
		agent.groupLock.Unlock()
		return fmt.Errorf("Group %d already exists", groupMod.GroupId)
	}

	groupEntry := fu.GroupEntryFromGroupMod(groupMod)
	groupChunk := GroupChunk{
		group: groupEntry,
	}
	//add to map
	agent.groups[groupMod.GroupId] = &groupChunk
	groupChunk.lock.Lock()
	defer groupChunk.lock.Unlock()
	agent.groupLock.Unlock()
	//add to the kv store
	path := fmt.Sprintf("groups/%s", agent.logicalDeviceID)
	groupID := strconv.Itoa(int(groupMod.GroupId))
	if err := agent.clusterDataProxy.AddWithID(ctx, path, groupID, groupEntry); err != nil {
		logger.Errorw("failed-adding-group", log.Fields{"deviceID": agent.logicalDeviceID, "groupID": groupID, "err": err})
		agent.groupLock.Lock()
		delete(agent.groups, groupMod.GroupId)
		agent.groupLock.Unlock()
		return err
	}
	deviceRules := fu.NewDeviceRules()
	deviceRules.CreateEntryIfNotExist(agent.rootDeviceID)
	fg := fu.NewFlowsAndGroups()
	fg.AddGroup(fu.GroupEntryFromGroupMod(groupMod))
	deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)

	logger.Debugw("rules", log.Fields{"rules for group-add": deviceRules.String()})

	// Update the devices
	respChnls := agent.addFlowsAndGroupsToDevices(ctx, deviceRules, &voltha.FlowMetadata{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}

func (agent *LogicalAgent) groupDelete(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupDelete")
	if groupMod == nil {
		return nil
	}
	affectedFlows := make([]*ofp.OfpFlowStats, 0)
	affectedGroups := make([]*ofp.OfpGroupEntry, 0)
	var groupsChanged bool
	groupID := groupMod.GroupId
	var err error
	if groupID == uint32(ofp.OfpGroup_OFPG_ALL) {
		if err := func() error {
			agent.groupLock.Lock()
			defer agent.groupLock.Unlock()
			for key, groupChunk := range agent.groups {
				//Remove from store and cache. Do this in a one time lock allocation.
				path := fmt.Sprintf("groups/%s/%d", agent.logicalDeviceID, key)
				if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
					return fmt.Errorf("couldnt-deleted-group-from-store-%s", path)
				}
				delete(agent.groups, groupID)
				var flows []*ofp.OfpFlowStats
				if flows, err = agent.deleteFlowsOfGroup(ctx, key); err != nil {
					logger.Errorw("cannot-update-flow-for-group-delete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "groupID": key})
					return err
				}
				affectedFlows = append(affectedFlows, flows...)
				affectedGroups = append(affectedGroups, groupChunk.group)
			}
			return nil
		}(); err != nil {
			return err
		}
		groupsChanged = true
	} else {
		agent.groupLock.RLock()
		groupChunk, ok := agent.groups[groupID]
		agent.groupLock.RUnlock()
		if !ok {
			logger.Warnw("group-not-found", log.Fields{"groupID": groupID})
			return nil
		}
		groupChunk.lock.Lock()
		defer groupChunk.lock.Unlock()
		var flows []*ofp.OfpFlowStats
		if flows, err = agent.deleteFlowsOfGroup(ctx, groupID); err != nil {
			logger.Errorw("cannot-update-flow-for-group-delete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "groupID": groupID})
			return err
		}
		//remove from store
		if err := agent.removeLogicalDeviceFlowGroup(ctx, groupID); err != nil {
			return err
		}
		affectedFlows = append(affectedFlows, flows...)
		affectedGroups = append(affectedGroups, groupChunk.group)
		groupsChanged = true

	}

	if err != nil || groupsChanged {
		var deviceRules *fu.DeviceRules
		deviceRules, err = agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: affectedFlows}, ofp.FlowGroups{Items: affectedGroups})
		if err != nil {
			return err
		}
		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		// Update the devices
		respChnls := agent.updateFlowsAndGroupsOfDevice(ctx, deviceRules, nil)

		// Wait for completion
		go func() {
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
				logger.Warnw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
				//TODO: Revert flow changes
			}
		}()
	}
	return nil
}

func (agent *LogicalAgent) deleteFlowsOfGroup(ctx context.Context, groupID uint32) ([]*ofp.OfpFlowStats, error) {
	logger.Infow("Delete-flows-matching-group", log.Fields{"groupID": groupID})
	var flowsRemoved []*ofp.OfpFlowStats
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()
	for flowID, flowChunk := range agent.flows {
		if fu.FlowHasOutGroup(flowChunk.flow, groupID) {
			path := fmt.Sprintf("logical_flows/%s/%d", agent.logicalDeviceID, flowID)
			if err := agent.clusterDataProxy.Remove(ctx, path); err != nil {
				return nil, fmt.Errorf("couldnt-delete-flow-from-store-%s", path)
			}
			delete(agent.flows, flowID)
			flowsRemoved = append(flowsRemoved, flowChunk.flow)
		}
	}
	return flowsRemoved, nil
}

func (agent *LogicalAgent) groupModify(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupModify")
	if groupMod == nil {
		return nil
	}

	groupID := groupMod.GroupId
	agent.groupLock.RLock()
	groupChunk, ok := agent.groups[groupID]
	agent.groupLock.RUnlock()
	if !ok {
		return fmt.Errorf("group-absent:%d", groupID)
	}
	//Don't let any other thread to make modifications to this group till all done here.
	groupChunk.lock.Lock()
	defer groupChunk.lock.Unlock()
	//replace existing group entry with new group definition
	groupEntry := fu.GroupEntryFromGroupMod(groupMod)
	deviceRules := fu.NewDeviceRules()
	deviceRules.CreateEntryIfNotExist(agent.rootDeviceID)
	fg := fu.NewFlowsAndGroups()
	fg.AddGroup(fu.GroupEntryFromGroupMod(groupMod))
	deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)

	logger.Debugw("rules", log.Fields{"rules-for-group-modify": deviceRules.String()})
	//update KV
	if err := agent.updateLogicalDeviceFlowGroup(ctx, groupEntry, groupChunk); err != nil {
		logger.Errorw("Cannot-update-logical-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return err
	}

	// Update the devices
	respChnls := agent.updateFlowsAndGroupsOfDevice(ctx, deviceRules, &voltha.FlowMetadata{})

	// Wait for completion
	go func() {
		if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
			logger.Warnw("failure-updating-device-flows-groups", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
			//TODO: Revert flow changes
		}
	}()
	return nil
}

// deleteLogicalPort removes the logical port
func (agent *LogicalAgent) deleteLogicalPort(ctx context.Context, lPort *voltha.LogicalPort) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logicalDevice := agent.getLogicalDeviceWithoutLock()

	index := -1
	for i, logicalPort := range logicalDevice.Ports {
		if logicalPort.Id == lPort.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		clonedPorts := clonePorts(logicalDevice.Ports)
		if index < len(clonedPorts)-1 {
			copy(clonedPorts[index:], clonedPorts[index+1:])
		}
		clonedPorts[len(clonedPorts)-1] = nil
		clonedPorts = clonedPorts[:len(clonedPorts)-1]
		logger.Debugw("logical-port-deleted", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		if err := agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, clonedPorts); err != nil {
			logger.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

		// Remove the logical port from cache
		agent.deleteLogicalPortsFromMap([]uint32{lPort.DevicePortNo})
		// Reset the logical device routes
		go func() {
			if err := agent.buildRoutes(context.Background()); err != nil {
				logger.Warnw("device-routes-not-ready", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "error": err})
			}
		}()
	}
	return nil
}

// deleteLogicalPorts removes the logical ports associated with that deviceId
func (agent *LogicalAgent) deleteLogicalPorts(ctx context.Context, deviceID string) error {
	logger.Debugw("deleting-logical-ports", log.Fields{"device-id": deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logicalDevice := agent.getLogicalDeviceWithoutLock()
	lPortstoKeep := []*voltha.LogicalPort{}
	lPortsNoToDelete := []uint32{}
	for _, logicalPort := range logicalDevice.Ports {
		if logicalPort.DeviceId != deviceID {
			lPortstoKeep = append(lPortstoKeep, logicalPort)
		} else {
			lPortsNoToDelete = append(lPortsNoToDelete, logicalPort.DevicePortNo)
		}
	}
	logger.Debugw("deleted-logical-ports", log.Fields{"ports": lPortstoKeep})
	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, lPortstoKeep); err != nil {
		logger.Errorw("logical-device-update-failed", log.Fields{"logical-device-id": agent.logicalDeviceID})
		return err
	}
	// Remove the port from the cached logical ports set
	agent.deleteLogicalPortsFromMap(lPortsNoToDelete)

	// Reset the logical device routes
	go func() {
		if err := agent.buildRoutes(context.Background()); err != nil {
			logger.Warnw("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
		}
	}()

	return nil
}

// enableLogicalPort enables the logical port
func (agent *LogicalAgent) enableLogicalPort(ctx context.Context, lPortID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	logicalDevice := agent.getLogicalDeviceWithoutLock()

	index := -1
	for i, logicalPort := range logicalDevice.Ports {
		if logicalPort.Id == lPortID {
			index = i
			break
		}
	}
	if index >= 0 {
		clonedPorts := clonePorts(logicalDevice.Ports)
		clonedPorts[index].OfpPort.Config = clonedPorts[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, clonedPorts)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

// disableLogicalPort disabled the logical port
func (agent *LogicalAgent) disableLogicalPort(ctx context.Context, lPortID string) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	// Get the most up to date logical device
	logicalDevice := agent.getLogicalDeviceWithoutLock()
	index := -1
	for i, logicalPort := range logicalDevice.Ports {
		if logicalPort.Id == lPortID {
			index = i
			break
		}
	}
	if index >= 0 {
		clonedPorts := clonePorts(logicalDevice.Ports)
		clonedPorts[index].OfpPort.Config = (clonedPorts[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)) | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDevicePortsWithoutLock(ctx, logicalDevice, clonedPorts)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

func (agent *LogicalAgent) getPreCalculatedRoute(ingress, egress uint32) ([]route.Hop, error) {
	logger.Debugw("ROUTE", log.Fields{"len": len(agent.deviceRoutes.Routes)})
	for routeLink, route := range agent.deviceRoutes.Routes {
		logger.Debugw("ROUTELINKS", log.Fields{"ingress": ingress, "egress": egress, "routelink": routeLink})
		if ingress == routeLink.Ingress && egress == routeLink.Egress {
			return route, nil
		}
	}
	return nil, status.Errorf(codes.FailedPrecondition, "no route from:%d to:%d", ingress, egress)
}

// GetRoute returns route
func (agent *LogicalAgent) GetRoute(ctx context.Context, ingressPortNo uint32, egressPortNo uint32) ([]route.Hop, error) {
	logger.Debugw("getting-route", log.Fields{"ingress-port": ingressPortNo, "egress-port": egressPortNo})
	routes := make([]route.Hop, 0)

	// Note: A port value of 0 is equivalent to a nil port

	//	Consider different possibilities
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		logger.Debugw("controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
		if agent.isNNIPort(ingressPortNo) {
			//This is a trap on the NNI Port
			if len(agent.deviceRoutes.Routes) == 0 {
				// If there are no routes set (usually when the logical device has only NNI port(s), then just return an
				// route with same IngressHop and EgressHop
				hop := route.Hop{DeviceID: agent.rootDeviceID, Ingress: ingressPortNo, Egress: ingressPortNo}
				routes = append(routes, hop)
				routes = append(routes, hop)
				return routes, nil
			}
			//Return a 'half' route to make the flow decomposer logic happy
			for routeLink, path := range agent.deviceRoutes.Routes {
				if agent.isNNIPort(routeLink.Egress) {
					routes = append(routes, route.Hop{}) // first hop is set to empty
					routes = append(routes, path[1])
					return routes, nil
				}
			}
			return nil, fmt.Errorf("no upstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
		}
		//treat it as if the output port is the first NNI of the OLT
		var err error
		if egressPortNo, err = agent.getFirstNNIPort(); err != nil {
			logger.Warnw("no-nni-port", log.Fields{"error": err})
			return nil, err
		}
	}
	//If ingress port is not specified (nil), it may be a wildcarded
	//route if egress port is OFPP_CONTROLLER or a nni logical port,
	//in which case we need to create a half-route where only the egress
	//hop is filled, the first hop is nil
	if ingressPortNo == 0 && agent.isNNIPort(egressPortNo) {
		// We can use the 2nd hop of any upstream route, so just find the first upstream:
		for routeLink, path := range agent.deviceRoutes.Routes {
			if agent.isNNIPort(routeLink.Egress) {
				routes = append(routes, route.Hop{}) // first hop is set to empty
				routes = append(routes, path[1])
				return routes, nil
			}
		}
		return nil, fmt.Errorf("no upstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
	}
	//If egress port is not specified (nil), we can also can return a "half" route
	if egressPortNo == 0 {
		for routeLink, path := range agent.deviceRoutes.Routes {
			if routeLink.Ingress == ingressPortNo {
				routes = append(routes, path[0])
				routes = append(routes, route.Hop{})
				return routes, nil
			}
		}
		return nil, fmt.Errorf("no downstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
	}
	//	Return the pre-calculated route
	return agent.getPreCalculatedRoute(ingressPortNo, egressPortNo)
}

//GetWildcardInputPorts filters out the logical port number from the set of logical ports on the device and
//returns their port numbers.  This function is invoked only during flow decomposition where the lock on the logical
//device is already held.  Therefore it is safe to retrieve the logical device without lock.
func (agent *LogicalAgent) GetWildcardInputPorts(excludePort ...uint32) []uint32 {
	lPorts := make([]uint32, 0)
	var exclPort uint32
	if len(excludePort) == 1 {
		exclPort = excludePort[0]
	}
	lDevice := agent.getLogicalDeviceWithoutLock()
	for _, port := range lDevice.Ports {
		if port.OfpPort.PortNo != exclPort {
			lPorts = append(lPorts, port.OfpPort.PortNo)
		}
	}
	return lPorts
}

// GetDeviceRoutes returns device graph
func (agent *LogicalAgent) GetDeviceRoutes() *route.DeviceRoutes {
	return agent.deviceRoutes
}

//rebuildRoutes rebuilds the device routes
func (agent *LogicalAgent) buildRoutes(ctx context.Context) error {
	logger.Debugf("building-routes", log.Fields{"logical-device-id": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.deviceRoutes == nil {
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.deviceMgr.getDevice)
	}
	// Get all the logical ports on that logical device
	lDevice := agent.getLogicalDeviceWithoutLock()

	if err := agent.deviceRoutes.ComputeRoutes(ctx, lDevice.Ports); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(); err != nil {
		return err
	}

	return nil
}

//updateRoutes updates the device routes
func (agent *LogicalAgent) updateRoutes(ctx context.Context, lp *voltha.LogicalPort) error {
	logger.Debugw("updateRoutes", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.deviceRoutes == nil {
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.deviceMgr.getDevice)
	}
	if err := agent.deviceRoutes.AddPort(ctx, lp, agent.logicalDevice.Ports); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(); err != nil {
		return err
	}
	return nil
}

// diff go over two lists of logical ports and return what's new, what's changed and what's removed.
func diff(oldList, newList []*voltha.LogicalPort) (newPorts, changedPorts, deletedPorts map[string]*voltha.LogicalPort) {
	newPorts = make(map[string]*voltha.LogicalPort, len(newList))
	changedPorts = make(map[string]*voltha.LogicalPort, len(oldList))
	deletedPorts = make(map[string]*voltha.LogicalPort, len(oldList))

	for _, n := range newList {
		newPorts[n.Id] = n
	}

	for _, o := range oldList {
		if n, have := newPorts[o.Id]; have {
			delete(newPorts, o.Id) // not new
			if !proto.Equal(n, o) {
				changedPorts[n.Id] = n // changed
			}
		} else {
			deletedPorts[o.Id] = o // deleted
		}
	}

	return newPorts, changedPorts, deletedPorts
}

// portUpdated is invoked when a port is updated on the logical device
func (agent *LogicalAgent) portUpdated(prevPorts, currPorts []*voltha.LogicalPort) interface{} {
	// Get the difference between the two list
	newPorts, changedPorts, deletedPorts := diff(prevPorts, currPorts)

	// Send the port change events to the OF controller
	for _, newP := range newPorts {
		go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_ADD, Desc: newP.OfpPort})
	}
	for _, change := range changedPorts {
		go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_MODIFY, Desc: change.OfpPort})
	}
	for _, del := range deletedPorts {
		go agent.ldeviceMgr.SendChangeEvent(agent.logicalDeviceID,
			&ofp.OfpPortStatus{Reason: ofp.OfpPortReason_OFPPR_DELETE, Desc: del.OfpPort})
	}

	return nil
}

// addNNILogicalPort adds an NNI port to the logical device.  It returns a bool representing whether a port has been
// added and an eror in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addNNILogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) (bool, error) {
	logger.Debugw("addNNILogicalPort", log.Fields{"NNI": port})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}
	if agent.portExist(device, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		agent.requestQueue.RequestComplete()
		return false, nil
	}
	agent.requestQueue.RequestComplete()

	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, device.Id, port.PortNo); err != nil {
		logger.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}

	defer agent.requestQueue.RequestComplete()
	// Double check again if this port has been already added since the getPortCapability could have taken a long time
	if agent.portExist(device, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}

	portCap.Port.RootPort = true
	lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
	lp.DeviceId = device.Id
	lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
	lp.OfpPort.PortNo = port.PortNo
	lp.OfpPort.Name = lp.Id
	lp.DevicePortNo = port.PortNo

	ld := agent.getLogicalDeviceWithoutLock()

	clonedPorts := clonePorts(ld.Ports)
	if clonedPorts == nil {
		clonedPorts = make([]*voltha.LogicalPort, 0)
	}
	clonedPorts = append(clonedPorts, lp)

	if err = agent.updateLogicalDevicePortsWithoutLock(ctx, ld, clonedPorts); err != nil {
		logger.Errorw("error-updating-logical-device", log.Fields{"error": err})
		return false, err
	}

	// Update the device routes with this new logical port
	clonedLP := (proto.Clone(lp)).(*voltha.LogicalPort)
	go func() {
		if err := agent.updateRoutes(context.Background(), clonedLP); err != nil {
			logger.Warnw("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "logical-port": lp.OfpPort.PortNo, "error": err})
		}
	}()

	return true, nil
}

func (agent *LogicalAgent) portExist(device *voltha.Device, port *voltha.Port) bool {
	ldevice := agent.getLogicalDeviceWithoutLock()
	for _, lPort := range ldevice.Ports {
		if lPort.DeviceId == device.Id && lPort.DevicePortNo == port.PortNo && lPort.Id == port.Label {
			return true
		}
	}
	return false
}

// addUNILogicalPort adds an UNI port to the logical device.  It returns a bool representing whether a port has been
// added and an eror in case a valid error is encountered. If the port was successfully added it will return
// (true, nil).   If the device is not in the correct state it will return (false, nil) as this is a valid
// scenario. This also applies to the case where the port was already added.
func (agent *LogicalAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device, port *voltha.Port) (bool, error) {
	logger.Debugw("addUNILogicalPort", log.Fields{"port": port})
	if childDevice.AdminState != voltha.AdminState_ENABLED || childDevice.OperStatus != voltha.OperStatus_ACTIVE {
		logger.Infow("device-not-ready", log.Fields{"deviceId": childDevice.Id, "admin": childDevice.AdminState, "oper": childDevice.OperStatus})
		return false, nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}

	if agent.portExist(childDevice, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		agent.requestQueue.RequestComplete()
		return false, nil
	}
	agent.requestQueue.RequestComplete()
	var portCap *ic.PortCapability
	var err error
	// First get the port capability
	if portCap, err = agent.deviceMgr.getPortCapability(ctx, childDevice.Id, port.PortNo); err != nil {
		logger.Errorw("error-retrieving-port-capabilities", log.Fields{"error": err})
		return false, err
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return false, err
	}
	defer agent.requestQueue.RequestComplete()
	// Double check again if this port has been already added since the getPortCapability could have taken a long time
	if agent.portExist(childDevice, port) {
		logger.Debugw("port-already-exist", log.Fields{"port": port})
		return false, nil
	}
	// Get stored logical device
	ldevice := agent.getLogicalDeviceWithoutLock()

	logger.Debugw("adding-uni", log.Fields{"deviceId": childDevice.Id})
	portCap.Port.RootPort = false
	portCap.Port.Id = port.Label
	portCap.Port.OfpPort.PortNo = port.PortNo
	portCap.Port.DeviceId = childDevice.Id
	portCap.Port.DevicePortNo = port.PortNo
	clonedPorts := clonePorts(ldevice.Ports)
	if clonedPorts == nil {
		clonedPorts = make([]*voltha.LogicalPort, 0)
	}
	clonedPorts = append(clonedPorts, portCap.Port)
	if err := agent.updateLogicalDevicePortsWithoutLock(ctx, ldevice, clonedPorts); err != nil {
		return false, err
	}
	// Update the device graph with this new logical port
	clonedLP := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)

	go func() {
		if err := agent.updateRoutes(context.Background(), clonedLP); err != nil {
			logger.Warn("routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
		}
	}()

	return true, nil
}

func (agent *LogicalAgent) packetOut(ctx context.Context, packet *ofp.OfpPacketOut) {
	logger.Debugw("packet-out", log.Fields{
		"packet": hex.EncodeToString(packet.Data),
		"inPort": packet.GetInPort(),
	})
	outPort := fu.GetPacketOutPort(packet)
	//frame := packet.GetData()
	//TODO: Use a channel between the logical agent and the device agent
	if err := agent.deviceMgr.packetOut(ctx, agent.rootDeviceID, outPort, packet); err != nil {
		logger.Error("packetout-failed", log.Fields{"logicalDeviceID": agent.rootDeviceID})
	}
}

func (agent *LogicalAgent) packetIn(port uint32, transactionID string, packet []byte) {
	logger.Debugw("packet-in", log.Fields{
		"port":          port,
		"packet":        hex.EncodeToString(packet),
		"transactionId": transactionID,
	})
	packetIn := fu.MkPacketIn(port, packet)
	agent.ldeviceMgr.SendPacketIn(agent.logicalDeviceID, transactionID, packetIn)
	logger.Debugw("sending-packet-in", log.Fields{"packet": hex.EncodeToString(packetIn.Data)})
}

func (agent *LogicalAgent) addLogicalPortToMap(portNo uint32, nniPort bool) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	if exist := agent.logicalPortsNo[portNo]; !exist {
		agent.logicalPortsNo[portNo] = nniPort
	}
}

func (agent *LogicalAgent) deleteLogicalPortsFromMap(portsNo []uint32) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, pNo := range portsNo {
		delete(agent.logicalPortsNo, pNo)
	}
}

func (agent *LogicalAgent) addLogicalPortsToMap(lps []*voltha.LogicalPort) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, lp := range lps {
		if exist := agent.logicalPortsNo[lp.DevicePortNo]; !exist {
			agent.logicalPortsNo[lp.DevicePortNo] = lp.RootPort
		}
	}
}

func (agent *LogicalAgent) loadFlows(ctx context.Context) {
	agent.flowLock.Lock()
	defer agent.flowLock.Unlock()

	var flowList []*ofp.OfpFlowStats
	if err := agent.clusterDataProxy.List(ctx, "logical_flows/"+agent.logicalDeviceID, &flowList); err != nil {
		logger.Errorw("Failed-to-list-logicalflows-from-cluster-data-proxy", log.Fields{"error": err})
		return
	}
	for _, flow := range flowList {
		if flow != nil {
			flowsChunk := FlowChunk{
				flow: flow,
			}
			agent.flows[flow.Id] = &flowsChunk
		}
	}
}

func (agent *LogicalAgent) loadMeters(ctx context.Context) {
	agent.meterLock.Lock()
	defer agent.meterLock.Unlock()

	var meters []*ofp.OfpMeterEntry
	if err := agent.clusterDataProxy.List(ctx, "meters/"+agent.logicalDeviceID, &meters); err != nil {
		logger.Errorw("Failed-to-list-meters-from-proxy", log.Fields{"error": err})
		return
	}
	for _, meter := range meters {
		if meter.Config != nil {
			meterChunk := MeterChunk{
				meter: meter,
			}
			agent.meters[meter.Config.MeterId] = &meterChunk
		}
	}
}

func (agent *LogicalAgent) loadGroups(ctx context.Context) {
	agent.groupLock.Lock()
	defer agent.groupLock.Unlock()

	var groups []*ofp.OfpGroupEntry
	if err := agent.clusterDataProxy.List(ctx, "groups/"+agent.logicalDeviceID, &groups); err != nil {
		logger.Errorw("Failed-to-list-groups-from-proxy", log.Fields{"error": err})
		return
	}
	for _, group := range groups {
		if group.Desc != nil {
			groupChunk := GroupChunk{
				group: group,
			}
			agent.groups[group.Desc.GroupId] = &groupChunk
		}
	}
	logger.Infow("Groups-are-loaded-into-the-cache-from-store", log.Fields{"logicalDeviceID": agent.logicalDeviceID})
}

func (agent *LogicalAgent) isNNIPort(portNo uint32) bool {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	if exist := agent.logicalPortsNo[portNo]; exist {
		return agent.logicalPortsNo[portNo]
	}
	return false
}

func (agent *LogicalAgent) getFirstNNIPort() (uint32, error) {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			return portNo, nil
		}
	}
	return 0, status.Error(codes.NotFound, "No NNI port found")
}

//GetNNIPorts returns NNI ports.
func (agent *LogicalAgent) GetNNIPorts() []uint32 {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	nniPorts := make([]uint32, 0)
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			nniPorts = append(nniPorts, portNo)
		}
	}
	return nniPorts
}
