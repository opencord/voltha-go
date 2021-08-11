package device

import (
	"context"
	"errors"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/common"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateDevice creates a new parent device in the data model
func (dMgr *Manager) CreateDevice(ctx context.Context, device *voltha.Device) (*voltha.Device, error) {
	if device.MacAddress == "" && device.GetHostAndPort() == "" {
		logger.Errorf(ctx, "no-device-info-present")
		return &voltha.Device{}, errors.New("no-device-info-present; MAC or HOSTIP&PORT")
	}
	ctx = utils.WithRPCMetadataContext(ctx, "CreateDevice")
	logger.Debugw(ctx, "create-device", log.Fields{"device": *device})

	deviceExist, err := dMgr.isParentDeviceExist(ctx, device)
	if err != nil {
		logger.Errorf(ctx, "failed-to-fetch-parent-device-info")
		return nil, err
	}
	if deviceExist {
		logger.Errorf(ctx, "device-is-pre-provisioned-already-with-same-ip-port-or-mac-address")
		return nil, errors.New("device is already pre-provisioned")
	}
	logger.Debugw(ctx, "create-device", log.Fields{"device": device})

	// Ensure this device is set as root
	device.Root = true
	// Create and start a device agent for that device
	agent := newAgent(device, dMgr, dMgr.dbPath, dMgr.dProxy, dMgr.internalTimeout, dMgr.rpcTimeout)
	device, err = agent.start(ctx, false, device)
	if err != nil {
		logger.Errorw(ctx, "fail-to-start-device", log.Fields{"device-id": agent.deviceID, "error": err})
		return nil, err
	}
	dMgr.addDeviceAgentToMap(agent)
	return device, nil
}

// EnableDevice activates a device by invoking the adopt_device API on the appropriate adapter
func (dMgr *Manager) EnableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "EnableDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "enable-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.enableDevice(ctx)
}

// DisableDevice disables a device along with any child device it may have
func (dMgr *Manager) DisableDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DisableDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "disable-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.disableDevice(ctx)
}

//RebootDevice invoked the reboot API to the corresponding adapter
func (dMgr *Manager) RebootDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "RebootDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "reboot-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.rebootDevice(ctx)
}

// DeleteDevice removes a device from the data model
func (dMgr *Manager) DeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DeleteDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "delete-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.deleteDevice(ctx)
}

// ForceDeleteDevice removes a device from the data model forcefully without successfully waiting for the adapters.
func (dMgr *Manager) ForceDeleteDevice(ctx context.Context, id *voltha.ID) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ForceDeleteDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "force-delete-device", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return &empty.Empty{}, agent.deleteDeviceForce(ctx)
}

// ListDevices retrieves the latest devices from the data model
func (dMgr *Manager) ListDevices(ctx context.Context, _ *empty.Empty) (*voltha.Devices, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDevices")

	logger.Debug(ctx, "list-devices")
	result := &voltha.Devices{}

	dMgr.deviceAgents.Range(func(key, value interface{}) bool {
		result.Items = append(result.Items, value.(*Agent).device)
		return true
	})

	logger.Debugw(ctx, "list-devices-end", log.Fields{"len": len(result.Items)})
	return result, nil
}

// ListDeviceIds retrieves the latest device IDs information from the data model (memory data only)
func (dMgr *Manager) ListDeviceIds(ctx context.Context, _ *empty.Empty) (*voltha.IDs, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDeviceIds")

	logger.Debug(ctx, "list-device-ids")
	// Report only device IDs that are in the device agent map
	return dMgr.listDeviceIdsFromMap(), nil
}

