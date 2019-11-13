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
	"reflect"
	"testing"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-protos/v2/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fields struct {
	rootProxy *model.Proxy
	basePath  string
}

func getModelProxyPathNotFound() *fields {
	var modelProxy fields

	TestProxyRoot := model.NewRoot(&voltha.Voltha{}, nil)
	TestProxyRootProxy := TestProxyRoot.CreateProxy(context.Background(), "/", false)
	modelProxy.rootProxy = TestProxyRootProxy
	modelProxy.basePath = "base_path"

	return &modelProxy
}

func getModelProxyPathFound() *fields {
	var modelProxy fields

	TestProxyRoot := model.NewRoot(&voltha.Voltha{}, nil)
	TestProxyRootProxy := TestProxyRoot.CreateProxy(context.Background(), "/", false)
	modelProxy.rootProxy = TestProxyRootProxy
	modelProxy.basePath = "devices"

	return &modelProxy
}

func testModelProxyObject(testModelProxy *fields) *ModelProxy {
	return &ModelProxy{
		rootProxy: testModelProxy.rootProxy,
		basePath:  testModelProxy.basePath,
	}
}

func TestNewModelProxy(t *testing.T) {
	type args struct {
		basePath  string
		rootProxy *model.Proxy
	}
	tests := []struct {
		name string
		args args
		want *ModelProxy
	}{
		{"NewModelProxy-1", args{"base_path", &model.Proxy{}}, &ModelProxy{}},
		{"NewModelProxy-2", args{"/base_path", &model.Proxy{}}, &ModelProxy{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newModelProxy(tt.args.basePath, tt.args.rootProxy); reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
				t.Errorf("newModelProxy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelProxy_Get(t *testing.T) {
	tests := []struct {
		name    string
		fields  *fields
		wantErr error
	}{
		{"Get-PathNotFound", getModelProxyPathNotFound(), status.Errorf(codes.NotFound, "data-path: base_path")},
		{"Get-PathFound", getModelProxyPathFound(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ModelProxyObj := testModelProxyObject(tt.fields)
			_, err := ModelProxyObj.Get()
			if err != nil && reflect.TypeOf(err) != reflect.TypeOf(tt.wantErr) {
				t.Errorf("Get() error = %t, wantErr %t", err, tt.wantErr)
			}
		})
	}
}
