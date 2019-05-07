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
	"github.com/golang/protobuf/proto"
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

	events    chan *kvstore.Event
	kvStore   *Backend
	mutex     sync.RWMutex
	isStored  bool
	isWatched bool
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
	pr.Revision = NewNonPersistedRevision(nil, branch, data, children)
	return pr
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

		if err := pr.kvStore.Put(pr.GetName(), blob); err != nil {
			log.Warnw("problem-storing-revision", log.Fields{"error": err, "hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data})
		} else {
			log.Debugw("storing-revision", log.Fields{"hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data})
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
		if pr.IsDiscarded() {
			break StopWatchLoop
		}

		select {
		case event, ok := <-pr.events:
			if !ok {
				log.Errorw("event-channel-failure: stopping watch loop", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})
				break StopWatchLoop
			}

			log.Debugw("received-event", log.Fields{"type": event.EventType, "watch": pr.GetName()})

			switch event.EventType {
			case kvstore.DELETE:
				log.Debugw("delete-from-memory", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})
				pr.Revision.Drop("", true)
				break StopWatchLoop

			case kvstore.PUT:
				log.Debugw("update-in-memory", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})

				data := reflect.New(reflect.TypeOf(pr.GetData()).Elem())

				if err := proto.Unmarshal(event.Value.([]byte), data.Interface().(proto.Message)); err != nil {
					log.Errorw("failed-to-unmarshal-watch-data", log.Fields{"key": pr.GetHash(), "watch": pr.GetName(), "error": err})
				} else {
					var pathLock string
					var pac *proxyAccessControl
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
					}

					if pr.GetNode().GetProxy() != nil {
						//
						// If a proxy exists for this revision, use it to lock access to the path
						// and prevent simultaneous updates to the object in memory
						//
						pathLock, _ = pr.GetNode().GetProxy().parseForControlledPath(pr.GetNode().GetProxy().getFullPath())

						//If the proxy already has a request in progress, then there is no need to process the watch
						log.Debugw("checking-if-path-is-locked", log.Fields{"key": pr.GetHash(), "pathLock": pathLock})
						if PAC().IsReserved(pathLock) {
							log.Debugw("operation-in-progress", log.Fields{
								"key":       pr.GetHash(),
								"path":      pr.GetNode().GetProxy().getFullPath(),
								"operation": pr.GetNode().GetRoot().GetProxy().Operation,
							})

							continue

							// TODO/FIXME: keep logic for now in case we need to control based on running operation
							//
							// The code below seems to revert the in-memory/persistence value (occasionally) with
							// the one received from the watch event.
							//
							// The same problem may occur, in the scenario where the core owning a device
							// receives a watch event for an update made by another core, and when the owning core is
							// also processing an update.  Need to investigate...
							//
							//switch pr.GetNode().GetRoot().GetProxy().Operation {
							//case PROXY_UPDATE:
							//	// We will need to reload once the update operation completes.
							//	// Therefore, the data of the current event is most likely out-dated
							//	// and should be ignored
							//	log.Debugw("reload-required", log.Fields{
							//		"key":       pr.GetHash(),
							//		"path":      pr.GetNode().GetProxy().getFullPath(),
							//		"operation": pr.GetNode().GetRoot().GetProxy().Operation,
							//	})
							//
							//	// Eliminate the object constructed earlier
							//	blobs = nil
							//
							//case PROXY_ADD:
							//	fallthrough
							//
							//case PROXY_REMOVE:
							//	fallthrough
							//
							//case PROXY_GET:
							//	fallthrough
							//
							//default:
							//	// No need to process the event ... move on
							//	log.Debugw("", log.Fields{
							//		"key":       pr.GetHash(),
							//		"path":      pr.GetNode().GetProxy().getFullPath(),
							//		"operation": pr.GetNode().GetRoot().GetProxy().Operation,
							//	})
							//
							//	continue
							//}
						}

						// Reserve the path to prevent others to modify while we reload from persistence
						log.Debugw("reserve-and-lock-path", log.Fields{"key": pr.GetHash(), "path": pathLock})
						pac = PAC().ReservePath(pr.GetNode().GetProxy().getFullPath(), pr.GetNode().GetProxy(), pathLock)
						pac.SetProxy(pr.GetNode().GetProxy())
						pac.lock()

						// Load changes and apply to memory
						pr.LoadFromPersistence(pr.GetName(), "", blobs)

						log.Debugw("release-and-unlock-path", log.Fields{"key": pr.GetHash(), "path": pathLock})
						pac.unlock()
						PAC().ReleasePath(pathLock)

					} else {
						log.Debugw("revision-with-no-proxy", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})

						// Load changes and apply to memory
						pr.LoadFromPersistence(pr.GetName(), "", blobs)
					}
				}

			default:
				log.Debugw("unhandled-event", log.Fields{"key": pr.GetHash(), "watch": pr.GetName(), "type": event.EventType})
			}
		}
	}

	Watches().Cache.Delete(pr.GetName() + "-" + pr.GetHash())

	log.Debugw("exiting-watch", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})
}

