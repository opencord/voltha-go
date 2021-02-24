/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package common

import (
	"context"
	"testing"

	adapterIf "github.com/opencord/voltha-lib-go/v4/pkg/adapters/adapterif"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	mocks "github.com/opencord/voltha-lib-go/v4/pkg/mocks/kafka"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCoreProxyImplementsAdapterIfCoreProxy(t *testing.T) {
	proxy := &CoreProxy{}

	if _, ok := interface{}(proxy).(adapterIf.CoreProxy); !ok {
		t.Error("common CoreProxy does not implement adapterif.CoreProxy interface")
	}

}

func TestCoreProxy_RegisterAdapter_default(t *testing.T) {
	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDevice"},
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	adapter := &voltha.Adapter{
		Id:      "testAdapter",
		Vendor:  "ONF",
		Version: "1.0.0",
	}
	types := []*voltha.DeviceType{{
		Id:                    "testolt",
		Adapter:               "testAdapter",
		AcceptsBulkFlowUpdate: true,
	}}
	devices := &voltha.DeviceTypes{Items: types}

	err := proxy.RegisterAdapter(context.TODO(), adapter, devices)

	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)
	assert.Equal(t, nil, err)

	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]
	assert.Equal(t, call.Rpc, "Register")
	assert.Equal(t, call.ToTopic, &kafka.Topic{Name: "testCoreTopic"})
	assert.Equal(t, call.ReplyToTopic, &kafka.Topic{Name: "testAdapterTopic"})
	assert.Equal(t, call.WaitForResponse, true)
	assert.Equal(t, call.Key, "")
	assert.Equal(t, call.KvArgs[0], &kafka.KVArg{Key: "adapter", Value: &voltha.Adapter{
		Id:             adapter.Id,
		Vendor:         adapter.Vendor,
		Version:        adapter.Version,
		CurrentReplica: 1,
		TotalReplicas:  1,
	}})
	assert.Equal(t, call.KvArgs[1], &kafka.KVArg{Key: "deviceTypes", Value: devices})
}

func TestCoreProxy_RegisterAdapter_multiple(t *testing.T) {
	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDevice"},
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	adapter := &voltha.Adapter{
		Id:             "testAdapter",
		Vendor:         "ONF",
		Version:        "1.0.0",
		CurrentReplica: 4,
		TotalReplicas:  8,
	}
	types := []*voltha.DeviceType{{
		Id:                    "testolt",
		Adapter:               "testAdapter",
		AcceptsBulkFlowUpdate: true,
	}}
	devices := &voltha.DeviceTypes{Items: types}

	err := proxy.RegisterAdapter(context.TODO(), adapter, devices)

	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)
	assert.Equal(t, nil, err)

	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]
	assert.Equal(t, call.KvArgs[0], &kafka.KVArg{Key: "adapter", Value: &voltha.Adapter{
		Id:             adapter.Id,
		Vendor:         adapter.Vendor,
		Version:        adapter.Version,
		CurrentReplica: 4,
		TotalReplicas:  8,
	}})
}

func TestCoreProxy_GetChildDevice_sn(t *testing.T) {

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDevice"},
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	kwargs := make(map[string]interface{})
	kwargs["serial_number"] = "TEST00000000001"

	parentDeviceId := "aabbcc"
	device, error := proxy.GetChildDevice(context.TODO(), parentDeviceId, kwargs)

	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)
	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]
	assert.Equal(t, call.Rpc, "GetChildDevice")
	assert.Equal(t, call.ToTopic, &kafka.Topic{Name: "testCoreTopic"})
	assert.Equal(t, call.ReplyToTopic, &kafka.Topic{Name: "testAdapterTopic"})
	assert.Equal(t, call.WaitForResponse, true)
	assert.Equal(t, call.Key, parentDeviceId)
	assert.Equal(t, call.KvArgs[0], &kafka.KVArg{Key: "device_id", Value: &voltha.ID{Id: parentDeviceId}})
	assert.Equal(t, call.KvArgs[1], &kafka.KVArg{Key: "serial_number", Value: &ic.StrType{Val: kwargs["serial_number"].(string)}})

	assert.Equal(t, "testDevice", device.Id)
	assert.Equal(t, nil, error)
}

