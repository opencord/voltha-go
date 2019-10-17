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
	"encoding/hex"
	"encoding/json"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/pkg/log"
	"reflect"
	"sync"
)

// Root is used to provide an abstraction to the base root structure
type Root interface {
	Node

	ExecuteCallbacks()
	AddCallback(callback CallbackFunction, args ...interface{})
	AddNotificationCallback(callback CallbackFunction, args ...interface{})
}

// root points to the top of the data model tree or sub-tree identified by a proxy
type root struct {
	*node

	Callbacks             []CallbackTuple
	NotificationCallbacks []CallbackTuple

	DirtyNodes    map[string][]*node
	KvStore       *Backend
	Loading       bool
	RevisionClass interface{}

	mutex sync.RWMutex
}

// NewRoot creates an new instance of a root object
func NewRoot(initialData interface{}, kvStore *Backend) *root {
	root := &root{}

	root.KvStore = kvStore
	root.DirtyNodes = make(map[string][]*node)
	root.Loading = false

	// If there is no storage in place just revert to
	// a non persistent mechanism
	if kvStore != nil {
		root.RevisionClass = reflect.TypeOf(PersistedRevision{})
	} else {
		root.RevisionClass = reflect.TypeOf(NonPersistedRevision{})
	}

	root.Callbacks = []CallbackTuple{}
	root.NotificationCallbacks = []CallbackTuple{}

	root.node = NewNode(root, initialData, false, "")

	return root
}

// MakeTxBranch creates a new transaction branch
func (r *root) MakeTxBranch() string {
	txidBin, _ := uuid.New().MarshalBinary()
	txid := hex.EncodeToString(txidBin)[:12]

	r.DirtyNodes[txid] = []*node{r.node}
	r.node.MakeBranch(txid)

	return txid
}

// DeleteTxBranch removes a transaction branch
func (r *root) DeleteTxBranch(txid string) {
	for _, dirtyNode := range r.DirtyNodes[txid] {
		dirtyNode.DeleteBranch(txid)
	}
	delete(r.DirtyNodes, txid)
	r.node.DeleteBranch(txid)
}

// FoldTxBranch will merge the contents of a transaction branch with the root object
func (r *root) FoldTxBranch(txid string) {
	// Start by doing a dry run of the merge
	// If that fails, it bails out and the branch is deleted
	if _, err := r.node.MergeBranch(txid, true); err != nil {
		// Merge operation fails
		r.DeleteTxBranch(txid)
	} else {
		r.node.MergeBranch(txid, false)
		r.node.GetRoot().ExecuteCallbacks()
		r.DeleteTxBranch(txid)
	}
}

// ExecuteCallbacks will invoke all the callbacks linked to root object
func (r *root) ExecuteCallbacks() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for len(r.Callbacks) > 0 {
		callback := r.Callbacks[0]
		r.Callbacks = r.Callbacks[1:]
		go callback.Execute(nil)
	}
	//for len(r.NotificationCallbacks) > 0 {
	//	callback := r.NotificationCallbacks[0]
	//	r.NotificationCallbacks = r.NotificationCallbacks[1:]
	//	go callback.Execute(nil)
	//}
}

func (r *root) hasCallbacks() bool {
	return len(r.Callbacks) == 0
}

// getCallbacks returns the available callbacks
func (r *root) GetCallbacks() []CallbackTuple {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.Callbacks
}

// getCallbacks returns the available notification callbacks
func (r *root) GetNotificationCallbacks() []CallbackTuple {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.NotificationCallbacks
}

// AddCallback inserts a new callback with its arguments
func (r *root) AddCallback(callback CallbackFunction, args ...interface{}) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.Callbacks = append(r.Callbacks, CallbackTuple{callback, args})
}

// AddNotificationCallback inserts a new notification callback with its arguments
func (r *root) AddNotificationCallback(callback CallbackFunction, args ...interface{}) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.NotificationCallbacks = append(r.NotificationCallbacks, CallbackTuple{callback, args})
}

func (r *root) syncParent(childRev Revision, txid string) {
	data := proto.Clone(r.GetProxy().ParentNode.Latest().GetData().(proto.Message))

	for fieldName, _ := range ChildrenFields(data) {
		childDataName, childDataHolder := GetAttributeValue(data, fieldName, 0)
		if reflect.TypeOf(childRev.GetData()) == reflect.TypeOf(childDataHolder.Interface()) {
			childDataHolder = reflect.ValueOf(childRev.GetData())
			reflect.ValueOf(data).Elem().FieldByName(childDataName).Set(childDataHolder)
		}
	}

	r.GetProxy().ParentNode.Latest().SetConfig(NewDataRevision(r.GetProxy().ParentNode.GetRoot(), data))
	r.GetProxy().ParentNode.Latest(txid).Finalize(false)
}

// Update modifies the content of an object at a given path with the provided data
func (r *root) Update(ctx context.Context, path string, data interface{}, strict bool, txid string, makeBranch MakeBranchFunction) Revision {
	var result Revision

	if makeBranch != nil {
		// TODO: raise error
	}

	if r.hasCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.MakeBranch(txid)
		}
		result = r.node.Update(ctx, path, data, strict, txid, trackDirty)
	} else {
		result = r.node.Update(ctx, path, data, strict, "", nil)
	}

	if result != nil {
		if r.GetProxy().FullPath != r.GetProxy().Path {
			r.syncParent(result, txid)
		} else {
			result.Finalize(false)
		}
	}

	r.node.GetRoot().ExecuteCallbacks()

	return result
}

// Add creates a new object at the given path with the provided data
func (r *root) Add(ctx context.Context, path string, data interface{}, txid string, makeBranch MakeBranchFunction) Revision {
	var result Revision

	if makeBranch != nil {
		// TODO: raise error
	}

	if r.hasCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.MakeBranch(txid)
		}
		result = r.node.Add(ctx, path, data, txid, trackDirty)
	} else {
		result = r.node.Add(ctx, path, data, "", nil)
	}

	if result != nil {
		result.Finalize(true)
		r.node.GetRoot().ExecuteCallbacks()
	}
	return result
}

// Remove discards an object at a given path
func (r *root) Remove(ctx context.Context, path string, txid string, makeBranch MakeBranchFunction) Revision {
	var result Revision

	if makeBranch != nil {
		// TODO: raise error
	}

	if r.hasCallbacks() {
		// TODO: raise error
	}

	if txid != "" {
		trackDirty := func(node *node) *Branch {
			r.DirtyNodes[txid] = append(r.DirtyNodes[txid], node)
			return node.MakeBranch(txid)
		}
		result = r.node.Remove(ctx, path, txid, trackDirty)
	} else {
		result = r.node.Remove(ctx, path, "", nil)
	}

	r.node.GetRoot().ExecuteCallbacks()

	return result
}

// MakeLatest updates a branch with the latest node revision
func (r *root) MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	r.makeLatest(branch, revision, changeAnnouncement)
}

func (r *root) MakeRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	if r.RevisionClass.(reflect.Type) == reflect.TypeOf(PersistedRevision{}) {
		return NewPersistedRevision(branch, data, children)
	}

	return NewNonPersistedRevision(r, branch, data, children)
}

func (r *root) makeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	r.node.makeLatest(branch, revision, changeAnnouncement)

	if r.KvStore != nil && branch.Txid == "" {
		tags := make(map[string]string)
		for k, v := range r.node.Tags {
			tags[k] = v.GetHash()
		}
		data := &rootData{
			Latest: branch.GetLatest().GetHash(),
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
	Latest string            `json:"latest"`
	Tags   map[string]string `json:"tags"`
}
