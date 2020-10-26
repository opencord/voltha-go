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
	"reflect"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
)

// RequestTimestamp attribute used to store a timestamp in the context object
const RequestTimestamp contextKey = "request-timestamp"

type contextKey string

// Path holds the information for a specific location within the data model
type Path struct {
	kvStore *db.Backend
	path    string
}

// NewDBPath returns a path to the default db location
func NewDBPath(kvStore *db.Backend) *Path {
	return &Path{kvStore: kvStore}
}

// SubPath returns a path which points to a more specific db location
func (p *Path) SubPath(path string) *Path {
	path = strings.TrimRight(strings.TrimLeft(path, "/"), "/")
	return &Path{
		kvStore: p.kvStore,
		path:    p.path + path + "/",
	}
}

// Proxy contains all the information needed to reference a specific resource within the kv
type Proxy Path

// Proxy returns a new proxy which references the specified resource
func (p *Path) Proxy(resource string) *Proxy {
	resource = strings.TrimRight(strings.TrimLeft(resource, "/"), "/")
	return &Proxy{
		kvStore: p.kvStore,
		path:    p.path + resource + "/",
	}
}

// List will retrieve information from the data model at the proxy's path location, and write it to the target slice
// target must be a type of the form *[]<proto.Message Type>  For example: *[]*voltha.Device
func (p *Proxy) List(ctx context.Context, target interface{}) error {
	logger.Debugw(ctx, "proxy-list", log.Fields{
		"path": p.path,
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

	blobs, err := p.kvStore.List(ctx, p.path)
	if err != nil {
		return fmt.Errorf("failed to retrieve %s from kvstore: %s", p.path, err)
	}

	logger.Debugw(ctx, "parsing-data-blobs", log.Fields{
		"path": p.path,
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

// Get will retrieve information from the data model at the proxy's path location, and write it to target
func (p *Proxy) Get(ctx context.Context, id string, target proto.Message) (bool, error) {
	completePath := p.path + id

	logger.Debugw(ctx, "proxy-get", log.Fields{
		"path": completePath,
	})

	blob, err := p.kvStore.Get(ctx, completePath)
	if err != nil {
		return false, fmt.Errorf("failed to retrieve %s from kvstore: %s", completePath, err)
	} else if blob == nil {
		return false, nil // this blob does not exist
	}

	logger.Debugw(ctx, "parsing-data-blobs", log.Fields{
		"path": completePath,
	})

	if err := proto.Unmarshal(blob.Value.([]byte), target); err != nil {
		return false, fmt.Errorf("failed to unmarshal %s: %s", blob.Key, err)
	}
	return true, nil
}

// Set will add new or update existing entry at the proxy's path location
func (p *Proxy) Set(ctx context.Context, id string, data proto.Message) error {
	completePath := p.path + id

	logger.Debugw(ctx, "proxy-add", log.Fields{
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

// Remove will delete an entry at the proxy's path location
func (p *Proxy) Remove(ctx context.Context, id string) error {
	completePath := p.path + id

	logger.Debugw(ctx, "proxy-remove", log.Fields{
		"path": completePath,
	})

	if err := p.kvStore.Delete(ctx, completePath); err != nil {
		return fmt.Errorf("unable to delete %s in kvStore: %s", completePath, err)
	}
	return nil
}
