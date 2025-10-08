/*
 * Copyright 2018-2024 Open Networking Foundation (ONF) and the ONF Contributors

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
	"fmt"
	"sync"
	"time"

	"github.com/opencord/voltha-protos/v5/go/common"

	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/core"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/event"
	"github.com/opencord/voltha-go/rw_core/core/device/state"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/events"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Manager represent device manager attributes
type Manager struct {
	rootDevices map[string]bool
	*event.Agent
	adapterMgr              *adapter.Manager
	logicalDeviceMgr        *LogicalManager
	stateTransitions        *state.TransitionMap
	dbPath                  *model.Path
	dProxy                  *model.Proxy
	deviceLoadingInProgress map[string][]chan int
	config                  *config.RWCoreFlags
	doneCh                  chan struct{}
	deviceAgents            sync.Map
	coreInstanceID          string
	internalTimeout         time.Duration
	rpcTimeout              time.Duration
	flowTimeout             time.Duration
	lockRootDeviceMap       sync.RWMutex
	devicesLoadingLock      sync.RWMutex
}

// NewManagers creates the Manager and the Logical Manager.
func NewManagers(dbPath *model.Path, adapterMgr *adapter.Manager, cf *config.RWCoreFlags, coreInstanceID string, eventProxy *events.EventProxy) (*Manager, *LogicalManager) {
	deviceMgr := &Manager{
		rootDevices:             make(map[string]bool),
		coreInstanceID:          coreInstanceID,
		dbPath:                  dbPath,
		dProxy:                  dbPath.Proxy("devices"),
		adapterMgr:              adapterMgr,
		internalTimeout:         cf.InternalTimeout,
		rpcTimeout:              cf.RPCTimeout,
		flowTimeout:             cf.FlowTimeout,
		Agent:                   event.NewAgent(eventProxy, coreInstanceID, cf.VolthaStackID),
		deviceLoadingInProgress: make(map[string][]chan int),
		config:                  cf,
		doneCh:                  make(chan struct{}),
	}
	deviceMgr.stateTransitions = state.NewTransitionMap(deviceMgr)

	logicalDeviceMgr := &LogicalManager{
		Manager:                        event.NewManager(eventProxy, coreInstanceID, cf.VolthaStackID),
		deviceMgr:                      deviceMgr,
		dbPath:                         dbPath,
		ldProxy:                        dbPath.Proxy("logical_devices"),
		internalTimeout:                cf.InternalTimeout,
		logicalDeviceLoadingInProgress: make(map[string][]chan int),
	}
	deviceMgr.logicalDeviceMgr = logicalDeviceMgr

	adapterMgr.SetAdapterRestartedCallback(deviceMgr.adapterRestartedHandler)

	return deviceMgr, logicalDeviceMgr
}

func (dMgr *Manager) Start(ctx context.Context, serviceName string) error {
	logger.Info(ctx, "starting-device-manager")
	probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusPreparing)

	// Load all the devices from the dB
	var devices []*voltha.Device
	if err := dMgr.dProxy.List(ctx, &devices); err != nil {
		// Any error from the dB means if we proceed we may end up with corrupted data
		logger.Errorw(ctx, "failed-to-list-devices-from-KV", log.Fields{"error": err, "service-name": serviceName})
		return err
	}

	defer probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusRunning)

	if len(devices) == 0 {
		logger.Info(ctx, "no-device-to-load")
		return nil
	}

	for _, device := range devices {
		// Create an agent for each device
		agent := newAgent(device, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.internalTimeout, dMgr.rpcTimeout, dMgr.flowTimeout)
		if _, err := agent.start(ctx, true, device); err != nil {
			logger.Warnw(ctx, "failure-starting-agent", log.Fields{"device-id": device.Id})
		} else {
			dMgr.addDeviceAgentToMap(agent)
		}
		// In case core goes down after it sets the transient state as reconciling but missed to fire the reconcile request to the adaptors, it should refire those reconcile requests on restart
		if device.OperStatus != common.OperStatus_RECONCILING && (device.OperStatus == common.OperStatus_RECONCILING_FAILED || agent.matchTransientState(core.DeviceTransientState_RECONCILE_IN_PROGRESS)) {
			go agent.ReconcileDevice(ctx)
		}
	}
	logger.Info(ctx, "device-manager-started")

	return nil
}

func (dMgr *Manager) Stop(ctx context.Context, serviceName string) {
	logger.Info(ctx, "stopping-device-manager")
	close(dMgr.doneCh)
	probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusStopped)
}

func (dMgr *Manager) addDeviceAgentToMap(agent *Agent) {
	if _, exist := dMgr.deviceAgents.Load(agent.deviceID); !exist {
		dMgr.deviceAgents.Store(agent.deviceID, agent)
	}
	dMgr.lockRootDeviceMap.Lock()
	defer dMgr.lockRootDeviceMap.Unlock()
	dMgr.rootDevices[agent.deviceID] = agent.isRootDevice

}

func (dMgr *Manager) deleteDeviceAgentFromMap(agent *Agent) {
	dMgr.deviceAgents.Delete(agent.deviceID)
	dMgr.lockRootDeviceMap.Lock()
	defer dMgr.lockRootDeviceMap.Unlock()
	delete(dMgr.rootDevices, agent.deviceID)
}

// getDeviceAgent returns the agent managing the device.  If the device is not in memory, it will loads it, if it exists
func (dMgr *Manager) getDeviceAgent(ctx context.Context, deviceID string) *Agent {
	agent, ok := dMgr.deviceAgents.Load(deviceID)
	if ok {
		return agent.(*Agent)
	}
	// Try to load into memory - loading will also create the device agent and set the device ownership
	err := dMgr.load(ctx, deviceID)
	if err == nil {
		agent, ok = dMgr.deviceAgents.Load(deviceID)
		if !ok {
			return nil
		}
		return agent.(*Agent)
	}
	// TODO: Change the return params to return an error as well
	logger.Errorw(ctx, "loading-device-failed", log.Fields{"device-id": deviceID, "error": err})
	return nil
}

// listDeviceIdsFromMap returns the list of device IDs that are in memory
func (dMgr *Manager) listDeviceIdsFromMap() *voltha.IDs {
	result := &voltha.IDs{Items: make([]*voltha.ID, 0)}

	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		result.Items = append(result.Items, &voltha.ID{Id: key.(string)})
		return true
	})

	return result
}

// stopManagingDevice stops the management of the device as well as any of its reference device and logical device.
// This function is called only in the Core that does not own this device.  In the Core that owns this device then a
// deletion deletion also includes removal of any reference of this device.
func (dMgr *Manager) stopManagingDevice(ctx context.Context, id string) {
	logger.Infow(ctx, "stop-managing-device", log.Fields{"device-id": id})
	if dMgr.IsDeviceInCache(id) { // Proceed only if an agent is present for this device
		if device, err := dMgr.getDeviceReadOnly(ctx, id); err == nil && device.Root {
			// stop managing the logical device
			_ = dMgr.logicalDeviceMgr.stopManagingLogicalDeviceWithDeviceID(ctx, id)
		}
		if agent := dMgr.getDeviceAgent(ctx, id); agent != nil {
			if err := agent.stop(ctx); err != nil {
				logger.Warnw(ctx, "unable-to-stop-device-agent", log.Fields{"device-id": agent.deviceID, "error": err})
			}
			dMgr.deleteDeviceAgentFromMap(agent)
		}
	}
}

// getDeviceReadOnly will returns a device, either from memory or from the dB, if present
func (dMgr *Manager) getDeviceReadOnly(ctx context.Context, id string) (*voltha.Device, error) {
	logger.Debugw(ctx, "get-device-read-only", log.Fields{"device-id": id})
	if agent := dMgr.getDeviceAgent(ctx, id); agent != nil {
		return agent.getDeviceReadOnly(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", id)
}

func (dMgr *Manager) listDevicePorts(ctx context.Context, id string) (map[uint32]*voltha.Port, error) {
	logger.Debugw(ctx, "list-device-ports", log.Fields{"device-id": id})
	agent := dMgr.getDeviceAgent(ctx, id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id)
	}
	return agent.listDevicePorts(), nil
}

// IsDeviceInCache returns true if device is found in the map
func (dMgr *Manager) IsDeviceInCache(id string) bool {
	_, exist := dMgr.deviceAgents.Load(id)
	return exist
}

// isParentDeviceExist checks whether device is already preprovisioned.
func (dMgr *Manager) isParentDeviceExist(ctx context.Context, newDevice *voltha.Device) (bool, error) {
	hostPort := newDevice.GetHostAndPort()
	var devices []*voltha.Device
	if err := dMgr.dProxy.List(ctx, &devices); err != nil {
		logger.Errorw(ctx, "failed-to-list-devices-from-cluster-data-proxy", log.Fields{"error": err})
		return false, err
	}
	for _, device := range devices {
		if !device.Root {
			continue
		}

		if hostPort != "" && hostPort == device.GetHostAndPort() {
			return true, nil
		}
		if newDevice.MacAddress != "" && newDevice.MacAddress == device.MacAddress {
			return true, nil
		}
	}
	return false, nil
}

// getDeviceFromModelretrieves the device data from the model.
func (dMgr *Manager) getDeviceFromModel(ctx context.Context, deviceID string) (*voltha.Device, error) {
	device := &voltha.Device{}
	if have, err := dMgr.dProxy.Get(ctx, deviceID, device); err != nil {
		logger.Errorw(ctx, "failed-to-get-device-info-from-cluster-proxy", log.Fields{"error": err})
		return nil, err
	} else if !have {
		return nil, status.Error(codes.NotFound, deviceID)
	}

	return device, nil
}

// loadDevice loads the deviceID in memory, if not present
func (dMgr *Manager) loadDevice(ctx context.Context, deviceID string) (*Agent, error) {
	if deviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "deviceId empty")
	}
	var err error
	var device *voltha.Device
	dMgr.devicesLoadingLock.Lock()
	if _, exist := dMgr.deviceLoadingInProgress[deviceID]; !exist {
		if !dMgr.IsDeviceInCache(deviceID) {
			dMgr.deviceLoadingInProgress[deviceID] = []chan int{make(chan int, 1)}
			dMgr.devicesLoadingLock.Unlock()
			// Proceed with the loading only if the device exist in the Model (could have been deleted)
			if device, err = dMgr.getDeviceFromModel(ctx, deviceID); err == nil {
				logger.Debugw(ctx, "loading-device", log.Fields{"device-id": deviceID})
				agent := newAgent(device, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.internalTimeout, dMgr.rpcTimeout, dMgr.flowTimeout)
				if _, err = agent.start(ctx, true, device); err != nil {
					logger.Warnw(ctx, "failure-loading-device", log.Fields{"device-id": deviceID, "error": err})
				} else {
					dMgr.addDeviceAgentToMap(agent)
				}
			} else {
				logger.Debugw(ctx, "device-is-not-in-model", log.Fields{"device-id": deviceID})
			}
			// announce completion of task to any number of waiting channels
			dMgr.devicesLoadingLock.Lock()
			if v, ok := dMgr.deviceLoadingInProgress[deviceID]; ok {
				for _, ch := range v {
					close(ch)
				}
				delete(dMgr.deviceLoadingInProgress, deviceID)
			}
			dMgr.devicesLoadingLock.Unlock()
		} else {
			dMgr.devicesLoadingLock.Unlock()
		}
	} else {
		ch := make(chan int, 1)
		dMgr.deviceLoadingInProgress[deviceID] = append(dMgr.deviceLoadingInProgress[deviceID], ch)
		dMgr.devicesLoadingLock.Unlock()
		// Wait for the channel to be closed, implying the process loading this device is done.
		<-ch
	}
	if agent, ok := dMgr.deviceAgents.Load(deviceID); ok {
		return agent.(*Agent), nil
	}
	return nil, status.Errorf(codes.Aborted, "Error loading device %s", deviceID)
}

// loadRootDeviceParentAndChildren loads the children and parents of a root device in memory
func (dMgr *Manager) loadRootDeviceParentAndChildren(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "loading-parent-and-children", log.Fields{"device-id": device.Id})
	if device.Root {
		// Scenario A
		if device.ParentId != "" {
			// Load logical device if needed.
			if err := dMgr.logicalDeviceMgr.load(ctx, device.ParentId); err != nil {
				logger.Warnw(ctx, "failure-loading-logical-device", log.Fields{"logical-device-id": device.ParentId})
			}
		} else {
			logger.Debugw(ctx, "no-parent-to-load", log.Fields{"device-id": device.Id})
		}
		// Load all child devices, if needed
		childDeviceIds := dMgr.getAllChildDeviceIds(ctx, device.Id)
		for childDeviceID := range childDeviceIds {
			if _, err := dMgr.loadDevice(ctx, childDeviceID); err != nil {
				logger.Warnw(ctx, "failure-loading-device", log.Fields{"device-id": childDeviceID, "error": err})
				return err
			}
		}
		logger.Debugw(ctx, "loaded-children", log.Fields{"device-id": device.Id, "num-children": len(childDeviceIds)})
	}
	return nil
}

// load loads the deviceId in memory, if not present, and also loads its accompanying parents and children.  Loading
// in memory is for improved performance.  It is not imperative that a device needs to be in memory when a request
// acting on the device is received by the core. In such a scenario, the Core will load the device in memory first
// and the proceed with the request.
func (dMgr *Manager) load(ctx context.Context, deviceID string) error {
	logger.Debug(ctx, "load...")
	// First load the device - this may fail in case the device was deleted intentionally by the other core
	var dAgent *Agent
	var err error
	if dAgent, err = dMgr.loadDevice(ctx, deviceID); err != nil {
		return err
	}
	// Get the loaded device details
	device, err := dAgent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}

	// If the device is in Pre-provisioning or getting deleted state stop here
	if device.AdminState == voltha.AdminState_PREPROVISIONED || dAgent.isDeletionInProgress() {
		return nil
	}

	// Now we face two scenarios
	if device.Root {

		// Load all children as well as the parent of this device (logical_device)
		if err := dMgr.loadRootDeviceParentAndChildren(ctx, device); err != nil {
			logger.Warnw(ctx, "failure-loading-device-parent-and-children", log.Fields{"device-id": deviceID})
			return err
		}
		logger.Debugw(ctx, "successfully-loaded-parent-and-children", log.Fields{"device-id": deviceID})
	} else if device.ParentId != "" {
		// Scenario B - use the parentId of that device (root device) to trigger the loading
		return dMgr.load(ctx, device.ParentId)
	}

	return nil
}

// adapterRestarted is invoked whenever an adapter is restarted
func (dMgr *Manager) adapterRestarted(ctx context.Context, adapter *voltha.Adapter) error {
	logger.Debugw(ctx, "adapter-restarted", log.Fields{"adapter-id": adapter.Id, "vendor": adapter.Vendor,
		"current-replica": adapter.CurrentReplica, "total-replicas": adapter.TotalReplicas,
		"restarted-endpoint": adapter.Endpoint, "current-version": adapter.Version})

	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		deviceAgent, ok := value.(*Agent)
		if ok && deviceAgent.adapterEndpoint == adapter.Endpoint {
			// Before reconciling, abort in-process request
			if err := deviceAgent.abortAllProcessing(utils.WithNewSpanAndRPCMetadataContext(ctx, "AbortProcessingOnRestart")); err == nil {
				logger.Debugw(ctx, "setting transient state",
					log.Fields{
						"device-id":          deviceAgent.deviceID,
						"root-device":        deviceAgent.isRootDevice,
						"restarted-endpoint": adapter.Endpoint,
						"device-type":        deviceAgent.deviceType,
						"adapter-type":       adapter.Type,
					})
			} else {
				logger.Errorw(ctx, "failed-aborting-exisiting-processing", log.Fields{"error": err})
			}
		}
		return true
	})

	go func() {
		numberOfDevicesToReconcile := 0
		dMgr.deviceAgents.Range(func(key, value interface{}) bool {
			deviceAgent, ok := value.(*Agent)
			if ok && deviceAgent.adapterEndpoint == adapter.Endpoint {
				logger.Debugw(ctx, "reconciling-device",
					log.Fields{
						"device-id":          deviceAgent.deviceID,
						"root-device":        deviceAgent.isRootDevice,
						"restarted-endpoint": adapter.Endpoint,
						"device-type":        deviceAgent.deviceType,
						"adapter-type":       adapter.Type,
					})
				go deviceAgent.StartReconcileWithRetry(utils.WithNewSpanAndRPCMetadataContext(ctx, "ReconcileDevice"))
				numberOfDevicesToReconcile++
			}
			return true
		})
		logger.Debug(ctx, "reconciling-on-adapter-restart-initiated", log.Fields{"adapter-endpoint": adapter.Endpoint, "number-of-devices-to-reconcile": numberOfDevicesToReconcile})
	}()

	return nil
}

func (dMgr *Manager) UpdateDeviceUsingAdapterData(ctx context.Context, device *voltha.Device) error {
	logger.Debugw(ctx, "update-device-using-adapter-data", log.Fields{"device-id": device.Id, "device": device})
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		return agent.updateDeviceUsingAdapterData(ctx, device)
	}
	return status.Errorf(codes.NotFound, "%s", device.Id)
}

func (dMgr *Manager) addPeerPort(ctx context.Context, deviceID string, port *voltha.Port) error {
	meAsPeer := &voltha.Port_PeerPort{DeviceId: deviceID, PortNo: port.PortNo}
	for _, peerPort := range port.Peers {
		if agent := dMgr.getDeviceAgent(ctx, peerPort.DeviceId); agent != nil {
			if err := agent.addPeerPort(ctx, meAsPeer); err != nil {
				return err
			}
		}
	}
	// Notify the logical device manager to setup a logical port, if needed.  If the added port is an NNI or UNI
	// then a logical port will be added to the logical device and the device route generated.  If the port is a
	// PON port then only the device graph will be generated.
	device, err := dMgr.getDeviceReadOnly(ctx, deviceID)
	if err != nil {
		return err
	}
	ports, err := dMgr.listDevicePorts(ctx, deviceID)
	if err != nil {
		return err
	}
	subCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)

	if err = dMgr.logicalDeviceMgr.updateLogicalPort(subCtx, device, ports, port); err != nil {
		return err
	}
	return nil
}

func (dMgr *Manager) AddPort(ctx context.Context, deviceID string, port *voltha.Port) error {
	agent := dMgr.getDeviceAgent(ctx, deviceID)
	if agent != nil {
		if err := agent.addPort(ctx, port); err != nil {
			return err
		}
		// Setup peer ports in its own routine
		if err := dMgr.addPeerPort(ctx, deviceID, port); err != nil {
			logger.Errorw(ctx, "unable-to-add-peer-port", log.Fields{"error": err, "device-id": deviceID})
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) canAdapterRequestProceed(ctx context.Context, deviceID string) error {
	agent := dMgr.getDeviceAgent(ctx, deviceID)
	if agent == nil {
		logger.Errorw(ctx, "device-nil", log.Fields{"device-id": deviceID})
		return status.Errorf(codes.NotFound, "device-nil-for-%s", deviceID)
	}
	if !agent.isAdapterConnectionUp(ctx) {
		return status.Errorf(codes.Unavailable, "adapter-connection-down-for-%s", deviceID)
	}
	if err := agent.canDeviceRequestProceed(ctx); err != nil {
		return err
	}
	// Perform the same checks for parent device
	if !agent.isRootDevice {
		parentDeviceAgent := dMgr.getDeviceAgent(ctx, agent.parentID)
		if parentDeviceAgent == nil {
			logger.Errorw(ctx, "parent-device-adapter-nil", log.Fields{"parent-id": agent.parentID})
			return status.Errorf(codes.NotFound, "parent-device-adapter-nil-for-%s", deviceID)
		}
		if err := parentDeviceAgent.canDeviceRequestProceed(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (dMgr *Manager) canMultipleAdapterRequestProceed(ctx context.Context, deviceIDs []string) error {
	if len(deviceIDs) == 0 {
		return status.Error(codes.Unavailable, "adapter(s)-not-ready")
	}

	for _, deviceID := range deviceIDs {
		if err := dMgr.canAdapterRequestProceed(ctx, deviceID); err != nil {
			return err
		}
	}

	return nil
}

func (dMgr *Manager) addFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) error {
	logger.Debugw(ctx, "add-flows-and-groups", log.Fields{"device-id": deviceID, "groups:": groups, "flow-metadata": flowMetadata})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.addFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// deleteParentFlows removes flows from the parent device based on  specific attributes
func (dMgr *Manager) deleteParentFlows(ctx context.Context, deviceID string, uniPort uint32, metadata *ofp.FlowMetadata) error {
	logger.Debugw(ctx, "delete-parent-flows", log.Fields{"device-id": deviceID, "uni-port": uniPort, "metadata": metadata})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if !agent.isRootDevice {
			return status.Errorf(codes.FailedPrecondition, "not-a-parent-device-%s", deviceID)
		}
		return agent.filterOutFlows(ctx, uniPort, metadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) deleteFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) error {
	logger.Debugw(ctx, "delete-flows-and-groups", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.deleteFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) updateFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *ofp.FlowMetadata) error {
	logger.Debugw(ctx, "update-flows-and-groups", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// InitPmConfigs initialize the pm configs as defined by the adapter.
func (dMgr *Manager) InitPmConfigs(ctx context.Context, deviceID string, pmConfigs *voltha.PmConfigs) error {
	if pmConfigs.Id == "" {
		return status.Errorf(codes.FailedPrecondition, "invalid-device-Id")
	}
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.initPmConfigs(ctx, pmConfigs)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) getSwitchCapability(ctx context.Context, deviceID string) (*ca.SwitchCapability, error) {
	logger.Debugw(ctx, "get-switch-capability", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.getSwitchCapability(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) UpdateDeviceStatus(ctx context.Context, deviceID string, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	logger.Debugw(ctx, "update-device-status", log.Fields{"device-id": deviceID, "oper-status": operStatus, "conn-status": connStatus})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateDeviceStatus(ctx, operStatus, connStatus)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) UpdateChildrenStatus(ctx context.Context, deviceID string, operStatus voltha.OperStatus_Types, connStatus voltha.ConnectStatus_Types) error {
	logger.Debugw(ctx, "update-children-status", log.Fields{"parent-device-id": deviceID, "oper-status": operStatus, "conn-status": connStatus})
	for childDeviceID := range dMgr.getAllChildDeviceIds(ctx, deviceID) {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err := agent.updateDeviceStatus(ctx, operStatus, connStatus); err != nil {
				return status.Errorf(codes.Aborted, "childDevice:%s, error:%s", childDeviceID, err.Error())
			}
		}
	}
	return nil
}

func (dMgr *Manager) UpdatePortState(ctx context.Context, deviceID string, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "update-port-state", log.Fields{"device-id": deviceID, "port-type": portType, "port-no": portNo, "oper-status": operStatus})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if err := agent.updatePortState(ctx, portType, portNo, operStatus); err != nil {
			logger.Errorw(ctx, "updating-port-state-failed", log.Fields{"device-id": deviceID, "port-no": portNo, "error": err})
			return err
		}
		// Notify the logical device manager to change the port state
		// Do this for NNI and UNIs only. PON ports are not known by logical device
		if portType == voltha.Port_ETHERNET_NNI || portType == voltha.Port_ETHERNET_UNI {
			err := dMgr.logicalDeviceMgr.updatePortState(ctx, deviceID, portNo, operStatus)
			if err != nil {
				// While we want to handle (catch) and log when
				// an update to a port was not able to be
				// propagated to the logical port, we can report
				// it as a warning and not an error because it
				// doesn't stop or modify processing.
				// TODO: VOL-2707
				logger.Warnw(ctx, "unable-to-update-logical-port-state", log.Fields{"error": err})
			}
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// UpdatePortsState updates all ports on the device
func (dMgr *Manager) UpdatePortsState(ctx context.Context, deviceID string, portTypeFilter uint32, state voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "update-ports-state", log.Fields{"device-id": deviceID})
	agent := dMgr.getDeviceAgent(ctx, deviceID)
	if agent == nil {
		return status.Errorf(codes.NotFound, "%s", deviceID)
	}
	if state != voltha.OperStatus_ACTIVE && state != voltha.OperStatus_UNKNOWN {
		return status.Error(codes.Unimplemented, "state-change-not-implemented")
	}
	if err := agent.updatePortsOperState(ctx, portTypeFilter, state); err != nil {
		logger.Warnw(ctx, "update-ports-state-failed", log.Fields{"device-id": deviceID, "error": err})
		return err
	}
	return nil
}

func (dMgr *Manager) packetOut(ctx context.Context, deviceID string, outPort uint32, packet *ofp.OfpPacketOut) error {
	logger.Debugw(ctx, "packet-out", log.Fields{"device-id": deviceID, "out-port": outPort})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.packetOut(ctx, outPort, packet)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// PacketIn receives packet from adapter
func (dMgr *Manager) PacketIn(ctx context.Context, deviceID string, port uint32, transactionID string, packet []byte) error {
	logger.Debugw(ctx, "packet-in", log.Fields{"device-id": deviceID, "port": port})
	// Get the logical device Id based on the deviceId
	var device *voltha.Device
	var err error
	if device, err = dMgr.getDeviceReadOnly(ctx, deviceID); err != nil {
		logger.Errorw(ctx, "device-not-found", log.Fields{"device-id": deviceID})
		return err
	}
	if !device.Root {
		logger.Errorw(ctx, "device-not-root", log.Fields{"device-id": deviceID})
		return status.Errorf(codes.FailedPrecondition, "%s", deviceID)
	}

	if err := dMgr.logicalDeviceMgr.packetIn(ctx, device.ParentId, port, packet); err != nil {
		return err
	}
	return nil
}

func (dMgr *Manager) setParentID(ctx context.Context, device *voltha.Device, parentID string) error {
	logger.Debugw(ctx, "set-parent-id", log.Fields{"device-id": device.Id, "parent-id": parentID})
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		return agent.setParentID(ctx, device, parentID)
	}
	return status.Errorf(codes.NotFound, "%s", device.Id)
}

func (dMgr *Manager) getParentDevice(ctx context.Context, childDevice *voltha.Device) (*voltha.Device, error) {
	//	Sanity check
	if childDevice.Root {
		// childDevice is the parent device
		return childDevice, nil
	}
	parentDevice, err := dMgr.getDeviceReadOnly(ctx, childDevice.ParentId)
	if err != nil {
		return nil, err
	}
	return parentDevice, nil

}

/*
 All the functions below are callback functions where they are invoked with the latest and previous data.  We can
 therefore use the data as is without trying to get the latest from the model.
*/

