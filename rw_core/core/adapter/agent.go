/*
 * Copyright 2020-present Open Networking Foundation

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

package adapter

import (
	"context"
	"github.com/golang/protobuf/ptypes"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"sync"
	"time"
)

// agent represents adapter agent
type agent struct {
	adapter *voltha.Adapter
	lock    sync.RWMutex
}

func newAdapterAgent(adapter *voltha.Adapter) *agent {
	return &agent{
		adapter: adapter,
	}
}

func (aa *agent) getAdapter(ctx context.Context) *voltha.Adapter {
	aa.lock.RLock()
	defer aa.lock.RUnlock()
	logger.Debugw(ctx, "getAdapter", log.Fields{"adapter": aa.adapter})
	return aa.adapter
}

// updateCommunicationTime updates the message to the specified time.
// No attempt is made to save the time to the db, so only recent times are guaranteed to be accurate.
func (aa *agent) updateCommunicationTime(new time.Time) {
	// only update if new time is not in the future, and either the old time is invalid or new time > old time
	aa.lock.Lock()
	defer aa.lock.Unlock()
	if last, err := ptypes.Timestamp(aa.adapter.LastCommunication); !new.After(time.Now()) && (err != nil || new.After(last)) {
		timestamp, err := ptypes.TimestampProto(new)
		if err != nil {
			return // if the new time cannot be encoded, just ignore it
		}

		aa.adapter.LastCommunication = timestamp
	}
}
