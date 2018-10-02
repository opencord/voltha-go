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
	"bytes"
	"encoding/gob"
	"reflect"
	"strings"
)

func IsProtoMessage(object interface{}) bool {
	var ok bool

	if object != nil {
		st := reflect.TypeOf(object)
		_, ok = st.MethodByName("ProtoMessage")
	}
	return ok
}

func FindOwnerType(obj reflect.Value, name string, depth int, found bool) reflect.Type {
	k := obj.Kind()
	switch k {
	case reflect.Ptr:
		t := obj.Type().Elem()
		n := reflect.New(t)

		if rc := FindOwnerType(n.Elem(), name, depth+1, false); rc != nil {
			return rc
		}

	case reflect.Struct:
		for i := 0; i < obj.NumField(); i += 1 {
			v := reflect.Indirect(obj)

			json := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")

			if json[0] == name {
				return FindOwnerType(obj.Field(i), name, depth+1, true)
			}

			if rc := FindOwnerType(obj.Field(i), name, depth+1, false); rc != nil {
				return rc
			}
		}
	case reflect.Slice:
		s := reflect.MakeSlice(obj.Type(), 1, 1)
		n := reflect.New(obj.Type())
		n.Elem().Set(s)

		for i := 0; i < n.Elem().Len(); i += 1 {
			if found {
				return reflect.ValueOf(n.Elem().Index(i).Interface()).Type()
			}
		}

		for i := 0; i < obj.Len(); i += 1 {
			if found {
				return obj.Index(i).Type()
			}

			if rc := FindOwnerType(obj.Index(i), name, depth+1, false); rc != nil {
				return rc
			}
		}
	default:
		//log.Debugf("%s Unhandled <%+v> ... It's a %+v\n", prefix, obj, k)
	}

	return nil
}

func FindKeyOwner(iface interface{}, name string, depth int) interface{} {
	obj := reflect.ValueOf(iface)
	k := obj.Kind()
	switch k {
	case reflect.Ptr:
		t := obj.Type().Elem()
		n := reflect.New(t)

		if rc := FindKeyOwner(n.Elem().Interface(), name, depth+1); rc != nil {
			return rc
		}

	case reflect.Struct:
		for i := 0; i < obj.NumField(); i++ {
			json := strings.Split(obj.Type().Field(i).Tag.Get("json"), ",")

			if json[0] == name {
				return obj.Type().Field(i).Type
			}

			if rc := FindKeyOwner(obj.Field(i).Interface(), name, depth+1); rc != nil {
				return rc
			}
		}

	case reflect.Slice:
		s := reflect.MakeSlice(obj.Type(), 1, 1)
		n := reflect.New(obj.Type())
		n.Elem().Set(s)

		for i := 0; i < n.Elem().Len(); i += 1 {
			if rc := FindKeyOwner(n.Elem().Index(i).Interface(), name, depth+1); rc != nil {
				return rc
			}
		}
	default:
		//log.Debugf("%s Unhandled <%+v> ... It's a %+v\n", prefix, obj, k)
	}

	return nil
}

// FIXME: Need to figure out if GetAttributeValue and GetAttributeStructure can become one
// Code is repeated in both, but outputs have a different purpose
// Left as-is for now to get things working
func GetAttributeValue(data interface{}, name string, depth int) (string, reflect.Value) {
	var attribName string
	var attribValue reflect.Value
	obj := reflect.ValueOf(data)

	if !obj.IsValid() {
		return attribName, attribValue
	}

	k := obj.Kind()
	switch k {
	case reflect.Ptr:
		if obj.IsNil() {
			return attribName, attribValue
		}

		if attribName, attribValue = GetAttributeValue(obj.Elem().Interface(), name, depth+1); attribValue.IsValid() {
			return attribName, attribValue
		}

	case reflect.Struct:
		for i := 0; i < obj.NumField(); i++ {
			json := strings.Split(obj.Type().Field(i).Tag.Get("json"), ",")

			if json[0] == name {
				return obj.Type().Field(i).Name, obj.Field(i)
			}

			if obj.Field(i).IsValid() {
				if attribName, attribValue = GetAttributeValue(obj.Field(i).Interface(), name, depth+1); attribValue.IsValid() {
					return attribName, attribValue
				}
			}
		}

	case reflect.Slice:
		s := reflect.MakeSlice(obj.Type(), 1, 1)
		n := reflect.New(obj.Type())
		n.Elem().Set(s)

		for i := 0; i < obj.Len(); i += 1 {
			if attribName, attribValue = GetAttributeValue(obj.Index(i).Interface(), name, depth+1); attribValue.IsValid() {
				return attribName, attribValue
			}
		}
	default:
		//log.Debugf("%s Unhandled <%+v> ... It's a %+v\n", prefix, obj, k)
	}

	return attribName, attribValue

}

// FIXME: See GetAttributeValue(...) comment
func GetAttributeStructure(data interface{}, name string, depth int) reflect.StructField {
	var result reflect.StructField
	obj := reflect.ValueOf(data)

	if !obj.IsValid() {
		return result
	}

	k := obj.Kind()
	switch k {
	case reflect.Ptr:
		t := obj.Type().Elem()
		n := reflect.New(t)

		if rc := GetAttributeStructure(n.Elem().Interface(), name, depth+1); rc.Name != "" {
			return rc
		}

	case reflect.Struct:
		for i := 0; i < obj.NumField(); i++ {
			v := reflect.Indirect(obj)
			json := strings.Split(obj.Type().Field(i).Tag.Get("json"), ",")

			if json[0] == name {
				return v.Type().Field(i)
			}

			if obj.Field(i).IsValid() {
				if rc := GetAttributeStructure(obj.Field(i).Interface(), name, depth+1); rc.Name != "" {
					return rc
				}
			}
		}

	case reflect.Slice:
		s := reflect.MakeSlice(obj.Type(), 1, 1)
		n := reflect.New(obj.Type())
		n.Elem().Set(s)

		for i := 0; i < obj.Len(); i += 1 {
			if rc := GetAttributeStructure(obj.Index(i).Interface(), name, depth+1); rc.Name != "" {
				return rc
			}

		}
	default:
		//log.Debugf("%s Unhandled <%+v> ... It's a %+v\n", prefix, obj, k)
	}

	return result

}

func Clone2(a interface{}) interface{} {
	b := reflect.ValueOf(a)
	buff := new(bytes.Buffer)
	enc := gob.NewEncoder(buff)
	dec := gob.NewDecoder(buff)
	enc.Encode(a)
	dec.Decode(b.Elem().Interface())

	return b.Interface()
}

func Clone(a, b interface{}) interface{} {
	buff := new(bytes.Buffer)
	enc := gob.NewEncoder(buff)
	dec := gob.NewDecoder(buff)
	enc.Encode(a)
	dec.Decode(b)
	return b
}
