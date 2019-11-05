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
	TestBranch_BRANCH *Branch
	TestBranch_HASH   string
)

// Create a new branch and ensure that fields are populated
func TestBranch_NewBranch(t *testing.T) {
	node := &node{}
	hash := fmt.Sprintf("%x", md5.Sum([]byte("origin_hash")))
	origin := &NonPersistedRevision{
		Config:   &DataRevision{},
		Children: make(map[string][]Revision),
		Hash:     hash,
		Branch:   &Branch{},
	}
	txid := fmt.Sprintf("%x", md5.Sum([]byte("branch_transaction_id")))

	TestBranch_BRANCH = NewBranch(node, txid, origin, true)
	t.Logf("New Branch(txid:%s) created: %+v\n", txid, TestBranch_BRANCH)

	if TestBranch_BRANCH.Latest == nil {
		t.Errorf("Branch latest pointer is nil")
	} else if TestBranch_BRANCH.Origin == nil {
		t.Errorf("Branch origin pointer is nil")
	} else if TestBranch_BRANCH.Node == nil {
		t.Errorf("Branch node pointer is nil")
	} else if TestBranch_BRANCH.Revisions == nil {
		t.Errorf("Branch revisions map is nil")
	} else if TestBranch_BRANCH.Txid == "" {
		t.Errorf("Branch transaction id is empty")
	}
}

// Add a new revision to the branch
func TestBranch_AddRevision(t *testing.T) {
	TestBranch_HASH = fmt.Sprintf("%x", md5.Sum([]byte("revision_hash")))
	rev := &NonPersistedRevision{
		Config:   &DataRevision{},
		Children: make(map[string][]Revision),
		Hash:     TestBranch_HASH,
		Branch:   &Branch{},
	}

	TestBranch_BRANCH.AddRevision(rev)
	t.Logf("Added revision: %+v\n", rev)

	if len(TestBranch_BRANCH.Revisions) == 0 {
		t.Errorf("Branch revisions map is empty")
	}
}

// Ensure that the added revision can be retrieved
func TestBranch_GetRevision(t *testing.T) {
	if rev := TestBranch_BRANCH.GetRevision(TestBranch_HASH); rev == nil {
		t.Errorf("Unable to retrieve revision for hash:%s", TestBranch_HASH)
	} else {
		t.Logf("Got revision for hash:%s rev:%+v\n", TestBranch_HASH, rev)
	}
}

// Set the added revision as the latest
func TestBranch_LatestRevision(t *testing.T) {
	addedRevision := TestBranch_BRANCH.GetRevision(TestBranch_HASH)
	TestBranch_BRANCH.SetLatest(addedRevision)

	rev := TestBranch_BRANCH.GetLatest()
	t.Logf("Retrieved latest revision :%+v", rev)

	if rev == nil {
		t.Error("Unable to retrieve latest revision")
	} else if rev.GetHash() != TestBranch_HASH {
		t.Errorf("Latest revision does not match hash: %s", TestBranch_HASH)
	}
}

// Ensure that the origin revision remains and differs from subsequent revisions
func TestBranch_OriginRevision(t *testing.T) {
	rev := TestBranch_BRANCH.Origin
	t.Logf("Retrieved origin revision :%+v", rev)

	if rev == nil {
		t.Error("Unable to retrieve origin revision")
	} else if rev.GetHash() == TestBranch_HASH {
		t.Errorf("Origin revision should differ from added revision: %s", TestBranch_HASH)
	}
}
