package model

import (
	"fmt"
	desc "github.com/golang/protobuf/descriptor"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/opencord/voltha/protos/go/common"
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

	if names, names_exist = GetInstance().ChildrenFieldsCache[msgType.String()]; !names_exist {
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
						fmt.Println("Child key is empty ... moving on")
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
