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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"reflect"
)

type Root struct {
	*Node
	DirtyNodes            map[string]*Node
	KvStore               *Backend
	Loading               bool
	RevisionClass         interface{}
	Callbacks             []func() interface{}
	NotificationCallbacks []func() interface{}
}

func NewRoot(initialData interface{}, kvStore *Backend, revisionClass interface{}) *Root {
	root := &Root{}
	root.KvStore = kvStore
	root.DirtyNodes = make(map[string]*Node)
	root.Loading = false
	if kvStore != nil /*&& FIXME: RevisionClass is a subclass of PersistedConfigRevision */ {
		revisionClass = reflect.TypeOf(PersistedRevision{})
	}
	root.RevisionClass = revisionClass
	root.Callbacks = []func() interface{}{}
	root.NotificationCallbacks = []func() interface{}{}

	root.Node = NewNode(root, initialData, false, "")

	return root
}

func (r *Root) makeRevision(branch *Branch, data interface{}, children map[string][]*Revision) *Revision {

	return &Revision{}
}

func (r *Root) makeTxBranch() string {
	txid_bin, _ := uuid.New().MarshalBinary()
	txid := hex.EncodeToString(txid_bin)[:12]
	r.DirtyNodes[txid] = r.Node
	r.Node.makeTxBranch(txid)
	return txid
}

func (r *Root) deleteTxBranch(txid string) {
	for _, dirtyNode := range r.DirtyNodes {
		dirtyNode.deleteTxBranch(txid)
	}
	delete(r.DirtyNodes, txid)
}

func (r *Root) foldTxBranch(txid string) {
	// TODO: implement foldTxBranch
	// if err := r.Node.mergeTxBranch(txid, dryRun=true); err != nil {
	//   r.deleteTxBranch(txid)
	// } else {
	//   r.Node.mergeTxBranch(txid)
	//   r.executeCallbacks()
	// }
}

func (r *Root) executeCallbacks() {
	for len(r.Callbacks) > 0 {
		callback := r.Callbacks[0]
		r.Callbacks = r.Callbacks[1:]
		callback()
	}
	for len(r.NotificationCallbacks) > 0 {
		callback := r.NotificationCallbacks[0]
		r.NotificationCallbacks = r.NotificationCallbacks[1:]
		callback()
	}
}

func (r *Root) noCallbacks() bool {
	return len(r.Callbacks) == 0
}

func (r *Root) addCallback(callback func() interface{}) {
	r.Callbacks = append(r.Callbacks, callback)
}
func (r *Root) addNotificationCallback(callback func() interface{}) {
	r.NotificationCallbacks = append(r.NotificationCallbacks, callback)
}

func (r *Root) Update(path string, data interface{}, strict bool, txid string, makeBranch t_makeBranch) *Revision {
	var result *Revision
	// FIXME: the more i look at this... i think i need to implement an interface for Node & root

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.noCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		//dirtied := r.DirtyNodes[txid]

		trackDirty := func(node *Node) *Branch {
			//dirtied.Add(Node)
			return node.makeTxBranch(txid)
		}
		result = r.Node.Update(path, data, strict, txid, trackDirty)
	} else {
		result = r.Node.Update(path, data, strict, "", nil)
	}

	r.executeCallbacks()

	return result
}

func (r *Root) Add(path string, data interface{}, txid string, makeBranch t_makeBranch) *Revision {
	var result *Revision
	// FIXME: the more i look at this... i think i need to implement an interface for Node & root

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.noCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		//dirtied := r.DirtyNodes[txid]

		trackDirty := func(node *Node) *Branch {
			//dirtied.Add(Node)
			return node.makeTxBranch(txid)
		}
		result = r.Node.Add(path, data, txid, trackDirty)
	} else {
		result = r.Node.Add(path, data, "", nil)
	}

	r.executeCallbacks()

	return result
}

func (r *Root) Remove(path string, txid string, makeBranch t_makeBranch) *Revision {
	var result *Revision
	// FIXME: the more i look at this... i think i need to implement an interface for Node & root

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.noCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		//dirtied := r.DirtyNodes[txid]

		trackDirty := func(node *Node) *Branch {
			//dirtied.Add(Node)
			return node.makeTxBranch(txid)
		}
		result = r.Node.Remove(path, txid, trackDirty)
	} else {
		result = r.Node.Remove(path, "", nil)
	}

	r.executeCallbacks()

	return result
}

func (r *Root) Load(rootClass interface{}) *Root {
	//fakeKvStore := &Backend{}
	//root := NewRoot(rootClass, fakeKvStore, PersistedRevision{})
	//r.KvStore = KvStore
	r.loadFromPersistence(rootClass)
	return r
}

func (r *Root) LoadLatest(hash string) {
	r.Node.LoadLatest(r.KvStore, hash)
}

type rootData struct {
	Latest string            `json:GetLatest`
	Tags   map[string]string `json:Tags`
}

func (r *Root) loadFromPersistence(rootClass interface{}) {
	var data rootData

	r.Loading = true
	blob, _ := r.KvStore.Get("root")

	if err := json.Unmarshal(blob.Value.([]byte), &data); err != nil {
		fmt.Errorf("problem to unmarshal blob - error:%s\n", err.Error())
	}

	for tag, hash := range data.Tags {
		r.Node.LoadLatest(r.KvStore, hash)
		r.Node.Tags[tag] = r.Node.Latest()
	}

	r.Node.LoadLatest(r.KvStore, data.Latest)
	r.Loading = false
}
