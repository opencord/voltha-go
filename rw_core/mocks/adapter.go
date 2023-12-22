/*
 * Copyright 2019-2023 Open Networking Foundation (ONF) and the ONF Contributors

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
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/health"
	"google.golang.org/grpc"

	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"
	"github.com/opencord/voltha-protos/v5/go/extension"
	"github.com/opencord/voltha-protos/v5/go/omci"
	"github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

const (
	numONUPerOLT      = 4
	startingUNIPortNo = 100
)

func macAddressToUint32Array(mac string) []uint32 {
	slist := strings.Split(mac, ":")
	result := make([]uint32, len(slist))
	var err error
	var tmp int64
	for index, val := range slist {
		if tmp, err = strconv.ParseInt(val, 16, 32); err != nil {
			return []uint32{1, 2, 3, 4, 5, 6}
		}
		result[index] = uint32(tmp)
	}
	return result
}

// GetNumONUPerOLT returns number of ONUs per OLT
func GetNumONUPerOLT() int {
	return numONUPerOLT
}

// Returns the starting UNI port number
func GetStartingUNIPortNo() int {
	return startingUNIPortNo
}

// Adapter represents adapter attributes
type Adapter struct {
	flows                map[string]map[uint64]*openflow_13.OfpFlowStats
	flowLock             sync.RWMutex
	devices              map[string]*voltha.Device
	deviceLock           sync.RWMutex
	failFlowAdd          map[string]bool
	failFlowAddLock      sync.RWMutex
	failFlowDelete       map[string]bool
	failFlowDeleteLock   sync.RWMutex
	failDeleteDevice     map[string]bool
	failDeleteDeviceLock sync.RWMutex
	coreEnpoint          string
	coreClient           *vgrpc.Client
	serviceEndpoint      string
	DeviceType           string
	vendor               string
	Probe                *probe.Probe
}

// NewAdapter creates adapter instance
func NewAdapter(serviceEndpoint, coreEndpoint, deviceType, vendor string) *Adapter {
	return &Adapter{
		flows:            map[string]map[uint64]*openflow_13.OfpFlowStats{},
		devices:          map[string]*voltha.Device{},
		failFlowAdd:      map[string]bool{},
		failFlowDelete:   map[string]bool{},
		failDeleteDevice: map[string]bool{},
		coreEnpoint:      coreEndpoint,
		serviceEndpoint:  serviceEndpoint,
		DeviceType:       deviceType,
		vendor:           vendor,
	}
}

func (ta *Adapter) IsReady() bool {
	return ta.Probe.IsReady()
}

func (ta *Adapter) storeDevice(d *voltha.Device) {
	ta.deviceLock.Lock()
	defer ta.deviceLock.Unlock()
	if d != nil {
		ta.devices[d.Id] = d
	}
}

func (ta *Adapter) getDevice(id string) *voltha.Device {
	ta.deviceLock.RLock()
	defer ta.deviceLock.RUnlock()
	return ta.devices[id]
}

func (ta *Adapter) updateDevice(d *voltha.Device) {
	ta.storeDevice(d)
}

func (ta *Adapter) GetEndPoint() string {
	return ta.serviceEndpoint
}

func (ta *Adapter) GetCoreClient() (core_service.CoreServiceClient, error) {
	// Wait until the Core is up and running
	for {
		if ta.coreClient != nil {
			client, err := ta.coreClient.GetClient()
			if err != nil {
				logger.Infow(context.Background(), "got-error-core-client", log.Fields{"error": err})
				time.Sleep(1 * time.Second)
				continue
			}
			c, ok := client.(core_service.CoreServiceClient)
			if ok {
				logger.Debug(context.Background(), "got-valid-client")
				return c, nil
			}
		}
		logger.Info(context.Background(), "waiting-for-grpc-core-client")
		time.Sleep(1 * time.Second)
	}
}

// Helper methods
// startGRPCService creates the grpc service handlers, registers it to the grpc server and starts the server
func (ta *Adapter) startGRPCService(ctx context.Context, server *vgrpc.GrpcServer, handler adapter_service.AdapterServiceServer, serviceName string) {
	logger.Infow(ctx, "service-created", log.Fields{"service": serviceName})

	server.AddService(func(gs *grpc.Server) { adapter_service.RegisterAdapterServiceServer(gs, handler) })
	logger.Infow(ctx, "service-added", log.Fields{"service": serviceName})

	ta.Probe.UpdateStatus(ctx, serviceName, probe.ServiceStatusRunning)
	logger.Infow(ctx, "service-started", log.Fields{"service": serviceName})

	// Note that there is a small window here in which the core could return its status as ready,
	// when it really isn't.  This is unlikely to cause issues, as the delay is incredibly short.
	server.Start(ctx)
	ta.Probe.UpdateStatus(ctx, serviceName, probe.ServiceStatusStopped)
}

func setCoreServiceHandler(ctx context.Context, conn *grpc.ClientConn) interface{} {
	if conn == nil {
		return nil
	}
	return core_service.NewCoreServiceClient(conn)
}

// gRPC service
func (ta *Adapter) GetHealthStatus(ctx context.Context, clientConn *common.Connection) (*health.HealthStatus, error) {
	return &health.HealthStatus{State: health.HealthStatus_HEALTHY}, nil
}

// Device

func (ta *Adapter) AdoptDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) ReconcileDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) DeleteDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	ta.failDeleteDeviceLock.RLock()
	haveToFail, ok := ta.failDeleteDevice[device.Id]
	if ok && haveToFail {
		ta.failDeleteDeviceLock.RUnlock()
		return nil, fmt.Errorf("delete-device-failure")
	}
	ta.failDeleteDeviceLock.RUnlock()
	if ok {
		ta.RemoveDevice(device.Id)
	}
	logger.Debugw(ctx, "device-deleted-in-adapter", log.Fields{"device-id": device.Id})
	return &empty.Empty{}, nil
}

func (ta *Adapter) DisableDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) ReEnableDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) RebootDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) SelfTestDevice(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) ChildDeviceLost(ctx context.Context, device *voltha.Device) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) GetOfpDeviceInfo(ctx context.Context, device *voltha.Device) (*ca.SwitchCapability, error) {
	return nil, nil
}

// Ports

func (ta *Adapter) EnablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) DisablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// Flows
func (ta *Adapter) UpdateFlowsBulk(ctx context.Context, flows *ca.BulkFlows) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) UpdateFlowsIncrementally(ctx context.Context, incrFlows *ca.IncrementalFlows) (*empty.Empty, error) {
	ta.flowLock.Lock()
	defer ta.flowLock.Unlock()

	if _, ok := ta.flows[incrFlows.Device.Id]; !ok {
		ta.flows[incrFlows.Device.Id] = map[uint64]*openflow_13.OfpFlowStats{}
	}

	if incrFlows.Flows.ToAdd != nil && len(incrFlows.Flows.ToAdd.Items) > 0 {
		ta.failFlowAddLock.RLock()
		if haveToFail, ok := ta.failFlowAdd[incrFlows.Device.Id]; ok && haveToFail {
			ta.failFlowAddLock.RUnlock()
			return nil, fmt.Errorf("flow-add-error")
		}
		ta.failFlowAddLock.RUnlock()
		for _, f := range incrFlows.Flows.ToAdd.Items {
			ta.flows[incrFlows.Device.Id][f.Id] = f
		}
	}
	if incrFlows.Flows.ToRemove != nil && len(incrFlows.Flows.ToRemove.Items) > 0 {
		ta.failFlowDeleteLock.RLock()
		if haveToFail, ok := ta.failFlowDelete[incrFlows.Device.Id]; ok && haveToFail {
			ta.failFlowDeleteLock.RUnlock()
			return nil, fmt.Errorf("flow-delete-error")
		}
		ta.failFlowDeleteLock.RUnlock()
		for _, f := range incrFlows.Flows.ToRemove.Items {
			delete(ta.flows[incrFlows.Device.Id], f.Id)
		}
	}
	return &empty.Empty{}, nil
}

// Packets
func (ta *Adapter) SendPacketOut(ctx context.Context, packet *ca.PacketOut) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// PM
func (ta *Adapter) UpdatePmConfig(ctx context.Context, configs *ca.PmConfigsInfo) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// Image
func (ta *Adapter) DownloadOnuImage(ctx context.Context, request *voltha.DeviceImageDownloadRequest) (*voltha.DeviceImageResponse, error) {
	return &voltha.DeviceImageResponse{}, nil
}

func (ta *Adapter) GetOnuImageStatus(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return &voltha.DeviceImageResponse{}, nil
}

func (ta *Adapter) AbortOnuImageUpgrade(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return &voltha.DeviceImageResponse{}, nil
}

func (ta *Adapter) GetOnuImages(ctx context.Context, id *common.ID) (*voltha.OnuImages, error) {
	return &voltha.OnuImages{}, nil
}

func (ta *Adapter) ActivateOnuImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return &voltha.DeviceImageResponse{}, nil
}

func (ta *Adapter) CommitOnuImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	return &voltha.DeviceImageResponse{}, nil
}

// Deprecated image APIs
func (ta *Adapter) DownloadImage(ctx context.Context, in *ca.ImageDownloadMessage) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (ta *Adapter) GetImageDownloadStatus(ctx context.Context, in *ca.ImageDownloadMessage) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (ta *Adapter) CancelImageDownload(ctx context.Context, in *ca.ImageDownloadMessage) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (ta *Adapter) ActivateImageUpdate(ctx context.Context, in *ca.ImageDownloadMessage) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

func (ta *Adapter) RevertImageUpdate(ctx context.Context, in *ca.ImageDownloadMessage) (*voltha.ImageDownload, error) {
	return &voltha.ImageDownload{}, nil
}

// OMCI test
func (ta *Adapter) StartOmciTest(ctx context.Context, test *ca.OMCITest) (*omci.TestResponse, error) {
	return nil, nil
}

// Events
func (ta *Adapter) SuppressEvent(ctx context.Context, filter *voltha.EventFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) UnSuppressEvent(ctx context.Context, filter *voltha.EventFilter) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) SimulateAlarm(context.Context, *ca.SimulateAlarmMessage) (*common.OperationResp, error) {
	return &common.OperationResp{}, nil
}

func (ta *Adapter) GetExtValue(context.Context, *ca.GetExtValueMessage) (*extension.ReturnValues, error) {
	return &extension.ReturnValues{}, nil
}

func (ta *Adapter) SetExtValue(context.Context, *ca.SetExtValueMessage) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (ta *Adapter) GetSingleValue(context.Context, *extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	return &extension.SingleGetValueResponse{}, nil
}

func (ta *Adapter) SetSingleValue(context.Context, *extension.SingleSetValueRequest) (*extension.SingleSetValueResponse, error) {
	return &extension.SingleSetValueResponse{}, nil
}

// APIs for test ONLY
// GetFlowCount returns the total number of flows presently under this adapter
func (ta *Adapter) GetFlowCount(deviceID string) int {
	ta.flowLock.RLock()
	defer ta.flowLock.RUnlock()

	if _, ok := ta.flows[deviceID]; ok {
		return len(ta.flows[deviceID])
	}
	return 0
}

// RemoveDevice removes all flows in this adapter
func (ta *Adapter) RemoveDevice(deviceID string) {
	ta.flowLock.Lock()
	defer ta.flowLock.Unlock()
	ta.failFlowAddLock.Lock()
	defer ta.failFlowAddLock.Unlock()
	ta.failFlowDeleteLock.Lock()
	defer ta.failFlowDeleteLock.Unlock()

	delete(ta.flows, deviceID)
	delete(ta.failFlowAdd, deviceID)
	delete(ta.failFlowDelete, deviceID)
}

// SetFlowAction sets the adapter action on addition and deletion of flows
func (ta *Adapter) SetFlowAction(deviceID string, failFlowAdd, failFlowDelete bool) {
	ta.failFlowAddLock.Lock()
	defer ta.failFlowAddLock.Unlock()
	ta.failFlowDeleteLock.Lock()
	defer ta.failFlowDeleteLock.Unlock()
	ta.failFlowAdd[deviceID] = failFlowAdd
	ta.failFlowDelete[deviceID] = failFlowDelete
}

// SetDeleteAction sets the adapter action on delete device
func (ta *Adapter) SetDeleteAction(deviceID string, failDeleteDevice bool) {
	ta.failDeleteDeviceLock.Lock()
	defer ta.failDeleteDeviceLock.Unlock()
	ta.failDeleteDevice[deviceID] = failDeleteDevice
}
# [EOF] - delta:force
