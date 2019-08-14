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
package adaptercore

import (
	"context"
	"errors"
	"fmt"
	com "github.com/opencord/voltha-go/adapters/common"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/kafka"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"github.com/opencord/voltha-protos/go/openflow_13"
	"github.com/opencord/voltha-protos/go/voltha"
	"sync"
)

type SimulatedOLT struct {
	deviceHandlers        map[string]*DeviceHandler
	coreProxy             *com.CoreProxy
	kafkaICProxy          *kafka.InterContainerProxy
	numOnus               int
	exitChannel           chan int
	lockDeviceHandlersMap sync.RWMutex
}

func NewSimulatedOLT(ctx context.Context, kafkaICProxy *kafka.InterContainerProxy, coreProxy *com.CoreProxy, onuNumber int) *SimulatedOLT {
	var simulatedOLT SimulatedOLT
	simulatedOLT.exitChannel = make(chan int, 1)
	simulatedOLT.deviceHandlers = make(map[string]*DeviceHandler)
	simulatedOLT.kafkaICProxy = kafkaICProxy
	simulatedOLT.numOnus = onuNumber
	simulatedOLT.coreProxy = coreProxy
	simulatedOLT.lockDeviceHandlersMap = sync.RWMutex{}
	return &simulatedOLT
}

func (so *SimulatedOLT) Start(ctx context.Context) error {
	log.Info("starting-device-manager")
	log.Info("device-manager-started")
	return nil
}

func (so *SimulatedOLT) Stop(ctx context.Context) error {
	log.Info("stopping-device-manager")
	so.exitChannel <- 1
	log.Info("device-manager-stopped")
	return nil
}

func sendResponse(ctx context.Context, ch chan interface{}, result interface{}) {
	if ctx.Err() == nil {
		// Returned response only of the ctx has not been cancelled/timeout/etc
		// Channel is automatically closed when a context is Done
		ch <- result
		log.Debugw("sendResponse", log.Fields{"result": result})
	} else {
		// Should the transaction be reverted back?
		log.Debugw("sendResponse-context-error", log.Fields{"context-error": ctx.Err()})
	}
}

func (so *SimulatedOLT) addDeviceHandlerToMap(agent *DeviceHandler) {
	so.lockDeviceHandlersMap.Lock()
	defer so.lockDeviceHandlersMap.Unlock()
	if _, exist := so.deviceHandlers[agent.deviceId]; !exist {
		so.deviceHandlers[agent.deviceId] = agent
	}
}

func (so *SimulatedOLT) deleteDeviceHandlerToMap(agent *DeviceHandler) {
	so.lockDeviceHandlersMap.Lock()
	defer so.lockDeviceHandlersMap.Unlock()
	delete(so.deviceHandlers, agent.deviceId)
}

func (so *SimulatedOLT) getDeviceHandler(deviceId string) *DeviceHandler {
	so.lockDeviceHandlersMap.Lock()
	defer so.lockDeviceHandlersMap.Unlock()
	if agent, ok := so.deviceHandlers[deviceId]; ok {
		return agent
	}
	return nil
}

func (so *SimulatedOLT) Adopt_device(device *voltha.Device) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Infow("adopt-device", log.Fields{"deviceId": device.Id})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler == nil {
		handler := NewDeviceHandler(so.coreProxy, device, so)
		so.addDeviceHandlerToMap(handler)
		go handler.AdoptDevice(device)
	}
	return nil
}

func (so *SimulatedOLT) Get_ofp_device_info(device *voltha.Device) (*ic.SwitchCapability, error) {
	log.Infow("Get_ofp_device_info", log.Fields{"deviceId": device.Id})
	if handler := so.getDeviceHandler(device.Id); handler != nil {
		info, err := handler.GetOfpDeviceInfo(device)
		log.Infow("Get_ofp_device_info-resp", log.Fields{"switch": info})
		return info, err
	}
	log.Errorw("device-handler-not-set", log.Fields{"deviceId": device.Id})
	return nil, errors.New("device-handler-not-set")
}

