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
package grpc

import (
	"context"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"testing"
)

// A Mock Probe that returns the Ready member using the IsReady() func
type MockReadyProbe struct {
	Ready bool
}

func (m *MockReadyProbe) IsReady() bool {
	return m.Ready
}

// A Mock handler that returns the request as its result
func MockUnaryHandler(ctx context.Context, req interface{}) (interface{}, error) {
	_ = ctx
	return req, nil
}

func TestNewGrpcServer(t *testing.T) {
	server := NewGrpcServer("127.0.0.1:1234", nil, false, nil)
	assert.NotNil(t, server)
}

func TestMkServerInterceptorNoProbe(t *testing.T) {
	server := NewGrpcServer("127.0.0.1:1234", nil, false, nil)
	assert.NotNil(t, server)

	f := mkServerInterceptor(server)
	assert.NotNil(t, f)

	req := "SomeRequest"
	serverInfo := grpc.UnaryServerInfo{Server: nil, FullMethod: "somemethod"}

	result, err := f(context.Background(),
		req,
		&serverInfo,
		MockUnaryHandler)

	assert.Nil(t, err)
	assert.Equal(t, "SomeRequest", result)
}

func TestMkServerInterceptorReady(t *testing.T) {
	probe := &MockReadyProbe{Ready: true}

	server := NewGrpcServer("127.0.0.1:1234", nil, false, probe)
	assert.NotNil(t, server)

	f := mkServerInterceptor(server)
	assert.NotNil(t, f)

	req := "SomeRequest"
	serverInfo := grpc.UnaryServerInfo{Server: nil, FullMethod: "somemethod"}

	result, err := f(context.Background(),
		req,
		&serverInfo,
		MockUnaryHandler)

	assert.Nil(t, err)
	assert.NotNil(t, result)
}

func TestMkServerInterceptorNotReady(t *testing.T) {
	probe := &MockReadyProbe{Ready: false}

	server := NewGrpcServer("127.0.0.1:1234", nil, false, probe)
	assert.NotNil(t, server)

	f := mkServerInterceptor(server)
	assert.NotNil(t, f)

	req := "SomeRequest"
	serverInfo := grpc.UnaryServerInfo{Server: nil, FullMethod: "somemethod"}

	result, err := f(context.Background(),
		req,
		&serverInfo,
		MockUnaryHandler)

	assert.NotNil(t, err)
	assert.Nil(t, result)
}
