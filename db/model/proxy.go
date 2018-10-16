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
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

type OperationContext struct {
	Path      string
	Data      interface{}
	FieldName string
	ChildKey  string
}

func NewOperationContext(path string, data interface{}, fieldName string, childKey string) *OperationContext {
	oc := &OperationContext{
		Path:      path,
		Data:      data,
		FieldName: fieldName,
		ChildKey:  childKey,
	}
	return oc
}

func (oc *OperationContext) Update(data interface{}) *OperationContext {
	oc.Data = data
	return oc
}

type Proxy struct {
	sync.Mutex
	Root      *root
	Path      string
	FullPath  string
	Exclusive bool
	Callbacks map[CallbackType]map[string]CallbackTuple
}

func NewProxy(root *root, path string, fullPath string, exclusive bool) *Proxy {
	callbacks := make(map[CallbackType]map[string]CallbackTuple)
	if fullPath == "/" {
		fullPath = ""
	}
	p := &Proxy{
		Root:      root,
		Exclusive: exclusive,
		Path:      path,
		FullPath:  fullPath,
		Callbacks: callbacks,
	}
	return p
}

func (p *Proxy) parseForControlledPath(path string) (pathLock string, controlled bool) {
	// TODO: Add other path prefixes they may need control
	if strings.HasPrefix(path, "/devices") {
		split := strings.SplitN(path, "/", -1)
		switch len(split) {
		case 2:
			controlled = false
			pathLock = ""
			break
		case 3:
			fallthrough
		default:
			pathLock = fmt.Sprintf("%s/%s", split[1], split[2])
			controlled = true
		}
	}
	return pathLock, controlled
}

func (p *Proxy) Get(path string, depth int, deep bool, txid string) interface{} {
	var effectivePath string
	if path == "/" {
		effectivePath = p.FullPath
	} else {
		effectivePath = p.FullPath + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	var pac interface{}
	var exists bool

	p.Lock()
	if pac, exists = GetProxyAccessControl().Cache.LoadOrStore(path, NewProxyAccessControl(p, pathLock)); !exists {
		defer GetProxyAccessControl().Cache.Delete(pathLock)
	}
	p.Unlock()

	return pac.(ProxyAccessControl).Get(path, depth, deep, txid, controlled)
}

func (p *Proxy) Update(path string, data interface{}, strict bool, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.Path
		effectivePath = p.FullPath
	} else {
		fullPath = p.Path + path
		effectivePath = p.FullPath + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	var pac interface{}
	var exists bool

	p.Lock()
	if pac, exists = GetProxyAccessControl().Cache.LoadOrStore(path, NewProxyAccessControl(p, pathLock)); !exists {
		defer GetProxyAccessControl().Cache.Delete(pathLock)
	}
	p.Unlock()

	return pac.(ProxyAccessControl).Update(fullPath, data, strict, txid, controlled)
}

func (p *Proxy) Add(path string, data interface{}, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.Path
		effectivePath = p.FullPath
	} else {
		fullPath = p.Path + path
		effectivePath = p.FullPath + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	var pac interface{}
	var exists bool

	p.Lock()
	if pac, exists = GetProxyAccessControl().Cache.LoadOrStore(path, NewProxyAccessControl(p, pathLock)); !exists {
		defer GetProxyAccessControl().Cache.Delete(pathLock)
	}
	p.Unlock()

	return pac.(ProxyAccessControl).Add(fullPath, data, txid, controlled)
}

func (p *Proxy) Remove(path string, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.Path
		effectivePath = p.FullPath
	} else {
		fullPath = p.Path + path
		effectivePath = p.FullPath + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	var pac interface{}
	var exists bool

	p.Lock()
	if pac, exists = GetProxyAccessControl().Cache.LoadOrStore(path, NewProxyAccessControl(p, pathLock)); !exists {
		defer GetProxyAccessControl().Cache.Delete(pathLock)
	}
	p.Unlock()

	return pac.(ProxyAccessControl).Remove(fullPath, txid, controlled)
}

func (p *Proxy) OpenTransaction() *Transaction {
	txid := p.Root.MakeTxBranch()
	return NewTransaction(p, txid)
}

func (p *Proxy) commitTransaction(txid string) {
	p.Root.FoldTxBranch(txid)
}

func (p *Proxy) cancelTransaction(txid string) {
	p.Root.DeleteTxBranch(txid)
}

type CallbackFunction func(args ...interface{}) interface{}
type CallbackTuple struct {
	callback CallbackFunction
	args     []interface{}
}

func (tuple *CallbackTuple) Execute(contextArgs interface{}) interface{} {
	args := []interface{}{}
	args = append(args, tuple.args...)
	if contextArgs != nil {
		args = append(args, contextArgs)
	}
	return tuple.callback(args...)
}

func (p *Proxy) RegisterCallback(callbackType CallbackType, callback CallbackFunction, args ...interface{}) {
	if _, exists := p.Callbacks[callbackType]; !exists {
		p.Callbacks[callbackType] = make(map[string]CallbackTuple)
	}
	funcName := runtime.FuncForPC(reflect.ValueOf(callback).Pointer()).Name()
	log.Debugf("value of function: %s", funcName)
	funcHash := fmt.Sprintf("%x", md5.Sum([]byte(funcName)))[:12]

	p.Callbacks[callbackType][funcHash] = CallbackTuple{callback, args}
}

func (p *Proxy) UnregisterCallback(callbackType CallbackType, callback CallbackFunction, args ...interface{}) {
	if _, exists := p.Callbacks[callbackType]; !exists {
		log.Errorf("no such callback type - %s", callbackType.String())
		return
	}
	// TODO: Not sure if this is the best way to do it.
	funcName := runtime.FuncForPC(reflect.ValueOf(callback).Pointer()).Name()
	log.Debugf("value of function: %s", funcName)
	funcHash := fmt.Sprintf("%x", md5.Sum([]byte(funcName)))[:12]
	if _, exists := p.Callbacks[callbackType][funcHash]; !exists {
		log.Errorf("function with hash value: '%s' not registered with callback type: '%s'", funcHash, callbackType)
		return
	}
	delete(p.Callbacks[callbackType], funcHash)
}

func (p *Proxy) invoke(callback CallbackTuple, context ...interface{}) (result interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			errStr := fmt.Sprintf("callback error occurred: %+v", r)
			err = errors.New(errStr)
			log.Error(errStr)
		}
	}()

	result = callback.Execute(context)

	return result, err
}

func (p *Proxy) InvokeCallbacks(args ...interface{}) (result interface{}) {
	callbackType := args[0].(CallbackType)
	proceedOnError := args[1].(bool)
	context := args[2:]

	var err error

	if _, exists := p.Callbacks[callbackType]; exists {
		for _, callback := range p.Callbacks[callbackType] {
			if result, err = p.invoke(callback, context); err != nil {
				if !proceedOnError {
					log.Info("An error occurred.  Stopping callback invocation")
					break
				}
				log.Info("An error occurred.  Invoking next callback")
			}
		}
	}
	return result
}
