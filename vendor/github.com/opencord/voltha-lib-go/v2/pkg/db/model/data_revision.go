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
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"reflect"
)

// DataRevision stores the data associated to a revision along with its calculated checksum hash value
type DataRevision struct {
	Data interface{}
	Hash string
}

// NewDataRevision creates a new instance of a DataRevision structure
func NewDataRevision(root *root, data interface{}) *DataRevision {
	dr := DataRevision{}
	dr.Data = data
	dr.Hash = dr.hashData(root, data)

	return &dr
}

func (dr *DataRevision) hashData(root *root, data interface{}) string {
	var buffer bytes.Buffer

	if IsProtoMessage(data) {
		if pbdata, err := proto.Marshal(data.(proto.Message)); err != nil {
			log.Debugf("problem to marshal protobuf data --> err: %s", err.Error())
		} else {
			buffer.Write(pbdata)
			// To ensure uniqueness in case data is nil, also include data type
			buffer.Write([]byte(reflect.TypeOf(data).String()))
		}

	} else if reflect.ValueOf(data).IsValid() {
		dataObj := reflect.New(reflect.TypeOf(data).Elem())
		if json, err := json.Marshal(dataObj.Interface()); err != nil {
			log.Debugf("problem to marshal data --> err: %s", err.Error())
		} else {
			buffer.Write(json)
		}
	} else {
		dataObj := reflect.New(reflect.TypeOf(data).Elem())
		buffer.Write(dataObj.Bytes())
	}

	// Add the root pointer that owns the current data for extra uniqueness
	rootPtr := fmt.Sprintf("%p", root)
	buffer.Write([]byte(rootPtr))

	return fmt.Sprintf("%x", md5.Sum(buffer.Bytes()))[:12]
}
