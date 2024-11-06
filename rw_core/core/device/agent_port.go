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

	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/common"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/core/device/port"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// listDevicePorts returns device ports
func (agent *Agent) listDevicePorts() map[uint32]*voltha.Port {
	portIDs := agent.portLoader.ListIDs()
	ports := make(map[uint32]*voltha.Port, len(portIDs))
	for portID := range portIDs {
		if portHandle, have := agent.portLoader.Lock(portID); have {
			ports[portID] = portHandle.GetReadOnly()
			portHandle.Unlock()
		}
	}
	return ports
}

// getPorts retrieves the ports information of the device based on the port type.
func (agent *Agent) getPorts(ctx context.Context, portType voltha.Port_PortType) *voltha.Ports {
	logger.Debugw(ctx, "get-ports", log.Fields{"device-id": agent.deviceID, "port-type": portType})
	ports := &voltha.Ports{}
	for _, port := range agent.listDevicePorts() {
		if port.Type == portType {
			ports.Items = append(ports.Items, port)
		}
	}
	return ports
}

func (agent *Agent) getDevicePort(portID uint32) (*voltha.Port, error) {
	portHandle, have := agent.portLoader.Lock(portID)
	if !have {
		return nil, status.Errorf(codes.NotFound, "port-%d", portID)
	}
	defer portHandle.Unlock()
	return portHandle.GetReadOnly(), nil
}

func (agent *Agent) updatePortsOperState(ctx context.Context, portTypeFilter uint32, operStatus voltha.OperStatus_Types) error {
	logger.Debugw(ctx, "update-ports-oper-state", log.Fields{"device-id": agent.deviceID})

	for portID := range agent.portLoader.ListIDs() {
		if portHandle, have := agent.portLoader.Lock(portID); have {
			if oldPort := portHandle.GetReadOnly(); (1<<oldPort.Type)&portTypeFilter == 0 { // only update port types not included in the mask
				// clone top-level port struct
				newPort := *oldPort
				newPort.OperStatus = operStatus
				if err := portHandle.Update(ctx, &newPort); err != nil {
					portHandle.Unlock()
					return err
				}

				// Notify the logical device manager to change the port state
				// Do this for NNI and UNIs only. PON ports are not known by logical device
				if newPort.Type == voltha.Port_ETHERNET_NNI || newPort.Type == voltha.Port_ETHERNET_UNI {
					if err := agent.deviceMgr.logicalDeviceMgr.updatePortState(ctx, agent.deviceID, portID, operStatus); err != nil {
						// TODO: VOL-2707
						logger.Warnw(ctx, "unable-to-update-logical-port-state", log.Fields{"error": err})
					}

				}
			}
			portHandle.Unlock()
		}
	}
	return nil
}

func (agent *Agent) updatePortState(ctx context.Context, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_Types) error {
	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if _, ok := voltha.Port_PortType_value[portType.String()]; !ok {
		return status.Errorf(codes.InvalidArgument, "%s", portType)
	}

	portHandle, have := agent.portLoader.Lock(portNo)
	if !have {
		return nil
	}
	defer portHandle.Unlock()

	port := portHandle.GetReadOnly()
	if port.Type != portType {
		return nil
	}

	newPort := *port // clone top-level port struct
	newPort.OperStatus = operStatus
	return portHandle.Update(ctx, &newPort)
}

func (agent *Agent) deleteAllPorts(ctx context.Context) error {
	logger.Debugw(ctx, "delete-all-ports", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}

	if !agent.isDeletionInProgress() {
		err := status.Error(codes.FailedPrecondition, fmt.Sprintf("invalid-admin-state-%v",
			device.AdminState))
		logger.Warnw(ctx, "invalid-state-removing-ports", log.Fields{"state": device.AdminState, "error": err})
		return err
	}

	for portID := range agent.portLoader.ListIDs() {
		if portHandle, have := agent.portLoader.Lock(portID); have {
			if err := portHandle.Delete(ctx); err != nil {
				portHandle.Unlock()
				return err
			}
			portHandle.Unlock()
		}
	}
	return nil
}

