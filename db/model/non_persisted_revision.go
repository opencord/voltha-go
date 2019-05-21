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
	"bytes"
	"crypto/md5"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"reflect"
	"sort"
	"sync"
	"time"
)

// TODO: Cache logic will have to be revisited to cleanup unused entries in memory (disabled for now)
//
type revCacheSingleton struct {
	sync.RWMutex
	Cache sync.Map
}

var revCacheInstance *revCacheSingleton
var revCacheOnce sync.Once

func GetRevCache() *revCacheSingleton {
	revCacheOnce.Do(func() {
		revCacheInstance = &revCacheSingleton{Cache: sync.Map{}}
	})
	return revCacheInstance
}

type NonPersistedRevision struct {
	mutex        sync.RWMutex
	Root         *root
	Config       *DataRevision
	childrenLock sync.RWMutex
	Children     map[string][]Revision
	Hash         string
	Branch       *Branch
	WeakRef      string
	Name         string
	discarded    bool
	lastUpdate   time.Time
}

func NewNonPersistedRevision(root *root, branch *Branch, data interface{}, children map[string][]Revision) Revision {
	r := &NonPersistedRevision{}
	r.Root = root
	r.Branch = branch
	r.Config = NewDataRevision(root, data)
	r.Children = children
	r.Hash = r.hashContent()
	r.discarded = false
	return r
}

func (npr *NonPersistedRevision) IsDiscarded() bool {
	return npr.discarded
}

func (npr *NonPersistedRevision) SetConfig(config *DataRevision) {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	npr.Config = config
}

func (npr *NonPersistedRevision) GetConfig() *DataRevision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	return npr.Config
}

func (npr *NonPersistedRevision) SetAllChildren(children map[string][]Revision) {
	npr.childrenLock.Lock()
	defer npr.childrenLock.Unlock()
	npr.Children = make(map[string][]Revision)

	for key, value := range children {
		npr.Children[key] = make([]Revision, len(value))
		copy(npr.Children[key], value)
	}
}

func (npr *NonPersistedRevision) SetChildren(name string, children []Revision) {
	npr.childrenLock.Lock()
	defer npr.childrenLock.Unlock()

	npr.Children[name] = make([]Revision, len(children))
	copy(npr.Children[name], children)
}

func (npr *NonPersistedRevision) GetAllChildren() map[string][]Revision {
	npr.childrenLock.Lock()
	defer npr.childrenLock.Unlock()

	return npr.Children
}

func (npr *NonPersistedRevision) GetChildren(name string) []Revision {
	npr.childrenLock.Lock()
	defer npr.childrenLock.Unlock()

	if _, exists := npr.Children[name]; exists {
		return npr.Children[name]
	}
	return nil
}

func (npr *NonPersistedRevision) SetHash(hash string) {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	npr.Hash = hash
}

func (npr *NonPersistedRevision) GetHash() string {
	//npr.mutex.Lock()
	//defer npr.mutex.Unlock()
	return npr.Hash
}

func (npr *NonPersistedRevision) ClearHash() {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	npr.Hash = ""
}

func (npr *NonPersistedRevision) GetName() string {
	//npr.mutex.Lock()
	//defer npr.mutex.Unlock()
	return npr.Name
}

func (npr *NonPersistedRevision) SetName(name string) {
	//npr.mutex.Lock()
	//defer npr.mutex.Unlock()
	npr.Name = name
}
func (npr *NonPersistedRevision) SetBranch(branch *Branch) {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	npr.Branch = branch
}

func (npr *NonPersistedRevision) GetBranch() *Branch {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	return npr.Branch
}

func (npr *NonPersistedRevision) GetData() interface{} {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	if npr.Config == nil {
		return nil
	}
	return npr.Config.Data
}

func (npr *NonPersistedRevision) GetNode() *node {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	return npr.Branch.Node
}

func (npr *NonPersistedRevision) Finalize(skipOnExist bool) {
	npr.Hash = npr.hashContent()
}

