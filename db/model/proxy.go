package model

import (
	"context"
	"fmt"
	"strings"
)

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
	txid := p.Root.makeTxBranch()
	return NewTransaction(p, txid)
}

func (p *Proxy) commitTransaction(txid string) {
	p.Root.foldTxBranch(txid)
}

func (p *Proxy) cancelTransaction(txid string) {
	p.Root.deleteTxBranch(txid)
}

func (p *Proxy) RegisterCallback(callbackType CallbackType, callback func(), args ...interface{}) {
}

func (p *Proxy) UnregisterCallback(callbackType CallbackType, callback func(), args ...interface{}) {
}

func (p *Proxy) InvokeCallback(callbackType CallbackType, context context.Context, proceedOnError bool) {
}
