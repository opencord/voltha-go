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
 * To ensure the key is removed despite possible standby core failures, a KV operation is
 * scheduled in the background on both cores to delete the key well after the transaction is
 * completed. The value of TransactionContext parameter timeToDeleteCompletedKeys should be
 * long enough, on the order of many seconds, to ensure the standby sees the transaction
 * closure. The aim is to prevent a growing list of TRANSACTION_COMPLETE values from loading
 * the KV store.
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
	STOPPED_WATCHING_KEY
	STOPPED_WAITING_FOR_KEY
)

var errorTransactionNotAcquired = status.Error(codes.Canceled, "transaction-not-acquired")

const (
	TRANSACTION_COMPLETE = "TRANSACTION-COMPLETE"
)

type TransactionContext struct {
	kvClient                  kvstore.Client
	kvOperationTimeout        int
	monitorLoopTime           int64
	owner                     string
	timeToDeleteCompletedKeys int
	txnPrefix                 string
}

var ctx *TransactionContext

var txnState = []string{
	"UNKNOWN",
	"SEIZED-BY-SELF",
	"COMPLETED-BY-OTHER",
	"ABANDONED-BY-OTHER",
	"STOPPED-WATCHING-KEY",
	"STOPPED-WAITING-FOR-KEY"}

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

