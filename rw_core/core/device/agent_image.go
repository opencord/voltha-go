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

package device

import (
	"context"

	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-protos/v4/go/common"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) downloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "download-image", log.Fields{"device-id": agent.deviceID})

	if agent.device.Root {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, is an OLT. Image update "+
			"not supported by VOLTHA. Use Device Manager or other means", agent.deviceID)
	}
	if !agent.proceedWithRequest() {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device reconciling or deletion is in progress.",
			agent.deviceID)
	}

	device := agent.cloneDeviceWithoutLock()
	if device.ImageDownloads != nil {
		for _, image := range device.ImageDownloads {
			if image.DownloadState == voltha.ImageDownload_DOWNLOAD_REQUESTED {
				return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, already downloading image:%s",
					agent.deviceID, image.Name)
			}
		}
	}

	// Save the image
	clonedImg := proto.Clone(img).(*voltha.ImageDownload)
	clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
	cloned := agent.cloneDeviceWithoutLock()
	_, index, err := getImage(img, device)
	if err != nil {
		cloned.ImageDownloads = append(device.ImageDownloads, clonedImg)
	} else {
		cloned.ImageDownloads[index] = clonedImg
	}

	cloned.AdminState = voltha.AdminState_DOWNLOADING_IMAGE
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	ch, err := agent.adapterProxy.DownloadImage(subCtx, cloned, clonedImg)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "downloadImage", ch, agent.onImageSuccess, agent.onImageFailure)

	return &common.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

// getImage is a helper method to figure out if an image is already registered
func getImage(img *voltha.ImageDownload, device *voltha.Device) (*voltha.ImageDownload, int, error) {
	for pos, image := range device.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return image, pos, nil
		}
	}
	return nil, -1, status.Errorf(codes.FailedPrecondition, "device-id:%s, image-not-registered:%s",
		device.Id, img.Name)
}

func (agent *Agent) cancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "cancel-image-download", log.Fields{"device-id": agent.deviceID})

	if !agent.proceedWithRequest() {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device reconciling or deletion is in progress.",
			agent.deviceID)
	}

	// Update image download state
	cloned := agent.cloneDeviceWithoutLock()
	_, index, err := getImage(img, cloned)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	cloned.ImageDownloads[index].DownloadState = voltha.ImageDownload_DOWNLOAD_CANCELLED

	if cloned.AdminState != voltha.AdminState_DOWNLOADING_IMAGE {
		agent.requestQueue.RequestComplete()
	} else {
		// Set the device to Enabled
		cloned.AdminState = voltha.AdminState_ENABLED
		if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
			return nil, err
		}
		subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
		subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

		ch, err := agent.adapterProxy.CancelImageDownload(subCtx, cloned, img)
		if err != nil {
			cancel()
			return nil, err
		}
		go agent.waitForAdapterResponse(subCtx, cancel, "cancelImageDownload", ch, agent.onImageSuccess,
			agent.onImageFailure)
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) activateImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "activate-image", log.Fields{"device-id": agent.deviceID})

	if !agent.proceedWithRequest() {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device reconciling or deletion is in progress.",
			agent.deviceID)
	}

	// Update image download state
	cloned := agent.cloneDeviceWithoutLock()
	image, index, err := getImage(img, cloned)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	if image.DownloadState != voltha.ImageDownload_DOWNLOAD_SUCCEEDED {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, device-has-not-downloaded-image:%s", agent.deviceID, img.Name)
	}

	//TODO does this need to be removed ?
	if cloned.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, device-in-downloading-state:%s", agent.deviceID, img.Name)
	}

	// Save the image
	cloned.ImageDownloads[index].ImageState = voltha.ImageDownload_IMAGE_ACTIVATING

	cloned.AdminState = voltha.AdminState_DOWNLOADING_IMAGE
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	ch, err := agent.adapterProxy.ActivateImageUpdate(subCtx, cloned, img)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "activateImageUpdate", ch, agent.onImageSuccess, agent.onFailure)

	// The status of the AdminState will be changed following the update_download_status response from the adapter
	// The image name will also be removed from the device list
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) revertImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "revert-image", log.Fields{"device-id": agent.deviceID})

	// Update image download state
	cloned := agent.cloneDeviceWithoutLock()
	_, index, err := getImage(img, cloned)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}
	if cloned.AdminState != voltha.AdminState_ENABLED {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, device-not-enabled-state:%s", agent.deviceID, img.Name)
	}

	cloned.ImageDownloads[index].ImageState = voltha.ImageDownload_IMAGE_REVERTING

	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	ch, err := agent.adapterProxy.RevertImageUpdate(subCtx, cloned, img)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "revertImageUpdate", ch, agent.onSuccess, agent.onFailure)

	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) getImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw(ctx, "get-image-download-status", log.Fields{"device-id": agent.deviceID})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	device := agent.getDeviceReadOnlyWithoutLock()
	ch, err := agent.adapterProxy.GetImageDownloadStatus(ctx, device, img)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}
	// Wait for the adapter response
	rpcResponse, ok := <-ch
	if !ok {
		return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
	}
	if rpcResponse.Err != nil {
		return nil, rpcResponse.Err
	}
	// Successful response
	imgDownload := &voltha.ImageDownload{}
	if err := ptypes.UnmarshalAny(rpcResponse.Reply, imgDownload); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}
	return imgDownload, nil
}

