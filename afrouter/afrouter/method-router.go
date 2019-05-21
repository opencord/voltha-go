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
	"google.golang.org/grpc/metadata"
)

const NoMeta = "nometa"

type MethodRouter struct {
	name         string
	service      string
	methodRouter map[string]map[string]Router // map of [metadata][method]
}

func newMethodRouter(config *RouterConfig) (Router, error) {
	mr := MethodRouter{name: config.Name, service: config.ProtoService, methodRouter: make(map[string]map[string]Router)}
	mr.methodRouter[NoMeta] = make(map[string]Router) // For routes not needing metadata (all expcept binding at this time)
	log.Debugf("Processing MethodRouter config %v", *config)
	if len(config.Routes) == 0 {
		return nil, errors.New(fmt.Sprintf("Router %s must have at least one route", config.Name))
	}
	for _, rtv := range config.Routes {
		//log.Debugf("Processing route: %v",rtv)
		var idx1 string
		r, err := newSubRouter(config, &rtv)
		if err != nil {
			return nil, err
		}
		if rtv.Type == RouteTypeBinding {
			idx1 = rtv.Binding.Field
			if _, ok := mr.methodRouter[idx1]; !ok { // /First attempt on this key
				mr.methodRouter[idx1] = make(map[string]Router)
			}
		} else {
			idx1 = NoMeta
		}
		switch len(rtv.Methods) {
		case 0:
			return nil, errors.New(fmt.Sprintf("Route for router %s must have at least one method", config.Name))
		case 1:
			if rtv.Methods[0] == "*" {
				return r, nil
			} else {
				log.Debugf("Setting router '%s' for single method '%s'", r.Name(), rtv.Methods[0])
				if _, ok := mr.methodRouter[idx1][rtv.Methods[0]]; !ok {
					mr.methodRouter[idx1][rtv.Methods[0]] = r
				} else {
					err := errors.New(fmt.Sprintf("Attempt to define method %s for 2 routes: %s & %s", rtv.Methods[0],
						r.Name(), mr.methodRouter[idx1][rtv.Methods[0]].Name()))
					log.Error(err)
					return mr, err
				}
			}
		default:
			for _, m := range rtv.Methods {
				log.Debugf("Processing Method %s", m)
				if _, ok := mr.methodRouter[idx1][m]; !ok {
					log.Debugf("Setting router '%s' for method '%s'", r.Name(), m)
					mr.methodRouter[idx1][m] = r
				} else {
					err := errors.New(fmt.Sprintf("Attempt to define method %s for 2 routes: %s & %s", m, r.Name(), mr.methodRouter[idx1][m].Name()))
					log.Error(err)
					return mr, err
				}
			}
		}
	}

	return mr, nil
}

func (mr MethodRouter) Name() string {
	return mr.name
}

func (mr MethodRouter) Service() string {
	return mr.service
}

func (mr MethodRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string, string, error) {
	var rtrnK = NoMeta
	var rtrnV = ""

	// Get the metadata from the server stream
	md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
		return rtrnK, rtrnV, errors.New("Could not get a server stream metadata")
	}

	// Determine if one of the method routing keys exists in the metadata
	for k := range mr.methodRouter {
		if _, ok := md[k]; ok {
			rtrnV = md[k][0]
			rtrnK = k
			break
		}
	}
	return rtrnK, rtrnV, nil

}

func (mr MethodRouter) ReplyHandler(sel interface{}) error {
	switch sl := sel.(type) {
	case *sbFrame:
		if r, ok := mr.methodRouter[NoMeta][sl.method]; ok {
			return r.ReplyHandler(sel)
		}
		// TODO: this case should also be an error
	default: //TODO: This should really be a big error
		// A reply handler should only be called on the sbFrame
		return nil
	}
	return nil
}

func (mr MethodRouter) Route(sel interface{}) *backend {
	switch sl := sel.(type) {
	case *nbFrame:
		if r, ok := mr.methodRouter[sl.metaKey][sl.methodInfo.method]; ok {
			return r.Route(sel)
		}
		log.Errorf("Attept to route on non-existent method '%s'", sl.methodInfo.method)
		return nil
	default:
		return nil
	}
}

func (mr MethodRouter) BackendCluster(mthd string, metaKey string) (*cluster, error) {
	if r, ok := mr.methodRouter[metaKey][mthd]; ok {
		return r.BackendCluster(mthd, metaKey)
	}
	err := errors.New(fmt.Sprintf("No backend cluster exists for method '%s' using meta key '%s'", mthd, metaKey))
	log.Error(err)
	return nil, err
}

func (mr MethodRouter) FindBackendCluster(beName string) *cluster {
	for _, meta := range mr.methodRouter {
		for _, r := range meta {
			if rtrn := r.FindBackendCluster(beName); rtrn != nil {
				return rtrn
			}
		}
	}
	return nil
}
