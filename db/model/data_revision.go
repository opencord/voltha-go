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