// DisableAllChildDevices is invoked as a callback when the parent device is disabled
func (dMgr *Manager) DisableAllChildDevices(ctx context.Context, parentCurrDevice *voltha.Device) error {
	logger.Debug(ctx, "disable-all-child-devices")
	for childDeviceID := range dMgr.getAllChildDeviceIds(ctx, parentCurrDevice.Id) {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err := agent.disableDevice(ctx); err != nil {
				// Just log the error - this error happens only if the child device was already in deleted state.
				logger.Errorw(ctx, "failure-disable-device", log.Fields{"device-id": childDeviceID, "error": err.Error()})
			}
		}
	}
	return nil
}

// getAllChildDeviceIds is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *Manager) getAllChildDeviceIds(ctx context.Context, parentDeviceID string) map[string]struct{} {
	logger.Debug(ctx, "get-all-child-device-ids")
	childDeviceIds := make(map[string]struct{})
	dMgr.deviceAgents.Range(func(_, value interface{}) bool {
		if value.(*Agent).device.ParentId == parentDeviceID && !value.(*Agent).device.Root {
			childDeviceIds[value.(*Agent).device.Id] = struct{}{}
		}
		return true
	})
	logger.Debugw(ctx, "returning-getAllChildDeviceIds.", log.Fields{"childDeviceIds": childDeviceIds})
	return childDeviceIds
}

