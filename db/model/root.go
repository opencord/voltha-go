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
	"github.com/opencord/voltha-go/common/log"
	"reflect"
	"time"
)

type Root struct {
	*Node
	DirtyNodes            map[string][]*Node
	KvStore               *Backend
	Loading               bool
	RevisionClass         interface{}
	Callbacks             []CallbackTuple
	NotificationCallbacks []CallbackTuple
}

func NewRoot(initialData interface{}, kvStore *Backend) *Root {
	root := &Root{}
	root.KvStore = kvStore
	root.DirtyNodes = make(map[string][]*Node)
	root.Loading = false
	if kvStore != nil {
		root.RevisionClass = reflect.TypeOf(PersistedRevision{})
	} else {
		root.RevisionClass = reflect.TypeOf(NonPersistedRevision{})
	}
	root.Callbacks = []CallbackTuple{}
	root.NotificationCallbacks = []CallbackTuple{}

	root.Node = NewNode(root, initialData, false, "")

	return root
}

func (r *Root) MakeRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	if r.RevisionClass.(reflect.Type) == reflect.TypeOf(PersistedRevision{}) {
		return NewPersistedRevision(branch, data, children)
	}

	return NewNonPersistedRevision(branch, data, children)
}

func (r *Root) MakeTxBranch() string {
	txid_bin, _ := uuid.New().MarshalBinary()
	txid := hex.EncodeToString(txid_bin)[:12]
	r.DirtyNodes[txid] = []*Node{r.Node}
	r.Node.makeTxBranch(txid)
	return txid
}

func (r *Root) DeleteTxBranch(txid string) {
	for _, dirtyNode := range r.DirtyNodes[txid] {
		dirtyNode.deleteTxBranch(txid)
	}
	delete(r.DirtyNodes, txid)
}

func (r *Root) FoldTxBranch(txid string) {
	if _, err := r.mergeTxBranch(txid, true); err != nil {
		r.DeleteTxBranch(txid)
	} else {
		r.mergeTxBranch(txid, false)
		r.executeCallbacks()
	}
}

func (r *Root) executeCallbacks() {
	for len(r.Callbacks) > 0 {
		callback := r.Callbacks[0]
		r.Callbacks = r.Callbacks[1:]
		callback.Execute(nil)
	}
	for len(r.NotificationCallbacks) > 0 {
		callback := r.NotificationCallbacks[0]
		r.NotificationCallbacks = r.NotificationCallbacks[1:]
		callback.Execute(nil)
	}
}

func (r *Root) noCallbacks() bool {
	return len(r.Callbacks) == 0
}

func (r *Root) addCallback(callback CallbackFunction, args ...interface{}) {
	r.Callbacks = append(r.Callbacks, CallbackTuple{callback, args})
}
func (r *Root) addNotificationCallback(callback CallbackFunction, args ...interface{}) {
	r.NotificationCallbacks = append(r.NotificationCallbacks, CallbackTuple{callback, args})
}

func (r *Root) Update(path string, data interface{}, strict bool, txid string, makeBranch t_makeBranch) Revision {
	var result Revision

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.noCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *Node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.makeTxBranch(txid)
		}
		result = r.Node.Update(path, data, strict, txid, trackDirty)
	} else {
		result = r.Node.Update(path, data, strict, "", nil)
	}

	r.executeCallbacks()

	return result
}

func (r *Root) Add(path string, data interface{}, txid string, makeBranch t_makeBranch) Revision {
	var result Revision

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.noCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *Node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.makeTxBranch(txid)
		}
		result = r.Node.Add(path, data, txid, trackDirty)
	} else {
		result = r.Node.Add(path, data, "", nil)
	}

	r.executeCallbacks()

	return result
}

func (r *Root) Remove(path string, txid string, makeBranch t_makeBranch) Revision {
	var result Revision

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.noCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *Node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
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
	//root := NewRoot(rootClass, nil)
	//root.KvStore = r.KvStore
	r.loadFromPersistence(rootClass)
	return r
}

func (r *Root) MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	r.Node.MakeLatest(branch, revision, changeAnnouncement)

	if r.KvStore != nil && branch.Txid == "" {
		tags := make(map[string]string)
		for k, v := range r.Tags {
			tags[k] = v.GetHash()
		}
		data := &rootData{
			Latest: branch.Latest.GetHash(),
			Tags:   tags,
		}
		if blob, err := json.Marshal(data); err != nil {
			// TODO report error
		} else {
			log.Debugf("Changing root to : %s", string(blob))
			if err := r.KvStore.Put("root", blob); err != nil {
				log.Errorf("failed to properly put value in kvstore - err: %s", err.Error())
			}
		}
	}
}

func (r *Root) LoadLatest(hash string) {
	r.Node.LoadLatest(r.KvStore, hash)
}

type rootData struct {
	Latest string            `json:latest`
	Tags   map[string]string `json:tags`
}

func (r *Root) loadFromPersistence(rootClass interface{}) {
	var data rootData

	r.Loading = true
	blob, _ := r.KvStore.Get("root")

	start := time.Now()
	if err := json.Unmarshal(blob.Value.([]byte), &data); err != nil {
		fmt.Errorf("problem to unmarshal blob - error:%s\n", err.Error())
	}
	stop := time.Now()
	GetProfiling().AddToInMemoryModelTime(stop.Sub(start).Seconds())
	for tag, hash := range data.Tags {
		r.Node.LoadLatest(r.KvStore, hash)
		r.Node.Tags[tag] = r.Node.Latest()
	}

	r.Node.LoadLatest(r.KvStore, data.Latest)
	r.Loading = false
}
