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
	"context"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"reflect"
	"strconv"
	"testing"
)

func MakeTestGrpcNbiConfig() (*Core, error) {
	var core *Core
	var roCoreFlgs *config.ROCoreFlags
	var roC *roCore

	freePort, errP := freeport.GetFreePort()
	if errP == nil {
		freePortStr := strconv.Itoa(freePort)

		roCoreFlgs = config.NewROCoreFlags()
		roC = newROCore(roCoreFlgs)
		if (roC != nil) && (roCoreFlgs != nil) {
			addr := "127.0.0.1" + ":" + freePortStr
			cli, err := newKVClient("etcd", addr, 5)
			if err == nil {
				roC.kvClient = cli
			}
		}
	}

	core = NewCore("ro_core", roCoreFlgs, roC.kvClient)

	return core, nil
}

// NewAPIHandler test
func TestNewAPIHandler(t *testing.T) {
	core, _ := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	// Check signature
	var fSign func(generalMgr *ModelProxyManager, deviceMgr *DeviceManager, lDeviceMgr *LogicalDeviceManager) *APIHandler
	if reflect.TypeOf(fSign) != reflect.TypeOf(NewAPIHandler) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(NewAPIHandler), reflect.TypeOf(fSign))
	}
	// NewAPIHandler declares, initializes and returns an handler struct
	apiHdl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)
	assert.NotNil(t, apiHdl)
}

// UpdateLogLevel test
func TestUpdateLogLevel(t *testing.T) {
	core, _ := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	var testCtx = context.Background()
	testLogDef := &voltha.Logging{
		ComponentName: "testing",
		PackageName:   "default",
		Level:         voltha.LogLevel_LogLevel(log.GetDefaultLogLevel())}
	testLogEmpty := &voltha.Logging{
		ComponentName: "testing",
		PackageName:   "",
		Level:         voltha.LogLevel_LogLevel(log.GetDefaultLogLevel())}
	testLog := &voltha.Logging{
		ComponentName: "testing",
		PackageName:   "testing",
		Level:         voltha.LogLevel_LogLevel(log.GetDefaultLogLevel())}
	testLog_3 := &voltha.Logging{
		ComponentName: "testing",
		PackageName:   "github.com/opencord/voltha-go/ro_core/core",
		Level:         3 /*voltha.LogLevel_LogLevel(log.GetDefaultLogLevel())*/}
	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)

	// Check signature
	var fSign func(ctx context.Context, logging *voltha.Logging) (*empty.Empty, error)
	if reflect.TypeOf(fSign) != reflect.TypeOf(ahndl.UpdateLogLevel) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(ahndl.UpdateLogLevel), reflect.TypeOf(fSign))
	}

	type args struct {
		ctx     context.Context
		logging *voltha.Logging
	}
	tests := []struct {
		name    string
		ah      *APIHandler
		args    args
		want    int
		wantErr error
	}{
		{"TestUpdateLogLevel-1", ahndl, args{testCtx, testLogDef}, 0, nil},
		{"TestUpdateLogLevel-2", ahndl, args{testCtx, testLogEmpty}, 5, nil},
		{"TestUpdateLogLevel-3", ahndl, args{testCtx, testLog}, 5, nil},
		{"TestUpdateLogLevel-4", ahndl, args{testCtx, testLog_3}, 3, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotErr := tt.ah.UpdateLogLevel(tt.args.ctx, tt.args.logging)
			if tt.wantErr != gotErr {
				t.Errorf("Error")
			}
			for _, packageNames := range log.GetPackageNames() {
				logLev, errLogLev := log.GetPackageLogLevel(packageNames)
				if errLogLev == nil {
					if packageNames == "github.com/opencord/voltha-go/ro_core/core" {
						if logLev != tt.want {
							t.Errorf("Error. Want %d, Got %d", tt.want, logLev)
						}
					}
				}
			}
		})
	}
}