// GgtAllChildDevices is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *Manager) getAllChildDevices(ctx context.Context, parentDeviceID string) (*voltha.Devices, error) {
	logger.Debugw(ctx, "get-all-child-devices", log.Fields{"parent-device-id": parentDeviceID})
	childDevices := make([]*voltha.Device, 0)
	for deviceID := range dMgr.getAllChildDeviceIds(ctx, parentDeviceID) {
		if d, e := dMgr.getDeviceReadOnly(ctx, deviceID); e == nil && d != nil {
			childDevices = append(childDevices, d)
		}
	}
	return &voltha.Devices{Items: childDevices}, nil
}

func (dMgr *Manager) NotifyInvalidTransition(ctx context.Context, device *voltha.Device) error {
	logger.Errorw(ctx, "notify-invalid-transition", log.Fields{
		"device":           device.Id,
		"curr-admin-state": device.AdminState,
		"curr-oper-state":  device.OperStatus,
		"curr-conn-state":  device.ConnectStatus,
	})
	// TODO: notify over kafka?
	return nil
}

// UpdateDeviceAttribute updates value of particular device attribute
func (dMgr *Manager) UpdateDeviceAttribute(ctx context.Context, deviceID string, attribute string, value interface{}) {
	if agent, ok := dMgr.deviceAgents.Load(deviceID); ok {
		agent.(*Agent).updateDeviceAttribute(ctx, attribute, value)
	}
}

