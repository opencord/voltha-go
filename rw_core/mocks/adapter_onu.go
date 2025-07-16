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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/common"
	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/extension"
	"github.com/opencord/voltha-protos/v5/go/health"
	"github.com/phayes/freeport"

	"github.com/gogo/protobuf/proto"
	com "github.com/opencord/voltha-lib-go/v7/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/omci"
	of "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

// ONUAdapter represent ONU adapter attributes
type ONUAdapter struct {
	*Adapter
	grpcServer *vgrpc.GrpcServer
}

// NewONUAdapter creates ONU adapter
func NewONUAdapter(ctx context.Context, coreEndpoint string, deviceType string, vendor string) *ONUAdapter {
	// Get an available  port
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatalw(ctx, "no-free-port", log.Fields{"error": err})
	}
	listeningAddress := fmt.Sprintf("127.0.0.1:%s", strconv.Itoa(grpcPort))
	onuAdapter := &ONUAdapter{Adapter: NewAdapter(listeningAddress, coreEndpoint, deviceType, vendor)}

	onuAdapter.start(ctx)
	return onuAdapter
}

func (onuA *ONUAdapter) onuRestarted(ctx context.Context, endPoint string) error {
	logger.Errorw(ctx, "remote-restarted", log.Fields{"endpoint": endPoint})
	return nil
}

func (onuA *ONUAdapter) start(ctx context.Context) {

	// Set up the probe service
	onuA.Probe = &probe.Probe{}
	probePort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for probePort")
	}
	probeAddress := "127.0.0.1:" + strconv.Itoa(probePort)
	go onuA.Probe.ListenAndServe(ctx, probeAddress)

	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, onuA.Probe)

	onuA.Probe.RegisterService(ctx, "onu-grpc-service", onuA.coreEnpoint)

	// start gRPC handler
	onuA.grpcServer = vgrpc.NewGrpcServer(onuA.serviceEndpoint, nil, false, nil)

	logger.Debugw(ctx, "ONUAdapter-address", log.Fields{"address": onuA.serviceEndpoint})

	go onuA.startGRPCService(ctx, onuA.grpcServer, onuA, "onu-grpc-service")

	// Establish grpc connection to Core
	if onuA.coreClient, err = vgrpc.NewClient(
		"mock-onu-endpoint",
		onuA.coreEnpoint,
		"core_service.CoreService",
		onuA.onuRestarted); err != nil {
		logger.Fatal(ctx, "grpc-client-not-created")
	}
	go onuA.coreClient.Start(probeCtx, setCoreServiceHandler)

	logger.Debugw(ctx, "ONUAdapter-started", log.Fields{"grpc-address": onuA.serviceEndpoint})
}

// Stop brings down core services
func (onuA *ONUAdapter) StopGrpcClient() {
	// Stop the grpc clients
	onuA.coreClient.Stop(context.Background())
}

func (onuA *ONUAdapter) Stop() {
	if onuA.grpcServer != nil {
		onuA.grpcServer.Stop()
	}
	logger.Debugw(context.Background(), "ONUAdapter-stopped", log.Fields{"grpc-address": onuA.serviceEndpoint})
}

