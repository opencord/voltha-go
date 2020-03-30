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
	"context"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"reflect"
)

// RequestTimestamp attribute used to store a timestamp in the context object
const RequestTimestamp contextKey = "request-timestamp"

type contextKey string

// Proxy holds the information for a specific location with the data model
type Proxy struct {
	kvStore *db.Backend
	path    string
}

// NewProxy instantiates a new proxy to a specific location
func NewProxy(kvStore *db.Backend, path string) *Proxy {
	if path == "/" {
		path = ""
	}
	return &Proxy{
		kvStore: kvStore,
		path:    path,
	}
}

// List will retrieve information from the data model at the specified path location, and write it to the target slice
// target must be a type of the form *[]<proto.Message Type>  For example: *[]*voltha.Device
func (p *Proxy) List(ctx context.Context, path string, target interface{}) error {
	completePath := p.path + path

	logger.Debugw("proxy-list", log.Fields{
		"path": completePath,
	})

	// verify type of target is *[]*<type>
	pointerType := reflect.TypeOf(target) // *[]*<type>
	if pointerType.Kind() != reflect.Ptr {
		return errors.New("target is not of type *[]*<type>")
	}
	sliceType := pointerType.Elem() // []*type
	if sliceType.Kind() != reflect.Slice {
		return errors.New("target is not of type *[]*<type>")
	}
	elemType := sliceType.Elem() // *type
	if sliceType.Implements(reflect.TypeOf((*proto.Message)(nil)).Elem()) {
		return errors.New("target slice does not contain elements of type proto.Message")
	}
	dataType := elemType.Elem() // type

	blobs, err := p.kvStore.List(ctx, completePath)
	if err != nil {
		return fmt.Errorf("failed to retrieve %s from kvstore: %s", path, err)
	}

	logger.Debugw("parsing-data-blobs", log.Fields{
		"path": path,
		"size": len(blobs),
	})

	ret := reflect.MakeSlice(sliceType, len(blobs), len(blobs))
	i := 0
	for _, blob := range blobs {
		data := reflect.New(dataType)
		if err := proto.Unmarshal(blob.Value.([]byte), data.Interface().(proto.Message)); err != nil {
			return fmt.Errorf("failed to unmarshal %s: %s", blob.Key, err)
		}
		ret.Index(i).Set(data)
		i++
	}
	reflect.ValueOf(target).Elem().Set(ret)
	return nil
}

// Get will retrieve information from the data model at the specified path location, and write it to target
func (p *Proxy) Get(ctx context.Context, path string, target proto.Message) (bool, error) {
	completePath := p.path + path

	logger.Debugw("proxy-get", log.Fields{
		"path": completePath,
	})

	blob, err := p.kvStore.Get(ctx, completePath)
	if err != nil {
		return false, fmt.Errorf("failed to retrieve %s from kvstore: %s", path, err)
	} else if blob == nil {
		return false, nil // this blob does not exist
	}

	logger.Debugw("parsing-data-blobs", log.Fields{
		"path": path,
	})

	if err := proto.Unmarshal(blob.Value.([]byte), target); err != nil {
		return false, fmt.Errorf("failed to unmarshal %s: %s", blob.Key, err)
	}
	return true, nil
}

// Update will modify information in the data model at the specified location with the provided data
func (p *Proxy) Update(ctx context.Context, path string, data proto.Message) error {
	return p.add(ctx, path, data)
}

// AddWithID will insert new data at specified location.
// This method also allows the user to specify the ID.
func (p *Proxy) AddWithID(ctx context.Context, path string, id string, data proto.Message) error {
	return p.add(ctx, path+"/"+id, data)
}

// add will insert new data at specified location.
func (p *Proxy) add(ctx context.Context, path string, data proto.Message) error {
	completePath := p.path + path

	logger.Debugw("proxy-add", log.Fields{
		"path": completePath,
	})

	blob, err := proto.Marshal(data)
	if err != nil {
		return fmt.Errorf("unable to save to kvStore, error marshalling: %s", err)
	}

	if err := p.kvStore.Put(ctx, completePath, blob); err != nil {
		return fmt.Errorf("unable to write to kvStore: %s", err)
	}
	return nil
}

// Remove will delete an entry at the specified location
func (p *Proxy) Remove(ctx context.Context, path string) error {
	completePath := p.path + path

	logger.Debugw("proxy-remove", log.Fields{
		"path": completePath,
	})

	if err := p.kvStore.Delete(ctx, completePath); err != nil {
		return fmt.Errorf("unable to delete %s in kvStore: %s", completePath, err)
	}
	return nil
}
