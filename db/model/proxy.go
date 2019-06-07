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

// OperationContext holds details on the information used during an operation
type OperationContext struct {
	Path      string
	Data      interface{}
	FieldName string
	ChildKey  string
}

// NewOperationContext instantiates a new OperationContext structure
func NewOperationContext(path string, data interface{}, fieldName string, childKey string) *OperationContext {
	oc := &OperationContext{
		Path:      path,
		Data:      data,
		FieldName: fieldName,
		ChildKey:  childKey,
	}
	return oc
}

// Update applies new data to the context structure
func (oc *OperationContext) Update(data interface{}) *OperationContext {
	oc.Data = data
	return oc
}

// Proxy holds the information for a specific location with the data model
type Proxy struct {
	sync.RWMutex
	Root       *root
	Node       *node
	ParentNode *node
	Path       string
	FullPath   string
	Exclusive  bool
	Callbacks  map[CallbackType]map[string]*CallbackTuple
	Operation  ProxyOperation
}

// NewProxy instantiates a new proxy to a specific location
func NewProxy(root *root, node *node, parentNode *node, path string, fullPath string, exclusive bool) *Proxy {
	callbacks := make(map[CallbackType]map[string]*CallbackTuple)
	if fullPath == "/" {
		fullPath = ""
	}
	p := &Proxy{
		Root:       root,
		Node:       node,
		ParentNode: parentNode,
		Exclusive:  exclusive,
		Path:       path,
		FullPath:   fullPath,
		Callbacks:  callbacks,
	}
	return p
}

// GetRoot returns the root attribute of the proxy
func (p *Proxy) GetRoot() *root {
	return p.Root
}

// getPath returns the path attribute of the proxy
func (p *Proxy) getPath() string {
	return p.Path
}

// getFullPath returns the full path attribute of the proxy
func (p *Proxy) getFullPath() string {
	return p.FullPath
}

// getCallbacks returns the full list of callbacks associated to the proxy
func (p *Proxy) getCallbacks(callbackType CallbackType) map[string]*CallbackTuple {
	if p != nil {
		if cb, exists := p.Callbacks[callbackType]; exists {
			return cb
		}
	} else {
		log.Debugw("proxy-is-nil", log.Fields{"callback-type": callbackType.String()})
	}
	return nil
}

// getCallback returns a specific callback matching the type and function hash
func (p *Proxy) getCallback(callbackType CallbackType, funcHash string) *CallbackTuple {
	p.Lock()
	defer p.Unlock()
	if tuple, exists := p.Callbacks[callbackType][funcHash]; exists {
		return tuple
	}
	return nil
}

// setCallbacks applies a callbacks list to a type
func (p *Proxy) setCallbacks(callbackType CallbackType, callbacks map[string]*CallbackTuple) {
	p.Lock()
	defer p.Unlock()
	p.Callbacks[callbackType] = callbacks
}

// setCallback applies a callback to a type and hash value
func (p *Proxy) setCallback(callbackType CallbackType, funcHash string, tuple *CallbackTuple) {
	p.Lock()
	defer p.Unlock()
	p.Callbacks[callbackType][funcHash] = tuple
}

// DeleteCallback removes a callback matching the type and hash
func (p *Proxy) DeleteCallback(callbackType CallbackType, funcHash string) {
	p.Lock()
	defer p.Unlock()
	delete(p.Callbacks[callbackType], funcHash)
}

// CallbackType is an enumerated value to express when a callback should be executed
type ProxyOperation uint8

// Enumerated list of callback types
const (
	PROXY_GET ProxyOperation = iota
	PROXY_LIST
	PROXY_ADD
	PROXY_UPDATE
	PROXY_REMOVE
	PROXY_CREATE
	PROXY_WATCH
)

var proxyOperationTypes = []string{
	"PROXY_GET",
	"PROXY_LIST",
	"PROXY_ADD",
	"PROXY_UPDATE",
	"PROXY_REMOVE",
	"PROXY_CREATE",
	"PROXY_WATCH",
}

func (t ProxyOperation) String() string {
	return proxyOperationTypes[t]
}

