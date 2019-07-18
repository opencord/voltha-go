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
	"context"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"reflect"
	"strings"
	"sync"
	"time"
)

// When a branch has no transaction id, everything gets stored in NONE
const (
	NONE string = "none"
)

// Node interface is an abstraction of the node data structure
type Node interface {
	MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple)

	// CRUD functions
	Add(ctx context.Context, path string, data interface{}, txid string, makeBranch MakeBranchFunction) Revision
	Get(ctx context.Context, path string, hash string, depth int, deep bool, txid string) interface{}
	List(ctx context.Context, path string, hash string, depth int, deep bool, txid string) interface{}
	Update(ctx context.Context, path string, data interface{}, strict bool, txid string, makeBranch MakeBranchFunction) Revision
	Remove(ctx context.Context, path string, txid string, makeBranch MakeBranchFunction) Revision
	CreateProxy(ctx context.Context, path string, exclusive bool) *Proxy

	GetProxy() *Proxy

	MakeBranch(txid string) *Branch
	DeleteBranch(txid string)
	MergeBranch(txid string, dryRun bool) (Revision, error)

	MakeTxBranch() string
	DeleteTxBranch(txid string)
	FoldTxBranch(txid string)
}

type node struct {
	mutex     sync.RWMutex
	Root      *root
	Type      interface{}
	Branches  map[string]*Branch
	Tags      map[string]Revision
	Proxy     *Proxy
	EventBus  *EventBus
	AutoPrune bool
}

// ChangeTuple holds details of modifications made to a revision
type ChangeTuple struct {
	Type         CallbackType
	PreviousData interface{}
	LatestData   interface{}
}

// NewNode creates a new instance of the node data structure
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
		// FIXME: this block does not reflect the original implementation
		// it should be checking if the provided initial_data is already a type!??!
		// it should be checked before IsProtoMessage
		n.Type = reflect.ValueOf(initialData).Interface()
	} else {
		// not implemented error
		log.Errorf("cannot process initial data - %+v", initialData)
	}

	return n
}

// MakeNode creates a new node in the tree
func (n *node) MakeNode(data interface{}, txid string) *node {
	return NewNode(n.Root, data, true, txid)
}

// MakeRevision create a new revision of the node in the tree
func (n *node) MakeRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	return n.GetRoot().MakeRevision(branch, data, children)
}

// makeLatest will mark the revision of a node as being the latest
func (n *node) makeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	// Keep a reference to the current revision
	var previous string
	if branch.GetLatest() != nil {
		previous = branch.GetLatest().GetHash()
	}

	branch.AddRevision(revision)

	// If anything is new, then set the revision as the latest
	if branch.GetLatest() == nil || revision.GetHash() != branch.GetLatest().GetHash() {
		if revision.GetName() != "" {
			log.Debugw("saving-latest-data", log.Fields{"hash": revision.GetHash(), "data": revision.GetData()})
			// Tag a timestamp to that revision
			revision.SetLastUpdate()
			GetRevCache().Set(revision.GetName(), revision)
		}
		branch.SetLatest(revision)
	}

	// Delete the previous revision if anything has changed
	if previous != "" && previous != branch.GetLatest().GetHash() {
		branch.DeleteRevision(previous)
	}

	if changeAnnouncement != nil && branch.Txid == "" {
		if n.Proxy != nil {
			for _, change := range changeAnnouncement {
				log.Debugw("adding-callback",
					log.Fields{
						"callbacks":    n.GetProxy().getCallbacks(change.Type),
						"type":         change.Type,
						"previousData": change.PreviousData,
						"latestData":   change.LatestData,
					})
				n.Root.AddCallback(
					n.GetProxy().InvokeCallbacks,
					change.Type,
					true,
					change.PreviousData,
					change.LatestData)
			}
		}
	}
}

// Latest returns the latest revision of node with or without the transaction id
func (n *node) Latest(txid ...string) Revision {
	var branch *Branch

	if len(txid) > 0 && txid[0] != "" {
		if branch = n.GetBranch(txid[0]); branch != nil {
			return branch.GetLatest()
		}
	} else if branch = n.GetBranch(NONE); branch != nil {
		return branch.GetLatest()
	}
	return nil
}

