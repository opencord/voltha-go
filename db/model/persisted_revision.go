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
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

// PersistedRevision holds information of revision meant to be saved in a persistent storage
type PersistedRevision struct {
	Revision
	Compress bool

	events       chan *kvstore.Event
	kvStore      *db.Backend
	mutex        sync.RWMutex
	versionMutex sync.RWMutex
	Version      int64
	isStored     bool
}

// NewPersistedRevision creates a new instance of a PersistentRevision structure
func NewPersistedRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	pr := &PersistedRevision{}
	pr.kvStore = branch.Node.GetRoot().KvStore
	pr.Version = 1
	pr.Revision = NewNonPersistedRevision(nil, branch, data, children)
	return pr
}

func (pr *PersistedRevision) getVersion() int64 {
	pr.versionMutex.RLock()
	defer pr.versionMutex.RUnlock()
	return pr.Version
}

func (pr *PersistedRevision) setVersion(version int64) {
	pr.versionMutex.Lock()
	defer pr.versionMutex.Unlock()
	pr.Version = version
}

// Finalize is responsible of saving the revision in the persistent storage
func (pr *PersistedRevision) Finalize(ctx context.Context, skipOnExist bool) {
	pr.store(ctx, skipOnExist)
}

func (pr *PersistedRevision) store(ctx context.Context, skipOnExist bool) {
	if pr.GetBranch().Txid != "" {
		return
	}

	log.Debugw("ready-to-store-revision", log.Fields{"hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetData()})

	// clone the revision data to avoid any race conditions with processes
	// accessing the same data
	cloned := proto.Clone(pr.GetConfig().Data.(proto.Message))

	if blob, err := proto.Marshal(cloned); err != nil {
		log.Errorw("problem-to-marshal", log.Fields{"error": err, "hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetData()})
	} else {
		if pr.Compress {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			if _, err := w.Write(blob); err != nil {
				log.Errorw("Unable to write a compressed form of p to the underlying io.Writer.", log.Fields{"error": err})
			}
			w.Close()
			blob = b.Bytes()
		}

		getRevCache().Set(pr.GetName(), pr)
		if err := pr.kvStore.Put(ctx, pr.GetName(), blob); err != nil {
			log.Warnw("problem-storing-revision", log.Fields{"error": err, "hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data})
		} else {
			log.Debugw("storing-revision", log.Fields{"hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data, "version": pr.getVersion()})
			pr.isStored = true
		}
	}
}

// UpdateData modifies the information in the data model and saves it in the persistent storage
func (pr *PersistedRevision) UpdateData(ctx context.Context, data interface{}, branch *Branch) Revision {
	log.Debugw("updating-persisted-data", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateData(ctx, data, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
		events:   pr.events,
		Version:  pr.getVersion(),
	}

	if newPR.GetHash() != pr.GetHash() {
		newPR.isStored = false
		pr.Drop(branch.Txid, false)
		pr.Drop(branch.Txid, false)
	} else {
		newPR.isStored = true
	}

	return newPR
}

// UpdateChildren modifies the children of a revision and of a specific component and saves it in the persistent storage
func (pr *PersistedRevision) UpdateChildren(ctx context.Context, name string, children []Revision, branch *Branch) Revision {
	log.Debugw("updating-persisted-children", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateChildren(ctx, name, children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
		events:   pr.events,
		Version:  pr.getVersion(),
	}

	if newPR.GetHash() != pr.GetHash() {
		newPR.isStored = false
		pr.Drop(branch.Txid, false)
	} else {
		newPR.isStored = true
	}

	return newPR
}

// UpdateAllChildren modifies the children for all components of a revision and saves it in the peristent storage
func (pr *PersistedRevision) UpdateAllChildren(ctx context.Context, children map[string][]Revision, branch *Branch) Revision {
	log.Debugw("updating-all-persisted-children", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateAllChildren(ctx, children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
		events:   pr.events,
		Version:  pr.getVersion(),
	}

	if newPR.GetHash() != pr.GetHash() {
		newPR.isStored = false
		pr.Drop(branch.Txid, false)
	} else {
		newPR.isStored = true
	}

	return newPR
}

// Drop takes care of eliminating a revision hash that is no longer needed
// and its associated config when required
func (pr *PersistedRevision) Drop(txid string, includeConfig bool) {
	pr.Revision.Drop(txid, includeConfig)
}

// StorageDrop takes care of eliminating a revision hash that is no longer needed
// and its associated config when required
func (pr *PersistedRevision) StorageDrop(ctx context.Context, txid string, includeConfig bool) {
	log.Debugw("dropping-revision", log.Fields{"txid": txid, "hash": pr.GetHash(), "config-hash": pr.GetConfig().Hash, "key": pr.GetName(), "isStored": pr.isStored})

	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	if pr.kvStore != nil && txid == "" {
		if err := pr.kvStore.Delete(ctx, pr.GetName()); err != nil {
			log.Errorw("failed-to-remove-revision", log.Fields{"hash": pr.GetHash(), "error": err.Error()})
		} else {
			pr.isStored = false
		}
	} else {
		if includeConfig {
			log.Debugw("attempted-to-remove-transacted-revision-config", log.Fields{"hash": pr.GetConfig().Hash, "txid": txid})
		}
		log.Debugw("attempted-to-remove-transacted-revision", log.Fields{"hash": pr.GetHash(), "txid": txid})
	}

	pr.Revision.Drop(txid, includeConfig)
}

// verifyPersistedEntry validates if the provided data is available or not in memory and applies updates as required
func (pr *PersistedRevision) verifyPersistedEntry(ctx context.Context, data interface{}, typeName string, keyName string,
	keyValue string, txid string, version int64) (response Revision) {
	// Parent which holds the current node entry
	parent := pr.GetBranch().Node.GetRoot()

	// Get a copy of the parent's children
	children := make([]Revision, len(parent.GetBranch(NONE).Latest.GetChildren(typeName)))
	copy(children, parent.GetBranch(NONE).Latest.GetChildren(typeName))

	// Verify if a child with the provided key value can be found
	if childIdx, childRev := pr.getNode().findRevByKey(children, keyName, keyValue); childRev != nil {
		// A child matching the provided key exists in memory
		// Verify if the data differs from what was retrieved from persistence
		// Also check if we are treating a newer revision of the data or not
		if childRev.GetData().(proto.Message).String() != data.(proto.Message).String() && childRev.getVersion() < version {
			log.Debugw("revision-data-is-different", log.Fields{
				"key":               childRev.GetHash(),
				"name":              childRev.GetName(),
				"data":              childRev.GetData(),
				"in-memory-version": childRev.getVersion(),
				"persisted-version": version,
			})

			//
			// Data has changed; replace the child entry and update the parent revision
			//

			// BEGIN Lock child -- prevent any incoming changes
			childRev.GetBranch().LatestLock.Lock()

			// Update child
			updatedChildRev := childRev.UpdateData(ctx, data, childRev.GetBranch())

			updatedChildRev.getNode().SetProxy(childRev.getNode().GetProxy())
			updatedChildRev.SetLastUpdate()
			updatedChildRev.(*PersistedRevision).setVersion(version)

			// Update cache
			getRevCache().Set(updatedChildRev.GetName(), updatedChildRev)
			childRev.Drop(txid, false)

			childRev.GetBranch().LatestLock.Unlock()
			// END lock child

			// Update child entry
			children[childIdx] = updatedChildRev

			// BEGIN lock parent -- Update parent
			parent.GetBranch(NONE).LatestLock.Lock()

			updatedRev := parent.GetBranch(NONE).GetLatest().UpdateChildren(ctx, typeName, children, parent.GetBranch(NONE))
			parent.GetBranch(NONE).Node.makeLatest(parent.GetBranch(NONE), updatedRev, nil)

			parent.GetBranch(NONE).LatestLock.Unlock()
			// END lock parent

			// Drop the previous child revision
			parent.GetBranch(NONE).Latest.ChildDrop(typeName, childRev.GetHash())

			if updatedChildRev != nil {
				log.Debugw("verify-persisted-entry--adding-child", log.Fields{
					"key":  updatedChildRev.GetHash(),
					"name": updatedChildRev.GetName(),
					"data": updatedChildRev.GetData(),
				})
				response = updatedChildRev
			}
		} else {
			log.Debugw("keeping-revision-data", log.Fields{
				"key":                 childRev.GetHash(),
				"name":                childRev.GetName(),
				"data":                childRev.GetData(),
				"in-memory-version":   childRev.getVersion(),
				"persistence-version": version,
			})

			// Update timestamp to reflect when it was last read and to reset tracked timeout
			childRev.SetLastUpdate()
			if childRev.getVersion() < version {
				childRev.(*PersistedRevision).setVersion(version)
			}
			getRevCache().Set(childRev.GetName(), childRev)
			response = childRev
		}

	} else {
		// There is no available child with that key value.
		// Create a new child and update the parent revision.
		log.Debugw("no-such-revision-entry", log.Fields{
			"key":     keyValue,
			"name":    typeName,
			"data":    data,
			"version": version,
		})

		// BEGIN child lock
		pr.GetBranch().LatestLock.Lock()

		// Construct a new child node with the retrieved persistence data
		childRev = pr.GetBranch().Node.MakeNode(data, txid).Latest(txid)

		// We need to start watching this entry for future changes
		childRev.SetName(typeName + "/" + keyValue)
		childRev.(*PersistedRevision).setVersion(version)

		// Add entry to cache
		getRevCache().Set(childRev.GetName(), childRev)

		pr.GetBranch().LatestLock.Unlock()
		// END child lock

		//
		// Add the child to the parent revision
		//

		// BEGIN parent lock
		parent.GetBranch(NONE).LatestLock.Lock()
		children = append(children, childRev)
		updatedRev := parent.GetBranch(NONE).GetLatest().UpdateChildren(ctx, typeName, children, parent.GetBranch(NONE))
		updatedRev.getNode().SetProxy(parent.GetBranch(NONE).Node.GetProxy())
		parent.GetBranch(NONE).Node.makeLatest(parent.GetBranch(NONE), updatedRev, nil)
		parent.GetBranch(NONE).LatestLock.Unlock()
		// END parent lock

		// Child entry is valid and can be included in the response object
		if childRev != nil {
			log.Debugw("adding-revision-to-response", log.Fields{
				"key":  childRev.GetHash(),
				"name": childRev.GetName(),
				"data": childRev.GetData(),
			})
			response = childRev
		}
	}

	return response
}

// LoadFromPersistence retrieves data from kv store at the specified location and refreshes the memory
// by adding missing entries, updating changed entries and ignoring unchanged ones
func (pr *PersistedRevision) LoadFromPersistence(ctx context.Context, path string, txid string, blobs map[string]*kvstore.KVPair) ([]Revision, error) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	log.Debugw("loading-from-persistence", log.Fields{"path": path, "txid": txid})

	var response []Revision
	var err error

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	if pr.kvStore != nil && path != "" {
		if len(blobs) == 0 {
			log.Debugw("retrieve-from-kv", log.Fields{"path": path, "txid": txid})

			if blobs, err = pr.kvStore.List(ctx, path); err != nil {
				log.Errorw("failed-to-retrieve-data-from-kvstore", log.Fields{"error": err})
				return nil, err
			}
		}

		partition := strings.SplitN(path, "/", 2)
		name := partition[0]

		var nodeType interface{}
		if len(partition) < 2 {
			path = ""
			nodeType = pr.GetBranch().Node.Type
		} else {
			path = partition[1]
			nodeType = pr.GetBranch().Node.GetRoot().Type
		}

		field := ChildrenFields(nodeType)[name]

		if field != nil && field.IsContainer {
			log.Debugw("parsing-data-blobs", log.Fields{
				"path": path,
				"name": name,
				"size": len(blobs),
			})

			for _, blob := range blobs {
				output := blob.Value.([]byte)

				data := reflect.New(field.ClassType.Elem())

				if err := proto.Unmarshal(output, data.Interface().(proto.Message)); err != nil {
					log.Errorw("failed-to-unmarshal", log.Fields{
						"path":  path,
						"txid":  txid,
						"error": err,
					})
				} else if path == "" {
					if field.Key != "" {
						log.Debugw("no-path-with-container-key", log.Fields{
							"path": path,
							"txid": txid,
							"data": data.Interface(),
						})

						// Retrieve the key identifier value from the data structure
						// based on the field's key attribute
						_, key := GetAttributeValue(data.Interface(), field.Key, 0)

						if entry := pr.verifyPersistedEntry(ctx, data.Interface(), name, field.Key, key.String(), txid, blob.Version); entry != nil {
							response = append(response, entry)
						}
					} else {
						log.Debugw("path-with-no-container-key", log.Fields{
							"path": path,
							"txid": txid,
							"data": data.Interface(),
						})
					}

				} else if field.Key != "" {
					log.Debugw("path-with-container-key", log.Fields{
						"path": path,
						"txid": txid,
						"data": data.Interface(),
					})
					// The request is for a specific entry/id
					partition := strings.SplitN(path, "/", 2)
					key := partition[0]
					if len(partition) < 2 {
						path = ""
					} else {
						path = partition[1]
					}
					keyValue := field.KeyFromStr(key)

					if entry := pr.verifyPersistedEntry(ctx, data.Interface(), name, field.Key, keyValue.(string), txid, blob.Version); entry != nil {
						response = append(response, entry)
					}
				}
			}

			log.Debugw("no-more-data-blobs", log.Fields{"path": path, "name": name})
		} else {
			log.Debugw("cannot-process-field", log.Fields{
				"type": pr.GetBranch().Node.Type,
				"name": name,
			})
		}
	}

	return response, nil
}
