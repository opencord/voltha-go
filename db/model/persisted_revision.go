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
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"reflect"
	"strings"
	"sync"
)

// PersistedRevision holds information of revision meant to be saved in a persistent storage
type PersistedRevision struct {
	Revision
	Compress bool

	events       chan *kvstore.Event
	kvStore      *Backend
	mutex        sync.RWMutex
	versionMutex sync.RWMutex
	Version      int64
	isStored     bool
	isWatched    bool
}

type watchCache struct {
	Cache sync.Map
}

var watchCacheInstance *watchCache
var watchCacheOne sync.Once

func Watches() *watchCache {
	watchCacheOne.Do(func() {
		watchCacheInstance = &watchCache{Cache: sync.Map{}}
	})
	return watchCacheInstance
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
func (pr *PersistedRevision) Finalize(skipOnExist bool) {
	pr.store(skipOnExist)
}

func (pr *PersistedRevision) store(skipOnExist bool) {
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
			w.Write(blob)
			w.Close()
			blob = b.Bytes()
		}

		GetRevCache().Set(pr.GetName(), pr)
		if err := pr.kvStore.Put(pr.GetName(), blob); err != nil {
			log.Warnw("problem-storing-revision", log.Fields{"error": err, "hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data})
		} else {
			log.Debugw("storing-revision", log.Fields{"hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data, "version": pr.getVersion()})
			pr.isStored = true
		}
	}
}

func (pr *PersistedRevision) SetupWatch(key string) {
	if key == "" {
		log.Debugw("ignoring-watch", log.Fields{"key": key, "revision-hash": pr.GetHash()})
		return
	}

	if _, exists := Watches().Cache.LoadOrStore(key+"-"+pr.GetHash(), struct{}{}); exists {
		return
	}

	if pr.events == nil {
		pr.events = make(chan *kvstore.Event)

		log.Debugw("setting-watch-channel", log.Fields{"key": key, "revision-hash": pr.GetHash()})

		pr.SetName(key)
		pr.events = pr.kvStore.CreateWatch(key)
	}

	if !pr.isWatched {
		pr.isWatched = true

		log.Debugw("setting-watch-routine", log.Fields{"key": key, "revision-hash": pr.GetHash()})

		// Start watching
		go pr.startWatching()
	}
}

func (pr *PersistedRevision) startWatching() {
	log.Debugw("starting-watch", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})

StopWatchLoop:
	for {
		latestRev := pr.GetBranch().GetLatest()

		select {
		case event, ok := <-pr.events:
			if !ok {
				log.Errorw("event-channel-failure: stopping watch loop", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName()})
				break StopWatchLoop
			}
			log.Debugw("received-event", log.Fields{"type": event.EventType, "watch": latestRev.GetName()})

			switch event.EventType {
			case kvstore.DELETE:
				log.Debugw("delete-from-memory", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName()})
				pr.Revision.Drop("", true)
				break StopWatchLoop

			case kvstore.PUT:
				log.Debugw("update-in-memory", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName()})
				if latestRev.getVersion() >= event.Version {
					log.Debugw("skipping-matching-or-older-revision", log.Fields{
						"watch":          latestRev.GetName(),
						"watch-version":  event.Version,
						"latest-version": latestRev.getVersion(),
					})
					continue
				} else {
					log.Debugw("watch-revision-is-newer", log.Fields{
						"watch":          latestRev.GetName(),
						"watch-version":  event.Version,
						"latest-version": latestRev.getVersion(),
					})
				}

				data := reflect.New(reflect.TypeOf(latestRev.GetData()).Elem())

				if err := proto.Unmarshal(event.Value.([]byte), data.Interface().(proto.Message)); err != nil {
					log.Errorw("failed-to-unmarshal-watch-data", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName(), "error": err})
				} else {
					log.Debugw("un-marshaled-watch-data", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName(), "data": data.Interface()})

					var pathLock string
					var blobs map[string]*kvstore.KVPair

					// The watch reported new persistence data.
					// Construct an object that will be used to update the memory
					blobs = make(map[string]*kvstore.KVPair)
					key, _ := kvstore.ToString(event.Key)
					blobs[key] = &kvstore.KVPair{
						Key:     key,
						Value:   event.Value,
						Session: "",
						Lease:   0,
						Version: event.Version,
					}

					if latestRev.GetNode().GetProxy() != nil {
						//
						// If a proxy exists for this revision, use it to lock access to the path
						// and prevent simultaneous updates to the object in memory
						//

						//If the proxy already has a request in progress, then there is no need to process the watch
						if latestRev.GetNode().GetProxy().GetOperation() != PROXY_NONE {
							log.Debugw("operation-in-progress", log.Fields{
								"key":       latestRev.GetHash(),
								"path":      latestRev.GetNode().GetProxy().getFullPath(),
								"operation": latestRev.GetNode().GetProxy().operation.String(),
							})
							continue
						}

						pathLock, _ = latestRev.GetNode().GetProxy().parseForControlledPath(latestRev.GetNode().GetProxy().getFullPath())

						// Reserve the path to prevent others to modify while we reload from persistence
						latestRev.GetNode().GetProxy().GetRoot().KvStore.Client.Reserve(pathLock+"_", uuid.New().String(), ReservationTTL)
						latestRev.GetNode().GetProxy().SetOperation(PROXY_WATCH)

						// Load changes and apply to memory
						latestRev.LoadFromPersistence(context.Background(), latestRev.GetName(), "", blobs)

						// Release path
						latestRev.GetNode().GetProxy().GetRoot().KvStore.Client.ReleaseReservation(pathLock + "_")

					} else {
						// This block should be reached only if coming from a non-proxied request
						log.Debugw("revision-with-no-proxy", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName()})

						// Load changes and apply to memory
						latestRev.LoadFromPersistence(context.Background(), latestRev.GetName(), "", blobs)
					}
				}

			default:
				log.Debugw("unhandled-event", log.Fields{"key": latestRev.GetHash(), "watch": latestRev.GetName(), "type": event.EventType})
			}
		}
	}

	Watches().Cache.Delete(pr.GetName() + "-" + pr.GetHash())

	log.Debugw("exiting-watch", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})
}