// initialize prepares the content of a node along with its possible ramifications
func (n *node) initialize(data interface{}, txid string) {
	children := make(map[string][]Revision)
	for fieldName, field := range ChildrenFields(n.Type) {
		_, fieldValue := GetAttributeValue(data, fieldName, 0)

		if fieldValue.IsValid() {
			if field.IsContainer {
				if field.Key != "" {
					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)

						if rev := n.MakeNode(v.Interface(), txid).Latest(txid); rev != nil {
							children[fieldName] = append(children[fieldName], rev)
						}

						// TODO: The following logic was ported from v1.0.  Need to verify if it is required
						//var keysSeen []string
						//_, key := GetAttributeValue(v.Interface(), field.Key, 0)
						//for _, k := range keysSeen {
						//	if k == key.String() {
						//		//log.Errorf("duplicate key - %s", k)
						//	}
						//}
						//keysSeen = append(keysSeen, key.String())
					}

				} else {
					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)
						if newNodeRev := n.MakeNode(v.Interface(), txid).Latest(); newNodeRev != nil {
							children[fieldName] = append(children[fieldName], newNodeRev)
						}
					}
				}
			} else {
				if newNodeRev := n.MakeNode(fieldValue.Interface(), txid).Latest(); newNodeRev != nil {
					children[fieldName] = append(children[fieldName], newNodeRev)
				}
			}
		} else {
			log.Errorf("field is invalid - %+v", fieldValue)
		}
	}

	branch := NewBranch(n, "", nil, n.AutoPrune)
	rev := n.MakeRevision(branch, data, children)
	n.makeLatest(branch, rev, nil)

	if txid == "" {
		n.SetBranch(NONE, branch)
	} else {
		n.SetBranch(txid, branch)
	}
}

// findRevByKey retrieves a specific revision from a node tree
func (n *node) findRevByKey(revs []Revision, keyName string, value interface{}) (int, Revision) {
	for i, rev := range revs {
		dataValue := reflect.ValueOf(rev.GetData())
		dataStruct := GetAttributeStructure(rev.GetData(), keyName, 0)

		fieldValue := dataValue.Elem().FieldByName(dataStruct.Name)

		a := fmt.Sprintf("%s", fieldValue.Interface())
		b := fmt.Sprintf("%s", value)
		if a == b {
			return i, revs[i]
		}
	}

	return -1, nil
}

// Get retrieves the data from a node tree that resides at the specified path
func (n *node) List(ctx context.Context, path string, hash string, depth int, deep bool, txid string) interface{} {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	log.Debugw("node-list-request", log.Fields{"path": path, "hash": hash, "depth": depth, "deep": deep, "txid": txid})
	if deep {
		depth = -1
	}

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var rev Revision

	if branch = n.GetBranch(txid); txid == "" || branch == nil {
		branch = n.GetBranch(NONE)
	}

	if hash != "" {
		rev = branch.GetRevision(hash)
	} else {
		rev = branch.GetLatest()
	}

	var result interface{}
	var prList []interface{}
	if pr := rev.LoadFromPersistence(ctx, path, txid, nil); pr != nil {
		for _, revEntry := range pr {
			prList = append(prList, revEntry.GetData())
		}
		result = prList
	}

	return result
}

