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
	"errors"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/device/event"
	"github.com/opencord/voltha-go/rw_core/core/device/remote"
	"github.com/opencord/voltha-go/rw_core/core/device/state"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v4/pkg/events"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/common"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Manager represent device manager attributes
type Manager struct {
	deviceAgents      sync.Map
	rootDevices       map[string]bool
	lockRootDeviceMap sync.RWMutex
	adapterProxy      *remote.AdapterProxy
	*event.Agent
	adapterMgr              *adapter.Manager
	logicalDeviceMgr        *LogicalManager
	kafkaICProxy            kafka.InterContainerProxy
	stateTransitions        *state.TransitionMap
	dbPath                  *model.Path
	dProxy                  *model.Proxy
	coreInstanceID          string
	defaultTimeout          time.Duration
	devicesLoadingLock      sync.RWMutex
	deviceLoadingInProgress map[string][]chan int
}

//NewManagers creates the Manager and the Logical Manager.
func NewManagers(dbPath *model.Path, adapterMgr *adapter.Manager, kmp kafka.InterContainerProxy, endpointMgr kafka.EndpointManager, coreTopic, coreInstanceID string, defaultCoreTimeout time.Duration, eventProxy *events.EventProxy, stackID string) (*Manager, *LogicalManager) {
	deviceMgr := &Manager{
		rootDevices:             make(map[string]bool),
		kafkaICProxy:            kmp,
		adapterProxy:            remote.NewAdapterProxy(kmp, coreTopic, endpointMgr),
		coreInstanceID:          coreInstanceID,
		dbPath:                  dbPath,
		dProxy:                  dbPath.Proxy("devices"),
		adapterMgr:              adapterMgr,
		defaultTimeout:          defaultCoreTimeout,
		Agent:                   event.NewAgent(eventProxy, coreInstanceID, stackID),
		deviceLoadingInProgress: make(map[string][]chan int),
	}
	deviceMgr.stateTransitions = state.NewTransitionMap(deviceMgr)

	logicalDeviceMgr := &LogicalManager{
		Manager:                        event.NewManager(eventProxy, coreInstanceID, stackID),
		deviceMgr:                      deviceMgr,
		kafkaICProxy:                   kmp,
		dbPath:                         dbPath,
		ldProxy:                        dbPath.Proxy("logical_devices"),
		defaultTimeout:                 defaultCoreTimeout,
		logicalDeviceLoadingInProgress: make(map[string][]chan int),
	}
	deviceMgr.logicalDeviceMgr = logicalDeviceMgr

	adapterMgr.SetAdapterRestartedCallback(deviceMgr.adapterRestarted)

	return deviceMgr, logicalDeviceMgr
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
	//TODO: Change the return params to return an error as well
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

// CreateDevice creates a new parent device in the data model
func (dMgr *Manager) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	if device.MacAddress == "" && device.GetHostAndPort() == "" {
		logger.Errorf(ctx, "no-device-info-present")
		return &voltha.Device{}, errors.New("no-device-info-present; MAC or HOSTIP&PORT")
	}
	ctx = utils.WithRPCMetadataContext(ctx, "CreateDevice")
	logger.Debugw(ctx, "create-device", log.Fields{"device": *device})

	deviceExist, err := dMgr.isParentDeviceExist(ctx, device)
	if err != nil {
		logger.Errorf(ctx, "failed-to-fetch-parent-device-info")
		return nil, err
	}
	if deviceExist {
		logger.Errorf(ctx, "device-is-pre-provisioned-already-with-same-ip-port-or-mac-address")
		return nil, errors.New("device is already pre-provisioned")
	}
	logger.Debugw(ctx, "create-device", log.Fields{"device": device, "aproxy": dMgr.adapterProxy})

	// Ensure this device is set as root
	device.Root = true
	// Create and start a device agent for that device
	agent := newAgent(dMgr.adapterProxy, device, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.defaultTimeout)
	device, err = agent.start(ctx, device)
	if err != nil {
		logger.Errorw(ctx, "fail-to-start-device", log.Fields{"device-id": agent.deviceID, "error": err})
		return nil, err
	}
	dMgr.addDeviceAgentToMap(agent)
	return device, nil
}

// EnableDevice activates a device by invoking the adopt_device API on the appropriate adapter
func (dMgr *Manager) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "EnableDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "enable-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.enableDevice(ctx)
}

// DisableDevice disables a device along with any child device it may have
func (dMgr *Manager) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DisableDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "disable-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.disableDevice(ctx)
}

//RebootDevice invoked the reboot API to the corresponding adapter
func (dMgr *Manager) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "RebootDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "reboot-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.rebootDevice(ctx)
}

// DeleteDevice removes a device from the data model
func (dMgr *Manager) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DeleteDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "delete-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.deleteDevice(ctx)
}

// ForceDeleteDevice removes a device from the data model forcefully without successfully waiting for the adapters.
func (dMgr *Manager) ForceDeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ForceDeleteDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "force-delete-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.deleteDeviceForce(ctx)
}

// GetDevicePort returns the port details for a specific device port entry
func (dMgr *Manager) GetDevicePort(ctx context.Context, deviceID string, portID uint32) (*voltha.Port, error) {
	logger.Debugw(ctx, "get-device-port", log.Fields{"device-id": deviceID})
	agent := dMgr.getDeviceAgent(ctx, deviceID)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", deviceID)
	}
	return agent.getDevicePort(portID)
}

// ListDevicePorts returns the ports details for a specific device entry
func (dMgr *Manager) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDevicePorts")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "list-device-ports", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", id.Id)
	}

	ports := agent.listDevicePorts()
	ctr, ret := 0, make([]*voltha.Port, len(ports))
	for _, port := range ports {
		ret[ctr] = port
		ctr++
	}
	return &voltha.Ports{Items: ret}, nil
}

// ListDeviceFlows returns the flow details for a specific device entry
func (dMgr *Manager) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*ofp.Flows, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDeviceFlows")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "list-device-flows", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", id.Id)
	}

	flows := agent.listDeviceFlows()
	ctr, ret := 0, make([]*ofp.OfpFlowStats, len(flows))
	for _, flow := range flows {
		ret[ctr] = flow
		ctr++
	}
	return &openflow_13.Flows{Items: ret}, nil
}

