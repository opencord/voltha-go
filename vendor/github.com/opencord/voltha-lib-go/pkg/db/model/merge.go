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
	"github.com/opencord/voltha-lib-go/pkg/log"
)

func revisionsAreEqual(a, b []Revision) bool {
	// If one is nil, the other must also be nil.
	if (a == nil) != (b == nil) {
		return false
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

type changeAnalysis struct {
	KeyMap1     map[string]int
	KeyMap2     map[string]int
	AddedKeys   map[string]struct{}
	RemovedKeys map[string]struct{}
	ChangedKeys map[string]struct{}
}

func newChangeAnalysis(lst1, lst2 []Revision, keyName string) *changeAnalysis {
	changes := &changeAnalysis{}

	changes.KeyMap1 = make(map[string]int)
	changes.KeyMap2 = make(map[string]int)

	changes.AddedKeys = make(map[string]struct{})
	changes.RemovedKeys = make(map[string]struct{})
	changes.ChangedKeys = make(map[string]struct{})

	for i, rev := range lst1 {
		_, v := GetAttributeValue(rev.GetData(), keyName, 0)
		changes.KeyMap1[v.String()] = i
	}
	for i, rev := range lst2 {
		_, v := GetAttributeValue(rev.GetData(), keyName, 0)
		changes.KeyMap2[v.String()] = i
	}
	for v := range changes.KeyMap2 {
		if _, ok := changes.KeyMap1[v]; !ok {
			changes.AddedKeys[v] = struct{}{}
		}
	}
	for v := range changes.KeyMap1 {
		if _, ok := changes.KeyMap2[v]; !ok {
			changes.RemovedKeys[v] = struct{}{}
		}
	}
	for v := range changes.KeyMap1 {
		if _, ok := changes.KeyMap2[v]; ok && lst1[changes.KeyMap1[v]].GetHash() != lst2[changes.KeyMap2[v]].GetHash() {
			changes.ChangedKeys[v] = struct{}{}
		}
	}

	return changes
}

// Merge3Way takes care of combining the revision contents of the same data set
func Merge3Way(
	forkRev, srcRev, dstRev Revision,
	mergeChildFunc func(Revision) Revision,
	dryRun bool) (rev Revision, changes []ChangeTuple) {

	log.Debugw("3-way-merge-request", log.Fields{"dryRun": dryRun})

	var configChanged bool
	var revsToDiscard []Revision

	if dstRev.GetConfig() == forkRev.GetConfig() {
		configChanged = dstRev.GetConfig() != srcRev.GetConfig()
	} else {
		if dstRev.GetConfig().Hash != srcRev.GetConfig().Hash {
			log.Error("config-collision")
		}
		configChanged = true
	}

	//newChildren := reflect.ValueOf(dstRev.GetAllChildren()).Interface().(map[string][]Revision)
	newChildren := make(map[string][]Revision)
	for entryName, childrenEntry := range dstRev.GetAllChildren() {
		//newRev.Children[entryName] = append(newRev.Children[entryName], childrenEntry...)
		newChildren[entryName] = make([]Revision, len(childrenEntry))
		copy(newChildren[entryName], childrenEntry)
	}

	childrenFields := ChildrenFields(forkRev.GetData())

	for fieldName, field := range childrenFields {
		forkList := forkRev.GetChildren(fieldName)
		srcList := srcRev.GetChildren(fieldName)
		dstList := dstRev.GetChildren(fieldName)

		if revisionsAreEqual(dstList, srcList) {
			for _, rev := range srcList {
				mergeChildFunc(rev)
			}
			continue
		}

		if field.Key == "" {
			if revisionsAreEqual(dstList, forkList) {
				if !revisionsAreEqual(srcList, forkList) {
					log.Error("we should not be here")
				} else {
					for _, rev := range srcList {
						newChildren[fieldName] = append(newChildren[fieldName], mergeChildFunc(rev))
					}
					if field.IsContainer {
						changes = append(
							changes, ChangeTuple{POST_LISTCHANGE,
								NewOperationContext("", nil, fieldName, ""), nil},
						)
					}
				}
			} else {
				if !revisionsAreEqual(srcList, forkList) {
					log.Error("cannot merge - single child node or un-keyed children list has changed")
				}
			}
		} else {
			if revisionsAreEqual(dstList, forkList) {
				src := newChangeAnalysis(forkList, srcList, field.Key)

				newList := make([]Revision, len(srcList))
				copy(newList, srcList)

				for key := range src.AddedKeys {
					idx := src.KeyMap2[key]
					newRev := mergeChildFunc(newList[idx])

					// FIXME: newRev may come back as nil... exclude those entries for now
					if newRev != nil {
						newList[idx] = newRev
						changes = append(changes, ChangeTuple{POST_ADD, newList[idx].GetData(), newRev.GetData()})
					}
				}
				for key := range src.RemovedKeys {
					oldRev := forkList[src.KeyMap1[key]]
					revsToDiscard = append(revsToDiscard, oldRev)
					changes = append(changes, ChangeTuple{POST_REMOVE, oldRev.GetData(), nil})
				}
				for key := range src.ChangedKeys {
					idx := src.KeyMap2[key]
					newRev := mergeChildFunc(newList[idx])

					// FIXME: newRev may come back as nil... exclude those entries for now
					if newRev != nil {
						newList[idx] = newRev
					}
				}

				if !dryRun {
					newChildren[fieldName] = newList
				}
			} else {
				src := newChangeAnalysis(forkList, srcList, field.Key)
				dst := newChangeAnalysis(forkList, dstList, field.Key)

				newList := make([]Revision, len(dstList))
				copy(newList, dstList)

				for key := range src.AddedKeys {
					if _, exists := dst.AddedKeys[key]; exists {
						childDstRev := dstList[dst.KeyMap2[key]]
						childSrcRev := srcList[src.KeyMap2[key]]
						if childDstRev.GetHash() == childSrcRev.GetHash() {
							mergeChildFunc(childDstRev)
						} else {
							log.Error("conflict error - revision has been added is different")
						}
					} else {
						newRev := mergeChildFunc(srcList[src.KeyMap2[key]])
						newList = append(newList, newRev)
						changes = append(changes, ChangeTuple{POST_ADD, srcList[src.KeyMap2[key]], newRev.GetData()})
					}
				}
				for key := range src.ChangedKeys {
					if _, removed := dst.RemovedKeys[key]; removed {
						log.Error("conflict error - revision has been removed")
					} else if _, changed := dst.ChangedKeys[key]; changed {
						childDstRev := dstList[dst.KeyMap2[key]]
						childSrcRev := srcList[src.KeyMap2[key]]
						if childDstRev.GetHash() == childSrcRev.GetHash() {
							mergeChildFunc(childSrcRev)
						} else if childDstRev.GetConfig().Hash != childSrcRev.GetConfig().Hash {
							log.Error("conflict error - revision has been changed and is different")
						} else {
							newRev := mergeChildFunc(srcList[src.KeyMap2[key]])
							newList[dst.KeyMap2[key]] = newRev
						}
					} else {
						newRev := mergeChildFunc(srcList[src.KeyMap2[key]])
						newList[dst.KeyMap2[key]] = newRev
					}
				}

				// TODO: how do i sort this map in reverse order?
				for key := range src.RemovedKeys {
					if _, changed := dst.ChangedKeys[key]; changed {
						log.Error("conflict error - revision has changed")
					}
					if _, removed := dst.RemovedKeys[key]; !removed {
						dstIdx := dst.KeyMap2[key]
						oldRev := newList[dstIdx]
						revsToDiscard = append(revsToDiscard, oldRev)

						copy(newList[dstIdx:], newList[dstIdx+1:])
						newList[len(newList)-1] = nil
						newList = newList[:len(newList)-1]

						changes = append(changes, ChangeTuple{POST_REMOVE, oldRev.GetData(), nil})
					}
				}

				if !dryRun {
					newChildren[fieldName] = newList
				}
			}
		}
	}

	if !dryRun && len(newChildren) > 0 {
		if configChanged {
			rev = srcRev
		} else {
			rev = dstRev
		}

		for _, discarded := range revsToDiscard {
			discarded.Drop("", true)
		}

		// FIXME: Do not discard the latest value for now
		//dstRev.GetBranch().GetLatest().Drop("", configChanged)
		rev = rev.UpdateAllChildren(newChildren, dstRev.GetBranch())

		if configChanged {
			changes = append(changes, ChangeTuple{POST_UPDATE, dstRev.GetBranch().GetLatest().GetData(), rev.GetData()})
		}
		return rev, changes
	}

	return nil, nil
}
