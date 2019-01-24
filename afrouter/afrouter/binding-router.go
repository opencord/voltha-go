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
	"google.golang.org/grpc/metadata"
	"github.com/opencord/voltha-go/common/log"
)

type BindingRouter struct {
	name string
	routerType int // TODO: This is probably not needed 
	association int
	//routingField string
	grpcService string
	//protoDescriptor *pb.FileDescriptorSet
	//methodMap map[string]byte
	bkndClstr *backendCluster
	bindings map[string]*backend
	bindingType string
	bindingField string
	bindingMethod string
	curBknd **backend
}

func (br BindingRouter) BackendCluster(s string, metaKey string) (*backendCluster,error) {
	return br.bkndClstr, nil
	//return nil,errors.New("Not implemented yet")
}
func (br BindingRouter) Name() (string) {
	return br.name
}
func (br BindingRouter) Service() (string) {
	return br.grpcService
}
func (br BindingRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string,string,error) {
	var rtrnK string = ""
	var rtrnV string = ""

	// Get the metadata from the server stream
    md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
	    return rtrnK, rtrnV, errors.New("Could not get a server stream metadata")
    }

	// Determine if one of the method routing keys exists in the metadata
	if  _,ok := md[br.bindingField]; ok == true {
		rtrnV = md[br.bindingField][0]
		rtrnK = br.bindingField
	}

	return rtrnK,rtrnV,nil
}
func (br BindingRouter) FindBackendCluster(string) (*backendCluster) {
	return nil
}
func (br BindingRouter) ReplyHandler(v interface{}) error {
	return nil
}
func (br BindingRouter) Route(sel interface{}) (*backend) {
	var err error
	switch sl := sel.(type) {
		case *nbFrame:
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
				if sl.mthdSlice[REQ_METHOD] != br.bindingMethod {
					err = errors.New(fmt.Sprintf("Binding must occur with method %s but attempted with method %s",
										br.bindingMethod, sl.mthdSlice[REQ_METHOD]))
					log.Error(err)
					sl.err = err
					return nil
				}
				log.Debugf("MUST CREATE A NEW BINDING MAP ENTRY!!")
				if len(br.bindings) < len(br.bkndClstr.backends) {
					if *br.curBknd, err = br.bkndClstr.nextBackend(*br.curBknd,BE_SEQ_RR); err == nil {
						// Use the name of the backend as the metaVal for this new binding
						br.bindings[(*br.curBknd).name] =  *br.curBknd
						return *br.curBknd
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
	return nil
}

func NewBindingRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
	var rtrn_err bool = false
	var err error = nil
	log.Debugf("Creating binding router %s",config.Name)
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
		name:config.Name,
		grpcService:rconf.ProtoService,
		bindings:make(map[string]*backend),
		//methodMap:make(map[string]byte),
		curBknd:&bptr,
		//serialNo:0,
	}

	// A binding association must exist
	br.association = strIndex(rAssnNames, config.Binding.Association)
	if br.association == 0 {
		if config.Binding.Association == "" {
		    log.Error("An binding association must be specified")
		} else {
		    log.Errorf("The binding association '%s' is not valid", config.Binding.Association)
		}
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


	// This has already been validated bfore this function
	// is called so just use it.
	for idx := range rTypeNames {
		if config.Type == rTypeNames[idx] {
			br.routerType = idx
			break
		}
	}

	// Create the backend cluster or link to an existing one 
	ok := true
	if br.bkndClstr, ok = bClusters[config.backendCluster.Name]; ok == false {
		if br.bkndClstr, err = NewBackendCluster(config.backendCluster); err != nil {
			log.Errorf("Could not create a backend for router %s", config.Name)
			rtrn_err = true
		}
	}

	// HERE HERE HERE

	if rtrn_err {
		return br,errors.New(fmt.Sprintf("Failed to create a new router '%s'",br.name))
	}


	return br,nil
}