func (so *SimulatedOLT) Get_ofp_port_info(device *voltha.Device, port_no int64) (*ic.PortCapability, error) {
	log.Infow("Get_ofp_port_info", log.Fields{"deviceId": device.Id})
	if handler := so.getDeviceHandler(device.Id); handler != nil {
		return handler.GetOfpPortInfo(device, port_no)
	}
	log.Errorw("device-handler-not-set", log.Fields{"deviceId": device.Id})
	return nil, errors.New("device-handler-not-set")
}

func (so *SimulatedOLT) Process_inter_adapter_message(msg *ic.InterAdapterMessage) error {
	log.Infow("Process_inter_adapter_message", log.Fields{"msgId": msg.Header.Id})
	targetDevice := msg.Header.ProxyDeviceId // Request?
	if targetDevice == "" && msg.Header.ToDeviceId != "" {
		// Typical response
		targetDevice = msg.Header.ToDeviceId
	}
	if handler := so.getDeviceHandler(targetDevice); handler != nil {
		return handler.Process_inter_adapter_message(msg)
	}
	return errors.New(fmt.Sprintf("handler-not-found-%s", targetDevice))
}

func (so *SimulatedOLT) Adapter_descriptor() error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Device_types() (*voltha.DeviceTypes, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedOLT) Health() (*voltha.HealthStatus, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedOLT) Reconcile_device(device *voltha.Device) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Infow("reconcile-device", log.Fields{"deviceId": device.Id})
	var handler *DeviceHandler
	handler = so.getDeviceHandler(device.Id)
	if handler == nil {
		//	Adapter has restarted
		handler = NewDeviceHandler(so.coreProxy, device, so)
		so.addDeviceHandlerToMap(handler)
	}
	go handler.ReconcileDevice(device)
	return nil
}

func (so *SimulatedOLT) Abandon_device(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Disable_device(device *voltha.Device) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Infow("disable-device", log.Fields{"deviceId": device.Id})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler != nil {
		go handler.DisableDevice(device)
	}
	return nil
}

func (so *SimulatedOLT) Reenable_device(device *voltha.Device) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Infow("reenable-device", log.Fields{"deviceId": device.Id})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler != nil {
		go handler.ReEnableDevice(device)
	}
	return nil
}

func (so *SimulatedOLT) Reboot_device(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Self_test_device(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Delete_device(device *voltha.Device) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Infow("delete-device", log.Fields{"deviceId": device.Id})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler != nil {
		go handler.DeleteDevice(device)
	}
	return nil
}

func (so *SimulatedOLT) Get_device_details(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Update_flows_bulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, metadata *voltha.FlowMetadata) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Debugw("bulk-flow-updates", log.Fields{"deviceId": device.Id, "flows": flows, "groups": groups})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler != nil {
		go handler.UpdateFlowsBulk(device, flows, groups, metadata)
	}
	return nil
}

func (so *SimulatedOLT) Update_flows_incrementally(device *voltha.Device, flowChanges *openflow_13.FlowChanges, groupChanges *openflow_13.FlowGroupChanges, metadata *voltha.FlowMetadata) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Debugw("incremental-flow-update", log.Fields{"deviceId": device.Id, "flowChanges": flowChanges, "groupChanges": groupChanges})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler != nil {
		go handler.UpdateFlowsIncremental(device, flowChanges, groupChanges, metadata)
	}
	return nil
}

func (so *SimulatedOLT) Update_pm_config(device *voltha.Device, pmConfigs *voltha.PmConfigs) error {
	if device == nil {
		log.Warn("device-is-nil")
		return errors.New("nil-device")
	}
	log.Debugw("update_pm_config", log.Fields{"deviceId": device.Id, "pmConfigs": pmConfigs})
	var handler *DeviceHandler
	if handler = so.getDeviceHandler(device.Id); handler != nil {
		go handler.UpdatePmConfigs(device, pmConfigs)
	}
	return nil
}

func (so *SimulatedOLT) Receive_packet_out(deviceId string, egress_port_no int, msg *openflow_13.OfpPacketOut) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Suppress_alarm(filter *voltha.AlarmFilter) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Unsuppress_alarm(filter *voltha.AlarmFilter) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedOLT) Download_image(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedOLT) Get_image_download_status(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedOLT) Cancel_image_download(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedOLT) Activate_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedOLT) Revert_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}