// ListDeviceFlowGroups returns the flow group details for a specific device entry
func (dMgr *Manager) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*voltha.FlowGroups, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDeviceFlowGroups")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "list-device-flow-groups", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", id.Id)
	}
	groups := agent.listDeviceGroups()
	ctr, ret := 0, make([]*openflow_13.OfpGroupEntry, len(groups))
	for _, group := range groups {
		ret[ctr] = group
		ctr++
	}
	return &voltha.FlowGroups{Items: ret}, nil
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

// RunPostDeviceDelete removes any reference of this device
func (dMgr *Manager) RunPostDeviceDelete(ctx context.Context, cDevice *voltha.Device) error {
	logger.Infow(ctx, "run-post-device-delete", log.Fields{"device-id": cDevice.Id})
	dMgr.stopManagingDevice(ctx, cDevice.Id)
	return nil
}

// GetDevice exists primarily to implement the gRPC interface.
// Internal functions should call getDeviceReadOnly instead.
func (dMgr *Manager) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	return dMgr.getDeviceReadOnly(ctx, id.Id)
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

// GetChildDevice will return a device, either from memory or from the dB, if present
func (dMgr *Manager) GetChildDevice(ctx context.Context, parentDeviceID string, serialNumber string, onuID int64, parentPortNo int64) (*voltha.Device, error) {
	logger.Debugw(ctx, "get-child-device", log.Fields{"parent-device-id": parentDeviceID, "serialNumber": serialNumber,
		"parent-port-no": parentPortNo, "onu-id": onuID})

	parentDevicePorts, err := dMgr.listDevicePorts(ctx, parentDeviceID)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	childDeviceIds := dMgr.getAllChildDeviceIds(ctx, parentDevicePorts)
	if len(childDeviceIds) == 0 {
		logger.Debugw(ctx, "no-child-devices", log.Fields{"parent-device-id": parentDeviceID, "serial-number": serialNumber, "onu-id": onuID})
		return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
	}

	var foundChildDevice *voltha.Device
	for childDeviceID := range childDeviceIds {
		var found bool
		if searchDevice, err := dMgr.getDeviceReadOnly(ctx, childDeviceID); err == nil {

			foundOnuID := false
			if searchDevice.ProxyAddress.OnuId == uint32(onuID) {
				if searchDevice.ParentPortNo == uint32(parentPortNo) {
					logger.Debugw(ctx, "found-child-by-onuid", log.Fields{"parent-device-id": parentDeviceID, "onu-id": onuID})
					foundOnuID = true
				}
			}

			foundSerialNumber := false
			if searchDevice.SerialNumber == serialNumber {
				logger.Debugw(ctx, "found-child-by-serial-number", log.Fields{"parent-device-id": parentDeviceID, "serial-number": serialNumber})
				foundSerialNumber = true
			}

			// if both onuId and serialNumber are provided both must be true for the device to be found
			// otherwise whichever one found a match is good enough
			if onuID > 0 && serialNumber != "" {
				found = foundOnuID && foundSerialNumber
			} else {
				found = foundOnuID || foundSerialNumber
			}

			if found {
				foundChildDevice = searchDevice
				break
			}
		}
	}

	if foundChildDevice != nil {
		logger.Debugw(ctx, "child-device-found", log.Fields{"parent-device-id": parentDeviceID, "found-child-device": foundChildDevice})
		return foundChildDevice, nil
	}

	logger.Debugw(ctx, "child-device-not-found", log.Fields{"parent-device-id": parentDeviceID,
		"serial-number": serialNumber, "onu-id": onuID, "parent-port-no": parentPortNo})
	return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
}

// GetChildDeviceWithProxyAddress will return a device based on proxy address
func (dMgr *Manager) GetChildDeviceWithProxyAddress(ctx context.Context, proxyAddress *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	logger.Debugw(ctx, "get-child-device-with-proxy-address", log.Fields{"proxy-address": proxyAddress})

	parentDevicePorts, err := dMgr.listDevicePorts(ctx, proxyAddress.DeviceId)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	childDeviceIds := dMgr.getAllChildDeviceIds(ctx, parentDevicePorts)
	if len(childDeviceIds) == 0 {
		logger.Debugw(ctx, "no-child-devices", log.Fields{"parent-device-id": proxyAddress.DeviceId})
		return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
	}

	var foundChildDevice *voltha.Device
	for childDeviceID := range childDeviceIds {
		if searchDevice, err := dMgr.getDeviceReadOnly(ctx, childDeviceID); err == nil {
			if searchDevice.ProxyAddress == proxyAddress {
				foundChildDevice = searchDevice
				break
			}
		}
	}

	if foundChildDevice != nil {
		logger.Debugw(ctx, "child-device-found", log.Fields{"proxy-address": proxyAddress})
		return foundChildDevice, nil
	}

	logger.Warnw(ctx, "child-device-not-found", log.Fields{"proxy-address": proxyAddress})
	return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
}

// IsDeviceInCache returns true if device is found in the map
func (dMgr *Manager) IsDeviceInCache(id string) bool {
	_, exist := dMgr.deviceAgents.Load(id)
	return exist
}

// ListDevices retrieves the latest devices from the data model
func (dMgr *Manager) ListDevices(ctx context.Context, _ *empty.Empty) (*voltha.Devices, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDevices")
	logger.Debug(ctx, "list-devices")
	result := &voltha.Devices{}

	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		result.Items = append(result.Items, value.(*Agent).device)
		return true
	})

	logger.Debugw(ctx, "list-devices-end", log.Fields{"len": len(result.Items)})
	return result, nil
}

//isParentDeviceExist checks whether device is already preprovisioned.
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

//getDeviceFromModelretrieves the device data from the model.
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
				agent := newAgent(dMgr.adapterProxy, device, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.defaultTimeout)
				if _, err = agent.start(ctx, nil); err != nil {
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
		//	Wait for the channel to be closed, implying the process loading this device is done.
		<-ch
	}
	if agent, ok := dMgr.deviceAgents.Load(deviceID); ok {
		return agent.(*Agent), nil
	}
	return nil, status.Errorf(codes.Aborted, "Error loading device %s", deviceID)
}