func TestCoreProxy_GetChildDevice_id(t *testing.T) {

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.Device{Id: "testDevice"},
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	kwargs := make(map[string]interface{})
	kwargs["onu_id"] = uint32(1234)

	parentDeviceId := "aabbcc"
	device, error := proxy.GetChildDevice(context.TODO(), parentDeviceId, kwargs)

	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)
	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]
	assert.Equal(t, call.Rpc, "GetChildDevice")
	assert.Equal(t, call.ToTopic, &kafka.Topic{Name: "testCoreTopic"})
	assert.Equal(t, call.ReplyToTopic, &kafka.Topic{Name: "testAdapterTopic"})
	assert.Equal(t, call.WaitForResponse, true)
	assert.Equal(t, call.Key, parentDeviceId)
	assert.Equal(t, call.KvArgs[0], &kafka.KVArg{Key: "device_id", Value: &voltha.ID{Id: parentDeviceId}})
	assert.Equal(t, call.KvArgs[1], &kafka.KVArg{Key: "onu_id", Value: &ic.IntType{Val: int64(kwargs["onu_id"].(uint32))}})

	assert.Equal(t, "testDevice", device.Id)
	assert.Equal(t, nil, error)
}

func TestCoreProxy_GetChildDevice_fail_timeout(t *testing.T) {

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:   make(map[int]mocks.InvokeRpcArgs),
			Timeout: true,
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	kwargs := make(map[string]interface{})
	kwargs["onu_id"] = uint32(1234)

	parentDeviceId := "aabbcc"
	device, error := proxy.GetChildDevice(context.TODO(), parentDeviceId, kwargs)

	assert.Nil(t, device)
	parsedErr, _ := status.FromError(error)

	assert.Equal(t, parsedErr.Code(), codes.DeadlineExceeded)
}

func TestCoreProxy_GetChildDevice_fail_unmarhsal(t *testing.T) {

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.LogicalDevice{Id: "testDevice"},
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	kwargs := make(map[string]interface{})
	kwargs["onu_id"] = uint32(1234)

	parentDeviceId := "aabbcc"
	device, error := proxy.GetChildDevice(context.TODO(), parentDeviceId, kwargs)

	assert.Nil(t, device)

	parsedErr, _ := status.FromError(error)
	assert.Equal(t, parsedErr.Code(), codes.InvalidArgument)
}

func TestCoreProxy_GetChildDevices_success(t *testing.T) {
	devicesResponse := &voltha.Devices{Items: []*voltha.Device{
		{Id: "testDevice1"},
		{Id: "testDevice2"},
	}}

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: devicesResponse,
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	parentDeviceId := "aabbcc"
	devices, error := proxy.GetChildDevices(context.TODO(), parentDeviceId)

	assert.Equal(t, mockKafkaIcProxy.InvokeRpcSpy.CallCount, 1)
	call := mockKafkaIcProxy.InvokeRpcSpy.Calls[1]
	assert.Equal(t, call.Rpc, "GetChildDevices")
	assert.Equal(t, call.ToTopic, &kafka.Topic{Name: "testCoreTopic"})
	assert.Equal(t, call.ReplyToTopic, &kafka.Topic{Name: "testAdapterTopic"})
	assert.Equal(t, call.WaitForResponse, true)
	assert.Equal(t, call.Key, parentDeviceId)
	assert.Equal(t, call.KvArgs[0], &kafka.KVArg{Key: "device_id", Value: &voltha.ID{Id: parentDeviceId}})

	assert.Equal(t, nil, error)
	assert.Equal(t, 2, len(devices.Items))
}

func TestCoreProxy_GetChildDevices_fail_unmarhsal(t *testing.T) {

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:    make(map[int]mocks.InvokeRpcArgs),
			Response: &voltha.LogicalDevice{Id: "testDevice"},
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	parentDeviceId := "aabbcc"
	devices, error := proxy.GetChildDevices(context.TODO(), parentDeviceId)

	assert.Nil(t, devices)

	parsedErr, _ := status.FromError(error)
	assert.Equal(t, parsedErr.Code(), codes.InvalidArgument)
}

func TestCoreProxy_GetChildDevices_fail_timeout(t *testing.T) {

	var mockKafkaIcProxy = mocks.MockKafkaICProxy{
		InvokeRpcSpy: mocks.InvokeRpcSpy{
			Calls:   make(map[int]mocks.InvokeRpcArgs),
			Timeout: true,
		},
	}

	proxy := NewCoreProxy(context.Background(), &mockKafkaIcProxy, "testAdapterTopic", "testCoreTopic")

	parentDeviceId := "aabbcc"
	devices, error := proxy.GetChildDevices(context.TODO(), parentDeviceId)

	assert.Nil(t, devices)

	parsedErr, _ := status.FromError(error)
	assert.Equal(t, parsedErr.Code(), codes.DeadlineExceeded)
}
