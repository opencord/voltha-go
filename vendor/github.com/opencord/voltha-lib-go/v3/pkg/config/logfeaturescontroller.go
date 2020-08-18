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

package config

import (
	"context"
	"errors"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"os"
	"strings"
)

const (
	defaultTracingStatusKey = "trace_publish" // kvstore key containing tracing configuration status
)

// ComponentLogFeatureController represents Configuration for Logging related features of Tracing and Log
// Correlation of specific Voltha component.
type ComponentLogFeaturesController struct {
	ComponentName        string
	componentNameConfig  *ComponentConfig
	configManager        *ConfigManager
	initialTracingStatus bool // Initial default tracing status set by helm chart
}

func NewComponentLogFeaturesController(ctx context.Context, cm *ConfigManager) (*ComponentLogFeaturesController, error) {
	logger.Debug(ctx, "creating-new-component-log-features-controller")
	componentName := os.Getenv("COMPONENT_NAME")
	if componentName == "" {
		return nil, errors.New("Unable to retrieve PoD Component Name from Runtime env")
	}

	tracingStatus := log.GetGlobalLFM().GetTracePublishingStatus()

	return &ComponentLogFeaturesController{
		ComponentName:        componentName,
		componentNameConfig:  nil,
		configManager:        cm,
		initialTracingStatus: tracingStatus,
	}, nil

}

// StartLogFeaturesConfigProcessing persists initial config of Log Features into Config Store before
// starting the loading and processing of Configuration updates
func StartLogFeaturesConfigProcessing(cm *ConfigManager, ctx context.Context) {
	cc, err := NewComponentLogFeaturesController(ctx, cm)
	if err != nil {
		logger.Errorw(ctx, "unable-to-construct-component-log-features-controller-instance-for-monitoring", log.Fields{"error": err})
		return
	}

	cc.componentNameConfig = cm.InitComponentConfig(cc.ComponentName, ConfigTypeLogFeatures)
	logger.Debugw(ctx, "component-log-features-config", log.Fields{"cc-component-name-config": cc.componentNameConfig})

	cc.persistInitialLogFeaturesConfigs(ctx)

	cc.processLogFeaturesConfig(ctx)
}

// Method to persist Initial status of Log Correlation and Tracing features (as set from command line)
// into config store (etcd kvstore), if not set yet
func (cc *ComponentLogFeaturesController) persistInitialLogFeaturesConfigs(ctx context.Context) {

	_, err := cc.componentNameConfig.Retrieve(ctx, defaultTracingStatusKey)
	if err != nil {
		statusString := "DISABLED"
		if cc.initialTracingStatus {
			statusString = "ENABLED"
		}
		err = cc.componentNameConfig.Save(ctx, defaultTracingStatusKey, statusString)
		if err != nil {
			logger.Errorw(ctx, "failed-to-persist-component-initial-tracing-status-at-startup", log.Fields{"error": err, "tracingstatus": statusString})
		}
	}
}

// processLogFeaturesConfig will first load and apply configuration of log features. Then it will start waiting for any changes
// made to configuration in config store (etcd) and apply the same
func (cc *ComponentLogFeaturesController) processLogFeaturesConfig(ctx context.Context) {

	// Load and apply Tracing Status for first time
	cc.loadAndApplyTracingStatusUpdate(ctx)

	componentConfigEventChan := cc.componentNameConfig.MonitorForConfigChange(ctx)

	// process the change events received on the channel
	var configEvent *ConfigChangeEvent
	for {
		select {
		case <-ctx.Done():
			return

		case configEvent = <-componentConfigEventChan:
			logger.Debugw(ctx, "processing-log-features-config-change", log.Fields{"ChangeType": configEvent.ChangeType, "Package": configEvent.ConfigAttribute})

			if strings.HasSuffix(configEvent.ConfigAttribute, defaultTracingStatusKey) {
				cc.loadAndApplyTracingStatusUpdate(ctx)
			}
		}
	}

}

func (cc *ComponentLogFeaturesController) loadAndApplyTracingStatusUpdate(ctx context.Context) {

	desiredTracingStatus, err := cc.componentNameConfig.Retrieve(ctx, defaultTracingStatusKey)
	if err != nil || desiredTracingStatus == "" {
		logger.Warn(ctx, "unable-to-retrieve-tracing-status-from-config-store")
		return
	}

	if desiredTracingStatus != "ENABLED" && desiredTracingStatus != "DISABLED" {
		logger.Warnw(ctx, "unsupported-tracing-status-configured-in-config-store", log.Fields{"tracing-status": desiredTracingStatus})
		return
	}

	logger.Debugw(ctx, "retrieved-tracing-status", log.Fields{"tracing-status": desiredTracingStatus})

	log.GetGlobalLFM().SetTracePublishingStatus(desiredTracingStatus == "ENABLED")
}