// parseForControlledPath verifies if a proxy path matches a pattern
// for locations that need to be access controlled.
func (p *Proxy) parseForControlledPath(path string) (pathLock string, controlled bool) {
	// TODO: Add other path prefixes that may need control
	if strings.HasPrefix(path, "/devices") ||
		strings.HasPrefix(path, "/logical_devices") ||
		strings.HasPrefix(path, "/adapters") {

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

// List will retrieve information from the data model at the specified path location
// A list operation will force access to persistence storage
func (p *Proxy) List(path string, depth int, deep bool, txid string) interface{} {
	var effectivePath string
	if path == "/" {
		effectivePath = p.getFullPath()
	} else {
		effectivePath = p.getFullPath() + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-list", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(effectivePath, p, pathLock)
	defer PAC().ReleasePath(pathLock)
	p.Operation = PROXY_LIST
	pac.SetProxy(p)
	defer func(op ProxyOperation) {
		pac.getProxy().Operation = op
	}(PROXY_GET)

	rv := pac.List(path, depth, deep, txid, controlled)

	return rv
}

// Get will retrieve information from the data model at the specified path location
func (p *Proxy) Get(path string, depth int, deep bool, txid string) interface{} {
	var effectivePath string
	if path == "/" {
		effectivePath = p.getFullPath()
	} else {
		effectivePath = p.getFullPath() + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-get", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(effectivePath, p, pathLock)
	defer PAC().ReleasePath(pathLock)
	p.Operation = PROXY_GET
	pac.SetProxy(p)

	rv := pac.Get(path, depth, deep, txid, controlled)

	return rv
}

// Update will modify information in the data model at the specified location with the provided data
func (p *Proxy) Update(path string, data interface{}, strict bool, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.getPath()
		effectivePath = p.getFullPath()
	} else {
		fullPath = p.getPath() + path
		effectivePath = p.getFullPath() + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-update", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"full":       fullPath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(effectivePath, p, pathLock)
	defer PAC().ReleasePath(pathLock)

	p.Operation = PROXY_UPDATE
	pac.SetProxy(p)
	defer func(op ProxyOperation) {
		pac.getProxy().Operation = op
	}(PROXY_GET)
	log.Debugw("proxy-operation--update", log.Fields{"operation": p.Operation})

	return pac.Update(fullPath, data, strict, txid, controlled)
}

// AddWithID will insert new data at specified location.
// This method also allows the user to specify the ID of the data entry to ensure
// that access control is active while inserting the information.
func (p *Proxy) AddWithID(path string, id string, data interface{}, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.getPath()
		effectivePath = p.getFullPath()
	} else {
		fullPath = p.getPath() + path
		effectivePath = p.getFullPath() + path + "/" + id
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-add-with-id", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"full":       fullPath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(path, p, pathLock)
	defer PAC().ReleasePath(pathLock)

	p.Operation = PROXY_ADD
	defer func(op ProxyOperation) {
		pac.getProxy().Operation = op
	}(PROXY_GET)

	pac.SetProxy(p)

	log.Debugw("proxy-operation--add", log.Fields{"operation": p.Operation})

	return pac.Add(fullPath, data, txid, controlled)
}

// Add will insert new data at specified location.
func (p *Proxy) Add(path string, data interface{}, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.getPath()
		effectivePath = p.getFullPath()
	} else {
		fullPath = p.getPath() + path
		effectivePath = p.getFullPath() + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-add", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"full":       fullPath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(path, p, pathLock)
	defer PAC().ReleasePath(pathLock)

	p.Operation = PROXY_ADD
	pac.SetProxy(p)
	defer func(op ProxyOperation) {
		pac.getProxy().Operation = op
	}(PROXY_GET)

	log.Debugw("proxy-operation--add", log.Fields{"operation": p.Operation})

	return pac.Add(fullPath, data, txid, controlled)
}

// Remove will delete an entry at the specified location
func (p *Proxy) Remove(path string, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.getPath()
		effectivePath = p.getFullPath()
	} else {
		fullPath = p.getPath() + path
		effectivePath = p.getFullPath() + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-remove", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"full":       fullPath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(effectivePath, p, pathLock)
	defer PAC().ReleasePath(pathLock)

	p.Operation = PROXY_REMOVE
	pac.SetProxy(p)
	defer func(op ProxyOperation) {
		pac.getProxy().Operation = op
	}(PROXY_GET)

	log.Debugw("proxy-operation--remove", log.Fields{"operation": p.Operation})

	return pac.Remove(fullPath, txid, controlled)
}

// CreateProxy to interact with specific path directly
func (p *Proxy) CreateProxy(path string, exclusive bool) *Proxy {
	if !strings.HasPrefix(path, "/") {
		log.Errorf("invalid path: %s", path)
		return nil
	}

	var fullPath string
	var effectivePath string
	if path == "/" {
		fullPath = p.getPath()
		effectivePath = p.getFullPath()
	} else {
		fullPath = p.getPath() + path
		effectivePath = p.getFullPath() + path
	}

	pathLock, controlled := p.parseForControlledPath(effectivePath)

	log.Debugw("proxy-create", log.Fields{
		"path":       path,
		"effective":  effectivePath,
		"full":       fullPath,
		"pathLock":   pathLock,
		"controlled": controlled,
	})

	pac := PAC().ReservePath(path, p, pathLock)
	defer PAC().ReleasePath(pathLock)

	p.Operation = PROXY_CREATE
	pac.SetProxy(p)
	defer func(op ProxyOperation) {
		pac.getProxy().Operation = op
	}(PROXY_GET)

	log.Debugw("proxy-operation--create-proxy", log.Fields{"operation": p.Operation})

	return pac.CreateProxy(fullPath, exclusive, controlled)
}

// OpenTransaction creates a new transaction branch to isolate operations made to the data model
func (p *Proxy) OpenTransaction() *Transaction {
	txid := p.GetRoot().MakeTxBranch()
	return NewTransaction(p, txid)
}

// commitTransaction will apply and merge modifications made in the transaction branch to the data model
func (p *Proxy) commitTransaction(txid string) {
	p.GetRoot().FoldTxBranch(txid)
}

// cancelTransaction will terminate a transaction branch along will all changes within it
func (p *Proxy) cancelTransaction(txid string) {
	p.GetRoot().DeleteTxBranch(txid)
}

// CallbackFunction is a type used to define callback functions
type CallbackFunction func(args ...interface{}) interface{}

// CallbackTuple holds the function and arguments details of a callback
type CallbackTuple struct {
	callback CallbackFunction
	args     []interface{}
}

// Execute will process the a callback with its provided arguments
func (tuple *CallbackTuple) Execute(contextArgs []interface{}) interface{} {
	args := []interface{}{}

	for _, ta := range tuple.args {
		args = append(args, ta)
	}

	if contextArgs != nil {
		for _, ca := range contextArgs {
			args = append(args, ca)
		}
	}

	return tuple.callback(args...)
}

// RegisterCallback associates a callback to the proxy
func (p *Proxy) RegisterCallback(callbackType CallbackType, callback CallbackFunction, args ...interface{}) {
	if p.getCallbacks(callbackType) == nil {
		p.setCallbacks(callbackType, make(map[string]*CallbackTuple))
	}
	funcName := runtime.FuncForPC(reflect.ValueOf(callback).Pointer()).Name()
	log.Debugf("value of function: %s", funcName)
	funcHash := fmt.Sprintf("%x", md5.Sum([]byte(funcName)))[:12]

	p.setCallback(callbackType, funcHash, &CallbackTuple{callback, args})
}

// UnregisterCallback removes references to a callback within a proxy
func (p *Proxy) UnregisterCallback(callbackType CallbackType, callback CallbackFunction, args ...interface{}) {
	if p.getCallbacks(callbackType) == nil {
		log.Errorf("no such callback type - %s", callbackType.String())
		return
	}

	funcName := runtime.FuncForPC(reflect.ValueOf(callback).Pointer()).Name()
	funcHash := fmt.Sprintf("%x", md5.Sum([]byte(funcName)))[:12]

	log.Debugf("value of function: %s", funcName)

	if p.getCallback(callbackType, funcHash) == nil {
		log.Errorf("function with hash value: '%s' not registered with callback type: '%s'", funcHash, callbackType)
		return
	}

	p.DeleteCallback(callbackType, funcHash)
}

func (p *Proxy) invoke(callback *CallbackTuple, context []interface{}) (result interface{}, err error) {
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

// InvokeCallbacks executes all callbacks associated to a specific type
func (p *Proxy) InvokeCallbacks(args ...interface{}) (result interface{}) {
	callbackType := args[0].(CallbackType)
	proceedOnError := args[1].(bool)
	context := args[2:]

	var err error

	if callbacks := p.getCallbacks(callbackType); callbacks != nil {
		p.Lock()
		for _, callback := range callbacks {
			if result, err = p.invoke(callback, context); err != nil {
				if !proceedOnError {
					log.Info("An error occurred.  Stopping callback invocation")
					break
				}
				log.Info("An error occurred.  Invoking next callback")
			}
		}
		p.Unlock()
	}

	return result
}