// Get retrieves the data from a node tree that resides at the specified path
func (n *node) Get(ctx context.Context, path string, hash string, depth int, reconcile bool, txid string) interface{} {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	log.Debugw("node-get-request", log.Fields{"path": path, "hash": hash, "depth": depth, "reconcile": reconcile, "txid": txid})

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var rev Revision

	if branch = n.GetBranch(txid); txid == "" || branch == nil {
		branch = n.GetBranch(NONE)
	}

	if hash != "" {
		rev = branch.GetRevision(hash)
	} else {
		rev = branch.GetLatest()
	}

	var result interface{}

	// If there is no request to reconcile, try to get it from memory
	if !reconcile {
		// Try to find an entry matching the path value from one of these sources
		// 1.  Start with the cache which stores revisions by watch names
		// 2.  Then look in the revision tree, especially if it's a sub-path such as /devices/1234/flows
		// 3.  Move on to the KV store if that path cannot be found or if the entry has expired
		if entry, exists := GetRevCache().Get(path); exists && entry.(Revision) != nil {
			entryAge := time.Now().Sub(entry.(Revision).GetLastUpdate()).Nanoseconds() / int64(time.Millisecond)
			if entryAge < DataRefreshPeriod {
				log.Debugw("using-cache-entry", log.Fields{
					"path": path,
					"hash": hash,
					"age":  entryAge,
				})
				return proto.Clone(entry.(Revision).GetData().(proto.Message))
			} else {
				log.Debugw("cache-entry-expired", log.Fields{"path": path, "hash": hash, "age": entryAge})
			}
		} else if result = n.getPath(ctx, rev.GetBranch().GetLatest(), path, depth); result != nil && reflect.ValueOf(result).IsValid() && !reflect.ValueOf(result).IsNil() {
			log.Debugw("using-rev-tree-entry", log.Fields{"path": path, "hash": hash, "depth": depth, "reconcile": reconcile, "txid": txid})
			return result
		} else {
			log.Debugw("not-using-cache-entry", log.Fields{
				"path": path,
				"hash": hash, "depth": depth,
				"reconcile": reconcile,
				"txid":      txid,
			})
		}
	} else {
		log.Debugw("reconcile-requested", log.Fields{
			"path":      path,
			"hash":      hash,
			"reconcile": reconcile,
		})
	}

	// If we got to this point, we are either trying to reconcile with the db
	// or we simply failed at getting information from memory
	if n.Root.KvStore != nil {
		if pr := rev.LoadFromPersistence(ctx, path, txid, nil); pr != nil && len(pr) > 0 {
			// Did we receive a single or multiple revisions?
			if len(pr) > 1 {
				var revs []interface{}
				for _, revEntry := range pr {
					revs = append(revs, revEntry.GetData())
				}
				result = revs
			} else {
				result = pr[0].GetData()
			}
		}
	}

	return result
}

//getPath traverses the specified path and retrieves the data associated to it
func (n *node) getPath(ctx context.Context, rev Revision, path string, depth int) interface{} {
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

	if field != nil && field.IsContainer {
		children := make([]Revision, len(rev.GetChildren(name)))
		copy(children, rev.GetChildren(name))

		if field.Key != "" {
			if path != "" {
				partition = strings.SplitN(path, "/", 2)
				key := partition[0]
				path = ""
				keyValue := field.KeyFromStr(key)
				if _, childRev := n.findRevByKey(children, field.Key, keyValue); childRev == nil {
					return nil
				} else {
					childNode := childRev.GetNode()
					return childNode.getPath(ctx, childRev, path, depth)
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
			for _, childRev := range children {
				childNode := childRev.GetNode()
				value := childNode.getData(childRev, depth)
				response = append(response, value)
			}
			return response
		}
	} else if children := rev.GetChildren(name); children != nil && len(children) > 0 {
		childRev := children[0]
		childNode := childRev.GetNode()
		return childNode.getPath(ctx, childRev, path, depth)
	}

	return nil
}

// getData retrieves the data from a node revision
func (n *node) getData(rev Revision, depth int) interface{} {
	msg := rev.GetBranch().GetLatest().Get(depth)
	var modifiedMsg interface{}

	if n.GetProxy() != nil {
		log.Debugw("invoking-get-callbacks", log.Fields{"data": msg})
		if modifiedMsg = n.GetProxy().InvokeCallbacks(GET, false, msg); modifiedMsg != nil {
			msg = modifiedMsg
		}

	}

	return msg
}

// Update changes the content of a node at the specified path with the provided data
func (n *node) Update(ctx context.Context, path string, data interface{}, strict bool, txid string, makeBranch MakeBranchFunction) Revision {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	log.Debugw("node-update-request", log.Fields{"path": path, "strict": strict, "txid": txid, "makeBranch": makeBranch})

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	if txid == "" {
		branch = n.GetBranch(NONE)
	} else if branch = n.GetBranch(txid); branch == nil {
		branch = makeBranch(n)
	}

	if branch.GetLatest() != nil {
		log.Debugf("Branch data : %+v, Passed data: %+v", branch.GetLatest().GetData(), data)
	}
	if path == "" {
		return n.doUpdate(ctx, branch, data, strict)
	}

	rev := branch.GetLatest()

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision

	if field == nil {
		return n.doUpdate(ctx, branch, data, strict)
	}

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

			children = make([]Revision, len(rev.GetChildren(name)))
			copy(children, rev.GetChildren(name))

			idx, childRev := n.findRevByKey(children, field.Key, keyValue)

			if childRev == nil {
				log.Debugw("child-revision-is-nil", log.Fields{"key": keyValue})
				return branch.GetLatest()
			}

			childNode := childRev.GetNode()

			// Save proxy in child node to ensure callbacks are called later on
			// only assign in cases of non sub-folder proxies, i.e. "/"
			if childNode.Proxy == nil && n.Proxy != nil && n.GetProxy().getFullPath() == "" {
				childNode.Proxy = n.Proxy
			}

			newChildRev := childNode.Update(ctx, path, data, strict, txid, makeBranch)

			if newChildRev.GetHash() == childRev.GetHash() {
				if newChildRev != childRev {
					log.Debug("clear-hash - %s %+v", newChildRev.GetHash(), newChildRev)
					newChildRev.ClearHash()
				}
				log.Debugw("child-revisions-have-matching-hash", log.Fields{"hash": childRev.GetHash(), "key": keyValue})
				return branch.GetLatest()
			}

			_, newKey := GetAttributeValue(newChildRev.GetData(), field.Key, 0)

			_newKeyType := fmt.Sprintf("%s", newKey)
			_keyValueType := fmt.Sprintf("%s", keyValue)

			if _newKeyType != _keyValueType {
				log.Errorf("cannot change key field")
			}

			// Prefix the hash value with the data type (e.g. devices, logical_devices, adapters)
			newChildRev.SetName(name + "/" + _keyValueType)

			branch.LatestLock.Lock()
			defer branch.LatestLock.Unlock()

			if idx >= 0 {
				children[idx] = newChildRev
			} else {
				children = append(children, newChildRev)
			}

			updatedRev := rev.UpdateChildren(ctx, name, children, branch)

			n.makeLatest(branch, updatedRev, nil)
			updatedRev.ChildDrop(name, childRev.GetHash())

			return newChildRev

		} else {
			log.Errorf("cannot index into container with no keys")
		}
	} else {
		childRev := rev.GetChildren(name)[0]
		childNode := childRev.GetNode()
		newChildRev := childNode.Update(ctx, path, data, strict, txid, makeBranch)

		branch.LatestLock.Lock()
		defer branch.LatestLock.Unlock()

		updatedRev := rev.UpdateChildren(ctx, name, []Revision{newChildRev}, branch)
		n.makeLatest(branch, updatedRev, nil)

		updatedRev.ChildDrop(name, childRev.GetHash())

		return newChildRev
	}

	return nil
}

