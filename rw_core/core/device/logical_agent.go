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
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/core/device/flow"
	"github.com/opencord/voltha-go/rw_core/core/device/group"
	lp "github.com/opencord/voltha-go/rw_core/core/device/logical_port"
	"github.com/opencord/voltha-go/rw_core/core/device/meter"
	fd "github.com/opencord/voltha-go/rw_core/flowdecomposition"
	"github.com/opencord/voltha-go/rw_core/route"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	fu "github.com/opencord/voltha-lib-go/v7/pkg/flows"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ic "github.com/opencord/voltha-protos/v5/go/inter_container"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalAgent represent attributes of logical device agent
type LogicalAgent struct {
	logicalDeviceID string
	serialNumber    string
	rootDeviceID    string
	deviceMgr       *Manager
	ldeviceMgr      *LogicalManager
	ldProxy         *model.Proxy
	stopped         bool
	deviceRoutes    *route.DeviceRoutes
	flowDecomposer  *fd.FlowDecomposer
	internalTimeout time.Duration
	logicalDevice   *voltha.LogicalDevice
	requestQueue    *coreutils.RequestQueue
	orderedEvents   orderedEvents
	startOnce       sync.Once
	stopOnce        sync.Once

	flowCache   *flow.Cache
	meterLoader *meter.Loader
	groupCache  *group.Cache
	portLoader  *lp.Loader
}

func newLogicalAgent(ctx context.Context, id string, sn string, deviceID string, ldeviceMgr *LogicalManager,
	deviceMgr *Manager, dbProxy *model.Path, ldProxy *model.Proxy, internalTimeout time.Duration) *LogicalAgent {
	return &LogicalAgent{
		logicalDeviceID: id,
		serialNumber:    sn,
		rootDeviceID:    deviceID,
		deviceMgr:       deviceMgr,
		ldProxy:         ldProxy,
		ldeviceMgr:      ldeviceMgr,
		deviceRoutes:    route.NewDeviceRoutes(id, deviceID, deviceMgr.listDevicePorts),
		flowDecomposer:  fd.NewFlowDecomposer(deviceMgr.getDeviceReadOnly),
		internalTimeout: internalTimeout,
		requestQueue:    coreutils.NewRequestQueue(),

		flowCache:   flow.NewCache(),
		groupCache:  group.NewCache(),
		meterLoader: meter.NewLoader(dbProxy.SubPath("logical_meters").Proxy(id)),
		portLoader:  lp.NewLoader(dbProxy.SubPath("logical_ports").Proxy(id)),
	}
}

// start creates the logical device and add it to the data model
func (agent *LogicalAgent) start(ctx context.Context, logicalDeviceExist bool, logicalDevice *voltha.LogicalDevice) error {
	needToStart := false
	if agent.startOnce.Do(func() { needToStart = true }); !needToStart {
		return nil
	}

	logger.Infow(ctx, "starting-logical-device-agent", log.Fields{"logical-device-id": agent.logicalDeviceID, "load-from-db": logicalDeviceExist})

	var startSucceeded bool
	defer func() {
		if !startSucceeded {
			if err := agent.stop(ctx); err != nil {
				logger.Errorw(ctx, "failed-to-cleanup-after-unsuccessful-start", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
			}
		}
	}()

	var ld *voltha.LogicalDevice
	if !logicalDeviceExist {
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
		logger.Debugw(ctx, "Switch-capability", log.Fields{"Desc": ld.Desc, "fromAd": switchCap.Desc})
		ld.SwitchFeatures = (proto.Clone(switchCap.SwitchFeatures)).(*ofp.OfpSwitchFeatures)

		// Save the logical device
		if err := agent.ldProxy.Set(ctx, ld.Id, ld); err != nil {
			logger.Errorw(ctx, "failed-to-add-logical-device", log.Fields{"logical-device-id": agent.logicalDeviceID})
			return err
		}
		logger.Debugw(ctx, "logical-device-created", log.Fields{"logical-device-id": agent.logicalDeviceID, "root-id": ld.RootDeviceId})

		agent.logicalDevice = ld

		// Setup the logicalports - internal processing, no need to propagate the client context
		go func() {
			subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
			err := agent.setupLogicalPorts(subCtx)
			if err != nil {
				logger.Errorw(ctx, "unable-to-setup-logical-ports", log.Fields{"error": err})
			}
		}()
	} else {
		// Check to see if we need to load from dB
		ld = logicalDevice
		if logicalDevice == nil {
			//	load from dB
			ld = &voltha.LogicalDevice{}
			have, err := agent.ldProxy.Get(ctx, agent.logicalDeviceID, ld)
			if err != nil {
				return err
			} else if !have {
				return status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
			}
		}

		// Update the root device Id
		agent.rootDeviceID = ld.RootDeviceId

		// Update the last data
		agent.logicalDevice = ld

		// now that the root device is known, create DeviceRoutes with it
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.rootDeviceID, agent.deviceMgr.listDevicePorts)

		// load the meters from KV to cache
		agent.meterLoader.Load(ctx)

		// load the logical ports from KV to cache
		agent.portLoader.Load(ctx)
	}

	// Setup the device routes. Building routes may fail if the pre-conditions are not satisfied (e.g. no PON ports present)
	if logicalDeviceExist {
		go func() {
			subCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
			if err := agent.buildRoutes(subCtx); err != nil {
				logger.Warn(ctx, "routes-not-ready", log.Fields{"logical-device-id": agent.logicalDeviceID, "error": err})
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
		logger.Info(ctx, "stopping-logical-device-agent")

		if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
			// This should never happen - an error is returned only if the agent is stopped and an agent is only stopped once.
			returnErr = err
			return
		}
		defer agent.requestQueue.RequestComplete()
		subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.internalTimeout)
		// Before deletion of the logical agent, make sure all events for ldagent are sent to avoid race conditions
		if err := agent.orderedEvents.waitForAllEventsToBeSent(subCtx, cancel); err != nil {
			//Log the error here
			logger.Errorw(ctx, "failed-to-send-all-events-on-the-logical-device-before-deletion",
				log.Fields{"error": err, "logical-device-id": agent.logicalDeviceID})
		}
		//Remove the logical device from the model
		if err := agent.ldProxy.Remove(ctx, agent.logicalDeviceID); err != nil {
			returnErr = err
		} else {
			logger.Debugw(ctx, "logical-device-removed", log.Fields{"logical-device-id": agent.logicalDeviceID})
		}
		// TODO: remove all entries from all loaders
		// TODO: don't allow any more modifications to flows/groups/meters/ports or to any logical device field

		agent.stopped = true

		logger.Info(ctx, "logical-device-agent-stopped")
	})
	return returnErr
}