// ReconcileDevices is a request to a voltha core to update its list of managed devices.  This will
// trigger loading the devices along with their children and parent in memory
func (dMgr *Manager) ReconcileDevices(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ReconcileDevices")

	numDevices := 0
	if ids != nil {
		numDevices = len(ids.Items)
	}

	logger.Debugw(ctx, "reconcile-devices", log.Fields{"num-devices": numDevices})
	if ids != nil && len(ids.Items) != 0 {
		toReconcile := len(ids.Items)
		reconciled := 0
		var err error
		for _, id := range ids.Items {
			if err = dMgr.load(ctx, id.Id); err != nil {
				logger.Warnw(ctx, "failure-reconciling-device", log.Fields{"device-id": id.Id, "error": err})
			} else {
				reconciled++
			}
		}
		if toReconcile != reconciled {
			return nil, status.Errorf(codes.DataLoss, "less-device-reconciled-than-requested:%d/%d", reconciled, toReconcile)
		}
	} else {
		return nil, status.Errorf(codes.InvalidArgument, "empty-list-of-ids")
	}
	return &empty.Empty{}, nil
}

// GetDevice exists primarily to implement the gRPC interface.
// Internal functions should call getDeviceReadOnly instead.
func (dMgr *Manager) GetDevice(ctx context.Context, id *voltha.ID) (*voltha.Device, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetDevice")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	return dMgr.getDeviceReadOnly(ctx, id.Id)
}

// convenience to avoid redefining
var operationFailureResp = &common.OperationResp{Code: voltha.OperationResp_OPERATION_FAILURE}

// DownloadImage execute an image download request
func (dMgr *Manager) DownloadImage(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DownloadImage")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "download-image", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.downloadImage(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// CancelImageDownload cancels image download request
func (dMgr *Manager) CancelImageDownload(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "CancelImageDownload")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "cancel-image-download", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.cancelImageDownload(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// ActivateImageUpdate activates image update request
func (dMgr *Manager) ActivateImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ActivateImageUpdate")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "activate-image-update", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.activateImage(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

// RevertImageUpdate reverts image update
func (dMgr *Manager) RevertImageUpdate(ctx context.Context, img *voltha.ImageDownload) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "RevertImageUpdate")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "rever-image-update", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return operationFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.revertImage(ctx, img)
	if err != nil {
		return operationFailureResp, err
	}
	return resp, nil
}

func (dMgr *Manager) DownloadImageToDevice(ctx context.Context, request *voltha.DeviceImageDownloadRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageDownloadRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "DownloadImageToDevice")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	for index, deviceID := range request.DeviceId {
		// Create download request per device
		downloadReq := &voltha.DeviceImageDownloadRequest{
			Image:             request.Image,
			ActivateOnSuccess: request.ActivateOnSuccess,
			CommitOnSuccess:   request.CommitOnSuccess,
		}

		//slice-out only single deviceID from the request
		downloadReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageDownloadRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.downloadImageToDevice(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "download-image-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "download-image-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), downloadReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "download-image-to-device", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) GetImageStatus(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "GetImageStatus")

	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))
	for index, deviceID := range request.DeviceId {
		// Create status request per device
		imageStatusReq := &voltha.DeviceImageRequest{
			Version:         request.Version,
			CommitOnSuccess: request.CommitOnSuccess,
		}

		//slice-out only single deviceID from the request
		imageStatusReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.getImageStatus(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "get-image-status-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "get-image-status-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), imageStatusReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "get-image-status", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) AbortImageUpgradeToDevice(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "AbortImageUpgradeToDevice")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	for index, deviceID := range request.DeviceId {
		// Create abort request per device
		abortImageReq := &voltha.DeviceImageRequest{
			Version:         request.Version,
			CommitOnSuccess: request.CommitOnSuccess,
		}

		//slice-out only single deviceID from the request
		abortImageReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.abortImageUpgradeToDevice(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "abort-image-upgrade-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "abort-image-upgrade-to-device-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), abortImageReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "abort-image-upgrade-to-device", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) GetOnuImages(ctx context.Context, id *common.ID) (*voltha.OnuImages, error) {
	if id == nil || id.Id == "" {
		return nil, status.Errorf(codes.InvalidArgument, "empty device id")
	}

	ctx = utils.WithRPCMetadataContext(ctx, "GetOnuImages")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}

	resp, err := agent.getOnuImages(ctx, id)
	if err != nil {
		return nil, err
	}

	logger.Debugw(ctx, "get-onu-images-result", log.Fields{"onu-image": resp.Items})

	return resp, nil
}