// GetParentDeviceID returns parent device id, either from memory or from the dB, if present
func (dMgr *Manager) GetParentDeviceID(ctx context.Context, deviceID string) string {
	if device, _ := dMgr.getDeviceReadOnly(ctx, deviceID); device != nil {
		logger.Infow(ctx, "get-parent-device-id", log.Fields{"device-id": device.Id, "parent-id": device.ParentId})
		return device.ParentId
	}
	return ""
}

func (dMgr *Manager) UpdateDeviceReason(ctx context.Context, deviceID string, reason string) error {
	logger.Debugw(ctx, "update-device-reason", log.Fields{"device-id": deviceID, "reason": reason})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateDeviceReason(ctx, reason)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) SendRPCEvent(ctx context.Context, id string, rpcEvent *voltha.RPCEvent,
	category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	// TODO Instead of directly sending to the kafka bus, queue the message and send it asynchronously
	dMgr.Agent.SendRPCEvent(ctx, id, rpcEvent, category, subCategory, raisedTs)
}

func (dMgr *Manager) GetTransientState(ctx context.Context, id string) (core.DeviceTransientState_Types, error) {
	agent := dMgr.getDeviceAgent(ctx, id)
	if agent == nil {
		return core.DeviceTransientState_NONE, status.Errorf(codes.NotFound, "%s", id)
	}
	return agent.getTransientState(), nil
}

