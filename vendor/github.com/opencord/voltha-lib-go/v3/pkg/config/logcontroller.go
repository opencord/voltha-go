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

// Package Config provides dynamic logging configuration for specific Voltha component with loglevel lookup
// from etcd kvstore implemented using backend.
// Any Voltha component can start utilizing dynamic logging by starting goroutine of StartLogLevelConfigProcessing after
// starting kvClient for the component.

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

const (
	defaultLogLevelKey                = "default" // kvstore key containing default loglevel
	globalConfigRootNode              = "global"  // Root Node in kvstore containing global config
	initialGlobalDefaultLogLevelValue = "WARN"    // Hard-coded Global Default loglevel pushed at PoD startup
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
	initialLogLevel     string // Initial default log level set by helm chart
}

func NewComponentLogController(cm *ConfigManager) (*ComponentLogController, error) {

	log.Debug("creating-new-component-log-controller")
	componentName := os.Getenv("COMPONENT_NAME")
	if componentName == "" {
		return nil, errors.New("Unable to retrieve PoD Component Name from Runtime env")
	}

	var defaultLogLevel string
	var err error
	// Retrieve and save default log level; used for fallback if all loglevel config is cleared in etcd
	if defaultLogLevel, err = log.LogLevelToString(log.GetDefaultLogLevel()); err != nil {
		defaultLogLevel = "DEBUG"
	}

	return &ComponentLogController{
		ComponentName:       componentName,
		componentNameConfig: nil,
		GlobalConfig:        nil,
		configManager:       cm,
		initialLogLevel:     defaultLogLevel,
	}, nil

}

// StartLogLevelConfigProcessing initialize component config and global config
// Then, it persists initial default Loglevels into Config Store before
// starting the loading and processing of all Log Configuration
func StartLogLevelConfigProcessing(cm *ConfigManager, ctx context.Context) {
	cc, err := NewComponentLogController(cm)
	if err != nil {
		log.Errorw("unable-to-construct-component-log-controller-instance-for-log-config-monitoring", log.Fields{"error": err})
		return
	}

	cc.GlobalConfig = cm.InitComponentConfig(globalConfigRootNode, ConfigTypeLogLevel)
	log.Debugw("global-log-config", log.Fields{"cc-global-config": cc.GlobalConfig})

	cc.componentNameConfig = cm.InitComponentConfig(cc.ComponentName, ConfigTypeLogLevel)
	log.Debugw("component-log-config", log.Fields{"cc-component-name-config": cc.componentNameConfig})

	cc.persistInitialDefaultLogConfigs(ctx)

	cc.processLogConfig(ctx)
}

// Method to persist Global default loglevel into etcd, if not set yet
// It also checks and set Component default loglevel into etcd with initial loglevel set from command line
func (c *ComponentLogController) persistInitialDefaultLogConfigs(ctx context.Context) {

	_, err := c.GlobalConfig.Retrieve(ctx, defaultLogLevelKey)
	if err != nil {
		log.Debugw("failed-to-retrieve-global-default-log-config-at-startup", log.Fields{"error": err})

		err = c.GlobalConfig.Save(ctx, defaultLogLevelKey, initialGlobalDefaultLogLevelValue)
		if err != nil {
			log.Errorw("failed-to-persist-global-default-log-config-at-startup", log.Fields{"error": err, "loglevel": initialGlobalDefaultLogLevelValue})
		}
	}

	_, err = c.componentNameConfig.Retrieve(ctx, defaultLogLevelKey)
	if err != nil {
		log.Debugw("failed-to-retrieve-component-default-log-config-at-startup", log.Fields{"error": err})

		err = c.componentNameConfig.Save(ctx, defaultLogLevelKey, c.initialLogLevel)
		if err != nil {
			log.Errorw("failed-to-persist-component-default-log-config-at-startup", log.Fields{"error": err, "loglevel": c.initialLogLevel})
		}
	}
}

