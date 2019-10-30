/*
 * Copyright 2019-present Open Networking Foundation

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
package core

import (
	"testing"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/stretchr/testify/assert"
)

func MakeTestDevManagerConfig() (*Core, error) {
	var core Core
	core.instanceID = "ro_core"
	core.config = config.NewROCoreFlags()
	backend := db.Backend{
		Client:     core.kvClient,
		StoreType:  core.config.KVStoreType,
		Host:       core.config.KVStoreHost,
		Port:       core.config.KVStorePort,
		Timeout:    core.config.KVStoreTimeout,
		PathPrefix: "service/voltha"}
	core.clusterDataRoot = model.NewRoot(&voltha.Voltha{}, &backend)
	core.genericMgr = newModelProxyManager(core.clusterDataProxy)

	return &core, nil
}
func TestNewDeviceManager(t *testing.T) {

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)
}

func TestListDeviceIds(t *testing.T) {

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	myIds, _ := core.deviceMgr.ListDeviceIDs()
	assert.NotNil(t, myIds)
}

func TestGetDeviceAgent(t *testing.T) {

	// Tests also methods addDeviceAgentToMap,
	//                    listDeviceIdsFromMap
	//                    deleteDeviceAgentToMap

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

	// addDeviceAgentToMap
	core.deviceMgr.addDeviceAgentToMap(devAgent)

	// listDeviceIdsFromMap
	myIDs := core.deviceMgr.listDeviceIDsFromMap()
	assert.NotNil(t, myIDs)

	// getDeviceAgent
	myDevAgent := core.deviceMgr.getDeviceAgent(devAgent.deviceID)
	assert.NotNil(t, myDevAgent)

	// deleteDeviceAgentToMap
	core.deviceMgr.deleteDeviceAgentToMap(devAgent)
}

func TestLoadDevice(t *testing.T) {

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

	// addDeviceAgentToMap
	core.deviceMgr.addDeviceAgentToMap(devAgent)

	myDev, err := core.deviceMgr.loadDevice("new_device")
	assert.NotNil(t, myDev)
	assert.Nil(t, err)
}

func TestIsDeviceInCache(t *testing.T) {

	// Tests also methods addDeviceAgentToMap,
	//                    listDeviceIdsFromMap
	//                    deleteDeviceAgentToMap

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

	// addDeviceAgentToMap
	core.deviceMgr.addDeviceAgentToMap(devAgent)

	isInCache := core.deviceMgr.IsDeviceInCache(devAgent.deviceID)
	assert.True(t, isInCache)

	// deleteDeviceAgentToMap
	core.deviceMgr.deleteDeviceAgentToMap(devAgent)

	isInCacheDel := core.deviceMgr.IsDeviceInCache(devAgent.deviceID)
	assert.False(t, isInCacheDel)
}

func TestLoadRootDeviceParentAndChildren(t *testing.T) {

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

	// addDeviceAgentToMap
	core.deviceMgr.addDeviceAgentToMap(devAgent)

	err := core.deviceMgr.loadRootDeviceParentAndChildren(devAgent.lastData)
	assert.Nil(t, err)
}

func TestGetParentDevice(t *testing.T) {

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

	// addDeviceAgentToMap
	core.deviceMgr.addDeviceAgentToMap(devAgent)

	myDev := core.deviceMgr.getParentDevice(devAgent.lastData)
	assert.Nil(t, myDev)
}

func TestGetAllChildDeviceIds(t *testing.T) {

	core, _ := MakeTestDevManagerConfig()

	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)
	assert.NotNil(t, core.deviceMgr)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

	// addDeviceAgentToMap
	core.deviceMgr.addDeviceAgentToMap(devAgent)

	myIds, err := core.deviceMgr.getAllChildDeviceIDs(devAgent.lastData)
	assert.NotNil(t, myIds)
	assert.Nil(t, err)
}
