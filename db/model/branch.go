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
	"github.com/opencord/voltha-go/common/log"
	"sync"
)

// TODO: implement weak references or something equivalent
// TODO: missing proper logging

// Branch structure is used to classify a collection of transaction based revisions
type Branch struct {
	mutex      sync.RWMutex
	Node       *node
	Txid       string
	Origin     Revision
	Revisions  map[string]Revision
	LatestLock sync.RWMutex
	Latest     Revision
}

// NewBranch creates a new instance of the Branch structure
func NewBranch(node *node, txid string, origin Revision, autoPrune bool) *Branch {
	b := &Branch{}
	b.Node = node
	b.Txid = txid
	b.Origin = origin
	b.Revisions = make(map[string]Revision)
	b.Latest = origin

	return b
}

// Utility function to extract all children names for a given revision (mostly for debugging purposes)
func (b *Branch) retrieveChildrenNames(revision Revision) []string {
	var childrenNames []string

	for _, child := range revision.GetChildren("devices") {
		childrenNames = append(childrenNames, child.GetName())
	}

	return childrenNames
}

// Utility function to compare children names and report the missing ones (mostly for debugging purposes)
func (b *Branch) findMissingChildrenNames(previousNames, latestNames []string) []string {
	var missingNames []string

	for _, previousName := range previousNames {
		found := false

		if len(latestNames) == 0 {
			break
		}

		for _, latestName := range latestNames {
			if previousName == latestName {
				found = true
				break
			}
		}
		if !found {
			missingNames = append(missingNames, previousName)
		}
	}

	return missingNames
}

// SetLatest assigns the latest revision for this branch
func (b *Branch) SetLatest(latest Revision) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.Latest != nil {
		log.Debugw("updating-latest-revision", log.Fields{"current": b.Latest.GetHash(), "new": latest.GetHash()})

		// Go through list of children names in current revision and new revision
		// and then compare the resulting outputs to ensure that we have not lost any entries.

		if level, _ := log.GetPackageLogLevel(); level == log.DebugLevel {
			var previousNames, latestNames, missingNames []string

			if previousNames = b.retrieveChildrenNames(b.Latest); len(previousNames) > 0 {
				log.Debugw("children-of-previous-revision", log.Fields{"hash": b.Latest.GetHash(), "names": previousNames})
			}

			if latestNames = b.retrieveChildrenNames(b.Latest); len(latestNames) > 0 {
				log.Debugw("children-of-latest-revision", log.Fields{"hash": latest.GetHash(), "names": latestNames})
			}

			if missingNames = b.findMissingChildrenNames(previousNames, latestNames); len(missingNames) > 0 {
				log.Debugw("children-missing-in-latest-revision", log.Fields{"hash": latest.GetHash(), "names": missingNames})
			}
		}

	} else {
		log.Debugw("setting-latest-revision", log.Fields{"new": latest.GetHash()})
	}

	b.Latest = latest
}

// GetLatest retrieves the latest revision of the branch
func (b *Branch) GetLatest() Revision {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	return b.Latest
}

// GetOrigin retrieves the original revision of the branch
func (b *Branch) GetOrigin() Revision {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	return b.Origin
}

// AddRevision inserts a new revision to the branch
func (b *Branch) AddRevision(revision Revision) {
	if revision != nil && b.GetRevision(revision.GetHash()) == nil {
		b.SetRevision(revision.GetHash(), revision)
	}
}

// GetRevision pulls a revision entry at the specified hash
func (b *Branch) GetRevision(hash string) Revision {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if revision, ok := b.Revisions[hash]; ok {
		return revision
	}

	return nil
}

// SetRevision updates a revision entry at the specified hash
func (b *Branch) SetRevision(hash string, revision Revision) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.Revisions[hash] = revision
}

// DeleteRevision removes a revision with the specified hash
func (b *Branch) DeleteRevision(hash string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if _, ok := b.Revisions[hash]; ok {
		delete(b.Revisions, hash)
	}
}
