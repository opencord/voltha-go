/*
 * Copyright 2018-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/*
 * Two voltha cores receive the same request; each tries to acquire ownership of the request
 * by writing its identifier (e.g. container name or pod name) to the transaction key named
 * after the serial number of the request. The core that loses the race for acquisition
 * monitors the progress of the core actually serving the request by watching for changes
 * in the value of the transaction key. Once the request is complete, the
 * serving core closes the transaction by invoking the KVTransaction's Close method, which
 * replaces the value of the transaction (i.e. serial number) key with the string
 * TRANSACTION_COMPLETE. The standby core observes this update, stops watching the transaction,
 * and then deletes the transaction key.
 *
 */

package core

import (
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
)

// Transaction acquisition results
const (
	UNKNOWN = iota
	SeizedBySelf
	CompletedByOther
	AbandonedByOther
	AbandonedWatchBySelf
)

// Transaction constant
const (
	TransactionComplete = "TRANSACTION-COMPLETE"
)

// Transaction constants used to guarantee the Core processing a request hold on to the transaction until
// it either completes it (either successfully or times out) or the Core itself crashes (
// e.g. a server failure).
// If a request has a timeout of x seconds then the Core processing the request will renew the transaction lease
// every x/NUM_TXN_RENEWAL_PER_REQUEST seconds. After the Core completes the request it stops renewing the
// transaction and sets the transaction value to TRANSACTION_COMPLETE. If the processing Core crashes then it
// will not renew the transaction causing the KV store to delete the transaction after its renewal period.  The
// Core watching the transaction will then take over.
// Since the MIN_TXN_RENEWAL_INTERVAL_IN_SEC is 3 seconds then for any transaction that completes within 3 seconds
// there won't be a transaction renewal done.
const (
	NumTxnRenewalPerRequest        = 2
	MinTxnRenewalIntervalInSec     = 3
	MinTxnReservationDurationInSec = 5
)

// TransactionContext represent transaction context attributes
type TransactionContext struct {
	kvClient           kvstore.Client
	kvOperationTimeout int
	owner              string
	txnPrefix          string
}

var ctx *TransactionContext

// NewTransactionContext creates transaction context instance
func NewTransactionContext(
	owner string,
	txnPrefix string,
	kvClient kvstore.Client,
	kvOpTimeout int) *TransactionContext {

	return &TransactionContext{
		owner:              owner,
		txnPrefix:          txnPrefix,
		kvClient:           kvClient,
		kvOperationTimeout: kvOpTimeout}
}

/*
 * Before instantiating a KVTransaction, a TransactionContext must be created.
 * The parameters stored in the context govern the behavior of all KVTransaction
 * instances.
 *
 * :param owner: The owner (i.e. voltha core name) of a transaction
 * :param txnPrefix: The key prefix under which all transaction IDs, or serial numbers,
 *                   will be created (e.g. "service/voltha/transactions")
 * :param kvClient: The client API used for all interactions with the KV store. Currently
 *                  only the etcd client is supported.
 * :param: kvOpTimeout: The maximum time, in seconds, to be taken by any KV operation
 *                      used by this package
 */

// SetTransactionContext creates new transaction context
func SetTransactionContext(owner string,
	txnPrefix string,
	kvClient kvstore.Client,
	kvOpTimeout int) error {

	ctx = NewTransactionContext(owner, txnPrefix, kvClient, kvOpTimeout)
	return nil
}

// KVTransaction represent KV transaction attributes
type KVTransaction struct {
	txnID  string
	txnKey string
}

/*
 * A KVTransaction constructor
 *
 * :param txnId: The serial number of a voltha request.
 * :return: A KVTransaction instance
 */

// NewKVTransaction creates KV transaction instance
func NewKVTransaction(txnID string) *KVTransaction {
	return &KVTransaction{
		txnID:  txnID,
		txnKey: ctx.txnPrefix + txnID}
}