// UpdateData modifies the information in the data model and saves it in the persistent storage
func (pr *PersistedRevision) UpdateData(ctx context.Context, data interface{}, branch *Branch) Revision {
	log.Debugw("updating-persisted-data", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateData(ctx, data, branch)

	newPR := &PersistedRevision{
		Revision:  newNPR,
		Compress:  pr.Compress,
		kvStore:   pr.kvStore,
		events:    pr.events,
		Version:   pr.getVersion(),
		isWatched: pr.isWatched,
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
		Revision:  newNPR,
		Compress:  pr.Compress,
		kvStore:   pr.kvStore,
		events:    pr.events,
		Version:   pr.getVersion(),
		isWatched: pr.isWatched,
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
func (pr *PersistedRevision) UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision {
	log.Debugw("updating-all-persisted-children", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateAllChildren(children, branch)

	newPR := &PersistedRevision{
		Revision:  newNPR,
		Compress:  pr.Compress,
		kvStore:   pr.kvStore,
		events:    pr.events,
		Version:   pr.getVersion(),
		isWatched: pr.isWatched,
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

// Drop takes care of eliminating a revision hash that is no longer needed
// and its associated config when required
func (pr *PersistedRevision) StorageDrop(txid string, includeConfig bool) {
	log.Debugw("dropping-revision", log.Fields{"txid": txid, "hash": pr.GetHash(), "config-hash": pr.GetConfig().Hash})

	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	if pr.kvStore != nil && txid == "" {
		if pr.isStored {
			if pr.isWatched {
				pr.kvStore.DeleteWatch(pr.GetName(), pr.events)
				pr.isWatched = false
			}

			if err := pr.kvStore.Delete(pr.GetName()); err != nil {
				log.Errorw("failed-to-remove-revision", log.Fields{"hash": pr.GetHash(), "error": err.Error()})
			} else {
				pr.isStored = false
			}
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
	if childIdx, childRev := pr.GetNode().findRevByKey(children, keyName, keyValue); childRev != nil {
		// A child matching the provided key exists in memory
		// Verify if the data differs from what was retrieved from persistence
		// Also check if we are treating a newer revision of the data or not
		if childRev.GetData().(proto.Message).String() != data.(proto.Message).String() && childRev.getVersion() < version {
			log.Debugw("revision-data-is-different", log.Fields{
				"key":     childRev.GetHash(),
				"name":    childRev.GetName(),
				"data":    childRev.GetData(),
				"version": childRev.getVersion(),
			})

			//
			// Data has changed; replace the child entry and update the parent revision
			//

			// BEGIN Lock child -- prevent any incoming changes
			childRev.GetBranch().LatestLock.Lock()

			// Update child
			updatedChildRev := childRev.UpdateData(ctx, data, childRev.GetBranch())

			updatedChildRev.GetNode().SetProxy(childRev.GetNode().GetProxy())
			updatedChildRev.SetupWatch(updatedChildRev.GetName())
			updatedChildRev.SetLastUpdate()
			updatedChildRev.(*PersistedRevision).setVersion(version)

			// Update cache
			GetRevCache().Set(updatedChildRev.GetName(), updatedChildRev)
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
			if childRev != nil {
				log.Debugw("keeping-revision-data", log.Fields{
					"key":  childRev.GetHash(),
					"name": childRev.GetName(),
					"data": childRev.GetData(),
				})

				// Update timestamp to reflect when it was last read and to reset tracked timeout
				childRev.SetLastUpdate()
				if childRev.getVersion() < version {
					childRev.(*PersistedRevision).setVersion(version)
				}
				GetRevCache().Set(childRev.GetName(), childRev)
				response = childRev
			}
		}

	} else {
		// There is no available child with that key value.
		// Create a new child and update the parent revision.
		log.Debugw("no-such-revision-entry", log.Fields{
			"key":  keyValue,
			"name": typeName,
			"data": data,
		})

		// BEGIN child lock
		pr.GetBranch().LatestLock.Lock()

		// Construct a new child node with the retrieved persistence data
		childRev = pr.GetBranch().Node.MakeNode(data, txid).Latest(txid)

		// We need to start watching this entry for future changes
		childRev.SetName(typeName + "/" + keyValue)
		childRev.SetupWatch(childRev.GetName())
		childRev.(*PersistedRevision).setVersion(version)

		// Add entry to cache
		GetRevCache().Set(childRev.GetName(), childRev)

		pr.GetBranch().LatestLock.Unlock()
		// END child lock

		//
		// Add the child to the parent revision
		//

		// BEGIN parent lock
		parent.GetBranch(NONE).LatestLock.Lock()
		children = append(children, childRev)
		updatedRev := parent.GetBranch(NONE).GetLatest().UpdateChildren(ctx, typeName, children, parent.GetBranch(NONE))
		updatedRev.GetNode().SetProxy(parent.GetBranch(NONE).Node.GetProxy())
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
func (pr *PersistedRevision) LoadFromPersistence(ctx context.Context, path string, txid string, blobs map[string]*kvstore.KVPair) []Revision {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	log.Debugw("loading-from-persistence", log.Fields{"path": path, "txid": txid})

	var response []Revision

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	if pr.kvStore != nil && path != "" {
		if blobs == nil || len(blobs) == 0 {
			log.Debugw("retrieve-from-kv", log.Fields{"path": path, "txid": txid})
			blobs, _ = pr.kvStore.List(path)
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

	return response
}
