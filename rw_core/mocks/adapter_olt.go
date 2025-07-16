/*
 * Copyright 2019-2024 Open Networking Foundation (ONF) and the ONF Contributors

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

package mocks

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/extension"
	"github.com/opencord/voltha-protos/v5/go/health"
	"github.com/opencord/voltha-protos/v5/go/omci"
	"github.com/phayes/freeport"

	"github.com/gogo/protobuf/proto"
	com "github.com/opencord/voltha-lib-go/v7/pkg/adapters/common"
	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	of "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

// OLTAdapter represent OLT adapter
type OLTAdapter struct {
	*Adapter
	grpcServer      *vgrpc.GrpcServer
	ChildDeviceType string
	childVendor     string
}

// NewOLTAdapter - creates OLT adapter instance
func NewOLTAdapter(ctx context.Context, coreEndpoint string, deviceType string, vendor string, childDeviceType, childVendor string) *OLTAdapter {
	// Get an available  port
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatalw(ctx, "no-free-port", log.Fields{"error": err})
	}

	// start gRPC handler
	listeningAddress := fmt.Sprintf("127.0.0.1:%s", strconv.Itoa(grpcPort))
	oltAdapter := &OLTAdapter{Adapter: NewAdapter(listeningAddress, coreEndpoint, deviceType, vendor),
		ChildDeviceType: childDeviceType, childVendor: childVendor}

	oltAdapter.start(ctx)
	return oltAdapter
}

func (oltA *OLTAdapter) oltRestarted(ctx context.Context, endPoint string) error {
	logger.Errorw(ctx, "remote-restarted", log.Fields{"endpoint": endPoint})
	return nil
}

func (oltA *OLTAdapter) start(ctx context.Context) {

	// Set up the probe service
	oltA.Probe = &probe.Probe{}
	probePort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for probePort")
	}
	probeAddress := "127.0.0.1:" + strconv.Itoa(probePort)
	go oltA.Probe.ListenAndServe(ctx, probeAddress)

	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, oltA.Probe)

	oltA.Probe.RegisterService(ctx, "olt-grpc-service", oltA.coreEnpoint)

	oltA.grpcServer = vgrpc.NewGrpcServer(oltA.serviceEndpoint, nil, false, nil)

	logger.Debugw(ctx, "OLTAdapter-address", log.Fields{"address": oltA.serviceEndpoint})

	go oltA.startGRPCService(ctx, oltA.grpcServer, oltA, "olt-grpc-service")

	// Establish grpc connection to Core
	if oltA.coreClient, err = vgrpc.NewClient(
		"mock-olt-endpoint",
		oltA.coreEnpoint,
		"core_service.CoreService",
		oltA.oltRestarted); err != nil {
		logger.Fatal(ctx, "grpc-client-not-created")
	}

	go oltA.coreClient.Start(probeCtx, setCoreServiceHandler)

	logger.Debugw(ctx, "OLTAdapter-started", log.Fields{"grpc-address": oltA.serviceEndpoint})

}

// Stop brings down core services
func (oltA *OLTAdapter) StopGrpcClient() {
	// Stop the grpc clients
	oltA.coreClient.Stop(context.Background())
}

// Stop brings down core services
func (oltA *OLTAdapter) Stop() {

	// Stop the grpc
	if oltA.grpcServer != nil {
		oltA.grpcServer.Stop()
	}
	logger.Debugw(context.Background(), "OLTAdapter-stopped", log.Fields{"grpc-address": oltA.serviceEndpoint})

}

// Adopt_device creates new handler for added device
func (oltA *OLTAdapter) AdoptDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	logger.Debugw(ctx, "AdoptDevice", log.Fields{"device": device.AdapterEndpoint, "device-type": oltA.DeviceType})
	go func() {
		d := proto.Clone(device).(*voltha.Device)
		d.Root = true
		d.Vendor = oltA.vendor
		d.Model = "go-mock"
		d.SerialNumber = com.GetRandomSerialNumber()
		d.MacAddress = strings.ToUpper(com.GetRandomMacAddress())
		oltA.storeDevice(d)
		c, err := oltA.GetCoreClient()
		if err != nil {
			return
		}
		if _, err = c.DeviceUpdate(context.TODO(), d); err != nil {
			logger.Fatalf(ctx, "deviceUpdate-failed-%s", err)
		}

		capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
		nniPort := &voltha.Port{
			DeviceId:   device.Id,
			PortNo:     2,
			Label:      fmt.Sprintf("nni-%d", 2),
			Type:       voltha.Port_ETHERNET_NNI,
			OperStatus: common.OperStatus_ACTIVE,
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array("11:22:33:44:55:66"),
				Config:     0,
				State:      uint32(of.OfpPortState_OFPPS_LIVE),
				Curr:       capability,
				Advertised: capability,
				Peer:       capability,
				CurrSpeed:  uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
				MaxSpeed:   uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
			},
		}
		if _, err = c.PortCreated(context.TODO(), nniPort); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		ponPort := &voltha.Port{
			DeviceId:   device.Id,
			PortNo:     1,
			Label:      fmt.Sprintf("pon-%d", 1),
			Type:       voltha.Port_PON_OLT,
			OperStatus: common.OperStatus_ACTIVE,
		}
		if _, err = c.PortCreated(context.TODO(), ponPort); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		d.ConnectStatus = common.ConnectStatus_REACHABLE
		d.OperStatus = common.OperStatus_ACTIVE

		if _, err = c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{DeviceId: d.Id, OperStatus: d.OperStatus, ConnStatus: d.ConnectStatus}); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		// Get the latest device data from the Core
		if d, err = c.GetDevice(context.TODO(), &common.ID{Id: d.Id}); err != nil {
			logger.Fatalf(ctx, "getting-device-failed-%s", err)
		}

		oltA.updateDevice(d)

		// Register Child devices
		initialUniPortNo := startingUNIPortNo
		for i := 0; i < numONUPerOLT; i++ {
			go func(seqNo int) {
				if _, err = c.ChildDeviceDetected(context.TODO(),
					&ca.DeviceDiscovery{
						ParentId:        d.Id,
						ParentPortNo:    1,
						ChildDeviceType: oltA.ChildDeviceType,
						ChannelId:       uint32(initialUniPortNo + seqNo),
						VendorId:        oltA.childVendor,
						SerialNumber:    com.GetRandomSerialNumber(),
						OnuId:           uint32(seqNo),
					}); err != nil {
					logger.Fatalw(ctx, "failure-sending-child-device", log.Fields{"error": err, "parent-id": d.Id, "child-device-type": oltA.ChildDeviceType})
				}
			}(i)
		}
	}()
	return &empty.Empty{}, nil
}

// Single_get_value_request retrieves a single value.
func (oltA *OLTAdapter) Single_get_value_request(ctx context.Context, // nolint
	request extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	logger.Fatalf(ctx, "Single_get_value_request unimplemented")
	return nil, nil
}

// Get_ofp_device_info returns ofp device info
func (oltA *OLTAdapter) GetOfpDeviceInfo(ctx context.Context, device *voltha.Device) (*ca.SwitchCapability, error) { // nolint
	if d := oltA.getDevice(device.Id); d == nil {
		logger.Fatalf(ctx, "device-not-found-%s", device.Id)
	}
	return &ca.SwitchCapability{
		Desc: &of.OfpDesc{
			HwDesc:    "olt_adapter_mock",
			SwDesc:    "olt_adapter_mock",
			SerialNum: "12345678",
		},
		SwitchFeatures: &of.OfpSwitchFeatures{
			NBuffers: 256,
			NTables:  2,
			Capabilities: uint32(of.OfpCapabilities_OFPC_FLOW_STATS |
				of.OfpCapabilities_OFPC_TABLE_STATS |
				of.OfpCapabilities_OFPC_PORT_STATS |
				of.OfpCapabilities_OFPC_GROUP_STATS),
		},
	}, nil
}

// Disable_device disables device
func (oltA *OLTAdapter) DisableDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) { // nolint
	go func() {
		if d := oltA.getDevice(device.Id); d == nil {
			logger.Fatalf(ctx, "device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to disable
		c, err := oltA.GetCoreClient()
		if err != nil {
			return
		}

		if _, err := c.PortsStateUpdate(context.TODO(),
			&ca.PortStateFilter{
				DeviceId:       cloned.Id,
				PortTypeFilter: 0,
				OperStatus:     common.OperStatus_UNKNOWN,
			}); err != nil {
			logger.Warnw(ctx, "updating-ports-failed", log.Fields{"device-id": device.Id, "error": err})
		}

		// Update the device operational state
		cloned.OperStatus = common.OperStatus_UNKNOWN
		// The device is still reachable after it has been disabled, so the connection status should not be changed.

		if _, err := c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{
			DeviceId:   cloned.Id,
			OperStatus: cloned.OperStatus,
			ConnStatus: cloned.ConnectStatus,
		}); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
			return
		}

		oltA.updateDevice(cloned)

		// Tell the Core that all child devices have been disabled (by default it's an action already taken by the Core
		if _, err := c.ChildDevicesLost(context.TODO(), &common.ID{Id: cloned.Id}); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw(ctx, "lost-notif-of-child-devices-failed", log.Fields{"device-id": device.Id, "error": err})
		}
	}()
	return &empty.Empty{}, nil
}

// Reenable_device reenables device
func (oltA *OLTAdapter) ReEnableDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) { // nolint
	go func() {
		if d := oltA.getDevice(device.Id); d == nil {
			logger.Fatalf(ctx, "device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)

		c, err := oltA.GetCoreClient()
		if err != nil {
			return
		}

		// Update the all ports state on that device to enable
		if _, err := c.PortsStateUpdate(context.TODO(),
			&ca.PortStateFilter{
				DeviceId:       cloned.Id,
				PortTypeFilter: 0,
				OperStatus:     common.OperStatus_ACTIVE,
			}); err != nil {
			logger.Warnw(ctx, "updating-ports-failed", log.Fields{"device-id": device.Id, "error": err})
		}

		// Update the device state
		cloned.OperStatus = common.OperStatus_ACTIVE

		if _, err := c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{
			DeviceId:   cloned.Id,
			OperStatus: cloned.OperStatus,
			ConnStatus: cloned.ConnectStatus,
		}); err != nil {
			// Device may already have been deleted in the core
			logger.Fatalf(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
			return
		}

		// Tell the Core that all child devices have been enabled
		if _, err := c.ChildDevicesDetected(context.TODO(), &common.ID{Id: cloned.Id}); err != nil {
			logger.Fatalf(ctx, "detection-notif-of-child-devices-failed", log.Fields{"device-id": device.Id, "error": err})
		}
	}()
	return &empty.Empty{}, nil
}

// Enable_port -
func (oltA *OLTAdapter) EnablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) { //nolint
	go func() {
		c, err := oltA.GetCoreClient()
		if err != nil {
			return
		}

		if port.Type == voltha.Port_PON_OLT {
			if _, err := c.PortStateUpdate(context.TODO(),
				&ca.PortState{
					DeviceId:   port.DeviceId,
					PortType:   voltha.Port_ETHERNET_NNI,
					PortNo:     port.PortNo,
					OperStatus: common.OperStatus_ACTIVE,
				}); err != nil {
				logger.Fatalf(ctx, "updating-ports-failed", log.Fields{"device-id": port.DeviceId, "error": err})
			}
		}

	}()
	return &empty.Empty{}, nil
}

// Disable_port -
func (oltA *OLTAdapter) DisablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) { //nolint
	go func() {
		c, err := oltA.GetCoreClient()
		if err != nil {
			return
		}
		if port.Type == voltha.Port_PON_OLT {
			if _, err := c.PortStateUpdate(context.TODO(),
				&ca.PortState{
					DeviceId:   port.DeviceId,
					PortType:   voltha.Port_PON_OLT,
					PortNo:     port.PortNo,
					OperStatus: common.OperStatus_DISCOVERED,
				}); err != nil {
				// Corresponding device may have been deleted
				logger.Warnw(ctx, "updating-ports-failed", log.Fields{"device-id": port.DeviceId, "error": err})
			}
		}
	}()
	return &empty.Empty{}, nil
}

// Reboot_device -
func (oltA *OLTAdapter) RebootDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) { // nolint
	logger.Infow(ctx, "reboot-device", log.Fields{"device-id": device.Id})

	go func() {
		c, err := oltA.GetCoreClient()
		if err != nil {
			return
		}

		if _, err := c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{
			DeviceId:   device.Id,
			OperStatus: common.OperStatus_UNKNOWN,
			ConnStatus: common.ConnectStatus_UNREACHABLE,
		}); err != nil {
			logger.Fatalf(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
			return
		}

		if _, err := c.PortsStateUpdate(context.TODO(),
			&ca.PortStateFilter{
				DeviceId:       device.Id,
				PortTypeFilter: 0,
				OperStatus:     common.OperStatus_UNKNOWN,
			}); err != nil {
			logger.Warnw(ctx, "updating-ports-failed", log.Fields{"device-id": device.Id, "error": err})
		}
	}()
	return &empty.Empty{}, nil
}

// TODO: REMOVE Start_omci_test begins an omci self-test
func (oltA *OLTAdapter) StartOmciTest(ctx context.Context, test *ca.OMCITest) (*omci.TestResponse, error) { // nolint
	return nil, errors.New("start-omci-test-not-implemented")
}

// Helper for test only
func (oltA *OLTAdapter) SetDeviceActive(deviceID string) {
	c, err := oltA.GetCoreClient()
	if err != nil {
		return
	}

	if _, err := c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{
		DeviceId:   deviceID,
		OperStatus: common.OperStatus_ACTIVE,
		ConnStatus: common.ConnectStatus_REACHABLE,
	}); err != nil {
		logger.Warnw(context.Background(), "device-state-update-failed", log.Fields{"device-id": deviceID, "error": err})
		return
	}

}

func (oltA *OLTAdapter) GetHealthStatus(stream adapter_service.AdapterService_GetHealthStatusServer) error {
	ctx := context.Background()
	logger.Debugw(ctx, "receive-stream-connection", log.Fields{"stream": stream})

	if stream == nil {
		return fmt.Errorf("conn-is-nil %v", stream)
	}
	initialRequestTime := time.Now()
	var remoteClient *common.Connection
	var tempClient *common.Connection
	var err error
loop:
	for {
		tempClient, err = stream.Recv()
		if err != nil {
			logger.Warnw(ctx, "received-stream-error", log.Fields{"remote-client": remoteClient, "error": err})
			break loop
		}
		err = stream.Send(&health.HealthStatus{State: health.HealthStatus_HEALTHY})
		if err != nil {
			logger.Warnw(ctx, "sending-stream-error", log.Fields{"remote-client": remoteClient, "error": err})
			break loop
		}
		remoteClient = tempClient
		logger.Debugw(ctx, "received-keep-alive", log.Fields{"remote-client": remoteClient})

		select {
		case <-stream.Context().Done():
			logger.Infow(ctx, "stream-keep-alive-context-done", log.Fields{"remote-client": remoteClient, "error": stream.Context().Err()})
			break loop
		default:
		}
	}
	logger.Errorw(ctx, "connection-down", log.Fields{"remote-client": remoteClient, "error": err, "initial-conn-time": initialRequestTime})
	return err
}

func (oltA *OLTAdapter) SetDeviceRebooted(deviceID string) {
	c, err := oltA.GetCoreClient()
	if err != nil {
		return
	}

	if _, err := c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{
		DeviceId:   deviceID,
		OperStatus: common.OperStatus_REBOOTED,
		ConnStatus: common.ConnectStatus_REACHABLE,
	}); err != nil {
		logger.Warnw(context.Background(), "device-state-update-failed", log.Fields{"device-id": deviceID, "error": err})
		return
	}

}

func (onuA *OLTAdapter) DisableChildSerialNumber(ctx context.Context, in *voltha.OnuSerialNumberOfOLTPon) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (onuA *OLTAdapter) EnableChildSerialNumber(ctx context.Context, in *voltha.OnuSerialNumberOfOLTPon) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (onuA *OLTAdapter) DisableChildDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (onuA *OLTAdapter) EnableChildDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}