// GetLogLevels test
func TestGetLogLevels(t *testing.T) {
	core, _ := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	var testCtx = context.Background()
	testLc := new(common.LoggingComponent)
	testLc.ComponentName = "testing"

	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)

	// Check signature
	var fSign func(ctx context.Context, in *voltha.LoggingComponent) (*voltha.Loggings, error)
	if reflect.TypeOf(fSign) != reflect.TypeOf(ahndl.GetLogLevels) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(ahndl.GetLogLevels), reflect.TypeOf(fSign))
	}

	type args struct {
		ctx    context.Context
		testLc *voltha.LoggingComponent
	}
	tests := []struct {
		name    string
		ah      *APIHandler
		args    args
		want    int
		wantErr error
	}{
		{"TestGetLogLevels-1", ahndl, args{testCtx, testLc}, 0, nil},
		{"TestGetLogLevels-2", ahndl, args{testCtx, testLc}, 1, nil},
		{"TestGetLogLevels-3", ahndl, args{testCtx, testLc}, 2, nil},
		{"TestGetLogLevels-4", ahndl, args{testCtx, testLc}, 3, nil},
		{"TestGetLogLevels-5", ahndl, args{testCtx, testLc}, 4, nil},
		{"TestGetLogLevels-6", ahndl, args{testCtx, testLc}, 5, nil},
		{"TestGetLogLevels-7", ahndl, args{testCtx, testLc}, 3, nil},
	}
	for itt, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// itt index match log levels to test (from DEBUG=0, to FATAL=6)
			log.SetPackageLogLevel("github.com/opencord/voltha-go/ro_core/core", itt)
			logLevs, gotErr := tt.ah.GetLogLevels(tt.args.ctx, tt.args.testLc)
			if (logLevs == nil) || (gotErr != nil) {
				t.Errorf("Error %v\n", gotErr)
			}
			for _, lli := range logLevs.Items {
				if lli.PackageName == "github.com/opencord/voltha-go/ro_core/core" {
					if int(lli.Level) != tt.want {
						t.Errorf("logLev wanted %v logLev %v \n", tt.want, lli.Level)
					}
				}
			}
		})
	}

}

// ReconcileDevices test
func TestReconcileDevices(t *testing.T) {
	core, _ := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	var testCtx = context.Background()

	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)
	testIds := &voltha.IDs{Items: make([]*voltha.ID, 0)}

	// Check signature
	var fSign func(ctx context.Context, ids *voltha.IDs) (*empty.Empty, error)
	if reflect.TypeOf(fSign) != reflect.TypeOf(ahndl.ReconcileDevices) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(ahndl.ReconcileDevices), reflect.TypeOf(fSign))
	}

	testEmp, gotErr := ahndl.ReconcileDevices(testCtx, testIds)
	assert.NotNil(t, testEmp)
	assert.Nil(t, gotErr)
}

// GetVoltha test
func TestGetVoltha(t *testing.T) {
	core, _ := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	var testCtx = context.Background()

	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)
	testIds := &voltha.IDs{Items: make([]*voltha.ID, 0)}

	// Check signature
	var fSign func(ctx context.Context, empty *empty.Empty) (*voltha.Voltha, error)
	if reflect.TypeOf(fSign) != reflect.TypeOf(ahndl.GetVoltha) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(ahndl.GetVoltha), reflect.TypeOf(fSign))
	}

	testEmp, _ := ahndl.ReconcileDevices(testCtx, testIds)
	testVol, gotErr := ahndl.GetVoltha(testCtx, testEmp)
	assert.NotNil(t, testVol)
	assert.Nil(t, gotErr)
}

// ListCoreInstances test
func TestListCoreInstances(t *testing.T) {
	core, _ := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	var testCtx = context.Background()

	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)
	testIds := &voltha.IDs{Items: make([]*voltha.ID, 0)}

	// Check signature
	var fSign func(ctx context.Context, empty *empty.Empty) (*voltha.CoreInstances, error)
	if reflect.TypeOf(fSign) != reflect.TypeOf(ahndl.ListCoreInstances) {
		t.Errorf("Function signature mismatch. current %s, was %s", reflect.TypeOf(ahndl.ListCoreInstances), reflect.TypeOf(fSign))
	}

	testEmp, _ := ahndl.ReconcileDevices(testCtx, testIds)
	testCoreIns, gotErr := ahndl.ListCoreInstances(testCtx, testEmp)
	assert.NotNil(t, testCoreIns)
	assert.NotNil(t, gotErr)
}
