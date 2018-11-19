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
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"strconv"
	"sync"
	"time"
)

//TODO: missing cache stuff
//TODO: missing retry stuff
//TODO: missing proper logging

// Backend structure holds details for accessing the kv store
type Backend struct {
	sync.RWMutex
	Client     kvstore.Client
	StoreType  string
	Host       string
	Port       int
	Timeout    int
	PathPrefix string
}

// NewBackend creates a new instance of a Backend structure
func NewBackend(storeType string, host string, port int, timeout int, pathPrefix string) *Backend {
	var err error

	b := &Backend{
		StoreType:  storeType,
		Host:       host,
		Port:       port,
		Timeout:    timeout,
		PathPrefix: pathPrefix,
	}

	address := host + ":" + strconv.Itoa(port)
	if b.Client, err = b.newClient(address, timeout); err != nil {
		log.Errorf("failed to create a new kv Client - %s", err.Error())
	}

	return b
}

func (b *Backend) newClient(address string, timeout int) (kvstore.Client, error) {
	switch b.StoreType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("Unsupported KV store")
}

func (b *Backend) makePath(key string) string {
	path := fmt.Sprintf("%s/%s", b.PathPrefix, key)
	return path
}

// List retrieves one or more items that match the specified key
func (b *Backend) List(key string) (map[string]*kvstore.KVPair, error) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugf("List key: %s, path: %s", key, formattedPath)

	return b.Client.List(formattedPath, b.Timeout)
}

// Get retrieves an item that matches the specified key
func (b *Backend) Get(key string) (*kvstore.KVPair, error) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugf("Get key: %s, path: %s", key, formattedPath)

	start := time.Now()
	err, pair := b.Client.Get(formattedPath, b.Timeout)
	stop := time.Now()

	GetProfiling().AddToDatabaseRetrieveTime(stop.Sub(start).Seconds())

	return err, pair
}

// Put stores an item value under the specifed key
func (b *Backend) Put(key string, value interface{}) error {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugf("Put key: %s, value: %+v, path: %s", key, string(value.([]byte)), formattedPath)

	return b.Client.Put(formattedPath, value, b.Timeout)
}

// Delete removes an item under the specified key
func (b *Backend) Delete(key string) error {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugf("Delete key: %s, path: %s", key, formattedPath)

	return b.Client.Delete(formattedPath, b.Timeout)
}