// hashContent generates a hash string based on the contents of the revision.
// The string should be unique to avoid conflicts with other revisions
func (npr *NonPersistedRevision) hashContent() string {
	var buffer bytes.Buffer
	var childrenKeys []string

	if npr.Config != nil {
		buffer.WriteString(npr.Config.Hash)
	}

	if npr.Name != "" {
		buffer.WriteString(npr.Name)
	}

	for key := range npr.Children {
		childrenKeys = append(childrenKeys, key)
	}

	sort.Strings(childrenKeys)

	if len(npr.Children) > 0 {
		// Loop through sorted Children keys
		for _, key := range childrenKeys {
			for _, child := range npr.Children[key] {
				if child != nil && child.GetHash() != "" {
					buffer.WriteString(child.GetHash())
				}
			}
		}
	}

	return fmt.Sprintf("%x", md5.Sum(buffer.Bytes()))[:12]
}

// Get will retrieve the data for the current revision
func (npr *NonPersistedRevision) Get(depth int) interface{} {
	// 1. Clone the data to avoid any concurrent access issues
	// 2. The current rev might still be pointing to an old config
	//    thus, force the revision to get its latest value
	latestRev := npr.GetBranch().GetLatest()
	originalData := proto.Clone(latestRev.GetData().(proto.Message))
	data := originalData

	if depth != 0 {
		// FIXME: Traversing the struct through reflection sometimes corrupts the data.
		// Unlike the original python implementation, golang structs are not lazy loaded.
		// Keeping this non-critical logic for now, but Get operations should be forced to
		// depth=0 to avoid going through the following loop.
		for fieldName, field := range ChildrenFields(latestRev.GetData()) {
			childDataName, childDataHolder := GetAttributeValue(data, fieldName, 0)
			if field.IsContainer {
				for _, rev := range latestRev.GetChildren(fieldName) {
					childData := rev.Get(depth - 1)
					foundEntry := false
					for i := 0; i < childDataHolder.Len(); i++ {
						cdh_if := childDataHolder.Index(i).Interface()
						if cdh_if.(proto.Message).String() == childData.(proto.Message).String() {
							foundEntry = true
							break
						}
					}
					if !foundEntry {
						// avoid duplicates by adding it only if the child was not found in the holder
						childDataHolder = reflect.Append(childDataHolder, reflect.ValueOf(childData))
					}
				}
			} else {
				if revs := npr.GetBranch().GetLatest().GetChildren(fieldName); revs != nil && len(revs) > 0 {
					rev := revs[0]
					if rev != nil {
						childData := rev.Get(depth - 1)
						if reflect.TypeOf(childData) == reflect.TypeOf(childDataHolder.Interface()) {
							childDataHolder = reflect.ValueOf(childData)
						}
					}
				}
			}
			// Merge child data with cloned object
			reflect.ValueOf(data).Elem().FieldByName(childDataName).Set(childDataHolder)
		}
	}

	result := data

	if result != nil {
		// We need to send back a copy of the retrieved object
		result = proto.Clone(data.(proto.Message))
	}

	return result
}

// UpdateData will refresh the data content of the revision
func (npr *NonPersistedRevision) UpdateData(data interface{}, branch *Branch) Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	// Do not update the revision if data is the same
	if npr.Config.Data != nil && npr.Config.hashData(npr.Root, data) == npr.Config.Hash {
		log.Debugw("stored-data-matches-latest", log.Fields{"stored": npr.Config.Data, "provided": data})
		return npr
	}

	// Construct a new revision based on the current one
	newRev := NonPersistedRevision{}
	newRev.Config = NewDataRevision(npr.Root, data)
	newRev.Hash = npr.Hash
	newRev.Root = npr.Root
	newRev.Name = npr.Name
	newRev.Branch = branch
	newRev.lastUpdate = npr.lastUpdate

	newRev.Children = make(map[string][]Revision)
	for entryName, childrenEntry := range branch.GetLatest().GetAllChildren() {
		newRev.Children[entryName] = append(newRev.Children[entryName], childrenEntry...)
	}

	newRev.Finalize(false)

	return &newRev
}

