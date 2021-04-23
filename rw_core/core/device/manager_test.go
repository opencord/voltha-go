/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, version 2.0 (the "License");
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
	"reflect"
	"strconv"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/rw_core/config"
	"github.com/opencord/voltha-go/rw_core/core/adapter"
	tst "github.com/opencord/voltha-go/rw_core/test"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/events"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

const (
	version = "dummy-version"
	url     = "http://127.0.0.1:2222/dummy-image"
	vendor  = "dummy"

	numberOfTestDevices = 10
)

func initialiseTest(ctx context.Context, t *testing.T) (*DATest, *MockInterContainerProxy, []*Agent) {
	dat := newDATest(ctx)

	controller := gomock.NewController(t)
	mockICProxy := NewMockInterContainerProxy(controller)

	// Set expectations for the mock
	mockICProxy.EXPECT().Start(gomock.Any()).AnyTimes().Return(nil)
	mockICProxy.EXPECT().SubscribeWithDefaultRequestHandler(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	dat.startCoreWithCustomICProxy(ctx, mockICProxy)

	var agents []*Agent
	for i := 1; i <= numberOfTestDevices; i++ {
		if agent := dat.createDeviceAgent(t); agent != nil {
			agents = append(agents, agent)
		}
	}

	assert.Equal(t, len(agents), numberOfTestDevices)

	dat.oltAdapter, dat.onuAdapter = tst.CreateAndregisterAdapters(ctx,
		t,
		dat.kClient,
		dat.coreInstanceID,
		dat.oltAdapterName,
		dat.onuAdapterName,
		dat.adapterMgr)

	return dat, mockICProxy, agents
}

func (dat *DATest) startCoreWithCustomICProxy(ctx context.Context, kmp kafka.InterContainerProxy) {
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
	dat, mockICProxy, agents := initialiseTest(ctx, t)

	tests := []struct {
		name    string
		args    args
		want    *voltha.DeviceImageResponse
		wantErr bool
	}{
		{
			name: "request-for-single-device",
			args: args{
				ctx:     ctx,
				request: newDeviceImageDownloadRequest(agents[:1]),
			},
			want:    newImageResponse(agents[:1], voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
		{
			name: "request-for-multiple-devices",
			args: args{
				ctx:     ctx,
				request: newDeviceImageDownloadRequest(agents),
			},
			want:    newImageResponse(agents, voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request-for-single-device" {
				chnl := make(chan *kafka.RpcResponse, 10)
				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Download_onu_image",
					gomock.Any(),
					gomock.Any(),
					true,
					gomock.Any(), gomock.Any()).Return(chnl)
				// Send the expected response to channel from a goroutine
				go func() {
					reply := newImageDownloadAdapterResponse(t, agents[0].deviceID, voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR)

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
			} else if tt.name == "request-for-multiple-devices" {
				// Map to store per device kafka response channel
				kafkaRespChans := make(map[string]chan *kafka.RpcResponse)
				for _, id := range tt.args.request.DeviceId {
					// Create a kafka response channel per device
					chnl := make(chan *kafka.RpcResponse)

					// Set expectation for the API invocation
					mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
						"Download_onu_image",
						gomock.Any(),
						gomock.Any(),
						true,
						id.Id, gomock.Any()).Return(chnl)

					kafkaRespChans[id.Id] = chnl
				}

				// Send the expected response to channel from a goroutine
				go func() {
					for _, agent := range agents {
						reply := newImageDownloadAdapterResponse(t, agent.deviceID, voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR)

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcSent,
							Err:   nil,
							Reply: reply,
						}

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcReply,
							Err:   nil,
							Reply: reply,
						}
					}
				}()
			}

			got, err := dat.deviceMgr.DownloadImageToDevice(tt.args.ctx, tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("DownloadImageToDevice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !gotAllSuccess(got, tt.want) {
				t.Errorf("DownloadImageToDevice() got = %v, want = %v", got, tt.want)
			}
		})
	}
}

