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

// TODO: proper error handling
// TODO: proper logging

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"reflect"
	"strings"
)

const (
	NONE string = "none"
)

type Node interface {
	MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple)

	// CRUD functions
	Add(path string, data interface{}, txid string, makeBranch MakeBranchFunction) Revision
	Get(path string, hash string, depth int, deep bool, txid string) interface{}
	Update(path string, data interface{}, strict bool, txid string, makeBranch MakeBranchFunction) Revision
	Remove(path string, txid string, makeBranch MakeBranchFunction) Revision

	MakeBranch(txid string) *Branch
	DeleteBranch(txid string)
	MergeBranch(txid string, dryRun bool) (Revision, error)

	MakeTxBranch() string
	DeleteTxBranch(txid string)
	FoldTxBranch(txid string)

	GetProxy(path string, exclusive bool) *Proxy

	ExecuteCallbacks()
	AddCallback(callback CallbackFunction, args ...interface{})
	AddNotificationCallback(callback CallbackFunction, args ...interface{})
}

type node struct {
	Root      *root
	Type      interface{}
	Branches  map[string]*Branch
	Tags      map[string]Revision
	Proxy     *Proxy
	EventBus  *EventBus
	AutoPrune bool
}

type ChangeTuple struct {
	Type         CallbackType
	PreviousData interface{}
	LatestData   interface{}
}

func NewNode(root *root, initialData interface{}, autoPrune bool, txid string) *node {
	n := &node{}

	n.Root = root
	n.Branches = make(map[string]*Branch)
	n.Tags = make(map[string]Revision)
	n.Proxy = nil
	n.EventBus = nil
	n.AutoPrune = autoPrune

	if IsProtoMessage(initialData) {
		n.Type = reflect.ValueOf(initialData).Interface()
		dataCopy := proto.Clone(initialData.(proto.Message))
		n.initialize(dataCopy, txid)
	} else if reflect.ValueOf(initialData).IsValid() {
		n.Type = reflect.ValueOf(initialData).Interface()
	} else {
		// not implemented error
		log.Errorf("cannot process initial data - %+v", initialData)
	}

	return n
}

func (n *node) MakeNode(data interface{}, txid string) *node {
	return NewNode(n.Root, data, true, txid)
}

func (n *node) MakeRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	if n.Root.RevisionClass.(reflect.Type) == reflect.TypeOf(PersistedRevision{}) {
		return NewPersistedRevision(branch, data, children)
	}

	return NewNonPersistedRevision(branch, data, children)
}

func (n *node) MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	n.makeLatest(branch, revision, changeAnnouncement)
}
func (n *node) makeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	if _, ok := branch.Revisions[revision.GetHash()]; !ok {
		branch.Revisions[revision.GetHash()] = revision
	}

	if branch.Latest == nil || revision.GetHash() != branch.Latest.GetHash() {
		branch.Latest = revision
	}

	if changeAnnouncement != nil && branch.Txid == "" {
		if n.Proxy != nil {
			for _, change := range changeAnnouncement {
				log.Debugf("invoking callback - changeType: %+v, previous:%+v, latest: %+v",
					change.Type,
					change.PreviousData,
					change.LatestData)
				n.Root.AddCallback(
					n.Proxy.InvokeCallbacks,
					change.Type,
					true,
					change.PreviousData,
					change.LatestData)
			}
		}

		for _, change := range changeAnnouncement {
			log.Debugf("sending notification - changeType: %+v, previous:%+v, latest: %+v",
				change.Type,
				change.PreviousData,
				change.LatestData)
			n.Root.AddNotificationCallback(
				n.makeEventBus().Advertise,
				change.Type,
				revision.GetHash(),
				change.PreviousData,
				change.LatestData)
		}
	}
}

func (n *node) Latest(txid ...string) Revision {
	var branch *Branch
	var exists bool

	if len(txid) > 0 && txid[0] != "" {
		if branch, exists = n.Branches[txid[0]]; exists {
			return branch.Latest
		}
	} else if branch, exists = n.Branches[NONE]; exists {
		return branch.Latest
	}
	return nil
}

func (n *node) GetHash(hash string) Revision {
	return n.Branches[NONE].Revisions[hash]
}