// loadRootDeviceParentAndChildren loads the children and parents of a root device in memory
func (dMgr *Manager) loadRootDeviceParentAndChildren(ctx context.Context, device *voltha.Device, devicePorts map[uint32]*voltha.Port) error {
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
		childDeviceIds := dMgr.getAllChildDeviceIds(ctx, devicePorts)
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
		devicePorts := dAgent.listDevicePorts()

		// Load all children as well as the parent of this device (logical_device)
		if err := dMgr.loadRootDeviceParentAndChildren(ctx, device, devicePorts); err != nil {
			logger.Warnw(ctx, "failure-loading-device-parent-and-children", log.Fields{"device-id": deviceID})
			return err
		}
		logger.Debugw(ctx, "successfully-loaded-parent-and-children", log.Fields{"device-id": deviceID})
	} else {
		//	Scenario B - use the parentId of that device (root device) to trigger the loading
		if device.ParentId != "" {
			return dMgr.load(ctx, device.ParentId)
		}
	}
	return nil
}

// ListDeviceIds retrieves the latest device IDs information from the data model (memory data only)
func (dMgr *Manager) ListDeviceIds(ctx context.Context, _ *empty.Empty) (*voltha.IDs, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDeviceIds")
	logger.Debug(ctx, "list-device-ids")
	// Report only device IDs that are in the device agent map
	return dMgr.listDeviceIdsFromMap(), nil
}

// ReconcileDevices is a request to a voltha core to update its list of managed devices.  This will
// trigger loading the devices along with their children and parent in memory
func (dMgr *Manager) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ReconcileDevices")
	logger.Debugw(ctx, "reconcile-devices", log.Fields{"num-devices": len(ids.Items)})
	if ids != nil && len(ids.Items) != 0 {
		toReconcile := len(ids.Items)
		reconciled := 0
		var err error
		for _, id := range ids.Items {
			if err = dMgr.load(ctx, id.Id); err != nil {
				logger.Warnw(ctx, "failure-reconciling-device", log.Fields{"device-id": id.Id, "error": err})
			} else {
				reconciled++
			}
		}
		if toReconcile != reconciled {
			return nil, status.Errorf(codes.DataLoss, "less-device-reconciled-than-requested:%d/%d", reconciled, toReconcile)
		}
	} else {
		return nil, status.Errorf(codes.InvalidArgument, "empty-list-of-ids")
	}
	return &empty.Empty{}, nil
}

// isOkToReconcile validates whether a device is in the correct status to be reconciled
func (dMgr *Manager) isOkToReconcile(ctx context.Context, device *voltha.Device) bool {
	if device == nil {
		return false
	}
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		return device.AdminState != voltha.AdminState_PREPROVISIONED && (!agent.isDeletionInProgress())
	}
	return false
}

