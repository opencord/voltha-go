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
	"github.com/opencord/voltha-go/common/log"
	"reflect"
	"sort"
)

var (
	RevisionCache = make(map[string]interface{})
)

type NonPersistedRevision struct {
	Config   *DataRevision
	Children map[string][]Revision
	Hash     string
	Branch   *Branch
	WeakRef  string
}

func NewNonPersistedRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	cr := &NonPersistedRevision{}
	cr.Branch = branch
	cr.Config = NewDataRevision(data)
	cr.Children = children
	cr.Finalize()

	return cr
}

func (npr *NonPersistedRevision) SetConfig(config *DataRevision) {
	npr.Config = config
}

func (npr *NonPersistedRevision) GetConfig() *DataRevision {
	return npr.Config
}

func (npr *NonPersistedRevision) SetChildren(children map[string][]Revision) {
	npr.Children = children
}

func (npr *NonPersistedRevision) GetChildren() map[string][]Revision {
	return npr.Children
}

func (npr *NonPersistedRevision) SetHash(hash string) {
	npr.Hash = hash
}

func (npr *NonPersistedRevision) GetHash() string {
	return npr.Hash
}

func (npr *NonPersistedRevision) ClearHash() {
	npr.Hash = ""
}

func (npr *NonPersistedRevision) SetBranch(branch *Branch) {
	npr.Branch = branch
}

func (npr *NonPersistedRevision) GetBranch() *Branch {
	return npr.Branch
}

func (npr *NonPersistedRevision) GetData() interface{} {
	if npr.Config == nil {
		return nil
	}
	return npr.Config.Data
}

func (npr *NonPersistedRevision) GetNode() *Node {
	return npr.Branch.Node
}

func (npr *NonPersistedRevision) Finalize() {
	npr.SetHash(npr.hashContent())

	if _, exists := RevisionCache[npr.Hash]; !exists {
		RevisionCache[npr.Hash] = npr
	}
	if _, exists := RevisionCache[npr.Config.Hash]; !exists {
		RevisionCache[npr.Config.Hash] = npr.Config
	} else {
		npr.Config = RevisionCache[npr.Config.Hash].(*DataRevision)
	}
}

func (npr *NonPersistedRevision) hashContent() string {
	var buffer bytes.Buffer
	var childrenKeys []string

	if npr.Config != nil {
		buffer.WriteString(npr.Config.Hash)
	}

	for key, _ := range npr.Children {
		childrenKeys = append(childrenKeys, key)
	}
	sort.Strings(childrenKeys)

	if npr.Children != nil && len(npr.Children) > 0 {
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
	originalData := npr.GetData()
	data := reflect.ValueOf(originalData).Interface()

	if depth != 0 {
		for fieldName, field := range ChildrenFields(npr.GetData()) {
			childDataName, childDataHolder := GetAttributeValue(data, fieldName, 0)
			if field.IsContainer {
				for _, rev := range npr.Children[fieldName] {
					childData := rev.Get(depth - 1)
					foundEntry := false
					for i := 0; i < childDataHolder.Len(); i++ {
						if reflect.DeepEqual(childDataHolder.Index(i).Interface(), childData) {
							foundEntry = true
							break
						}
					}
					if !foundEntry {
						// avoid duplicates by adding if the child was not found in the holder
						childDataHolder = reflect.Append(childDataHolder, reflect.ValueOf(childData))
					}
				}
			} else {
				rev := npr.Children[fieldName][0]
				childData := rev.Get(depth - 1)
				foundEntry := false
				for i := 0; i < childDataHolder.Len(); i++ {
					if reflect.DeepEqual(childDataHolder.Index(i).Interface(), childData) {
						foundEntry = true
						break
					}
				}
				if !foundEntry {
					// avoid duplicates by adding if the child was not found in the holder
					childDataHolder = reflect.Append(childDataHolder, reflect.ValueOf(childData))
				}
			}
			// Merge child data with cloned object
			reflect.ValueOf(data).Elem().FieldByName(childDataName).Set(childDataHolder)
		}
	}
	return data
}

func (npr *NonPersistedRevision) UpdateData(data interface{}, branch *Branch) Revision {
	// TODO: Need to keep the hash for the old revision.
	// TODO: This will allow us to get rid of the unnecessary data

	newRev := reflect.ValueOf(npr).Elem().Interface().(NonPersistedRevision)
	newRev.SetBranch(branch)
	log.Debugf("newRev config : %+v, npr: %+v", newRev.GetConfig(), npr)
	newRev.SetConfig(NewDataRevision(data))
	newRev.Finalize()

	return &newRev
}

func (npr *NonPersistedRevision) UpdateChildren(name string, children []Revision, branch *Branch) Revision {
	newChildren := make(map[string][]Revision)
	for entryName, childrenEntry := range npr.Children {
		for _, revisionEntry := range childrenEntry {
			newEntry := reflect.ValueOf(revisionEntry).Interface().(Revision)
			newChildren[entryName] = append(newChildren[entryName], newEntry)
		}
	}
	newChildren[name] = children

	newRev := reflect.ValueOf(npr).Elem().Interface().(NonPersistedRevision)
	newRev.SetBranch(branch)
	newRev.SetChildren(newChildren)
	newRev.Finalize()

	return &newRev
}

func (npr *NonPersistedRevision) UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision {
	newRev := reflect.ValueOf(npr).Elem().Interface().(NonPersistedRevision)
	newRev.SetBranch(branch)
	newRev.SetChildren(children)
	newRev.Finalize()

	return &newRev
}

func (npr *NonPersistedRevision) Drop(txid string, includeConfig bool) {
	//npr.SetConfig(nil)
}
