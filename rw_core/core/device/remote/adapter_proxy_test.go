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
package remote

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	any2 "github.com/golang/protobuf/ptypes/any"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	com "github.com/opencord/voltha-lib-go/v4/pkg/adapters/common"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	mock_kafka "github.com/opencord/voltha-lib-go/v4/pkg/mocks/kafka"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	of "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
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
	coreKafkaICProxy    kafka.InterContainerProxy
	adapterKafkaICProxy kafka.InterContainerProxy
	kc                  kafka.Client
	adapterReqHandler   *com.RequestHandlerProxy
	adapter             *cm.Adapter
)

func init() {
	ctx := context.Background()
	if _, err := log.SetDefaultLogger(log.JSON, 0, log.Fields{"instanceId": coreInstanceID}); err != nil {
		logger.With(log.Fields{"error": err}).Fatal(ctx, "Cannot setup logging")
	}
	// Set the log level to Warning
	log.SetAllLogLevel(log.WarnLevel)

	var err error

	// Create the KV client
	kc = mock_kafka.NewKafkaClient()

	// Setup core inter-container proxy and core request handler
	coreKafkaICProxy = kafka.NewInterContainerProxy(
		kafka.MsgClient(kc),
		kafka.DefaultTopic(&kafka.Topic{Name: coreName}))

	if err = coreKafkaICProxy.Start(ctx); err != nil {
		logger.Fatalw(ctx, "Failure-starting-core-kafka-intercontainerProxy", log.Fields{"error": err})
	}
	if err = coreKafkaICProxy.SubscribeWithDefaultRequestHandler(ctx, kafka.Topic{Name: coreName}, 0); err != nil {
		logger.Fatalw(ctx, "Failure-subscribing-core-request-handler", log.Fields{"error": err})
	}

	// Setup adapter inter-container proxy and adapter request handler
	adapterCoreProxy := com.NewCoreProxy(ctx, nil, adapterName, coreName)
	adapter = cm.NewAdapter(adapterCoreProxy)
	adapterReqHandler = com.NewRequestHandlerProxy(coreInstanceID, adapter, adapterCoreProxy)
	adapterKafkaICProxy = kafka.NewInterContainerProxy(
		kafka.MsgClient(kc),
		kafka.DefaultTopic(&kafka.Topic{Name: adapterName}),
		kafka.RequestHandlerInterface(adapterReqHandler))

	if err = adapterKafkaICProxy.Start(ctx); err != nil {
		logger.Fatalw(ctx, "Failure-starting-adapter-kafka-intercontainerProxy", log.Fields{"error": err})
	}
	if err = adapterKafkaICProxy.SubscribeWithDefaultRequestHandler(ctx, kafka.Topic{Name: adapterName}, 0); err != nil {
		logger.Fatalw(ctx, "Failure-subscribing-adapter-request-handler", log.Fields{"error": err})
	}
}

func getRandomBytes(size int) (bytes []byte, err error) {
	bytes = make([]byte, size)
	_, err = rand.Read(bytes)
	return
}

func TestCreateAdapterProxy(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName, mock_kafka.NewEndpointManager())
	assert.NotNil(t, ap)
}

