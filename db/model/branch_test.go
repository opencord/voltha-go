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
	TestBranchBranch *Branch
	TestBranchHash   string
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

	TestBranchBranch = NewBranch(node, txid, origin, true)
	t.Logf("New Branch(txid:%s) created: %+v\n", txid, TestBranchBranch)

	if TestBranchBranch.Latest == nil {
		t.Errorf("Branch latest pointer is nil")
	} else if TestBranchBranch.Origin == nil {
		t.Errorf("Branch origin pointer is nil")
	} else if TestBranchBranch.Node == nil {
		t.Errorf("Branch node pointer is nil")
	} else if TestBranchBranch.Revisions == nil {
		t.Errorf("Branch revisions map is nil")
	} else if TestBranchBranch.Txid == "" {
		t.Errorf("Branch transaction id is empty")
	}
}

// Add a new revision to the branch
func TestBranch_AddRevision(t *testing.T) {
	TestBranchHash = fmt.Sprintf("%x", md5.Sum([]byte("revision_hash")))
	rev := &NonPersistedRevision{
		Config:   &DataRevision{},
		Children: make(map[string][]Revision),
		Hash:     TestBranchHash,
		Branch:   &Branch{},
	}

	TestBranchBranch.AddRevision(rev)
	t.Logf("Added revision: %+v\n", rev)

	if len(TestBranchBranch.Revisions) == 0 {
		t.Errorf("Branch revisions map is empty")
	}
}

// Ensure that the added revision can be retrieved
func TestBranch_GetRevision(t *testing.T) {
	if rev := TestBranchBranch.GetRevision(TestBranchHash); rev == nil {
		t.Errorf("Unable to retrieve revision for hash:%s", TestBranchHash)
	} else {
		t.Logf("Got revision for hash:%s rev:%+v\n", TestBranchHash, rev)
	}
}

// Set the added revision as the latest
func TestBranch_LatestRevision(t *testing.T) {
	addedRevision := TestBranchBranch.GetRevision(TestBranchHash)
	TestBranchBranch.SetLatest(addedRevision)

	rev := TestBranchBranch.GetLatest()
	t.Logf("Retrieved latest revision :%+v", rev)

	if rev == nil {
		t.Error("Unable to retrieve latest revision")
	} else if rev.GetHash() != TestBranchHash {
		t.Errorf("Latest revision does not match hash: %s", TestBranchHash)
	}
}

// Ensure that the origin revision remains and differs from subsequent revisions
func TestBranch_OriginRevision(t *testing.T) {
	rev := TestBranchBranch.Origin
	t.Logf("Retrieved origin revision :%+v", rev)

	if rev == nil {
		t.Error("Unable to retrieve origin revision")
	} else if rev.GetHash() == TestBranchHash {
		t.Errorf("Origin revision should differ from added revision: %s", TestBranchHash)
	}
}
