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
	"github.com/opencord/voltha-go/db/kvstore"
	"strconv"
)

//TODO: missing cache stuff
//TODO: missing retry stuff
//TODO: missing proper logging

type Backend struct {
	Client     kvstore.Client
	StoreType  string
	Host       string
	Port       int
	Timeout    int
	PathPrefix string
}

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
		fmt.Errorf("failed to create a new kv Client - %s", err.Error())
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
	return fmt.Sprintf("%s/%s", b.PathPrefix, key)
}
func (b *Backend) Get(key string) (*kvstore.KVPair, error) {
	return b.Client.Get(b.makePath(key), b.Timeout)
}
func (b *Backend) Put(key string, value interface{}) error {
	return b.Client.Put(b.makePath(key), value, b.Timeout)
}
func (b *Backend) Delete(key string) error {
	return b.Client.Delete(b.makePath(key), b.Timeout)
}
