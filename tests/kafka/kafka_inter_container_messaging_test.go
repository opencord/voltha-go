// +build integration

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
package kafka

import (
	"context"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	rhp "github.com/opencord/voltha-go/rw_core/core"
	kk "github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
)

/*
Prerequite:  Start the kafka/zookeeper containers.
*/

const (
	TEST_RPC_KEY = ""
)

var coreKafkaProxy *kk.InterContainerProxy
var adapterKafkaProxy *kk.InterContainerProxy
var kafkaPartitionClient kk.Client
var affinityRouterTopic string
var hostIP string
var kafkaClient kk.Client

func init() {
	affinityRouterTopic = "AffinityRouter"
	hostIP = os.Getenv("DOCKER_HOST_IP")
	kafkaClient = kk.NewSaramaClient(
		kk.Host(hostIP),
		kk.Port(9092))

	coreKafkaProxy = kk.NewInterContainerProxy(
		kk.InterContainerHost(hostIP),
		kk.InterContainerPort(9092),
		kk.DefaultTopic(&kk.Topic{Name: "Core"}),
		kk.MsgClient(kafkaClient),
		kk.DeviceDiscoveryTopic(&kk.Topic{Name: affinityRouterTopic}))

	adapterKafkaProxy = kk.NewInterContainerProxy(
		kk.InterContainerHost(hostIP),
		kk.InterContainerPort(9092),
		kk.DefaultTopic(&kk.Topic{Name: "Adapter"}),
		kk.MsgClient(kafkaClient))

	kafkaPartitionClient = kk.NewSaramaClient(
		kk.ConsumerType(kk.PartitionConsumer),
		kk.Host(hostIP),
		kk.Port(9092),
		kk.AutoCreateTopic(true),
		kk.ProducerFlushFrequency(5))
	kafkaPartitionClient.Start()

	coreKafkaProxy.Start()
	adapterKafkaProxy.Start()
	subscribeTarget(coreKafkaProxy)
}

func subscribeTarget(kmp *kk.InterContainerProxy) {
	topic := kk.Topic{Name: "Core"}
	requestProxy := &rhp.AdapterRequestHandlerProxy{TestMode: true}
	kmp.SubscribeWithRequestHandlerInterface(topic, requestProxy)
}

func waitForRPCMessage(topic kk.Topic, ch <-chan *ic.InterContainerMessage, doneCh chan string) {
	for msg := range ch {
		logger.Debugw("Got-RPC-message", log.Fields{"msg": msg})
		//	Unpack message
		requestBody := &ic.InterContainerRequestBody{}
		if err := ptypes.UnmarshalAny(msg.Body, requestBody); err != nil {
			doneCh <- "Error"
		} else {
			doneCh <- requestBody.Rpc
		}
		break
	}
}

//func TestSubscribeUnsubscribe(t *testing.T) {
//	// First subscribe to the specific topic
//	topic := kk.Topic{Name: "Core"}
//	ch, err := coreKafkaProxy.Subs(topic)
//	assert.NotNil(t, ch)
//	assert.Nil(t, err)
//	// Create a channel to receive a response
//	waitCh := make(chan string)
//	// Wait for a message
//	go waitForRPCMessage(topic, ch, waitCh)
//	// Send the message - don't care of the response
//	rpc := "AnyRPCRequestForTest"
//	adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, true)
//	// Wait for the result on ouw own channel
//	result := <-waitCh
//	assert.Equal(t, result, rpc)
//	close(waitCh)
//	err = coreKafkaProxy.UnSubscribe(topic, ch)
//	assert.Nil(t, err)
//}
//
//func TestMultipleSubscribeUnsubscribe(t *testing.T) {
//	// First subscribe to the specific topic
//	//log.SetPackageLogLevel("github.com/opencord/voltha-lib-go/v3/pkg/kafka", log.DebugLevel)
//	var err error
//	var ch1 <-chan *ic.InterContainerMessage
//	var ch2 <-chan *ic.InterContainerMessage
//	topic := kk.Topic{Name: "Core"}
//	ch1, err = coreKafkaProxy.Subscribe(topic)
//	assert.NotNil(t, ch1)
//	assert.Nil(t, err)
//	// Create a channel to receive responses
//	waitCh := make(chan string)
//	ch2, err = coreKafkaProxy.Subscribe(topic)
//	assert.NotNil(t, ch2)
//	assert.Nil(t, err)
//	// Wait for a message
//	go waitForRPCMessage(topic, ch2, waitCh)
//	go waitForRPCMessage(topic, ch1, waitCh)
//
//	// Send the message - don't care of the response
//	rpc := "AnyRPCRequestForTest"
//	adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, true)
//	// Wait for the result on ouw own channel
//
//	responses := 0
//	for msg := range waitCh {
//		assert.Equal(t, msg, rpc)
//		responses = responses + 1
//		if responses > 1 {
//			break
//		}
//	}
//	assert.Equal(t, responses, 2)
//	close(waitCh)
//	err = coreKafkaProxy.UnSubscribe(topic, ch1)
//	assert.Nil(t, err)
//	err = coreKafkaProxy.UnSubscribe(topic, ch2)
//	assert.Nil(t, err)
//}

