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

func TestNewDeviceAgent(t *testing.T) {

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
	core.deviceMgr = newDeviceManager(core.clusterDataProxy, core.instanceID)

	devAgent := newDeviceAgent(&voltha.Device{Id: "new_device"}, core.deviceMgr, core.clusterDataProxy)
	assert.NotNil(t, devAgent)

}