func (n *node) initialize(data interface{}, txid string) {
	var children map[string][]Revision
	children = make(map[string][]Revision)
	for fieldName, field := range ChildrenFields(n.Type) {
		_, fieldValue := GetAttributeValue(data, fieldName, 0)

		if fieldValue.IsValid() {
			if field.IsContainer {
				if field.Key != "" {
					var keysSeen []string

					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)

						rev := n.MakeNode(v.Interface(), txid).Latest(txid)

						_, key := GetAttributeValue(v.Interface(), field.Key, 0)
						for _, k := range keysSeen {
							if k == key.String() {
								log.Errorf("duplicate key - %s", k)
							}
						}
						children[fieldName] = append(children[fieldName], rev)
						keysSeen = append(keysSeen, key.String())
					}

				} else {
					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)
						children[fieldName] = append(children[fieldName], n.MakeNode(v.Interface(), txid).Latest())
					}
				}
			} else {
				children[fieldName] = append(children[fieldName], n.MakeNode(fieldValue.Interface(), txid).Latest())
			}
		} else {
			log.Errorf("field is invalid - %+v", fieldValue)
		}
	}
	// FIXME: ClearField???  No such method in go protos.  Reset?
	//data.ClearField(field_name)
	branch := NewBranch(n, "", nil, n.AutoPrune)
	rev := n.MakeRevision(branch, data, children)
	n.makeLatest(branch, rev, nil)

	if txid == "" {
		n.Branches[NONE] = branch
	} else {
		n.Branches[txid] = branch
	}
}

func (n *node) findRevByKey(revs []Revision, keyName string, value interface{}) (int, Revision) {
	for i, rev := range revs {
		dataValue := reflect.ValueOf(rev.GetData())
		dataStruct := GetAttributeStructure(rev.GetData(), keyName, 0)

		fieldValue := dataValue.Elem().FieldByName(dataStruct.Name)

		//log.Debugf("fieldValue: %+v, type: %+v, value: %+v", fieldValue.Interface(), fieldValue.Type(), value)
		a := fmt.Sprintf("%s", fieldValue.Interface())
		b := fmt.Sprintf("%s", value)
		if a == b {
			return i, rev
		}
	}

	log.Errorf("key %s=%s not found", keyName, value)

	return -1, nil
}

//
// Get operation
//
func (n *node) Get(path string, hash string, depth int, deep bool, txid string) interface{} {
	if deep {
		depth = -1
	}

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var rev Revision

	// FIXME: should empty txid be cleaned up?
	if branch = n.Branches[txid]; txid == "" || branch == nil {
		branch = n.Branches[NONE]
	}

	if hash != "" {
		rev = branch.Revisions[hash]
	} else {
		rev = branch.Latest
	}

	return n.getPath(rev, path, depth)
}

func (n *node) getPath(rev Revision, path string, depth int) interface{} {
	if path == "" {
		return n.getData(rev, depth)
	}

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	names := ChildrenFields(n.Type)
	field := names[name]

	if field.IsContainer {
		if field.Key != "" {
			children := rev.GetChildren()[name]
			if path != "" {
				partition = strings.SplitN(path, "/", 2)
				key := partition[0]
				path = ""
				keyValue := field.KeyFromStr(key)
				if _, childRev := n.findRevByKey(children, field.Key, keyValue); childRev == nil {
					return nil
				} else {
					childNode := childRev.GetNode()
					return childNode.getPath(childRev, path, depth)
				}
			} else {
				var response []interface{}
				for _, childRev := range children {
					childNode := childRev.GetNode()
					value := childNode.getData(childRev, depth)
					response = append(response, value)
				}
				return response
			}
		} else {
			var response []interface{}
			if path != "" {
				// TODO: raise error
				return response
			}
			for _, childRev := range rev.GetChildren()[name] {
				childNode := childRev.GetNode()
				value := childNode.getData(childRev, depth)
				response = append(response, value)
			}
			return response
		}
	} else {
		childRev := rev.GetChildren()[name][0]
		childNode := childRev.GetNode()
		return childNode.getPath(childRev, path, depth)
	}
	return nil
}

func (n *node) getData(rev Revision, depth int) interface{} {
	msg := rev.Get(depth)
	var modifiedMsg interface{}

	if n.Proxy != nil {
		log.Debug("invoking proxy GET Callbacks")
		if modifiedMsg = n.Proxy.InvokeCallbacks(GET, false, msg); modifiedMsg != nil {
			msg = modifiedMsg
		}

	}
	return msg
}

