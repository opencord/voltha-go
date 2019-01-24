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
	"strconv"
	"io/ioutil"
	"google.golang.org/grpc"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type AffinityRouter struct {
	name string
	routerType int // TODO: This is probably not needed 
	association int
	routingField string
	grpcService string
	protoDescriptor *pb.FileDescriptorSet
	methodMap map[string]byte
	nbBindingMthdMap map[string]byte
	bkndClstr *backendCluster
	affinity map[string]*backend
	curBknd **backend
}

func newAffinityRouter(rconf *RouterConfig, config *RouteConfig) (Router,error) {
	var err error = nil
	var rtrn_err bool = false
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
	bptr =  nil
	dr := AffinityRouter{
		name:config.Name,
		grpcService:rconf.ProtoService,
		affinity:make(map[string]*backend),
		methodMap:make(map[string]byte),
		nbBindingMthdMap:make(map[string]byte),
		curBknd:&bptr,
		//serialNo:0,
	}
	// An association must exist
	dr.association = strIndex(rAssnNames, config.Association)
	if dr.association == 0 {
		if config.Association == "" {
		    log.Error("An association must be specified")
		} else {
		    log.Errorf("The association '%s' is not valid", config.Association)
		}
		rtrn_err = true
	}


	// This has already been validated bfore this function
	// is called so just use it.
	for idx := range rTypeNames {
		if config.Type == rTypeNames[idx] {
			dr.routerType = idx
			break
		}
	}

	// Load the protobuf descriptor file
	dr.protoDescriptor = &pb.FileDescriptorSet{}
	fb, err := ioutil.ReadFile(config.ProtoFile);
	if err != nil {
		log.Errorf("Could not open proto file '%s'",config.ProtoFile)
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
		mthd string
		field string
	}
	var msgs map[key]byte = make(map[key]byte)
	for _,f := range dr.protoDescriptor.File {
		// Build a temporary map of message types by name.
		for _,m := range f.MessageType {
			for _,fld := range m.Field {
				log.Debugf("Processing message '%s', field '%s'", *m.Name, *fld.Name)
				msgs[key{*m.Name, *fld.Name}] = byte(*fld.Number)
			}
		}
	}
	log.Debugf("The map contains: %v", msgs)
	for _,f := range dr.protoDescriptor.File {
		if *f.Package == rconf.ProtoPackage {
			for _, s:= range f.Service {
				if *s.Name == rconf.ProtoService {
					log.Debugf("Loading package data '%s' for service '%s' for router '%s'", *f.Package, *s.Name, dr.name)
					// Now create a map keyed by method name with the value being the
					// field number of the route selector.
					var ok bool
					for _,m := range s.Method {
						// Find the input type in the messages and extract the
						// field number and save it for future reference.
						log.Debugf("Processing method '%s'",*m.Name)
						// Determine if this is a method we're supposed to be processing.
						if needMethod(*m.Name, config) == true {
							log.Debugf("Enabling method '%s'",*m.Name)
							// The input type has the package name prepended to it. Remove it.
							in := (*m.InputType)[len(rconf.ProtoPackage)+2:]
							dr.methodMap[*m.Name], ok = msgs[key{in, config.RouteField}]
							if ok == false {
								log.Errorf("Method '%s' has no field named '%s' in it's parameter message '%s'",
											*m.Name, config.RouteField, in)
								rtrn_err = true
							}
						}
						// The sb method is always included in the methods so we can check it here too.
						if needSbMethod(*m.Name, config) == true {
							log.Debugf("Enabling southbound method '%s'",*m.Name)
							// The output type has the package name prepended to it. Remove it.
							out := (*m.OutputType)[len(rconf.ProtoPackage)+2:]
							dr.nbBindingMthdMap[*m.Name], ok = msgs[key{out, config.RouteField}]
							if ok == false {
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
	if dr.bkndClstr, ok = bClusters[config.backendCluster.Name]; ok == false {
		if dr.bkndClstr, err = newBackendCluster(config.backendCluster); err != nil {
			log.Errorf("Could not create a backend for router %s", config.Name)
			rtrn_err = true
		}
	}

	if rtrn_err {
		return dr,errors.New(fmt.Sprintf("Failed to create a new router '%s'",dr.name))
	}

	return dr,nil
}

func needSbMethod(mthd string, conf *RouteConfig) bool {
	for _,m := range conf.NbBindingMethods {
		if mthd == m {
			return true
		}
	}
	return false
}

func needMethod(mthd string, conf *RouteConfig) bool {
	for _,m := range conf.Methods {
		if mthd == m {
			return true
		}
	}
	return false
}

func (r AffinityRouter) Service() (string) {
	return r.grpcService
}

func (r AffinityRouter) Name() (string) {
	return r.name
}

func (r AffinityRouter) skipField(data *[]byte, idx *int) (error) {
	switch (*data)[*idx]&3 {
		case 0: // Varint
		(*idx)++
			for (*data)[*idx] >= 128 { (*idx)++}
		case 1: // 64 bit
			(*idx)+= 9
		case 2: // Length delimited
			(*idx)++
			b := proto.NewBuffer((*data)[*idx:])
			t , _ := b.DecodeVarint()
			(*idx) += int(t)+1
		case 3: // Deprecated
		case 4: // Deprecated
		case 5: // 32 bit
			(*idx)+= 5
	}
	return nil
}

func (r AffinityRouter) decodeProtoField(payload []byte, fieldId byte) (string, error) {
	idx :=0
	b := proto.NewBuffer([]byte{})
	b.DebugPrint("The Buffer", payload)
	for { // Find the route selector field
		log.Debugf("Decoding afinity value attributeNumber: %d from %v at index %d", fieldId, payload, idx)
		log.Debugf("Attempting match with payload: %d, methodTable: %d", payload[idx], fieldId)
		if payload[idx]>>3 == fieldId {
			log.Debugf("Method match with payload: %d, methodTable: %d", payload[idx], fieldId)
			// TODO: Consider supporting other selector types.... Way, way in the future
			// ok, the future is now, support strings as well... ugh.
			var selector string
			switch payload[idx]&3 {
				case 0: // Integer
					b.SetBuf(payload[idx+1:])
					v,e := b.DecodeVarint()
					if e == nil {
						log.Debugf("Decoded the ing field: %v", v)
						selector = strconv.Itoa(int(v))
					} else {
						log.Errorf("Failed to decode varint %v", e)
						return "", e
					}
				case 2: // Length delimited AKA string
					b.SetBuf(payload[idx+1:])
					v,e := b.DecodeStringBytes()
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
		} else if err := r.skipField(&payload, &idx); err != nil {
			log.Errorf("Parsing message failed %v", err)
			return "", err
		}
	}
}

func (r AffinityRouter) Route(sel interface{}) *backend {
	switch sl := sel.(type) {
		case *nbFrame:
			log.Debugf("Route called for nbFrame with method %s", sl.mthdSlice[REQ_METHOD]);
			// Check if this method should be affinity bound from the
			// reply rather than the request.
			if _,ok := r.nbBindingMthdMap[sl.mthdSlice[REQ_METHOD]]; ok == true {
				var err error
				log.Debugf("Method '%s' affinity binds on reply", sl.mthdSlice[REQ_METHOD])
				// Just round robin route the southbound request
				if *r.curBknd, err = r.bkndClstr.nextBackend(*r.curBknd,BE_SEQ_RR); err == nil {
					return *r.curBknd
				} else {
					sl.err = err
					return nil
				}
			}
			// Not a south affinity binding method, proceed with north affinity binding.
			if selector,err := r.decodeProtoField(sl.payload, r.methodMap[sl.mthdSlice[REQ_METHOD]]); err == nil {
				if rtrn,ok := r.affinity[selector]; ok {
					return rtrn
				} else {
					// The selector isn't in the map, create a new affinity mapping
					log.Debugf("MUST CREATE A NEW AFFINITY MAP ENTRY!!")
					var err error
					if *r.curBknd, err = r.bkndClstr.nextBackend(*r.curBknd,BE_SEQ_RR); err == nil {
						r.setAffinity(selector, *r.curBknd)
						//r.affinity[selector] = *r.curBknd
						//log.Debugf("New affinity set to backend %s",(*r.curBknd).name)
						return *r.curBknd
					} else {
						sl.err = err
						return nil
					}
				}
			}
		default:
			log.Errorf("Internal: invalid data type in Route call %v", sel);
			return nil
	}
	log.Errorf("Bad lookup in affinity map %v",r.affinity);
	return nil
}

func (ar AffinityRouter) GetMetaKeyVal(serverStream grpc.ServerStream) (string,string,error) {
	return "","",nil
}

func (ar AffinityRouter) BackendCluster(mthd string, metaKey string) (*backendCluster,error) {
	return ar.bkndClstr, nil
}

func (ar AffinityRouter) FindBackendCluster(beName string) *backendCluster {
	if beName == ar.bkndClstr.name {
		return ar.bkndClstr
	}
	return nil
}

func (r AffinityRouter) ReplyHandler(sel interface{}) error {
	switch sl := sel.(type) {
		case *sbFrame:
			sl.lck.Lock()
			defer sl.lck.Unlock()
			log.Debugf("Reply handler called for sbFrame with method %s", sl.method);
			// Determine if reply action is required.
			if fld, ok := r.nbBindingMthdMap[sl.method]; ok == true && len(sl.payload) > 0 {
				// Extract the field value from the frame and
				// and set affinity accordingly
				if selector,err := r.decodeProtoField(sl.payload, fld); err == nil {
					log.Debug("Settign affinity on reply")
					if r.setAffinity(selector, sl.be) != nil {
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
	if be2,ok := ar.affinity[key]; ok == false {
		ar.affinity[key] = be
		log.Debugf("New affinity set to backend %s for key %s",be.name, key)
	} else if be2 != be {
		err := errors.New(fmt.Sprintf("Attempting multiple sets of affinity for key %s to backend %s from %s on router %s",
							key, be.name, ar.affinity[key].name, ar.name))
		log.Error(err)
		return err
	}
	return nil
}
