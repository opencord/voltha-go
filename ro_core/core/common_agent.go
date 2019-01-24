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
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"sync"
)

// CommonAgent controls requests made to data model proxy
type CommonAgent struct {
	rootProxy *model.Proxy
	basePath  string
	mutex     sync.RWMutex
}

func newCommonAgent(basePath string, rootProxy *model.Proxy) *CommonAgent {
	ga := &CommonAgent{}
	ga.rootProxy = rootProxy

	if strings.HasPrefix(basePath, "/") {
		ga.basePath = basePath
	} else {
		ga.basePath = "/" + basePath
	}

	return ga
}

// Get retrieves information at the provided path
func (agent *CommonAgent) Get(parts ...string) (interface{}, error) {
	agent.mutex.Lock()
	defer agent.mutex.Unlock()

	rawPath := []string{agent.basePath}
	rawPath = append(rawPath, parts...)
	path := strings.Join(rawPath, "/")

	log.Debugw("get-data", log.Fields{"path": path})

	if data := agent.rootProxy.Get(path, 1, false, ""); data != nil {
		return data, nil
	}
	return nil, status.Errorf(codes.NotFound, "data-path: %s", path)
}