//
// Update operation
//
func (n *node) Update(path string, data interface{}, strict bool, txid string, makeBranch MakeBranchFunction) Revision {
	// FIXME: is this required ... a bit overkill to take out a "/"
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var ok bool
	if txid == "" {
		branch = n.Branches[NONE]
	} else if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	log.Debugf("Branch data : %+v, Passed data: %+v", branch.Latest.GetData(), data)

	if path == "" {
		return n.doUpdate(branch, data, strict)
	}

	// TODO missing some code here...
	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision

	if field.IsContainer {
		if path == "" {
			log.Errorf("cannot update a list")
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			keyValue := field.KeyFromStr(key)
			// TODO. Est-ce que le copy ne fonctionne pas? dois-je plutôt faire un clone de chaque item?
			for _, v := range rev.GetChildren()[name] {
				revCopy := reflect.ValueOf(v).Interface().(Revision)
				children = append(children, revCopy)
			}
			idx, childRev := n.findRevByKey(children, field.Key, keyValue)
			childNode := childRev.GetNode()
			childNode.Proxy = n.Proxy

			newChildRev := childNode.Update(path, data, strict, txid, makeBranch)

			if newChildRev.GetHash() == childRev.GetHash() {
				if newChildRev != childRev {
					log.Debug("clear-hash - %s %+v", newChildRev.GetHash(), newChildRev)
					newChildRev.ClearHash()
				}
				return branch.Latest
			}

			_, newKey := GetAttributeValue(newChildRev.GetData(), field.Key, 0)
			log.Debugf("newKey is %s", newKey.Interface())
			_newKeyType := fmt.Sprintf("%s", newKey)
			_keyValueType := fmt.Sprintf("%s", keyValue)
			if _newKeyType != _keyValueType {
				log.Errorf("cannot change key field")
			}
			children[idx] = newChildRev
			rev = rev.UpdateChildren(name, children, branch)
			branch.Latest.Drop(txid, false)
			n.Root.MakeLatest(branch, rev, nil)
			return newChildRev
		} else {
			log.Errorf("cannot index into container with no keys")
		}
	} else {
		childRev := rev.GetChildren()[name][0]
		childNode := childRev.GetNode()
		childNode.Proxy = n.Proxy
		newChildRev := childNode.Update(path, data, strict, txid, makeBranch)
		rev = rev.UpdateChildren(name, []Revision{newChildRev}, branch)
		branch.Latest.Drop(txid, false)
		n.Root.MakeLatest(branch, rev, nil)
		return newChildRev
	}
	return nil
}

func (n *node) doUpdate(branch *Branch, data interface{}, strict bool) Revision {
	log.Debugf("Comparing types - expected: %+v, actual: %+v", reflect.ValueOf(n.Type).Type(), reflect.TypeOf(data))

	if reflect.TypeOf(data) != reflect.ValueOf(n.Type).Type() {
		// TODO raise error
		log.Errorf("data does not match type: %+v", n.Type)
		return nil
	}

	// TODO: validate that this actually works
	//if n.hasChildren(data) {
	//	return nil
	//}

	if n.Proxy != nil {
		log.Debug("invoking proxy PRE_UPDATE Callbacks")
		n.Proxy.InvokeCallbacks(PRE_UPDATE, false, branch.Latest.GetData(), data)
	}
	if !reflect.DeepEqual(branch.Latest.GetData(), data) {
		if strict {
			// TODO: checkAccessViolations(data, Branch.GetLatest.data)
			log.Debugf("checking access violations")
		}
		rev := branch.Latest.UpdateData(data, branch)
		changes := []ChangeTuple{{POST_UPDATE, branch.Latest.GetData(), rev.GetData()}}

		// FIXME VOL-1293: the following statement corrupts the kv when using a subproxy (commenting for now)
		// FIXME VOL-1293 cont'd: need to figure out the required conditions otherwise we are not cleaning up entries
		//branch.Latest.Drop(branch.Txid, true)

		n.Root.Proxy = n.Proxy
		n.Root.MakeLatest(branch, rev, changes)
		return rev
	} else {
		return branch.Latest
	}
}