func (agent *Agent) addPort(ctx context.Context, port *voltha.Port) error {
	logger.Debugw(ctx, "addPort", log.Fields{"device-id": agent.deviceID})
	var desc string
	var err error
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}

	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	port.AdminState = voltha.AdminState_ENABLED

	portHandle, created, err := agent.portLoader.LockOrCreate(ctx, port)
	if err != nil {
		desc = err.Error()
		return err
	}
	defer portHandle.Unlock()

	if created {
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return nil
	}

	oldPort := portHandle.GetReadOnly()
	if oldPort.Label != "" || oldPort.Type != voltha.Port_PON_OLT {
		logger.Debugw(ctx, "port-already-exists", log.Fields{"port": port})
		desc = fmt.Sprintf("port already exists, port : %s", port)
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
		return nil
	}

	// Creation of OLT PON port is being processed after a default PON port was created.  Just update it.
	logger.Infow(ctx, "update-pon-port-created-by-default", log.Fields{"default-port": oldPort, "port-to-add": port})
	newPort := *oldPort // clone top-level port struct
	newPort.Label = port.Label
	newPort.OperStatus = port.OperStatus

	err = portHandle.Update(ctx, &newPort)
	if err != nil {
		desc = err.Error()
		return err
	}
	operStatus.Code = common.OperationResp_OPERATION_SUCCESS
	return err
}

func (agent *Agent) addPeerPort(ctx context.Context, peerPort *voltha.Port_PeerPort) error {
	logger.Debugw(ctx, "adding-peer-peerPort", log.Fields{"device-id": agent.deviceID, "peer-peerPort": peerPort})

	var portHandle *port.Handle
	if agent.isRootDevice {
		// If an ONU PON port needs to be referenced before the corresponding creation of the OLT PON port, then create the OLT PON port
		// with default values, and update it later when the OLT PON port creation is processed.
		ponPort := &voltha.Port{
			PortNo:     peerPort.PortNo,
			Type:       voltha.Port_PON_OLT,
			AdminState: voltha.AdminState_ENABLED,
			DeviceId:   agent.deviceID,
			Peers:      []*voltha.Port_PeerPort{peerPort},
		}

		h, created, err := agent.portLoader.LockOrCreate(ctx, ponPort)
		if err != nil {
			return err
		}
		defer h.Unlock()

		if created {
			logger.Infow(ctx, "added-default-pon-port", log.Fields{"device-id": agent.deviceID, "peer": peerPort, "pon-port": ponPort})
			return nil
		}

		portHandle = h
	} else {
		h, have := agent.portLoader.Lock(peerPort.PortNo)
		if !have {
			return nil
		}
		defer h.Unlock()

		portHandle = h
	}

	logger.Debugw(ctx, "found-peer", log.Fields{"device-id": agent.deviceID, "portNo": peerPort.PortNo, "deviceId": agent.deviceID})

	newPort := proto.Clone(portHandle.GetReadOnly()).(*voltha.Port)
	newPort.Peers = append(newPort.Peers, peerPort)

	return portHandle.Update(ctx, newPort)
}

func (agent *Agent) disablePort(ctx context.Context, portID uint32) error {
	logger.Debugw(ctx, "disable-port", log.Fields{"device-id": agent.deviceID, "port-no": portID})

	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	portHandle, have := agent.portLoader.Lock(portID)
	if !have {
		err = status.Errorf(codes.InvalidArgument, "%v", portID)
		return err
	}
	defer portHandle.Unlock()

	oldPort := portHandle.GetReadOnly()

	if oldPort.Type != voltha.Port_PON_OLT {
		err = status.Errorf(codes.Unimplemented, "disabling of Port Type %v unimplemented", oldPort.Type)
		return err
	}

	newPort := *oldPort
	newPort.AdminState = voltha.AdminState_DISABLED
	if err = portHandle.Update(ctx, &newPort); err != nil {
		return err
	}

	// Send request to adapter
	var device *voltha.Device
	device, err = agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}

	// Send the request to the adapter
	var client adapter_service.AdapterServiceClient
	client, err = agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err = client.DisablePort(subCtx, &newPort)
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()
	return nil
}

func (agent *Agent) enablePort(ctx context.Context, portID uint32) error {
	logger.Debugw(ctx, "enable-port", log.Fields{"device-id": agent.deviceID, "port-no": portID})

	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	portHandle, have := agent.portLoader.Lock(portID)
	if !have {
		err = status.Errorf(codes.InvalidArgument, "%v", portID)
		return err
	}
	defer portHandle.Unlock()

	oldPort := portHandle.GetReadOnly()

	if oldPort.Type != voltha.Port_PON_OLT {
		err = status.Errorf(codes.Unimplemented, "enabling of Port Type %v unimplemented", oldPort.Type)
		return err
	}

	newPort := *oldPort
	newPort.AdminState = voltha.AdminState_ENABLED
	if err = portHandle.Update(ctx, &newPort); err != nil {
		return err
	}

	// Send request to adapter
	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}

	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		return err
	}
	subCtx, cancel := context.WithTimeout(coreutils.WithAllMetadataFromContext(ctx), agent.rpcTimeout)
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err := client.EnablePort(subCtx, &newPort)
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()
	return nil
}
