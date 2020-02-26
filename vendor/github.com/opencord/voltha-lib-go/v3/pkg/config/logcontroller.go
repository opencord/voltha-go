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

// Package Config provides dynamic logging configuration for specific Voltha component type implemented using backend.The package can be used in following manner
// Any Voltha component type can start dynamic logging by starting goroutine of ProcessLogConfigChange after starting kvClient for the component.

package config

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"os"
	"strings"
)

// ComponentLogController represents a Configuration for Logging Config of specific Voltha component type
// It stores ComponentConfig and GlobalConfig of loglevel config of specific Voltha component type
// For example,ComponentLogController instance will be created for rw-core component
type ComponentLogController struct {
	ComponentName       string
	componentNameConfig *ComponentConfig
	GlobalConfig        *ComponentConfig
	configManager       *ConfigManager
	logHash             [16]byte
}

func NewComponentLogController(cm *ConfigManager) (*ComponentLogController, error) {

	log.Debug("creating-new-component-log-controller")
	componentName := os.Getenv("COMPONENT_NAME")
	if componentName == "" {
		return nil, errors.New("Unable to retrieve PoD Component Name from Runtime env")
	}

	return &ComponentLogController{
		ComponentName:       componentName,
		componentNameConfig: nil,
		GlobalConfig:        nil,
		configManager:       cm,
	}, nil

}

// ProcessLogConfigChange initialize component config and global config
func ProcessLogConfigChange(cm *ConfigManager, ctx context.Context) {
	cc, err := NewComponentLogController(cm)
	if err != nil {
		log.Errorw("unable-to-construct-component-log-controller-instance-for-log-config-monitoring", log.Fields{"error": err})
		return
	}

	log.Debugw("processing-log-config-change", log.Fields{"cc": cc})

	cc.GlobalConfig = cm.InitComponentConfig("global", ConfigTypeLogLevel)
	log.Debugw("global-log-config", log.Fields{"cc-global-config": cc.GlobalConfig})

	cc.componentNameConfig = cm.InitComponentConfig(cc.ComponentName, ConfigTypeLogLevel)
	log.Debugw("component-log-config", log.Fields{"cc-component-name-config": cc.componentNameConfig})

	cc.processLogConfig(ctx)
}

// ProcessLogConfig wait on componentn config and global config channel for any changes
// Event channel will be recieved from backend for valid change type
// Then data for componentn log config and global log config will be retrieved from backend and stored in updatedLogConfig in precedence order
// If any changes in updatedLogConfig will be applied on component
func (c *ComponentLogController) processLogConfig(ctx context.Context) {

	componentConfigEventChan := c.componentNameConfig.MonitorForConfigChange(ctx)

	globalConfigEventChan := c.GlobalConfig.MonitorForConfigChange(ctx)

	// process the events for componentName and  global config
	var configEvent *ConfigChangeEvent
	for {
		select {
		case configEvent = <-globalConfigEventChan:
		case configEvent = <-componentConfigEventChan:

		}
		log.Debugw("processing-log-config-change", log.Fields{"config-event": configEvent})

		updatedLogConfig, err := c.buildUpdatedLogConfig(ctx)
		if err != nil {
			log.Warnw("unable-to-fetch-updated-log-config", log.Fields{"error": err})
			continue
		}

		log.Debugw("applying-updated-log-config", log.Fields{"updated-log-config": updatedLogConfig})

		if err := c.loadAndApplyLogConfig(updatedLogConfig); err != nil {
			log.Warnw("unable-to-load-and-apply-log-config", log.Fields{"error": err})
		}
	}

}

// get active loglevel from the zap logger
func getActiveLogLevel() map[string]string {
	loglevel := make(map[string]string)

	// now do the default log level
	if level, err := log.LogLevelToString(log.GetDefaultLogLevel()); err == nil {
		loglevel["default"] = level
	}

	// do the per-package log levels
	for _, packageName := range log.GetPackageNames() {
		level, err := log.GetPackageLogLevel(packageName)
		if err != nil {
			log.Warnw("unable-to-fetch-current-active-loglevel-for-package-name", log.Fields{"package-name": packageName, "error": err})
		}

		packagename := strings.ReplaceAll(packageName, "/", "#")
		if l, err := log.LogLevelToString(level); err == nil {
			loglevel[packagename] = l
		}

	}
	log.Debugw("getting-log-levels-from-zap-logger", log.Fields{"log-level": loglevel})

	return loglevel
}

func (c *ComponentLogController) getGlobalLogConfig(ctx context.Context) (string, error) {

	globalDefaultLogLevel := ""
	globalLogConfig, err := c.GlobalConfig.RetrieveAll(ctx)
	if err != nil {
		return "", err
	}

	if globalLevel, ok := globalLogConfig["default"]; ok {
		if _, err := log.StringToLogLevel(globalLevel); err != nil {
			log.Warnw("unsupported-loglevel-config-defined-at-global-context-pacakge-name", log.Fields{"log-level": globalLevel})
		} else {
			globalDefaultLogLevel = globalLevel
		}
	}
	log.Debugw("retrieved-global-log-config", log.Fields{"global-log-config": globalLogConfig})

	return globalDefaultLogLevel, nil
}