func TestManager_GetImageStatus(t *testing.T) {
	type args struct {
		ctx     context.Context
		request *voltha.DeviceImageRequest
	}

	ctx := context.Background()
	dat, mockICProxy, agents := initialiseTest(ctx, t)

	tests := []struct {
		name    string
		args    args
		want    *voltha.DeviceImageResponse
		wantErr bool
	}{
		{
			name: "request-for-single-device",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents[:1]),
			},
			want:    newImageResponse(agents[:1], voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
		{
			name: "request-for-multiple-devices",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents),
			},
			want:    newImageResponse(agents, voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request-for-single-device" {
				chnl := make(chan *kafka.RpcResponse, 10)
				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Get_onu_image_status",
					gomock.Any(),
					gomock.Any(),
					true,
					gomock.Any(), gomock.Any()).Return(chnl)
				// Send the expected response to channel from a goroutine
				go func() {
					reply := newImageStatusAdapterResponse(t, agents[:1], voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR)

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
			} else if tt.name == "request-for-multiple-devices" {
				// Map to store per device kafka response channel
				kafkaRespChans := make(map[string]chan *kafka.RpcResponse)
				for _, id := range tt.args.request.DeviceId {
					// Create a kafka response channel per device
					chnl := make(chan *kafka.RpcResponse)

					// Set expectation for the API invocation
					mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
						"Get_onu_image_status",
						gomock.Any(),
						gomock.Any(),
						true,
						id.Id, gomock.Any()).Return(chnl)

					kafkaRespChans[id.Id] = chnl
				}

				// Send the expected response to channel from a goroutine
				go func() {
					for _, agent := range agents {
						reply := newImageStatusAdapterResponse(t, agents, voltha.ImageState_DOWNLOAD_REQUESTED, voltha.ImageState_IMAGE_DOWNLOADING, voltha.ImageState_NO_ERROR)

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcSent,
							Err:   nil,
							Reply: reply,
						}

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcReply,
							Err:   nil,
							Reply: reply,
						}
					}
				}()
			}

			got, err := dat.deviceMgr.GetImageStatus(tt.args.ctx, tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetImageStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !gotAllSuccess(got, tt.want) {
				t.Errorf("GetImageStatus() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_AbortImageUpgradeToDevice(t *testing.T) {

	type args struct {
		ctx     context.Context
		request *voltha.DeviceImageRequest
	}

	ctx := context.Background()
	dat, mockICProxy, agents := initialiseTest(ctx, t)

	tests := []struct {
		name    string
		args    args
		want    *voltha.DeviceImageResponse
		wantErr bool
	}{
		{
			name: "request-for-single-device",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents[:1]),
			},
			want:    newImageResponse(agents[:1], voltha.ImageState_DOWNLOAD_CANCELLED, voltha.ImageState_IMAGE_ACTIVATION_ABORTED, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
		{
			name: "request-for-multiple-devices",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents[:1]),
			},
			want:    newImageResponse(agents, voltha.ImageState_DOWNLOAD_CANCELLED, voltha.ImageState_IMAGE_ACTIVATION_ABORTED, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request-for-single-device" {
				chnl := make(chan *kafka.RpcResponse, 10)
				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Abort_onu_image_upgrade",
					gomock.Any(),
					gomock.Any(),
					true,
					gomock.Any(), gomock.Any()).Return(chnl)
				// Send the expected response to channel from a goroutine
				go func() {
					reply := newImageStatusAdapterResponse(t, agents[:1], voltha.ImageState_DOWNLOAD_CANCELLED, voltha.ImageState_IMAGE_ACTIVATION_ABORTED, voltha.ImageState_NO_ERROR)

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
			} else if tt.name == "request-for-multiple-devices" {
				// Map to store per device kafka response channel
				kafkaRespChans := make(map[string]chan *kafka.RpcResponse)
				for _, id := range tt.args.request.DeviceId {
					// Create a kafka response channel per device
					chnl := make(chan *kafka.RpcResponse)

					// Set expectation for the API invocation
					mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
						"Abort_onu_image_upgrade",
						gomock.Any(),
						gomock.Any(),
						true,
						id.Id, gomock.Any()).Return(chnl)

					kafkaRespChans[id.Id] = chnl
				}

				// Send the expected response to channel from a goroutine
				go func() {
					for _, agent := range agents {
						reply := newImageStatusAdapterResponse(t, agents, voltha.ImageState_DOWNLOAD_CANCELLED, voltha.ImageState_IMAGE_ACTIVATION_ABORTED, voltha.ImageState_NO_ERROR)

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcSent,
							Err:   nil,
							Reply: reply,
						}

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcReply,
							Err:   nil,
							Reply: reply,
						}
					}
				}()
			}
			got, err := dat.deviceMgr.AbortImageUpgradeToDevice(tt.args.ctx, tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("AbortImageUpgradeToDevice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !gotAllSuccess(got, tt.want) {
				t.Errorf("AbortImageUpgradeToDevice() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_ActivateImage(t *testing.T) {
	type args struct {
		ctx     context.Context
		request *voltha.DeviceImageRequest
	}

	ctx := context.Background()
	dat, mockICProxy, agents := initialiseTest(ctx, t)

	tests := []struct {
		name    string
		args    args
		want    *voltha.DeviceImageResponse
		wantErr bool
	}{
		{
			name: "request-for-single-device",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents[:1]),
			},
			want:    newImageResponse(agents[:1], voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_ACTIVATING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
		{
			name: "request-for-multiple-devices",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents),
			},
			want:    newImageResponse(agents, voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_ACTIVATING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request-for-single-device" {
				chnl := make(chan *kafka.RpcResponse, 10)
				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Activate_onu_image",
					gomock.Any(),
					gomock.Any(),
					true,
					gomock.Any(), gomock.Any()).Return(chnl)
				// Send the expected response to channel from a goroutine
				go func() {
					reply := newImageStatusAdapterResponse(t, agents[:1], voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_ACTIVATING, voltha.ImageState_NO_ERROR)

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
			} else if tt.name == "request-for-multiple-devices" {
				// Map to store per device kafka response channel
				kafkaRespChans := make(map[string]chan *kafka.RpcResponse)
				for _, id := range tt.args.request.DeviceId {
					// Create a kafka response channel per device
					chnl := make(chan *kafka.RpcResponse)

					// Set expectation for the API invocation
					mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
						"Activate_onu_image",
						gomock.Any(),
						gomock.Any(),
						true,
						id.Id, gomock.Any()).Return(chnl)

					kafkaRespChans[id.Id] = chnl
				}

				// Send the expected response to channel from a goroutine
				go func() {
					for _, agent := range agents {
						reply := newImageStatusAdapterResponse(t, agents, voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_ACTIVATING, voltha.ImageState_NO_ERROR)

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcSent,
							Err:   nil,
							Reply: reply,
						}

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcReply,
							Err:   nil,
							Reply: reply,
						}
					}
				}()
			}
			got, err := dat.deviceMgr.ActivateImage(tt.args.ctx, tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("ActivateImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !gotAllSuccess(got, tt.want) {
				t.Errorf("ActivateImage() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_CommitImage(t *testing.T) {
	type args struct {
		ctx     context.Context
		request *voltha.DeviceImageRequest
	}

	ctx := context.Background()
	dat, mockICProxy, agents := initialiseTest(ctx, t)

	tests := []struct {
		name    string
		args    args
		want    *voltha.DeviceImageResponse
		wantErr bool
	}{
		{
			name: "request-for-single-device",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents[:1]),
			},
			want:    newImageResponse(agents[:1], voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_COMMITTING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
		{
			name: "request-for-multiple-devices",
			args: args{
				ctx:     ctx,
				request: newDeviceImagedRequest(agents),
			},
			want:    newImageResponse(agents, voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_COMMITTING, voltha.ImageState_NO_ERROR),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request-for-single-device" {
				chnl := make(chan *kafka.RpcResponse, 10)
				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Commit_onu_image",
					gomock.Any(),
					gomock.Any(),
					true,
					gomock.Any(), gomock.Any()).Return(chnl)
				// Send the expected response to channel from a goroutine
				go func() {
					reply := newImageStatusAdapterResponse(t, agents[:1], voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_COMMITTING, voltha.ImageState_NO_ERROR)

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
			} else if tt.name == "request-for-multiple-devices" {
				// Map to store per device kafka response channel
				kafkaRespChans := make(map[string]chan *kafka.RpcResponse)
				for _, id := range tt.args.request.DeviceId {
					// Create a kafka response channel per device
					chnl := make(chan *kafka.RpcResponse)

					// Set expectation for the API invocation
					mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
						"Commit_onu_image",
						gomock.Any(),
						gomock.Any(),
						true,
						id.Id, gomock.Any()).Return(chnl)

					kafkaRespChans[id.Id] = chnl
				}

				// Send the expected response to channel from a goroutine
				go func() {
					for _, agent := range agents {
						reply := newImageStatusAdapterResponse(t, agents, voltha.ImageState_DOWNLOAD_SUCCEEDED, voltha.ImageState_IMAGE_COMMITTING, voltha.ImageState_NO_ERROR)

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcSent,
							Err:   nil,
							Reply: reply,
						}

						kafkaRespChans[agent.deviceID] <- &kafka.RpcResponse{
							MType: kafka.RpcReply,
							Err:   nil,
							Reply: reply,
						}
					}
				}()
			}
			got, err := dat.deviceMgr.CommitImage(tt.args.ctx, tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("CommitImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !gotAllSuccess(got, tt.want) {
				t.Errorf("CommitImage() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_GetOnuImages(t *testing.T) {
	type args struct {
		ctx context.Context
		id  *common.ID
	}

	ctx := context.Background()
	dat, mockICProxy, agents := initialiseTest(ctx, t)

	tests := []struct {
		name    string
		args    args
		want    *voltha.OnuImages
		wantErr bool
	}{
		{
			name: "request-for-single-device",
			args: args{
				ctx: ctx,
				id: &common.ID{
					Id: agents[0].deviceID,
				},
			},
			want: &voltha.OnuImages{
				Items: []*voltha.OnuImage{{
					Version:    version,
					IsCommited: true,
					IsActive:   true,
					IsValid:    true,
				}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request-for-single-device" {
				chnl := make(chan *kafka.RpcResponse, 10)
				// Set expectation for the API invocation
				mockICProxy.EXPECT().InvokeAsyncRPC(gomock.Any(),
					"Get_onu_images",
					gomock.Any(),
					gomock.Any(),
					true,
					gomock.Any(), gomock.Any()).Return(chnl)
				// Send the expected response to channel from a goroutine
				go func() {
					reply := newOnuImagesResponse(t)
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

			got, err := dat.deviceMgr.GetOnuImages(tt.args.ctx, tt.args.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetOnuImages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetOnuImages() got = %v, want %v", got, tt.want)
			}
		})
	}
}

// verify that we got all the wanted response (order not important)
func gotAllSuccess(got, want *voltha.DeviceImageResponse) bool {
	for _, imagestateGot := range got.DeviceImageStates {
		found := false
		for _, imageStateWant := range want.DeviceImageStates {
			if reflect.DeepEqual(imagestateGot, imageStateWant) {
				found = true
			}
		}

		if !found {
			return false
		}
	}

	return true
}

func newDeviceImagedRequest(agents []*Agent) *voltha.DeviceImageRequest {
	imgReq := &voltha.DeviceImageRequest{
		Version:         version,
		CommitOnSuccess: true,
	}

	for _, agent := range agents {
		if agent != nil {
			imgReq.DeviceId = append(imgReq.DeviceId, &common.ID{
				Id: agent.deviceID,
			})
		}
	}

	return imgReq
}

func newDeviceImageDownloadRequest(agents []*Agent) *voltha.DeviceImageDownloadRequest {
	imgDownReq := &voltha.DeviceImageDownloadRequest{
		Image: &voltha.Image{
			Version: version,
			Url:     url,
			Vendor:  vendor,
		},
		ActivateOnSuccess: true,
		CommitOnSuccess:   true,
	}

	for _, agent := range agents {
		if agent != nil {
			imgDownReq.DeviceId = append(imgDownReq.DeviceId, &common.ID{
				Id: agent.deviceID,
			})
		}
	}

	return imgDownReq
}

func newImageResponse(agents []*Agent,
	downloadState voltha.ImageState_ImageDownloadState,
	imageSate voltha.ImageState_ImageActivationState,
	reason voltha.ImageState_ImageFailureReason) *voltha.DeviceImageResponse {
	response := &voltha.DeviceImageResponse{}

	for _, agent := range agents {
		response.DeviceImageStates = append(response.DeviceImageStates, &voltha.DeviceImageState{
			DeviceId: agent.deviceID,
			ImageState: &voltha.ImageState{
				Version:       version,
				DownloadState: downloadState,
				Reason:        reason,
				ImageState:    imageSate,
			},
		})
	}

	return response
}

func newImageDownloadAdapterResponse(t *testing.T,
	deviceID string,
	downloadState voltha.ImageState_ImageDownloadState,
	imageSate voltha.ImageState_ImageActivationState,
	reason voltha.ImageState_ImageFailureReason) *any.Any {
	reply, err := ptypes.MarshalAny(&voltha.DeviceImageResponse{
		DeviceImageStates: []*voltha.DeviceImageState{{
			DeviceId: deviceID,
			ImageState: &voltha.ImageState{
				Version:       version,
				DownloadState: downloadState,
				Reason:        reason,
				ImageState:    imageSate,
			},
		}},
	})
	assert.Nil(t, err)
	return reply
}

func newImageStatusAdapterResponse(t *testing.T,
	agents []*Agent,
	downloadState voltha.ImageState_ImageDownloadState,
	imageSate voltha.ImageState_ImageActivationState,
	reason voltha.ImageState_ImageFailureReason) *any.Any {
	imgResponse := &voltha.DeviceImageResponse{}
	for _, agent := range agents {
		imgResponse.DeviceImageStates = append(imgResponse.DeviceImageStates, &voltha.DeviceImageState{
			DeviceId: agent.deviceID,
			ImageState: &voltha.ImageState{
				Version:       version,
				DownloadState: downloadState,
				Reason:        reason,
				ImageState:    imageSate,
			},
		})
	}

	reply, err := ptypes.MarshalAny(imgResponse)
	assert.Nil(t, err)
	return reply
}

func newOnuImagesResponse(t *testing.T) *any.Any {
	onuImages := &voltha.OnuImages{
		Items: []*voltha.OnuImage{{
			Version:    version,
			IsCommited: true,
			IsActive:   true,
			IsValid:    true,
		}},
	}

	reply, err := ptypes.MarshalAny(onuImages)
	assert.Nil(t, err)
	return reply
}
