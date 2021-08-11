/*
 * Copyright 2020-present Open Networking Foundation

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
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	vgrpc "github.com/opencord/voltha-lib-go/v6/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v6/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/adapter_services"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc"
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
}

func setAndTestAdapterServiceHandler(ctx context.Context, conn *grpc.ClientConn) interface{} {
	svc := adapter_services.NewAdapterServiceClient(conn)
	if h, err := svc.GetHealthStatus(ctx, &empty.Empty{}); err != nil || h.State != voltha.HealthStatus_HEALTHY {
		logger.Debugw(ctx, "connection-not-ready", log.Fields{"error": err, "health": h})
		return nil
	}
	return svc
}

func newAdapterAgent(adapter *voltha.Adapter, onAdapterRestart vgrpc.RestartedHandler, liveProbeInterval time.Duration) *agent {
	return &agent{
		adapter:            adapter,
		onAdapterRestart:   onAdapterRestart,
		adapterAPIEndPoint: adapter.Endpoint,
		liveProbeInterval:  liveProbeInterval,
	}
}

func (aa *agent) start(ctx context.Context) error {
	// Establish grpc connection to Core
	var err error
	if aa.vClient, err = vgrpc.NewClient(aa.adapterAPIEndPoint,
		aa.onAdapterRestart,
		vgrpc.ActivityCheck(true)); err != nil {
		return err
	}

	// Add a liveness communication update
	aa.vClient.SubscribeForLiveness(aa.updateCommunicationTime)

	go aa.vClient.Start(ctx, setAndTestAdapterServiceHandler)
	return nil
}

func (aa *agent) stop(ctx context.Context) {
	// Close the client
	if aa.vClient != nil {
		aa.vClient.Stop(ctx)
	}
}

func (aa *agent) getAdapter(ctx context.Context) *voltha.Adapter {
	aa.adapterLock.RLock()
	defer aa.adapterLock.RUnlock()
	return aa.adapter
}

func (aa *agent) getClient() (adapter_services.AdapterServiceClient, error) {
	client, err := aa.vClient.GetClient()
	if err != nil {
		return nil, err
	}
	c, ok := client.(adapter_services.AdapterServiceClient)
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
	client, err := aa.getClient()
	return err == nil && client != nil
}
