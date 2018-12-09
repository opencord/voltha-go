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
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
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
		log.Debugf("Config already exists - hash:%s, stack: %s", pr.GetConfig().Hash, string(debug.Stack()))
		return
		//}
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
			log.Warnf("Problem storing revision config - error: %s, hash: %s, data: %+v", err.Error(),
				pr.GetHash(),
				pr.GetConfig().Data)
		} else {
			log.Debugf("Stored config - hash:%s, blob: %+v, stack: %s", pr.GetHash(), pr.GetConfig().Data,
				string(debug.Stack()))
		}
	}
}

func (pr *PersistedRevision) LoadFromPersistence(path string, txid string) []Revision {
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
								children = make([]Revision, len(rev.GetChildren()[name]))
								copy(children, rev.GetChildren()[name])

								_, key := GetAttributeValue(data.Interface(), field.Key, 0)

								childRev := rev.GetBranch().Node.MakeNode(data.Interface(), txid).Latest(txid)
								childRev.SetHash(name + "/" + key.String())
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

						children = make([]Revision, len(rev.GetChildren()[name]))
						copy(children, rev.GetChildren()[name])

						idx, childRev := rev.GetBranch().Node.findRevByKey(children, field.Key, keyValue)

						newChildRev := childRev.LoadFromPersistence(path, txid)

						children[idx] = newChildRev[0]

						rev := rev.UpdateChildren(name, rev.GetChildren()[name], rev.GetBranch())
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
	newNPR := pr.Revision.UpdateData(data, branch)

	log.Debugf("Updating data %+v", data)
	newPR := &PersistedRevision{
		Revision: newNPR,
		Compress: pr.Compress,
		kvStore:  pr.kvStore,
	}

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
