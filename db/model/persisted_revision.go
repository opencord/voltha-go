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

	if pair, _ := pr.kvStore.Get(pr.GetHash()); pair != nil && skipOnExist {
		log.Debugw("revision-config-already-exists", log.Fields{"hash": pr.GetHash()})
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

		if err := pr.kvStore.Put(pr.GetHash(), blob); err != nil {
			log.Warnw("problem-storing-revision-config",
				log.Fields{"error": err, "hash": pr.GetHash(), "data": pr.GetConfig().Data})
		} else {
			log.Debugw("storing-revision-config",
				log.Fields{"hash": pr.GetHash(), "data": pr.GetConfig().Data})
			pr.isStored = true
		}
	}
}

func (pr *PersistedRevision) SetupWatch(key string) {
	if pr.events == nil {
		pr.events = make(chan *kvstore.Event)

		log.Debugw("setting-watch", log.Fields{"key": key})

		pr.events = pr.kvStore.CreateWatch(key)

		pr.isWatched = true

		// Start watching
		go pr.startWatching()
	}
}

func (pr *PersistedRevision) startWatching() {
	log.Debugw("starting-watch", log.Fields{"key": pr.GetHash()})

StopWatchLoop:
	for {
		select {
		case event, ok := <-pr.events:
			if !ok {
				log.Errorw("event-channel-failure: stopping watch loop", log.Fields{"key": pr.GetHash()})
				break StopWatchLoop
			}

			log.Debugw("received-event", log.Fields{"type": event.EventType})

			switch event.EventType {
			case kvstore.DELETE:
				log.Debugw("delete-from-memory", log.Fields{"key": pr.GetHash()})
				pr.Revision.Drop("", true)
				break StopWatchLoop

			case kvstore.PUT:
				log.Debugw("update-in-memory", log.Fields{"key": pr.GetHash()})

				if dataPair, err := pr.kvStore.Get(pr.GetHash()); err != nil || dataPair == nil {
					log.Errorw("update-in-memory--key-retrieval-failed", log.Fields{"key": pr.GetHash(), "error": err})
				} else {
					data := reflect.New(reflect.TypeOf(pr.GetData()).Elem())

					if err := proto.Unmarshal(dataPair.Value.([]byte), data.Interface().(proto.Message)); err != nil {
						log.Errorw("update-in-memory--unmarshal-failed", log.Fields{"key": pr.GetHash(), "error": err})
					} else {
						pr.UpdateData(data.Interface(), pr.GetBranch())
					}
				}

			default:
				log.Debugw("unhandled-event", log.Fields{"key": pr.GetHash(), "type": event.EventType})
			}
		}
	}

	log.Debugw("exiting-watch", log.Fields{"key": pr.GetHash()})
}

func (pr *PersistedRevision) LoadFromPersistence(path string, txid string) []Revision {
	log.Debugw("loading-from-persistence", log.Fields{"path": path, "txid": txid})

	var response []Revision
	var rev Revision

	rev = pr

	if pr.kvStore != nil {
		blobMap, _ := pr.kvStore.List(path)

		partition := strings.SplitN(path, "/", 2)
		name := partition[0]

		if len(partition) < 2 {
			path = ""
		} else {
			path = partition[1]
		}

		field := ChildrenFields(rev.GetBranch().Node.Type)[name]

		if field.IsContainer {
			for _, blob := range blobMap {
				output := blob.Value.([]byte)

				data := reflect.New(field.ClassType.Elem())

				if err := proto.Unmarshal(output, data.Interface().(proto.Message)); err != nil {
					// TODO report error
				} else {

					var children []Revision

					if path == "" {
						if field.Key != "" {
							// e.g. /logical_devices/abcde --> path="" name=logical_devices key=abcde
							if field.Key != "" {
								children = make([]Revision, len(rev.GetChildren(name)))
								copy(children, rev.GetChildren(name))

								_, key := GetAttributeValue(data.Interface(), field.Key, 0)

								childRev := rev.GetBranch().Node.MakeNode(data.Interface(), txid).Latest(txid)
								childRev.SetHash(name + "/" + key.String())

								// Create watch for <component>/<key>
								pr.SetupWatch(childRev.GetHash())

								children = append(children, childRev)
								rev = rev.UpdateChildren(name, children, rev.GetBranch())

								rev.GetBranch().Node.makeLatest(rev.GetBranch(), rev, nil)

								response = append(response, childRev)
								continue
							}
						}
					} else if field.Key != "" {
						// e.g. /logical_devices/abcde/flows/vwxyz --> path=abcde/flows/vwxyz

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

						idx, childRev := rev.GetBranch().Node.findRevByKey(children, field.Key, keyValue)

						newChildRev := childRev.LoadFromPersistence(path, txid)

						children[idx] = newChildRev[0]

						rev := rev.UpdateChildren(name, rev.GetChildren(name), rev.GetBranch())
						rev.GetBranch().Node.makeLatest(rev.GetBranch(), rev, nil)

						response = append(response, newChildRev[0])
						continue
					}
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
	log.Debugw("dropping-revision",
		log.Fields{"txid": txid, "hash": pr.GetHash(), "config-hash": pr.GetConfig().Hash, "stack": string(debug.Stack())})

	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	if pr.kvStore != nil && txid == "" {
		if pr.isStored {
			if includeConfig {
				if err := pr.kvStore.Delete(pr.GetConfig().Hash); err != nil {
					log.Errorw("failed-to-remove-revision-config", log.Fields{"hash": pr.GetConfig().Hash, "error": err.Error()})
				}
			}

			if err := pr.kvStore.Delete(pr.GetHash()); err != nil {
				log.Errorw("failed-to-remove-revision", log.Fields{"hash": pr.GetHash(), "error": err.Error()})
			} else {
				pr.isStored = false
			}

			if pr.isWatched {
				pr.kvStore.DeleteWatch(pr.GetHash(), pr.events)
				pr.isWatched = false
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
