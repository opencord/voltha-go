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
	"strconv"
	"testing"

	"github.com/opencord/voltha-go/ro_core/config"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/common"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
)

func MakeTestGrpcNbiConfig() *Core {
	var ctx context.Context
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
				core = NewCore(ctx, "ro_core", roCoreFlgs, roC.kvClient)
			}
		}
	}

	return core
}

func TestNewAPIHandler_grpc(t *testing.T) {
	core := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	// NewAPIHandler declares, initializes and returns an handler struct
	apiHdl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)
	assert.NotNil(t, apiHdl)
}

func TestUpdateLogLevel_grpc(t *testing.T) {
	core := MakeTestGrpcNbiConfig()
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
	testLog3 := &voltha.Logging{
		ComponentName: "testing",
		PackageName:   "github.com/opencord/voltha-go/ro_core/core",
		Level:         3 /*voltha.LogLevel_LogLevel(log.GetDefaultLogLevel())*/}
	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)

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
		{"TestUpdateLogLevel-4", ahndl, args{testCtx, testLog3}, 3, nil},
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

func TestGetLogLevels_grpc(t *testing.T) {
	core := MakeTestGrpcNbiConfig()
	assert.NotNil(t, core)

	testCtx := context.Background()
	testLc := new(common.LoggingComponent)
	testLc.ComponentName = "testing"

	ahndl := NewAPIHandler(core.genericMgr, core.deviceMgr, core.logicalDeviceMgr)

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