// adapterRestarted is invoked whenever an adapter is restarted
func (dMgr *Manager) adapterRestarted(ctx context.Context, adapter *voltha.Adapter) error {
	logger.Debugw(ctx, "adapter-restarted", log.Fields{"adapter-id": adapter.Id, "vendor": adapter.Vendor,
		"current-replica": adapter.CurrentReplica, "total-replicas": adapter.TotalReplicas, "endpoint": adapter.Endpoint})

	// Let's reconcile the device managed by this Core only
	if len(dMgr.rootDevices) == 0 {
		logger.Debugw(ctx, "nothing-to-reconcile", log.Fields{"adapter-id": adapter.Id})
		return nil
	}

	responses := make([]utils.Response, 0)
	for rootDeviceID := range dMgr.rootDevices {
		dAgent := dMgr.getDeviceAgent(ctx, rootDeviceID)
		if dAgent == nil {
			continue
		}
		logger.Debugw(ctx, "checking-adapter-type", log.Fields{"agentType": dAgent.deviceType, "adapter-type": adapter.Type})
		if dAgent.deviceType == adapter.Type {
			rootDevice, _ := dAgent.getDeviceReadOnly(ctx)
			if rootDevice == nil {
				continue
			}
			isDeviceOwnedByService, err := dMgr.adapterProxy.IsDeviceOwnedByService(ctx, rootDeviceID, adapter.Type, adapter.CurrentReplica)
			if err != nil {
				logger.Warnw(ctx, "is-device-owned-by-service", log.Fields{"error": err, "root-device-id": rootDeviceID, "adapter-type": adapter.Type, "replica-number": adapter.CurrentReplica})
				continue
			}
			if isDeviceOwnedByService {
				if dMgr.isOkToReconcile(ctx, rootDevice) {
					logger.Debugw(ctx, "reconciling-root-device", log.Fields{"rootId": rootDevice.Id})
					responses = append(responses, dMgr.sendReconcileDeviceRequest(ctx, rootDevice))
				} else {
					logger.Debugw(ctx, "not-reconciling-root-device", log.Fields{"rootId": rootDevice.Id, "state": rootDevice.AdminState})
				}
			} else { // Should we be reconciling the root's children instead?
				rootDevicePorts, _ := dMgr.listDevicePorts(ctx, rootDeviceID)
			childManagedByAdapter:
				for _, port := range rootDevicePorts {
					for _, peer := range port.Peers {
						if childDevice, _ := dMgr.getDeviceFromModel(ctx, peer.DeviceId); childDevice != nil {
							isDeviceOwnedByService, err := dMgr.adapterProxy.IsDeviceOwnedByService(ctx, childDevice.Id, adapter.Type, adapter.CurrentReplica)
							if err != nil {
								logger.Warnw(ctx, "is-device-owned-by-service", log.Fields{"error": err, "child-device-id": childDevice.Id, "adapter-type": adapter.Type, "replica-number": adapter.CurrentReplica})
							}
							if isDeviceOwnedByService {
								if dMgr.isOkToReconcile(ctx, childDevice) {
									logger.Debugw(ctx, "reconciling-child-device", log.Fields{"child-device-id": childDevice.Id})
									responses = append(responses, dMgr.sendReconcileDeviceRequest(ctx, childDevice))
								} else {
									logger.Debugw(ctx, "not-reconciling-child-device", log.Fields{"child-device-id": childDevice.Id, "state": childDevice.AdminState})
								}
							} else {
								// All child devices under a parent device are typically managed by the same adapter type.
								// Therefore we only need to check whether the first device we retrieved is managed by that adapter
								break childManagedByAdapter
							}
						}
					}
				}
			}
		}
	}
	if len(responses) > 0 {
		// Wait for completion
		if res := utils.WaitForNilOrErrorResponses(dMgr.defaultTimeout, responses...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	} else {
		logger.Debugw(ctx, "no-managed-device-to-reconcile", log.Fields{"adapter-id": adapter.Id})
	}
	return nil
}

func (dMgr *Manager) sendReconcileDeviceRequest(ctx context.Context, device *voltha.Device) utils.Response {
	// Send a reconcile request to the adapter. Since this Core may not be managing this device then there is no
	// point of creating a device agent (if the device is not being managed by this Core) before sending the request
	// to the adapter.   We will therefore bypass the adapter adapter and send the request directly to the adapter via
	// the adapter proxy.
	response := utils.NewResponse()
	ch, err := dMgr.adapterProxy.ReconcileDevice(ctx, device)
	if err != nil {
		response.Error(err)
	}
	// Wait for adapter response in its own routine
	go func() {
		resp, ok := <-ch
		if !ok {
			response.Error(status.Errorf(codes.Aborted, "channel-closed-device: %s", device.Id))
		} else if resp.Err != nil {
			response.Error(resp.Err)
		}
		response.Done()
	}()
	return response
}

func (dMgr *Manager) ReconcileChildDevices(ctx context.Context, parentDeviceID string) error {
	if parentDevicePorts, err := dMgr.listDevicePorts(ctx, parentDeviceID); err == nil {
		responses := make([]utils.Response, 0)
		for _, port := range parentDevicePorts {
			for _, peer := range port.Peers {
				if childDevice, err := dMgr.getDeviceFromModel(ctx, peer.DeviceId); err == nil {
					responses = append(responses, dMgr.sendReconcileDeviceRequest(ctx, childDevice))
				}
			}
		}
		// Wait for completion
		if res := utils.WaitForNilOrErrorResponses(dMgr.defaultTimeout, responses...); res != nil {
			return status.Errorf(codes.Aborted, "errors-%s", res)
		}
	}
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
		//	Setup peer ports in its own routine
		go func() {
			subCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)
			if err := dMgr.addPeerPort(subCtx, deviceID, port); err != nil {
				logger.Errorw(ctx, "unable-to-add-peer-port", log.Fields{"error": err, "device-id": deviceID})
			}
		}()
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) addFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	logger.Debugw(ctx, "add-flows-and-groups", log.Fields{"device-id": deviceID, "groups:": groups, "flow-metadata": flowMetadata})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.addFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// deleteParentFlows removes flows from the parent device based on  specific attributes
func (dMgr *Manager) deleteParentFlows(ctx context.Context, deviceID string, uniPort uint32, metadata *voltha.FlowMetadata) error {
	logger.Debugw(ctx, "delete-parent-flows", log.Fields{"device-id": deviceID, "uni-port": uniPort, "metadata": metadata})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if !agent.isRootDevice {
			return status.Errorf(codes.FailedPrecondition, "not-a-parent-device-%s", deviceID)
		}
		return agent.filterOutFlows(ctx, uniPort, metadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) deleteFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	logger.Debugw(ctx, "delete-flows-and-groups", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.deleteFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) updateFlowsAndGroups(ctx context.Context, deviceID string, flows []*ofp.OfpFlowStats, groups []*ofp.OfpGroupEntry, flowMetadata *voltha.FlowMetadata) error {
	logger.Debugw(ctx, "update-flows-and-groups", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateFlowsAndGroups(ctx, flows, groups, flowMetadata)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

// UpdateDevicePmConfigs updates the PM configs.  This is executed when the northbound gRPC API is invoked, typically
// following a user action
func (dMgr *Manager) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "UpdateDevicePmConfigs")
	log.EnrichSpan(ctx, log.Fields{"device-id": configs.Id})
	if configs.Id == "" {
		return nil, status.Error(codes.FailedPrecondition, "invalid-device-Id")
	}
	agent := dMgr.getDeviceAgent(ctx, configs.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", configs.Id)
	}
	return &empty.Empty{}, agent.updatePmConfigs(ctx, configs)
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

// ListDevicePmConfigs returns pm configs of device
func (dMgr *Manager) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDevicePmConfigs")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return agent.listPmConfigs(ctx)
}

func (dMgr *Manager) getSwitchCapability(ctx context.Context, deviceID string) (*ic.SwitchCapability, error) {
	logger.Debugw(ctx, "get-switch-capability", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.getSwitchCapability(ctx)
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) GetPorts(ctx context.Context, deviceID string, portType voltha.Port_PortType) (*voltha.Ports, error) {
	logger.Debugw(ctx, "get-ports", log.Fields{"device-id": deviceID, "port-type": portType})
	agent := dMgr.getDeviceAgent(ctx, deviceID)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", deviceID)
	}
	return agent.getPorts(ctx, portType), nil
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
	parentDevicePorts, err := dMgr.listDevicePorts(ctx, deviceID)
	if err != nil {
		return status.Errorf(codes.Aborted, "%s", err.Error())
	}
	for childDeviceID := range dMgr.getAllChildDeviceIds(ctx, parentDevicePorts) {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err = agent.updateDeviceStatus(ctx, operStatus, connStatus); err != nil {
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
			go func() {
				subCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)
				err := dMgr.logicalDeviceMgr.updatePortState(subCtx, deviceID, portNo, operStatus)
				if err != nil {
					// While we want to handle (catch) and log when
					// an update to a port was not able to be
					// propagated to the logical port, we can report
					// it as a warning and not an error because it
					// doesn't stop or modify processing.
					// TODO: VOL-2707
					logger.Warnw(ctx, "unable-to-update-logical-port-state", log.Fields{"error": err})
				}
			}()
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) DeleteAllPorts(ctx context.Context, deviceID string) error {
	logger.Debugw(ctx, "delete-all-ports", log.Fields{"device-id": deviceID})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if err := agent.deleteAllPorts(ctx); err != nil {
			return err
		}
		// Notify the logical device manager to remove all logical ports, if needed.
		// At this stage the device itself may gave been deleted already at a DeleteAllPorts
		// typically is part of a device deletion phase.
		if device, err := dMgr.getDeviceReadOnly(ctx, deviceID); err == nil {
			go func() {
				subCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)
				if err := dMgr.logicalDeviceMgr.deleteAllLogicalPorts(subCtx, device); err != nil {
					logger.Errorw(ctx, "unable-to-delete-logical-ports", log.Fields{"error": err})
				}
			}()
		} else {
			logger.Warnw(ctx, "failed-to-retrieve-device", log.Fields{"device-id": deviceID})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

//UpdatePortsState updates all ports on the device
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

func (dMgr *Manager) ChildDeviceDetected(ctx context.Context, parentDeviceID string, parentPortNo int64, deviceType string,
	channelID int64, vendorID string, serialNumber string, onuID int64) (*voltha.Device, error) {
	logger.Debugw(ctx, "child-device-detected", log.Fields{"parent-device-id": parentDeviceID, "parent-port-no": parentPortNo, "device-type": deviceType, "channel-id": channelID, "vendor-id": vendorID, "serial-number": serialNumber, "onu-id": onuID})

	if deviceType == "" && vendorID != "" {
		logger.Debug(ctx, "device-type-is-nil-fetching-device-type")
		deviceTypes, err := dMgr.adapterMgr.ListDeviceTypes(ctx, nil)
		if err != nil {
			return nil, err
		}
	OLoop:
		for _, dType := range deviceTypes.Items {
			for _, v := range dType.VendorIds {
				if v == vendorID {
					deviceType = dType.Adapter
					break OLoop
				}
			}
		}
	}
	//if no match found for the vendorid,report adapter with the custom error message
	if deviceType == "" {
		logger.Errorw(ctx, "failed-to-fetch-adapter-name ", log.Fields{"vendor-id": vendorID})
		return nil, status.Errorf(codes.NotFound, "%s", vendorID)
	}

	// Create the ONU device
	childDevice := &voltha.Device{}
	childDevice.Type = deviceType
	childDevice.ParentId = parentDeviceID
	childDevice.ParentPortNo = uint32(parentPortNo)
	childDevice.VendorId = vendorID
	childDevice.SerialNumber = serialNumber
	childDevice.Root = false

	// Get parent device type
	pAgent := dMgr.getDeviceAgent(ctx, parentDeviceID)
	if pAgent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
	}
	if pAgent.deviceType == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "device Type not set %s", parentDeviceID)
	}

	if device, err := dMgr.GetChildDevice(ctx, parentDeviceID, serialNumber, onuID, parentPortNo); err == nil {
		logger.Warnw(ctx, "child-device-exists", log.Fields{"parent-device-id": parentDeviceID, "serial-number": serialNumber})
		return device, status.Errorf(codes.AlreadyExists, "%s", serialNumber)
	}

	childDevice.ProxyAddress = &voltha.Device_ProxyAddress{DeviceId: parentDeviceID, DeviceType: pAgent.deviceType, ChannelId: uint32(channelID), OnuId: uint32(onuID)}

	// Create and start a device agent for that device
	agent := newAgent(dMgr.adapterProxy, childDevice, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.defaultTimeout)
	insertedChildDevice, err := agent.start(ctx, childDevice)
	if err != nil {
		logger.Errorw(ctx, "error-starting-child-device", log.Fields{"parent-device-id": childDevice.ParentId, "child-device-id": agent.deviceID, "error": err})
		return nil, err
	}
	dMgr.addDeviceAgentToMap(agent)

	// Activate the child device
	if agent = dMgr.getDeviceAgent(ctx, agent.deviceID); agent != nil {
		go func() {
			subCtx := utils.WithFromTopicMetadataFromContext(utils.WithSpanAndRPCMetadataFromContext(ctx), ctx)
			err := agent.enableDevice(subCtx)
			if err != nil {
				logger.Errorw(ctx, "unable-to-enable-device", log.Fields{"error": err})
			}
		}()
	}

	return insertedChildDevice, nil
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

	if err := dMgr.logicalDeviceMgr.packetIn(ctx, device.ParentId, port, transactionID, packet); err != nil {
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

//
//CreateLogicalDevice creates logical device in core
func (dMgr *Manager) CreateLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info(ctx, "create-logical-device")
	// Verify whether the logical device has already been created
	if cDevice.ParentId != "" {
		logger.Debugw(ctx, "parent-device-already-exist", log.Fields{"device-id": cDevice.Id, "logical-device-id": cDevice.Id})
		return nil
	}
	var err error
	if _, err = dMgr.logicalDeviceMgr.createLogicalDevice(ctx, cDevice); err != nil {
		logger.Warnw(ctx, "create-logical-device-error", log.Fields{"device": cDevice})
		return err
	}
	return nil
}

// DeleteLogicalDevice deletes logical device from core
func (dMgr *Manager) DeleteLogicalDevice(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info(ctx, "delete-logical-device")
	var err error
	if err = dMgr.logicalDeviceMgr.deleteLogicalDevice(ctx, cDevice); err != nil {
		logger.Warnw(ctx, "delete-logical-device-error", log.Fields{"device-id": cDevice.Id})
		return err
	}
	// Remove the logical device Id from the parent device
	logicalID := ""
	dMgr.UpdateDeviceAttribute(ctx, cDevice.Id, "ParentId", logicalID)
	return nil
}

// DeleteLogicalPorts removes the logical ports associated with that deviceId
func (dMgr *Manager) DeleteLogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-logical-ports", log.Fields{"device-id": cDevice.Id})
	if err := dMgr.logicalDeviceMgr.deleteLogicalPorts(ctx, cDevice.Id); err != nil {
		// Just log the error.   The logical device or port may already have been deleted before this callback is invoked.
		logger.Warnw(ctx, "delete-logical-ports-error", log.Fields{"device-id": cDevice.Id, "error": err})
	}
	return nil
}

