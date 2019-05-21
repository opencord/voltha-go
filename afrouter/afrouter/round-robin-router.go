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

package afrouter

import (
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
)

type RoundRobinRouter struct {
	name           string
	grpcService    string
	cluster        *cluster
	currentBackend **backend
}

func newRoundRobinRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
	var err error = nil
	var rtrn_err = false
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
		name:           config.Name,
		grpcService:    rconf.ProtoService,
		currentBackend: &bptr,
	}

	// Create the backend cluster or link to an existing one
	ok := true
	if rr.cluster, ok = clusters[config.backendCluster.Name]; !ok {
		if rr.cluster, err = newBackendCluster(config.backendCluster); err != nil {
			log.Errorf("Could not create a backend for router %s", config.Name)
			rtrn_err = true
		}
	}

	if rtrn_err {
		return rr, errors.New(fmt.Sprintf("Failed to create a new router '%s'", rr.name))
	}

	return rr, nil
}

func (rr RoundRobinRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string, string, error) {
	return "", "", nil
}

func (rr RoundRobinRouter) BackendCluster(s string, mk string) (*cluster, error) {
	return rr.cluster, nil
}

func (rr RoundRobinRouter) Name() string {
	return rr.name
}

func (rr RoundRobinRouter) Route(sel interface{}) *backend {
	var err error
	switch sl := sel.(type) {
	case *nbFrame:
		// Since this is a round robin router just get the next backend
		if *rr.currentBackend, err = rr.cluster.nextBackend(*rr.currentBackend, BackendSequenceRoundRobin); err == nil {
			return *rr.currentBackend
		} else {
			sl.err = err
			return nil
		}
	default:
		log.Errorf("Internal: invalid data type in Route call %v", sel)
		return nil
	}
}

func (rr RoundRobinRouter) Service() string {
	return rr.grpcService
}

func (rr RoundRobinRouter) FindBackendCluster(becName string) *cluster {
	if becName == rr.cluster.name {
		return rr.cluster
	}
	return nil
}

func (rr RoundRobinRouter) ReplyHandler(sel interface{}) error { // This is a no-op
	return nil
}
