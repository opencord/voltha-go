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
	"github.com/opencord/voltha-go/common/log"
	"runtime/debug"
	"sync"
	"time"
)

type _singletonProxyAccessControl struct {
	Cache sync.Map
}

var _instanceProxyAccessControl *_singletonProxyAccessControl
var _onceProxyAccessControl sync.Once

func GetProxyAccessControl() *_singletonProxyAccessControl {
	_onceProxyAccessControl.Do(func() {
		_instanceProxyAccessControl = &_singletonProxyAccessControl{}
	})
	return _instanceProxyAccessControl
}

type ProxyAccessControl interface {
	Get(path string, depth int, deep bool, txid string, control bool) interface{}
	Update(path string, data interface{}, strict bool, txid string, control bool) interface{}
	Add(path string, data interface{}, txid string, control bool) interface{}
	Remove(path string, txid string, control bool) interface{}
}

type proxyAccessControl struct {
	//sync.Mutex
	Proxy    *Proxy
	PathLock chan struct{}
	Path     string

	start time.Time
	stop  time.Time
}

func NewProxyAccessControl(proxy *Proxy, path string) ProxyAccessControl {
	return &proxyAccessControl{
		Proxy:    proxy,
		Path:     path,
		PathLock: make(chan struct{}, 1),
	}
}

func (pac *proxyAccessControl) lock() {
	log.CleanUp()
	log.Debugf("Before lock ... pac: %+v, stack = %s", pac, string(debug.Stack()))
	pac.PathLock <- struct{}{}
	pac.start = time.Now()
	log.Debugf("Got lock ... pac: %+v, stack = %s", pac, string(debug.Stack()))
	//time.Sleep(1 * time.Second)
	log.Debugf("<<<<< %s >>>>>> locked, stack=%s", pac.Path, string(debug.Stack()))
}
func (pac *proxyAccessControl) unlock() {
	log.CleanUp()
	log.Debugf("Before unlock ... pac: %+v, stack = %s", pac, string(debug.Stack()))
	<-pac.PathLock
	pac.stop = time.Now()
	GetProfiling().AddToInMemoryLockTime(pac.stop.Sub(pac.start).Seconds())
	log.Debugf("Got unlock ... pac: %+v, stack = %s", pac, string(debug.Stack()))
	log.Debugf("<<<<< %s >>>>>> unlocked, stack=%s", pac.Path, string(debug.Stack()))
}

func (pac *proxyAccessControl) Get(path string, depth int, deep bool, txid string, control bool) interface{} {
	if control {
		pac.lock()
		defer pac.unlock()
		log.Debugf("controlling get, stack = %s", string(debug.Stack()))
	}
	pac.Proxy.Root.Proxy = pac.Proxy
	return pac.Proxy.Root.Get(path, "", depth, deep, txid)
}
func (pac *proxyAccessControl) Update(path string, data interface{}, strict bool, txid string, control bool) interface{} {
	if control {
		pac.lock()
		defer pac.unlock()
		log.Debugf("controlling update, stack = %s", string(debug.Stack()))
	}
	pac.Proxy.Root.Proxy = pac.Proxy
	return pac.Proxy.Root.Update(path, data, strict, txid, nil)
}
func (pac *proxyAccessControl) Add(path string, data interface{}, txid string, control bool) interface{} {
	if control {
		pac.lock()
		defer pac.unlock()
		log.Debugf("controlling add, stack = %s", string(debug.Stack()))
	}
	pac.Proxy.Root.Proxy = pac.Proxy
	return pac.Proxy.Root.Add(path, data, txid, nil)
}
func (pac *proxyAccessControl) Remove(path string, txid string, control bool) interface{} {
	if control {
		pac.lock()
		defer pac.unlock()
		log.Debugf("controlling remove, stack = %s", string(debug.Stack()))
	}
	pac.Proxy.Root.Proxy = pac.Proxy
	return pac.Proxy.Root.Remove(path, txid, nil)
}