func (dMgr *Manager) ActivateImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "ActivateImage")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	for index, deviceID := range request.DeviceId {
		// Create activate request per device
		activateImageReq := &voltha.DeviceImageRequest{
			Version:         request.Version,
			CommitOnSuccess: request.CommitOnSuccess,
		}

		//slice-out only single deviceID from the request
		activateImageReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.activateImageOnDevice(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "activate-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorw(ctx, "activate-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), activateImageReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "activate-image", respCh, len(request.GetDeviceId()))
}

func (dMgr *Manager) CommitImage(ctx context.Context, request *voltha.DeviceImageRequest) (*voltha.DeviceImageResponse, error) {
	if err := dMgr.validateImageRequest(request); err != nil {
		return nil, err
	}

	ctx = utils.WithRPCMetadataContext(ctx, "CommitImage")
	respCh := make(chan []*voltha.DeviceImageState, len(request.GetDeviceId()))

	for index, deviceID := range request.DeviceId {
		// Create commit request per device
		commitImageReq := &voltha.DeviceImageRequest{
			Version:         request.Version,
			CommitOnSuccess: request.CommitOnSuccess,
		}
		//slice-out only single deviceID from the request
		commitImageReq.DeviceId = request.DeviceId[index : index+1]

		go func(deviceID string, req *voltha.DeviceImageRequest, ch chan []*voltha.DeviceImageState) {
			agent := dMgr.getDeviceAgent(ctx, deviceID)
			if agent == nil {
				logger.Errorw(ctx, "Not-found", log.Fields{"device-id": deviceID})
				ch <- nil
				return
			}

			resp, err := agent.commitImage(ctx, req)
			if err != nil {
				logger.Errorw(ctx, "commit-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}

			err = dMgr.validateDeviceImageResponse(resp)
			if err != nil {
				logger.Errorf(ctx, "commit-image-failed", log.Fields{"device-id": deviceID, "error": err})
				ch <- nil
				return
			}
			ch <- resp.GetDeviceImageStates()
		}(deviceID.GetId(), commitImageReq, respCh)

	}

	return dMgr.waitForAllResponses(ctx, "commit-image", respCh, len(request.GetDeviceId()))
}

// convenience to avoid redefining
var imageDownloadFailureResp = &voltha.ImageDownload{DownloadState: voltha.ImageDownload_DOWNLOAD_UNKNOWN}

// GetImageDownloadStatus returns status of image download
func (dMgr *Manager) GetImageDownloadStatus(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetImageDownloadStatus")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "get-image-download-status", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return imageDownloadFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.getImageDownloadStatus(ctx, img)
	if err != nil {
		return imageDownloadFailureResp, err
	}
	return resp, nil
}

// GetImageDownload returns image download
func (dMgr *Manager) GetImageDownload(ctx context.Context, img *voltha.ImageDownload) (*voltha.ImageDownload, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetImageDownload")
	log.EnrichSpan(ctx, log.Fields{"device-id": img.Id})

	logger.Debugw(ctx, "get-image-download", log.Fields{"device-id": img.Id, "image-name": img.Name})
	agent := dMgr.getDeviceAgent(ctx, img.Id)
	if agent == nil {
		return imageDownloadFailureResp, status.Errorf(codes.NotFound, "%s", img.Id)
	}
	resp, err := agent.getImageDownload(ctx, img)
	if err != nil {
		return imageDownloadFailureResp, err
	}
	return resp, nil
}

// ListImageDownloads returns image downloads
func (dMgr *Manager) ListImageDownloads(ctx context.Context, id *voltha.ID) (*voltha.ImageDownloads, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListImageDownloads")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "list-image-downloads", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return &voltha.ImageDownloads{Items: []*voltha.ImageDownload{imageDownloadFailureResp}}, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	resp, err := agent.listImageDownloads(ctx, id.Id)
	if err != nil {
		return &voltha.ImageDownloads{Items: []*voltha.ImageDownload{imageDownloadFailureResp}}, err
	}
	return resp, nil
}

// GetImages returns all images for a specific device entry
func (dMgr *Manager) GetImages(ctx context.Context, id *voltha.ID) (*voltha.Images, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetImages")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "get-images", log.Fields{"device-id": id.Id})
	device, err := dMgr.getDeviceReadOnly(ctx, id.Id)
	if err != nil {
		return nil, err
	}
	return device.Images, nil
}

// ListDevicePorts returns the ports details for a specific device entry
func (dMgr *Manager) ListDevicePorts(ctx context.Context, id *voltha.ID) (*voltha.Ports, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDevicePorts")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	logger.Debugw(ctx, "list-device-ports", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", id.Id)
	}

	ports := agent.listDevicePorts()
	ctr, ret := 0, make([]*voltha.Port, len(ports))
	for _, port := range ports {
		ret[ctr] = port
		ctr++
	}
	return &voltha.Ports{Items: ret}, nil
}

