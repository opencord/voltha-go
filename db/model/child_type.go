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
	"fmt"
	desc "github.com/golang/protobuf/descriptor"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/opencord/voltha-go/protos/common"
	"reflect"
	"strconv"
	"sync"
)

type singleton struct {
	ChildrenFieldsCache map[interface{}]map[string]*ChildType
}

var instance *singleton
var once sync.Once

func GetInstance() *singleton {
	once.Do(func() {
		instance = &singleton{}
	})
	return instance
}

type ChildType struct {
	ClassModule string
	ClassType   reflect.Type
	IsContainer bool
	Key         string
	KeyFromStr  func(s string) interface{}
}

func ChildrenFields(cls interface{}) map[string]*ChildType {
	if cls == nil {
		return nil
	}
	var names map[string]*ChildType
	var names_exist bool

	if GetInstance().ChildrenFieldsCache == nil {
		GetInstance().ChildrenFieldsCache = make(map[interface{}]map[string]*ChildType)
	}

	msgType := reflect.TypeOf(cls)

	inst := GetInstance()

	if names, names_exist = inst.ChildrenFieldsCache[msgType.String()]; !names_exist {
		names = make(map[string]*ChildType)

		_, md := desc.ForMessage(cls.(desc.Message))

		// TODO: Do we need to validate MD for nil, panic or exception?
		for _, field := range md.Field {
			if options := field.GetOptions(); options != nil {
				if proto.HasExtension(options, common.E_ChildNode) {
					isContainer := *field.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
					meta, _ := proto.GetExtension(options, common.E_ChildNode)
					var keyFromStr func(string) interface{}

					if meta.(*common.ChildNode).GetKey() == "" {
						//fmt.Println("Child key is empty ... moving on")
					} else {
						parentType := FindOwnerType(reflect.ValueOf(cls), field.GetName(), 0, false)
						keyType := FindKeyOwner(reflect.New(parentType).Elem().Interface(), meta.(*common.ChildNode).GetKey(), 0)

						switch keyType.(reflect.Type).Name() {
						case "string":
							keyFromStr = func(s string) interface{} {
								return s
							}
						case "int32":
							fallthrough
						case "int64":
							fallthrough
						case "uint32":
							fallthrough
						case "uint64":
							keyFromStr = func(s string) interface{} {
								i, _ := strconv.Atoi(s)
								return i
							}
						default:
							fmt.Errorf("Key type not implemented - type: %s\n", keyType.(reflect.Type))
						}

						ct := ChildType{
							ClassModule: parentType.String(),
							ClassType:   parentType,
							IsContainer: isContainer,
							Key:         meta.(*common.ChildNode).GetKey(),
							KeyFromStr:  keyFromStr,
						}

						names[field.GetName()] = &ct

					}
				}
			}
		}

		GetInstance().ChildrenFieldsCache[msgType.String()] = names
	}

	return names
}
