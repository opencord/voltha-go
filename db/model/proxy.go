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
	"fmt"
	"strings"
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
	Root      *Root
	Node      *Node
	Path      string
	Exclusive bool
	Callbacks []interface{}
}

func NewProxy(root *Root, node *Node, path string, exclusive bool) *Proxy {
	p := &Proxy{
		Root:      root,
		Node:      node,
		Exclusive: exclusive,
		Path:      path,
		Callbacks: []interface{}{},
	}
	return p
}

func (p *Proxy) Get(path string, depth int, deep bool, txid string) interface{} {
	return p.Node.Get(path, "", depth, deep, txid)
}

func (p *Proxy) Update(path string, data interface{}, strict bool, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		fmt.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	if path == "/" {
		fullPath = p.Path
	} else {
		fullPath = p.Path + path
	}
	return p.Node.Update(fullPath, data, strict, txid, nil)
}

func (p *Proxy) Add(path string, data interface{}, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		fmt.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	if path == "/" {
		fullPath = p.Path
	} else {
		fullPath = p.Path + path
	}
	return p.Node.Add(fullPath, data, txid, nil)
}

func (p *Proxy) Remove(path string, txid string) interface{} {
	if !strings.HasPrefix(path, "/") {
		fmt.Errorf("invalid path: %s", path)
		return nil
	}
	var fullPath string
	if path == "/" {
		fullPath = p.Path
	} else {
		fullPath = p.Path + path
	}
	return p.Node.Remove(fullPath, txid, nil)
}

func (p *Proxy) openTransaction() *Transaction {
	txid := p.Root.MakeTxBranch()
	return NewTransaction(p, txid)
}

func (p *Proxy) commitTransaction(txid string) {
	p.Root.FoldTxBranch(txid)
}

func (p *Proxy) cancelTransaction(txid string) {
	p.Root.DeleteTxBranch(txid)
}

func (p *Proxy) RegisterCallback(callbackType CallbackType, callback func(), args ...interface{}) {
}

func (p *Proxy) UnregisterCallback(callbackType CallbackType, callback func(), args ...interface{}) {
}

func (p *Proxy) InvokeCallback(callbackType CallbackType, context context.Context, proceedOnError bool) {
}
