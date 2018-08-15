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
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/golang/protobuf/proto"
	"reflect"
)

type DataRevision struct {
	Data interface{}
	Hash string
}

func NewDataRevision(data interface{}) *DataRevision {
	cdr := &DataRevision{}
	cdr.Data = data
	cdr.Hash = cdr.hashData(data)

	return cdr
}

func (cr *DataRevision) hashData(data interface{}) string {
	var buffer bytes.Buffer

	if IsProtoMessage(data) {
		if pbdata, err := proto.Marshal(data.(proto.Message)); err != nil {
			fmt.Errorf("problem to marshal protobuf data --> err: %s", err.Error())
		} else {
			buffer.Write(pbdata)
		}

	} else if reflect.ValueOf(data).IsValid() {
		dataObj := reflect.New(reflect.TypeOf(data).Elem())
		if json, err := json.Marshal(dataObj.Interface()); err != nil {
			fmt.Errorf("problem to marshal data --> err: %s", err.Error())
		} else {
			buffer.Write(json)
		}
	} else {
		dataObj := reflect.New(reflect.TypeOf(data).Elem())
		buffer.Write(dataObj.Bytes())
	}

	return fmt.Sprintf("%x", md5.Sum(buffer.Bytes()))[:12]
}
