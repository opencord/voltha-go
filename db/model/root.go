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
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	"reflect"
	"time"
)

type Root interface {
	Node
}

type root struct {
	*node

	Callbacks             []CallbackTuple
	NotificationCallbacks []CallbackTuple

	DirtyNodes            map[string][]*node
	KvStore               *Backend
	Loading               bool
	RevisionClass         interface{}
}

func NewRoot(initialData interface{}, kvStore *Backend) *root {
	root := &root{}

	root.KvStore = kvStore
	root.DirtyNodes = make(map[string][]*node)
	root.Loading = false

	if kvStore != nil {
		root.RevisionClass = reflect.TypeOf(PersistedRevision{})
	} else {
		root.RevisionClass = reflect.TypeOf(NonPersistedRevision{})
	}
	root.Callbacks = []CallbackTuple{}
	root.NotificationCallbacks = []CallbackTuple{}

	root.node = NewNode(root, initialData,false, "")

	return root
}

func (r *root) MakeTxBranch() string {
	txid_bin, _ := uuid.New().MarshalBinary()
	txid := hex.EncodeToString(txid_bin)[:12]
	r.DirtyNodes[txid] = []*node{r.node}
	r.node.MakeBranch(txid)
	return txid
}

func (r *root) DeleteTxBranch(txid string) {
	for _, dirtyNode := range r.DirtyNodes[txid] {
		dirtyNode.DeleteBranch(txid)
	}
	delete(r.DirtyNodes, txid)
}

func (r *root) FoldTxBranch(txid string) {
	if _, err := r.MergeBranch(txid, true); err != nil {
		r.DeleteTxBranch(txid)
	} else {
		r.MergeBranch(txid, false)
		r.ExecuteCallbacks()
	}
}

func (r *root) ExecuteCallbacks() {
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

func (r *root) HasCallbacks() bool {
	return len(r.Callbacks) == 0
}

func (r *root) AddCallback(callback CallbackFunction, args ...interface{}) {
	r.Callbacks = append(r.Callbacks, CallbackTuple{callback, args})
}
func (r *root) AddNotificationCallback(callback CallbackFunction, args ...interface{}) {
	r.NotificationCallbacks = append(r.NotificationCallbacks, CallbackTuple{callback, args})
}

func (r *root) Update(path string, data interface{}, strict bool, txid string, makeBranch MakeBranchFunction) Revision {
	var result Revision

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.HasCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.MakeBranch(txid)
		}
		result = r.node.Update(path, data, strict, txid, trackDirty)
	} else {
		result = r.node.Update(path, data, strict, "", nil)
	}

	r.node.ExecuteCallbacks()

	return result
}

func (r *root) Add(path string, data interface{}, txid string, makeBranch MakeBranchFunction) Revision {
	var result Revision

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.HasCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.MakeBranch(txid)
		}
		result = r.node.Add(path, data, txid, trackDirty)
	} else {
		result = r.node.Add(path, data, "", nil)
	}

	r.node.ExecuteCallbacks()

	return result
}

func (r *root) Remove(path string, txid string, makeBranch MakeBranchFunction) Revision {
	var result Revision

	if makeBranch == nil {
		// TODO: raise error
	}

	if r.HasCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.MakeBranch(txid)
		}
		result = r.node.Remove(path, txid, trackDirty)
	} else {
		result = r.node.Remove(path, "", nil)
	}

	r.node.ExecuteCallbacks()

	return result
}

func (r *root) Load(rootClass interface{}) *root {
	//fakeKvStore := &Backend{}
	//root := NewRoot(rootClass, nil)
	//root.KvStore = r.KvStore
	r.LoadFromPersistence(rootClass)
	return r
}

func (r *root) MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	r.makeLatest(branch, revision, changeAnnouncement)
}

func (r *root) makeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	r.node.makeLatest(branch, revision, changeAnnouncement)

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

type rootData struct {
	Latest string            `json:latest`
	Tags   map[string]string `json:tags`
}

func (r *root) LoadFromPersistence(rootClass interface{}) {
	var data rootData

	r.Loading = true
	blob, _ := r.KvStore.Get("root")

	start := time.Now()
	if err := json.Unmarshal(blob.Value.([]byte), &data); err != nil {
		log.Errorf("problem to unmarshal blob - error:%s\n", err.Error())
	}
	stop := time.Now()
	GetProfiling().AddToInMemoryModelTime(stop.Sub(start).Seconds())
	for tag, hash := range data.Tags {
		r.LoadLatest(hash)
		r.Tags[tag] = r.Latest()
	}

	r.LoadLatest(data.Latest)
	r.Loading = false
}