func NewTransactionContext(
	owner string,
	txnPrefix string,
	kvClient kvstore.Client,
	kvOpTimeout int,
	keyDeleteTime int,
	monLoopTime int64) *TransactionContext {

	return &TransactionContext{
		owner:                     owner,
		txnPrefix:                 txnPrefix,
		kvClient:                  kvClient,
		kvOperationTimeout:        kvOpTimeout,
		monitorLoopTime:           monLoopTime,
		timeToDeleteCompletedKeys: keyDeleteTime}
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
 * :param keyDeleteTime: The time (seconds) to wait, in the background, before deleting
 *                       a TRANSACTION_COMPLETE key
 * :param monLoopTime: The time in milliseconds that the monitor sleeps between
 *                     checks for the existence of the transaction key
 */
func SetTransactionContext(owner string,
	txnPrefix string,
	kvClient kvstore.Client,
	kvOpTimeout int,
	keyDeleteTime int,
	monLoopTime int64) error {

	ctx = NewTransactionContext(owner, txnPrefix, kvClient, kvOpTimeout, keyDeleteTime, monLoopTime)
	return nil
}

type KVTransaction struct {
	ch     chan int
	txnId  string
	txnKey string
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

/*
 * This function returns a boolean indicating whether or not the caller should process
 * the request. True is returned in one of two cases:
 * (1) The current core successfully reserved the request's serial number with the KV store
 * (2) The current core failed in its reservation attempt but observed that the serving core
 *     has abandoned processing the request
 *
 * :param duration: The duration of the reservation in milliseconds
 * :return: true - reservation acquired, process the request
 *          false - reservation not acquired, request being processed by another core
 */
func (c *KVTransaction) Acquired(duration int64) bool {
	var acquired bool
	var currOwner string = ""
	var res int

	// Convert milliseconds to seconds, rounding up
	// The reservation TTL is specified in seconds
	durationInSecs := duration / 1000
	if remainder := duration % 1000; remainder > 0 {
		durationInSecs++
	}
	value, err := ctx.kvClient.Reserve(c.txnKey, ctx.owner, durationInSecs)

	// If the reservation failed, do we simply abort or drop into watch mode anyway?
	// Setting value to nil leads to watch mode
	if value != nil {
		if currOwner, err = kvstore.ToString(value); err != nil {
			log.Errorw("unexpected-owner-type", log.Fields{"txn": c.txnId})
			value = nil
		}
	}
	if err == nil && value != nil && currOwner == ctx.owner {
		// Process the request immediately
		res = SEIZED_BY_SELF
	} else {
		// Another core instance has reserved the request
		// Watch for reservation expiry or successful request completion
		log.Debugw("watch-other-server",
			log.Fields{"txn": c.txnId, "owner": currOwner, "timeout": duration})

		res = c.Watch(duration)
	}
	// Clean-up: delete the transaction key after a long delay
	go c.deleteTransactionKey()

	log.Debugw("acquire-transaction", log.Fields{"txn": c.txnId, "result": txnState[res]})
	switch res {
	case SEIZED_BY_SELF, ABANDONED_BY_OTHER, STOPPED_WATCHING_KEY:
		acquired = true
	default:
		acquired = false
	}
	// Ensure the request watcher does not reply before the request server
	if !acquired {
		log.Debugw("Transaction was not ACQUIRED", log.Fields{"txn": c.txnId})
	}
	return acquired
}

/*
 * This function monitors the progress of a request that's been reserved by another
 * Voltha core.
 *
 * :param duration: The duration of the reservation in milliseconds
 * :return: true - reservation abandoned by the other core, process the request
 *          false - reservation not owned, request being processed by another core
 */
func (c *KVTransaction) Monitor(duration int64) bool {
	var acquired bool
	var res int

	// Convert milliseconds to seconds, rounding up
	// The reservation TTL is specified in seconds
	durationInSecs := duration / 1000
	if remainder := duration % 1000; remainder > 0 {
		durationInSecs++
	}

	res = c.Watch(duration)

	// Clean-up: delete the transaction key after a long delay
	go c.deleteTransactionKey()

	log.Debugw("monitor-transaction", log.Fields{"txn": c.txnId, "result": txnState[res]})
	switch res {
	case ABANDONED_BY_OTHER, STOPPED_WATCHING_KEY, STOPPED_WAITING_FOR_KEY:
		acquired = true
	default:
		acquired = false
	}
	// Ensure the request watcher does not reply before the request server
	if !acquired {
		log.Debugw("Transaction was not acquired", log.Fields{"txn": c.txnId})
	}
	return acquired
}

// duration in milliseconds
func (c *KVTransaction) Watch(duration int64) int {
	var res int

	events := ctx.kvClient.Watch(c.txnKey)

	for {
		select {
		// Add a timeout here in case we miss an event from the KV
		case <-time.After(time.Duration(duration) * time.Millisecond):
			// In case of missing events, let's check the transaction key
			kvp, err := ctx.kvClient.Get(c.txnKey, ctx.kvOperationTimeout, false)
			if err == nil && kvp == nil {
				log.Debugw("missed-delete-event", log.Fields{"txn": c.txnId})
				res = ABANDONED_BY_OTHER
			} else if val, err := kvstore.ToString(kvp.Value); err == nil && val == TRANSACTION_COMPLETE {
				log.Debugw("missed-put-event", log.Fields{"txn": c.txnId, "value": val})
				res = COMPLETED_BY_OTHER
			} else {
				log.Debugw("watch-timeout", log.Fields{"txn": c.txnId, "value": val})
				res = STOPPED_WATCHING_KEY
			}

		case event := <-events:
			log.Debugw("received-event", log.Fields{"txn": c.txnId, "type": event.EventType})
			if event.EventType == kvstore.DELETE {
				// The other core failed to process the request
				res = ABANDONED_BY_OTHER
			} else if event.EventType == kvstore.PUT {
				key, e1 := kvstore.ToString(event.Key)
				val, e2 := kvstore.ToString(event.Value)
				if e1 == nil && key == c.txnKey && e2 == nil {
					if val == TRANSACTION_COMPLETE {
						res = COMPLETED_BY_OTHER
						// Successful request completion has been detected
						// Remove the transaction key
						c.Delete()
					} else {
						log.Debugf("Ignoring reservation PUT event with val %v for key %v",
							val, key)
						continue
					}
				}
			}
		}
		break
	}
	return res
}

func (c *KVTransaction) deleteTransactionKey() {
	log.Debugw("schedule-key-deletion", log.Fields{"txnId": c.txnId, "txnkey": c.txnKey})
	time.Sleep(time.Duration(ctx.timeToDeleteCompletedKeys) * time.Second)
	log.Debugw("background-key-deletion", log.Fields{"txn": c.txnId, "txnkey": c.txnKey})
	ctx.kvClient.Delete(c.txnKey, ctx.kvOperationTimeout, false)
}

func (c *KVTransaction) Close() error {
	log.Debugw("close", log.Fields{"txn": c.txnId})
	return ctx.kvClient.Put(c.txnKey, TRANSACTION_COMPLETE, ctx.kvOperationTimeout, false)
}

func (c *KVTransaction) Delete() error {
	log.Debugw("delete", log.Fields{"txn": c.txnId})
	err := ctx.kvClient.Delete(c.txnKey, ctx.kvOperationTimeout, false)
	return err
}
