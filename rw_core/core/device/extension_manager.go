package device

import (
	"context"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/extension"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ExtensionManager struct {
	DeviceManager *Manager
}

func GetNewExtensionManager(deviceManager *Manager) *ExtensionManager {
	return &ExtensionManager{DeviceManager:deviceManager}
}

func (e ExtensionManager) GetExtValue(ctx context.Context, request *extension.SingleGetValueRequest) (*extension.SingleGetValueResponse, error) {
	log.EnrichSpan(ctx, log.Fields{"device-id": request.TargetId})

	logger.Debugw(ctx, "GetExtValue", log.Fields{"request": request})
	agent := e.DeviceManager.getDeviceAgent(ctx, request.TargetId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "target-id %s", request.TargetId)
	}

	response, err := agent.getSingleValue(ctx, request)
	if err != nil {
		logger.Errorw(ctx, "Fail-to-get-single-value", log.Fields{"device-id": agent.deviceID, "error": err})
		return nil, err
	}

	logger.Debugw(ctx, "GetExtValue response", log.Fields{"response": response})
	return response, nil
}

func (e ExtensionManager) SetExtValue(ctx context.Context, request *extension.SingleSetValueRequest) (*extension.SingleSetValueResponse, error) {
	log.EnrichSpan(ctx, log.Fields{"device-id": request.TargetId})

	logger.Debugw(ctx, "SetExtValue", log.Fields{"request": request})
	agent := e.DeviceManager.getDeviceAgent(ctx, request.TargetId)
	if agent == nil {
		return nil, status.Errorf(codes.NotFound, "target-id %s", request.TargetId)
	}

	response, err := agent.setSingleValue(ctx, request)
	if err != nil {
		logger.Errorw(ctx, "Fail-to-set-single-value", log.Fields{"device-id": agent.deviceID, "error": err})
		return nil, err
	}

	logger.Debugw(ctx, "SetExtValue response", log.Fields{"response": response})
	return response, nil
}

