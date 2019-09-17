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
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"regexp"
	"strconv"
)

// TODO: Used in multiple routers, should move to common file
const (
	PKG_MTHD_PKG  int = 1
	PKG_MTHD_MTHD int = 2
)

type AffinityRouter struct {
	name               string
	association        associationType
	routingField       string
	grpcService        string
	methodMap          map[string]byte
	nbBindingMethodMap map[string]byte
	cluster            *cluster
	affinity           map[string]*backend
	currentBackend     **backend
}

func newAffinityRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
	var err error = nil
	var rtrn_err = false
	var pkg_re = regexp.MustCompile(`^(\.[^.]+\.)(.+)$`)
	// Validate the configuration

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

	// TODO The overrieds section is currently not being used
	// so the router will route all methods based on the
	// routing_field. This needs to be added so that methods
	// can have different routing fields.
	var bptr *backend
	bptr = nil
	dr := AffinityRouter{
		name:               config.Name,
		grpcService:        rconf.ProtoService,
		affinity:           make(map[string]*backend),
		methodMap:          make(map[string]byte),
		nbBindingMethodMap: make(map[string]byte),
		currentBackend:     &bptr,
	}
	// An association must exist
	dr.association = config.Association
	if dr.association == AssociationUndefined {
		log.Error("An association must be specified")
		rtrn_err = true
	}

	// Build the routing structure based on the loaded protobuf
	// descriptor file and the config information.
	type key struct {
		method string
		field  string
	}
	var fieldNumberLookup = make(map[key]byte)
	for _, f := range rconf.protoDescriptor.File {
		// Build a temporary map of message types by name.
		for _, m := range f.MessageType {
			for _, fld := range m.Field {
				log.Debugf("Processing message '%s', field '%s'", *m.Name, *fld.Name)
				fieldNumberLookup[key{*m.Name, *fld.Name}] = byte(*fld.Number)
			}
		}
	}
	for _, f := range rconf.protoDescriptor.File {
		if *f.Package == rconf.ProtoPackage {
			for _, s := range f.Service {
				if *s.Name == rconf.ProtoService {
					log.Debugf("Loading package data '%s' for service '%s' for router '%s'", *f.Package, *s.Name, dr.name)
					// Now create a map keyed by method name with the value being the
					// field number of the route selector.
					var ok bool
					for _, m := range s.Method {
						// Find the input type in the messages and extract the
						// field number and save it for future reference.
						log.Debugf("Processing method '%s'", *m.Name)
						// Determine if this is a method we're supposed to be processing.
						if needMethod(*m.Name, config) {
							log.Debugf("Enabling method '%s'", *m.Name)
							pkg_methd := pkg_re.FindStringSubmatch(*m.InputType)
							if pkg_methd == nil {
								log.Errorf("Regular expression didn't match input type '%s'", *m.InputType)
								rtrn_err = true
							}
							// The input type has the package name prepended to it. Remove it.
							//in := (*m.InputType)[len(rconf.ProtoPackage)+2:]
							in := pkg_methd[PKG_MTHD_MTHD]
							dr.methodMap[*m.Name], ok = fieldNumberLookup[key{in, config.RouteField}]
							if !ok {
								log.Errorf("Method '%s' has no field named '%s' in it's parameter message '%s'",
									*m.Name, config.RouteField, in)
								rtrn_err = true
							}
						}
						// The sb method is always included in the methods so we can check it here too.
						if needNbBindingMethod(*m.Name, config) {
							log.Debugf("Enabling southbound method '%s'", *m.Name)
							// The output type has the package name prepended to it. Remove it.
							out := (*m.OutputType)[len(rconf.ProtoPackage)+2:]
							dr.nbBindingMethodMap[*m.Name], ok = fieldNumberLookup[key{out, config.RouteField}]
							if !ok {
								log.Errorf("Method '%s' has no field named '%s' in it's parameter message '%s'",
									*m.Name, config.RouteField, out)
								rtrn_err = true
							}
						}
					}
				}
			}
		}
	}

	// Create the backend cluster or link to an existing one
	ok := true
	if dr.cluster, ok = clusters[config.backendCluster.Name]; !ok {
		if dr.cluster, err = newBackendCluster(config.backendCluster); err != nil {
			log.Errorf("Could not create a backend for router %s", config.Name)
			rtrn_err = true
		}
	}

	if rtrn_err {
		return dr, errors.New(fmt.Sprintf("Failed to create a new router '%s'", dr.name))
	}

	return dr, nil
}

func needNbBindingMethod(mthd string, conf *RouteConfig) bool {
	for _, m := range conf.NbBindingMethods {
		if mthd == m {
			return true
		}
	}
	return false
}

// TODO: Used in multiple routers, should move to common file
func needMethod(mthd string, conf *RouteConfig) bool {
	for _, m := range conf.Methods {
		if mthd == m {
			return true
		}
	}
	return false
}

func (ar AffinityRouter) Service() string {
	return ar.grpcService
}

func (ar AffinityRouter) Name() string {
	return ar.name
}

func (ar AffinityRouter) skipField(data *[]byte, idx *int) error {
	switch (*data)[*idx] & 3 {
	case 0: // Varint
		*idx++
		for (*data)[*idx] >= 128 {
			*idx++
		}
	case 1: // 64 bit
		*idx += 9
	case 2: // Length delimited
		*idx++
		b := proto.NewBuffer((*data)[*idx:])
		t, _ := b.DecodeVarint()
		*idx += int(t) + 1
	case 3: // Deprecated
	case 4: // Deprecated
	case 5: // 32 bit
		*idx += 5
	}
	return nil
}

