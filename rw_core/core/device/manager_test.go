/*
 * Copyright 2019-present Open Networking Foundation
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

package device

import (
	"context"
	"strconv"

	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/events"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/phayes/freeport"

	tst "github.com/opencord/voltha-go/rw_core/test"

	"reflect"
	"testing"

	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

// TODO change the function name after first round of review
func (dat *DATest) startCoreNew(ctx context.Context, kmp kafka.InterContainerProxy) {
	cfg := config.NewRWCoreFlags()
	cfg.CoreTopic = "rw_core"
	cfg.EventTopic = "voltha.events"
	cfg.DefaultRequestTimeout = dat.defaultTimeout
	cfg.KVStoreAddress = "127.0.0.1" + ":" + strconv.Itoa(dat.kvClientPort)
	grpcPort, err := freeport.GetFreePort()
	if err != nil {
		logger.Fatal(ctx, "Cannot get a freeport for grpc")
	}
	cfg.GrpcAddress = "127.0.0.1" + ":" + strconv.Itoa(grpcPort)
	client := tst.SetupKVClient(ctx, cfg, dat.coreInstanceID)
	backend := &db.Backend{
		Client:                  client,
		StoreType:               cfg.KVStoreType,
		Address:                 cfg.KVStoreAddress,
		Timeout:                 cfg.KVStoreTimeout,
		LivenessChannelInterval: cfg.LiveProbeInterval / 2}

	dat.kmp = kmp

	endpointMgr := kafka.NewEndpointManager(backend)
	proxy := model.NewDBPath(backend)
	dat.adapterMgr = adapter.NewAdapterManager(ctx, proxy, dat.coreInstanceID, dat.kClient)
	eventProxy := events.NewEventProxy(events.MsgClient(dat.kEventClient), events.MsgTopic(kafka.Topic{Name: cfg.EventTopic}))
	dat.deviceMgr, dat.logicalDeviceMgr = NewManagers(proxy, dat.adapterMgr, dat.kmp, endpointMgr, cfg.CoreTopic, dat.coreInstanceID, cfg.DefaultCoreTimeout, eventProxy, cfg.VolthaStackID)
	dat.adapterMgr.Start(context.Background())
	if err = dat.kmp.Start(ctx); err != nil {
		logger.Fatal(ctx, "Cannot start InterContainerProxy")
	}

	if err := dat.kmp.SubscribeWithDefaultRequestHandler(ctx, kafka.Topic{Name: cfg.CoreTopic}, kafka.OffsetNewest); err != nil {
		logger.Fatalf(ctx, "Cannot add default request handler: %s", err)
	}

}

func TestManager_DownloadImageToDevice(t *testing.T) {
	type args struct {
		ctx     context.Context
		request *voltha.DeviceImageDownloadRequest
	}
	ctx := context.Background()
	dat := newDATest(context.Background())

	controller := gomock.NewController(t)
	mockICProxy := NewMockInterContainerProxy(controller)

	// Set expectations for the mock
	mockICProxy.EXPECT().Start(gomock.Any()).AnyTimes().Return(nil)
	mockICProxy.EXPECT().SubscribeWithDefaultRequestHandler(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	dat.startCoreNew(ctx, mockICProxy)

	agent := dat.createDeviceAgent(t)
	dat.oltAdapter, dat.onuAdapter = tst.CreateAndregisterAdapters(ctx,
		t,
		dat.kClient,
		dat.coreInstanceID,
		dat.oltAdapterName,
		dat.onuAdapterName,
		dat.adapterMgr)

	tests := []struct {
		name    string
		args    args
		want    *voltha.DeviceImageResponse
		wantErr bool
	}{
		{
			name: "success",
			args: args{
				ctx: ctx,
				request: &voltha.DeviceImageDownloadRequest{
					DeviceId: []*common.ID{{
						Id: agent.deviceID,
					}},
					Image: &voltha.Image{
						Name:        "dummy",
						Version:     "dummy-version",
						IsActive:    false,
						IsCommitted: false,
						IsValid:     false,
						Url:         "http://127.0.0.1:2222",
						Vendor:      "dummy",
						Crc32:       0xd1dea096,
					},
					ActivateOnSuccess: true,
					CommitOnSuccess:   true,
				},
			},
			want: &voltha.DeviceImageResponse{
				DeviceImageStates: []*voltha.DeviceImageState{{
					DeviceId: agent.deviceID,
					ImageState: &voltha.ImageState{
						Version:       "dummy-version",
						DownloadState: voltha.ImageState_DOWNLOAD_REQUESTED,
						Reason:        voltha.ImageState_NO_ERROR,
						ImageState:    voltha.ImageState_IMAGE_DOWNLOADING,
					},
				}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "success" {
				chnl := make(chan *kafka.RpcResponse)

				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Download_onu_image",
					gomock.Any(),
					gomock.Any(),
					true,
					agent.deviceID, gomock.Any()).AnyTimes().Return(chnl)

				// Send the expected response to channel from a goroutine
				go func() {
					reply, err := ptypes.MarshalAny(&voltha.DeviceImageResponse{
						DeviceImageStates: []*voltha.DeviceImageState{{
							DeviceId: agent.deviceID,
							ImageState: &voltha.ImageState{
								Version:       "dummy-version",
								DownloadState: voltha.ImageState_DOWNLOAD_REQUESTED,
								Reason:        voltha.ImageState_NO_ERROR,
								ImageState:    voltha.ImageState_IMAGE_DOWNLOADING,
							},
						}},
					})
					if err != nil {
						chnl <- nil
					}

					chnl <- &kafka.RpcResponse{
						MType: kafka.RpcSent,
						Err:   nil,
						Reply: reply,
					}

					chnl <- &kafka.RpcResponse{
						MType: kafka.RpcReply,
						Err:   nil,
						Reply: reply,
					}
				}()
			}
			got, err := dat.deviceMgr.DownloadImageToDevice(tt.args.ctx, tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("DownloadImageToDevice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DownloadImageToDevice() got = %v, want %v", got, tt.want)
			}
		})
	}
}
