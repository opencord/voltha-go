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
	"errors"
	"google.golang.org/grpc"
	"github.com/opencord/voltha-go/common/log"
)

type RoundRobinRouter struct {
	name string
	routerType int // TODO: Likely not needed.
	grpcService string
	bkndClstr *backendCluster
	curBknd **backend
}

func newRoundRobinRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
	var err error = nil
	var rtrn_err bool = false
	// Validate the configuration

	log.Debug("Creating a new round robin router")
	// A name must exist
	if config.Name == "" {
		log.Error("A router 'name' must be specified")
		rtrn_err = true
	}

	if rconf.ProtoPackage == "" {
		log.Error("A 'package' must be specified")
		rtrn_err = true
	}

	if rconf.ProtoService == "" {
		log.Error("A 'service' must be specified")
		rtrn_err = true
	}

	var bptr *backend
	bptr = nil
	rr := RoundRobinRouter{
		name:config.Name,
		grpcService:rconf.ProtoService,
		curBknd:&bptr,
	}

	// This has already been validated bfore this function
	// is called so just use it.
	for idx := range rTypeNames {
		if config.Type == rTypeNames[idx] {
			rr.routerType = idx
			break
		}
	}

	// Create the backend cluster or link to an existing one 
	ok := true
	if rr.bkndClstr, ok = bClusters[config.backendCluster.Name]; ok == false {
		if rr.bkndClstr, err = newBackendCluster(config.backendCluster); err != nil {
			log.Errorf("Could not create a backend for router %s", config.Name)
			rtrn_err = true
		}
	}

	if rtrn_err {
		return rr,errors.New(fmt.Sprintf("Failed to create a new router '%s'",rr.name))
	}

	return rr,nil
}

func (rr RoundRobinRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string,string,error) {
	return "","",nil
}

func (rr RoundRobinRouter) BackendCluster(s string, mk string) (*backendCluster,error) {
	return rr.bkndClstr, nil
}

func (rr RoundRobinRouter) Name() (string) {
	return rr.name
}

func(rr RoundRobinRouter) Route(sel interface{}) (*backend) {
	var err error
	switch sl := sel.(type) {
		case *nbFrame:
			// Since this is a round robin router just get the next backend
			if *rr.curBknd, err = rr.bkndClstr.nextBackend(*rr.curBknd,BE_SEQ_RR); err == nil {
				return *rr.curBknd
			} else {
				sl.err = err
				return nil
			}
		default:
			log.Errorf("Internal: invalid data type in Route call %v", sel);
			return nil
	}
	log.Errorf("Round robin error %v", err);
	return nil
}

func (rr RoundRobinRouter) Service() (string) {
	return rr.grpcService
}

func (rr RoundRobinRouter) FindBackendCluster(becName string) (*backendCluster) {
	if becName ==  rr.bkndClstr.name {
		return rr.bkndClstr
	}
	return nil
}

func (rr RoundRobinRouter) ReplyHandler(sel interface{}) error { // This is a no-op
	return nil
}
