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

package adapterif

import (
	"context"

	"github.com/golang/protobuf/proto"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
)

// AdapterProxy interface for AdapterProxy implementation.
type AdapterProxy interface {
	SendInterAdapterMessage(ctx context.Context,
		msg proto.Message,
		msgType ic.InterAdapterMessageType_Types,
		fromAdapter string,
		toAdapter string,
		toDeviceID string,
		proxyDeviceID string,
		messageID string) error
}
