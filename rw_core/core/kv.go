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

package core

import (
	"context"
	"errors"
	"time"

	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-lib-go/v4/pkg/probe"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newKVClient(ctx context.Context, storeType string, address string, timeout time.Duration) (kvstore.Client, error) {
	logger.Infow(ctx, "kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(ctx, address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(ctx, address, timeout, log.FatalLevel)
	}
	return nil, errors.New("unsupported-kv-store")
}

func stopKVClient(ctx context.Context, kvClient kvstore.Client) {
	// Release all reservations
	if err := kvClient.ReleaseAllReservations(ctx); err != nil {
		logger.Infow(ctx, "fail-to-release-all-reservations", log.Fields{"error": err})
	}
	// Close the DB connection
	kvClient.Close(ctx)
}

// waitUntilKVStoreReachableOrMaxTries will wait until it can connect to a KV store or until maxtries has been reached
func waitUntilKVStoreReachableOrMaxTries(ctx context.Context, kvClient kvstore.Client, maxRetries int, retryInterval time.Duration) error {
	logger.Infow(ctx, "verifying-KV-store-connectivity", log.Fields{"retries": maxRetries, "retryInterval": retryInterval})
	count := 0
	for {
		if !kvClient.IsConnectionUp(ctx) {
			logger.Info(ctx, "KV-store-unreachable")
			if maxRetries != -1 {
				if count >= maxRetries {
					return status.Error(codes.Unavailable, "kv store unreachable")
				}
			}
			count++

			//	Take a nap before retrying
			select {
			case <-ctx.Done():
				//ctx canceled
				return ctx.Err()
			case <-time.After(retryInterval):
			}
			logger.Infow(ctx, "retry-KV-store-connectivity", log.Fields{"retryCount": count, "maxRetries": maxRetries, "retryInterval": retryInterval})
		} else {
			break
		}
	}
	probe.UpdateStatusFromContext(ctx, "kv-store", probe.ServiceStatusRunning)
	logger.Info(ctx, "KV-store-reachable")
	return nil
}

/*
 * Thread to monitor kvstore Liveness (connection status)
 *
 * This function constantly monitors Liveness State of kvstore as reported
 * periodically by backend and updates the Status of kv-store service registered
 * with rw_core probe.
 *
 * If no liveness event has been seen within a timeout, then the thread will
 * perform a "liveness" check attempt, which will in turn trigger a liveness event on
 * the liveness channel, true or false depending on whether the attempt succeeded.
 *
 * The gRPC server in turn monitors the state of the readiness probe and will
 * start issuing UNAVAILABLE response while the probe is not ready.
 */
func monitorKVStoreLiveness(ctx context.Context, backend *db.Backend, liveProbeInterval, notLiveProbeInterval time.Duration) {
	logger.Info(ctx, "start-monitoring-kvstore-liveness")

	// Instruct backend to create Liveness channel for transporting state updates
	livenessChannel := backend.EnableLivenessChannel(ctx)

	logger.Debug(ctx, "enabled-kvstore-liveness-channel")

	// Default state for kvstore is alive for rw_core
	timeout := liveProbeInterval
loop:
	for {
		timeoutTimer := time.NewTimer(timeout)
		select {

		case liveness := <-livenessChannel:
			logger.Debugw(ctx, "received-liveness-change-notification", log.Fields{"liveness": liveness})

			if !liveness {
				probe.UpdateStatusFromContext(ctx, "kv-store", probe.ServiceStatusNotReady)
				logger.Info(ctx, "kvstore-set-server-notready")

				timeout = notLiveProbeInterval

			} else {
				probe.UpdateStatusFromContext(ctx, "kv-store", probe.ServiceStatusRunning)
				logger.Info(ctx, "kvstore-set-server-ready")

				timeout = liveProbeInterval
			}

			if !timeoutTimer.Stop() {
				<-timeoutTimer.C
			}

		case <-ctx.Done():
			break loop

		case <-timeoutTimer.C:
			logger.Info(ctx, "kvstore-perform-liveness-check-on-timeout")

			// Trigger Liveness check if no liveness update received within the timeout period.
			// The Liveness check will push Live state to same channel which this routine is
			// reading and processing. This, do it asynchronously to avoid blocking for
			// backend response and avoid any possibility of deadlock
			go backend.PerformLivenessCheck(ctx)
		}
	}
}
