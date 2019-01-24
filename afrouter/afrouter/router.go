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
)

const (
    RT_RPC_AFFINITY_MESSAGE = iota+1
    RT_RPC_AFFINITY_HEADER = iota+1
    RT_BINDING = iota+1
	RT_ROUND_ROBIN = iota+1
)

// String names for display in error messages.
var rTypeNames = []string{"","rpc_affinity_message","rpc_affinity_header","binding", "round_robin"}
var rAssnNames = []string{"","round_robin"}

var allRouters map[string]Router = make(map[string]Router)

// The router interface
type Router interface {
	Name() (string)
	Route(interface{}) *backend
	Service() (string)
	BackendCluster(string, string) (*backendCluster, error)
	FindBackendCluster(string) (*backendCluster)
	ReplyHandler(interface{}) error
	GetMetaKeyVal(serverStream grpc.ServerStream) (string,string,error)
}

func newRouter(config *RouterConfig) (Router, error) {
	r,err := newMethodRouter(config)
	if  err == nil {
		allRouters[r.Name()] = r
	}
	return r, err
}

func newSubRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
	idx := strIndex(rTypeNames, config.Type)
	switch idx {
	case RT_RPC_AFFINITY_MESSAGE:
		r,err := newAffinityRouter(rconf, config)
		if err == nil {
			allRouters[rconf.Name+config.Name] = r
		}
		return r, err
	case RT_BINDING:
		r,err := newBindingRouter(rconf, config)
		if err == nil {
			allRouters[rconf.Name+config.Name] = r
		}
		return r, err
	case RT_ROUND_ROBIN:
		r,err := newRoundRobinRouter(rconf, config)
		if err == nil {
			allRouters[rconf.Name+config.Name] = r
		}
		return r, err
	default:
		return nil, errors.New(fmt.Sprintf("Internal error, undefined router type: %s", config.Type))
	}

	return nil, errors.New(fmt.Sprintf("Unrecognized router type '%s'",config.Type))
}