//
// Add operation
//
func (n *node) Add(path string, data interface{}, txid string, makeBranch MakeBranchFunction) Revision {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		log.Errorf("cannot add for non-container mode")
	}

	var branch *Branch
	var ok bool
	if txid == "" {
		branch = n.Branches[NONE]
	} else if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision

	if field.IsContainer {
		if path == "" {
			if field.Key != "" {
				if n.Proxy != nil {
					log.Debug("invoking proxy PRE_ADD Callbacks")
					n.Proxy.InvokeCallbacks(PRE_ADD, false, data)
				}

				for _, v := range rev.GetChildren()[name] {
					revCopy := reflect.ValueOf(v).Interface().(Revision)
					children = append(children, revCopy)
				}
				_, key := GetAttributeValue(data, field.Key, 0)
				if _, exists := n.findRevByKey(children, field.Key, key.String()); exists != nil {
					// TODO raise error
					log.Errorf("duplicate key found: %s", key.String())
				} else {
					childRev := n.MakeNode(data, txid).Latest(txid)
					children = append(children, childRev)
					rev := rev.UpdateChildren(name, children, branch)
					changes := []ChangeTuple{{POST_ADD, nil, rev.GetData()}}
					branch.Latest.Drop(txid, false)
					n.Root.MakeLatest(branch, rev, changes)
					return rev
				}

			} else {
				log.Errorf("cannot add to non-keyed container")
			}
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			keyValue := field.KeyFromStr(key)
			copy(children, rev.GetChildren()[name])
			idx, childRev := n.findRevByKey(children, field.Key, keyValue)
			childNode := childRev.GetNode()
			newChildRev := childNode.Add(path, data, txid, makeBranch)
			children[idx] = newChildRev
			rev := rev.UpdateChildren(name, children, branch)
			branch.Latest.Drop(txid, false)
			n.Root.MakeLatest(branch, rev, nil)
			return rev
		} else {
			log.Errorf("cannot add to non-keyed container")
		}
	} else {
		log.Errorf("cannot add to non-container field")
	}
	return nil
}

//
// Remove operation
//
func (n *node) Remove(path string, txid string, makeBranch MakeBranchFunction) Revision {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		log.Errorf("cannot remove for non-container mode")
	}
	var branch *Branch
	var ok bool
	if txid == "" {
		branch = n.Branches[NONE]
	} else if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision
	postAnnouncement := []ChangeTuple{}

	if field.IsContainer {
		if path == "" {
			log.Errorf("cannot remove without a key")
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			keyValue := field.KeyFromStr(key)
			if path != "" {
				for _, v := range rev.GetChildren()[name] {
					newV := reflect.ValueOf(v).Interface().(Revision)
					children = append(children, newV)
				}
				idx, childRev := n.findRevByKey(children, field.Key, keyValue)
				childNode := childRev.GetNode()
				newChildRev := childNode.Remove(path, txid, makeBranch)
				children[idx] = newChildRev
				rev := rev.UpdateChildren(name, children, branch)
				branch.Latest.Drop(txid, false)
				n.Root.MakeLatest(branch, rev, nil)
				return rev
			} else {
				for _, v := range rev.GetChildren()[name] {
					newV := reflect.ValueOf(v).Interface().(Revision)
					children = append(children, newV)
				}
				idx, childRev := n.findRevByKey(children, field.Key, keyValue)
				if n.Proxy != nil {
					data := childRev.GetData()
					n.Proxy.InvokeCallbacks(PRE_REMOVE, false, data)
					postAnnouncement = append(postAnnouncement, ChangeTuple{POST_REMOVE, data, nil})
				} else {
					postAnnouncement = append(postAnnouncement, ChangeTuple{POST_REMOVE, childRev.GetData(), nil})
				}
				childRev.Drop(txid, true)
				children = append(children[:idx], children[idx+1:]...)
				rev := rev.UpdateChildren(name, children, branch)
				branch.Latest.Drop(txid, false)
				n.Root.MakeLatest(branch, rev, postAnnouncement)
				return rev
			}
		} else {
			log.Errorf("cannot add to non-keyed container")
		}
	} else {
		log.Errorf("cannot add to non-container field")
	}

	return nil
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Branching ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

type MakeBranchFunction func(*node) *Branch

func (n *node) MakeBranch(txid string) *Branch {
	branchPoint := n.Branches[NONE].Latest
	branch := NewBranch(n, txid, branchPoint, true)
	n.Branches[txid] = branch
	return branch
}

func (n *node) DeleteBranch(txid string) {
	delete(n.Branches, txid)
}

