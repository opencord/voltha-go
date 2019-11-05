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

package db

import (
	"errors"
	"fmt"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"strconv"
	"sync"
)

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
		log.Errorw("failed-to-create-kv-client",
			log.Fields{
				"type": storeType, "host": host, "port": port,
				"timeout": timeout, "prefix": pathPrefix,
				"error": err.Error(),
			})
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
	return nil, errors.New("unsupported-kv-store")
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
	log.Debugw("listing-key", log.Fields{"key": key, "path": formattedPath})

	return b.Client.List(formattedPath, b.Timeout)
}

// Get retrieves an item that matches the specified key
func (b *Backend) Get(key string) (*kvstore.KVPair, error) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("getting-key", log.Fields{"key": key, "path": formattedPath})

	return b.Client.Get(formattedPath, b.Timeout)
}

// Put stores an item value under the specifed key
func (b *Backend) Put(key string, value interface{}) error {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("putting-key", log.Fields{"key": key, "value": string(value.([]byte)), "path": formattedPath})

	return b.Client.Put(formattedPath, value, b.Timeout)
}

// Delete removes an item under the specified key
func (b *Backend) Delete(key string) error {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("deleting-key", log.Fields{"key": key, "path": formattedPath})

	return b.Client.Delete(formattedPath, b.Timeout)
}

// CreateWatch starts watching events for the specified key
func (b *Backend) CreateWatch(key string) chan *kvstore.Event {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("creating-key-watch", log.Fields{"key": key, "path": formattedPath})

	return b.Client.Watch(formattedPath)
}

// DeleteWatch stops watching events for the specified key
func (b *Backend) DeleteWatch(key string, ch chan *kvstore.Event) {
	b.Lock()
	defer b.Unlock()

	formattedPath := b.makePath(key)
	log.Debugw("deleting-key-watch", log.Fields{"key": key, "path": formattedPath})

	b.Client.CloseWatch(formattedPath, ch)
}
