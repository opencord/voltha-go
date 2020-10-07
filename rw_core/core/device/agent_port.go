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
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/rw_core/core/device/port"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
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
	logger.Debugw(ctx, "getPorts", log.Fields{"device-id": agent.deviceID, "port-type": portType})
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
	logger.Debugw(ctx, "updatePortsOperState", log.Fields{"device-id": agent.deviceID})

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
					go func(portID uint32, ctx context.Context) {
						if err := agent.deviceMgr.logicalDeviceMgr.updatePortState(ctx, agent.deviceID, portID, operStatus); err != nil {
							// TODO: VOL-2707
							logger.Warnw(ctx, "unable-to-update-logical-port-state", log.Fields{"error": err})
						}
					}(portID, log.WithSpanFromContext(context.Background(), ctx))
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
	logger.Debugw(ctx, "deleteAllPorts", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}

	if device.AdminState != voltha.AdminState_DISABLED && !agent.isDeletionInProgress() {
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

	port.AdminState = voltha.AdminState_ENABLED

	portHandle, created, err := agent.portLoader.LockOrCreate(ctx, port)
	if err != nil {
		return err
	}
	defer portHandle.Unlock()

	if created {
		return nil
	}

	oldPort := portHandle.GetReadOnly()
	if oldPort.Label != "" || oldPort.Type != voltha.Port_PON_OLT {
		logger.Debugw(ctx, "port already exists", log.Fields{"port": port})
		return nil
	}

	// Creation of OLT PON port is being processed after a default PON port was created.  Just update it.
	logger.Infow(ctx, "update-pon-port-created-by-default", log.Fields{"default-port": oldPort, "port-to-add": port})
	newPort := *oldPort // clone top-level port struct
	newPort.Label = port.Label
	newPort.OperStatus = port.OperStatus

	return portHandle.Update(ctx, &newPort)
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
	logger.Debugw(ctx, "disablePort", log.Fields{"device-id": agent.deviceID, "port-no": portID})

	portHandle, have := agent.portLoader.Lock(portID)
	if !have {
		return status.Errorf(codes.InvalidArgument, "%v", portID)
	}
	defer portHandle.Unlock()

	oldPort := portHandle.GetReadOnly()

	if oldPort.Type != voltha.Port_PON_OLT {
		return status.Errorf(codes.InvalidArgument, "Disabling of Port Type %v unimplemented", oldPort.Type)
	}

	newPort := *oldPort
	newPort.AdminState = voltha.AdminState_DISABLED
	if err := portHandle.Update(ctx, &newPort); err != nil {
		return err
	}

	//send request to adapter
	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.DisablePort(ctx, device, &newPort)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "disablePort", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) enablePort(ctx context.Context, portID uint32) error {
	logger.Debugw(ctx, "enablePort", log.Fields{"device-id": agent.deviceID, "port-no": portID})

	portHandle, have := agent.portLoader.Lock(portID)
	if !have {
		return status.Errorf(codes.InvalidArgument, "%v", portID)
	}
	defer portHandle.Unlock()

	oldPort := portHandle.GetReadOnly()

	if oldPort.Type != voltha.Port_PON_OLT {
		return status.Errorf(codes.InvalidArgument, "Enabling of Port Type %v unimplemented", oldPort.Type)
	}

	newPort := *oldPort
	newPort.AdminState = voltha.AdminState_ENABLED
	if err := portHandle.Update(ctx, &newPort); err != nil {
		return err
	}

	//send request to adapter
	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	ch, err := agent.adapterProxy.EnablePort(ctx, device, &newPort)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "enablePort", ch, agent.onSuccess, agent.onFailure)
	return nil
}