func (n *node) doUpdate(ctx context.Context, branch *Branch, data interface{}, strict bool) Revision {
	log.Debugw("comparing-types", log.Fields{"expected": reflect.ValueOf(n.Type).Type(), "actual": reflect.TypeOf(data)})

	if reflect.TypeOf(data) != reflect.ValueOf(n.Type).Type() {
		// TODO raise error
		log.Errorw("types-do-not-match: %+v", log.Fields{"actual": reflect.TypeOf(data), "expected": n.Type})
		return nil
	}

	// TODO: validate that this actually works
	//if n.hasChildren(data) {
	//	return nil
	//}

	if n.GetProxy() != nil {
		log.Debug("invoking proxy PRE_UPDATE Callbacks")
		n.GetProxy().InvokeCallbacks(PRE_UPDATE, false, branch.GetLatest(), data)
	}

	if branch.GetLatest().GetData().(proto.Message).String() != data.(proto.Message).String() {
		if strict {
			// TODO: checkAccessViolations(data, Branch.GetLatest.data)
			log.Debugf("checking access violations")
		}

		rev := branch.GetLatest().UpdateData(ctx, data, branch)
		changes := []ChangeTuple{{POST_UPDATE, branch.GetLatest().GetData(), rev.GetData()}}
		n.makeLatest(branch, rev, changes)

		return rev
	}
	return branch.GetLatest()
}