// GetLogicalDeviceReadOnly returns the latest logical device data
func (agent *LogicalAgent) GetLogicalDeviceReadOnly(ctx context.Context) (*voltha.LogicalDevice, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	return agent.logicalDevice, nil
}

func (agent *LogicalAgent) addFlowsAndGroupsToDevices(ctx context.Context, deviceRules *fu.DeviceRules) []coreutils.Response {
	logger.Debugw(ctx, "send-add-flows-to-device-manager", log.Fields{"logical-device-id": agent.logicalDeviceID, "device-rules": deviceRules})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.internalTimeout)
			subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
			defer cancel()

			flowMeterConfig, err := agent.GetMeterConfig(ctx, value.ListFlows())
			if err != nil {
				logger.Error(ctx, "meter-referred-in-flow-not-present")
				response.Error(status.Errorf(codes.NotFound, "meter-referred-in-flow-not-present"))
				return
			}
			start := time.Now()
			if err := agent.deviceMgr.addFlowsAndGroups(subCtx, deviceId, value.ListFlows(), value.ListGroups(), toMetadata(flowMeterConfig)); err != nil {
				logger.Errorw(ctx, "flow-add-failed", log.Fields{
					"device-id": deviceId,
					"error":     err,
					"wait-time": time.Since(start),
					"flows":     value.ListFlows(),
					"groups":    value.ListGroups(),
				})
				response.Error(status.Errorf(codes.Internal, "flow-add-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	// Return responses (an array of channels) for the caller to wait for a response from the far end.
	return responses
}

func (agent *LogicalAgent) deleteFlowsAndGroupsFromDevices(ctx context.Context, deviceRules *fu.DeviceRules, mod *ofp.OfpFlowMod) []coreutils.Response {
	logger.Debugw(ctx, "send-delete-flows-to-device-manager", log.Fields{"logical-device-id": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.internalTimeout)
			subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
			defer cancel()

			flowMeterConfig, err := agent.GetMeterConfig(ctx, value.ListFlows())
			if err != nil {
				logger.Error(ctx, "meter-referred-in-flow-not-present")
				response.Error(status.Errorf(codes.NotFound, "meter-referred-in-flow-not-present"))
				return
			}
			start := time.Now()
			if err := agent.deviceMgr.deleteFlowsAndGroups(subCtx, deviceId, value.ListFlows(), value.ListGroups(), toMetadata(flowMeterConfig)); err != nil {
				logger.Errorw(ctx, "flows-and-groups-delete-failed", log.Fields{
					"device-id":   deviceId,
					"error":       err,
					"flow-cookie": mod.Cookie,
					"wait-time":   time.Since(start),
				})
				response.Error(status.Errorf(codes.Internal, "flow-delete-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	return responses
}

func (agent *LogicalAgent) updateFlowsAndGroupsOfDevice(ctx context.Context, deviceRules *fu.DeviceRules, flowMetadata *voltha.FlowMetadata) []coreutils.Response {
	logger.Debugw(ctx, "send-update-flows-to-device-manager", log.Fields{"logical-device-id": agent.logicalDeviceID})

	responses := make([]coreutils.Response, 0)
	for deviceID, value := range deviceRules.GetRules() {
		response := coreutils.NewResponse()
		responses = append(responses, response)
		go func(deviceId string, value *fu.FlowsAndGroups) {
			subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.internalTimeout)
			subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

			defer cancel()
			if err := agent.deviceMgr.updateFlowsAndGroups(subCtx, deviceId, value.ListFlows(), value.ListGroups(), flowMetadata); err != nil {
				logger.Errorw(ctx, "flow-update-failed", log.Fields{"device-id": deviceId, "error": err})
				response.Error(status.Errorf(codes.Internal, "flow-update-failed: %s", deviceId))
			}
			response.Done()
		}(deviceID, value)
	}
	return responses
}

func (agent *LogicalAgent) deleteFlowsFromParentDevice(ctx context.Context, flows map[uint64]*ofp.OfpFlowStats, mod *ofp.OfpFlowMod) []coreutils.Response {
	logger.Debugw(ctx, "deleting-flows-from-parent-device", log.Fields{"logical-device-id": agent.logicalDeviceID, "flows": flows})
	responses := make([]coreutils.Response, 0)
	for _, flow := range flows {
		response := coreutils.NewResponse()
		responses = append(responses, response)

		flowMeterConfig, err := agent.GetMeterConfig(ctx, []*ofp.OfpFlowStats{flow})
		if err != nil {
			logger.Error(ctx, "meter-referred-in-flow-not-present")
			response.Error(status.Errorf(codes.NotFound, "meter-referred-in-flow-not-present"))
			return responses
		}
		uniPort, err := agent.getUNILogicalPortNo(flow)
		if err != nil {
			logger.Error(ctx, "no-uni-port-in-flow", log.Fields{"device-id": agent.rootDeviceID, "flow": flow, "error": err})
			response.Error(err)
			response.Done()
			continue
		}
		logger.Debugw(ctx, "uni-port", log.Fields{"flows": flows, "uni-port": uniPort})
		go func(uniPort uint32, metadata *voltha.FlowMetadata) {
			subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.internalTimeout)
			subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

			defer cancel()
			if err := agent.deviceMgr.deleteParentFlows(subCtx, agent.rootDeviceID, uniPort, metadata); err != nil {
				logger.Error(ctx, "flow-delete-failed-from-parent-device", log.Fields{
					"device-id":   agent.rootDeviceID,
					"error":       err,
					"flow-cookie": mod.Cookie,
				})
				response.Error(status.Errorf(codes.Internal, "flow-delete-failed: %s %v", agent.rootDeviceID, err))
			}
			response.Done()
		}(uniPort, toMetadata(flowMeterConfig))
	}
	return responses
}

func (agent *LogicalAgent) packetOut(ctx context.Context, packet *ofp.OfpPacketOut) {
	if logger.V(log.InfoLevel) {
		logger.Infow(ctx, "packet-out", log.Fields{
			"packet":  hex.EncodeToString(packet.Data),
			"in-port": packet.GetInPort(),
		})
	}
	outPort := fu.GetPacketOutPort(packet)
	//frame := packet.GetData()
	//TODO: Use a channel between the logical agent and the device agent
	if err := agent.deviceMgr.packetOut(ctx, agent.rootDeviceID, outPort, packet); err != nil {
		logger.Error(ctx, "packet-out-failed", log.Fields{"logical-device-id": agent.rootDeviceID})
	}
}

func (agent *LogicalAgent) packetIn(ctx context.Context, port uint32, packet []byte) {
	if logger.V(log.InfoLevel) {
		logger.Infow(ctx, "packet-in", log.Fields{
			"port":   port,
			"packet": hex.EncodeToString(packet),
		})
	}

	packetIn := fu.MkPacketIn(port, packet)
	agent.ldeviceMgr.SendPacketIn(ctx, agent.logicalDeviceID, packetIn)
	logger.Debugw(ctx, "sending-packet-in", log.Fields{"packet": hex.EncodeToString(packetIn.Data)})
}

func (agent *LogicalAgent) deleteAllLogicalMeters(ctx context.Context) error {
	logger.Debugw(ctx, "deleteAllLogicalMeters", log.Fields{"logical-device-id": agent.logicalDeviceID})

	for meterID := range agent.meterLoader.ListIDs() {
		if meterHandle, have := agent.meterLoader.Lock(meterID); have {
			// Update the store and cache
			if err := meterHandle.Delete(ctx); err != nil {
				meterHandle.Unlock()
				logger.Errorw(ctx, "unable-to-delete-meter", log.Fields{"logical-device-id": agent.logicalDeviceID, "meterID": meterID})
				continue
			}
			meterHandle.Unlock()
		}
	}
	return nil
}
