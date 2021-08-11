/*
 * Copyright 2021-present Open Networking Foundation

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

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ic "github.com/opencord/voltha-protos/v5/go/inter_container"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (dMgr *Manager) PortCreated(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "PortCreated")

	logger.Debugw(ctx, "port-created", log.Fields{"port": port})

	agent := dMgr.getDeviceAgent(ctx, port.DeviceId)
	if agent != nil {
		if err := agent.addPort(ctx, port); err != nil {
			return nil, err
		}
		//	Setup peer ports in its own routine
		go func() {
			if err := dMgr.addPeerPort(log.WithSpanFromContext(context.Background(), ctx), port.DeviceId, port); err != nil {
				logger.Errorw(ctx, "unable-to-add-peer-port", log.Fields{"error": err, "device-id": port.DeviceId})
			}
		}()
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", port.DeviceId)
}

func (dMgr *Manager) DeviceUpdate(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "DeviceUpdate")
	logger.Debugw(ctx, "device-update", log.Fields{"device-id": device.Id, "device": device})

	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		if err := agent.updateDeviceUsingAdapterData(ctx, device); err != nil {
			return nil, err
		}
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", device.Id)
}

func (dMgr *Manager) DeviceStateUpdate(ctx context.Context, ds *ic.DeviceStateFilter) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "DeviceStateUpdate")
	logger.Debugw(ctx, "device-state-update", log.Fields{"device-id": ds.DeviceId, "operStatus": ds.OperStatus, "connStatus": ds.ConnStatus})

	if agent := dMgr.getDeviceAgent(ctx, ds.DeviceId); agent != nil {
		if err := agent.updateDeviceStatus(ctx, ds.OperStatus, ds.ConnStatus); err != nil {
			return nil, err
		}
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", ds.DeviceId)
}

func (dMgr *Manager) ChildDeviceDetected(ctx context.Context, dd *ic.DeviceDiscovery) (*voltha.Device, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "ChildDeviceDetected")
	logger.Debugw(ctx, "child-device-detected",
		log.Fields{
			"parent-device-id": dd.ParentId,
			"parentPortNo":     dd.ParentPortNo,
			"deviceType":       dd.ChildDeviceType,
			"channelId":        dd.ChannelId,
			"vendorId":         dd.VendorId,
			"serialNumber":     dd.SerialNumber,
			"onuId":            dd.OnuId,
		})

	var err error
	if dd.ChildDeviceType == "" && dd.VendorId != "" {
		logger.Debug(ctx, "device-type-is-nil-fetching-device-type")
		if dd.ChildDeviceType, err = dMgr.adapterMgr.GetAdapterTypeByVendorID(dd.VendorId); err != nil {
			return nil, err
		}
	}
	//if no match found for the vendorid,report adapter with the custom error message
	if dd.ChildDeviceType == "" {
		logger.Errorw(ctx, "failed-to-fetch-adapter-name ", log.Fields{"vendorId": dd.VendorId})
		return nil, status.Errorf(codes.NotFound, "%s", dd.VendorId)
	}

	// Create the ONU device
	childDevice := &voltha.Device{}
	childDevice.Type = dd.ChildDeviceType
	childDevice.ParentId = dd.ParentId
	childDevice.ParentPortNo = uint32(dd.ParentPortNo)
	childDevice.VendorId = dd.VendorId
	childDevice.SerialNumber = dd.SerialNumber
	childDevice.Root = false

	// Get parent device type
	pAgent := dMgr.getDeviceAgent(ctx, dd.ParentId)
	if pAgent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", dd.ParentId)
	}
	if pAgent.deviceType == "" {
		pDevice, err := pAgent.getDeviceReadOnly(ctx)
		logger.Errorw(ctx, "device-type-not-set", log.Fields{"parent-device": pDevice, "error": err})
		return nil, status.Errorf(codes.FailedPrecondition, "device Type not set %s", dd.ParentId)
	}

	if device, err := dMgr.GetChildDevice(ctx, &ic.ChildDeviceFilter{
		ParentId:     dd.ParentId,
		SerialNumber: dd.SerialNumber,
		OnuId:        dd.OnuId,
		ParentPortNo: dd.ParentPortNo}); err == nil {
		logger.Warnw(ctx, "child-device-exists", log.Fields{"parent-device-id": dd.ParentId, "serialNumber": dd.SerialNumber})
		return device, status.Errorf(codes.AlreadyExists, "%s", dd.SerialNumber)
	}

	//Get parent endpoint
	pEndPoint, err := dMgr.adapterMgr.GetAdapterEndpoint(ctx, pAgent.deviceID, pAgent.deviceType)
	if err != nil {
		logger.Errorw(ctx, "endpoint-error", log.Fields{"error": err, "parent-id": pAgent.deviceID, "parent-device-type": pAgent.deviceType})
		return nil, status.Errorf(codes.NotFound, "parent-endpoint-%s", dd.ParentId)
	}

	childDevice.ProxyAddress = &voltha.Device_ProxyAddress{DeviceId: dd.ParentId, DeviceType: pAgent.deviceType, ChannelId: dd.ChannelId, OnuId: dd.OnuId, AdapterEndpoint: pEndPoint}

	// Set child device ID -- needed to get the device endpoint
	childDevice.Id = utils.CreateDeviceID()

	// Set the child adapter endpoint
	childDevice.AdapterEndpoint, err = dMgr.adapterMgr.GetAdapterEndpoint(ctx, childDevice.Id, childDevice.Type)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "child-endpoint-%s", childDevice.Id)
	}

	// Create and start a device agent for that device
	agent := newAgent(childDevice, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.internalTimeout, dMgr.rpcTimeout)
	insertedChildDevice, err := agent.start(ctx, false, childDevice)
	if err != nil {
		logger.Errorw(ctx, "error-starting-child-device", log.Fields{"parent-device-id": childDevice.ParentId, "child-device-id": agent.deviceID, "error": err})
		return nil, err
	}
	dMgr.addDeviceAgentToMap(agent)

	// Activate the child device
	if agent = dMgr.getDeviceAgent(ctx, agent.deviceID); agent != nil {
		go func() {
			err := agent.enableDevice(utils.WithSpanAndRPCMetadataFromContext(ctx))
			if err != nil {
				logger.Errorw(ctx, "unable-to-enable-device", log.Fields{"error": err, "device-id": agent.deviceID})
			}
		}()
	}

	return insertedChildDevice, nil
}

func (dMgr *Manager) GetChildDevice(ctx context.Context, df *ic.ChildDeviceFilter) (*voltha.Device, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "GetChildDevice")
	logger.Debugw(ctx, "get-child-device", log.Fields{"filter": df})

	parentDevicePorts, err := dMgr.listDevicePorts(ctx, df.ParentId)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	childDeviceIds := dMgr.getAllChildDeviceIds(ctx, parentDevicePorts)
	if len(childDeviceIds) == 0 {
		logger.Debugw(ctx, "no-child-devices", log.Fields{"parent-device-id": df.ParentId, "serial-number": df.SerialNumber, "onu-id": df.OnuId})
		return nil, status.Errorf(codes.NotFound, "%s", df.ParentId)
	}

	var foundChildDevice *voltha.Device
	for childDeviceID := range childDeviceIds {
		var found bool
		if searchDevice, err := dMgr.getDeviceReadOnly(ctx, childDeviceID); err == nil {

			foundOnuID := false
			if searchDevice.ProxyAddress.OnuId == uint32(df.OnuId) {
				if searchDevice.ParentPortNo == uint32(df.ParentPortNo) {
					logger.Debugw(ctx, "found-child-by-onu-id", log.Fields{"parent-device-id": df.ParentId, "onuId": df.OnuId})
					foundOnuID = true
				}
			}

			foundSerialNumber := false
			if searchDevice.SerialNumber == df.SerialNumber {
				logger.Debugw(ctx, "found-child-by-serial-number", log.Fields{"parent-device-id": df.ParentId, "serialNumber": df.SerialNumber})
				foundSerialNumber = true
			}

			// if both onuId and serialNumber are provided both must be true for the device to be found
			// otherwise whichever one found a match is good enough
			if df.OnuId > 0 && df.SerialNumber != "" {
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
		logger.Debugw(ctx, "child-device-found", log.Fields{"parent-device-id": df.ParentId, "foundChildDevice": foundChildDevice})
		return foundChildDevice, nil
	}

	logger.Debugw(ctx, "child-device-not-found", log.Fields{"parent-device-id": df.ParentId,
		"serialNumber": df.SerialNumber, "onuId": df.OnuId, "parentPortNo": df.ParentPortNo})
	return nil, status.Errorf(codes.NotFound, "%s", df.ParentId)
}

// PortsStateUpdate updates the operational status of all ports on the device
func (dMgr *Manager) PortsStateUpdate(ctx context.Context, ps *ic.PortStateFilter) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "PortsStateUpdate")
	logger.Debugw(ctx, "ports-state-update", log.Fields{"device-id": ps.DeviceId})

	agent := dMgr.getDeviceAgent(ctx, ps.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", ps.DeviceId)
	}
	if ps.OperStatus != voltha.OperStatus_ACTIVE && ps.OperStatus != voltha.OperStatus_UNKNOWN {
		return nil, status.Error(codes.Unimplemented, "state-change-not-implemented")
	}
	if err := agent.updatePortsOperState(ctx, ps.PortTypeFilter, ps.OperStatus); err != nil {
		logger.Warnw(ctx, "ports-state-update-failed", log.Fields{"device-id": ps.DeviceId, "error": err})
		return nil, err
	}
	return &empty.Empty{}, nil
}

//ChildDevicesLost is invoked by an adapter to indicate that a parent device is in a state (Disabled) where it
//cannot manage the child devices.  This will trigger the Core to disable all the child devices.
func (dMgr *Manager) ChildDevicesLost(ctx context.Context, parentID *common.ID) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "ChildDevicesLost")
	logger.Debugw(ctx, "child-devices-lost", log.Fields{"parent-id": parentID.Id})

	parentDevice, err := dMgr.getDeviceReadOnly(ctx, parentID.Id)
	if err != nil {
		logger.Warnw(ctx, "failed-getting-device", log.Fields{"parent-device-id": parentID.Id, "error": err})
		return nil, err
	}
	if err = dMgr.DisableAllChildDevices(ctx, parentDevice); err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}

//ChildDevicesDetected is invoked by an adapter when child devices are found, typically after after a
// disable/enable sequence.  This will trigger the Core to Enable all the child devices of that parent.
func (dMgr *Manager) ChildDevicesDetected(ctx context.Context, parentDeviceID *common.ID) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "ChildDevicesDetected")
	logger.Debugw(ctx, "child-devices-detected", log.Fields{"parent-device-id": parentDeviceID})

	parentDevicePorts, err := dMgr.listDevicePorts(ctx, parentDeviceID.Id)
	if err != nil {
		logger.Warnw(ctx, "failed-getting-device", log.Fields{"device-id": parentDeviceID.Id, "error": err})
		return nil, err
	}
	childDeviceIds := dMgr.getAllChildDeviceIds(ctx, parentDevicePorts)
	if len(childDeviceIds) == 0 {
		logger.Debugw(ctx, "no-child-device", log.Fields{"parent-device-id": parentDeviceID.Id})
	}
	allChildEnableRequestSent := true
	for childDeviceID := range childDeviceIds {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			// Run the children re-registration in its own routine
			go func(ctx context.Context) {
				err = agent.enableDevice(ctx)
				if err != nil {
					logger.Errorw(ctx, "unable-to-enable-device", log.Fields{"error": err})
				}
			}(log.WithSpanFromContext(context.Background(), ctx))
		} else {
			err = status.Errorf(codes.Unavailable, "no agent for child device %s", childDeviceID)
			logger.Errorw(ctx, "no-child-device-agent", log.Fields{"parent-device-id": parentDeviceID.Id, "childId": childDeviceID})
			allChildEnableRequestSent = false
		}
	}
	if !allChildEnableRequestSent {
		return nil, err
	}
	return &empty.Empty{}, nil
}

// GetChildDeviceWithProxyAddress will return a device based on proxy address
func (dMgr *Manager) GetChildDeviceWithProxyAddress(ctx context.Context, proxyAddress *voltha.Device_ProxyAddress) (*voltha.Device, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "GetChildDeviceWithProxyAddress")

	logger.Debugw(ctx, "get-child-device-with-proxy-address", log.Fields{"proxyAddress": proxyAddress})

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
		logger.Debugw(ctx, "child-device-found", log.Fields{"proxyAddress": proxyAddress})
		return foundChildDevice, nil
	}

	logger.Warnw(ctx, "child-device-not-found", log.Fields{"proxyAddress": proxyAddress})
	return nil, status.Errorf(codes.NotFound, "%s", proxyAddress)
}

func (dMgr *Manager) GetPorts(ctx context.Context, pf *ic.PortFilter) (*voltha.Ports, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "GetPorts")
	logger.Debugw(ctx, "get-ports", log.Fields{"device-id": pf.DeviceId, "portType": pf.PortType})

	agent := dMgr.getDeviceAgent(ctx, pf.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", pf.DeviceId)
	}
	return agent.getPorts(ctx, pf.PortType), nil
}

func (dMgr *Manager) GetChildDevices(ctx context.Context, parentDeviceID *common.ID) (*voltha.Devices, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "GetChildDevices")

	logger.Debugw(ctx, "get-child-devices", log.Fields{"parent-device-id": parentDeviceID.Id})
	return dMgr.getAllChildDevices(ctx, parentDeviceID.Id)
}

func (dMgr *Manager) ChildrenStateUpdate(ctx context.Context, ds *ic.DeviceStateFilter) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "ChildrenStateUpdate")
	logger.Debugw(ctx, "children-state-update", log.Fields{"parent-device-id": ds.ParentDeviceId, "operStatus": ds.OperStatus, "connStatus": ds.ConnStatus})

	parentDevicePorts, err := dMgr.listDevicePorts(ctx, ds.ParentDeviceId)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	for childDeviceID := range dMgr.getAllChildDeviceIds(ctx, parentDevicePorts) {
		if agent := dMgr.getDeviceAgent(ctx, childDeviceID); agent != nil {
			if err = agent.updateDeviceStatus(ctx, ds.OperStatus, ds.ConnStatus); err != nil {
				return nil, status.Errorf(codes.Aborted, "childDevice:%s, error:%s", childDeviceID, err.Error())
			}
		}
	}
	return &empty.Empty{}, nil
}

func (dMgr *Manager) PortStateUpdate(ctx context.Context, ps *ic.PortState) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "PortStateUpdate")
	logger.Debugw(ctx, "port-state-update", log.Fields{"device-id": ps.DeviceId, "portType": ps.PortType, "portNo": ps.PortNo, "operStatus": ps.OperStatus})

	if agent := dMgr.getDeviceAgent(ctx, ps.DeviceId); agent != nil {
		if err := agent.updatePortState(ctx, ps.PortType, ps.PortNo, ps.OperStatus); err != nil {
			logger.Errorw(ctx, "updating-port-state-failed", log.Fields{"device-id": ps.DeviceId, "portNo": ps.PortNo, "error": err})
			return nil, err
		}
		// Notify the logical device manager to change the port state
		// Do this for NNI and UNIs only. PON ports are not known by logical device
		if ps.PortType == voltha.Port_ETHERNET_NNI || ps.PortType == voltha.Port_ETHERNET_UNI {
			go func() {
				err := dMgr.logicalDeviceMgr.updatePortState(log.WithSpanFromContext(context.Background(), ctx), ps.DeviceId, ps.PortNo, ps.OperStatus)
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
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", ps.DeviceId)
}

func (dMgr *Manager) DeleteAllPorts(ctx context.Context, deviceID *common.ID) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "DeleteAllPorts")
	logger.Debugw(ctx, "delete-all-ports", log.Fields{"device-id": deviceID.Id})

	if agent := dMgr.getDeviceAgent(ctx, deviceID.Id); agent != nil {
		if err := agent.deleteAllPorts(ctx); err != nil {
			return nil, err
		}
		// Notify the logical device manager to remove all logical ports, if needed.
		// At this stage the device itself may gave been deleted already at a DeleteAllPorts
		// typically is part of a device deletion phase.
		if device, err := dMgr.getDeviceReadOnly(ctx, deviceID.Id); err == nil {
			go func() {
				subCtx := utils.WithSpanAndRPCMetadataFromContext(ctx)
				if err := dMgr.logicalDeviceMgr.deleteAllLogicalPorts(subCtx, device); err != nil {
					logger.Errorw(ctx, "unable-to-delete-logical-ports", log.Fields{"error": err})
				}
			}()
		} else {
			logger.Warnw(ctx, "failed-to-retrieve-device", log.Fields{"device-id": deviceID.Id})
			return nil, err
		}
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", deviceID.Id)
}

// GetDevicePort returns the port details for a specific device port entry
func (dMgr *Manager) GetDevicePort(ctx context.Context, pf *ic.PortFilter) (*voltha.Port, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "GetDevicePort")
	logger.Debugw(ctx, "get-device-port", log.Fields{"device-id": pf.DeviceId})

	agent := dMgr.getDeviceAgent(ctx, pf.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", pf.DeviceId)
	}
	return agent.getDevicePort(pf.Port)
}

// DevicePMConfigUpdate updates the pm configs as defined by the adapter.
func (dMgr *Manager) DevicePMConfigUpdate(ctx context.Context, pc *voltha.PmConfigs) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "DevicePMConfigUpdate")
	logger.Debugw(ctx, "device-pm-config-update", log.Fields{"device-id": pc.Id})

	if pc.Id == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "invalid-device-Id")
	}
	if agent := dMgr.getDeviceAgent(ctx, pc.Id); agent != nil {
		if err := agent.initPmConfigs(ctx, pc); err != nil {
			return nil, err
		}
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", pc.Id)
}

// SendPacketIn receives packetIn request from adapter
func (dMgr *Manager) SendPacketIn(ctx context.Context, pi *ic.PacketIn) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "SendPacketIn")
	logger.Debugw(ctx, "packet-in", log.Fields{"device-id": pi.DeviceId, "port": pi.Port})

	// Get the logical device Id based on the deviceId
	var device *voltha.Device
	var err error
	if device, err = dMgr.getDeviceReadOnly(ctx, pi.DeviceId); err != nil {
		logger.Errorw(ctx, "device-not-found", log.Fields{"device-id": pi.DeviceId})
		return nil, err
	}
	if !device.Root {
		logger.Errorw(ctx, "device-not-root", log.Fields{"device-id": pi.DeviceId})
		return nil, status.Errorf(codes.FailedPrecondition, "%s", pi.DeviceId)
	}

	if err := dMgr.logicalDeviceMgr.packetIn(ctx, device.ParentId, pi.Port, pi.Packet); err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}

func (dMgr *Manager) DeviceReasonUpdate(ctx context.Context, dr *ic.DeviceReason) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "DeviceReasonUpdate")
	logger.Debugw(ctx, "update-device-reason", log.Fields{"device-id": dr.DeviceId, "reason": dr.Reason})

	if agent := dMgr.getDeviceAgent(ctx, dr.DeviceId); agent != nil {
		if err := agent.updateDeviceReason(ctx, dr.Reason); err != nil {
			return nil, err
		}
		return &empty.Empty{}, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", dr.DeviceId)
}

func (dMgr *Manager) ReconcileChildDevices(ctx context.Context, parentDeviceID *common.ID) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "ReconcileChildDevices")
	logger.Debugw(ctx, "reconcile-child-devices", log.Fields{"device-id": parentDeviceID.Id})

	numberOfDevicesToReconcile := 0
	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		deviceAgent, ok := value.(*Agent)
		if ok && deviceAgent.parentID == parentDeviceID.Id {
			go deviceAgent.ReconcileDevice(utils.WithNewSpanAndRPCMetadataContext(ctx, "ReconcileChildDevices"))
			numberOfDevicesToReconcile++
		}
		return true
	})
	logger.Debugw(ctx, "reconciling-child-devices-initiated", log.Fields{"parent-device-id": parentDeviceID.Id, "number-of-child-devices-to-reconcile": numberOfDevicesToReconcile})
	return &empty.Empty{}, nil
}

func (dMgr *Manager) UpdateImageDownload(ctx context.Context, img *voltha.ImageDownload) (*empty.Empty, error) {
	ctx = utils.WithNewSpanAndRPCMetadataContext(ctx, "UpdateImageDownload")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "update-image-download", log.Fields{"device-id": img.Id, "image-name": img.Name})

	if agent := dMgr.getDeviceAgent(ctx, img.Id); agent != nil {
		if err := agent.updateImageDownload(ctx, img); err != nil {
			logger.Debugw(ctx, "update-image-download-failed", log.Fields{"err": err, "image-name": img.Name})
			return nil, err
		}
	} else {
		return nil, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	return &empty.Empty{}, nil
}