func (agent *Agent) updateImageDownload(ctx context.Context, img *voltha.ImageDownload) error {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	logger.Debugw(ctx, "updating-image-download", log.Fields{"device-id": agent.deviceID, "img": img})

	if !agent.proceedWithRequest() {
		agent.requestQueue.RequestComplete()
		return status.Errorf(codes.FailedPrecondition, "deviceId:%s, Device reconciling or deletion is in progress.",
			agent.deviceID)
	}

	// Update the image as well as remove it if the download was cancelled
	cloned := agent.cloneDeviceWithoutLock()
	clonedImages := make([]*voltha.ImageDownload, len(cloned.ImageDownloads))
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			if image.DownloadState != voltha.ImageDownload_DOWNLOAD_CANCELLED {
				clonedImages = append(clonedImages, img)
			}
		}
	}
	cloned.ImageDownloads = clonedImages

	// Set the Admin state to enabled if required
	if (img.DownloadState != voltha.ImageDownload_DOWNLOAD_REQUESTED &&
		img.DownloadState != voltha.ImageDownload_DOWNLOAD_STARTED) ||
		img.ImageState != voltha.ImageDownload_IMAGE_ACTIVATING {
		cloned.AdminState = voltha.AdminState_ENABLED
	}
	return agent.updateDeviceAndReleaseLock(ctx, cloned)
}

func (agent *Agent) getImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw(ctx, "get-image-download", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err)
	}
	for _, image := range device.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return image, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "image-not-found:%s", img.Name)
}

func (agent *Agent) listImageDownloads(ctx context.Context, deviceID string) (*voltha.ImageDownloads, error) {
	logger.Debugw(ctx, "list-image-downloads", log.Fields{"device-id": agent.deviceID})

	device, err := agent.getDeviceReadOnly(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err)
	}
	return &voltha.ImageDownloads{Items: device.ImageDownloads}, nil
}

// onImageFailure brings back the device to Enabled state and sets the image to image download_failed.
func (agent *Agent) onImageFailure(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{}) {
	// original context has failed due to timeout , let's open a new one
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	defer cancel()

	if err := agent.requestQueue.WaitForGreenLight(subCtx); err != nil {
		logger.Errorw(subCtx, "can't obtain lock", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": err, "args": reqArgs})
		return
	}
	if !agent.proceedWithRequest() {
		agent.requestQueue.RequestComplete()
		return
	}
	if res, ok := response.(error); ok {
		logger.Errorw(subCtx, "rpc-failed", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": res, "args": reqArgs})
		cloned := agent.cloneDeviceWithoutLock()
		//TODO base this on IMAGE ID when created
		var imageFailed *voltha.ImageDownload
		var index int
		if cloned.ImageDownloads != nil {
			for pos, image := range cloned.ImageDownloads {
				if image.DownloadState == voltha.ImageDownload_DOWNLOAD_REQUESTED ||
					image.ImageState == voltha.ImageDownload_IMAGE_ACTIVATING {
					imageFailed = image
					index = pos
				}
			}
		}

		if imageFailed == nil {
			logger.Errorw(subCtx, "can't find image", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "args": reqArgs})
			return
		}

		//update image state on failure
		if imageFailed.DownloadState == voltha.ImageDownload_DOWNLOAD_REQUESTED {
			cloned.ImageDownloads[index].DownloadState = voltha.ImageDownload_DOWNLOAD_FAILED
		} else if imageFailed.ImageState == voltha.ImageDownload_IMAGE_ACTIVATING {
			cloned.ImageDownloads[index].ImageState = voltha.ImageDownload_IMAGE_INACTIVE
		}
		//Enabled is the only state we can go back to.
		cloned.AdminState = voltha.AdminState_ENABLED
		if err := agent.updateDeviceAndReleaseLock(subCtx, cloned); err != nil {
			logger.Errorw(subCtx, "failed-enable-device-after-image-failure",
				log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": res, "args": reqArgs})
		}
	} else {
		logger.Errorw(subCtx, "rpc-failed-invalid-error", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "args": reqArgs})
		return
	}
	// TODO: Post failure message onto kafka
}

// onImageSuccess brings back the device to Enabled state and sets the image to image download_failed.
func (agent *Agent) onImageSuccess(ctx context.Context, rpc string, response interface{}, reqArgs ...interface{}) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		logger.Errorw(ctx, "cannot-obtain-lock", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": err, "args": reqArgs})
		return
	}
	if !agent.proceedWithRequest() {
		agent.requestQueue.RequestComplete()
		return
	}
	logger.Infow(ctx, "rpc-successful", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "response": response, "args": reqArgs})
	cloned := agent.cloneDeviceWithoutLock()
	//TODO base this on IMAGE ID when created
	var imageSucceeded *voltha.ImageDownload
	var index int
	if cloned.ImageDownloads != nil {
		for pos, image := range cloned.ImageDownloads {
			if image.DownloadState == voltha.ImageDownload_DOWNLOAD_REQUESTED ||
				image.ImageState == voltha.ImageDownload_IMAGE_ACTIVATING {
				imageSucceeded = image
				index = pos
			}
		}
	}

	if imageSucceeded == nil {
		logger.Errorw(ctx, "can't find image", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "args": reqArgs})
		return
	}
	//update image state on success
	if imageSucceeded.DownloadState == voltha.ImageDownload_DOWNLOAD_REQUESTED {
		cloned.ImageDownloads[index].DownloadState = voltha.ImageDownload_DOWNLOAD_SUCCEEDED
	} else if imageSucceeded.ImageState == voltha.ImageDownload_IMAGE_ACTIVATING {
		cloned.ImageDownloads[index].ImageState = voltha.ImageDownload_IMAGE_ACTIVE
	}
	//Enabled is the only state we can go back to.
	cloned.AdminState = voltha.AdminState_ENABLED
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		logger.Errorw(ctx, "failed-enable-device-after-image-download-success",
			log.Fields{"rpc": rpc, "device-id": agent.deviceID, "response": response, "args": reqArgs})
	}

}

