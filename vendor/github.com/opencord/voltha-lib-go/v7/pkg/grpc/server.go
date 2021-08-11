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
	"net"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

/*
To add a GRPC server to your existing component simply follow these steps:

1. Create a server instance by passing the host and port where it should run and optionally add certificate information

	e.g.
	s.server = server.NewGrpcServer(s.config.GrpcHost, s.config.GrpcPort, nil, false)

2. Create a function that will register your service with the GRPC server

    e.g.
	f := func(gs *grpc.Server) {
		voltha.RegisterVolthaReadOnlyServiceServer(
			gs,
			core.NewReadOnlyServiceHandler(s.root),
		)
	}

3. Add the service to the server

    e.g.
	s.server.AddService(f)

4. Start the server

	s.server.Start(ctx)
*/

// Interface allows probes to be attached to server
// A probe must support the IsReady() method
type ReadyProbe interface {
	IsReady() bool
}

type GrpcServer struct {
	gs       *grpc.Server
	address  string
	secure   bool
	services []func(*grpc.Server)
	probe    ReadyProbe // optional

	*GrpcSecurity
}

/*
Instantiate a GRPC server data structure
*/
func NewGrpcServer(
	address string,
	certs *GrpcSecurity,
	secure bool,
	probe ReadyProbe,
) *GrpcServer {
	server := &GrpcServer{
		address:      address,
		secure:       secure,
		GrpcSecurity: certs,
		probe:        probe,
	}
	return server
}

/*
Start prepares the GRPC server and starts servicing requests
*/
func (s *GrpcServer) Start(ctx context.Context) {

	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		logger.Fatalf(ctx, "failed to listen: %v", err)
	}

	// Use Intercepters to automatically inject and publish Open Tracing Spans by this GRPC server
	serverOptions := []grpc.ServerOption{
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_opentracing.StreamServerInterceptor(grpc_opentracing.WithTracer(log.ActiveTracerProxy{})),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_opentracing.UnaryServerInterceptor(grpc_opentracing.WithTracer(log.ActiveTracerProxy{})),
			mkServerInterceptor(s),
		))}

	if s.secure && s.GrpcSecurity != nil {
		creds, err := credentials.NewServerTLSFromFile(s.CertFile, s.KeyFile)
		if err != nil {
			logger.Fatalf(ctx, "could not load TLS keys: %s", err)
		}

		serverOptions = append(serverOptions, grpc.Creds(creds))
		s.gs = grpc.NewServer(serverOptions...)
	} else {
		logger.Info(ctx, "starting-insecure-grpc-server")
		s.gs = grpc.NewServer(serverOptions...)
	}

	// Register all required services
	for _, service := range s.services {
		service(s.gs)
	}
	reflection.Register(s.gs)

	if err := s.gs.Serve(lis); err != nil {
		logger.Fatalf(ctx, "failed to serve: %v\n", err)
	}
}

// Make a serverInterceptor for the given GrpcServer
// This interceptor will check whether there is an attached probe,
// and if that probe indicates NotReady, then an UNAVAILABLE
// response will be returned.
func mkServerInterceptor(s *GrpcServer) func(ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	return func(ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {

		if (s.probe != nil) && (!s.probe.IsReady()) {
			logger.Warnf(ctx, "Grpc request received while not ready %v", req)
			return nil, status.Error(codes.Unavailable, "system is not ready")
		}

		// Calls the handler
		h, err := handler(ctx, req)

		return h, err
	}
}

/*
Stop servicing GRPC requests
*/
func (s *GrpcServer) Stop() {
	if s.gs != nil {
		s.gs.Stop()
	}
}

/*
AddService appends a generic service request function
*/
func (s *GrpcServer) AddService(
	registerFunction func(*grpc.Server),
) {
	s.services = append(s.services, registerFunction)
}
