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
	"context"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"time"
)

type Revision interface {
	Finalize(bool)
	SetConfig(revision *DataRevision)
	GetConfig() *DataRevision
	Drop(txid string, includeConfig bool)
	StorageDrop(txid string, includeConfig bool)
	ChildDrop(childType string, childHash string)
	ChildDropByName(childName string)
	SetChildren(name string, children []Revision)
	GetChildren(name string) []Revision
	SetAllChildren(children map[string][]Revision)
	GetAllChildren() map[string][]Revision
	SetHash(hash string)
	GetHash() string
	ClearHash()
	getVersion() int64
	SetupWatch(key string)
	SetName(name string)
	GetName() string
	SetBranch(branch *Branch)
	GetBranch() *Branch
	Get(int) interface{}
	GetData() interface{}
	GetNode() *node
	SetLastUpdate(ts ...time.Time)
	GetLastUpdate() time.Time
	LoadFromPersistence(ctx context.Context, path string, txid string, blobs map[string]*kvstore.KVPair) []Revision
	UpdateData(ctx context.Context, data interface{}, branch *Branch) Revision
	UpdateChildren(ctx context.Context, name string, children []Revision, branch *Branch) Revision
	UpdateAllChildren(children map[string][]Revision, branch *Branch) Revision
}
