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
	"errors"
	"time"

	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"

	"github.com/opencord/voltha-protos/v5/go/common"

	"github.com/gogo/protobuf/proto"
	coreutils "github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *Agent) downloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	var err error
	var desc string
	var prevAdminState, currAdminState common.AdminState_Types
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, &prevAdminState, &currAdminState, operStatus, err, desc) }()

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "download-image", log.Fields{"device-id": agent.deviceID})

	if agent.device.Root {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "device-id:%s, is an OLT. Image update "+
			"not supported by VOLTHA. Use Device Manager or other means", agent.deviceID)
		return nil, err
	}

	device := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(device) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return nil, err
	}

	if device.ImageDownloads != nil {
		for _, image := range device.ImageDownloads {
			if image.DownloadState == voltha.ImageDownload_DOWNLOAD_REQUESTED {
				err = status.Errorf(codes.FailedPrecondition, "device-id:%s, already downloading image:%s",
					agent.deviceID, image.Name)
				agent.requestQueue.RequestComplete()
				return nil, err
			}
		}
	}

	// Save the image
	clonedImg := proto.Clone(img).(*voltha.ImageDownload)
	clonedImg.DownloadState = voltha.ImageDownload_DOWNLOAD_REQUESTED
	cloned := agent.cloneDeviceWithoutLock()
	_, index, err := getImage(img, device)
	if err != nil {
		cloned.ImageDownloads = append(cloned.ImageDownloads, clonedImg)
	} else {
		cloned.ImageDownloads[index] = clonedImg
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		response, err := client.DownloadImage(subCtx, &ca.ImageDownloadMessage{
			Device: cloned,
			Image:  clonedImg,
		})
		if err == nil {
			agent.onImageSuccess(subCtx, response)
		} else {
			agent.onImageFailure(subCtx, err)
		}
	}()

	cloned.AdminState = voltha.AdminState_DOWNLOADING_IMAGE
	if err = agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}
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

	var err error
	var desc string
	var prevAdminState, currAdminState common.AdminState_Types
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, &prevAdminState, &currAdminState, operStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "cancel-image-download", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return nil, err
	}

	// Update image download state
	_, index, err := getImage(img, cloned)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	prevAdminState = cloned.AdminState
	cloned.ImageDownloads[index].DownloadState = voltha.ImageDownload_DOWNLOAD_CANCELLED

	if cloned.AdminState != voltha.AdminState_DOWNLOADING_IMAGE {
		err = status.Errorf(codes.Aborted, "device not in image download state %s", cloned.Id)
		agent.requestQueue.RequestComplete()
		return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_FAILURE}, err
	}
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		response, err := client.CancelImageDownload(subCtx, &ca.ImageDownloadMessage{
			Device: cloned,
			Image:  img,
		})
		if err == nil {
			agent.onImageSuccess(subCtx, response)
		} else {
			agent.onImageFailure(subCtx, err)
		}
	}()

	// Set the device to Enabled
	cloned.AdminState = voltha.AdminState_ENABLED
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}
	currAdminState = cloned.AdminState

	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) activateImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "activate-image", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	// Update image download state
	image, index, err := getImage(img, cloned)
	if err != nil {
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	if image.DownloadState != voltha.ImageDownload_DOWNLOAD_SUCCEEDED {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "device-id:%s, device-has-not-downloaded-image:%s", agent.deviceID, img.Name)
		return nil, err
	}

	//TODO does this need to be removed ?
	if cloned.AdminState == voltha.AdminState_DOWNLOADING_IMAGE {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "device-id:%s, device-in-downloading-state:%s", agent.deviceID, img.Name)
		return nil, err
	}

	// Update the image
	cloned.ImageDownloads[index].ImageState = voltha.ImageDownload_IMAGE_ACTIVATING
	cloned.AdminState = voltha.AdminState_DOWNLOADING_IMAGE

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		response, err := client.ActivateImageUpdate(subCtx, &ca.ImageDownloadMessage{
			Device: cloned,
			Image:  img,
		})
		if err == nil {
			agent.onImageSuccess(subCtx, response)
		} else {
			agent.onImageFailure(subCtx, err)
		}
	}()

	// Save the image
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}

	// The status of the AdminState will be changed following the update_download_status response from the adapter
	// The image name will also be removed from the device list
	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) revertImage(ctx context.Context, img *voltha.ImageDownload) (*voltha.OperationResp, error) {
	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}
	logger.Debugw(ctx, "revert-image", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()
	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return nil, err
	}

	// Update image download state
	_, index, err := getImage(img, cloned)
	if err != nil {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "deviceId:%s, image-not-registered:%s", agent.deviceID, img.Name)
		return nil, err
	}
	if cloned.AdminState != voltha.AdminState_ENABLED {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "deviceId:%s, device-not-enabled-state:%s", agent.deviceID, img.Name)
		return nil, err
	}

	cloned.ImageDownloads[index].ImageState = voltha.ImageDownload_IMAGE_REVERTING
	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	operStatus.Code = common.OperationResp_OPERATION_IN_PROGRESS
	go func() {
		defer cancel()
		_, err := client.RevertImageUpdate(subCtx, &ca.ImageDownloadMessage{
			Device: cloned,
			Image:  img,
		})
		if err == nil {
			agent.onSuccess(subCtx, nil, nil, true)
		} else {
			agent.onFailure(subCtx, err, nil, nil, true)
		}
	}()

	// Save data
	if err := agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		return nil, err
	}

	return &voltha.OperationResp{Code: voltha.OperationResp_OPERATION_SUCCESS}, nil
}

