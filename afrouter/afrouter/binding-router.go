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
	"sync"
)

type BindingRouter struct {
	name        string
	association associationType
	//routingField string
	grpcService string
	//protoDescriptor *pb.FileDescriptorSet
	//methodMap map[string]byte
	beCluster *cluster
	bindings  map[string]*backend
	// map of backend references
	references     map[string]int
	bindingType    string
	bindingField   string
	bindingMethod  string
	currentBackend **backend
	refLock        *sync.Mutex
}

func (br BindingRouter) IsStreaming(_ string) (bool, bool) {
	panic("not implemented")
}

func (br BindingRouter) BackendCluster(s string, metaKey string) (*cluster, error) {
	return br.beCluster, nil
	//return nil,errors.New("Not implemented yet")
}
func (br BindingRouter) Name() string {
	return br.name
}
func (br BindingRouter) Service() string {
	return br.grpcService
}
func (br BindingRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string, string, error) {
	var rtrnK = ""
	var rtrnV = ""

	// Get the metadata from the server stream
	md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
		return rtrnK, rtrnV, errors.New("Could not get a server stream metadata")
	}

	// Determine if one of the method routing keys exists in the metadata
	if _, ok := md[br.bindingField]; ok == true {
		rtrnV = md[br.bindingField][0]
		rtrnK = br.bindingField
	}

	return rtrnK, rtrnV, nil
}
func (br BindingRouter) FindBackendCluster(becName string) *cluster {
	if becName == br.beCluster.name {
		return br.beCluster
	}
	return nil
}
func (br BindingRouter) ReplyHandler(v interface{}) error {
	return nil
}

func (br BindingRouter) GetReference(be *backend, sel interface{}) error {
	switch sl := sel.(type) {
	case *requestFrame:
		br.refLock.Lock()
		defer br.refLock.Unlock()
		_, streamingResponse := sl.router.IsStreaming(sl.methodInfo.method)
		if sl.metaVal != "" && streamingResponse {
			if _, ok := br.references[be.name]; ok {
				br.references[be.name] += 1
			} else {
				br.references[be.name] = 1
			}
			log.Debugf("Increasing reference for backend %s to %d for meta key %s, method %s",
				be.name, br.references[be.name], sl.metaKey, sl.methodInfo.method)
		}
		return nil
	default:
		return nil
	}
}

func (br BindingRouter) DropReference(be *backend, sel interface{}, rpc_status error) error {
	switch sl := sel.(type) {
	case *requestFrame:
		br.refLock.Lock()
		defer br.refLock.Unlock()
		_, streamingResponse := sl.router.IsStreaming(sl.methodInfo.method)
		if sl.metaVal != "" && streamingResponse {
			if _, ok := br.references[be.name]; ok {
				br.references[be.name] -= 1
				log.Debugf("Dropping reference for backend %s to %d for meta key %s, method %s",
					be.name, br.references[be.name], sl.metaKey, sl.methodInfo.method)
				if br.references[be.name] <= 0 {
					br.references[be.name] = 0
					log.Debugf("No reference for backend found."+
						"Removing backend %s from bindings with no references for meta key %s, method %s",
						be.name, sl.metaKey, sl.methodInfo.method)
					delete(br.bindings, be.name)
				}
			} else {
				log.Debugf("No reference for backend %s", be.name)
				return errors.New(fmt.Sprintf("No reference for backend %s", be.name))
			}
		} else if sl.metaVal == "" && rpc_status != nil {
			// if we failed to send Subscribe request but had bound the backend, unbind
			// The subscribe would be retried again
			br.references[be.name] = 0
			if _, ok := br.bindings[be.name]; ok {
				delete(br.bindings, be.name)
				log.Debugf("Removing backend %s from bindings as Subscribe had failed", be.name)
			}
		}
		return nil
	default:
		return nil
	}
}