func (dMgr *Manager) getParentDevice(ctx context.Context, childDevice *voltha.Device) *voltha.Device {
	//	Sanity check
	if childDevice.Root {
		// childDevice is the parent device
		return childDevice
	}
	parentDevice, _ := dMgr.getDeviceReadOnly(ctx, childDevice.ParentId)
	return parentDevice
}

//ChildDevicesLost is invoked by an adapter to indicate that a parent device is in a state (Disabled) where it
//cannot manage the child devices.  This will trigger the Core to disable all the child devices.
func (dMgr *Manager) ChildDevicesLost(ctx context.Context, parentDeviceID string) error {
	logger.Debug(ctx, "child-devices-lost")
	parentDevice, err := dMgr.getDeviceReadOnly(ctx, parentDeviceID)
	if err != nil {
		logger.Warnw(ctx, "failed-getting-device", log.Fields{"parent-device-id": parentDeviceID, "error": err})
		return err
	}
	return dMgr.DisableAllChildDevices(ctx, parentDevice)
}

//ChildDevicesDetected is invoked by an adapter when child devices are found, typically after after a
// disable/enable sequence.  This will trigger the Core to Enable all the child devices of that parent.
func (dMgr *Manager) ChildDevicesDetected(ctx context.Context, parentDeviceID string) error {
	logger.Debug(ctx, "child-devices-detected")
	parentDevicePorts, err := dMgr.listDevicePorts(ctx, parentDeviceID)
	if err != nil {
		logger.Warnw(ctx, "failed-getting-device", log.Fields{"device-id": parentDeviceID, "error": err})
		return err
	}
	childDeviceIds := dMgr.getAllChildDeviceIds(ctx, parentDevicePorts)
	if len(childDeviceIds) == 0 {
		logger.Debugw(ctx, "no-child-device", log.Fields{"parent-device-id": parentDeviceID})
	}
	allChildEnableRequestSent := true
	for childDeviceID := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			subCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)
			// Run the children re-registration in its own routine
			go func(ctx context.Context) {
				err = agent.enableDevice(ctx)
				if err != nil {
					logger.Errorw(ctx, "unable-to-enable-device", log.Fields{"error": err})
				}
			}(subCtx)
		} else {
			err = status.Errorf(codes.Unavailable, "no agent for child device %s", childDeviceID)
			logger.Errorw(ctx, "no-child-device-agent", log.Fields{"parent-device-id": parentDeviceID, "child-id": childDeviceID})
			allChildEnableRequestSent = false
		}
	}
	if !allChildEnableRequestSent {
		return err
	}
	return nil
}

