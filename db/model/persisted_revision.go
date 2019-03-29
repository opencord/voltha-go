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
	"encoding/hex"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
)

// PersistedRevision holds information of revision meant to be saved in a persistent storage
type PersistedRevision struct {
	Revision
	Compress bool

	events    chan *kvstore.Event `json:"-"`
	kvStore   *Backend            `json:"-"`
	mutex     sync.RWMutex        `json:"-"`
	isStored  bool
	isWatched bool
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

type revData struct {
	Children map[string][]string
	Config   string
}

func (pr *PersistedRevision) store(skipOnExist bool) {
	if pr.GetBranch().Txid != "" {
		return
	}

	if pair, _ := pr.kvStore.Get(pr.GetName()); pair != nil && skipOnExist {
		log.Debugw("revision-config-already-exists", log.Fields{"hash": pr.GetHash(), "name": pr.GetName()})
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

		if err := pr.kvStore.Put(pr.GetName(), blob); err != nil {
			log.Warnw("problem-storing-revision-config",
				log.Fields{"error": err, "hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data})
		} else {
			log.Debugw("storing-revision-config", log.Fields{"hash": pr.GetHash(), "name": pr.GetName(), "data": pr.GetConfig().Data})
			pr.isStored = true
		}
	}
}

func (pr *PersistedRevision) SetupWatch(key string) {
	if pr.events == nil {
		pr.events = make(chan *kvstore.Event)

		log.Debugw("setting-watch", log.Fields{"key": key, "revision-hash": pr.GetHash()})

		pr.SetName(key)
		pr.events = pr.kvStore.CreateWatch(key)

		pr.isWatched = true

		// Start watching
		go pr.startWatching()
	}
}

func (pr *PersistedRevision) updateInMemory(data interface{}) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	var pac *proxyAccessControl
	var pathLock string

	//
	// If a proxy exists for this revision, use it to lock access to the path
	// and prevent simultaneous updates to the object in memory
	//
	if pr.GetNode().GetProxy() != nil {
		pathLock, _ = pr.GetNode().GetProxy().parseForControlledPath(pr.GetNode().GetProxy().getFullPath())

		// If the proxy already has a request in progress, then there is no need to process the watch
		log.Debugw("update-in-memory--checking-pathlock", log.Fields{"key": pr.GetHash(), "pathLock": pathLock})
		if PAC().IsReserved(pathLock) {
			switch pr.GetNode().GetRoot().GetProxy().Operation {
			case PROXY_ADD:
				fallthrough
			case PROXY_REMOVE:
				fallthrough
			case PROXY_UPDATE:
				log.Debugw("update-in-memory--skipping", log.Fields{"key": pr.GetHash(), "path": pr.GetNode().GetProxy().getFullPath()})
				return
			default:
				log.Debugw("update-in-memory--operation", log.Fields{"operation": pr.GetNode().GetRoot().GetProxy().Operation})
			}
		} else {
			log.Debugw("update-in-memory--path-not-locked", log.Fields{"key": pr.GetHash(), "path": pr.GetNode().GetProxy().getFullPath()})
		}

		log.Debugw("update-in-memory--reserve-and-lock", log.Fields{"key": pr.GetHash(), "path": pathLock})

		pac = PAC().ReservePath(pr.GetNode().GetProxy().getFullPath(), pr.GetNode().GetProxy(), pathLock)
		pac.SetProxy(pr.GetNode().GetProxy())
		pac.lock()

		defer log.Debugw("update-in-memory--release-and-unlock", log.Fields{"key": pr.GetHash(), "path": pathLock})
		defer pac.unlock()
		defer PAC().ReleasePath(pathLock)
	}

	//
	// Update the object in memory through a transaction
	// This will allow for the object to be subsequently merged with any changes
	// that might have occurred in memory
	//

	log.Debugw("update-in-memory--custom-transaction", log.Fields{"key": pr.GetHash()})

	// Prepare the transaction
	branch := pr.GetBranch()
	latest := branch.GetLatest()
	txidBin, _ := uuid.New().MarshalBinary()
	txid := hex.EncodeToString(txidBin)[:12]

	makeBranch := func(node *node) *Branch {
		return node.MakeBranch(txid)
	}

	// Apply the update in a transaction branch
	updatedRev := latest.GetNode().Update("", data, false, txid, makeBranch)
	updatedRev.SetName(latest.GetName())

	// Merge the transaction branch in memory
	if mergedRev, _ := latest.GetNode().MergeBranch(txid, false); mergedRev != nil {
		branch.SetLatest(mergedRev)
	}
}

func (pr *PersistedRevision) startWatching() {
	log.Debugw("starting-watch", log.Fields{"key": pr.GetHash()})

StopWatchLoop:
	for {
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

				if dataPair, err := pr.kvStore.Get(pr.GetName()); err != nil || dataPair == nil {
					log.Errorw("update-in-memory--key-retrieval-failed", log.Fields{"key": pr.GetHash(), "watch": pr.GetName(), "error": err})
				} else {
					data := reflect.New(reflect.TypeOf(pr.GetData()).Elem())

					if err := proto.Unmarshal(dataPair.Value.([]byte), data.Interface().(proto.Message)); err != nil {
						log.Errorw("update-in-memory--unmarshal-failed", log.Fields{"key": pr.GetHash(), "watch": pr.GetName(), "error": err})
					} else {
						pr.updateInMemory(data.Interface())
					}
				}

			default:
				log.Debugw("unhandled-event", log.Fields{"key": pr.GetHash(), "watch": pr.GetName(), "type": event.EventType})
			}
		}
	}

	log.Debugw("exiting-watch", log.Fields{"key": pr.GetHash(), "watch": pr.GetName()})
}

