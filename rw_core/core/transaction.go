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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

// Transaction acquisition results
const (
	UNKNOWN = iota
	SEIZED_BY_SELF
	COMPLETED_BY_OTHER
	ABANDONED_BY_OTHER
	ABANDONED_WATCH_BY_SELF
)

var errorTransactionNotAcquired = status.Error(codes.Canceled, "transaction-not-acquired")
var errorTransactionInvalidId = status.Error(codes.Canceled, "transaction-invalid-id")

const (
	TRANSACTION_COMPLETE = "TRANSACTION-COMPLETE"
)

// Transaction constants used to guarantee the Core processing a request hold on to the transaction until
// it either completes it (either successfully or times out) or the Core itself crashes (e.g. a server failure).
// If a request has a timeout of x seconds then the Core processing the request will renew the transaction lease
// every x/NUM_TXN_RENEWAL_PER_REQUEST seconds. After the Core completes the request it stops renewing the
// transaction and sets the transaction value to TRANSACTION_COMPLETE. If the processing Core crashes then it
// will not renew the transaction causing the KV store to delete the transaction after its renewal period.  The
// Core watching the transaction will then take over.
// Since the MIN_TXN_RENEWAL_INTERVAL_IN_SEC is 3 seconds then for any transaction that completes within 3 seconds
// there won't be a transaction renewal done.
const (
	NUM_TXN_RENEWAL_PER_REQUEST         = 2
	MIN_TXN_RENEWAL_INTERVAL_IN_SEC     = 3
	MIN_TXN_RESERVATION_DURATION_IN_SEC = 5
)

type TransactionContext struct {
	kvClient           kvstore.Client
	kvOperationTimeout int
	owner              string
	txnPrefix          string
}

var ctx *TransactionContext

var txnState = []string{
	"UNKNOWN",
	"SEIZED-BY-SELF",
	"COMPLETED-BY-OTHER",
	"ABANDONED-BY-OTHER",
	"ABANDONED_WATCH_BY_SELF"}

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

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
 * The parameters stored in the context govern the behaviour of all KVTransaction
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
func SetTransactionContext(owner string,
	txnPrefix string,
	kvClient kvstore.Client,
	kvOpTimeout int) error {

	ctx = NewTransactionContext(owner, txnPrefix, kvClient, kvOpTimeout)
	return nil
}

type KVTransaction struct {
	monitorCh chan int
	txnId     string
	txnKey    string
}

/*
 * A KVTransaction constructor
 *
 * :param txnId: The serial number of a voltha request.
 * :return: A KVTransaction instance
 */
func NewKVTransaction(txnId string) *KVTransaction {
	return &KVTransaction{
		txnId:  txnId,
		txnKey: ctx.txnPrefix + txnId}
}

func (c *KVTransaction) Acquired(minDuration int64, ownedByMe ...bool) (bool, error) {
	var acquired bool
	var currOwner string = ""
	var res int

	// Convert milliseconds to seconds, rounding up
	// The reservation TTL is specified in seconds
	durationInSecs := minDuration / 1000
	if remainder := minDuration % 1000; remainder > 0 {
		durationInSecs++
	}
	if durationInSecs < int64(MIN_TXN_RESERVATION_DURATION_IN_SEC) {
		durationInSecs = int64(MIN_TXN_RESERVATION_DURATION_IN_SEC)
	}
	genericRequest := true
	resourceOwned := false
	if len(ownedByMe) > 0 {
		genericRequest = false
		resourceOwned = ownedByMe[0]
	}
	if resourceOwned || genericRequest {
		// Keep the reservation longer that the minDuration (which is really the request timeout) to ensure the
		// transaction key stays in the KV store until after the Core finalize a request timeout condition (which is
		// a success from a request completion perspective).
		if err := c.tryToReserveTxn(durationInSecs * 2); err == nil {
			res = SEIZED_BY_SELF
		} else {
			log.Debugw("watch-other-server",
				log.Fields{"transactionId": c.txnId, "owner": currOwner, "timeout": durationInSecs})
			res = c.Watch(durationInSecs)
		}
	} else {
		res = c.Watch(durationInSecs)
	}
	switch res {
	case SEIZED_BY_SELF, ABANDONED_BY_OTHER:
		acquired = true
	default:
		acquired = false
	}
	log.Debugw("acquire-transaction-status", log.Fields{"transactionId": c.txnId, "acquired": acquired, "result": txnState[res]})
	return acquired, nil
}