// Adopt_device creates new handler for added device
func (onuA *ONUAdapter) AdoptDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	logger.Debugw(ctx, "AdoptDevice", log.Fields{"device": device.AdapterEndpoint, "device-type": onuA.DeviceType})
	go func() {
		d := proto.Clone(device).(*voltha.Device)
		d.Root = false
		d.Vendor = onuA.vendor
		d.Model = "go-mock"
		d.SerialNumber = com.GetRandomSerialNumber()
		d.MacAddress = strings.ToUpper(com.GetRandomMacAddress())
		onuA.storeDevice(d)

		c, err := onuA.GetCoreClient()
		if err != nil {
			return
		}
		if _, err = c.DeviceUpdate(context.TODO(), d); err != nil {
			logger.Fatalf(ctx, "deviceUpdate-failed-%s", err)
		}

		d.ConnectStatus = common.ConnectStatus_REACHABLE
		d.OperStatus = common.OperStatus_DISCOVERED

		if _, err = c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{DeviceId: d.Id, OperStatus: d.OperStatus, ConnStatus: d.ConnectStatus}); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		uniPortNo := uint32(2)
		if device.ProxyAddress != nil {
			if device.ProxyAddress.ChannelId != 0 {
				uniPortNo = device.ProxyAddress.ChannelId
			}
		}

		capability := uint32(of.OfpPortFeatures_OFPPF_1GB_FD | of.OfpPortFeatures_OFPPF_FIBER)
		uniPort := &voltha.Port{
			DeviceId:   d.Id,
			PortNo:     uniPortNo,
			Label:      fmt.Sprintf("uni-%d", uniPortNo),
			Type:       voltha.Port_ETHERNET_UNI,
			OperStatus: common.OperStatus_ACTIVE,
			OfpPort: &of.OfpPort{
				HwAddr:     macAddressToUint32Array("12:12:12:12:12:12"),
				Config:     0,
				State:      uint32(of.OfpPortState_OFPPS_LIVE),
				Curr:       capability,
				Advertised: capability,
				Peer:       capability,
				CurrSpeed:  uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
				MaxSpeed:   uint32(of.OfpPortFeatures_OFPPF_1GB_FD),
			},
		}

		if _, err = c.PortCreated(context.TODO(), uniPort); err != nil {
			logger.Fatalf(ctx, "PortCreated-failed-%s", err)
		}

		ponPortNo := uint32(1)
		if device.ParentPortNo != 0 {
			ponPortNo = device.ParentPortNo
		}

		ponPort := &voltha.Port{
			DeviceId:   d.Id,
			PortNo:     ponPortNo,
			Label:      fmt.Sprintf("pon-%d", ponPortNo),
			Type:       voltha.Port_PON_ONU,
			OperStatus: common.OperStatus_ACTIVE,
			Peers: []*voltha.Port_PeerPort{{DeviceId: d.ParentId, // Peer device  is OLT
				PortNo: device.ParentPortNo}}, // Peer port is parent's port number
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

		onuA.updateDevice(d)
	}()
	return &empty.Empty{}, nil
}

// Single_get_value_request retrieves a single value.
func (onuA *ONUAdapter) Single_get_value_request(ctx context.Context, // nolint
	request extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	logger.Fatalf(ctx, "Single_get_value_request unimplemented")
	return nil, nil
}

// Disable_device disables device
func (onuA *ONUAdapter) DisableDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) { // nolint
	go func() {
		if d := onuA.getDevice(device.Id); d == nil {
			logger.Fatalf(ctx, "device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)
		// Update the all ports state on that device to disable
		c, err := onuA.GetCoreClient()
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
		cloned.ConnectStatus = common.ConnectStatus_UNREACHABLE
		cloned.OperStatus = common.OperStatus_UNKNOWN

		if _, err := c.DeviceStateUpdate(context.TODO(), &ca.DeviceStateFilter{
			DeviceId:   cloned.Id,
			OperStatus: cloned.OperStatus,
			ConnStatus: cloned.ConnectStatus,
		}); err != nil {
			// Device may already have been deleted in the core
			logger.Warnw(ctx, "device-state-update-failed", log.Fields{"device-id": device.Id, "error": err})
			return
		}

		onuA.updateDevice(cloned)

	}()
	return &empty.Empty{}, nil
}

func (onuA *ONUAdapter) ReEnableDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) { // nolint
	go func() {
		if d := onuA.getDevice(device.Id); d == nil {
			logger.Fatalf(ctx, "device-not-found-%s", device.Id)
		}

		cloned := proto.Clone(device).(*voltha.Device)

		c, err := onuA.GetCoreClient()
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
		cloned.ConnectStatus = common.ConnectStatus_REACHABLE
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
		onuA.updateDevice(cloned)
	}()
	return &empty.Empty{}, nil
}

func (onuA *ONUAdapter) StartOmciTest(ctx context.Context, _ *ca.OMCITest) (*omci.TestResponse, error) { // nolint
	return &omci.TestResponse{Result: omci.TestResponse_SUCCESS}, nil
}

func (onuA *ONUAdapter) GetHealthStatus(stream adapter_service.AdapterService_GetHealthStatusServer) error {
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

func (onuA *ONUAdapter) DisableOnuSerialNumber(ctx context.Context, in *voltha.OnuSerialNumberOnOLTPon) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (onuA *ONUAdapter) EnableOnuSerialNumber(ctx context.Context, in *voltha.OnuSerialNumberOnOLTPon) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (onuA *ONUAdapter) DisableOnuDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (onuA *ONUAdapter) EnableOnuDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}
