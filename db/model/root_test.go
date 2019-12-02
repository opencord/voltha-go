/*
 * Copyright 2019-present Open Networking Foundation

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
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-lib-go/v2/pkg/mocks"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

var (
	TestRoot_BRANCH *Branch
	TestRoot_HASH   string
)

func newKVClient(storeType string, address string, timeout int) (kvstore.Client, error) {

	log.Infow("kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("unsupported-kv-store")
}

func initTestRoot() (*db.Backend, error) {
	var roCoreFlgs config.ROCoreFlags
	var kvCli kvstore.Client

	clientPort, err := freeport.GetFreePort()
	if err == nil {
		peerPort, err := freeport.GetFreePort()
		if err != nil {
			log.Fatal(err)
			return nil, err
		}
		etcdServer := mocks.StartEtcdServer(mocks.MKConfig("voltha.mock.TestDbModelRoot", clientPort, peerPort, "voltha.lib.mocks.TestDbModelRoot_etcd", "error"))
		if etcdServer == nil {
			log.Fatal("Embedded server failed to start")
			return nil, err
		}
		clientAddr := fmt.Sprintf("localhost:%d", clientPort)

		roCoreFlgs = *config.NewROCoreFlags()

		cli, err := newKVClient("etcd", clientAddr, 5)
		kvCli = cli

		if err != nil {
			etcdServer.Stop()
			log.Fatal("Failed to create an Etcd client")
			return nil, err
		}
		testBackend := db.Backend{
			Client:     kvCli,
			StoreType:  roCoreFlgs.KVStoreType,
			Host:       roCoreFlgs.KVStoreHost,
			Port:       roCoreFlgs.KVStorePort,
			Timeout:    roCoreFlgs.KVStoreTimeout,
			PathPrefix: "service/voltha"}

		if etcdServer != nil {
			etcdServer.Stop()
		}
		return &testBackend, nil
	}
	log.Fatal("Failed to get a free port")
	return nil, err
}

// Exercise root creation code
func TestRoot_01_NewRoot(t *testing.T) {

	testBckEnd, err := initTestRoot()
	assert.NotNil(t, testBckEnd)
	assert.Nil(t, err)

	wantRev_01 := reflect.TypeOf(NonPersistedRevision{})
	wantRev_02 := reflect.TypeOf(PersistedRevision{})

	type args struct {
		initialData interface{}
		kvStore     *db.Backend
	}
	tests := []struct {
		name    string
		args    args
		wantRev interface{}
	}{
		{"TestNewRoot_01", args{&voltha.Voltha{}, nil}, wantRev_01},
		{"TestNewRoot_02", args{&voltha.Voltha{}, testBckEnd}, wantRev_02},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRoot := NewRoot(tt.args.initialData, tt.args.kvStore)
			assert.NotNil(t, gotRoot)
			if gotRoot.RevisionClass != tt.wantRev {
				t.Errorf("newRoot() = %v, want %v", gotRoot.RevisionClass, tt.wantRev)
			}
		})
	}
}

func TestRoot_02_Branches(t *testing.T) {
	_, err := initTestRoot()
	assert.Nil(t, err)
	rTest := NewRoot(&voltha.Voltha{}, nil)

	branchTxId := rTest.MakeTxBranch()
	if len(branchTxId) == 0 {
		t.Errorf("root MakeTxBranch failed\n")
	}
	rTest.FoldTxBranch(branchTxId)
	rTest.DeleteTxBranch(branchTxId)
}

func callBackTest_01(args ...interface{}) interface{} {
	id := args[0]
	log.Infof("Callback for test %d", id)
	return nil
}
func callBackTest_02(args ...interface{}) interface{} {
	id := args[0]
	log.Infof("Callback for test %s", id)
	return nil
}
func TestRoot_03_Callbacks(t *testing.T) {
	_, err := initTestRoot()
	assert.Nil(t, err)
	rTest := NewRoot(&voltha.Voltha{}, nil)

	if hasCallbacks := rTest.hasCallbacks(); hasCallbacks {
		t.Errorf("root hasCallbacks failed")
	}
	if callBacks := rTest.GetCallbacks(); len(callBacks) > 0 {
		t.Errorf("root GetCallbacks failed")
	}
	rTest.AddCallback(callBackTest_01, 1)
	if len(rTest.Callbacks) == 0 {
		t.Errorf("root AddCallback failed")
	}
	if callBacks := rTest.GetCallbacks(); len(callBacks) == 0 {
		t.Errorf("root GetCallbacks failed")
	}
	rTest.AddCallback(callBackTest_02, "test")
	if callBacks := rTest.GetCallbacks(); len(callBacks) != 2 {
		t.Errorf("root GetCallbacks failed")
	}
	rTest.ExecuteCallbacks()
	if hasCallbacks := rTest.hasCallbacks(); hasCallbacks {
		t.Errorf("root ExecuteCallbacks should empty the callback list. Failed")
	}
}

func TestRoot_04_Revisions(t *testing.T) {
	testBckEnd, err := initTestRoot()
	assert.Nil(t, err)
	rTest := NewRoot(&voltha.Voltha{}, testBckEnd)

	node := &node{}
	hash := fmt.Sprintf("%x", md5.Sum([]byte("origin_hash")))
	origin := &NonPersistedRevision{
		Config:   &DataRevision{},
		Children: make(map[string][]Revision),
		Hash:     hash,
		Branch:   &Branch{},
	}
	txid := fmt.Sprintf("%x", md5.Sum([]byte("branch_transaction_id")))

	TestRoot_BRANCH = NewBranch(node, txid, origin, true)

	node.Root = rTest

	Rev := rTest.MakeRevision(TestRoot_BRANCH, &voltha.Voltha{}, origin.Children)
	if Rev.GetNode().GetRoot().KvStore != testBckEnd {
		t.Errorf("root MakeRevision failed")
	}
	TestRoot_BRANCH.Txid = ""
	rTest.makeLatest(TestRoot_BRANCH, Rev, nil)
}
