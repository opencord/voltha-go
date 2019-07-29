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

type SimulatedONU struct {
	deviceHandlers        map[string]*DeviceHandler
	coreProxy             *com.CoreProxy
	kafkaICProxy          *kafka.InterContainerProxy
	exitChannel           chan int
	lockDeviceHandlersMap sync.RWMutex
}

func NewSimulatedONU(ctx context.Context, kafkaICProxy *kafka.InterContainerProxy, coreProxy *com.CoreProxy) *SimulatedONU {
	var simulatedOLT SimulatedONU
	simulatedOLT.exitChannel = make(chan int, 1)
	simulatedOLT.deviceHandlers = make(map[string]*DeviceHandler)
	simulatedOLT.kafkaICProxy = kafkaICProxy
	simulatedOLT.coreProxy = coreProxy
	simulatedOLT.lockDeviceHandlersMap = sync.RWMutex{}
	return &simulatedOLT
}

func (so *SimulatedONU) Start(ctx context.Context) error {
	log.Info("starting-device-manager")
	log.Info("device-manager-started")
	return nil
}

func (so *SimulatedONU) Stop(ctx context.Context) error {
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

func (so *SimulatedONU) addDeviceHandlerToMap(agent *DeviceHandler) {
	so.lockDeviceHandlersMap.Lock()
	defer so.lockDeviceHandlersMap.Unlock()
	if _, exist := so.deviceHandlers[agent.deviceId]; !exist {
		so.deviceHandlers[agent.deviceId] = agent
	}
}

func (so *SimulatedONU) deleteDeviceHandlerToMap(agent *DeviceHandler) {
	so.lockDeviceHandlersMap.Lock()
	defer so.lockDeviceHandlersMap.Unlock()
	delete(so.deviceHandlers, agent.deviceId)
}

func (so *SimulatedONU) getDeviceHandler(deviceId string) *DeviceHandler {
	so.lockDeviceHandlersMap.Lock()
	defer so.lockDeviceHandlersMap.Unlock()
	if agent, ok := so.deviceHandlers[deviceId]; ok {
		return agent
	}
	return nil
}

func (so *SimulatedONU) Adopt_device(device *voltha.Device) error {
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

func (so *SimulatedONU) Get_ofp_device_info(device *voltha.Device) (*ic.SwitchCapability, error) {
	log.Infow("not-implemented-for-onu", log.Fields{"deviceId": device.Id})
	return nil, nil
}

func (so *SimulatedONU) Get_ofp_port_info(device *voltha.Device, port_no int64) (*ic.PortCapability, error) {
	log.Infow("Get_ofp_port_info", log.Fields{"deviceId": device.Id})
	if handler := so.getDeviceHandler(device.Id); handler != nil {
		return handler.GetOfpPortInfo(device, port_no)
	}
	log.Errorw("device-handler-not-set", log.Fields{"deviceId": device.Id})
	return nil, errors.New("device-handler-not-set")
}

func (so *SimulatedONU) Process_inter_adapter_message(msg *ic.InterAdapterMessage) error {
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

func (so *SimulatedONU) Adapter_descriptor() error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Device_types() (*voltha.DeviceTypes, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedONU) Health() (*voltha.HealthStatus, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedONU) Reconcile_device(device *voltha.Device) error {
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

func (so *SimulatedONU) Abandon_device(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Disable_device(device *voltha.Device) error {
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

func (so *SimulatedONU) Reenable_device(device *voltha.Device) error {
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

func (so *SimulatedONU) Reboot_device(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Self_test_device(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Delete_device(device *voltha.Device) error {
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

func (so *SimulatedONU) Get_device_details(device *voltha.Device) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Update_flows_bulk(device *voltha.Device, flows *voltha.Flows, groups *voltha.FlowGroups, metadata *voltha.FlowMetadata) error {
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

func (so *SimulatedONU) Update_flows_incrementally(device *voltha.Device, flowChanges *openflow_13.FlowChanges, groupChanges *openflow_13.FlowGroupChanges, metadata *voltha.FlowMetadata) error {
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

func (so *SimulatedONU) Update_pm_config(device *voltha.Device, pmConfigs *voltha.PmConfigs) error {
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

func (so *SimulatedONU) Receive_packet_out(deviceId string, egress_port_no int, msg *openflow_13.OfpPacketOut) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Suppress_alarm(filter *voltha.AlarmFilter) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Unsuppress_alarm(filter *voltha.AlarmFilter) error {
	return errors.New("UnImplemented")
}

func (so *SimulatedONU) Download_image(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedONU) Get_image_download_status(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedONU) Cancel_image_download(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedONU) Activate_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}

func (so *SimulatedONU) Revert_image_update(device *voltha.Device, request *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	return nil, errors.New("UnImplemented")
}