func waitForResponse(ctx context.Context, ch chan *kafka.RpcResponse) (*any2.Any, error) {
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			return nil, status.Error(codes.Aborted, "channel-closed")
		} else if rpcResponse.Err != nil {
			return nil, rpcResponse.Err
		} else {
			return rpcResponse.Reply, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func testSimpleRequests(t *testing.T) {
	type simpleRequest func(context.Context, *voltha.Device) (chan *kafka.RpcResponse, error)
	ap := NewAdapterProxy(coreKafkaICProxy, coreName, mock_kafka.NewEndpointManager())
	simpleRequests := []simpleRequest{
		ap.AdoptDevice,
		ap.DisableDevice,
		ap.RebootDevice,
		ap.DeleteDevice,
		ap.ReconcileDevice,
		ap.ReEnableDevice,
	}
	for _, f := range simpleRequests {
		// Success
		d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		rpcResponse, err := f(ctx, d)
		assert.Nil(t, err)
		_, err = waitForResponse(ctx, rpcResponse)
		assert.Nil(t, err)
		cancel()

		//	Failure - invalid adapter
		expectedError := "context deadline exceeded"
		d = &voltha.Device{Id: "deviceId", Adapter: "adapter_mock_1"}
		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
		rpcResponse, err = f(ctx, d)
		assert.Nil(t, err)
		_, err = waitForResponse(ctx, rpcResponse)
		cancel()
		assert.NotNil(t, err)
		assert.True(t, strings.Contains(err.Error(), expectedError))

		// Failure -  timeout
		d = &voltha.Device{Id: "deviceId", Adapter: adapterName}
		ctx, cancel = context.WithTimeout(context.Background(), 100*time.Nanosecond)
		rpcResponse, err = f(ctx, d)
		assert.Nil(t, err)
		_, err = waitForResponse(ctx, rpcResponse)
		cancel()
		assert.NotNil(t, err)
		assert.True(t, strings.Contains(err.Error(), expectedError))
	}
}

func testGetSwitchCapabilityFromAdapter(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName, mock_kafka.NewEndpointManager())
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rpcResponse, err := ap.GetOfpDeviceInfo(ctx, d)
	assert.Nil(t, err)
	response, err := waitForResponse(ctx, rpcResponse)
	assert.Nil(t, err)
	switchCap := &ic.SwitchCapability{}
	err = ptypes.UnmarshalAny(response, switchCap)
	assert.Nil(t, err)
	assert.NotNil(t, switchCap)
	expectedCap, _ := adapter.Get_ofp_device_info(ctx, d)
	assert.Equal(t, switchCap.String(), expectedCap.String())
}

func testPacketOut(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName, mock_kafka.NewEndpointManager())
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	outPort := uint32(1)
	packet, err := getRandomBytes(50)
	assert.Nil(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	rpcResponse, err := ap.PacketOut(ctx, adapterName, d.Id, outPort, &of.OfpPacketOut{Data: packet})
	assert.Nil(t, err)
	_, err = waitForResponse(ctx, rpcResponse)
	assert.Nil(t, err)
}

func testFlowUpdates(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName, mock_kafka.NewEndpointManager())
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	_, err := ap.UpdateFlowsBulk(context.Background(), d, nil, nil, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	flowChanges := &voltha.FlowChanges{ToAdd: &voltha.Flows{Items: nil}, ToRemove: &voltha.Flows{Items: nil}}
	groupChanges := &voltha.FlowGroupChanges{ToAdd: &voltha.FlowGroups{Items: nil}, ToRemove: &voltha.FlowGroups{Items: nil}, ToUpdate: &voltha.FlowGroups{Items: nil}}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	rpcResponse, err := ap.UpdateFlowsIncremental(ctx, d, flowChanges, groupChanges, &voltha.FlowMetadata{})
	assert.Nil(t, err)
	_, err = waitForResponse(ctx, rpcResponse)
	assert.Nil(t, err)
}

func testPmUpdates(t *testing.T) {
	ap := NewAdapterProxy(coreKafkaICProxy, coreName, mock_kafka.NewEndpointManager())
	d := &voltha.Device{Id: "deviceId", Adapter: adapterName}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rpcResponse, err := ap.UpdatePmConfigs(ctx, d, &voltha.PmConfigs{})
	assert.Nil(t, err)
	_, err = waitForResponse(ctx, rpcResponse)
	assert.Nil(t, err)
}

func TestSuiteAdapterProxy(t *testing.T) {
	//1. Test the simple requests first
	testSimpleRequests(t)

	//2.  Test get switch capability
	testGetSwitchCapabilityFromAdapter(t)

	//3. Test PacketOut
	testPacketOut(t)

	//4. Test flow updates
	testFlowUpdates(t)

	//5. Pm configs
	testPmUpdates(t)
}
