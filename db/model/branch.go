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
	sync.RWMutex
	Node      *node
	Txid      string
	Origin    Revision
	Revisions map[string]Revision
	Latest    Revision
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

// SetLatest assigns the latest revision for this branch
func (b *Branch) SetLatest(latest Revision) {
	b.Lock()
	defer b.Unlock()

	if b.Latest != nil {
		log.Debugf("Switching latest from <%s> to <%s>", b.Latest.GetHash(), latest.GetHash())
	} else {
		log.Debugf("Switching latest from <NIL> to <%s>", latest.GetHash())
	}


	b.Latest = latest
}

// GetLatest retrieves the latest revision of the branch
func (b *Branch) GetLatest() Revision {
	b.Lock()
	defer b.Unlock()

	return b.Latest
}

// GetOrigin retrieves the original revision of the branch
func (b *Branch) GetOrigin() Revision {
	b.Lock()
	defer b.Unlock()

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
	b.Lock()
	defer b.Unlock()

	if revision, ok := b.Revisions[hash]; ok {
		return revision
	}

	return nil
}

// SetRevision updates a revision entry at the specified hash
func (b *Branch) SetRevision(hash string, revision Revision) {
	b.Lock()
	defer b.Unlock()

	b.Revisions[hash] = revision
}
