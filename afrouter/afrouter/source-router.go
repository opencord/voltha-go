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

/* Source-Router

   This router implements source routing where the caller identifies the
   component the message should be routed to. The `RouteField` should be
   configured with the gRPC field name to inspect to determine the
   destination. This field is assumed to be a string. That string will
   then be used to identify a particular connection on a particular
   backend.

   The source-router must be configured with a backend cluster, as all routers
   must identify a backend cluster. However, that backend cluster
   is merely a placeholder and is not used by the source-router. The
   source-router's Route() function will return whatever backend cluster is
   specified by the `RouteField`.
*/

import (
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"io/ioutil"
	"regexp"
	"strconv"
)

/* already declared in affinity_router.go
   TODO: Should be moved from affinity-router.go to a common place
const (
	PKG_MTHD_PKG  int = 1
	PKG_MTHD_MTHD int = 2
)*/

type SourceRouter struct {
	name string
	//association     associationType
	routingField    string
	grpcService     string
	protoDescriptor *pb.FileDescriptorSet
	methodMap       map[string]byte
	cluster         *cluster
}

func newSourceRouter(rconf *RouterConfig, config *RouteConfig) (Router, error) {
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

	if config.RouteField == "" {
		log.Error("A 'routing_field' must be specified")
		rtrn_err = true
	}

	// TODO The overrieds section is currently not being used
	// so the router will route all methods based on the
	// routing_field. This needs to be added so that methods
	// can have different routing fields.
	dr := SourceRouter{
		name:        config.Name,
		grpcService: rconf.ProtoService,
		methodMap:   make(map[string]byte),
	}

	// Load the protobuf descriptor file
	dr.protoDescriptor = &pb.FileDescriptorSet{}
	fb, err := ioutil.ReadFile(rconf.ProtoFile)
	if err != nil {
		log.Errorf("Could not open proto file '%s'", rconf.ProtoFile)
		rtrn_err = true
	}
	err = proto.Unmarshal(fb, dr.protoDescriptor)
	if err != nil {
		log.Errorf("Could not unmarshal %s, %v", "proto.pb", err)
		rtrn_err = true
	}

	// Build the routing structure based on the loaded protobuf
	// descriptor file and the config information.
	type key struct {
		method string
		field  string
	}
	var msgs = make(map[key]byte)
	for _, f := range dr.protoDescriptor.File {
		// Build a temporary map of message types by name.
		for _, m := range f.MessageType {
			for _, fld := range m.Field {
				log.Debugf("Processing message '%s', field '%s'", *m.Name, *fld.Name)
				msgs[key{*m.Name, *fld.Name}] = byte(*fld.Number)
			}
		}
	}
	log.Debugf("The map contains: %v", msgs)
	for _, f := range dr.protoDescriptor.File {
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
							dr.methodMap[*m.Name], ok = msgs[key{in, config.RouteField}]
							if !ok {
								log.Errorf("Method '%s' has no field named '%s' in it's parameter message '%s'",
									*m.Name, config.RouteField, in)
								rtrn_err = true
							}
						}
					}
				}
			}
		}
	}

	// We need to pick a cluster, because server will call cluster.handler. The choice we make doesn't
	// matter, as we can return a different cluster from Route().
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

/* Already declared in affinity_router.go
   TODO: Should be moved from affinity-router.go to a common place

func needMethod(mthd string, conf *RouteConfig) bool {
	for _, m := range conf.Methods {
		if mthd == m {
			return true
		}
	}
	return false
}*/

func (ar SourceRouter) Service() string {
	return ar.grpcService
}

func (ar SourceRouter) Name() string {
	return ar.name
}

func (ar SourceRouter) skipField(data *[]byte, idx *int) error {
	switch (*data)[*idx] & 3 {
	case 0: // Varint
		// skip the field number/type
		*idx++
		// if the msb is set, then more bytes to follow
		for (*data)[*idx] >= 128 {
			*idx++
		}
		// the last byte doesn't have the msb set
		*idx++
	case 1: // 64 bit
		*idx += 9
	case 2: // Length delimited
		// skip the field number / type
		*idx++
		// read a varint that tells length of string
		b := proto.NewBuffer((*data)[*idx:])
		t, _ := b.DecodeVarint()
		// skip the length varint and the string bytes
		// TODO: This assumes the varint was one byte long -- max string length is 127 bytes
		*idx += int(t) + 1
	case 3: // Deprecated
	case 4: // Deprecated
	case 5: // 32 bit
		*idx += 5
	}
	return nil
}

func (ar SourceRouter) decodeProtoField(payload []byte, fieldId byte) (string, error) {
	idx := 0
	b := proto.NewBuffer([]byte{})
	//b.DebugPrint("The Buffer", payload)
	for { // Find the route selector field
		log.Debugf("Decoding source value attributeNumber: %d from %v at index %d", fieldId, payload, idx)
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

func (ar SourceRouter) Route(sel interface{}) (*backend, *connection) {
	log.Debugf("SourceRouter sel %v", sel)
	switch sl := sel.(type) {
	case *requestFrame:
		log.Debugf("Route called for nbFrame with method %s", sl.methodInfo.method)
		// Not a south affinity binding method, proceed with north affinity binding.
		if selector, err := ar.decodeProtoField(sl.payload, ar.methodMap[sl.methodInfo.method]); err == nil {
			// selector is

			for _, cluster := range clusters {
				for _, backend := range cluster.backends {
					log.Debugf("Checking backend %s", backend.name)
					for _, connection := range backend.connections {
						log.Debugf("Checking connection %s", connection.name)
						// caller specified a backend and a connection
						if backend.name+"."+connection.name == selector {
							return backend, connection
						}
					}
					// caller specified just a backend
					if backend.name == selector {
						return backend, nil
					}
				}
			}
			sl.err = fmt.Errorf("Backend %s not found", selector)
			return nil, nil
		}
	default:
		log.Errorf("Internal: invalid data type in Route call %v", sel)
		return nil, nil
	}
	log.Errorf("Bad routing in SourceRouter:Route")
	return nil, nil
}

func (ar SourceRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string, string, error) {
	return "", "", nil
}

func (ar SourceRouter) IsStreaming(_ string) (bool, bool) {
	panic("not implemented")
}

func (ar SourceRouter) BackendCluster(mthd string, metaKey string) (*cluster, error) {
	// unsupported?
	return ar.cluster, nil
}

func (ar SourceRouter) FindBackendCluster(beName string) *cluster {
	// unsupported?
	if beName == ar.cluster.name {
		return ar.cluster
	}
	return nil
}

func (rr SourceRouter) ReplyHandler(sel interface{}) error { // This is a no-op
	return nil
}