func TestIncorrectAPI(t *testing.T) {
	log.SetPackageLogLevel("github.com/opencord/voltha-lib-go/v3/pkg/kafka", log.ErrorLevel)
	trnsId := uuid.New().String()
	protoMsg := &voltha.Device{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "device",
		Value: protoMsg,
	}
	rpc := "IncorrectAPI"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, false)
	//Unpack the result into the actual proto object
	unpackResult := &ic.Error{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.NotNil(t, unpackResult)
}

func TestIncorrectAPIParams(t *testing.T) {
	trnsId := uuid.New().String()
	protoMsg := &voltha.Device{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "device",
		Value: protoMsg,
	}
	rpc := "GetDevice"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, false)
	//Unpack the result into the actual proto object
	unpackResult := &ic.Error{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.NotNil(t, unpackResult)
}

func TestGetDevice(t *testing.T) {
	trnsId := uuid.New().String()
	protoMsg := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoMsg,
	}
	rpc := "GetDevice"
	topic := kk.Topic{Name: "Core"}
	expectedResponse := &voltha.Device{Id: trnsId}
	timeout := time.Duration(50) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(ctx, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	unpackResult := &voltha.Device{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.Equal(t, unpackResult, expectedResponse)
}

func TestGetDeviceTimeout(t *testing.T) {
	trnsId := uuid.New().String()
	protoMsg := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoMsg,
	}
	rpc := "GetDevice"
	topic := kk.Topic{Name: "Core"}
	timeout := time.Duration(2) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(ctx, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, false)
	unpackResult := &ic.Error{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.NotNil(t, unpackResult)
}

func TestGetChildDevice(t *testing.T) {
	trnsId := uuid.New().String()
	protoMsg := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoMsg,
	}
	rpc := "GetChildDevice"
	topic := kk.Topic{Name: "Core"}
	expectedResponse := &voltha.Device{Id: trnsId}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	unpackResult := &voltha.Device{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.Equal(t, unpackResult, expectedResponse)
}

func TestGetChildDevices(t *testing.T) {
	trnsId := uuid.New().String()
	protoMsg := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoMsg,
	}
	rpc := "GetChildDevices"
	topic := kk.Topic{Name: "Core"}
	expectedResponse := &voltha.Device{Id: trnsId}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	unpackResult := &voltha.Device{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.Equal(t, unpackResult, expectedResponse)
}

func TestGetPorts(t *testing.T) {
	trnsId := uuid.New().String()
	protoArg1 := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 2)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoArg1,
	}
	protoArg2 := &ic.IntType{Val: 1}
	args[1] = &kk.KVArg{
		Key:   "portType",
		Value: protoArg2,
	}
	rpc := "GetPorts"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	unpackResult := &voltha.Ports{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	expectedLen := len(unpackResult.Items) >= 1
	assert.Equal(t, true, expectedLen)
}

func TestGetPortsMissingArgs(t *testing.T) {
	trnsId := uuid.New().String()
	protoArg1 := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 1)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoArg1,
	}
	rpc := "GetPorts"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, false)
	//Unpack the result into the actual proto object
	unpackResult := &ic.Error{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.NotNil(t, unpackResult)
}

func TestChildDeviceDetected(t *testing.T) {
	trnsId := uuid.New().String()
	protoArg1 := &ic.StrType{Val: trnsId}
	args := make([]*kk.KVArg, 5)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoArg1,
	}
	protoArg2 := &ic.IntType{Val: 1}
	args[1] = &kk.KVArg{
		Key:   "parentPortNo",
		Value: protoArg2,
	}
	protoArg3 := &ic.StrType{Val: "great_onu"}
	args[2] = &kk.KVArg{
		Key:   "childDeviceType",
		Value: protoArg3,
	}
	protoArg4 := &voltha.Device_ProxyAddress{DeviceId: trnsId, ChannelId: 100}
	args[3] = &kk.KVArg{
		Key:   "proxyAddress",
		Value: protoArg4,
	}
	protoArg5 := &ic.IntType{Val: 1}
	args[4] = &kk.KVArg{
		Key:   "portType",
		Value: protoArg5,
	}

	rpc := "ChildDeviceDetected"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	assert.Nil(t, result)
}