// ListDevicePmConfigs returns pm configs of device
func (dMgr *Manager) ListDevicePmConfigs(ctx context.Context, id *voltha.ID) (*voltha.PmConfigs, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDevicePmConfigs")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})

	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", id.Id)
	}
	return agent.listPmConfigs(ctx)
}

// UpdateDevicePmConfigs updates the PM configs.  This is executed when the northbound gRPC API is invoked, typically
// following a user action
func (dMgr *Manager) UpdateDevicePmConfigs(ctx context.Context, configs *voltha.PmConfigs) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "UpdateDevicePmConfigs")
	log.EnrichSpan(ctx, log.Fields{"device-id": configs.Id})

	if configs.Id == "" {
		return nil, status.Error(codes.FailedPrecondition, "invalid-device-Id")
	}
	agent := dMgr.getDeviceAgent(ctx, configs.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", configs.Id)
	}
	return &empty.Empty{}, agent.updatePmConfigs(ctx, configs)
}

// ListDeviceFlows returns the flow details for a specific device entry
func (dMgr *Manager) ListDeviceFlows(ctx context.Context, id *voltha.ID) (*ofp.Flows, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDeviceFlows")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "list-device-flows", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", id.Id)
	}

	flows := agent.listDeviceFlows()
	ctr, ret := 0, make([]*ofp.OfpFlowStats, len(flows))
	for _, flow := range flows {
		ret[ctr] = flow
		ctr++
	}
	return &ofp.Flows{Items: ret}, nil
}

// ListDeviceFlowGroups returns the flow group details for a specific device entry
func (dMgr *Manager) ListDeviceFlowGroups(ctx context.Context, id *voltha.ID) (*voltha.FlowGroups, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "ListDeviceFlowGroups")
	log.EnrichSpan(ctx, log.Fields{"device-id": id.Id})
	logger.Debugw(ctx, "list-device-flow-groups", log.Fields{"device-id": id.Id})
	agent := dMgr.getDeviceAgent(ctx, id.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "device-%s", id.Id)
	}
	groups := agent.listDeviceGroups()
	ctr, ret := 0, make([]*ofp.OfpGroupEntry, len(groups))
	for _, group := range groups {
		ret[ctr] = group
		ctr++
	}
	return &voltha.FlowGroups{Items: ret}, nil
}

func (dMgr *Manager) EnablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "EnablePort")
	log.EnrichSpan(ctx, log.Fields{"device-id": port.DeviceId})

	logger.Debugw(ctx, "enable-port", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	agent := dMgr.getDeviceAgent(ctx, port.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", port.DeviceId)
	}
	return &empty.Empty{}, agent.enablePort(ctx, port.PortNo)
}

