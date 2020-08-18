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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
)

const (
	defaultkvStoreConfigPath     = "config"
	defaultkvStoreDataPathPrefix = "service/voltha"
	kvStorePathSeparator         = "/"
)

// ConfigType represents the type for which config is created inside the kvstore
// For example, loglevel
type ConfigType int

const (
	ConfigTypeLogLevel ConfigType = iota
	ConfigTypeMetadata
	ConfigTypeKafka
	ConfigTypeLogFeatures
)

func (c ConfigType) String() string {
	return [...]string{"loglevel", "metadata", "kafka", "logfeatures"}[c]
}

// ChangeEvent represents the event recieved from watch
// For example, Put Event
type ChangeEvent int

const (
	Put ChangeEvent = iota
	Delete
)

func (ce ChangeEvent) String() string {
	return [...]string{"Put", "Delete"}[ce]
}

// ConfigChangeEvent represents config for the events recieved from watch
// For example,ChangeType is Put ,ConfigAttribute default
type ConfigChangeEvent struct {
	ChangeType      ChangeEvent
	ConfigAttribute string
}

// ConfigManager is a wrapper over Backend to maintain Configuration of voltha components
// in kvstore based persistent storage
type ConfigManager struct {
	Backend               *db.Backend
	KVStoreConfigPrefix   string
	KVStoreDataPathPrefix string
}

// ComponentConfig represents a category of configuration for a specific VOLTHA component type
// stored in a persistent storage pointed to by Config Manager
// For example, one ComponentConfig instance will be created for loglevel config type for rw-core
// component while another ComponentConfig instance will refer to connection config type for same
// rw-core component. So, there can be multiple ComponentConfig instance created per component
// pointing to different category of configuration.
//
// Configuration pointed to be by ComponentConfig is stored in kvstore as a list of key/value pairs
// under the hierarchical tree with following base path
// <Backend Prefix Path>/<Config Prefix>/<Component Name>/<Config Type>/
//
// For example, rw-core ComponentConfig for loglevel config entries will be stored under following path
// /voltha/service/config/rw-core/loglevel/
type ComponentConfig struct {
	cManager         *ConfigManager
	componentLabel   string
	configType       ConfigType
	changeEventChan  chan *ConfigChangeEvent
	kvStoreEventChan chan *kvstore.Event
}

func NewConfigManager(ctx context.Context, kvClient kvstore.Client, kvStoreType, kvStoreAddress string, kvStoreTimeout time.Duration) *ConfigManager {
	var kvStorePrefix string
	if prefix, present := os.LookupEnv("KV_STORE_DATAPATH_PREFIX"); present {
		kvStorePrefix = prefix
		logger.Infow(ctx, "KV_STORE_DATAPATH_PREFIX env variable is set, ", log.Fields{"kvStoreDataPathPrefix": kvStorePrefix})
	} else {
		kvStorePrefix = defaultkvStoreDataPathPrefix
		logger.Infow(ctx, "KV_STORE_DATAPATH_PREFIX env variable is not set, using default", log.Fields{"kvStoreDataPathPrefix": defaultkvStoreDataPathPrefix})
	}
	return &ConfigManager{
		KVStoreConfigPrefix:   defaultkvStoreConfigPath,
		KVStoreDataPathPrefix: kvStorePrefix,
		Backend: &db.Backend{
			Client:     kvClient,
			StoreType:  kvStoreType,
			Address:    kvStoreAddress,
			Timeout:    kvStoreTimeout,
			PathPrefix: kvStorePrefix,
		},
	}
}

// RetrieveComponentList list the component Names for which loglevel is stored in kvstore
func (c *ConfigManager) RetrieveComponentList(ctx context.Context, configType ConfigType) ([]string, error) {
	data, err := c.Backend.List(ctx, c.KVStoreConfigPrefix)
	if err != nil {
		return nil, err
	}

	// Looping through the data recieved from the Backend for config
	// Trimming and Splitting the required key and value from data and  storing as componentName,PackageName and Level
	// For Example, recieved key would be <Backend Prefix Path>/<Config Prefix>/<Component Name>/<Config Type>/default and value \"DEBUG\"
	// Then in default will be stored as PackageName,componentName as <Component Name> and DEBUG will be stored as value in List struct
	ccPathPrefix := kvStorePathSeparator + configType.String() + kvStorePathSeparator
	pathPrefix := c.KVStoreDataPathPrefix + kvStorePathSeparator + c.KVStoreConfigPrefix + kvStorePathSeparator
	var list []string
	keys := make(map[string]interface{})
	for attr := range data {
		cname := strings.TrimPrefix(attr, pathPrefix)
		cName := strings.SplitN(cname, ccPathPrefix, 2)
		if len(cName) != 2 {
			continue
		}
		if _, exist := keys[cName[0]]; !exist {
			keys[cName[0]] = nil
			list = append(list, cName[0])
		}
	}
	return list, nil
}

// Initialize the component config
func (cm *ConfigManager) InitComponentConfig(componentLabel string, configType ConfigType) *ComponentConfig {

	return &ComponentConfig{
		componentLabel:   componentLabel,
		configType:       configType,
		cManager:         cm,
		changeEventChan:  nil,
		kvStoreEventChan: nil,
	}

}