func (n *node) mergeChild(txid string, dryRun bool) func(Revision) Revision {
	f := func(rev Revision) Revision {
		childBranch := rev.GetBranch()

		if childBranch.Txid == txid {
			rev, _ = childBranch.Node.MergeBranch(txid, dryRun)
		}

		return rev
	}
	return f
}

func (n *node) MergeBranch(txid string, dryRun bool) (Revision, error) {
	srcBranch := n.Branches[txid]
	dstBranch := n.Branches[NONE]

	forkRev := srcBranch.Origin
	srcRev := srcBranch.Latest
	dstRev := dstBranch.Latest

	rev, changes := Merge3Way(forkRev, srcRev, dstRev, n.mergeChild(txid, dryRun), dryRun)

	if !dryRun {
		n.Root.MakeLatest(dstBranch, rev, changes)
		delete(n.Branches, txid)
	}

	// TODO: return proper error when one occurs
	return rev, nil
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Diff utility ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

//func (n *node) diff(hash1, hash2, txid string) {
//	branch := n.Branches[txid]
//	rev1 := branch.get(hash1)
//	rev2 := branch.get(hash2)
//
//	if rev1.GetHash() == rev2.GetHash() {
//		// empty patch
//	} else {
//		// translate data to json and generate patch
//		patch, err := jsonpatch.MakePatch(rev1.GetData(), rev2.GetData())
//		patch.
//	}
//}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Tag utility ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

// TODO: is tag mgmt used in the python implementation? Need to validate

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Internals ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

func (n *node) hasChildren(data interface{}) bool {
	for fieldName, field := range ChildrenFields(n.Type) {
		_, fieldValue := GetAttributeValue(data, fieldName, 0)

		if (field.IsContainer && fieldValue.Len() > 0) || !fieldValue.IsNil() {
			log.Error("cannot update external children")
			return true
		}
	}

	return false
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ node Proxy ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

func (n *node) GetProxy(path string, exclusive bool) *Proxy {
	//r := NewRoot(n.Type, n.KvStore)
	//r.node = n
	//r.KvStore = n.KvStore

	return n.getProxy(path, path, exclusive)
}
func (n *node) getProxy(path string, fullPath string, exclusive bool) *Proxy {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		return n.makeProxy(path, fullPath, exclusive)
	}

	rev := n.Branches[NONE].Latest
	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	if field != nil && field.IsContainer {
		if path == "" {
			log.Error("cannot proxy a container field")
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			keyValue := field.KeyFromStr(key)
			var children []Revision
			for _, v := range rev.GetChildren()[name] {
				newV := reflect.ValueOf(v).Interface().(Revision)
				children = append(children, newV)
			}
			_, childRev := n.findRevByKey(children, field.Key, keyValue)
			childNode := childRev.GetNode()

			return childNode.getProxy(path, fullPath, exclusive)
		} else {
			log.Error("cannot index into container with no keys")
		}
	} else {
		childRev := rev.GetChildren()[name][0]
		childNode := childRev.GetNode()
		return childNode.getProxy(path, fullPath, exclusive)
	}

	return nil
}

func (n *node) makeProxy(path string, fullPath string, exclusive bool) *Proxy {
	r := &root{
		node:                  n,
		Callbacks:             n.Root.Callbacks,
		NotificationCallbacks: n.Root.NotificationCallbacks,
		DirtyNodes:            n.Root.DirtyNodes,
		KvStore:               n.Root.KvStore,
		Loading:               n.Root.Loading,
		RevisionClass:         n.Root.RevisionClass,
	}

	if n.Proxy == nil {
		n.Proxy = NewProxy(r, path, fullPath, exclusive)
	} else {
		if n.Proxy.Exclusive {
			log.Error("node is already owned exclusively")
		}
	}
	return n.Proxy
}

func (n *node) makeEventBus() *EventBus {
	if n.EventBus == nil {
		n.EventBus = NewEventBus()
	}
	return n.EventBus
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Persistence Loading ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

func (n *node) LoadLatest(hash string) {
	branch := NewBranch(n, "", nil, n.AutoPrune)
	pr := &PersistedRevision{}
	rev := pr.Load(branch, n.Root.KvStore, n.Type, hash)
	n.makeLatest(branch, rev, nil)
	n.Branches[NONE] = branch
}

func (n *node) ExecuteCallbacks() {
	n.Root.ExecuteCallbacks()
}
