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

	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	tp_pb "github.com/opencord/voltha-protos/v3/go/tech_profile"
)

type TechProfileIf interface {
	SetKVClient() *db.Backend
	GetTechProfileInstanceKVPath(techProfiletblID uint32, uniPortName string) string
	GetTPInstanceFromKVStore(ctx context.Context, techProfiletblID uint32, path string) (*TechProfile, error)
	CreateTechProfInstance(ctx context.Context, techProfiletblID uint32, uniPortName string, intfId uint32) (*TechProfile, error)
	DeleteTechProfileInstance(ctx context.Context, techProfiletblID uint32, uniPortName string) error
	GetprotoBufParamValue(paramType string, paramKey string) int32
	GetUsScheduler(tpInstance *TechProfile) (*tp_pb.SchedulerConfig, error)
	GetDsScheduler(tpInstance *TechProfile) (*tp_pb.SchedulerConfig, error)
	GetTrafficScheduler(tpInstance *TechProfile, SchedCfg *tp_pb.SchedulerConfig,
		ShapingCfg *tp_pb.TrafficShapingInfo) *tp_pb.TrafficScheduler
	GetTrafficQueues(tp *TechProfile, Dir tp_pb.Direction) ([]*tp_pb.TrafficQueue, error)
	GetMulticastTrafficQueues(tp *TechProfile) []*tp_pb.TrafficQueue
	GetGemportIDForPbit(tp *TechProfile, Dir tp_pb.Direction, pbit uint32) uint32
	FindAllTpInstances(ctx context.Context, techProfiletblID uint32, ponIntf uint32, onuID uint32) []TechProfile
}
