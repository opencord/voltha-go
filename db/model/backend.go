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
