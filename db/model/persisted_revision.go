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
	"compress/gzip"
	"encoding/json"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"io/ioutil"
	"reflect"
	"runtime/debug"
	"sync"
	"time"
)

// PersistedRevision holds information of revision meant to be saved in a persistent storage
type PersistedRevision struct {
	mutex sync.RWMutex
	Revision
	Compress bool
	kvStore  *Backend
}

// NewPersistedRevision creates a new instance of a PersistentRevision structure
func NewPersistedRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	pr := &PersistedRevision{}
	pr.kvStore = branch.Node.GetRoot().KvStore
	pr.Revision = NewNonPersistedRevision(nil, branch, data, children)
	pr.Finalize()
	return pr
}

// Finalize is responsible of saving the revision in the persistent storage
func (pr *PersistedRevision) Finalize() {
	pr.store()
}

type revData struct {
	Children map[string][]string
	Config   string
}

func (pr *PersistedRevision) store() {
	if pr.GetBranch().Txid != "" {
		return
	}
	if ok, _ := pr.kvStore.Get(pr.Revision.GetHash()); ok != nil {
		log.Debugf("Entry already exists - hash:%s, stack: %s", pr.Revision.GetHash(), string(debug.Stack()))
		return
	}

	pr.storeConfig()

	childrenHashes := make(map[string][]string)
	for fieldName, children := range pr.GetChildren() {
		hashes := []string{}
		for _, rev := range children {
			if rev != nil {
				hashes = append(hashes, rev.GetHash())
			}
		}
		childrenHashes[fieldName] = hashes
	}
	data := &revData{
		Children: childrenHashes,
		Config:   pr.GetConfig().Hash,
	}
	if blob, err := json.Marshal(data); err != nil {
		// TODO report error
	} else {
		if pr.Compress {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(blob)
			w.Close()
			blob = b.Bytes()
		}
		if err := pr.kvStore.Put(pr.GetHash(), blob); err != nil {
			log.Warnf("Problem storing revision - error: %s, hash: %s, data: %s", err.Error(), pr.GetHash(),
				string(blob))
		} else {
			log.Debugf("Stored entry - hash:%s, blob: %s, stack: %s", pr.Revision.GetHash(), string(blob),
				string(debug.Stack()))
		}
	}
}

// Load retrieves a revision from th persistent storage
func (pr *PersistedRevision) Load(branch *Branch, kvStore *Backend, msgClass interface{}, hash string) Revision {
	blob, _ := kvStore.Get(hash)

	start := time.Now()
	output := blob.Value.([]byte)
	var data revData
	if pr.Compress {
		b := bytes.NewBuffer(blob.Value.([]byte))
		if r, err := gzip.NewReader(b); err != nil {
			// TODO : report error
		} else {
			if output, err = ioutil.ReadAll(r); err != nil {
				// TODO report error
			}
		}
	}
	if err := json.Unmarshal(output, &data); err != nil {
		log.Errorf("problem to unmarshal data - %s", err.Error())
	}

	stop := time.Now()
	GetProfiling().AddToInMemoryModelTime(stop.Sub(start).Seconds())
	configHash := data.Config
	configData := pr.loadConfig(kvStore, msgClass, configHash)

	assembledChildren := make(map[string][]Revision)

	childrenHashes := data.Children
	node := branch.Node
	for fieldName, child := range ChildrenFields(msgClass) {
		var children []Revision
		for _, childHash := range childrenHashes[fieldName] {
			childNode := node.MakeNode(reflect.New(child.ClassType).Elem().Interface(), "")
			childNode.LoadLatest(childHash)
			childRev := childNode.Latest()
			children = append(children, childRev)
		}
		assembledChildren[fieldName] = children
	}

	rev := NewPersistedRevision(branch, configData, assembledChildren)
	return rev
}