// UpdateChildren will refresh the list of children with the provided ones
// It will carefully go through the list and ensure that no child is lost
func (npr *NonPersistedRevision) UpdateChildren(name string, children []Revision, branch *Branch) Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	// Construct a new revision based on the current one
	updatedRev := &NonPersistedRevision{}
	updatedRev.Config = NewDataRevision(npr.Root, npr.Config.Data)
	updatedRev.Hash = npr.Hash
	updatedRev.Branch = branch
	updatedRev.Name = npr.Name
	updatedRev.lastUpdate = npr.lastUpdate

	updatedRev.Children = make(map[string][]Revision)
	for entryName, childrenEntry := range branch.GetLatest().GetAllChildren() {
		updatedRev.Children[entryName] = append(updatedRev.Children[entryName], childrenEntry...)
	}

	var updatedChildren []Revision

	// Verify if the map contains already contains an entry matching the name value
	// If so, we need to retain the contents of that entry and merge them with the provided children revision list
	if existingChildren := branch.GetLatest().GetChildren(name); existingChildren != nil {
		// Construct a map of unique child names with the respective index value
		// for the children in the existing revision as well as the new ones
		existingNames := make(map[string]int)
		newNames := make(map[string]int)

		for i, newChild := range children {
			newNames[newChild.GetName()] = i
		}

		for i, existingChild := range existingChildren {
			existingNames[existingChild.GetName()] = i

			// If an existing entry is not in the new list, add it to the updated list, so it is not forgotten
			if _, exists := newNames[existingChild.GetName()]; !exists {
				updatedChildren = append(updatedChildren, existingChild)
			}
		}

		log.Debugw("existing-children-names", log.Fields{"hash": npr.GetHash(), "names": existingNames})

		// Merge existing and new children
		for _, newChild := range children {
			nameIndex, nameExists := existingNames[newChild.GetName()]

			// Does the existing list contain a child with that name?
			if nameExists {
				// Check if the data has changed or not
				if existingChildren[nameIndex].GetData().(proto.Message).String() != newChild.GetData().(proto.Message).String() {
					// replace entry
					newChild.GetNode().Root = existingChildren[nameIndex].GetNode().Root
					updatedChildren = append(updatedChildren, newChild)
				} else {
					// keep existing entry
					updatedChildren = append(updatedChildren, existingChildren[nameIndex])
				}
			} else {
				// new entry ... just add it
				updatedChildren = append(updatedChildren, newChild)
			}
		}

		// Save children in new revision
		updatedRev.SetChildren(name, updatedChildren)

		updatedNames := make(map[string]int)
		for i, updatedChild := range updatedChildren {
			updatedNames[updatedChild.GetName()] = i
		}

		log.Debugw("updated-children-names", log.Fields{"hash": npr.GetHash(), "names": updatedNames})

	} else {
		// There are no children available, just save the provided ones
		updatedRev.SetChildren(name, children)
	}

	updatedRev.Finalize(false)

	return updatedRev
}

// UpdateAllChildren will replace the current list of children with the provided ones
func (npr *NonPersistedRevision) UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	newRev := npr
	newRev.Config = npr.Config
	newRev.Hash = npr.Hash
	newRev.Branch = branch
	newRev.Name = npr.Name
	newRev.lastUpdate = npr.lastUpdate

	newRev.Children = make(map[string][]Revision)
	for entryName, childrenEntry := range children {
		newRev.Children[entryName] = append(newRev.Children[entryName], childrenEntry...)
	}
	newRev.Finalize(false)

	return newRev
}

// Drop is used to indicate when a revision is no longer required
func (npr *NonPersistedRevision) Drop(txid string, includeConfig bool) {
	log.Debugw("dropping-revision", log.Fields{"hash": npr.GetHash(), "name": npr.GetName()})
	npr.discarded = true
}

// ChildDrop will remove a child entry matching the provided parameters from the current revision
func (npr *NonPersistedRevision) ChildDrop(childType string, childHash string) {
	if childType != "" {
		children := make([]Revision, len(npr.GetChildren(childType)))
		copy(children, npr.GetChildren(childType))
		for i, child := range children {
			if child.GetHash() == childHash {
				children = append(children[:i], children[i+1:]...)
				npr.SetChildren(childType, children)
				break
			}
		}
	}
}

func (npr *NonPersistedRevision) SetLastUpdate(ts ...time.Time) {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	if ts != nil && len(ts) > 0 {
		npr.lastUpdate = ts[0]
	} else {
		npr.lastUpdate = time.Now()
	}
}

func (npr *NonPersistedRevision) GetLastUpdate() time.Time {
	npr.mutex.RLock()
	defer npr.mutex.RUnlock()

	return npr.lastUpdate
}

func (npr *NonPersistedRevision) LoadFromPersistence(path string, txid string, blobs map[string]*kvstore.KVPair) []Revision {
	// stub... required by interface
	return nil
}

func (npr *NonPersistedRevision) SetupWatch(key string) {
	// stub ... required by interface
}

func (pr *NonPersistedRevision) StorageDrop(txid string, includeConfig bool) {
	// stub ... required by interface
}