func (ar AffinityRouter) decodeProtoField(payload []byte, fieldId byte) (string, error) {
	idx := 0
	b := proto.NewBuffer([]byte{})
	//b.DebugPrint("The Buffer", payload)
	for { // Find the route selector field
		log.Debugf("Decoding afinity value attributeNumber: %d from %v at index %d", fieldId, payload, idx)
		log.Debugf("Attempting match with payload: %d, methodTable: %d", payload[idx], fieldId)
		if payload[idx]>>3 == fieldId {
			log.Debugf("Method match with payload: %d, methodTable: %d", payload[idx], fieldId)
			// TODO: Consider supporting other selector types.... Way, way in the future
			// ok, the future is now, support strings as well... ugh.
			var selector string
			switch payload[idx] & 3 {
			case 0: // Integer
				b.SetBuf(payload[idx+1:])
				v, e := b.DecodeVarint()
				if e == nil {
					log.Debugf("Decoded the ing field: %v", v)
					selector = strconv.Itoa(int(v))
				} else {
					log.Errorf("Failed to decode varint %v", e)
					return "", e
				}
			case 2: // Length delimited AKA string
				b.SetBuf(payload[idx+1:])
				v, e := b.DecodeStringBytes()
				if e == nil {
					log.Debugf("Decoded the string field: %v", v)
					selector = v
				} else {
					log.Errorf("Failed to decode string %v", e)
					return "", e
				}
			default:
				err := errors.New(fmt.Sprintf("Only integer and string route selectors are permitted"))
				log.Error(err)
				return "", err
			}
			return selector, nil
		} else if err := ar.skipField(&payload, &idx); err != nil {
			log.Errorf("Parsing message failed %v", err)
			return "", err
		}
	}
}

func (ar AffinityRouter) Route(sel interface{}) (*backend, *connection) {
	switch sl := sel.(type) {
	case *requestFrame:
		log.Debugf("Route called for requestFrame with method %s", sl.methodInfo.method)
		// Check if this method should be affinity bound from the
		// reply rather than the request.
		if _, ok := ar.nbBindingMethodMap[sl.methodInfo.method]; ok {
			var err error
			log.Debugf("Method '%s' affinity binds on reply", sl.methodInfo.method)
			// Just round robin route the southbound request
			if *ar.currentBackend, err = ar.cluster.nextBackend(*ar.currentBackend, BackendSequenceRoundRobin); err == nil {
				return *ar.currentBackend, nil
			} else {
				sl.err = err
				return nil, nil
			}
		}
		// Not a south affinity binding method, proceed with north affinity binding.
		if selector, err := ar.decodeProtoField(sl.payload, ar.methodMap[sl.methodInfo.method]); err == nil {
			log.Debugf("Establishing affinity for selector: %s", selector)
			if rtrn, ok := ar.affinity[selector]; ok {
				return rtrn, nil
			} else {
				// The selector isn't in the map, create a new affinity mapping
				log.Debugf("MUST CREATE A NEW AFFINITY MAP ENTRY!!")
				var err error
				if *ar.currentBackend, err = ar.cluster.nextBackend(*ar.currentBackend, BackendSequenceRoundRobin); err == nil {
					ar.setAffinity(selector, *ar.currentBackend)
					//ar.affinity[selector] = *ar.currentBackend
					//log.Debugf("New affinity set to backend %s",(*ar.currentBackend).name)
					return *ar.currentBackend, nil
				} else {
					sl.err = err
					return nil, nil
				}
			}
		}
	default:
		log.Errorf("Internal: invalid data type in Route call %v", sel)
		return nil, nil
	}
	log.Errorf("Bad lookup in affinity map %v", ar.affinity)
	return nil, nil
}

func (ar AffinityRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string, string, error) {
	return "", "", nil
}

func (ar AffinityRouter) IsStreaming(_ string) (bool, bool) {
	panic("not implemented")
}

func (ar AffinityRouter) BackendCluster(mthd string, metaKey string) (*cluster, error) {
	return ar.cluster, nil
}

func (ar AffinityRouter) FindBackendCluster(beName string) *cluster {
	if beName == ar.cluster.name {
		return ar.cluster
	}
	return nil
}

func (ar AffinityRouter) ReplyHandler(sel interface{}) error {
	switch sl := sel.(type) {
	case *responseFrame:
		log.Debugf("Reply handler called for responseFrame with method %s", sl.method)
		// Determine if reply action is required.
		if fld, ok := ar.nbBindingMethodMap[sl.method]; ok && len(sl.payload) > 0 {
			// Extract the field value from the frame and
			// and set affinity accordingly
			if selector, err := ar.decodeProtoField(sl.payload, fld); err == nil {
				log.Debug("Settign affinity on reply")
				if ar.setAffinity(selector, sl.backend) != nil {
					log.Error("Setting affinity on reply failed")
				}
				return nil
			} else {
				err := errors.New(fmt.Sprintf("Failed to decode reply field %d for method %s", fld, sl.method))
				log.Error(err)
				return err
			}
		}
		return nil
	default:
		err := errors.New(fmt.Sprintf("Internal: invalid data type in ReplyHander call %v", sl))
		log.Error(err)
		return err
	}
}

func (ar AffinityRouter) setAffinity(key string, be *backend) error {
	if be2, ok := ar.affinity[key]; !ok {
		ar.affinity[key] = be
		log.Debugf("New affinity set to backend %s for key %s", be.name, key)
	} else if be2 != be {
		err := errors.New(fmt.Sprintf("Attempting multiple sets of affinity for key %s to backend %s from %s on router %s",
			key, be.name, ar.affinity[key].name, ar.name))
		log.Error(err)
		return err
	}
	return nil
}
