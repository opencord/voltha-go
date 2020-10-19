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

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
)

type InvokeRpcArgs struct {
	Rpc             string
	ToTopic         *kafka.Topic
	ReplyToTopic    *kafka.Topic
	WaitForResponse bool
	Key             string
	ParentDeviceId  string
	KvArgs          map[int]interface{}
}

type InvokeRpcSpy struct {
	CallCount int
	Calls     map[int]InvokeRpcArgs
	Timeout   bool
	Response  proto.Message
}

type InvokeAsyncRpcSpy struct {
	CallCount int
	Calls     map[int]InvokeRpcArgs
	Timeout   bool
	Response  proto.Message
}

type MockKafkaICProxy struct {
	InvokeRpcSpy InvokeRpcSpy
}

func (s *MockKafkaICProxy) Start(ctx context.Context) error { return nil }
func (s *MockKafkaICProxy) GetDefaultTopic() *kafka.Topic {
	t := kafka.Topic{
		Name: "test-topic",
	}
	return &t
}

func (s *MockKafkaICProxy) DeleteTopic(ctx context.Context, topic kafka.Topic) error { return nil }

func (s *MockKafkaICProxy) Stop(ctx context.Context) {}

func (s *MockKafkaICProxy) InvokeAsyncRPC(ctx context.Context, rpc string, toTopic *kafka.Topic, replyToTopic *kafka.Topic,
	waitForResponse bool, key string, kvArgs ...*kafka.KVArg) chan *kafka.RpcResponse {

	args := make(map[int]interface{}, 4)
	for k, v := range kvArgs {
		args[k] = v
	}

	s.InvokeRpcSpy.Calls[s.InvokeRpcSpy.CallCount] = InvokeRpcArgs{
		Rpc:             rpc,
		ToTopic:         toTopic,
		ReplyToTopic:    replyToTopic,
		WaitForResponse: waitForResponse,
		Key:             key,
		KvArgs:          args,
	}

	chnl := make(chan *kafka.RpcResponse)

	return chnl
}

func (s *MockKafkaICProxy) InvokeRPC(ctx context.Context, rpc string, toTopic *kafka.Topic, replyToTopic *kafka.Topic, waitForResponse bool, key string, kvArgs ...*kafka.KVArg) (bool, *any.Any) {
	s.InvokeRpcSpy.CallCount++

	success := true

	args := make(map[int]interface{}, 4)
	for k, v := range kvArgs {
		args[k] = v
	}

	s.InvokeRpcSpy.Calls[s.InvokeRpcSpy.CallCount] = InvokeRpcArgs{
		Rpc:             rpc,
		ToTopic:         toTopic,
		ReplyToTopic:    replyToTopic,
		WaitForResponse: waitForResponse,
		Key:             key,
		KvArgs:          args,
	}

	var response any.Any
	if s.InvokeRpcSpy.Timeout {

		success = false

		err := &ic.Error{Reason: "context deadline exceeded", Code: ic.ErrorCode_DEADLINE_EXCEEDED}
		res, _ := ptypes.MarshalAny(err)
		response = *res
	} else {
		res, _ := ptypes.MarshalAny(s.InvokeRpcSpy.Response)
		response = *res
	}

	return success, &response
}
func (s *MockKafkaICProxy) SubscribeWithRequestHandlerInterface(ctx context.Context, topic kafka.Topic, handler interface{}) error {
	return nil
}
func (s *MockKafkaICProxy) SubscribeWithDefaultRequestHandler(ctx context.Context, topic kafka.Topic, initialOffset int64) error {
	return nil
}
func (s *MockKafkaICProxy) UnSubscribeFromRequestHandler(ctx context.Context, topic kafka.Topic) error {
	return nil
}
func (s *MockKafkaICProxy) EnableLivenessChannel(ctx context.Context, enable bool) chan bool {
	return nil
}
func (s *MockKafkaICProxy) SendLiveness(ctx context.Context) error { return nil }