func (c *ComponentConfig) makeConfigPath() string {

	cType := c.configType.String()
	return c.cManager.KVStoreConfigPrefix + kvStorePathSeparator +
		c.componentLabel + kvStorePathSeparator + cType
}

// MonitorForConfigChange watch on the subkeys for the given key
// Any changes to the subkeys for the given key will return an event channel
// Then Event channel will be processed and  new event channel with required values will be created and return
// For example, rw-core will be watching on <Backend Prefix Path>/<Config Prefix>/<Component Name>/<Config Type>/
// will return an event channel for PUT,DELETE eventType.
// Then values from event channel will be processed and  stored in kvStoreEventChan.
func (c *ComponentConfig) MonitorForConfigChange(ctx context.Context) chan *ConfigChangeEvent {
	key := c.makeConfigPath()

	logger.Debugw(ctx, "monitoring-for-config-change", log.Fields{"key": key})

	c.changeEventChan = make(chan *ConfigChangeEvent, 1)

	c.kvStoreEventChan = c.cManager.Backend.CreateWatch(ctx, key, true)

	go c.processKVStoreWatchEvents(ctx)

	return c.changeEventChan
}

// processKVStoreWatchEvents process event channel recieved from the Backend for any ChangeType
// It checks for the EventType is valid or not.For the valid EventTypes creates ConfigChangeEvent and send it on channel
func (c *ComponentConfig) processKVStoreWatchEvents(ctx context.Context) {

	ccKeyPrefix := c.makeConfigPath()

	logger.Debugw(ctx, "processing-kvstore-event-change", log.Fields{"key-prefix": ccKeyPrefix})

	ccPathPrefix := c.cManager.Backend.PathPrefix + ccKeyPrefix + kvStorePathSeparator

	for watchResp := range c.kvStoreEventChan {

		if watchResp.EventType == kvstore.CONNECTIONDOWN || watchResp.EventType == kvstore.UNKNOWN {
			logger.Warnw(ctx, "received-invalid-change-type-in-watch-channel-from-kvstore", log.Fields{"change-type": watchResp.EventType})
			continue
		}

		// populating the configAttribute from the received Key
		// For Example, Key received would be <Backend Prefix Path>/<Config Prefix>/<Component Name>/<Config Type>/default
		// Storing default in configAttribute variable
		ky := fmt.Sprintf("%s", watchResp.Key)

		c.changeEventChan <- &ConfigChangeEvent{
			ChangeType:      ChangeEvent(watchResp.EventType),
			ConfigAttribute: strings.TrimPrefix(ky, ccPathPrefix),
		}
	}
}

// Retrieves value of a specific config key. Value of key is returned in String format
func (c *ComponentConfig) Retrieve(ctx context.Context, configKey string) (string, error) {
	key := c.makeConfigPath() + "/" + configKey

	logger.Debugw(ctx, "retrieving-config", log.Fields{"key": key})

	if kvpair, err := c.cManager.Backend.Get(ctx, key); err != nil {
		return "", err
	} else {
		if kvpair == nil {
			return "", fmt.Errorf("config-key-does-not-exist : %s", key)
		}

		value := strings.Trim(fmt.Sprintf("%s", kvpair.Value), "\"")
		logger.Debugw(ctx, "retrieved-config", log.Fields{"key": key, "value": value})
		return value, nil
	}
}

func (c *ComponentConfig) RetrieveAll(ctx context.Context) (map[string]string, error) {
	key := c.makeConfigPath()

	logger.Debugw(ctx, "retreiving-list", log.Fields{"key": key})

	data, err := c.cManager.Backend.List(ctx, key)
	if err != nil {
		return nil, err
	}

	// Looping through the data recieved from the Backend for the given key
	// Trimming the required key and value from data and  storing as key/value pair
	// For Example, recieved key would be <Backend Prefix Path>/<Config Prefix>/<Component Name>/<Config Type>/default and value \"DEBUG\"
	// Then in default will be stored as key and DEBUG will be stored as value in map[string]string
	res := make(map[string]string)
	ccPathPrefix := c.cManager.Backend.PathPrefix + kvStorePathSeparator + key + kvStorePathSeparator
	for attr, val := range data {
		res[strings.TrimPrefix(attr, ccPathPrefix)] = strings.Trim(fmt.Sprintf("%s", val.Value), "\"")
	}

	return res, nil
}

func (c *ComponentConfig) Save(ctx context.Context, configKey string, configValue string) error {
	key := c.makeConfigPath() + "/" + configKey

	logger.Debugw(ctx, "saving-config", log.Fields{"key": key, "value": configValue})

	//save the data for update config
	if err := c.cManager.Backend.Put(ctx, key, configValue); err != nil {
		return err
	}
	return nil
}

func (c *ComponentConfig) Delete(ctx context.Context, configKey string) error {
	//construct key using makeConfigPath
	key := c.makeConfigPath() + "/" + configKey

	logger.Debugw(ctx, "deleting-config", log.Fields{"key": key})
	//delete the config
	if err := c.cManager.Backend.Delete(ctx, key); err != nil {
		return err
	}
	return nil
}
