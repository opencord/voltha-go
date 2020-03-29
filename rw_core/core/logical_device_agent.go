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
	"reflect"
	"sync"
	"time"
)

const (
	maxOrderedLogicalDeviceRequestQueueSize = 1000
)

// LogicalDeviceAgent represent attributes of logical device agent
type LogicalDeviceAgent struct {
	logicalDeviceID    string
	rootDeviceID       string
	deviceMgr          *DeviceManager
	ldeviceMgr         *LogicalDeviceManager
	clusterDataProxy   *model.Proxy
	exitChannel        chan int
	deviceRoutes       *route.DeviceRoutes
	flowProxy          *model.Proxy
	groupProxy         *model.Proxy
	meterProxy         *model.Proxy
	ldProxy            *model.Proxy
	portProxies        map[string]*model.Proxy
	lockDeviceRoutes   sync.RWMutex
	logicalPortsNo     map[uint32]bool //value is true for NNI port
	lockLogicalPortsNo sync.RWMutex
	flowDecomposer     *fd.FlowDecomposer
	defaultTimeout     time.Duration
	logicalDevice      *voltha.LogicalDevice
	requestQueue       *coreutils.RequestQueue
	startOnce          sync.Once
	stopOnce           sync.Once
}

func newLogicalDeviceAgent(id string, deviceID string, ldeviceMgr *LogicalDeviceManager,
	deviceMgr *DeviceManager,
	cdProxy *model.Proxy, timeout time.Duration) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceID = id
	agent.rootDeviceID = deviceID
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.flowDecomposer = fd.NewFlowDecomposer(agent.deviceMgr)
	agent.portProxies = make(map[string]*model.Proxy)
	agent.logicalPortsNo = make(map[uint32]bool)
	agent.defaultTimeout = timeout
	agent.requestQueue = coreutils.NewRequestQueue(agent.logicalDeviceID, maxOrderedLogicalDeviceRequestQueueSize)
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context, loadFromDB bool) error {
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

	// Launch the request queue - it will launch a go routine
	agent.requestQueue.Start()

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
		if datapathID, err = CreateDataPathID(agent.logicalDeviceID); err != nil {
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
		added, err := agent.clusterDataProxy.AddWithID(ctx, "/logical_devices", ld.Id, ld, "")
		if err != nil {
			return err
		}
		if added == nil {
			logger.Errorw("failed-to-add-logical-device", log.Fields{"logical-device-id": agent.logicalDeviceID})
		} else {
			logger.Debugw("logicaldevice-created", log.Fields{"logical-device-id": agent.logicalDeviceID, "root-id": ld.RootDeviceId})
		}

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
		logicalDevice, err := agent.clusterDataProxy.Get(ctx, "/logical_devices/"+agent.logicalDeviceID, 0, true, "")
		if err != nil {
			return err
		}
		ld, ok := logicalDevice.(*voltha.LogicalDevice)
		if !ok {
			return status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
		}
		// Update the root device Id
		agent.rootDeviceID = ld.RootDeviceId

		// Update the last data
		agent.logicalDevice = proto.Clone(ld).(*voltha.LogicalDevice)

		// Setup the local list of logical ports
		agent.addLogicalPortsToMap(ld.Ports)
	}

	var err error
	agent.flowProxy, err = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s/flows", agent.logicalDeviceID),
		false)
	if err != nil {
		return err
	}
	agent.meterProxy, err = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s/meters", agent.logicalDeviceID),
		false)
	if err != nil {
		logger.Errorw("failed-to-create-meter-proxy", log.Fields{"error": err})
		return err
	}
	agent.groupProxy, err = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s/flow_groups", agent.logicalDeviceID),
		false)
	if err != nil {
		logger.Errorw("failed-to-create-group-proxy", log.Fields{"error": err})
		return err
	}
	agent.ldProxy, err = agent.clusterDataProxy.CreateProxy(
		ctx,
		fmt.Sprintf("/logical_devices/%s", agent.logicalDeviceID),
		false)
	if err != nil {
		logger.Errorw("failed-to-create-logical-device-proxy", log.Fields{"error": err})
		return err
	}
	// TODO:  Use a port proxy once the POST_ADD is fixed
	if agent.ldProxy != nil {
		agent.ldProxy.RegisterCallback(model.PostUpdate, agent.portUpdated)
	} else {
		return status.Error(codes.Internal, "logical-device-proxy-null")
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
func (agent *LogicalDeviceAgent) stop(ctx context.Context) error {
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
		if removed, err := agent.clusterDataProxy.Remove(ctx, "/logical_devices/"+agent.logicalDeviceID, ""); err != nil {
			returnErr = err
		} else if removed == nil {
			returnErr = status.Errorf(codes.Aborted, "failed-to-remove-logical-ldevice-%s", agent.logicalDeviceID)
		} else {
			logger.Debugw("logicaldevice-removed", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		}

		// Stop the request queue and request complete indication
		agent.requestQueue.Stop()

		close(agent.exitChannel)

		logger.Info("logical_device-agent-stopped")
	})
	return returnErr
}

