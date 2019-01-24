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
// gRPC affinity router with active/active backends

package afrouter

import (
	"fmt"
	"net"
	"regexp"
	"errors"
	"strconv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"github.com/opencord/voltha-go/common/log"
)

var (
	clientStreamDescForProxying = &grpc.StreamDesc{
		ServerStreams: true,
		ClientStreams: true,
	}
)
const (
	REQ_ALL = 0
	REQ_PACKAGE = 1
	REQ_SERVICE = 2
	REQ_METHOD = 3
)

type server struct {
	running bool
	name string
	stype nbi
	proxyListener net.Listener
	routers map[string]map[string]Router
	proxyServer *grpc.Server
}

type nbRequest struct {
	srv interface{}
	serverStream grpc.ServerStream
	r Router
	mthdSlice []string
	metaKey string  // There should be at most one key specified. More than one is an error.
	metaVal string  // This is the value extracted from the meta key if it exists or "" otherwise
}

var mthdSlicerExp string = `^/([a-zA-Z][a-zA-Z0-9]+)\.([a-zA-Z][a-zA-Z0-9]+)/([a-zA-Z][a-zA-Z0-9]+)`
var mthdSlicer *regexp.Regexp // The compiled regex to extract the package/service/method


func newServer(config *ServerConfig) (*server,error) {
	var err error = nil
	var rtrn_err bool = false
	var srvr *server
	// Change over to the new configuration format
	// Validate the configuration
	// There should be a name
	if config.Name == "" {
	    log.Error("A server has been defined with no name")
		rtrn_err = true
	}
	// Validate that there's a port specified
	if config.Port == 0 {
		log.Errorf("Server %s does not have a valid port assigned", config.Name)
		rtrn_err = true
	}
	// Validate the ip address if one is provided
	if ip := net.ParseIP(config.Addr); config.Addr != "" && ip == nil {
		log.Errorf("Invalid address '%s' provided for server '%s'", config.Addr, config.Name)
		rtrn_err = true
	}
	if config.Type != "grpc" && config.Type != "streaming_grpc" {
		if config.Type == "" {
		    log.Errorf("A server 'type' must be defined for server %s",config.Name)
		} else {
	        log.Errorf("The server type must be either 'grpc' or 'streaming_grpc' "+
			    "but '%s' was found for server '%s'", config.Type, config.Name)
		}
		rtrn_err = true
	}
	if len(config.Routers) == 0 {
	    log.Errorf("At least one router must be specified for server '%s'", config.Name)
		rtrn_err = true
	}

	if rtrn_err == true {
		return nil, errors.New("Server configuration failed")
	} else {
		// The configuration is valid, create a server and configure it.
		srvr = &server{name:config.Name,routers:make(map[string]map[string]Router)}
		// The listener
		if srvr.proxyListener, err =
			net.Listen("tcp", config.Addr + ":"+
			strconv.Itoa(int(config.Port))); err != nil {
			log.Error(err)
			return nil, err
		}
		// Create the routers
		log.Debugf("Configuring the routers for server %s", srvr.name)
		for p,r := range config.routers {
			log.Debugf("Processing router %s for package %s", r.Name,p)
		    if dr,err := newRouter(r); err != nil {
			    log.Error(err)
			    return nil, err
		    } else {
				log.Debugf("Adding router %s to the server %s for package %s and service %s",
							dr.Name(), srvr.name, p, dr.Service())
				if _,ok := srvr.routers[p]; ok {
					srvr.routers[p][dr.Service()] = dr
				} else {
					srvr.routers[p] = make(map[string]Router)
					srvr.routers[p][dr.Service()] = dr
				}
			}
		}
		// Configure the grpc handler
		srvr.proxyServer = grpc.NewServer(
			grpc.CustomCodec(Codec()),
			grpc.UnknownServiceHandler(srvr.TransparentHandler()),
		)

	}
	// Compile the regular expression to extract the method
	mthdSlicer = regexp.MustCompile(mthdSlicerExp)
	return srvr, nil
}

func (s *server) Name() (string) {
	return s.name
}

func (s *server) TransparentHandler() grpc.StreamHandler {
	return s.handler
}


func (s *server) getRouter(pkg *string, service *string) (Router,bool) {
	if fn, ok := s.routers[*pkg][*service]; ok { // Both specified
		return fn, ok
	} else if fn, ok = s.routers["*"][*service]; ok { // Package wild card
		return fn, ok
	} else if fn, ok = s.routers[*pkg]["*"]; ok { // Service wild card
		return fn, ok
	} else if fn, ok = s.routers["*"]["*"]; ok { // Both Wildcarded
		return fn, ok
	} else {
		return nil,false
	}
}


func (s *server) handler(srv interface{}, serverStream grpc.ServerStream) error {
	// Determine what router is intended to handle this request
	fullMethodName, ok := grpc.MethodFromServerStream(serverStream)
	if !ok {
		return grpc.Errorf(codes.Internal, "lowLevelServerStream doesn't exist in context")
	}
	log.Debugf("Processing grpc request %s on server %s",fullMethodName,s.name)
	// The full method name is structured as follows:
	// <package name>.<service>/<method>
	mthdSlice := mthdSlicer.FindStringSubmatch(fullMethodName)
	if mthdSlice == nil {
		log.Errorf("Faled to slice full method %s, result: %v", fullMethodName, mthdSlice)
	} else {
		log.Debugf("Sliced full method %s: %v", fullMethodName, mthdSlice)
	}
	r, ok := s.getRouter(&mthdSlice[REQ_PACKAGE],&mthdSlice[REQ_SERVICE])
	//fn, ok := s.routers[mthdSlice[REQ_PACKAGE]][mthdSlice[REQ_SERVICE]]
	if !ok {
		// TODO: Should this be punted to a default transparent router??
		// Probably not, if one is defined yes otherwise just crap out.

		err := errors.New(
			fmt.Sprintf("Unable to dispatch! Service '%s' for package '%s' not found.",
				mthdSlice[REQ_SERVICE], mthdSlice[REQ_PACKAGE]))
		log.Error(err)
		return err
	}
	log.Debugf("Selected router %s\n", r.Name())

	mk,mv,err := r.GetMetaKeyVal(serverStream)
	if err != nil {
		log.Error(err)
		return err
	}

	//nbR := &nbRequest(srv:srv,serverStream:serverStream,r:r,mthdSlice:mthdSlice,metaKey:mk,metaVal:mv)

	// Extract the cluster from the selected router and use it to manage the transfer
	if bkndClstr,err := r.BackendCluster(mthdSlice[REQ_METHOD], mk); err != nil {
		return err
	} else {
		//return bkndClstr.handler(nbR)
		return bkndClstr.handler(srv, serverStream, r, mthdSlice, mk, mv)
	}

	return grpc.Errorf(codes.Internal, "gRPC proxying should never reach this stage.")
}

