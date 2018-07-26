package model

import (
	"encoding/json"
	"fmt"
	"testing"
)

const (
	ETCD_KV    = "etcd"
	CONSUL_KV  = "consul"
	INVALID_KV = "invalid"

	etcd_host   = "10.104.149.247"
	etcd_port   = 2379

	/*
		To debug locally with the remote ETCD container

		ssh -f -N vagrant@10.100.198.220 -L 22379:10.104.149.247:2379
	*/
	//etcd_host   = "localhost"
	//etcd_port   = 22379
	consul_host = "k8s-consul"
	consul_port = 30080
	timeout     = 5
	prefix      = "backend/test"
	key         = "stephane/1"
	value       = "barbarie"
)

var (
	etcd_backend   *Backend
	consul_backend *Backend
)

func Test_Etcd_Backend_New(t *testing.T) {
	etcd_backend = NewBackend(ETCD_KV, etcd_host, etcd_port, timeout, prefix)
}

func Test_Etcd_Backend_Put(t *testing.T) {
	etcd_backend.Put(key, value)

}

func Test_Etcd_Backend_Get(t *testing.T) {
	if pair, err := etcd_backend.Client.Get("service/voltha/data/core/0001/root", timeout); err != nil {
		t.Errorf("backend get failed - %s", err.Error())
	} else {
		j, _ := json.Marshal(pair)
		t.Logf("pair: %s", string(j))
	}
}

func Test_Etcd_Backend_GetRoot(t *testing.T) {
	if pair, err := etcd_backend.Get(key); err != nil {
		t.Errorf("backend get failed - %s", err.Error())
	} else {
		j, _ := json.Marshal(pair)
		t.Logf("pair: %s", string(j))
		if pair.Key != (prefix + "/" + key) {
			t.Errorf("backend key differs - key: %s, expected: %s", pair.Key, key)
		}

		s := fmt.Sprintf("%s", pair.Value)
		if s != value {
			t.Errorf("backend value differs - value: %s, expected:%s", pair.Value, value)
		}
	}
}

func Test_Etcd_Backend_Delete(t *testing.T) {
	if err := etcd_backend.Delete(key); err != nil {
		t.Errorf("backend delete failed - %s", err.Error())
	}
	//if _, err := backend.Client.Get(key, Timeout); err == nil {
	//	t.Errorf("backend delete failed - %s", err.Error())
	//}
}

func Test_Consul_Backend_New(t *testing.T) {
	consul_backend = NewBackend(CONSUL_KV, consul_host, consul_port, timeout, prefix)
}

func Test_Consul_Backend_Put(t *testing.T) {
	consul_backend.Put(key, value)

}

func Test_Consul_Backend_Get(t *testing.T) {
	if pair, err := consul_backend.Get(key); err != nil {
		t.Errorf("backend get failed - %s", err.Error())
	} else {
		j, _ := json.Marshal(pair)
		t.Logf("pair: %s", string(j))
		if pair.Key != (prefix + "/" + key) {
			t.Errorf("backend key differs - key: %s, expected: %s", pair.Key, key)
		}

		v := fmt.Sprintf("%s", pair.Value)
		if v != value {
			t.Errorf("backend value differs - value: %s, expected:%s", pair.Value, value)
		}
	}
}

func Test_Consul_Backend_Delete(t *testing.T) {
	if err := consul_backend.Delete(key); err != nil {
		t.Errorf("backend delete failed - %s", err.Error())
	}
	//if _, err := backend.Client.Get(key, Timeout); err == nil {
	//	t.Errorf("backend delete failed - %s", err.Error())
	//}
}
