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
	"context"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Transaction acquisition results
const (
	UNKNOWN = iota
	SeizedBySelf
	CompletedByOther
	AbandonedByOther
	AbandonedWatchBySelf
)

var errorTransactionNotAcquired = status.Error(codes.Canceled, "transaction-not-acquired")

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

var txnState = []string{
	"UNKNOWN",
	"SEIZED-BY-SELF",
	"COMPLETED-BY-OTHER",
	"ABANDONED-BY-OTHER",
	"ABANDONED_WATCH_BY_SELF"}

func init() {
	_, err := log.AddPackage(log.JSON, log.DebugLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

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
	monitorCh chan int
	txnID     string
	txnKey    string
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

/*
 * Acquired is invoked by a Core, upon reception of a request, to reserve the transaction key in the KV store. The
 * request may be resource specific (i.e will include an ID for that resource) or may be generic (i.e. list a set of
 * resources). If the request is resource specific then this function should be invoked with the ownedByMe flag to
 * indicate whether this Core owns this resource.  In the case where this Core owns this resource or it is a generic
 * request then we will proceed to reserve the transaction key in the KV store for a minimum time specified by the
 * minDuration param.  If the reservation request fails (i.e. the other Core got the reservation before this one - this
 * can happen only for generic request) then the Core will start to watch for changes to the key to determine
 * whether the other Core completed the transaction successfully or the Core just died.  If the Core does not own the
 * resource then we will proceed to watch the transaction key.
 *
 * :param minDuration: minimum time to reserve the transaction key in the KV store
 * :param ownedByMe: specify whether the request is about a resource owned or not. If it's absent then this is a
 * generic request that has no specific resource ID (e.g. list)
 *
 * :return: A boolean specifying whether the resource was acquired. An error is return in case this function is invoked
 * for a resource that is nonexistent.
 */

// Acquired aquires transaction status
func (c *KVTransaction) Acquired(ctx context.Context, minDuration int64, ownedByMe ...bool) (bool, error) {
	var acquired bool
	var currOwner string
	var res int

	// Convert milliseconds to seconds, rounding up
	// The reservation TTL is specified in seconds
	durationInSecs := minDuration / 1000
	if remainder := minDuration % 1000; remainder > 0 {
		durationInSecs++
	}
	if durationInSecs < int64(MinTxnReservationDurationInSec) {
		durationInSecs = int64(MinTxnReservationDurationInSec)
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
		if err := c.tryToReserveTxn(ctx, durationInSecs*2); err == nil {
			res = SeizedBySelf
		} else {
			log.Debugw("watch-other-server",
				log.Fields{"transactionId": c.txnID, "owner": currOwner, "timeout": durationInSecs})
			res = c.Watch(ctx, durationInSecs)
		}
	} else {
		res = c.Watch(ctx, durationInSecs)
	}
	switch res {
	case SeizedBySelf, AbandonedByOther:
		acquired = true
	default:
		acquired = false
	}
	log.Debugw("acquire-transaction-status", log.Fields{"transactionId": c.txnID, "acquired": acquired, "result": txnState[res]})
	return acquired, nil
}

func (c *KVTransaction) tryToReserveTxn(ctxt context.Context, durationInSecs int64) error {
	var currOwner string
	var res int
	var err error
	value, _ := ctx.kvClient.Reserve(ctxt, c.txnKey, ctx.owner, durationInSecs)
	if value != nil {
		if currOwner, err = kvstore.ToString(value); err != nil { // This should never happen
			log.Errorw("unexpected-owner-type", log.Fields{"transactionId": c.txnID, "error": err})
			return err
		}
		if currOwner == ctx.owner {
			log.Debugw("acquired-transaction", log.Fields{"transactionId": c.txnID, "result": txnState[res]})
			// Setup the monitoring channel
			c.monitorCh = make(chan int)
			go c.holdOnToTxnUntilProcessingCompleted(ctxt, c.txnKey, ctx.owner, durationInSecs)
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "reservation-denied")
}

// Watch watches transaction
func (c *KVTransaction) Watch(ctxt context.Context, durationInSecs int64) int {
	var res int
	events := ctx.kvClient.Watch(ctxt, c.txnKey, false)
	defer ctx.kvClient.CloseWatch(c.txnKey, events)

	transactionWasAcquiredByOther := false

	//Check whether the transaction was already completed by the other Core before we got here.
	if kvp, _ := ctx.kvClient.Get(ctxt, c.txnKey); kvp != nil {
		transactionWasAcquiredByOther = true
		if val, err := kvstore.ToString(kvp.Value); err == nil {
			if val == TransactionComplete {
				res = CompletedByOther
				// Do an immediate delete of the transaction in the KV Store to free up KV Storage faster
				err = c.Delete(ctxt)
				if err != nil {
					log.Errorw("unable-to-delete-the-transaction", log.Fields{"error": err})
				}
				return res
			}
		} else {
			// An unexpected value - let's get out of here as something did not go according to plan
			res = AbandonedWatchBySelf
			log.Debugw("cannot-read-transaction-value", log.Fields{"txn": c.txnID, "error": err})
			return res
		}
	}

	for {
		select {
		case event := <-events:
			transactionWasAcquiredByOther = true
			log.Debugw("received-event", log.Fields{"txn": c.txnID, "type": event.EventType})
			if event.EventType == kvstore.DELETE {
				// The other core failed to process the request
				res = AbandonedByOther
			} else if event.EventType == kvstore.PUT {
				key, e1 := kvstore.ToString(event.Key)
				val, e2 := kvstore.ToString(event.Value)
				if e1 == nil && e2 == nil && key == c.txnKey {
					if val == TransactionComplete {
						res = CompletedByOther
						// Successful request completion has been detected. Remove the transaction key
						err := c.Delete(ctxt)
						if err != nil {
							log.Errorw("unable-to-delete-the-transaction", log.Fields{"error": err})
						}
					} else {
						log.Debugw("Ignoring-PUT-event", log.Fields{"val": val, "key": key})
						continue
					}
				} else {
					log.Warnw("received-unexpected-PUT-event", log.Fields{"txn": c.txnID, "key": key, "ctxKey": c.txnKey})
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
				log.Debugw("timeout-no-peer", log.Fields{"txId": c.txnID})
				res = AbandonedByOther
			} else {
				continue
			}
		}
		break
	}
	return res
}

// Close closes transaction
func (c *KVTransaction) Close(ctxt context.Context) error {
	log.Debugw("close", log.Fields{"txn": c.txnID})
	// Stop monitoring the key (applies only when there has been no transaction switch over)
	if c.monitorCh != nil {
		close(c.monitorCh)
		err := ctx.kvClient.Put(ctxt, c.txnKey, TransactionComplete)

		if err != nil {
			log.Errorw("unable-to-write-a-key-value-pair-to-the-KV-store", log.Fields{"error": err})
		}
	}
	return nil
}

// Delete deletes transaction
func (c *KVTransaction) Delete(ctxt context.Context) error {
	log.Debugw("delete", log.Fields{"txn": c.txnID})
	return ctx.kvClient.Delete(ctxt, c.txnKey)
}

// holdOnToTxnUntilProcessingCompleted renews the transaction lease until the transaction is complete.  durationInSecs
// is used to calculate the frequency at which the Core processing the transaction renews the lease.  This function
// exits only when the transaction is Closed, i.e completed.
func (c *KVTransaction) holdOnToTxnUntilProcessingCompleted(ctxt context.Context, key string, owner string, durationInSecs int64) {
	log.Debugw("holdOnToTxnUntilProcessingCompleted", log.Fields{"txn": c.txnID})
	renewInterval := durationInSecs / NumTxnRenewalPerRequest
	if renewInterval < MinTxnRenewalIntervalInSec {
		renewInterval = MinTxnRenewalIntervalInSec
	}
forLoop:
	for {
		select {
		case <-c.monitorCh:
			log.Debugw("transaction-renewal-exits", log.Fields{"txn": c.txnID})
			break forLoop
		case <-time.After(time.Duration(renewInterval) * time.Second):
			if err := ctx.kvClient.RenewReservation(ctxt, c.txnKey); err != nil {
				// Log and continue.
				log.Warnw("transaction-renewal-failed", log.Fields{"txnId": c.txnKey, "error": err})
			}
		}
	}
}