// Add inserts a new node at the specified path with the provided data
func (n *node) Add(ctx context.Context, path string, data interface{}, txid string, makeBranch MakeBranchFunction) Revision {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	log.Debugw("node-add-request", log.Fields{"path": path, "txid": txid, "makeBranch": makeBranch})

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		log.Errorf("cannot add for non-container mode")
		return nil
	}

	var branch *Branch
	if txid == "" {
		branch = n.GetBranch(NONE)
	} else if branch = n.GetBranch(txid); branch == nil {
		branch = makeBranch(n)
	}

	rev := branch.GetLatest()

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
				if n.GetProxy() != nil {
					log.Debug("invoking proxy PRE_ADD Callbacks")
					n.GetProxy().InvokeCallbacks(PRE_ADD, false, data)
				}

				children = make([]Revision, len(rev.GetChildren(name)))
				copy(children, rev.GetChildren(name))

				_, key := GetAttributeValue(data, field.Key, 0)

				if _, exists := n.findRevByKey(children, field.Key, key.String()); exists != nil {
					// TODO raise error
					log.Warnw("duplicate-key-found", log.Fields{"key": key.String()})
					return exists
				}
				childRev := n.MakeNode(data, "").Latest()

				// Prefix the hash with the data type (e.g. devices, logical_devices, adapters)
				childRev.SetName(name + "/" + key.String())

				branch.LatestLock.Lock()
				defer branch.LatestLock.Unlock()

				children = append(children, childRev)

				updatedRev := rev.UpdateChildren(ctx, name, children, branch)
				changes := []ChangeTuple{{POST_ADD, nil, childRev.GetData()}}
				childRev.SetupWatch(childRev.GetName())

				n.makeLatest(branch, updatedRev, changes)

				return childRev
			}
			log.Errorf("cannot add to non-keyed container")

		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			keyValue := field.KeyFromStr(key)

			children = make([]Revision, len(rev.GetChildren(name)))
			copy(children, rev.GetChildren(name))

			idx, childRev := n.findRevByKey(children, field.Key, keyValue)

			if childRev == nil {
				return branch.GetLatest()
			}

			childNode := childRev.GetNode()
			newChildRev := childNode.Add(ctx, path, data, txid, makeBranch)

			// Prefix the hash with the data type (e.g. devices, logical_devices, adapters)
			newChildRev.SetName(name + "/" + keyValue.(string))

			branch.LatestLock.Lock()
			defer branch.LatestLock.Unlock()

			if idx >= 0 {
				children[idx] = newChildRev
			} else {
				children = append(children, newChildRev)
			}

			updatedRev := rev.UpdateChildren(ctx, name, children, branch)
			n.makeLatest(branch, updatedRev, nil)

			updatedRev.ChildDrop(name, childRev.GetHash())

			return newChildRev
		} else {
			log.Errorf("cannot add to non-keyed container")
		}
	} else {
		log.Errorf("cannot add to non-container field")
	}

	return nil
}

// Remove eliminates a node at the specified path
func (n *node) Remove(ctx context.Context, path string, txid string, makeBranch MakeBranchFunction) Revision {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	log.Debugw("node-remove-request", log.Fields{"path": path, "txid": txid, "makeBranch": makeBranch})

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		log.Errorf("cannot remove for non-container mode")
	}
	var branch *Branch
	if txid == "" {
		branch = n.GetBranch(NONE)
	} else if branch = n.GetBranch(txid); branch == nil {
		branch = makeBranch(n)
	}

	rev := branch.GetLatest()

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
			log.Errorw("cannot-remove-without-key", log.Fields{"name": name, "key": path})
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}

			keyValue := field.KeyFromStr(key)
			children = make([]Revision, len(rev.GetChildren(name)))
			copy(children, rev.GetChildren(name))

			if path != "" {
				if idx, childRev := n.findRevByKey(children, field.Key, keyValue); childRev != nil {
					childNode := childRev.GetNode()
					if childNode.Proxy == nil {
						childNode.Proxy = n.Proxy
					}
					newChildRev := childNode.Remove(ctx, path, txid, makeBranch)

					branch.LatestLock.Lock()
					defer branch.LatestLock.Unlock()

					if idx >= 0 {
						children[idx] = newChildRev
					} else {
						children = append(children, newChildRev)
					}

					rev.SetChildren(name, children)
					branch.GetLatest().Drop(txid, false)
					n.makeLatest(branch, rev, nil)
				}
				return branch.GetLatest()
			}

			if idx, childRev := n.findRevByKey(children, field.Key, keyValue); childRev != nil && idx >= 0 {
				if n.GetProxy() != nil {
					data := childRev.GetData()
					n.GetProxy().InvokeCallbacks(PRE_REMOVE, false, data)
					postAnnouncement = append(postAnnouncement, ChangeTuple{POST_REMOVE, data, nil})
				} else {
					postAnnouncement = append(postAnnouncement, ChangeTuple{POST_REMOVE, childRev.GetData(), nil})
				}

				childRev.StorageDrop(txid, true)
				GetRevCache().Delete(childRev.GetName())

				branch.LatestLock.Lock()
				defer branch.LatestLock.Unlock()

				children = append(children[:idx], children[idx+1:]...)
				rev.SetChildren(name, children)

				branch.GetLatest().Drop(txid, false)
				n.makeLatest(branch, rev, postAnnouncement)

				return rev
			} else {
				log.Errorw("failed-to-find-revision", log.Fields{"name": name, "key": keyValue.(string)})
			}
		}
		log.Errorw("cannot-add-to-non-keyed-container", log.Fields{"name": name, "path": path, "fieldKey": field.Key})

	} else {
		log.Errorw("cannot-add-to-non-container-field", log.Fields{"name": name, "path": path})
	}

	return nil
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Branching ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