func (c *KVTransaction) tryToReserveTxn(durationInSecs int64) error {
	var currOwner string = ""
	var res int
	value, err := ctx.kvClient.Reserve(c.txnKey, ctx.owner, durationInSecs)
	if value != nil {
		if currOwner, err = kvstore.ToString(value); err != nil { // This should never happen
			log.Errorw("unexpected-owner-type", log.Fields{"transactionId": c.txnId, "error": err})
			return err
		}
		if currOwner == ctx.owner {
			log.Debugw("acquired-transaction", log.Fields{"transactionId": c.txnId, "result": txnState[res]})
			// Setup the monitoring channel
			c.monitorCh = make(chan int)
			go c.holdOnToTxnUntilProcessingCompleted(c.txnKey, ctx.owner, durationInSecs)
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "reservation-denied")
}

func (c *KVTransaction) Watch(durationInSecs int64) int {
	var res int

	events := ctx.kvClient.Watch(c.txnKey)
	defer ctx.kvClient.CloseWatch(c.txnKey, events)

	transactionWasAcquiredByOther := false

	//Check whether the transaction was already completed by the other Core before we got here.
	if kvp, _ := ctx.kvClient.Get(c.txnKey, ctx.kvOperationTimeout); kvp != nil {
		transactionWasAcquiredByOther = true
		if val, err := kvstore.ToString(kvp.Value); err == nil {
			if val == TRANSACTION_COMPLETE {
				res = COMPLETED_BY_OTHER
				// Do an immediate delete of the transaction in the KV Store to free up KV Storage faster
				c.Delete()
				return res
			}
		} else {
			// An unexpected value - let's get out of here as something did not go according to plan
			res = ABANDONED_WATCH_BY_SELF
			log.Debugw("cannot-read-transaction-value", log.Fields{"txn": c.txnId, "error": err})
			return res
		}
	}

	for {
		select {
		case event := <-events:
			transactionWasAcquiredByOther = true
			log.Debugw("received-event", log.Fields{"txn": c.txnId, "type": event.EventType})
			if event.EventType == kvstore.DELETE {
				// The other core failed to process the request
				res = ABANDONED_BY_OTHER
			} else if event.EventType == kvstore.PUT {
				key, e1 := kvstore.ToString(event.Key)
				val, e2 := kvstore.ToString(event.Value)
				if e1 == nil && e2 == nil && key == c.txnKey {
					if val == TRANSACTION_COMPLETE {
						res = COMPLETED_BY_OTHER
						// Successful request completion has been detected. Remove the transaction key
						c.Delete()
					} else {
						log.Debugw("Ignoring-PUT-event", log.Fields{"val": val, "key": key})
						continue
					}
				} else {
					log.Warnw("received-unexpected-PUT-event", log.Fields{"txn": c.txnId, "key": key, "ctxKey": c.txnKey})
				}
			}
		case <-time.After(time.Duration(durationInSecs) * time.Second):
			// Corner case: In the case where the Core owning the device dies and before this Core takes ownership of
			// this device there is a window where new requests will end up being watched instead of being processed.
			// Grab the request if the other Core did not create the transaction in the KV store.
			// TODO: Use a peer-monitoring probe to switch over (still relies on the probe frequency). This will
			// guarantee that the peer is actually gone instead of limiting the time the peer can get hold of a
			// request.
			if !transactionWasAcquiredByOther {
				log.Debugw("timeout-no-peer", log.Fields{"txId": c.txnId})
				res = ABANDONED_BY_OTHER
			} else {
				continue
			}
		}
		break
	}
	return res
}

func (c *KVTransaction) Close() error {
	log.Debugw("close", log.Fields{"txn": c.txnId})
	// Stop monitoring the key (applies only when there has been no transaction switch over)
	if c.monitorCh != nil {
		close(c.monitorCh)
		ctx.kvClient.Put(c.txnKey, TRANSACTION_COMPLETE, ctx.kvOperationTimeout, false)
	}
	return nil
}

func (c *KVTransaction) Delete() error {
	log.Debugw("delete", log.Fields{"txn": c.txnId})
	return ctx.kvClient.Delete(c.txnKey, ctx.kvOperationTimeout, false)
}

// holdOnToTxnUntilProcessingCompleted renews the transaction lease until the transaction is complete.  durationInSecs
// is used to calculate the frequency at which the Core processing the transaction renews the lease.  This function
// exits only when the transaction is Closed, i.e completed.
func (c *KVTransaction) holdOnToTxnUntilProcessingCompleted(key string, owner string, durationInSecs int64) {
	log.Debugw("holdOnToTxnUntilProcessingCompleted", log.Fields{"txn": c.txnId})
	renewInterval := durationInSecs / NUM_TXN_RENEWAL_PER_REQUEST
	if renewInterval < MIN_TXN_RENEWAL_INTERVAL_IN_SEC {
		renewInterval = MIN_TXN_RENEWAL_INTERVAL_IN_SEC
	}
forLoop:
	for {
		select {
		case <-c.monitorCh:
			log.Debugw("transaction-renewal-exits", log.Fields{"txn": c.txnId})
			break forLoop
		case <-time.After(time.Duration(renewInterval) * time.Second):
			if err := ctx.kvClient.RenewReservation(c.txnKey); err != nil {
				// Log and continue.
				log.Warnw("transaction-renewal-failed", log.Fields{"txnId": c.txnKey, "error": err})
			}
		}
	}
}