// GetLogicalDevice returns the latest logical device data
func (agent *LogicalDeviceAgent) GetLogicalDevice(ctx context.Context) (*voltha.LogicalDevice, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	return proto.Clone(agent.logicalDevice).(*voltha.LogicalDevice), nil
}

// ListLogicalDeviceFlows returns logical device flows
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlows(ctx context.Context) (*ofp.Flows, error) {
	logger.Debug("ListLogicalDeviceFlows")

	logicalDevice, err := agent.GetLogicalDevice(ctx)
	if err != nil {
		return nil, err
	}
	if logicalDevice.Flows == nil {
		return &ofp.Flows{}, nil
	}
	return (proto.Clone(logicalDevice.Flows)).(*ofp.Flows), nil
}

// ListLogicalDeviceMeters returns logical device meters
func (agent *LogicalDeviceAgent) ListLogicalDeviceMeters(ctx context.Context) (*ofp.Meters, error) {
	logger.Debug("ListLogicalDeviceMeters")

	logicalDevice, err := agent.GetLogicalDevice(ctx)
	if err != nil {
		return nil, err
	}
	if logicalDevice.Meters == nil {
		return &ofp.Meters{}, nil
	}
	return (proto.Clone(logicalDevice.Meters)).(*ofp.Meters), nil
}

// ListLogicalDeviceFlowGroups returns logical device flow groups
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlowGroups(ctx context.Context) (*ofp.FlowGroups, error) {
	logger.Debug("ListLogicalDeviceFlowGroups")

	logicalDevice, err := agent.GetLogicalDevice(ctx)
	if err != nil {
		return nil, err
	}
	if logicalDevice.FlowGroups == nil {
		return &ofp.FlowGroups{}, nil
	}
	return (proto.Clone(logicalDevice.FlowGroups)).(*ofp.FlowGroups), nil
}

