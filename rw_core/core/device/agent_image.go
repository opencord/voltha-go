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

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) downloadImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw(ctx, "downloadImage", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceWithoutLock()

	if device.AdminState != voltha.AdminState_ENABLED {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, expected-admin-state:%s", agent.deviceID, voltha.AdminState_ENABLED)
	}
	// Save the image
	clonedImg := proto.Clone(img).(*voltha.ImageDownload)
	clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
	cloned := proto.Clone(device).(*voltha.Device)
	if cloned.ImageDownloads == nil {
		cloned.ImageDownloads = []*voltha.ImageDownload{clonedImg}
	} else {
		if device.AdminState != voltha.AdminState_ENABLED {
			logger.Debugw(ctx, "device-not-enabled", log.Fields{"id": agent.deviceID})
			return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, expected-admin-state:%s", agent.deviceID, voltha.AdminState_ENABLED)
		}
		// Save the image
		clonedImg := proto.Clone(img).(*voltha.ImageDownload)
		clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
		if device.ImageDownloads == nil {
			device.ImageDownloads = []*voltha.ImageDownload{clonedImg}
		} else {
			device.ImageDownloads = append(device.ImageDownloads, clonedImg)
		}
		if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_DOWNLOADING_IMAGE, device.ConnectStatus, device.OperStatus); err != nil {
			return nil, err
		}

		// Send the request to the adapter
		subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
		ch, err := agent.adapterProxy.DownloadImage(ctx, cloned, clonedImg)
		if err != nil {
			cancel()
			return nil, err
		}
		go agent.waitForAdapterResponse(subCtx, cancel, "downloadImage", ch, agent.onSuccess, agent.onFailure)
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

// isImageRegistered is a helper method to figure out if an image is already registered
func isImageRegistered(img *voltha.ImageDownload, device *voltha.Device) bool {
	for _, image := range device.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return true
		}
	}
	return false
}

func (agent *Agent) cancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()

	logger.Debugw(ctx, "cancelImageDownload", log.Fields{"device-id": agent.deviceID})

	device := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, device) {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	// Update image download state
	for _, image := range device.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.DownloadState = voltha.ImageDownload_DOWNLOAD_CANCELLED
		}
	}

	if device.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		// Set the device to Enabled
		if err := agent.updateDeviceStateInStoreWithoutLock(ctx, device, voltha.AdminState_ENABLED, device.ConnectStatus, device.OperStatus); err != nil {
			return nil, err
		}
		subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
		ch, err := agent.adapterProxy.CancelImageDownload(subCtx, device, img)
		if err != nil {
			cancel()
			return nil, err
		}
		go agent.waitForAdapterResponse(subCtx, cancel, "cancelImageDownload", ch, agent.onSuccess, agent.onFailure)
	}
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) activateImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "activateImage", log.Fields{"device-id": agent.deviceID})
	cloned := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, cloned) {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	if cloned.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		return nil, status.Errorf(codes.FailedPrecondition, "device-id:%s, device-in-downloading-state:%s", agent.deviceID, img.Name)
	}
	// Update image download state
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.ImageState = voltha.ImageDownload_IMAGE_ACTIVATING
		}
	}
	// Set the device to downloading_image
	if err := agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_DOWNLOADING_IMAGE, cloned.ConnectStatus, cloned.OperStatus); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.ActivateImageUpdate(subCtx, proto.Clone(cloned).(*voltha.Device), img)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "activateImageUpdate", ch, agent.onSuccess, agent.onFailure)

	// The status of the AdminState will be changed following the update_download_status response from the adapter
	// The image name will also be removed from the device list
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) revertImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "revertImage", log.Fields{"device-id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()

	// Verify whether the Image is in the list of image being downloaded
	if !isImageRegistered(img, cloned) {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, image-not-registered:%s", agent.deviceID, img.Name)
	}

	if cloned.AdminState != voltha.AdminState_ENABLED {
		return nil, status.Errorf(codes.FailedPrecondition, "deviceId:%s, device-not-enabled-state:%s", agent.deviceID, img.Name)
	}
	// Update image download state
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			image.ImageState = voltha.ImageDownload_IMAGE_REVERTING
		}
	}

	if err := agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, ""); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(context.Background(), agent.defaultTimeout)
	ch, err := agent.adapterProxy.RevertImageUpdate(subCtx, proto.Clone(cloned).(*voltha.Device), img)
	if err != nil {
		cancel()
		return nil, err
	}
	go agent.waitForAdapterResponse(subCtx, cancel, "revertImageUpdate", ch, agent.onSuccess, agent.onFailure)

	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) getImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw(ctx, "getImageDownloadStatus", log.Fields{"device-id": agent.deviceID})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	device := agent.getDeviceWithoutLock()
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
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "updating-image-download", log.Fields{"device-id": agent.deviceID, "img": img})

	cloned := agent.getDeviceWithoutLock()

	// Update the image as well as remove it if the download was cancelled
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
		(img.ImageState != voltha.ImageDownload_IMAGE_ACTIVATING) {
		return agent.updateDeviceStateInStoreWithoutLock(ctx, cloned, voltha.AdminState_ENABLED, cloned.ConnectStatus, cloned.OperStatus)
	}
	return agent.updateDeviceInStoreWithoutLock(ctx, cloned, false, "")
}

func (agent *Agent) getImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "getImageDownload", log.Fields{"device-id": agent.deviceID})

	cloned := agent.getDeviceWithoutLock()
	for _, image := range cloned.ImageDownloads {
		if image.Id == img.Id && image.Name == img.Name {
			return image, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "image-not-found:%s", img.Name)
}

func (agent *Agent) listImageDownloads(ctx context.Context, deviceID string) (*voltha.ImageDownloads, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	defer agent.requestQueue.RequestComplete()
	logger.Debugw(ctx, "listImageDownloads", log.Fields{"device-id": agent.deviceID})

	return &voltha.ImageDownloads{Items: agent.getDeviceWithoutLock().ImageDownloads}, nil
}