func TestChildDeviceDetectedNoWait(t *testing.T) {
	trnsId := uuid.New().String()
	protoArg1 := &ic.StrType{Val: trnsId}
	args := make([]*kk.KVArg, 5)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoArg1,
	}
	protoArg2 := &ic.IntType{Val: 1}
	args[1] = &kk.KVArg{
		Key:   "parentPortNo",
		Value: protoArg2,
	}
	protoArg3 := &ic.StrType{Val: "great_onu"}
	args[2] = &kk.KVArg{
		Key:   "childDeviceType",
		Value: protoArg3,
	}
	protoArg4 := &voltha.Device_ProxyAddress{DeviceId: trnsId, ChannelId: 100}
	args[3] = &kk.KVArg{
		Key:   "proxyAddress",
		Value: protoArg4,
	}
	protoArg5 := &ic.IntType{Val: 1}
	args[4] = &kk.KVArg{
		Key:   "portType",
		Value: protoArg5,
	}

	rpc := "ChildDeviceDetected"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, false, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	assert.Nil(t, result)
}

func TestChildDeviceDetectedMissingArgs(t *testing.T) {
	trnsId := uuid.New().String()
	protoArg1 := &ic.StrType{Val: trnsId}
	args := make([]*kk.KVArg, 4)
	args[0] = &kk.KVArg{
		Key:   "deviceID",
		Value: protoArg1,
	}
	protoArg2 := &ic.IntType{Val: 1}
	args[1] = &kk.KVArg{
		Key:   "parentPortNo",
		Value: protoArg2,
	}
	protoArg3 := &ic.StrType{Val: "great_onu"}
	args[2] = &kk.KVArg{
		Key:   "childDeviceType",
		Value: protoArg3,
	}

	rpc := "ChildDeviceDetected"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, false)
	unpackResult := &ic.Error{}
	if err := ptypes.UnmarshalAny(result, unpackResult); err != nil {
		logger.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
	}
	assert.NotNil(t, unpackResult)
}

func TestDeviceStateChange(t *testing.T) {
	log.SetAllLogLevel(log.DebugLevel)
	trnsId := uuid.New().String()
	protoArg1 := &voltha.ID{Id: trnsId}
	args := make([]*kk.KVArg, 4)
	args[0] = &kk.KVArg{
		Key:   "device_id",
		Value: protoArg1,
	}
	protoArg2 := &ic.IntType{Val: 1}
	args[1] = &kk.KVArg{
		Key:   "oper_status",
		Value: protoArg2,
	}
	protoArg3 := &ic.IntType{Val: 1}
	args[2] = &kk.KVArg{
		Key:   "connect_status",
		Value: protoArg3,
	}

	rpc := "DeviceStateUpdate"
	topic := kk.Topic{Name: "Core"}
	start := time.Now()
	status, result := adapterKafkaProxy.InvokeRPC(nil, rpc, &topic, &topic, true, TEST_RPC_KEY, args...)
	elapsed := time.Since(start)
	logger.Infow("Result", log.Fields{"status": status, "result": result, "time": elapsed})
	assert.Equal(t, status, true)
	assert.Nil(t, result)
}

func subscribeToTopic(topic *kk.Topic, waitingChannel chan *ic.InterContainerMessage) error {
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = kafkaPartitionClient.Subscribe(topic); err != nil {
		return nil
	}
	msg := <-ch

	logger.Debugw("msg-received", log.Fields{"msg": msg})
	waitingChannel <- msg
	return nil
}

func TestDeviceDiscovery(t *testing.T) {
	// Create an intercontainer proxy - similar to the Core
	testProxy := kk.NewInterContainerProxy(
		kk.InterContainerHost(hostIP),
		kk.InterContainerPort(9092),
		kk.DefaultTopic(&kk.Topic{Name: "Test"}),
		kk.MsgClient(kafkaClient),
		kk.DeviceDiscoveryTopic(&kk.Topic{Name: affinityRouterTopic}))

	//	First start to wait for the message
	waitingChannel := make(chan *ic.InterContainerMessage)
	go subscribeToTopic(&kk.Topic{Name: affinityRouterTopic}, waitingChannel)

	// Sleep to make sure the consumer is ready
	time.Sleep(time.Millisecond * 100)

	// Send the message
	go testProxy.DeviceDiscovered("TestDeviceId", "TestDevicetype", "TestParentId", "myPODName")

	msg := <-waitingChannel
	totalTime := (time.Now().UnixNano() - msg.Header.Timestamp) / int64(time.Millisecond)
	assert.Equal(t, msg.Header.Type, ic.MessageType_DEVICE_DISCOVERED)
	//	Unpack message
	dd := &ic.DeviceDiscovered{}
	err := ptypes.UnmarshalAny(msg.Body, dd)
	assert.Nil(t, err)
	assert.Equal(t, dd.Id, "TestDeviceId")
	assert.Equal(t, dd.DeviceType, "TestDevicetype")
	assert.Equal(t, dd.ParentId, "TestParentId")
	assert.Equal(t, dd.Publisher, "myPODName")
	logger.Debugw("TotalTime", log.Fields{"time": totalTime})
}

func TestStopKafkaProxy(t *testing.T) {
	adapterKafkaProxy.Stop()
	coreKafkaProxy.Stop()
}

//func TestMain(m *testing.T) {
//	logger.Info("Main")
//}