// MakeBranchFunction is a type for function references intented to create a branch
type MakeBranchFunction func(*node) *Branch

// MakeBranch creates a new branch for the provided transaction id
func (n *node) MakeBranch(txid string) *Branch {
	branchPoint := n.GetBranch(NONE).GetLatest()
	branch := NewBranch(n, txid, branchPoint, true)
	n.SetBranch(txid, branch)
	return branch
}

// DeleteBranch removes a branch with the specified id
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

// MergeBranch will integrate the contents of a transaction branch within the latest branch of a given node
func (n *node) MergeBranch(txid string, dryRun bool) (Revision, error) {
	srcBranch := n.GetBranch(txid)
	dstBranch := n.GetBranch(NONE)

	forkRev := srcBranch.Origin
	srcRev := srcBranch.GetLatest()
	dstRev := dstBranch.GetLatest()

	rev, changes := Merge3Way(forkRev, srcRev, dstRev, n.mergeChild(txid, dryRun), dryRun)

	if !dryRun {
		if rev != nil {
			rev.SetName(dstRev.GetName())
			n.makeLatest(dstBranch, rev, changes)
		}
		n.DeleteBranch(txid)
	}

	// TODO: return proper error when one occurs
	return rev, nil
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Diff utility ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

//func (n *node) diff(hash1, hash2, txid string) {
//	branch := n.Branches[txid]
//	rev1 := branch.GetHash(hash1)
//	rev2 := branch.GetHash(hash2)
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

// CreateProxy returns a reference to a sub-tree of the data model
func (n *node) CreateProxy(ctx context.Context, path string, exclusive bool) *Proxy {
	return n.createProxy(ctx, path, path, n, exclusive)
}

func (n *node) createProxy(ctx context.Context, path string, fullPath string, parentNode *node, exclusive bool) *Proxy {
	log.Debugw("node-create-proxy", log.Fields{
		"node-type":        reflect.ValueOf(n.Type).Type(),
		"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
		"path":             path,
		"fullPath":         fullPath,
	})

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		return n.makeProxy(path, fullPath, parentNode, exclusive)
	}

	rev := n.GetBranch(NONE).GetLatest()
	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	var nodeType interface{}
	if len(partition) < 2 {
		path = ""
		nodeType = n.Type
	} else {
		path = partition[1]
		nodeType = parentNode.Type
	}

	field := ChildrenFields(nodeType)[name]

	if field != nil {
		if field.IsContainer {
			log.Debugw("container-field", log.Fields{
				"node-type":        reflect.ValueOf(n.Type).Type(),
				"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
				"path":             path,
				"name":             name,
			})
			if path == "" {
				log.Debugw("folder-proxy", log.Fields{
					"node-type":        reflect.ValueOf(n.Type).Type(),
					"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
					"fullPath":         fullPath,
					"name":             name,
				})
				newNode := n.MakeNode(reflect.New(field.ClassType.Elem()).Interface(), "")
				return newNode.makeProxy(path, fullPath, parentNode, exclusive)
			} else if field.Key != "" {
				log.Debugw("key-proxy", log.Fields{
					"node-type":        reflect.ValueOf(n.Type).Type(),
					"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
					"fullPath":         fullPath,
					"name":             name,
				})
				partition := strings.SplitN(path, "/", 2)
				key := partition[0]
				if len(partition) < 2 {
					path = ""
				} else {
					path = partition[1]
				}
				keyValue := field.KeyFromStr(key)
				var children []Revision
				children = make([]Revision, len(rev.GetChildren(name)))
				copy(children, rev.GetChildren(name))

				var childRev Revision
				if _, childRev = n.findRevByKey(children, field.Key, keyValue); childRev != nil {
					log.Debugw("found-revision-matching-key-in-memory", log.Fields{
						"node-type":        reflect.ValueOf(n.Type).Type(),
						"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
						"fullPath":         fullPath,
						"name":             name,
					})
				} else if revs := n.GetBranch(NONE).GetLatest().LoadFromPersistence(ctx, fullPath, "", nil); revs != nil && len(revs) > 0 {
					log.Debugw("found-revision-matching-key-in-db", log.Fields{
						"node-type":        reflect.ValueOf(n.Type).Type(),
						"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
						"fullPath":         fullPath,
						"name":             name,
					})
					childRev = revs[0]
				} else {
					log.Debugw("no-revision-matching-key", log.Fields{
						"node-type":        reflect.ValueOf(n.Type).Type(),
						"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
						"fullPath":         fullPath,
						"name":             name,
					})
				}
				if childRev != nil {
					childNode := childRev.GetNode()
					return childNode.createProxy(ctx, path, fullPath, n, exclusive)
				}
			} else {
				log.Errorw("cannot-access-index-of-empty-container", log.Fields{
					"node-type":        reflect.ValueOf(n.Type).Type(),
					"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
					"path":             path,
					"name":             name,
				})
			}
		} else {
			log.Debugw("non-container-field", log.Fields{
				"node-type":        reflect.ValueOf(n.Type).Type(),
				"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
				"path":             path,
				"name":             name,
			})
			childRev := rev.GetChildren(name)[0]
			childNode := childRev.GetNode()
			return childNode.createProxy(ctx, path, fullPath, n, exclusive)
		}
	} else {
		log.Debugw("field-object-is-nil", log.Fields{
			"node-type":        reflect.ValueOf(n.Type).Type(),
			"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
			"fullPath":         fullPath,
			"name":             name,
		})
	}

	log.Warnw("cannot-create-proxy", log.Fields{
		"node-type":        reflect.ValueOf(n.Type).Type(),
		"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
		"path":             path,
		"fullPath":         fullPath,
		"latest-rev":       rev.GetHash(),
	})
	return nil
}