/*
All the functions below are callback functions where they are invoked with the latest and previous data.  We can
therefore use the data as is without trying to get the latest from the model.
*/

//DisableAllChildDevices is invoked as a callback when the parent device is disabled
func (dMgr *Manager) DisableAllChildDevices(ctx context.Context, parentCurrDevice *voltha.Device) error {
	logger.Debug(ctx, "disable-all-child-devices")
	ports, _ := dMgr.listDevicePorts(ctx, parentCurrDevice.Id)
	for childDeviceID := range dMgr.getAllChildDeviceIds(ctx, ports) {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err := agent.disableDevice(ctx); err != nil {
				// Just log the error - this error happens only if the child device was already in deleted state.
				logger.Errorw(ctx, "failure-disable-device", log.Fields{"device-id": childDeviceID, "error": err.Error()})
			}
		}
	}
	return nil
}

//DeleteAllChildDevices is invoked as a callback when the parent device is deleted
func (dMgr *Manager) DeleteAllChildDevices(ctx context.Context, parentCurrDevice *voltha.Device) error {
	logger.Debug(ctx, "delete-all-child-devices")
	force := false
	// Get the parent device Transient state, if its FORCE_DELETED(go for force delete for child devices)
	// So in cases when this handler is getting called other than DELETE operation, no force option would be used.
	agent := dMgr.getDeviceAgent(ctx, parentCurrDevice.Id)
	if agent == nil {
		return status.Errorf(codes.NotFound, "%s", parentCurrDevice.Id)
	}

	force = agent.getTransientState() == voltha.DeviceTransientState_FORCE_DELETING

	ports, _ := dMgr.listDevicePorts(ctx, parentCurrDevice.Id)
	for childDeviceID := range dMgr.getAllChildDeviceIds(ctx, ports) {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if force {
				if err := agent.deleteDeviceForce(ctx); err != nil {
					logger.Warnw(ctx, "failure-delete-device-force", log.Fields{"device-id": childDeviceID,
						"error": err.Error()})
				}
			} else {
				if err := agent.deleteDevice(ctx); err != nil {
					logger.Warnw(ctx, "failure-delete-device", log.Fields{"device-id": childDeviceID,
						"error": err.Error()})
				}
			}
			// No further action is required here.  The deleteDevice will change the device state where the resulting
			// callback will take care of cleaning the child device agent.
		}
	}
	return nil
}

//DeleteAllLogicalPorts is invoked as a callback when the parent device's connection status moves to UNREACHABLE
func (dMgr *Manager) DeleteAllLogicalPorts(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-logical-ports", log.Fields{"parent-device-id": parentDevice.Id})
	if err := dMgr.logicalDeviceMgr.deleteAllLogicalPorts(ctx, parentDevice); err != nil {
		// Just log error as logical device may already have been deleted
		logger.Warnw(ctx, "delete-all-logical-ports-fail", log.Fields{"parent-device-id": parentDevice.Id, "error": err})
	}
	return nil
}

//DeleteAllDeviceFlows is invoked as a callback when the parent device's connection status moves to UNREACHABLE
func (dMgr *Manager) DeleteAllDeviceFlows(ctx context.Context, parentDevice *voltha.Device) error {
	logger.Debugw(ctx, "delete-all-device-flows", log.Fields{"parent-device-id": parentDevice.Id})
	if agent := dMgr.getDeviceAgent(ctx, parentDevice.Id); agent != nil {
		if err := agent.deleteAllFlows(ctx); err != nil {
			logger.Errorw(ctx, "error-deleting-all-device-flows", log.Fields{"parent-device-id": parentDevice.Id})
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "%s", parentDevice.Id)
}

//getAllChildDeviceIds is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *Manager) getAllChildDeviceIds(ctx context.Context, parentDevicePorts map[uint32]*voltha.Port) map[string]struct{} {
	logger.Debug(ctx, "get-all-child-device-ids")
	childDeviceIds := make(map[string]struct{}, len(parentDevicePorts))
	for _, port := range parentDevicePorts {
		for _, peer := range port.Peers {
			childDeviceIds[peer.DeviceId] = struct{}{}
		}
	}
	logger.Debugw(ctx, "returning-getAllChildDeviceIds", log.Fields{"childDeviceIds": childDeviceIds})
	return childDeviceIds
}