func (br BindingRouter) Route(sel interface{}) *backend {
	var err error

	switch sl := sel.(type) {
	case *requestFrame:
		if b, ok := br.bindings[sl.metaVal]; ok == true { // binding exists, just return it
			return b
		} else { // establish a new binding or error.
			if sl.metaVal != "" {
				err = errors.New(fmt.Sprintf("Attempt to route on non-existent metadata value '%s' in key '%s'",
					sl.metaVal, sl.metaKey))
				log.Error(err)
				sl.err = err
				return nil
			}
			if sl.methodInfo.method != br.bindingMethod {
				err = errors.New(fmt.Sprintf("Binding must occur with method %s but attempted with method %s",
					br.bindingMethod, sl.methodInfo.method))
				log.Error(err)
				sl.err = err
				return nil
			}
			log.Debugf("MUST CREATE A NEW BINDING MAP ENTRY!!")
			if len(br.bindings) < len(br.beCluster.backends) {
				if *br.currentBackend, err = br.beCluster.nextBackend(*br.currentBackend, BackendSequenceRoundRobin); err == nil {
					// Use the name of the backend as the metaVal for this new binding
					log.Debugf("Assigning backend %s", (*br.currentBackend).name)
					br.bindings[(*br.currentBackend).name] = *br.currentBackend
					return *br.currentBackend
				} else {
					log.Error(err)
					sl.err = err
					return nil
				}
			} else {
				err = errors.New(fmt.Sprintf("Backends exhausted in attempt to bind for metakey '%s' with value '%s'",
					sl.metaKey, sl.metaVal))
				log.Error(err)
				sl.err = err
			}
		}
		return nil
	default:
		return nil
	}
}

func newBindingRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
	var rtrn_err = false
	var err error = nil
	log.Debugf("Creating binding router %s", config.Name)
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

	//if config.RouteField == "" {
	//	log.Error("A 'routing_field' must be specified")
	//	rtrn_err = true
	//}

	// TODO: Using the specified service, the imported proto
	// descriptor file should be scanned for all methods provided
	// for this router to ensure that this field exists in
	// the message(s) passed to the method. This will avoid run
	// time failures that might not be detected for long periods
	// of time.

	// TODO The routes section is currently not being used
	// so the router will route all methods based on the
	// routing_field. This needs to be done.
	var bptr *backend
	bptr = nil
	br := BindingRouter{
		name:        config.Name,
		grpcService: rconf.ProtoService,
		bindings:    make(map[string]*backend),
		references:  make(map[string]int),
		//methodMap:make(map[string]byte),
		currentBackend: &bptr,
		refLock:        new(sync.Mutex),
		//serialNo:0,
	}

	// A binding association must exist
	br.association = config.Binding.Association
	if br.association == AssociationUndefined {
		log.Error("An binding association must be specified")
		rtrn_err = true
	}
	// A binding type must exist
	// TODO: This is parsed but ignored and a header based type is used.
	if config.Binding.Type != "header" {
		log.Error("The binding type must be set to header")
		rtrn_err = true
	} else {
		br.bindingType = config.Binding.Type
	}
	// A binding method must exist
	if config.Binding.Method == "" {
		log.Error("The binding method must be specified")
		rtrn_err = true
	} else {
		br.bindingMethod = config.Binding.Method
	}
	// A binding field must exxist
	if config.Binding.Field == "" {
		log.Error("The binding field must be specified")
		rtrn_err = true
	} else {
		br.bindingField = config.Binding.Field
	}

	// Create the backend cluster or link to an existing one
	ok := true
	if br.beCluster, ok = clusters[config.backendCluster.Name]; ok == false {
		if br.beCluster, err = newBackendCluster(config.backendCluster); err != nil {
			log.Errorf("Could not create a backend for router %s", config.Name)
			rtrn_err = true
		}
	}

	// HERE HERE HERE

	if rtrn_err {
		return br, errors.New(fmt.Sprintf("Failed to create a new router '%s'", br.name))
	}

	return br, nil
}