func (n *node) makeProxy(path string, fullPath string, parentNode *node, exclusive bool) *Proxy {
	log.Debugw("node-make-proxy", log.Fields{
		"node-type":        reflect.ValueOf(n.Type).Type(),
		"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
		"path":             path,
		"fullPath":         fullPath,
	})

	r := &root{
		node:                  n,
		Callbacks:             n.Root.GetCallbacks(),
		NotificationCallbacks: n.Root.GetNotificationCallbacks(),
		DirtyNodes:            n.Root.DirtyNodes,
		KvStore:               n.Root.KvStore,
		Loading:               n.Root.Loading,
		RevisionClass:         n.Root.RevisionClass,
	}

	if n.Proxy == nil {
		log.Debugw("constructing-new-proxy", log.Fields{
			"node-type":        reflect.ValueOf(n.Type).Type(),
			"parent-node-type": reflect.ValueOf(parentNode.Type).Type(),
			"path":             path,
			"fullPath":         fullPath,
		})
		n.Proxy = NewProxy(r, n, parentNode, path, fullPath, exclusive)
	} else {
		log.Debugw("node-has-existing-proxy", log.Fields{
			"node-type":        reflect.ValueOf(n.GetProxy().Node.Type).Type(),
			"parent-node-type": reflect.ValueOf(n.GetProxy().ParentNode.Type).Type(),
			"path":             n.GetProxy().Path,
			"fullPath":         n.GetProxy().FullPath,
		})
		if n.GetProxy().Exclusive {
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

func (n *node) SetProxy(proxy *Proxy) {
	n.Proxy = proxy
}

func (n *node) GetProxy() *Proxy {
	return n.Proxy
}

func (n *node) GetBranch(key string) *Branch {
	if n.Branches != nil {
		if branch, exists := n.Branches[key]; exists {
			return branch
		}
	}
	return nil
}

func (n *node) SetBranch(key string, branch *Branch) {
	n.Branches[key] = branch
}

func (n *node) GetRoot() *root {
	return n.Root
}
func (n *node) SetRoot(root *root) {
	n.Root = root
}