func (dMgr *Manager) DisablePort(ctx context.Context, port *voltha.Port) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "DisablePort")
	log.EnrichSpan(ctx, log.Fields{"device-id": port.DeviceId})

	logger.Debugw(ctx, "disable-port", log.Fields{"device-id": port.DeviceId, "port-no": port.PortNo})
	agent := dMgr.getDeviceAgent(ctx, port.DeviceId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", port.DeviceId)
	}
	return &empty.Empty{}, agent.disablePort(ctx, port.PortNo)
}

func (dMgr *Manager) GetExtValue(ctx context.Context, value *voltha.ValueSpecifier) (*voltha.ReturnValues, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "GetExtValue")
	log.EnrichSpan(ctx, log.Fields{"device-id": value.Id})

	logger.Debugw(ctx, "get-ext-value", log.Fields{"onu-id": value.Id})
	cDevice, err := dMgr.getDeviceReadOnly(ctx, value.Id)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	pDevice, err := dMgr.getDeviceReadOnly(ctx, cDevice.ParentId)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if agent := dMgr.getDeviceAgent(ctx, cDevice.ParentId); agent != nil {
		resp, err := agent.getExtValue(ctx, pDevice, cDevice, value)
		if err != nil {
			return nil, err
		}
		logger.Debugw(ctx, "get-ext-value-result", log.Fields{"result": resp})
		return resp, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", value.Id)

}

// SetExtValue  set some given configs or value
func (dMgr *Manager) SetExtValue(ctx context.Context, value *voltha.ValueSet) (*empty.Empty, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "SetExtValue")
	logger.Debugw(ctx, "set-ext-value", log.Fields{"onu-id": value.Id})

	device, err := dMgr.getDeviceReadOnly(ctx, value.Id)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "%s", err.Error())
	}
	if agent := dMgr.getDeviceAgent(ctx, device.Id); agent != nil {
		resp, err := agent.setExtValue(ctx, device, value)
		if err != nil {
			return nil, err
		}
		logger.Debugw(ctx, "set-ext-value-result", log.Fields{"result": resp})
		return resp, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s", value.Id)

}

func (dMgr *Manager) StartOmciTestAction(ctx context.Context, request *voltha.OmciTestRequest) (*voltha.TestResponse, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "StartOmciTestAction")
	log.EnrichSpan(ctx, log.Fields{"device-id": request.Id})

	logger.Debugw(ctx, "start-omci-test-action", log.Fields{"device-id": request.Id, "uuid": request.Uuid})
	agent := dMgr.getDeviceAgent(ctx, request.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", request.Id)
	}
	return agent.startOmciTest(ctx, request)
}

func (dMgr *Manager) SimulateAlarm(ctx context.Context, simulateReq *voltha.SimulateAlarmRequest) (*common.OperationResp, error) {
	ctx = utils.WithRPCMetadataContext(ctx, "SimulateAlarm")

	logger.Debugw(ctx, "simulate-alarm", log.Fields{"id": simulateReq.Id, "indicator": simulateReq.Indicator, "intf-id": simulateReq.IntfId,
		"port-type-name": simulateReq.PortTypeName, "onu-device-id": simulateReq.OnuDeviceId, "inverse-bit-error-rate": simulateReq.InverseBitErrorRate,
		"drift": simulateReq.Drift, "new-eqd": simulateReq.NewEqd, "onu-serial-number": simulateReq.OnuSerialNumber, "operation": simulateReq.Operation})
	agent := dMgr.getDeviceAgent(ctx, simulateReq.Id)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "%s", simulateReq.Id)
	}
	if err := agent.simulateAlarm(ctx, simulateReq); err != nil {
		return nil, err
	}
	return &common.OperationResp{Code: common.OperationResp_OPERATION_SUCCESS}, nil
}