// UpdateData modifies the information in the data model and saves it in the persistent storage
func (pr *PersistedRevision) UpdateData(data interface{}, branch *Branch) Revision {
	log.Debugw("updating-persisted-data", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateData(data, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
		events:   pr.events,
	}

	if newPR.GetHash() != pr.GetHash() {
		newPR.isWatched = false
		newPR.isStored = false
		pr.Drop(branch.Txid, false)
		pr.Drop(branch.Txid, false)
	} else {
		newPR.isWatched = true
		newPR.isStored = true
	}

	return newPR
}

// UpdateChildren modifies the children of a revision and of a specific component and saves it in the persistent storage
func (pr *PersistedRevision) UpdateChildren(name string, children []Revision,
	branch *Branch) Revision {
	log.Debugw("updating-persisted-children", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateChildren(name, children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
		events:   pr.events,
	}

	if newPR.GetHash() != pr.GetHash() {
		newPR.isWatched = false
		newPR.isStored = false
		pr.Drop(branch.Txid, false)
	} else {
		newPR.isWatched = true
		newPR.isStored = true
	}

	return newPR
}

// UpdateAllChildren modifies the children for all components of a revision and saves it in the peristent storage
func (pr *PersistedRevision) UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision {
	log.Debugw("updating-all-persisted-children", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateAllChildren(children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
		events:   pr.events,
	}

	if newPR.GetHash() != pr.GetHash() {
		newPR.isWatched = false
		newPR.isStored = false
		pr.Drop(branch.Txid, false)
	} else {
		newPR.isWatched = true
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
	log.Debugw("dropping-revision",
		log.Fields{"txid": txid, "hash": pr.GetHash(), "config-hash": pr.GetConfig().Hash})

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
func (pr *PersistedRevision) verifyPersistedEntry(data interface{}, typeName string, keyName string, keyValue string, txid string) (response Revision) {
	//rev := pr

	children := make([]Revision, len(pr.GetBranch().GetLatest().GetChildren(typeName)))
	copy(children, pr.GetBranch().GetLatest().GetChildren(typeName))

	// Verify if the revision contains a child that matches that key
	if childIdx, childRev := pr.GetNode().findRevByKey(pr.GetBranch().GetLatest().GetChildren(typeName), keyName,
		keyValue); childRev != nil {
		// A child matching the provided key exists in memory
		// Verify if the data differs to what was retrieved from persistence
		if childRev.GetData().(proto.Message).String() != data.(proto.Message).String() {
			log.Debugw("verify-persisted-entry--data-is-different", log.Fields{
				"key":  childRev.GetHash(),
				"name": childRev.GetName(),
				"data": childRev.GetData(),
			})

			// Data has changed; replace the child entry and update the parent revision
			childRev.Drop(txid, false)
			updatedChildRev := childRev.UpdateData(data, childRev.GetBranch())
			updatedChildRev.GetNode().SetProxy(childRev.GetNode().GetProxy())
			updatedChildRev.SetupWatch(updatedChildRev.GetName())

			if childIdx >= 0 {
				children[childIdx] = updatedChildRev
			} else {
				children = append(children, updatedChildRev)
			}

			pr.GetBranch().LatestLock.Lock()
			updatedRev := pr.GetBranch().Node.Latest().UpdateChildren(typeName, children, pr.GetBranch())
			pr.GetBranch().Node.makeLatest(pr.GetBranch(), updatedRev, nil)
			pr.GetBranch().LatestLock.Unlock()

			// Drop the previous child revision
			pr.GetBranch().Node.Latest().ChildDrop(typeName, childRev.GetHash())

			if updatedChildRev != nil {
				log.Debugw("verify-persisted-entry--adding-child", log.Fields{
					"key":  updatedChildRev.GetHash(),
					"name": updatedChildRev.GetName(),
					"data": updatedChildRev.GetData(),
				})
				response = updatedChildRev
			}
		} else {
			// Data is the same. Continue to the next entry
			log.Debugw("verify-persisted-entry--same-data", log.Fields{
				"key":  childRev.GetHash(),
				"name": childRev.GetName(),
				"data": childRev.GetData(),
			})
			if childRev != nil {
				log.Debugw("verify-persisted-entry--keeping-child", log.Fields{
					"key":  childRev.GetHash(),
					"name": childRev.GetName(),
					"data": childRev.GetData(),
				})
				response = childRev
			}
		}
	} else {
		// There is no available child with that key value.
		// Create a new child and update the parent revision.
		log.Debugw("verify-persisted-entry--no-such-entry", log.Fields{
			"key":  keyValue,
			"name": typeName,
			"data": data,
		})

		// Construct a new child node with the retrieved persistence data
		childRev = pr.GetBranch().Node.MakeNode(data, txid).Latest(txid)

		// We need to start watching this entry for future changes
		childRev.SetName(typeName + "/" + keyValue)

		// Add the child to the parent revision
		pr.GetBranch().LatestLock.Lock()
		children = append(children, childRev)
		updatedRev := pr.GetBranch().Node.Latest().UpdateChildren(typeName, children, pr.GetBranch())
		updatedRev.GetNode().SetProxy(pr.GetNode().GetProxy())
		childRev.SetupWatch(childRev.GetName())

		//rev.GetBranch().Node.Latest().Drop(txid, false)
		pr.GetBranch().Node.makeLatest(pr.GetBranch(), updatedRev, nil)
		pr.GetBranch().LatestLock.Unlock()

		// Child entry is valid and can be included in the response object
		if childRev != nil {
			log.Debugw("verify-persisted-entry--adding-child", log.Fields{
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
func (pr *PersistedRevision) LoadFromPersistence(path string, txid string, blobs map[string]*kvstore.KVPair) []Revision {
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
			nodeType = pr.GetBranch().Node.Root.Type
		}

		field := ChildrenFields(nodeType)[name]

		if field != nil && field.IsContainer {
			log.Debugw("load-from-persistence--start-blobs", log.Fields{
				"path": path,
				"name": name,
				"size": len(blobs),
			})

			for _, blob := range blobs {
				output := blob.Value.([]byte)

				data := reflect.New(field.ClassType.Elem())

				if err := proto.Unmarshal(output, data.Interface().(proto.Message)); err != nil {
					log.Errorw("load-from-persistence--failed-to-unmarshal", log.Fields{
						"path":  path,
						"txid":  txid,
						"error": err,
					})
				} else if path == "" {
					if field.Key != "" {
						// Retrieve the key identifier value from the data structure
						// based on the field's key attribute
						_, key := GetAttributeValue(data.Interface(), field.Key, 0)

						if entry := pr.verifyPersistedEntry(data.Interface(), name, field.Key, key.String(),
							txid); entry != nil {
							response = append(response, entry)
						}
					}

				} else if field.Key != "" {
					// The request is for a specific entry/id
					partition := strings.SplitN(path, "/", 2)
					key := partition[0]
					if len(partition) < 2 {
						path = ""
					} else {
						path = partition[1]
					}
					keyValue := field.KeyFromStr(key)

					if entry := pr.verifyPersistedEntry(data.Interface(), name, field.Key, keyValue.(string),
						txid); entry != nil {
						response = append(response, entry)
					}
				}
			}

			log.Debugw("load-from-persistence--end-blobs", log.Fields{"path": path, "name": name})
		} else {
			log.Debugw("load-from-persistence--cannot-process-field", log.Fields{

				"type": pr.GetBranch().Node.Type,
				"name": name,
			})
		}
	}

	return response
}
