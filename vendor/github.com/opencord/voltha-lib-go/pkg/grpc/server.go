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
	"github.com/opencord/voltha-lib-go/pkg/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

type GrpcServer struct {
	gs       *grpc.Server
	address  string
	port     int
	secure   bool
	services []func(*grpc.Server)

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
) *GrpcServer {
	server := &GrpcServer{
		address:      address,
		port:         port,
		secure:       secure,
		GrpcSecurity: certs,
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
		log.Fatalf("failed to listen: %v", err)
	}

	if s.secure && s.GrpcSecurity != nil {
		creds, err := credentials.NewServerTLSFromFile(s.CertFile, s.KeyFile)
		if err != nil {
			log.Fatalf("could not load TLS keys: %s", err)
		}
		s.gs = grpc.NewServer(grpc.Creds(creds))

	} else {
		log.Info("starting-insecure-grpc-server")
		s.gs = grpc.NewServer()
	}

	// Register all required services
	for _, service := range s.services {
		service(s.gs)
	}

	if err := s.gs.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v\n", err)
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
