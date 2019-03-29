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
	"reflect"
	"sort"
	"sync"
)

type revCacheSingleton struct {
	sync.RWMutex
	Cache map[string]interface{}
}

var revCacheInstance *revCacheSingleton
var revCacheOnce sync.Once

func GetRevCache() *revCacheSingleton {
	revCacheOnce.Do(func() {
		revCacheInstance = &revCacheSingleton{Cache: make(map[string]interface{})}
	})
	return revCacheInstance
}

type NonPersistedRevision struct {
	mutex    sync.RWMutex
	Root     *root
	Config   *DataRevision
	Children map[string][]Revision
	Hash     string
	Branch   *Branch
	WeakRef  string
	Name     string
}

func NewNonPersistedRevision(root *root, branch *Branch, data interface{}, children map[string][]Revision) Revision {
	r := &NonPersistedRevision{}
	r.Root = root
	r.Branch = branch
	r.Config = NewDataRevision(root, data)
	r.Children = children
	r.Hash = r.hashContent()
	return r
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
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	npr.Children = children
}

func (npr *NonPersistedRevision) SetChildren(name string, children []Revision) {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	if _, exists := npr.Children[name]; exists {
		npr.Children[name] = children
	}
}

func (npr *NonPersistedRevision) GetAllChildren() map[string][]Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
	return npr.Children
}

func (npr *NonPersistedRevision) GetChildren(name string) []Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()
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
	GetRevCache().Lock()
	defer GetRevCache().Unlock()

	if !skipOnExist {
		npr.Hash = npr.hashContent()
	}
	if _, exists := GetRevCache().Cache[npr.Hash]; !exists {
		GetRevCache().Cache[npr.Hash] = npr
	}
	if _, exists := GetRevCache().Cache[npr.Config.Hash]; !exists {
		GetRevCache().Cache[npr.Config.Hash] = npr.Config
	} else {
		npr.Config = GetRevCache().Cache[npr.Config.Hash].(*DataRevision)
	}
}

func (npr *NonPersistedRevision) hashContent() string {
	var buffer bytes.Buffer
	var childrenKeys []string

	if npr.Config != nil {
		buffer.WriteString(npr.Config.Hash)
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

func (npr *NonPersistedRevision) Get(depth int) interface{} {
	// 1. Clone the data to avoid any concurrent access issues
	// 2. The current rev might still be pointing to an old config
	//    thus, force the revision to get its latest value
	latestRev := npr.GetBranch().GetLatest()
	originalData := proto.Clone(latestRev.GetData().(proto.Message))

	data := originalData
	// Get back to the interface type
	//data := reflect.ValueOf(originalData).Interface()

	if depth != 0 {
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
				if revs := latestRev.GetChildren(fieldName); revs != nil && len(revs) > 0 {
					rev := latestRev.GetChildren(fieldName)[0]
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

func (npr *NonPersistedRevision) UpdateData(data interface{}, branch *Branch) Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	if npr.Config.Data != nil && npr.Config.hashData(npr.Root, data) == npr.Config.Hash {
		log.Debugw("stored-data-matches-latest", log.Fields{"stored": npr.Config.Data, "provided": data})
		return npr
	}

	newRev := NonPersistedRevision{}
	newRev.Config = NewDataRevision(npr.Root, data)
	newRev.Hash = npr.Hash
	newRev.Branch = branch

	newRev.Children = make(map[string][]Revision)
	for entryName, childrenEntry := range npr.Children {
		newRev.Children[entryName] = append(newRev.Children[entryName], childrenEntry...)
	}

	newRev.Finalize(false)

	return &newRev
}

func (npr *NonPersistedRevision) UpdateChildren(name string, children []Revision, branch *Branch) Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	updatedRev := npr

	// Verify if the map contains already contains an entry matching the name value
	// If so, we need to retain the contents of that entry and merge them with the provided children revision list
	if _, exists := updatedRev.Children[name]; exists {
		// Go through all child hashes and save their index within the map
		existChildMap := make(map[string]int)
		for i, child := range updatedRev.Children[name] {
			existChildMap[child.GetHash()] = i
		}

		for _, newChild := range children {
			if _, childExists := existChildMap[newChild.GetHash()]; !childExists {
				// revision is not present in the existing list... add it
				updatedRev.Children[name] = append(updatedRev.Children[name], newChild)
			} else {
				// replace
				updatedRev.Children[name][existChildMap[newChild.GetHash()]] = newChild
			}
		}
	} else {
		// Map entry does not exist, thus just create a new entry and assign the provided revisions
		updatedRev.Children[name] = make([]Revision, len(children))
		copy(updatedRev.Children[name], children)
	}

	updatedRev.Config = NewDataRevision(npr.Root, npr.Config.Data)
	updatedRev.Hash = npr.Hash
	updatedRev.Branch = branch
	updatedRev.Finalize(false)

	return updatedRev
}

func (npr *NonPersistedRevision) UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision {
	npr.mutex.Lock()
	defer npr.mutex.Unlock()

	newRev := npr
	newRev.Config = npr.Config
	newRev.Hash = npr.Hash
	newRev.Branch = branch
	newRev.Children = make(map[string][]Revision)
	for entryName, childrenEntry := range children {
		newRev.Children[entryName] = append(newRev.Children[entryName], childrenEntry...)
	}
	newRev.Finalize(false)

	return newRev
}

func (npr *NonPersistedRevision) Drop(txid string, includeConfig bool) {
	GetRevCache().Lock()
	defer GetRevCache().Unlock()

	if includeConfig {
		delete(GetRevCache().Cache, npr.Config.Hash)
	}
	delete(GetRevCache().Cache, npr.Hash)
}

func (npr *NonPersistedRevision) LoadFromPersistence(path string, txid string) []Revision {
	// stub... required by interface
	return nil
}

func (npr *NonPersistedRevision) SetupWatch(key string) {
	// stub ... required by interface
}

func (pr *NonPersistedRevision) StorageDrop(txid string, includeConfig bool) {
	// stub ... required by interface
}
