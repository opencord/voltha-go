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
	"crypto/md5"
	"fmt"
	"testing"
)

var (
	BRANCH *Branch
	HASH   string
)

func Test_ConfigBranch_New(t *testing.T) {
	node := &Node{}
	hash := fmt.Sprintf("%x", md5.Sum([]byte("origin_hash")))
	origin := &Revision{
		Config:   &DataRevision{},
		Children: make(map[string][]*Revision),
		Hash:     hash,
		branch:   &Branch{},
		WeakRef:  "need to fix this",
	}
	txid := fmt.Sprintf("%x", md5.Sum([]byte("branch_transaction_id")))

	BRANCH = NewBranch(node, txid, origin, true)

	t.Logf("New branch created: %+v\n", BRANCH)
}

func Test_ConfigBranch_AddRevision(t *testing.T) {
	HASH = fmt.Sprintf("%x", md5.Sum([]byte("revision_hash")))
	rev := &Revision{
		Config:   &DataRevision{},
		Children: make(map[string][]*Revision),
		Hash:     HASH,
		branch:   &Branch{},
		WeakRef:  "need to fix this",
	}

	BRANCH.revisions[HASH] = rev
	t.Logf("Added revision: %+v\n", rev)
}

func Test_ConfigBranch_GetRevision(t *testing.T) {
	rev := BRANCH.get(HASH)
	t.Logf("Got revision for hash:%s rev:%+v\n", HASH, rev)
}
func Test_ConfigBranch_LatestRevision(t *testing.T) {
	rev := BRANCH.GetLatest()
	t.Logf("Got GetLatest revision:%+v\n", rev)
}
func Test_ConfigBranch_OriginRevision(t *testing.T) {
	rev := BRANCH.origin
	t.Logf("Got origin revision:%+v\n", rev)
}