// ListLogicalDevicePorts returns logical device ports
func (agent *LogicalDeviceAgent) ListLogicalDevicePorts(ctx context.Context) (*voltha.LogicalPorts, error) {
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

//updateLogicalDeviceFlowsWithoutLock updates the logical device with the latest flows in the model.
func (agent *LogicalDeviceAgent) updateLogicalDeviceFlowsWithoutLock(ctx context.Context, flows *ofp.Flows) error {
	ld := agent.getLogicalDeviceWithoutLock()

	logger.Debugw("logical-device-before", log.Fields{"lports": len(ld.Ports)})
	ld.Flows = flows

	if err := agent.updateLogicalDeviceWithoutLock(ctx, ld); err != nil {
		logger.Errorw("error-updating-logical-device-with-flows", log.Fields{"error": err})
		return err
	}
	return nil
}

//updateLogicalDeviceMetersWithoutLock updates the logical device with the meters info
func (agent *LogicalDeviceAgent) updateLogicalDeviceMetersWithoutLock(ctx context.Context, meters *ofp.Meters) error {
	ld := agent.getLogicalDeviceWithoutLock()

	logger.Debugw("logical-device-before", log.Fields{"lports": len(ld.Ports)})
	ld.Meters = meters

	if err := agent.updateLogicalDeviceWithoutLock(ctx, ld); err != nil {
		logger.Errorw("error-updating-logical-device-with-meters", log.Fields{"error": err})
		return err
	}
	return nil
}

//updateLogicalDeviceFlowGroupsWithoutLock updates the logical device with the flow groups
func (agent *LogicalDeviceAgent) updateLogicalDeviceFlowGroupsWithoutLock(ctx context.Context, flowGroups *ofp.FlowGroups) error {
	ld := agent.getLogicalDeviceWithoutLock()

	logger.Debugw("logical-device-before", log.Fields{"lports": len(ld.Ports)})
	ld.FlowGroups = flowGroups

	if err := agent.updateLogicalDeviceWithoutLock(ctx, ld); err != nil {
		logger.Errorw("error-updating-logical-device-with-flowgroups", log.Fields{"error": err})
		return err
	}
	return nil
}

// getLogicalDeviceWithoutLock returns a cloned logical device to a function that already holds the agent lock.
func (agent *LogicalDeviceAgent) getLogicalDeviceWithoutLock() *voltha.LogicalDevice {
	logger.Debug("getLogicalDeviceWithoutLock")
	return proto.Clone(agent.logicalDevice).(*voltha.LogicalDevice)
}

func (agent *LogicalDeviceAgent) updateLogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) error {
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
func (agent *LogicalDeviceAgent) setupLogicalPorts(ctx context.Context) error {
	logger.Infow("setupLogicalPorts", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// First add any NNI ports which could have been missing
	if err := agent.setupNNILogicalPorts(ctx, agent.rootDeviceID); err != nil {
		logger.Errorw("error-setting-up-NNI-ports", log.Fields{"error": err, "deviceId": agent.rootDeviceID})
		return err
	}

	// Now, set up the UNI ports if needed.
	children, err := agent.deviceMgr.getAllChildDevices(ctx, agent.rootDeviceID)
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
func (agent *LogicalDeviceAgent) setupNNILogicalPorts(ctx context.Context, deviceID string) error {
	logger.Infow("setupNNILogicalPorts-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	// Build the logical device based on information retrieved from the device adapter
	var err error

	var device *voltha.Device
	if device, err = agent.deviceMgr.GetDevice(ctx, deviceID); err != nil {
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
func (agent *LogicalDeviceAgent) updatePortState(ctx context.Context, deviceID string, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Infow("updatePortState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "portNo": portNo, "state": operStatus})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	cloned := agent.getLogicalDeviceWithoutLock()
	for idx, lPort := range cloned.Ports {
		if lPort.DeviceId == deviceID && lPort.DevicePortNo == portNo {
			if operStatus == voltha.OperStatus_ACTIVE {
				cloned.Ports[idx].OfpPort.Config = cloned.Ports[idx].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				cloned.Ports[idx].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				cloned.Ports[idx].OfpPort.Config = cloned.Ports[idx].OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				cloned.Ports[idx].OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}
			// Update the logical device
			if err := agent.updateLogicalDeviceWithoutLock(ctx, cloned); err != nil {
				logger.Errorw("error-updating-logical-device", log.Fields{"error": err})
				return err
			}
			return nil
		}
	}
	return status.Errorf(codes.NotFound, "port-%d-not-exist", portNo)
}

// updatePortsState updates the ports state related to the device
func (agent *LogicalDeviceAgent) updatePortsState(ctx context.Context, device *voltha.Device, state voltha.OperStatus_Types) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	cloned := agent.getLogicalDeviceWithoutLock()
	for _, lport := range cloned.Ports {
		if lport.DeviceId == device.Id {
			if state == voltha.OperStatus_ACTIVE {
				lport.OfpPort.Config = lport.OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				lport.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LIVE)
			} else {
				lport.OfpPort.Config = lport.OfpPort.Config | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
				lport.OfpPort.State = uint32(ofp.OfpPortState_OFPPS_LINK_DOWN)
			}

		}
	}
	// Updating the logical device will trigger the poprt change events to be populated to the controller
	if err := agent.updateLogicalDeviceWithoutLock(ctx, cloned); err != nil {
		logger.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
}

// setupUNILogicalPorts creates a UNI port on the logical device that represents a child UNI interface
func (agent *LogicalDeviceAgent) setupUNILogicalPorts(ctx context.Context, childDevice *voltha.Device) error {
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
func (agent *LogicalDeviceAgent) deleteAllLogicalPorts(ctx context.Context) error {
	logger.Infow("updatePortsState-start", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	ld := agent.getLogicalDeviceWithoutLock()

	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	var updateLogicalPorts []*voltha.LogicalPort
	// Update an empty ports slice to remove all the ports
	cloned.Ports = updateLogicalPorts

	if err := agent.updateLogicalDeviceWithoutLock(ctx, cloned); err != nil {
		logger.Warnw("logical-device-update-failed", log.Fields{"ldeviceId": agent.logicalDeviceID, "error": err})
		return err
	}
	return nil
}

// deleteAllUNILogicalPorts deletes all UNI logical ports associated with this parent device
func (agent *LogicalDeviceAgent) deleteAllUNILogicalPorts(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw("delete-all-uni-logical-ports", log.Fields{"logical-device-id": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Get the latest logical device info
	ld := agent.getLogicalDeviceWithoutLock()

	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	updateLogicalPorts := []*voltha.LogicalPort{}
	for _, lport := range cloned.Ports {
		// Save NNI ports only
		if agent.isNNIPort(lport.DevicePortNo) {
			updateLogicalPorts = append(updateLogicalPorts, lport)
		}
	}
	if len(updateLogicalPorts) < len(cloned.Ports) {
		cloned.Ports = updateLogicalPorts
		// Updating the logical device will trigger the port change events to be populated to the controller
		if err := agent.updateLogicalDeviceWithoutLock(ctx, cloned); err != nil {
			return err
		}
	} else {
		logger.Debugw("no-change-required", log.Fields{"logical-device-id": agent.logicalDeviceID})
	}
	return nil
}

//updateLogicalDeviceWithoutLock updates the model with the logical device.  It clones the logicaldevice before saving it
func (agent *LogicalDeviceAgent) updateLogicalDeviceWithoutLock(ctx context.Context, logicalDevice *voltha.LogicalDevice) error {
	updateCtx := context.WithValue(ctx, model.RequestTimestamp, time.Now().UnixNano())
	afterUpdate, err := agent.clusterDataProxy.Update(updateCtx, "/logical_devices/"+agent.logicalDeviceID, logicalDevice, false, "")
	if err != nil {
		logger.Errorw("failed-to-update-logical-devices-to-cluster-proxy", log.Fields{"error": err})
		return err
	}
	if afterUpdate == nil {
		return status.Errorf(codes.Internal, "failed-updating-logical-device:%s", agent.logicalDeviceID)
	}
	//agent.logicalDevice = (proto.Clone(logicalDevice)).(*voltha.LogicalDevice)
	agent.logicalDevice = logicalDevice

	return nil
}

//generateDeviceRoutesIfNeeded generates the device routes if the logical device has been updated since the last time
//that device graph was generated.
func (agent *LogicalDeviceAgent) generateDeviceRoutesIfNeeded(ctx context.Context) error {
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
		return err
	}
	return nil
}

//updateFlowTable updates the flow table of that logical device
func (agent *LogicalDeviceAgent) updateFlowTable(ctx context.Context, flow *ofp.OfpFlowMod) error {
	logger.Debug("updateFlowTable")
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
func (agent *LogicalDeviceAgent) updateGroupTable(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
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
func (agent *LogicalDeviceAgent) updateMeterTable(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
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

func (agent *LogicalDeviceAgent) meterAdd(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debugw("meterAdd", log.Fields{"metermod": *meterMod})
	if meterMod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debug("Acquired logical device lock")
	lDevice := agent.getLogicalDeviceWithoutLock()

	var meters []*ofp.OfpMeterEntry
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	logger.Debugw("Available meters", log.Fields{"meters": meters})

	for _, meter := range meters {
		if meterMod.MeterId == meter.Config.MeterId {
			logger.Infow("Meter-already-exists", log.Fields{"meter": *meterMod})
			return nil
		}
	}

	meterEntry := fu.MeterEntryFromMeterMod(meterMod)
	meters = append(meters, meterEntry)
	//Update model
	if err := agent.updateLogicalDeviceMetersWithoutLock(ctx, &ofp.Meters{Items: meters}); err != nil {
		logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		return err
	}
	logger.Debugw("Meter-added-successfully", log.Fields{"Added-meter": meterEntry, "updated-meters": lDevice.Meters})
	return nil
}

func (agent *LogicalDeviceAgent) meterDelete(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("meterDelete", log.Fields{"meterMod": *meterMod})
	if meterMod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()

	var meters []*ofp.OfpMeterEntry
	var flows []*ofp.OfpFlowStats
	updatedFlows := make([]*ofp.OfpFlowStats, 0)
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}

	changedMeter := false
	changedFow := false
	logger.Debugw("Available meters", log.Fields{"meters": meters})
	for index, meter := range meters {
		if meterMod.MeterId == meter.Config.MeterId {
			flows = lDevice.Flows.Items
			changedFow, updatedFlows = agent.getUpdatedFlowsAfterDeletebyMeterID(flows, meterMod.MeterId)
			meters = append(meters[:index], meters[index+1:]...)
			logger.Debugw("Meter has been deleted", log.Fields{"meter": meter, "index": index})
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
		if err := agent.updateLogicalDeviceMetersWithoutLock(ctx, metersToUpdate); err != nil {
			logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		logger.Debug("Meter-deleted-from-DB-successfully", log.Fields{"updatedMeters": metersToUpdate, "no-of-meter": len(metersToUpdate.Items)})

	}
	if changedFow {
		//Update model
		if err := agent.updateLogicalDeviceFlowsWithoutLock(ctx, &ofp.Flows{Items: updatedFlows}); err != nil {
			logger.Errorw("db-flow-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		logger.Debug("Flows-associated-with-meter-deleted-from-DB-successfully",
			log.Fields{"updated-no-of-flows": len(updatedFlows), "meter": meterMod.MeterId})
	}
	logger.Debugw("meterDelete success", log.Fields{"meterID": meterMod.MeterId})
	return nil
}

func (agent *LogicalDeviceAgent) meterModify(ctx context.Context, meterMod *ofp.OfpMeterMod) error {
	logger.Debug("meterModify")
	if meterMod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()

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
			logger.Debugw("Found meter, replaced with new meter", log.Fields{"old meter": meter, "new meter": newmeterEntry})
			break
		}
	}
	if changedMeter {
		//Update model
		metersToUpdate := &ofp.Meters{}
		if lDevice.Meters != nil {
			metersToUpdate = &ofp.Meters{Items: meters}
		}
		if err := agent.updateLogicalDeviceMetersWithoutLock(ctx, metersToUpdate); err != nil {
			logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}
		logger.Debugw("meter-updated-in-DB-successfully", log.Fields{"updated_meters": meters})
		return nil
	}

	logger.Errorw("Meter not found ", log.Fields{"meter": meterMod})
	return fmt.Errorf("no-logical-device-present:%d", meterMod.MeterId)

}

func (agent *LogicalDeviceAgent) getUpdatedFlowsAfterDeletebyMeterID(flows []*ofp.OfpFlowStats, meterID uint32) (bool, []*ofp.OfpFlowStats) {
	logger.Infow("Delete flows matching meter", log.Fields{"meter": meterID})
	changed := false
	//updatedFlows := make([]*ofp.OfpFlowStats, 0)
	for index := len(flows) - 1; index >= 0; index-- {
		if mID := fu.GetMeterIdFromFlow(flows[index]); mID != 0 && mID == meterID {
			logger.Debugw("Flow to be deleted", log.Fields{"flow": flows[index], "index": index})
			flows = append(flows[:index], flows[index+1:]...)
			changed = true
		}
	}
	return changed, flows
}

func (agent *LogicalDeviceAgent) updateFlowCountOfMeterStats(modCommand *ofp.OfpFlowMod, meters []*ofp.OfpMeterEntry, flow *ofp.OfpFlowStats) bool {

	flowCommand := modCommand.GetCommand()
	meterID := fu.GetMeterIdFromFlow(flow)
	logger.Debugw("Meter-id-in-flow-mod", log.Fields{"meterId": meterID})
	if meterID == 0 {
		logger.Debugw("No meter present in the flow", log.Fields{"flow": *flow})
		return false
	}
	if meters == nil {
		logger.Debug("No meters present in logical device")
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
			logger.Debugw("Found meter, updated meter flow stats", log.Fields{" meterId": meterID})
			break
		}
	}
	return changedMeter
}

//flowAdd adds a flow to the flow table of that logical device
func (agent *LogicalDeviceAgent) flowAdd(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debugw("flowAdd", log.Fields{"flow": mod})
	if mod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()

	var flows []*ofp.OfpFlowStats
	var meters []*ofp.OfpMeterEntry
	var flow *ofp.OfpFlowStats
	var err error

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
			logger.Warnw("overlapped-flows", log.Fields{"logicaldeviceId": agent.logicalDeviceID})
		} else {
			//	Add flow
			flow, err = fu.FlowStatsEntryFromFlowModMessage(mod)
			if err != nil {
				return err
			}
			flows = append(flows, flow)
			updatedFlows = append(updatedFlows, flow)
			changed = true
		}
	} else {
		flow, err = fu.FlowStatsEntryFromFlowModMessage(mod)
		if err != nil {
			return err
		}
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
	logger.Debugw("flowAdd-changed", log.Fields{"changed": changed})

	if changed {
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(updatedFlows, meters, &flowMetadata); err != nil { // This should never happen,meters should be installed before flow arrives
			logger.Error("Meter-referred-in-flows-not-present")
			return err
		}
		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: updatedFlows}, *lDevice.FlowGroups)
		if err != nil {
			return err
		}
		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		//	Update model
		if err := agent.updateLogicalDeviceFlowsWithoutLock(ctx, &ofp.Flows{Items: flows}); err != nil {
			logger.Errorw("db-flow-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
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
				if err := agent.updateLogicalDeviceMetersWithoutLock(ctx, metersToUpdate); err != nil {
					logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
					return err
				}
				logger.Debugw("meter-stats-updated-in-DB-successfully", log.Fields{"updated_meters": meters})

			}
		}
		// Send the flows to the devices
		respChannels := agent.addFlowsAndGroupsToDevices(ctx, deviceRules, &flowMetadata)

		// Create the go routines to wait
		go func() {
			// Wait for completion
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChannels...); res != nil {
				logger.Warnw("failure-to-add-flows", log.Fields{"errors": res, "logical-device-id": agent.logicalDeviceID})
				// TODO : revert added flow
			}
		}()
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
				return errors.New("Meter-referred-by-flow-is-not-found-in-logicaldevice")
			}
		}
	}
	logger.Debugw("meter-bands-for-flows", log.Fields{"flows": len(flows), "metadata": metadata})
	return nil

}

//flowDelete deletes a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowDelete(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debug("flowDelete")
	if mod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()

	var meters []*ofp.OfpMeterEntry
	var flows []*ofp.OfpFlowStats
	var flowGroups []*ofp.OfpGroupEntry

	if lDevice.Flows != nil && lDevice.Flows.Items != nil {
		flows = lDevice.Flows.Items
	}

	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}

	if lDevice.FlowGroups != nil && lDevice.FlowGroups.Items != nil {
		flowGroups = lDevice.FlowGroups.Items
	}

	//build a list of what to keep vs what to delete
	toKeep := make([]*ofp.OfpFlowStats, 0)
	toDelete := make([]*ofp.OfpFlowStats, 0)
	for _, f := range flows {
		// Check whether the flow and the flowmod matches
		fs, err := fu.FlowStatsEntryFromFlowModMessage(mod)
		if err != nil {
			return err
		}
		if fu.FlowMatch(f, fs) {
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

	logger.Debugw("flowDelete", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "toKeep": len(toKeep), "toDelete": toDelete})

	//Update flows
	if len(toDelete) > 0 {
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(toDelete, meters, &flowMetadata); err != nil { // This should never happen
			logger.Error("Meter-referred-in-flows-not-present")
			return errors.New("Meter-referred-in-flows-not-present")
		}
		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: toDelete}, ofp.FlowGroups{Items: flowGroups})
		if err != nil {
			return err
		}
		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.updateLogicalDeviceFlowsWithoutLock(ctx, &ofp.Flows{Items: toKeep}); err != nil {
			logger.Errorw("cannot-update-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

		// Update the devices
		respChnls := agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &flowMetadata)

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

func (agent *LogicalDeviceAgent) addFlowsAndGroupsToDevices(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
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

func (agent *LogicalDeviceAgent) deleteFlowsAndGroupsFromDevices(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw("send-delete-flows-to-device-manager", log.Fields{"logicalDeviceID": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			ctx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
			defer cancel()
			if err := agent.deviceMgr.deleteFlowsAndGroups(ctx, deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				logger.Error("flow-delete-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-delete-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	return responses
}

func (agent *LogicalDeviceAgent) updateFlowsAndGroupsOfDevice(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw("send-update-flows-to-device-manager", log.Fields{"logicalDeviceID": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			ctx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
			defer cancel()
			if err := agent.deviceMgr.updateFlowsAndGroups(ctx, deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				logger.Error("flow-update-failed", log.Fields{"deviceID": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-update-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	return responses
}

//flowDeleteStrict deletes a flow from the flow table of that logical device
func (agent *LogicalDeviceAgent) flowDeleteStrict(ctx context.Context, mod *ofp.OfpFlowMod) error {
	logger.Debug("flowDeleteStrict")
	if mod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()

	var meters []*ofp.OfpMeterEntry
	var flows []*ofp.OfpFlowStats
	var flowGroups []*ofp.OfpGroupEntry
	if lDevice.Meters != nil && lDevice.Meters.Items != nil {
		meters = lDevice.Meters.Items
	}
	if lDevice.Flows != nil && lDevice.Flows.Items != nil {
		flows = lDevice.Flows.Items
	}
	if lDevice.FlowGroups != nil && lDevice.FlowGroups.Items != nil {
		flowGroups = lDevice.FlowGroups.Items
	}

	changedFlow := false
	changedMeter := false
	flow, err := fu.FlowStatsEntryFromFlowModMessage(mod)
	if err != nil {
		return err
	}
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
		if err := agent.updateLogicalDeviceMetersWithoutLock(ctx, metersToUpdate); err != nil {
			logger.Errorw("db-meter-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

	}
	if changedFlow {
		var flowMetadata voltha.FlowMetadata
		if err := agent.GetMeterConfig(flowsToDelete, meters, &flowMetadata); err != nil {
			logger.Error("meter-referred-in-flows-not-present")
			return err
		}
		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: flowsToDelete}, ofp.FlowGroups{Items: flowGroups})
		if err != nil {
			return err
		}
		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if err := agent.updateLogicalDeviceFlowsWithoutLock(ctx, &ofp.Flows{Items: flows}); err != nil {
			logger.Errorw("cannot-update-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

		// Update the devices
		respChnls := agent.deleteFlowsAndGroupsFromDevices(ctx, deviceRules, &flowMetadata)

		// Wait for completion
		go func() {
			if res := coreutils.WaitForNilOrErrorResponses(agent.defaultTimeout, respChnls...); res != nil {
				logger.Warnw("failure-deleting-device-flows", log.Fields{"logicalDeviceId": agent.logicalDeviceID, "errors": res})
				//TODO: Revert flow changes
			}
		}()
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

func (agent *LogicalDeviceAgent) groupAdd(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupAdd")
	if groupMod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()

	groups := lDevice.FlowGroups.Items
	if fu.FindGroup(groups, groupMod.GroupId) == -1 {
		groups = append(groups, fu.GroupEntryFromGroupMod(groupMod))

		deviceRules := fu.NewDeviceRules()
		deviceRules.CreateEntryIfNotExist(agent.rootDeviceID)
		fg := fu.NewFlowsAndGroups()
		fg.AddGroup(fu.GroupEntryFromGroupMod(groupMod))
		deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)

		logger.Debugw("rules", log.Fields{"rules for group-add": deviceRules.String()})

		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(ctx, &ofp.FlowGroups{Items: groups}); err != nil {
			logger.Errorw("cannot-update-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
			return err
		}

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
	return fmt.Errorf("Groups %d already present", groupMod.GroupId)
}

func (agent *LogicalDeviceAgent) groupDelete(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupDelete")
	if groupMod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()
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
		deviceRules, err := agent.flowDecomposer.DecomposeRules(ctx, agent, ofp.Flows{Items: flows}, ofp.FlowGroups{Items: groups})
		if err != nil {
			return err
		}
		logger.Debugw("rules", log.Fields{"rules": deviceRules.String()})

		if groupsChanged {
			if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(ctx, &ofp.FlowGroups{Items: groups}); err != nil {
				logger.Errorw("cannot-update-group", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
				return err
			}
		}
		if flowsChanged {
			if err := agent.updateLogicalDeviceFlowsWithoutLock(ctx, &ofp.Flows{Items: flows}); err != nil {
				logger.Errorw("cannot-update-flow", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
				return err
			}
		}

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

func (agent *LogicalDeviceAgent) groupModify(ctx context.Context, groupMod *ofp.OfpGroupMod) error {
	logger.Debug("groupModify")
	if groupMod == nil {
		return nil
	}
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	lDevice := agent.getLogicalDeviceWithoutLock()
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
		deviceRules := fu.NewDeviceRules()
		deviceRules.CreateEntryIfNotExist(agent.rootDeviceID)
		fg := fu.NewFlowsAndGroups()
		fg.AddGroup(fu.GroupEntryFromGroupMod(groupMod))
		deviceRules.AddFlowsAndGroup(agent.rootDeviceID, fg)

		logger.Debugw("rules", log.Fields{"rules for group-modify": deviceRules.String()})

		if err := agent.updateLogicalDeviceFlowGroupsWithoutLock(ctx, &ofp.FlowGroups{Items: groups}); err != nil {
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
	}
	return nil
}

// deleteLogicalPort removes the logical port
func (agent *LogicalDeviceAgent) deleteLogicalPort(ctx context.Context, lPort *voltha.LogicalPort) error {
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
		copy(logicalDevice.Ports[index:], logicalDevice.Ports[index+1:])
		logicalDevice.Ports[len(logicalDevice.Ports)-1] = nil
		logicalDevice.Ports = logicalDevice.Ports[:len(logicalDevice.Ports)-1]
		logger.Debugw("logical-port-deleted", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
		if err := agent.updateLogicalDeviceWithoutLock(ctx, logicalDevice); err != nil {
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
func (agent *LogicalDeviceAgent) deleteLogicalPorts(ctx context.Context, deviceID string) error {
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
	logicalDevice.Ports = lPortstoKeep

	logger.Debugw("updated-logical-ports", log.Fields{"ports": lPortstoKeep})
	if err := agent.updateLogicalDeviceWithoutLock(ctx, logicalDevice); err != nil {
		logger.Errorw("logical-device-update-failed", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
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
func (agent *LogicalDeviceAgent) enableLogicalPort(ctx context.Context, lPortID string) error {
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
		logicalDevice.Ports[index].OfpPort.Config = logicalDevice.Ports[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDeviceWithoutLock(ctx, logicalDevice)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

// disableLogicalPort disabled the logical port
func (agent *LogicalDeviceAgent) disableLogicalPort(ctx context.Context, lPortID string) error {
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
		logicalDevice.Ports[index].OfpPort.Config = (logicalDevice.Ports[index].OfpPort.Config & ^uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)) | uint32(ofp.OfpPortConfig_OFPPC_PORT_DOWN)
		return agent.updateLogicalDeviceWithoutLock(ctx, logicalDevice)
	}
	return status.Errorf(codes.NotFound, "Port %s on Logical Device %s", lPortID, agent.logicalDeviceID)
}

func (agent *LogicalDeviceAgent) getPreCalculatedRoute(ingress, egress uint32) ([]route.Hop, error) {
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
func (agent *LogicalDeviceAgent) GetRoute(ctx context.Context, ingressPortNo uint32, egressPortNo uint32) ([]route.Hop, error) {
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
			return nil, status.Errorf(codes.FailedPrecondition, "no upstream route from:%d to:%d", ingressPortNo, egressPortNo)
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
		return nil, status.Errorf(codes.FailedPrecondition, "no upstream route from:%d to:%d", ingressPortNo, egressPortNo)
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
		return nil, status.Errorf(codes.FailedPrecondition, "no downstream route from:%d to:%d", ingressPortNo, egressPortNo)
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
	lDevice := agent.getLogicalDeviceWithoutLock()
	for _, port := range lDevice.Ports {
		if port.OfpPort.PortNo != exclPort {
			lPorts = append(lPorts, port.OfpPort.PortNo)
		}
	}
	return lPorts
}

// GetDeviceRoutes returns device graph
func (agent *LogicalDeviceAgent) GetDeviceRoutes() *route.DeviceRoutes {
	return agent.deviceRoutes
}

//rebuildRoutes rebuilds the device routes
func (agent *LogicalDeviceAgent) buildRoutes(ctx context.Context) error {
	logger.Debugf("building-routes", log.Fields{"logical-device-id": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.deviceRoutes == nil {
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.deviceMgr.GetDevice)
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
func (agent *LogicalDeviceAgent) updateRoutes(ctx context.Context, lp *voltha.LogicalPort) error {
	logger.Debugw("updateRoutes", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.deviceRoutes == nil {
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.deviceMgr.GetDevice)
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
func (agent *LogicalDeviceAgent) portUpdated(ctx context.Context, args ...interface{}) interface{} {
	logger.Debugw("portUpdated-callback", log.Fields{"argsLen": len(args)})

	var oldLD *voltha.LogicalDevice
	var newlD *voltha.LogicalDevice

	var ok bool
	if oldLD, ok = args[0].(*voltha.LogicalDevice); !ok {
		logger.Errorw("invalid-args", log.Fields{"args0": args[0]})
		return nil
	}
	if newlD, ok = args[1].(*voltha.LogicalDevice); !ok {
		logger.Errorw("invalid-args", log.Fields{"args1": args[1]})
		return nil
	}

	if reflect.DeepEqual(oldLD.Ports, newlD.Ports) {
		logger.Debug("ports-have-not-changed")
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
func (agent *LogicalDeviceAgent) addNNILogicalPort(ctx context.Context, device *voltha.Device, port *voltha.Port) (bool, error) {
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

	cloned := (proto.Clone(ld)).(*voltha.LogicalDevice)
	if cloned.Ports == nil {
		cloned.Ports = make([]*voltha.LogicalPort, 0)
	}
	cloned.Ports = append(cloned.Ports, lp)

	if err = agent.updateLogicalDeviceWithoutLock(ctx, cloned); err != nil {
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

func (agent *LogicalDeviceAgent) portExist(device *voltha.Device, port *voltha.Port) bool {
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
func (agent *LogicalDeviceAgent) addUNILogicalPort(ctx context.Context, childDevice *voltha.Device, port *voltha.Port) (bool, error) {
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
	cloned := (proto.Clone(ldevice)).(*voltha.LogicalDevice)
	if cloned.Ports == nil {
		cloned.Ports = make([]*voltha.LogicalPort, 0)
	}
	cloned.Ports = append(cloned.Ports, portCap.Port)
	if err := agent.updateLogicalDeviceWithoutLock(ctx, cloned); err != nil {
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

func (agent *LogicalDeviceAgent) packetOut(ctx context.Context, packet *ofp.OfpPacketOut) {
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

func (agent *LogicalDeviceAgent) packetIn(port uint32, transactionID string, packet []byte) {
	logger.Debugw("packet-in", log.Fields{
		"port":          port,
		"packet":        hex.EncodeToString(packet),
		"transactionId": transactionID,
	})
	packetIn := fu.MkPacketIn(port, packet)
	agent.ldeviceMgr.grpcNbiHdlr.sendPacketIn(agent.logicalDeviceID, transactionID, packetIn)
	logger.Debugw("sending-packet-in", log.Fields{"packet": hex.EncodeToString(packetIn.Data)})
}

func (agent *LogicalDeviceAgent) addLogicalPortToMap(portNo uint32, nniPort bool) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	if exist := agent.logicalPortsNo[portNo]; !exist {
		agent.logicalPortsNo[portNo] = nniPort
	}
}

func (agent *LogicalDeviceAgent) deleteLogicalPortsFromMap(portsNo []uint32) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, pNo := range portsNo {
		delete(agent.logicalPortsNo, pNo)
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

//GetNNIPorts returns NNI ports.
func (agent *LogicalDeviceAgent) GetNNIPorts() []uint32 {
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
