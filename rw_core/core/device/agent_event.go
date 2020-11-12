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

package device

import (
	"context"
	"fmt"

	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-protos/v4/go/common"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/opentracing/opentracing-go"
	jtracing "github.com/uber/jaeger-client-go"
)

func (agent *Agent) newRPCEvent(ctx context.Context, rpc, coreInstanceID, resourceID string) *voltha.RPCEvent {
	logger.Debugw(ctx, "newRPCEvent", log.Fields{"device-id": agent.deviceID})
	var opID string
	if span := opentracing.SpanFromContext(ctx); span != nil {
		if jSpan, ok := span.(*jtracing.Span); ok {
			opID = fmt.Sprintf("%016x", jSpan.SpanContext().TraceID().Low) // Using Sprintf to avoid removal of leading 0s
		}
	}
	rpcev := &voltha.RPCEvent{
		Rpc:         rpc,
		OperationId: opID,
		ResourceId:  agent.deviceID,
		Service:     coreInstanceID,
		Status: &common.OperationResp{
			Code: common.OperationResp_OPERATION_FAILURE,
		},
	}
	return rpcev
}
