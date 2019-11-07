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
	"strings"
	"sync"

	"github.com/opencord/voltha-go/db/model"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ModelProxy controls requests made to the data model
type ModelProxy struct {
	rootProxy *model.Proxy
	basePath  string
	mutex     sync.RWMutex
}

func newModelProxy(basePath string, rootProxy *model.Proxy) *ModelProxy {
	ga := &ModelProxy{}
	ga.rootProxy = rootProxy

	if strings.HasPrefix(basePath, "/") {
		ga.basePath = basePath
	} else {
		ga.basePath = "/" + basePath
	}

	return ga
}

// Get retrieves information at the provided path
func (mp *ModelProxy) Get(parts ...string) (interface{}, error) {
	mp.mutex.Lock()
	defer mp.mutex.Unlock()

	rawPath := []string{mp.basePath}
	rawPath = append(rawPath, parts...)
	path := strings.Join(rawPath, "/")

	log.Debugw("get-data", log.Fields{"path": path})

	if data, err := mp.rootProxy.Get(context.Background(), path, 1, false, ""); err != nil {
		log.Errorw("failed-to-retrieve-data-from-model-proxy", log.Fields{"error": err})
		return nil, err
	} else if data != nil {
		return data, nil
	}
	return nil, status.Errorf(codes.NotFound, "data-path: %s", path)
}
