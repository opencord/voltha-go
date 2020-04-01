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
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"net"
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
	port     int
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
	port int,
	certs *GrpcSecurity,
	secure bool,
	probe ReadyProbe,
) *GrpcServer {
	server := &GrpcServer{
		address:      address,
		port:         port,
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

	host := fmt.Sprintf("%s:%d", s.address, s.port)

	lis, err := net.Listen("tcp", host)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}

	if s.secure && s.GrpcSecurity != nil {
		creds, err := credentials.NewServerTLSFromFile(s.CertFile, s.KeyFile)
		if err != nil {
			logger.Fatalf("could not load TLS keys: %s", err)
		}
		s.gs = grpc.NewServer(grpc.Creds(creds),
			withServerUnaryInterceptor(s))

	} else {
		logger.Info("starting-insecure-grpc-server")
		s.gs = grpc.NewServer(withServerUnaryInterceptor(s))
	}

	// Register all required services
	for _, service := range s.services {
		service(s.gs)
	}

	if err := s.gs.Serve(lis); err != nil {
		logger.Fatalf("failed to serve: %v\n", err)
	}
}

func withServerUnaryInterceptor(s *GrpcServer) grpc.ServerOption {
	return grpc.UnaryInterceptor(mkServerInterceptor(s))
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
			logger.Warnf("Grpc request received while not ready %v", req)
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
