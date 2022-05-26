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

package techprofile

import (
	"context"

	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	tp_pb "github.com/opencord/voltha-protos/v5/go/tech_profile"
)

type TechProfileIf interface {
	SetKVClient(ctx context.Context, pathPrefix string) *db.Backend
	GetTechProfileInstanceKey(ctx context.Context, tpID uint32, uniPortName string) string
	GetTPInstance(ctx context.Context, path string) (interface{}, error)
	CreateTechProfileInstance(ctx context.Context, tpID uint32, uniPortName string, intfID uint32) (interface{}, error)
	DeleteTechProfileInstance(ctx context.Context, tpID uint32, uniPortName string) error
	GetUsScheduler(tpInstance *tp_pb.TechProfileInstance) *tp_pb.SchedulerConfig
	GetDsScheduler(tpInstance *tp_pb.TechProfileInstance) *tp_pb.SchedulerConfig
	GetTrafficScheduler(tpInstance *tp_pb.TechProfileInstance, SchedCfg *tp_pb.SchedulerConfig, ShapingCfg *tp_pb.TrafficShapingInfo) *tp_pb.TrafficScheduler
	GetTrafficQueues(ctx context.Context, tp *tp_pb.TechProfileInstance, Dir tp_pb.Direction) ([]*tp_pb.TrafficQueue, error)
	GetMulticastTrafficQueues(ctx context.Context, tp *tp_pb.TechProfileInstance) []*tp_pb.TrafficQueue
	GetGemportForPbit(ctx context.Context, tp interface{}, Dir tp_pb.Direction, pbit uint32) interface{}
	FindAllTpInstances(ctx context.Context, oltDeviceID string, tpID uint32, ponIntf uint32, onuID uint32) interface{}
	GetResourceID(ctx context.Context, IntfID uint32, ResourceType string, NumIDs uint32) ([]uint32, error)
	FreeResourceID(ctx context.Context, IntfID uint32, ResourceType string, ReleaseContent []uint32) error
}