func (agent *Agent) getImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	logger.Debugw(ctx, "get-image-download-status", log.Fields{"device-id": agent.deviceID})

	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	device := agent.getDeviceReadOnlyWithoutLock()
	if !agent.proceedWithRequest(device) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": device.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	agent.requestQueue.RequestComplete()
	return client.GetImageDownloadStatus(ctx, &ca.ImageDownloadMessage{
		Device: device,
		Image:  img,
	})
}

func (agent *Agent) updateImageDownload(ctx context.Context, img *voltha.ImageDownload) error {
	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}

	logger.Debugw(ctx, "updating-image-download", log.Fields{"device-id": agent.deviceID, "img": img})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return err
	}

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
func (agent *Agent) onImageFailure(ctx context.Context, imgErr error) {
	// original context has failed due to timeout , let's open a new one
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.internalTimeout)
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)
	defer cancel()
	rpc := coreutils.GetRPCMetadataFromContext(subCtx)

	defer func() {
		eCtx := coreutils.WithSpanAndRPCMetadataFromContext(ctx)
		rpce := agent.deviceMgr.NewRPCEvent(eCtx, agent.deviceID, imgErr.Error(), nil)
		go agent.deviceMgr.SendRPCEvent(eCtx, "RPC_ERROR_RAISE_EVENT", rpce,
			voltha.EventCategory_COMMUNICATION, nil, time.Now().Unix())
		operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
		desc := "adapter-response"
		agent.logDeviceUpdate(ctx, nil, nil, operStatus, imgErr, desc)
	}()

	if err := agent.requestQueue.WaitForGreenLight(subCtx); err != nil {
		logger.Errorw(subCtx, "can't obtain lock", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": err})
		return
	}

	device := agent.getDeviceReadOnlyWithoutLock()

	if !agent.proceedWithRequest(device) {
		agent.requestQueue.RequestComplete()
		logger.Errorw(subCtx, "Cannot complete operation as device deletion is in progress or reconciling is in progress/failed.",
			log.Fields{"rpc": rpc, "device-id": agent.deviceID})
		return
	}
	if imgErr != nil {
		logger.Errorw(subCtx, "rpc-failed", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": imgErr})
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
			logger.Errorw(subCtx, "can't find image", log.Fields{"rpc": rpc, "device-id": agent.deviceID})
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
				log.Fields{"rpc": rpc, "device-id": agent.deviceID, "error": err})
		}
	} else {
		logger.Errorw(subCtx, "rpc-failed-invalid-error", log.Fields{"rpc": rpc, "device-id": agent.deviceID})
		return
	}
}

// onImageSuccess brings back the device to Enabled state and sets the image to image download_failed.
func (agent *Agent) onImageSuccess(ctx context.Context, response interface{}) {
	rpc := coreutils.GetRPCMetadataFromContext(ctx)

	var err error
	var desc string
	operStatus := &common.OperationResp{Code: common.OperationResp_OPERATION_FAILURE}
	defer func() { agent.logDeviceUpdate(ctx, nil, nil, operStatus, err, desc) }()

	if err = agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return
	}

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		err = status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
		return
	}
	logger.Infow(ctx, "rpc-successful", log.Fields{"rpc": rpc, "device-id": agent.deviceID, "response": response})
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
		err = errors.New("can't find image")
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
	if err = agent.updateDeviceAndReleaseLock(ctx, cloned); err != nil {
		logger.Errorw(ctx, "failed-enable-device-after-image-download-success",
			log.Fields{"rpc": rpc, "device-id": agent.deviceID, "response": response})
	}
	// Update operation status
	if err == nil {
		operStatus.Code = common.OperationResp_OPERATION_SUCCESS
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
	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	agent.requestQueue.RequestComplete()
	return client.DownloadOnuImage(subCtx, request)
}

func (agent *Agent) getImageStatus(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "get-image-status", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()
	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	agent.requestQueue.RequestComplete()
	return client.GetOnuImageStatus(subCtx, request)
}

func (agent *Agent) activateImageOnDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "activate-image-on-device", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	agent.requestQueue.RequestComplete()
	return client.ActivateOnuImage(subCtx, request)
}

func (agent *Agent) abortImageUpgradeToDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "abort-image-on-device", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	agent.requestQueue.RequestComplete()
	return client.AbortOnuImageUpgrade(subCtx, request)
}

func (agent *Agent) commitImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "commit-image-on-device", log.Fields{"device-id": agent.deviceID})

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}
	subCtx, cancel := context.WithTimeout(log.WithSpanFromContext(context.Background(), ctx), agent.rpcTimeout)
	defer cancel()
	subCtx = coreutils.WithRPCMetadataFromContext(subCtx, ctx)

	agent.requestQueue.RequestComplete()
	return client.CommitOnuImage(subCtx, request)
}

func (agent *Agent) getOnuImages(ctx context.Context, id *common.ID) (*voltha.OnuImages, error) {
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return nil, err
	}

	logger.Debug(ctx, "get-onu-images")

	cloned := agent.cloneDeviceWithoutLock()

	if !agent.proceedWithRequest(cloned) {
		agent.requestQueue.RequestComplete()
		return nil, status.Errorf(codes.FailedPrecondition, "%s", "cannot complete operation as device deletion is in progress or reconciling is in progress/failed")
	}

	// Send the request to the adapter
	client, err := agent.adapterMgr.GetAdapterClient(ctx, agent.adapterEndpoint)
	if err != nil {
		logger.Errorw(ctx, "grpc-client-nil",
			log.Fields{
				"error":            err,
				"device-id":        agent.deviceID,
				"device-type":      agent.deviceType,
				"adapter-endpoint": cloned.AdapterEndpoint,
			})
		agent.requestQueue.RequestComplete()
		return nil, err
	}

	agent.requestQueue.RequestComplete()
	return client.GetOnuImages(ctx, id)
}