func (pr *PersistedRevision) LoadFromPersistence(path string, txid string) []Revision {
	log.Debugw("loading-from-persistence", log.Fields{"path": path, "txid": txid})

	var response []Revision
	var rev Revision

	rev = pr

	if pr.kvStore != nil && path != "" {
		blobMap, _ := pr.kvStore.List(path)

		partition := strings.SplitN(path, "/", 2)
		name := partition[0]

		if len(partition) < 2 {
			path = ""
		} else {
			path = partition[1]
		}

		field := ChildrenFields(rev.GetBranch().Node.Type)[name]

		if field != nil && field.IsContainer {
			var children []Revision
			children = make([]Revision, len(rev.GetChildren(name)))
			copy(children, rev.GetChildren(name))
			existChildMap := make(map[string]int)
			for i, child := range rev.GetChildren(name) {
				existChildMap[child.GetHash()] = i
			}

			for _, blob := range blobMap {
				output := blob.Value.([]byte)

				data := reflect.New(field.ClassType.Elem())

				if err := proto.Unmarshal(output, data.Interface().(proto.Message)); err != nil {
					log.Errorw(
						"loading-from-persistence--failed-to-unmarshal",
						log.Fields{"path": path, "txid": txid, "error": err},
					)
				} else if field.Key != "" {
					var key reflect.Value
					var keyValue interface{}
					var keyStr string

					if path == "" {
						// e.g. /logical_devices --> path="" name=logical_devices key=""
						_, key = GetAttributeValue(data.Interface(), field.Key, 0)
						keyStr = key.String()

					} else {
						// e.g.
						// /logical_devices/abcde --> path="abcde" name=logical_devices
						// /logical_devices/abcde/image_downloads --> path="abcde/image_downloads" name=logical_devices

						partition := strings.SplitN(path, "/", 2)
						key := partition[0]
						if len(partition) < 2 {
							path = ""
						} else {
							path = partition[1]
						}
						keyValue = field.KeyFromStr(key)
						keyStr = keyValue.(string)

						if idx, childRev := rev.GetBranch().Node.findRevByKey(children, field.Key, keyValue); childRev != nil {
							// Key is memory, continue recursing path
							if newChildRev := childRev.LoadFromPersistence(path, txid); newChildRev != nil {
								children[idx] = newChildRev[0]

								rev := rev.UpdateChildren(name, rev.GetChildren(name), rev.GetBranch())
								rev.GetBranch().Node.makeLatest(rev.GetBranch(), rev, nil)

								response = append(response, newChildRev[0])
								continue
							}
						}
					}

					childRev := rev.GetBranch().Node.MakeNode(data.Interface(), txid).Latest(txid)
					childRev.SetName(name + "/" + keyStr)

					// Do not process a child that is already in memory
					if _, childExists := existChildMap[childRev.GetHash()]; !childExists {
						// Create watch for <component>/<key>
						childRev.SetupWatch(childRev.GetName())

						children = append(children, childRev)
						rev = rev.UpdateChildren(name, children, rev.GetBranch())

						rev.GetBranch().Node.makeLatest(rev.GetBranch(), rev, nil)
					}
					response = append(response, childRev)
					continue
				}
			}
		}
	}

	return response
}

// UpdateData modifies the information in the data model and saves it in the persistent storage
func (pr *PersistedRevision) UpdateData(data interface{}, branch *Branch) Revision {
	log.Debugw("updating-persisted-data", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateData(data, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
	}

	return newPR
}

// UpdateChildren modifies the children of a revision and of a specific component and saves it in the persistent storage
func (pr *PersistedRevision) UpdateChildren(name string, children []Revision, branch *Branch) Revision {
	log.Debugw("updating-persisted-children", log.Fields{"hash": pr.GetHash()})

	newNPR := pr.Revision.UpdateChildren(name, children, branch)

	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
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
		log.Fields{"txid": txid, "hash": pr.GetHash(), "config-hash": pr.GetConfig().Hash, "stack": string(debug.Stack())})

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
