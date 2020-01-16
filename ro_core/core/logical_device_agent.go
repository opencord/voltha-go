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
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogicalDeviceAgent represents logical device agent related information
type LogicalDeviceAgent struct {
	logicalDeviceID   string
	lastData          *voltha.LogicalDevice
	rootDeviceID      string
	deviceMgr         *DeviceManager
	ldeviceMgr        *LogicalDeviceManager
	clusterDataProxy  *model.Proxy
	exitChannel       chan int
	lockLogicalDevice sync.RWMutex
}

func newLogicalDeviceAgent(id string, deviceID string, ldeviceMgr *LogicalDeviceManager, deviceMgr *DeviceManager, cdProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceID = id
	agent.rootDeviceID = deviceID
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.lockLogicalDevice = sync.RWMutex{}
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context, loadFromDb bool) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceID": agent.logicalDeviceID, "loadFromdB": loadFromDb})
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	if loadFromDb {
		//	load from dB - the logical may not exist at this time.  On error, just return and the calling function
		// will destroy this agent.
		if logicalDevice, err := agent.clusterDataProxy.Get(ctx, "/logical_devices/"+agent.logicalDeviceID, 0, false, ""); err != nil {
			log.Errorw("failed-to-get-logical-device", log.Fields{"error": err})
			return err
		} else if logicalDevice != nil {
			if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
				agent.lastData = proto.Clone(lDevice).(*voltha.LogicalDevice)
			}
		} else {
			log.Errorw("failed-to-load-device", log.Fields{"logicaldeviceID": agent.logicalDeviceID})
			return status.Errorf(codes.NotFound, "logicaldeviceID-%s", agent.logicalDeviceID)
		}
	}
	log.Info("logical_device-agent-started")
	return nil
}

// stop terminates the logical device agent.
func (agent *LogicalDeviceAgent) stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	//Remove the logical device from the model
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}

// GetLogicalDevice locks the logical device model and then retrieves the latest logical device information
func (agent *LogicalDeviceAgent) GetLogicalDevice(ctx context.Context) (*voltha.LogicalDevice, error) {
	log.Debug("GetLogicalDevice")

	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	t, _ := ctx.Deadline()
	log.Infof("timeout---------------", t)
	if logicalDevice, err := agent.clusterDataProxy.Get(ctx, "/logical_devices/"+agent.logicalDeviceID, 0, false, ""); err != nil {
		log.Errorw("failed-to-get-logical-device", log.Fields{"error": err})
		return nil, err
	} else if logicalDevice != nil {
		if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
			return lDevice, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDevicePorts returns all logical device ports details
func (agent *LogicalDeviceAgent) ListLogicalDevicePorts(ctx context.Context) (*voltha.LogicalPorts, error) {
	log.Debug("ListLogicalDevicePorts")
	if logicalDevice, _ := agent.ldeviceMgr.getLogicalDevice(ctx, agent.logicalDeviceID); logicalDevice != nil {
		lPorts := make([]*voltha.LogicalPort, 0)
		lPorts = append(lPorts, logicalDevice.Ports...)
		return &voltha.LogicalPorts{Items: lPorts}, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDeviceFlows - listFlows locks the logical device model and then retrieves the latest flow information
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlows(ctx context.Context) (*voltha.Flows, error) {
	log.Debug("ListLogicalDeviceFlows")

	if logicalDevice, _ := agent.ldeviceMgr.getLogicalDevice(ctx, agent.logicalDeviceID); logicalDevice != nil {
		return logicalDevice.GetFlows(), nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}

// ListLogicalDeviceFlowGroups - listFlowGroups locks the logical device model and then retrieves the latest flow groups information
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlowGroups(ctx context.Context) (*voltha.FlowGroups, error) {
	log.Debug("ListLogicalDeviceFlowGroups")

	if logicalDevice, _ := agent.ldeviceMgr.getLogicalDevice(ctx, agent.logicalDeviceID); logicalDevice != nil {
		return logicalDevice.GetFlowGroups(), nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceID)
}