// ProcessLogConfig will first load and apply log config and then start waiting on component config and global config
// channels for any changes. Event channel will be recieved from backend for valid change type
// Then data for componentn log config and global log config will be retrieved from backend and stored in updatedLogConfig in precedence order
// If any changes in updatedLogConfig will be applied on component
func (c *ComponentLogController) processLogConfig(ctx context.Context) {

	// Load and apply Log Config for first time
	initialLogConfig, err := c.buildUpdatedLogConfig(ctx)
	if err != nil {
		log.Warnw("unable-to-load-log-config-at-startup", log.Fields{"error": err})
	} else {
		if err := c.loadAndApplyLogConfig(initialLogConfig); err != nil {
			log.Warnw("unable-to-apply-log-config-at-startup", log.Fields{"error": err})
		}
	}

	componentConfigEventChan := c.componentNameConfig.MonitorForConfigChange(ctx)

	globalConfigEventChan := c.GlobalConfig.MonitorForConfigChange(ctx)

	// process the events for componentName and  global config
	var configEvent *ConfigChangeEvent
	for {
		select {
		case configEvent = <-globalConfigEventChan:
		case configEvent = <-componentConfigEventChan:

		}
		log.Debugw("processing-log-config-change", log.Fields{"ChangeType": configEvent.ChangeType, "Package": configEvent.ConfigAttribute})

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
func getActiveLogLevels() map[string]string {
	loglevels := make(map[string]string)

	// now do the default log level
	if level, err := log.LogLevelToString(log.GetDefaultLogLevel()); err == nil {
		loglevels[defaultLogLevelKey] = level
	}

	// do the per-package log levels
	for _, packageName := range log.GetPackageNames() {
		level, err := log.GetPackageLogLevel(packageName)
		if err != nil {
			log.Warnw("unable-to-fetch-current-active-loglevel-for-package-name", log.Fields{"package-name": packageName, "error": err})
			continue
		}

		if l, err := log.LogLevelToString(level); err == nil {
			loglevels[packageName] = l
		}
	}

	log.Debugw("retreived-log-levels-from-zap-logger", log.Fields{"loglevels": loglevels})

	return loglevels
}

func (c *ComponentLogController) getGlobalLogConfig(ctx context.Context) (string, error) {

	globalDefaultLogLevel, err := c.GlobalConfig.Retrieve(ctx, defaultLogLevelKey)
	if err != nil {
		return "", err
	}

	// Handle edge cases when global default loglevel is deleted directly from etcd or set to a invalid value
	// We should use hard-coded initial default value in such cases
	if globalDefaultLogLevel == "" {
		log.Warn("global-default-loglevel-not-found-in-config-store")
		globalDefaultLogLevel = initialGlobalDefaultLogLevelValue
	}

	if _, err := log.StringToLogLevel(globalDefaultLogLevel); err != nil {
		log.Warnw("unsupported-loglevel-config-defined-at-global-default", log.Fields{"log-level": globalDefaultLogLevel})
		globalDefaultLogLevel = initialGlobalDefaultLogLevelValue
	}

	log.Debugw("retrieved-global-default-loglevel", log.Fields{"level": globalDefaultLogLevel})

	return globalDefaultLogLevel, nil
}

func (c *ComponentLogController) getComponentLogConfig(ctx context.Context, globalDefaultLogLevel string) (map[string]string, error) {
	componentLogConfig, err := c.componentNameConfig.RetrieveAll(ctx)
	if err != nil {
		return nil, err
	}

	effectiveDefaultLogLevel := ""
	for logConfigKey, logConfigValue := range componentLogConfig {
		if _, err := log.StringToLogLevel(logConfigValue); err != nil || logConfigKey == "" {
			log.Warnw("unsupported-loglevel-config-defined-at-component-context", log.Fields{"package-name": logConfigKey, "log-level": logConfigValue})
			delete(componentLogConfig, logConfigKey)
		} else {
			if logConfigKey == defaultLogLevelKey {
				effectiveDefaultLogLevel = componentLogConfig[defaultLogLevelKey]
			}
		}
	}

	// if default loglevel is not configured for the component, component should use
	// default loglevel configured at global level
	if effectiveDefaultLogLevel == "" {
		effectiveDefaultLogLevel = globalDefaultLogLevel
	}

	componentLogConfig[defaultLogLevelKey] = effectiveDefaultLogLevel

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
		log.Errorw("unable-to-retrieve-global-log-config", log.Fields{"err": err})
	}

	componentLogConfig, err := c.getComponentLogConfig(ctx, globalLogLevel)
	if err != nil {
		return nil, err
	}

	finalLogConfig := make(map[string]string)
	for packageName, logLevel := range componentLogConfig {
		finalLogConfig[strings.ReplaceAll(packageName, "#", "/")] = logLevel
	}

	return finalLogConfig, nil
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

	if c.logHash != currentLogHash {
		UpdateLogLevels(logConfig)
		c.logHash = currentLogHash
	} else {
		log.Debug("effective-loglevel-config-same-as-currently-active")
	}

	return nil
}

// createModifiedLogLevels loops through the activeLogLevels recieved from zap logger and updatedLogLevels recieved from buildUpdatedLogConfig
// to identify and create map of modified Log Levels of 2 types:
// - Packages for which log level has been changed
// - Packages for which log level config has been cleared - set to default log level
func createModifiedLogLevels(activeLogLevels, updatedLogLevels map[string]string) map[string]string {
	defaultLevel := updatedLogLevels[defaultLogLevelKey]

	modifiedLogLevels := make(map[string]string)
	for activeKey, activeLevel := range activeLogLevels {
		if _, exist := updatedLogLevels[activeKey]; !exist {
			if activeLevel != defaultLevel {
				modifiedLogLevels[activeKey] = defaultLevel
			}
		} else if activeLevel != updatedLogLevels[activeKey] {
			modifiedLogLevels[activeKey] = updatedLogLevels[activeKey]
		}
	}

	// Log warnings for all invalid packages for which log config has been set
	for key, value := range updatedLogLevels {
		if _, exist := activeLogLevels[key]; !exist {
			log.Warnw("ignoring-loglevel-set-for-invalid-package", log.Fields{"package": key, "log-level": value})
		}
	}

	return modifiedLogLevels
}

// updateLogLevels update the loglevels for the component
// retrieve active confguration from logger
// compare with entries one by one and apply
func UpdateLogLevels(updatedLogConfig map[string]string) {

	activeLogLevels := getActiveLogLevels()
	changedLogLevels := createModifiedLogLevels(activeLogLevels, updatedLogConfig)

	// If no changed log levels are found, just return. It may happen on configuration of a invalid package
	if len(changedLogLevels) == 0 {
		log.Debug("no-change-in-effective-loglevel-config")
		return
	}

	log.Debugw("applying-log-level-for-modified-packages", log.Fields{"changed-log-levels": changedLogLevels})
	for key, level := range changedLogLevels {
		if key == defaultLogLevelKey {
			if l, err := log.StringToLogLevel(level); err == nil {
				log.SetDefaultLogLevel(l)
			}
		} else {
			if l, err := log.StringToLogLevel(level); err == nil {
				log.SetPackageLogLevel(key, l)
			}
		}
	}
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
