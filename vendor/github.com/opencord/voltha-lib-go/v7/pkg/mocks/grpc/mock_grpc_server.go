/*
 * Copyright 2021-present Open Networking Foundation
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
package grpc

import (
	"context"
	"strconv"

	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/phayes/freeport"
	"google.golang.org/grpc"
)

const (
	mockGrpcServer = "mock-grpc-server"
)

type MockGRPCServer struct {
	ApiEndpoint string
	server      *vgrpc.GrpcServer
	probe       *probe.Probe
}

func NewMockGRPCServer(ctx context.Context) (*MockGRPCServer, error) {
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, err
	}
	s := &MockGRPCServer{
		ApiEndpoint: "127.0.0.1:" + strconv.Itoa(grpcPort),
		probe:       &probe.Probe{},
	}
	probePort, err := freeport.GetFreePort()
	if err != nil {
		return nil, err
	}
	probeEndpoint := "127.0.0.1:" + strconv.Itoa(probePort)
	go s.probe.ListenAndServe(ctx, probeEndpoint)
	s.probe.RegisterService(ctx, mockGrpcServer)
	s.server = vgrpc.NewGrpcServer(s.ApiEndpoint, nil, false, s.probe)

	logger.Infow(ctx, "mock-grpc-server-created", log.Fields{"endpoint": s.ApiEndpoint})
	return s, nil
}

func (s *MockGRPCServer) AddCoreService(ctx context.Context, srv core_service.CoreServiceServer) {
	s.server.AddService(func(server *grpc.Server) {
		core_service.RegisterCoreServiceServer(server, srv)
	})
}

func (s *MockGRPCServer) AddAdapterService(ctx context.Context, srv adapter_service.AdapterServiceServer) {
	s.server.AddService(func(server *grpc.Server) {
		adapter_service.RegisterAdapterServiceServer(server, srv)
	})
}

func (s *MockGRPCServer) Start(ctx context.Context) {
	s.probe.UpdateStatus(ctx, mockGrpcServer, probe.ServiceStatusRunning)
	s.server.Start(ctx)
	s.probe.UpdateStatus(ctx, mockGrpcServer, probe.ServiceStatusStopped)
}

func (s *MockGRPCServer) Stop() {
	if s.server != nil {
		s.server.Stop()
	}
}