func (dMgr *Manager) validateImageDownloadRequest(request *voltha.DeviceImageDownloadRequest) error {
	if request == nil || request.Image == nil || len(request.DeviceId) == 0 {
		return status.Errorf(codes.InvalidArgument, "invalid argument")
	}

	for _, deviceID := range request.DeviceId {
		if deviceID == nil {
			return status.Errorf(codes.InvalidArgument, "id is nil")
		}
	}
	return nil
}

func (dMgr *Manager) validateImageRequest(request *voltha.DeviceImageRequest) error {
	if request == nil {
		return status.Errorf(codes.InvalidArgument, "invalid argument")
	}

	for _, deviceID := range request.DeviceId {
		if deviceID == nil {
			return status.Errorf(codes.InvalidArgument, "id is nil")
		}
	}

	return nil
}

func (dMgr *Manager) validateDeviceImageResponse(response *voltha.DeviceImageResponse) error {
	if response == nil || len(response.GetDeviceImageStates()) == 0 || response.GetDeviceImageStates()[0] == nil {
		return status.Errorf(codes.Internal, "invalid-response-from-adapter")
	}

	return nil
}

func (dMgr *Manager) waitForAllResponses(ctx context.Context, opName string, respCh chan []*voltha.DeviceImageState, expectedResps int) (*voltha.DeviceImageResponse, error) {
	response := &voltha.DeviceImageResponse{}
	respCount := 0
	for {
		select {
		case resp, ok := <-respCh:
			if !ok {
				logger.Errorw(ctx, opName+"-failed", log.Fields{"error": "channel-closed"})
				return response, status.Errorf(codes.Aborted, "channel-closed")
			}

			if resp != nil {
				logger.Debugw(ctx, opName+"-result", log.Fields{"image-state": resp[0].GetImageState(), "device-id": resp[0].GetDeviceId()})
				response.DeviceImageStates = append(response.DeviceImageStates, resp...)
			}

			respCount++

			// Check whether all responses received, if so, sent back the collated response
			if respCount == expectedResps {
				return response, nil
			}
			continue
		case <-ctx.Done():
			return nil, status.Errorf(codes.Aborted, opName+"-failed-%s", ctx.Err())
		}
	}
}