//GetAllChildDevices is a helper method to get all the child device IDs from the device passed as parameter
func (dMgr *Manager) GetAllChildDevices(ctx context.Context, parentDeviceID string) (*voltha.Devices, error) {
	logger.Debugw(ctx, "get-all-child-devices", log.Fields{"parent-device-id": parentDeviceID})
	if parentDevicePorts, err := dMgr.listDevicePorts(ctx, parentDeviceID); err == nil {
		childDevices := make([]*voltha.Device, 0)
		for deviceID := range dMgr.getAllChildDeviceIds(ctx, parentDevicePorts) {
			if d, e := dMgr.getDeviceReadOnly(ctx, deviceID); e == nil && d != nil {
				childDevices = append(childDevices, d)
			}
		}
		return &voltha.Devices{Items: childDevices}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", parentDeviceID)
}

// SetupUNILogicalPorts creates UNI ports on the logical device that represents a child UNI interface
func (dMgr *Manager) SetupUNILogicalPorts(ctx context.Context, cDevice *voltha.Device) error {
	logger.Info(ctx, "setup-uni-logical-ports")
	cDevicePorts, err := dMgr.listDevicePorts(ctx, cDevice.Id)
	if err != nil {
		return err
	}
	if err := dMgr.logicalDeviceMgr.setupUNILogicalPorts(ctx, cDevice, cDevicePorts); err != nil {
		logger.Warnw(ctx, "setup-uni-logical-ports-error", log.Fields{"device": cDevice, "err": err})
		return err
	}
	return nil
}

// convenience to avoid redefining
var operationFailureResp = &common.OperationResp{Code: voltha.OperationResp_OPERATION_FAILURE}

// DownloadImage execute an image download request
func (dMgr *Manager) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DownloadImage")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "download-image", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.downloadImage(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// CancelImageDownload cancels image download request
func (dMgr *Manager) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "CancelImageDownload")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "cancel-image-download", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.cancelImageDownload(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// ActivateImageUpdate activates image update request
func (dMgr *Manager) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ActivateImageUpdate")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "activate-image-update", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.activateImage(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// RevertImageUpdate reverts image update
func (dMgr *Manager) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "RevertImageUpdate")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "rever-image-update", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.revertImage(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// convenience to avoid redefining
var imageDownloadFailureResp = &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN}

// GetImageDownloadStatus returns status of image download
func (dMgr *Manager) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetImageDownloadStatus")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "get-image-download-status", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return imageDownloadFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.getImageDownloadStatus(ctx, img)
	if err != nil {
		return imageDownloadFailureResp, err
	}
	return resp, nil
}

func (dMgr *Manager) UpdateImageDownload(ctx context.Context, deviceID string, img *voltha.ImageDownload) error {
	ctx = utils.WithRPCMetadataContext(ctx, "UpdateImageDownload")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "update-image-download", log.Fields{"device-id": img.Id, "image-name": img.Name})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		if err := agent.updateImageDownload(ctx, img); err != nil {
			logger.Debugw(ctx, "update-image-download-failed", log.Fields{"err": err, "image-name": img.Name})
			return err
		}
	} else {
		return status.Errorf(codes.NotFound, "%s", img.Id)
	}
	return nil
}

// GetImageDownload returns image download
func (dMgr *Manager) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetImageDownload")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})
	logger.Debugw(ctx, "get-image-download", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return imageDownloadFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.getImageDownload(ctx, img)
	if err != nil {
		return imageDownloadFailureResp, err
	}
	return resp, nil
}

// ListImageDownloads returns image downloads
func (dMgr *Manager) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListImageDownloads")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "list-image-downloads", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return &voltha.ImageDownloads{Items: []*voltha.ImageDownload{imageDownloadFailureResp}}, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	resp, err := agent.listImageDownloads(ctx, id.Id)
	if err != nil {
		return &voltha.ImageDownloads{Items: []*voltha.ImageDownload{imageDownloadFailureResp}}, err
	}
	return resp, nil
}

// GetImages returns all images for a specific device entry
func (dMgr *Manager) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetImages")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "get-images", log.Fields{"device-id": id.Id})
	device, err := dMgr.getDeviceReadOnly(ctx, id.Id)
	if err != nil {
		return nil, err
	}
	return device.Images, nil
}

func (dMgr *Manager) NotifyInvalidTransition(ctx context.Context, device *voltha.Device) error {
	logger.Errorw(ctx, "notify-invalid-transition", log.Fields{
		"device":           device.Id,
		"curr-admin-state": device.AdminState,
		"curr-oper-state":  device.OperStatus,
		"curr-conn-state":  device.ConnectStatus,
	})
	//TODO: notify over kafka?
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

func (dMgr *Manager) SimulateAlarm(ctx context.Context, simulateReq *voltha.SimulateAlarmRequest) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "SimulateAlarm")
	logger.Debugw(ctx, "simulate-alarm", log.Fields{"id": simulateReq.Id, "indicator": simulateReq.Indicator, "intf-id": simulateReq.IntfId,
		"port-type-name": simulateReq.PortTypeName, "onu-device-id": simulateReq.OnuDeviceId, "inverse-bit-error-rate": simulateReq.InverseBitErrorRate,
		"drift": simulateReq.Drift, "new-eqd": simulateReq.NewEqd, "onu-serial-number": simulateReq.OnuSerialNumber, "operation": simulateReq.Operation})
	agent := dMgr.getDeviceAgent(ctx, simulateReq.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", simulateReq.Id)
	}
	if err := agent.simulateAlarm(ctx, simulateReq); err != nil {
		return nil, err
	}
	return &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}, nil
}

func (dMgr *Manager) UpdateDeviceReason(ctx context.Context, deviceID string, reason string) error {
	logger.Debugw(ctx, "update-device-reason", log.Fields{"device-id": deviceID, "reason": reason})
	if agent := dMgr.getDeviceAgent(ctx, deviceID); agent != nil {
		return agent.updateDeviceReason(ctx, reason)
	}
	return status.Errorf(codes.NotFound, "%s", deviceID)
}

func (dMgr *Manager) EnablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "EnablePort")
	log.EnrichSpan(ctx, log.Fields{"device-id": port.DeviceId})
	logger.Debugw(ctx, "enable-port", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	agent := dMgr.getDeviceAgent(ctx, port.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", port.DeviceId)
	}
	return &empty.Empty{}, agent.enablePort(ctx, port.PortNo)
}

func (dMgr *Manager) DisablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DisablePort")
	log.EnrichSpan(ctx, log.Fields{"device-id": port.DeviceId})
	logger.Debugw(ctx, "disable-port", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	agent := dMgr.getDeviceAgent(ctx, port.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", port.DeviceId)
	}
	return &empty.Empty{}, agent.disablePort(ctx, port.PortNo)
}

// ChildDeviceLost  calls parent adapter to delete child device and all its references
func (dMgr *Manager) ChildDeviceLost(ctx context.Context, curr *voltha.Device) error {
	logger.Debugw(ctx, "child-device-lost", log.Fields{"child-device-id": curr.Id, "parent-device-id": curr.ParentId})
	if parentAgent := dMgr.getDeviceAgent(ctx, curr.ParentId); parentAgent != nil {
		if err := parentAgent.ChildDeviceLost(ctx, curr); err != nil {
			// Just log the message and let the remaining pipeline proceed.
			logger.Warnw(ctx, "childDeviceLost", log.Fields{"child-device-id": curr.Id, "parent-device-id": curr.ParentId, "error": err})
		}
	}
	// Do not return an error as parent device may also have been deleted.  Let the remaining pipeline proceed.
	return nil
}