func (c *ComponentLogController) getComponentLogConfig(globalDefaultLogLevel string, ctx context.Context) (map[string]string, error) {
	var defaultPresent bool
	componentLogConfig, err := c.componentNameConfig.RetrieveAll(ctx)
	if err != nil {
		return nil, err
	}

	for componentKey, componentLevel := range componentLogConfig {
		if _, err := log.StringToLogLevel(componentLevel); err != nil || componentKey == "" {
			log.Warnw("unsupported-loglevel-config-defined-at-component-context", log.Fields{"package-name": componentKey, "log-level": componentLevel})
			delete(componentLogConfig, componentKey)
		} else {
			if componentKey == "default" {
				defaultPresent = true
			}
		}
	}
	if !defaultPresent {
		if globalDefaultLogLevel != "" {
			componentLogConfig["default"] = globalDefaultLogLevel
		}
	}
	log.Debugw("retrieved-component-log-config", log.Fields{"component-log-level": componentLogConfig})

	return componentLogConfig, nil
}

// buildUpdatedLogConfig retrieve the global logConfig and component logConfig  from backend
// component logConfig stores the log config with precedence order
// For example, If the global logConfig is set and component logConfig is set only for specific package then
// component logConfig is stored with global logConfig  and component logConfig of specific package
// For example, If the global logConfig is set and component logConfig is set for specific package and as well as for default then
// component logConfig is stored with  component logConfig data only
func (c *ComponentLogController) buildUpdatedLogConfig(ctx context.Context) (map[string]string, error) {
	globalLogLevel, err := c.getGlobalLogConfig(ctx)
	if err != nil {
		return nil, err
	}

	componentLogConfig, err := c.getComponentLogConfig(globalLogLevel, ctx)
	if err != nil {
		return nil, err
	}

	log.Debugw("building-and-updating-log-config", log.Fields{"component-log-config": componentLogConfig})
	return componentLogConfig, nil
}

// load and apply the current configuration for component name
// create hash of loaded configuration using GenerateLogConfigHash
// if there is previous hash stored, compare the hash to stored hash
// if there is any change will call UpdateLogLevels
func (c *ComponentLogController) loadAndApplyLogConfig(logConfig map[string]string) error {
	currentLogHash, err := GenerateLogConfigHash(logConfig)
	if err != nil {
		return err
	}

	log.Debugw("loading-and-applying-log-config", log.Fields{"log-config": logConfig})
	if c.logHash != currentLogHash {
		UpdateLogLevels(logConfig)
		c.logHash = currentLogHash
	}
	return nil
}

// getDefaultLogLevel to return active default log level
func getDefaultLogLevel(logConfig map[string]string) string {

	for key, level := range logConfig {
		if key == "default" {
			return level
		}
	}
	return ""
}

// createCurrentLogLevel loop through the activeLogLevels recieved from zap logger and updatedLogLevels recieved from buildUpdatedLogConfig
// The packageName is present or not will be checked in updatedLogLevels ,if the package name is not present then updatedLogLevels will be updated with
// the packageName and loglevel with  default log level
func createCurrentLogLevel(activeLogLevels, updatedLogLevels map[string]string) map[string]string {
	level := getDefaultLogLevel(updatedLogLevels)
	for activeKey, activeLevel := range activeLogLevels {
		if _, exist := updatedLogLevels[activeKey]; !exist {
			if level != "" {
				activeLevel = level
			}
			updatedLogLevels[activeKey] = activeLevel
		}
	}
	return updatedLogLevels
}

// updateLogLevels update the loglevels for the component
// retrieve active confguration from logger
// compare with entries one by one and apply
func UpdateLogLevels(logLevel map[string]string) {

	activeLogLevels := getActiveLogLevel()
	currentLogLevel := createCurrentLogLevel(activeLogLevels, logLevel)
	for key, level := range currentLogLevel {
		if key == "default" {
			if l, err := log.StringToLogLevel(level); err == nil {
				log.SetDefaultLogLevel(l)
			}
		} else {
			pname := strings.ReplaceAll(key, "#", "/")
			if _, err := log.AddPackage(log.JSON, log.DebugLevel, nil, pname); err != nil {
				log.Warnw("unable-to-add-log-package", log.Fields{"package-name": pname, "error": err})
			}
			if l, err := log.StringToLogLevel(level); err == nil {
				log.SetPackageLogLevel(pname, l)
			}
		}
	}
	log.Debugw("updated-log-level", log.Fields{"current-log-level": currentLogLevel})
}

// generate md5 hash of key value pairs appended into a single string
// in order by key name
func GenerateLogConfigHash(createHashLog map[string]string) ([16]byte, error) {
	createHashLogBytes := []byte{}
	levelData, err := json.Marshal(createHashLog)
	if err != nil {
		return [16]byte{}, err
	}
	createHashLogBytes = append(createHashLogBytes, levelData...)
	return md5.Sum(createHashLogBytes), nil
}