func (dMgr *Manager) ReconcilingCleanup(ctx context.Context, device *voltha.Device) error {
	agent := dMgr.getDeviceAgent(ctx, device.Id)
	if agent == nil {
		logger.Errorf(ctx, "Not able to get device agent.")
		return status.Errorf(codes.NotFound, "Not able to get device agent for device : %s", device.Id)
	}
	err := agent.reconcilingCleanup(ctx)
	if err != nil {
		logger.Errorf(ctx, err.Error())
		return status.Errorf(codes.Internal, "%s", err.Error())
	}
	return nil
}

func (dMgr *Manager) adapterRestartedHandler(ctx context.Context, endpoint string) error {
	// Get the adapter corresponding to that endpoint
	if a, _ := dMgr.adapterMgr.GetAdapterWithEndpoint(ctx, endpoint); a != nil {
		if rollingUpdate, _ := dMgr.adapterMgr.GetRollingUpdate(ctx, endpoint); rollingUpdate {
			dMgr.adapterMgr.RegisterOnRxStreamCloseChMap(ctx, endpoint)
			dMgr.adapterMgr.DeleteRollingUpdate(ctx, endpoint)
			// In case of rolling update we need to start the connection towards the new adapter instance now
			if err := dMgr.adapterMgr.StartAdapterWithEndPoint(ctx, endpoint); err != nil {
				return err
			}
		}
		return dMgr.adapterRestarted(ctx, a)
	}
	logger.Errorw(ctx, "restarted-adapter-not-found", log.Fields{"endpoint": endpoint})
	return fmt.Errorf("restarted adapter at endpoint %s not found", endpoint)
}

