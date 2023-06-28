/*
 * Copyright 2020-2023 Open Networking Foundation (ONF) and the ONF Contributors

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

package adapter

import (
	"context"
	"errors"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	"sync"
	"time"
)

// agent represents adapter agent
type agent struct {
	adapter            *voltha.Adapter
	lock               sync.RWMutex
	adapterAPIEndPoint string
	vClient            *vgrpc.Client
	adapterLock        sync.RWMutex
	onAdapterRestart   vgrpc.RestartedHandler
	liveProbeInterval  time.Duration
	coreEndpoint       string
	maxRetries         uint
	perRPCRetryTimeout time.Duration
}

func getAdapterServiceClientHandler(ctx context.Context, conn *grpc.ClientConn) interface{} {
	if conn == nil {
		return nil
	}
	return adapter_service.NewAdapterServiceClient(conn)
}

func newAdapterAgent(coreEndpoint string, adapter *voltha.Adapter, onAdapterRestart vgrpc.RestartedHandler, liveProbeInterval time.Duration, maxRetries uint, perRPCRetryTimeout time.Duration) *agent {
	return &agent{
		adapter:            adapter,
		onAdapterRestart:   onAdapterRestart,
		adapterAPIEndPoint: adapter.Endpoint,
		liveProbeInterval:  liveProbeInterval,
		coreEndpoint:       coreEndpoint,
		maxRetries:         maxRetries,
		perRPCRetryTimeout: perRPCRetryTimeout,
	}
}

func (aa *agent) start(ctx context.Context) error {
	// Establish grpc connection to Core
	var err error
	if aa.vClient, err = vgrpc.NewClient(
		aa.coreEndpoint,
		aa.adapterAPIEndPoint,
		"adapter_service.AdapterService",
		aa.onAdapterRestart); err != nil {
		return err
	}

	// Add a liveness communication update
	aa.vClient.SubscribeForLiveness(aa.updateCommunicationTime)
	// the backoff function sets the wait time bw each grpc retries, if not set it will take the deafault value of 50ms which is too low, the jitter sets the rpc retry wait time to be in a range of[PerRPCRetryTimeout-0.2, PerRPCRetryTimeout+0.2]
	backoffCtxOption := grpc_retry.WithBackoff(grpc_retry.BackoffLinearWithJitter(aa.perRPCRetryTimeout, 0.2))
	retryCodes := []codes.Code{
		codes.Unavailable,      // server is currently unavailable
		codes.DeadlineExceeded, // deadline for the operation was exceeded
	}
	grpcRetryOptions := grpc_retry.UnaryClientInterceptor(grpc_retry.WithMax(aa.maxRetries), grpc_retry.WithPerRetryTimeout(aa.perRPCRetryTimeout), grpc_retry.WithCodes(retryCodes...), backoffCtxOption)
	logger.Debug(ctx, "Configuration values", log.Fields{"RETRY": aa.maxRetries, "TIMEOUT": aa.perRPCRetryTimeout})
	go aa.vClient.Start(ctx, getAdapterServiceClientHandler, grpcRetryOptions)
	return nil
}

func (aa *agent) stop(ctx context.Context) {
	// Close the client
	logger.Infow(ctx, "stopping-adapter-agent", log.Fields{"adapter": aa.adapter})
	if aa.vClient != nil {
		aa.vClient.Stop(ctx)
	}
}

func (aa *agent) getAdapter(ctx context.Context) *voltha.Adapter {
	aa.adapterLock.RLock()
	defer aa.adapterLock.RUnlock()
	return aa.adapter
}

func (aa *agent) getClient() (adapter_service.AdapterServiceClient, error) {
	client, err := aa.vClient.GetClient()
	if err != nil {
		return nil, err
	}

	c, ok := client.(adapter_service.AdapterServiceClient)
	if ok {
		return c, nil
	}
	return nil, errors.New("invalid client returned")
}

func (aa *agent) resetConnection(ctx context.Context) {
	if aa.vClient != nil {
		aa.vClient.Reset(ctx)
	}
}

// updateCommunicationTime updates the message to the specified time.
// No attempt is made to save the time to the db, so only recent times are guaranteed to be accurate.
func (aa *agent) updateCommunicationTime(new time.Time) {
	// only update if new time is not in the future, and either the old time is invalid or new time > old time
	aa.lock.Lock()
	defer aa.lock.Unlock()
	timestamp := time.Unix(aa.adapter.LastCommunication, 0)
	if !new.After(time.Now()) && new.After(timestamp) {
		timestamp = new
		aa.adapter.LastCommunication = timestamp.Unix()
	}
}

func (aa *agent) IsConnectionUp() bool {
	_, err := aa.getClient()
	return err == nil
}