func (agent *Agent) downloadImageToDevice(ctx context.Context, request *voltha.DeviceImageDownloadRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "download-image-to-device", log.Fields{"device-id": agent.deviceID})
	if agent.device.Root {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, is an OLT. Image update "+
			"not supported by VOLTHA. Use Device Manager or other means", agent.deviceID)
	}

	cloned := agent.cloneDeviceWithoutLock()

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	ch, err := agent.adapterProxy.DownloadImageToOnuDevice(subCtx, cloned, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	return agent.getDeviceImageResponseFromAdapter(ctx, ch)
}

func (agent *Agent) getImageStatus(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	logger.Debugw(ctx, "get-image-status", log.Fields{"device-id": agent.deviceID})

	ch, err := agent.adapterProxy.GetOnuImageStatus(subCtx, cloned, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	return agent.getDeviceImageResponseFromAdapter(subCtx, ch)
}

func (agent *Agent) activateImageOnDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	logger.Debugw(ctx, "activate-image-on-device", log.Fields{"device-id": agent.deviceID})

	ch, err := agent.adapterProxy.ActivateOnuImage(subCtx, cloned, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	return agent.getDeviceImageResponseFromAdapter(subCtx, ch)
}

func (agent *Agent) abortImageUpgradeToDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	logger.Debugw(ctx, "abort-image-on-device", log.Fields{"device-id": agent.deviceID})

	ch, err := agent.adapterProxy.AbortImageUpgrade(subCtx, cloned, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	return agent.getDeviceImageResponseFromAdapter(ctx, ch)
}

func (agent *Agent) commitImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	// Send the request to the adapter
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	logger.Debugw(ctx, "commit-image-on-device", log.Fields{"device-id": agent.deviceID})

	ch, err := agent.adapterProxy.CommitImage(subCtx, cloned, request)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	return agent.getDeviceImageResponseFromAdapter(ctx, ch)
}

func (agent *Agent) getOnuImages(ctx context.Context, id *common.ID) (*voltha.OnuImages, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	cloned := agent.cloneDeviceWithoutLock()

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.defaultTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	logger.Debugw(ctx, "get-onu-images", log.Fields{"device-id": agent.deviceID})

	// Send the request to the adapter
	ch, err := agent.adapterProxy.GetOnuImages(subCtx, cloned, id)
	agent.requestQueue.RequestComplete()
	if err != nil {
		return nil, err
	}

	//wait for adapter response
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
		} else if rpcResponse.Err != nil {
			// return error
			return nil, status.Errorf(codes.Internal, "%s", rpcResponse.Err.Error())
		} else {
			resp := &voltha.OnuImages{}
			if err := ptypes.UnmarshalAny(rpcResponse.Reply, resp); err != nil {
				return nil, status.Errorf(codes.Internal, "%s", err.Error())
			}

			return resp, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (agent *Agent) getDeviceImageResponseFromAdapter(ctx context.Context, ch chan *kafka.RpcResponse) (*voltha.DeviceImageResponse, error) {
	//wait for adapter response
	select {
	case rpcResponse, ok := <-ch:
		if !ok {
			return nil, status.Errorf(codes.Aborted, "channel-closed-device-id-%s", agent.deviceID)
		} else if rpcResponse.Err != nil {
			// return error
			return nil, status.Errorf(codes.Internal, "%s", rpcResponse.Err.Error())
		} else {
			resp := &voltha.DeviceImageResponse{}
			if err := ptypes.UnmarshalAny(rpcResponse.Reply, resp); err != nil {
				return nil, status.Errorf(codes.Internal, "%s", err.Error())
			}

			if len(resp.DeviceImageStates) == 0 || resp.DeviceImageStates[0] == nil {
				return nil, status.Errorf(codes.Internal, "invalid response from adapter")
			}

			return resp, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