func (dMgr *Manager) GetOnuDeviceIdBySerial(ctx context.Context, device *voltha.OnuSerialNumberOnOLTPon) (string, error) {
	var onuDeviceId string
	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		devAgent := value.(*Agent)
		dev := devAgent.device
		if dev.SerialNumber == device.GetSerialNumber() && dev.ParentId == device.GetOltDeviceId().Id {
			onuDeviceId = dev.Id
			return false
		}
		return true
	})
	if onuDeviceId != "" {
		return onuDeviceId, nil
	}
	return "", fmt.Errorf("ONU with serial %s not found under OLT %s", device.GetSerialNumber(), device.GetOltDeviceId().Id)
}

func (dMgr *Manager) checkIPExists(ctx context.Context, config *voltha.UpdateDevice) error {

	newType, newIP := utils.GetAddrTypeAndValue(config.Address)
	if newType == "" {
		return errInvalidAddr
	}

	for id := range dMgr.rootDevices {

		agent := dMgr.getDeviceAgent(ctx, id)
		if agent == nil || agent.device == nil {
			continue
		}

		existingType, existingIP := utils.GetAddrTypeAndValue(agent.device.Address)
		if existingType == "" || existingType != newType {
			continue
		}

		if existingIP == newIP {

			// Same device → no change
			if agent.device.Id == config.Id {
				logger.Debugw(ctx, "no-change-in-device-address", log.Fields{"device-id": agent.device.Id})
				return errNoAddrChange
			}

			// Another root device → duplicate
			logger.Debugw(ctx, "ip-address-already-used-by-another-device", log.Fields{"device-id": agent.device.Id, "type": newType, "address": newIP})
			return errAddrDuplicate
		}
	}

	return nil
}