func (dMgr *Manager) StartOmciTestAction(ctx context.Context, request *voltha.OmciTestRequest) (*voltha.TestResponse, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "StartOmciTestAction")
	log.EnrichSpan(ctx, log.Fields{"device-id": request.Id})
	logger.Debugw(ctx, "start-omci-test-action", log.Fields{"device-id": request.Id, "uuid": request.Uuid})
	agent := dMgr.getDeviceAgent(ctx, request.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", request.Id)
	}
	return agent.startOmciTest(ctx, request)
}

func (dMgr *Manager) GetExtValue(ctx context.Context, value *voltha.ValueSpecifier) (*voltha.ReturnValues, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetExtValue")
	log.EnrichSpan(ctx, log.Fields{"device-id": value.Id})
	logger.Debugw(ctx, "get-ext-value", log.Fields{"onu-id": value.Id})
	cDevice, err := dMgr.getDeviceReadOnly(ctx, value.Id)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	pDevice, err := dMgr.getDeviceReadOnly(ctx, cDevice.ParentId)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if agent := dMgr.getDeviceAgent(ctx, cDevice.ParentId); agent != nil {
		resp, err := agent.getExtValue(ctx, pDevice, cDevice, value)
		if err != nil {
			return nil, err
		}
		logger.Debugw(ctx, "get-ext-value-result", log.Fields{"result": resp})
		return resp, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", value.Id)

}

// SetExtValue  set some given configs or value
func (dMgr *Manager) SetExtValue(ctx context.Context, value *voltha.ValueSet) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "SetExtValue")
	logger.Debugw(ctx, "set-ext-value", log.Fields{"onu-id": value.Id})
	device, err := dMgr.getDeviceReadOnly(ctx, value.Id)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		resp, err := agent.setExtValue(ctx, device, value)
		if err != nil {
			return nil, err
		}
		logger.Debugw(ctx, "set-ext-value-result", log.Fields{"result": resp})
		return resp, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", value.Id)

}

func (dMgr *Manager) SendRPCEvent(ctx context.Context, id string, rpcEvent *voltha.RPCEvent,
	category voltha.EventCategory_Types, subCategory *voltha.EventSubCategory_Types, raisedTs int64) {
	//TODO Instead of directly sending to the kafka bus, queue the message and send it asynchronously
	dMgr.Agent.SendRPCEvent(ctx, id, rpcEvent, category, subCategory, raisedTs)
}

func (dMgr *Manager) GetTransientState(ctx context.Context, id string) (voltha.DeviceTransientState_Types, error) {
	agent := dMgr.getDeviceAgent(ctx, id)
	if agent == nil {
		return voltha.DeviceTransientState_NONE, status.Errorf(codes.NotFound, "%s", id)
	}
	return agent.getTransientState(), nil
}

func (dMgr *Manager) DownloadImageToDevice(ctx context.Context, request *voltha.DeviceImageDownloadRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageDownloadRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "DownloadImageToDevice")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	downloadReq := &voltha.DeviceImageDownloadRequest{
		Image:             request.Image,
		ActivateOnSuccess: request.ActivateOnSuccess,
		CommitOnSuccess:   request.CommitOnSuccess,
	}

	for index, deviceID := range request.DeviceId {
		//slice-out only single deviceID from the request
		downloadReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageDownloadRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.downloadImageToDevice(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "download-image-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "download-image-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), downloadReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "download-image-to-device", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) GetImageStatus(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "GetImageStatus")

	imageStatusReq := &voltha.DeviceImageRequest{
		Version:         request.Version,
		CommitOnSuccess: request.CommitOnSuccess,
	}

	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))
	for index, deviceID := range request.DeviceId {
		//slice-out only single deviceID from the request
		imageStatusReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.getImageStatus(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "get-image-status-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "get-image-status-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), imageStatusReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "get-image-status", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) AbortImageUpgradeToDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "AbortImageUpgradeToDevice")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	abortImageReq := &voltha.DeviceImageRequest{
		Version:         request.Version,
		CommitOnSuccess: request.CommitOnSuccess,
	}

	for index, deviceID := range request.DeviceId {
		//slice-out only single deviceID from the request
		abortImageReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.abortImageUpgradeToDevice(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "abort-image-upgrade-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "abort-image-upgrade-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), abortImageReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "abort-image-upgrade-to-device", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) GetOnuImages(ctx context.Context, id *common.ID) (*voltha.OnuImages, error) {
	if id == nil || id.Id == "" {
		return nil, status.Errorf(codes.InvalidArgument, "empty device id")
	}

	ctx = utils.WithRPCMetadataContext(ctx, "GetOnuImages")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}

	resp, err := agent.getOnuImages(ctx, id)
	if err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "get-onu-images-result", log.Fields{"onu-image": resp.Items})

	return resp, nil
}

func (dMgr *Manager) ActivateImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "ActivateImage")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	activateImageReq := &voltha.DeviceImageRequest{
		Version:         request.Version,
		CommitOnSuccess: request.CommitOnSuccess,
	}

	for index, deviceID := range request.DeviceId {
		//slice-out only single deviceID from the request
		activateImageReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.activateImageOnDevice(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "activate-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "activate-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), activateImageReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "activate-image", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) CommitImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "CommitImage")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	commitImageReq := &voltha.DeviceImageRequest{
		Version:         request.Version,
		CommitOnSuccess: request.CommitOnSuccess,
	}

	for index, deviceID := range request.DeviceId {
		//slice-out only single deviceID from the request
		commitImageReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.commitImage(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "commit-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorf(ctx, "commit-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), commitImageReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "commit-image", respCh, len(request.GetDeviceId()))
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
	if request == nil || len(request.DeviceId) == 0 || request.DeviceId[0] == nil {
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

			//check whether all responses received, if so, sent back the collated response
			if respCount == expectedResps {
				return response, nil
			}
			continue
		case <-ctx.Done():
			return nil, status.Errorf(codes.Aborted, opName+"-failed-%s", ctx.Err())
		}
	}
}
