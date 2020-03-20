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

package model

import (
	desc "github.com/golang/protobuf/descriptor"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/opencord/voltha-protos/v3/go/common"
	"reflect"
	"strconv"
	"sync"
)

type childTypesSingleton struct {
	mutex sync.RWMutex
	cache map[reflect.Type]map[string]*ChildType
}

var childTypes = &childTypesSingleton{cache: make(map[reflect.Type]map[string]*ChildType)}

func (s *childTypesSingleton) GetCacheEntry(key reflect.Type) (map[string]*ChildType, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	childTypeMap, exists := s.cache[key]
	return childTypeMap, exists
}

func (s *childTypesSingleton) SetCacheEntry(key reflect.Type, value map[string]*ChildType) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cache[key] = value
}

// ChildType structure contains construct details of an object
type ChildType struct {
	ClassModule string
	ClassType   reflect.Type
	IsContainer bool
	Key         string
	KeyFromStr  func(s string) interface{}
}

// ChildrenFields retrieves list of child objects associated to a given type
func ChildrenFields(cls interface{}) map[string]*ChildType {
	if cls == nil {
		return nil
	}

	msgType := reflect.TypeOf(cls)

	if fields, have := childTypes.GetCacheEntry(msgType); have {
		return fields
	}

	fields := make(map[string]*ChildType)
	_, md := desc.ForMessage(cls.(desc.Message))

	// TODO: Do we need to validate MD for nil, panic or exception?
	for _, field := range md.Field {
		if options := field.GetOptions(); options != nil {
			if proto.HasExtension(options, common.E_ChildNode) {
				isContainer := *field.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
				meta, _ := proto.GetExtension(options, common.E_ChildNode)

				var keyFromStr func(string) interface{}
				var ct ChildType

				parentType := FindOwnerType(reflect.ValueOf(cls), field.GetName(), 0, false)
				if meta.(*common.ChildNode).GetKey() != "" {
					keyType := FindKeyOwner(reflect.New(parentType).Elem().Interface(), meta.(*common.ChildNode).GetKey(), 0)

					switch keyType.(reflect.Type).Name() {
					case "string":
						keyFromStr = func(s string) interface{} {
							return s
						}
					case "int32":
						keyFromStr = func(s string) interface{} {
							i, _ := strconv.Atoi(s)
							return int32(i)
						}
					case "int64":
						keyFromStr = func(s string) interface{} {
							i, _ := strconv.Atoi(s)
							return int64(i)
						}
					case "uint32":
						keyFromStr = func(s string) interface{} {
							i, _ := strconv.Atoi(s)
							return uint32(i)
						}
					case "uint64":
						keyFromStr = func(s string) interface{} {
							i, _ := strconv.Atoi(s)
							return uint64(i)
						}
					default:
						logger.Errorf("Key type not implemented - type: %s\n", keyType.(reflect.Type))
					}
				}

				ct = ChildType{
					ClassModule: parentType.String(),
					ClassType:   parentType,
					IsContainer: isContainer,
					Key:         meta.(*common.ChildNode).GetKey(),
					KeyFromStr:  keyFromStr,
				}

				fields[field.GetName()] = &ct
			}
		}
	}

	// If called multiple times in quick succession w/ the same message types, it is possible for different cache entries to be returned.
	// This should not be an issue, as the cache is merely for optimization purposes.
	childTypes.SetCacheEntry(msgType, fields)
	return fields
}
