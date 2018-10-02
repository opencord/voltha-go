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
)

type Transaction struct {
	proxy *Proxy
	txid  string
}

func NewTransaction(proxy *Proxy, txid string) *Transaction {
	tx := &Transaction{
		proxy: proxy,
		txid:  txid,
	}
	return tx
}
func (t *Transaction) Get(path string, depth int, deep bool) interface{} {
	if t.txid == "" {
		log.Errorf("closed transaction")
		return nil
	}
	// TODO: need to review the return values at the different layers!!!!!
	return t.proxy.Get(path, depth, deep, t.txid)
}
func (t *Transaction) Update(path string, data interface{}, strict bool) interface{} {
	if t.txid == "" {
		log.Errorf("closed transaction")
		return nil
	}
	return t.proxy.Update(path, data, strict, t.txid)
}
func (t *Transaction) Add(path string, data interface{}) interface{} {
	if t.txid == "" {
		log.Errorf("closed transaction")
		return nil
	}
	return t.proxy.Add(path, data, t.txid)
}
func (t *Transaction) Remove(path string) interface{} {
	if t.txid == "" {
		log.Errorf("closed transaction")
		return nil
	}
	return t.proxy.Remove(path, t.txid)
}
func (t *Transaction) Cancel() {
	t.proxy.cancelTransaction(t.txid)
	t.txid = ""
}
func (t *Transaction) Commit() {
	t.proxy.commitTransaction(t.txid)
	t.txid = ""
}
