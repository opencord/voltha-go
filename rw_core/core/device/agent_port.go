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
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// getPorts retrieves the ports information of the device based on the port type.
func (agent *Agent) getPorts(ctx context.Context, portType voltha.Port_PortType) *voltha.Ports {
	logger.Debugw("getPorts", log.Fields{"device-id": agent.deviceID, "port-type": portType})
	ports := &voltha.Ports{}
	if device, _ := agent.deviceMgr.getDevice(ctx, agent.deviceID); device != nil {
		for _, port := range device.Ports {
			if port.Type == portType {
				ports.Items = append(ports.Items, port)
			}
		}
	}
	return ports
}

// getPortCapability retrieves the port capability of a device
func (agent *Agent) getPortCapability(ctx context.Context, portNo uint32) (*ic.PortCapability, error) {
	logger.Debugw("getPortCapability", log.Fields{"device-id": agent.deviceID})
	device, err := agent.getDevice(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := agent.adapterProxy.GetOfpPortInfo(ctx, device, portNo)
	if err != nil {
		return nil, err
	}
	// Wait for adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed")
	}
	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}
	// Successful response
	portCap := &ic.PortCapability{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, portCap); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}
	return portCap, nil
}
func (agent *Agent) updatePortsOperState(ctx context.Context, operStatus voltha.OperStatus_Types) error {
	logger.Debugw("updatePortsOperState", log.Fields{"device-id": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	cloned := agent.getDeviceWithoutLock()
	for _, port := range cloned.Ports {
		port.OperStatus = operStatus
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) updatePortState(ctx context.Context, portType voltha.Port_PortType, portNo uint32, operStatus voltha.OperStatus_Types) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	// Work only on latest data
	// TODO: Get list of ports from device directly instead of the entire device
	cloned := agent.getDeviceWithoutLock()

	// Ensure the enums passed in are valid - they will be invalid if they are not set when this function is invoked
	if _, ok := voltha.Port_PortType_value[portType.String()]; !ok {
		return status.Errorf(codes.InvalidArgument, "%s", portType)
	}
	for _, port := range cloned.Ports {
		if port.Type == portType && port.PortNo == portNo {
			port.OperStatus = operStatus
		}
	}
	logger.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) deleteAllPorts(ctx context.Context) error {
	logger.Debugw("deleteAllPorts", log.Fields{"deviceId": agent.deviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	cloned := agent.getDeviceWithoutLock()

	if cloned.AdminState != voltha.AdminState_DISABLED && cloned.AdminState != voltha.AdminState_DELETED {
		err := status.Error(codes.FailedPrecondition, fmt.Sprintf("invalid-state-%v", cloned.AdminState))
		logger.Warnw("invalid-state-removing-ports", log.Fields{"state": cloned.AdminState, "error": err})
		return err
	}
	if len(cloned.Ports) == 0 {
		logger.Debugw("no-ports-present", log.Fields{"deviceId": agent.deviceID})
		return nil
	}

	cloned.Ports = []*voltha.Port{}
	logger.Debugw("portStatusUpdate", log.Fields{"deviceId": cloned.Id})
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) addPort(ctx context.Context, port *voltha.Port) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("addPort", log.Fields{"deviceId": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	updatePort := false
	if cloned.Ports == nil {
		//	First port
		logger.Debugw("addPort-first-port-to-add", log.Fields{"deviceId": agent.deviceID})
		cloned.Ports = make([]*voltha.Port, 0)
	} else {
		for _, p := range cloned.Ports {
			if p.Type == port.Type && p.PortNo == port.PortNo {
				if p.Label == "" && p.Type == voltha.Port_PON_OLT {
					//Creation of OLT PON port is being processed after a default PON port was created.  Just update it.
					logger.Infow("update-pon-port-created-by-default", log.Fields{"default-port": p, "port-to-add": port})
					p.Label = port.Label
					p.OperStatus = port.OperStatus
					updatePort = true
					break
				}
				logger.Debugw("port already exists", log.Fields{"port": port})
				return nil
			}
		}
	}
	if !updatePort {
		cp := proto.Clone(port).(*voltha.Port)
		// Set the admin state of the port to ENABLE
		cp.AdminState = voltha.AdminState_ENABLED
		cloned.Ports = append(cloned.Ports, cp)
	}
	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) addPeerPort(ctx context.Context, peerPort *voltha.Port_PeerPort) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("adding-peer-peerPort", log.Fields{"device-id": agent.deviceID, "peer-peerPort": peerPort})

	cloned := agent.getDeviceWithoutLock()

	// Get the peer port on the device based on the peerPort no
	found := false
	for _, port := range cloned.Ports {
		if port.PortNo == peerPort.PortNo { // found peerPort
			cp := proto.Clone(peerPort).(*voltha.Port_PeerPort)
			port.Peers = append(port.Peers, cp)
			logger.Debugw("found-peer", log.Fields{"device-id": agent.deviceID, "portNo": peerPort.PortNo, "deviceId": agent.deviceID})
			found = true
			break
		}
	}
	if !found && agent.isRootdevice {
		// An ONU PON port has been created before the corresponding creation of the OLT PON port.  Create the OLT PON port
		// with default values which will be updated once the OLT PON port creation is processed.
		ponPort := &voltha.Port{
			PortNo:     peerPort.PortNo,
			Type:       voltha.Port_PON_OLT,
			AdminState: voltha.AdminState_ENABLED,
			DeviceId:   agent.deviceID,
			Peers:      []*voltha.Port_PeerPort{proto.Clone(peerPort).(*voltha.Port_PeerPort)},
		}
		cloned.Ports = append(cloned.Ports, ponPort)
		logger.Infow("adding-default-pon-port", log.Fields{"device-id": agent.deviceID, "peer": peerPort, "pon-port": ponPort})
	}

	// Store the device
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) disablePort(ctx context.Context, Port *voltha.Port) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("disablePort", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo})
	var cp *voltha.Port
	// Get the most up to date the device info
	device := agent.getDeviceWithoutLock()
	for _, port := range device.Ports {
		if port.PortNo == Port.PortNo {
			port.AdminState = voltha.AdminState_DISABLED
			cp = proto.Clone(port).(*voltha.Port)
			break

		}
	}
	if cp == nil {
		return status.Errorf(codes.InvalidArgument, "%v", Port.PortNo)
	}

	if cp.Type != voltha.Port_PON_OLT {
		return status.Errorf(codes.InvalidArgument, "Disabling of Port Type %v unimplemented", cp.Type)
	}
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, device, false, ""); err != nil {
		logger.Debugw("updateDeviceInStoreWithoutLock error ", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}

	//send request to adapter
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.DisablePort(ctx, device, cp)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "disablePort", ch, agent.onSuccess, agent.onFailure)
	return nil
}

func (agent *Agent) enablePort(ctx context.Context, Port *voltha.Port) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw("enablePort", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo})

	var cp *voltha.Port
	// Get the most up to date the device info
	device := agent.getDeviceWithoutLock()
	for _, port := range device.Ports {
		if port.PortNo == Port.PortNo {
			port.AdminState = voltha.AdminState_ENABLED
			cp = proto.Clone(port).(*voltha.Port)
			break
		}
	}

	if cp == nil {
		return status.Errorf(codes.InvalidArgument, "%v", Port.PortNo)
	}

	if cp.Type != voltha.Port_PON_OLT {
		return status.Errorf(codes.InvalidArgument, "Enabling of Port Type %v unimplemented", cp.Type)
	}
	// Store the device
	if err := agent.updateDeviceInStoreWithoutLock(ctx, device, false, ""); err != nil {
		logger.Debugw("updateDeviceInStoreWithoutLock error ", log.Fields{"device-id": agent.deviceID, "port-no": Port.PortNo, "error": err})
		return err
	}
	//send request to adapter
	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.EnablePort(ctx, device, cp)
	if err != nil {
		cancel()
		return err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "enablePort", ch, agent.onSuccess, agent.onFailure)
	return nil
}