// storeConfig saves the data associated to a revision in the persistent storage
func (pr *PersistedRevision) storeConfig() {
	if ok, _ := pr.kvStore.Get(pr.GetConfig().Hash); ok != nil {
		log.Debugf("Config already exists - hash:%s, stack: %s", pr.GetConfig().Hash, string(debug.Stack()))
		return
	}
	if blob, err := proto.Marshal(pr.GetConfig().Data.(proto.Message)); err != nil {
		// TODO report error
	} else {
		if pr.Compress {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(blob)
			w.Close()
			blob = b.Bytes()
		}

		if err := pr.kvStore.Put(pr.GetConfig().Hash, blob); err != nil {
			log.Warnf("Problem storing revision config - error: %s, hash: %s, data: %+v", err.Error(),
				pr.GetConfig().Hash,
				pr.GetConfig().Data)
		} else {
			log.Debugf("Stored config - hash:%s, blob: %+v, stack: %s", pr.GetConfig().Hash, pr.GetConfig().Data,
				string(debug.Stack()))
		}
	}
}

// loadConfig restores the data associated to a revision from the persistent storage
func (pr *PersistedRevision) loadConfig(kvStore *Backend, msgClass interface{}, hash string) interface{} {
	blob, _ := kvStore.Get(hash)
	start := time.Now()
	output := blob.Value.([]byte)

	if pr.Compress {
		b := bytes.NewBuffer(blob.Value.([]byte))
		if r, err := gzip.NewReader(b); err != nil {
			// TODO : report error
		} else {
			if output, err = ioutil.ReadAll(r); err != nil {
				// TODO report error
			}
		}
	}

	var data reflect.Value
	if msgClass != nil {
		data = reflect.New(reflect.TypeOf(msgClass).Elem())
		if err := proto.Unmarshal(output, data.Interface().(proto.Message)); err != nil {
			// TODO report error
		}
	}

	stop := time.Now()

	GetProfiling().AddToInMemoryModelTime(stop.Sub(start).Seconds())
	return data.Interface()
}

// UpdateData modifies the information in the data model and saves it in the persistent storage
func (pr *PersistedRevision) UpdateData(data interface{}, branch *Branch) Revision {
	newNPR := pr.Revision.UpdateData(data, branch)

	log.Debugf("Updating data %+v", data)
	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
	}

	newPR.Finalize()

	return newPR
}

// UpdateChildren modifies the children of a revision and of a specific component and saves it in the persistent storage
func (pr *PersistedRevision) UpdateChildren(name string, children []Revision, branch *Branch) Revision {
	newNPR := pr.Revision.UpdateChildren(name, children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
	}

	newPR.Finalize()

	return newPR
}

// UpdateAllChildren modifies the children for all components of a revision and saves it in the peristent storage
func (pr *PersistedRevision) UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision {
	newNPR := pr.Revision.UpdateAllChildren(children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
	}

	newPR.Finalize()

	return newPR
}

// Drop takes care of eliminating a revision hash that is no longer needed
// and its associated config when required
func (pr *PersistedRevision) Drop(txid string, includeConfig bool) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	if pr.kvStore != nil && txid == "" {
		if includeConfig {
			log.Debugf("removing rev config - hash: %s", pr.GetConfig().Hash)
			if err := pr.kvStore.Delete(pr.GetConfig().Hash); err != nil {
				log.Errorf(
					"failed to remove rev config - hash: %s, err: %s",
					pr.GetConfig().Hash,
					err.Error(),
				)
			}
		}

		log.Debugf("removing rev - hash: %s", pr.GetHash())
		if err := pr.kvStore.Delete(pr.GetHash()); err != nil {
			log.Errorf("failed to remove rev - hash: %s, err: %s", pr.GetHash(), err.Error())
		}
	} else {
		if includeConfig {
			log.Debugf("Attempted to remove revision config:%s linked to transaction:%s", pr.GetConfig().Hash, txid)
		}
		log.Debugf("Attempted to remove revision:%s linked to transaction:%s", pr.GetHash(), txid)
	}

	pr.Revision.Drop(txid, includeConfig)
}
