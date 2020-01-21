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
package core

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	cm "github.com/opencord/voltha-go/rw_core/mocks"
	com "github.com/opencord/voltha-lib-go/v3/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	lm "github.com/opencord/voltha-lib-go/v3/pkg/mocks"
	of "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	coreName       = "rw_core"
	adapterName    = "adapter_mock"
	coreInstanceID = "1000"
)

var (
	coreKafkaICProxy    *kafka.InterContainerProxy
	adapterKafkaICProxy *kafka.InterContainerProxy
	kc                  kafka.Client
	adapterReqHandler   *com.RequestHandlerProxy
	adapter             *cm.Adapter
)

func init() {
	if _, err := log.SetDefaultLogger(log.JSON, 0, log.Fields{"instanceId": coreInstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	// Set the log level to Warning
	log.SetAllLogLevel(2)

	var err error

	// Create the KV client
	kc = lm.NewKafkaClient()

	// Setup core inter-container proxy and core request handler
	if coreKafkaICProxy, err = kafka.NewInterContainerProxy(
		kafka.MsgClient(kc),
		kafka.DefaultTopic(&kafka.Topic{Name: coreName})); err != nil || coreKafkaICProxy == nil {
		log.Fatalw("Failure-creating-core-intercontainerProxy", log.Fields{"error": err})

	}
	if err = coreKafkaICProxy.Start(); err != nil {
		log.Fatalw("Failure-starting-core-kafka-intercontainerProxy", log.Fields{"error": err})
	}
	if err = coreKafkaICProxy.SubscribeWithDefaultRequestHandler(kafka.Topic{Name: coreName}, 0); err != nil {
		log.Fatalw("Failure-subscribing-core-request-handler", log.Fields{"error": err})
	}

	// Setup adapter inter-container proxy and adapter request handler
	adapterCoreProxy := com.NewCoreProxy(nil, adapterName, coreName)
	adapter = cm.NewAdapter(adapterCoreProxy)
	adapterReqHandler = com.NewRequestHandlerProxy(coreInstanceID, adapter, adapterCoreProxy)
	if adapterKafkaICProxy, err = kafka.NewInterContainerProxy(
		kafka.MsgClient(kc),
		kafka.DefaultTopic(&kafka.Topic{Name: adapterName}),
		kafka.RequestHandlerInterface(adapterReqHandler)); err != nil || adapterKafkaICProxy == nil {
		log.Fatalw("Failure-creating-adapter-intercontainerProxy", log.Fields{"error": err})
	}
	if err = adapterKafkaICProxy.Start(); err != nil {
		log.Fatalw("Failure-starting-adapter-kafka-intercontainerProxy", log.Fields{"error": err})
	}
	if err = adapterKafkaICProxy.SubscribeWithDefaultRequestHandler(kafka.Topic{Name: adapterName}, 0); err != nil {
		log.Fatalw("Failure-subscribing-adapter-request-handler", log.Fields{"error": err})
	}
}

func getRandomBytes(size int) (bytes []byte, err error) {
	bytes = make([]byte, size)
	_, err = rand.Read(bytes)
	return
}

func TestCreateAdapterProxy(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	assert.NotNil(t, ap)
}

func testSimpleRequests(t *testing.T) {
	type simpleRequest func(context.Context, *voltha.Device) error
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	simpleRequests := []simpleRequest{
		ap.AdoptDevice,
		ap.DisableDevice,
		ap.RebootDevice,
		ap.DeleteDevice,
		ap.ReconcileDevice,
		ap.ReEnableDevice,
	}
	for _, f := range simpleRequests {
		//Success
		d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		err := f(ctx, d)
		cancel()
		assert.Nil(t, err)

		//	Failure - invalid adapter
		expectedError := status.Error(codes.Canceled, "context deadline exceeded")
		d = &voltha.Device{Id: "deviceId", Adapter: "adapter_mock_1"}
		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
		err = f(ctx, d)
		cancel()
		assert.NotNil(t, err)
		assert.Equal(t, expectedError.Error(), err.Error())

		// Failure - short timeout
		expectedError = status.Error(codes.Canceled, "context deadline exceeded")
		d = &voltha.Device{Id: "deviceId", Adapter: adapterName}
		ctx, cancel = context.WithTimeout(context.Background(), 100*time.Nanosecond)
		err = f(ctx, d)
		cancel()
		assert.NotNil(t, err)
		assert.Equal(t, expectedError.Error(), err.Error())
	}
}

func testGetSwitchCapabilityFromAdapter(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	switchCap, err := ap.GetOfpDeviceInfo(ctx, d)
	cancel()
	assert.Nil(t, err)
	assert.NotNil(t, switchCap)
	expectedCap, _ := adapter.Get_ofp_device_info(d)
	assert.Equal(t, switchCap.String(), expectedCap.String())
}

func testGetPortInfoFromAdapter(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	portNo := uint32(1)
	portInfo, err := ap.GetOfpPortInfo(ctx, d, portNo)
	cancel()
	assert.Nil(t, err)
	assert.NotNil(t, portInfo)
	expectedPortInfo, _ := adapter.Get_ofp_port_info(d, int64(portNo))
	assert.Equal(t, portInfo.String(), expectedPortInfo.String())
}

func testPacketOut(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	outPort := uint32(1)
	packet, err := getRandomBytes(50)
	assert.Nil(t, err)
	err = ap.packetOut(adapterName, d.Id, outPort, &of.OfpPacketOut{Data: packet})
	assert.Nil(t, err)
}

func testFlowUpdates(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	err := ap.UpdateFlowsBulk(d, &voltha.Flows{}, &voltha.FlowGroups{}, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	flowChanges := &voltha.FlowChanges{ToAdd: &voltha.Flows{Items: nil}, ToRemove: &voltha.Flows{Items: nil}}
	groupChanges := &voltha.FlowGroupChanges{ToAdd: &voltha.FlowGroups{Items: nil}, ToRemove: &voltha.FlowGroups{Items: nil}, ToUpdate: &voltha.FlowGroups{Items: nil}}
	err = ap.UpdateFlowsIncremental(d, flowChanges, groupChanges, &voltha.FlowMetadata{})
	assert.Nil(t, err)
}

func testPmUpdates(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName)
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := ap.UpdatePmConfigs(ctx, d, &voltha.PmConfigs{})
	cancel()
	assert.Nil(t, err)
}

func TestSuite(t *testing.T) {
	//1. Test the simple requests first
	testSimpleRequests(t)

	//2.  Test get switch capability
	testGetSwitchCapabilityFromAdapter(t)

	//3.  Test get port info
	testGetPortInfoFromAdapter(t)

	//4. Test PacketOut
	testPacketOut(t)

	//	5. Test flow updates
	testFlowUpdates(t)

	//6. Pm configs
	testPmUpdates(t)
}
