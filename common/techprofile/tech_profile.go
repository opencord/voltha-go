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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-go/db/model"
	openolt_pb "github.com/opencord/voltha-protos/go/openolt"
)

// Interface to pon resource manager APIs
type iPonResourceMgr interface {
	GetResourceID(IntfID uint32, ResourceType string, NumIDs uint32) ([]uint32, error)
	GetResourceTypeAllocID() string
	GetResourceTypeGemPortID() string
	GetTechnology() string
}

type Direction int32

const (
	Direction_UPSTREAM      Direction = 0
	Direction_DOWNSTREAM    Direction = 1
	Direction_BIDIRECTIONAL Direction = 2
)

var Direction_name = map[Direction]string{
	0: "UPSTREAM",
	1: "DOWNSTREAM",
	2: "BIDIRECTIONAL",
}

type SchedulingPolicy int32

const (
	SchedulingPolicy_WRR            SchedulingPolicy = 0
	SchedulingPolicy_StrictPriority SchedulingPolicy = 1
	SchedulingPolicy_Hybrid         SchedulingPolicy = 2
)

var SchedulingPolicy_name = map[SchedulingPolicy]string{
	0: "WRR",
	1: "StrictPriority",
	2: "Hybrid",
}

type AdditionalBW int32

const (
	AdditionalBW_AdditionalBW_None       AdditionalBW = 0
	AdditionalBW_AdditionalBW_NA         AdditionalBW = 1
	AdditionalBW_AdditionalBW_BestEffort AdditionalBW = 2
	AdditionalBW_AdditionalBW_Auto       AdditionalBW = 3
)

var AdditionalBW_name = map[AdditionalBW]string{
	0: "AdditionalBW_None",
	1: "AdditionalBW_NA",
	2: "AdditionalBW_BestEffort",
	3: "AdditionalBW_Auto",
}

type DiscardPolicy int32

const (
	DiscardPolicy_TailDrop  DiscardPolicy = 0
	DiscardPolicy_WTailDrop DiscardPolicy = 1
	DiscardPolicy_Red       DiscardPolicy = 2
	DiscardPolicy_WRed      DiscardPolicy = 3
)

var DiscardPolicy_name = map[DiscardPolicy]string{
	0: "TailDrop",
	1: "WTailDrop",
	2: "Red",
	3: "WRed",
}

/*
type InferredAdditionBWIndication int32

const (
	InferredAdditionBWIndication_InferredAdditionBWIndication_None       InferredAdditionBWIndication = 0
	InferredAdditionBWIndication_InferredAdditionBWIndication_Assured    InferredAdditionBWIndication = 1
	InferredAdditionBWIndication_InferredAdditionBWIndication_BestEffort InferredAdditionBWIndication = 2
)

var InferredAdditionBWIndication_name = map[int32]string{
	0: "InferredAdditionBWIndication_None",
	1: "InferredAdditionBWIndication_Assured",
	2: "InferredAdditionBWIndication_BestEffort",
}
*/
// instance control defaults
const (
	defaultOnuInstance    = "multi-instance"
	defaultUniInstance    = "single-instance"
	defaultNumGemPorts    = 1
	defaultGemPayloadSize = "auto"
)

const MAX_GEM_PAYLOAD = "max_gem_payload_size"

type InstanceControl struct {
	Onu               string `json:"ONU"`
	Uni               string `json:"uni"`
	MaxGemPayloadSize string `json:"max_gem_payload_size"`
}

// default discard config constants
const (
	defaultMinThreshold   = 0
	defaultMaxThreshold   = 0
	defaultMaxProbability = 0
)

type DiscardConfig struct {
	MinThreshold   int `json:"min_threshold"`
	MaxThreshold   int `json:"max_threshold"`
	MaxProbability int `json:"max_probability"`
}

// default scheduler contants
const (
	defaultAddtionalBw      = AdditionalBW_AdditionalBW_Auto
	defaultPriority         = 0
	defaultWeight           = 0
	defaultQueueSchedPolicy = SchedulingPolicy_Hybrid
)

type Scheduler struct {
	Direction    string `json:"direction"`
	AdditionalBw string `json:"additional_bw"`
	Priority     uint32 `json:"priority"`
	Weight       uint32 `json:"weight"`
	QSchedPolicy string `json:"q_sched_policy"`
}

// default GEM attribute constants
const (
	defaultAESEncryption  = "True"
	defaultPriorityQueue  = 0
	defaultQueueWeight    = 0
	defaultMaxQueueSize   = "auto"
	defaultdropPolicy     = DiscardPolicy_TailDrop
	defaultSchedulePolicy = SchedulingPolicy_WRR
)

type GemPortAttribute struct {
	MaxQueueSize     string        `json:"max_q_size"`
	PbitMap          string        `json:"pbit_map"`
	AesEncryption    string        `json:"aes_encryption"`
	SchedulingPolicy string        `json:"scheduling_policy"`
	PriorityQueue    int           `json:"priority_q"`
	Weight           int           `json:"weight"`
	DiscardPolicy    string        `json:"discard_policy"`
	DiscardConfig    DiscardConfig `json:"discard_config"`
}

type iScheduler struct {
	AllocID      uint32 `json:"alloc_id"`
	Direction    string `json:"direction"`
	AdditionalBw string `json:"additional_bw"`
	Priority     uint32 `json:"priority"`
	Weight       uint32 `json:"weight"`
	QSchedPolicy string `json:"q_sched_policy"`
}
type iGemPortAttribute struct {
	GemportID        uint32        `json:"gemport_id"`
	MaxQueueSize     string        `json:"max_q_size"`
	PbitMap          string        `json:"pbit_map"`
	AesEncryption    string        `json:"aes_encryption"`
	SchedulingPolicy string        `json:"scheduling_policy"`
	PriorityQueue    int           `json:"priority_q"`
	Weight           int           `json:"weight"`
	DiscardPolicy    string        `json:"discard_policy"`
	DiscardConfig    DiscardConfig `json:"discard_config"`
}

type TechProfileMgr struct {
	config      *TechProfileFlags
	resourceMgr iPonResourceMgr
}
type DefaultTechProfile struct {
	Name                           string             `json:"name"`
	ProfileType                    string             `json:"profile_type"`
	Version                        int                `json:"version"`
	NumGemPorts                    uint32             `json:"num_gem_ports"`
	InstanceCtrl                   InstanceControl    `json:"instance_control"`
	UsScheduler                    Scheduler          `json:"us_scheduler"`
	DsScheduler                    Scheduler          `json:"ds_scheduler"`
	UpstreamGemPortAttributeList   []GemPortAttribute `json:"upstream_gem_port_attribute_list"`
	DownstreamGemPortAttributeList []GemPortAttribute `json:"downstream_gem_port_attribute_list"`
}
type TechProfile struct {
	Name                           string              `json:"name"`
	SubscriberIdentifier           string              `json:"subscriber_identifier"`
	ProfileType                    string              `json:"profile_type"`
	Version                        int                 `json:"version"`
	NumGemPorts                    uint32              `json:"num_gem_ports"`
	NumTconts                      uint32              `json:"num_of_tconts"`
	InstanceCtrl                   InstanceControl     `json:"instance_control"`
	UsScheduler                    iScheduler          `json:"us_scheduler"`
	DsScheduler                    iScheduler          `json:"ds_scheduler"`
	UpstreamGemPortAttributeList   []iGemPortAttribute `json:"upstream_gem_port_attribute_list"`
	DownstreamGemPortAttributeList []iGemPortAttribute `json:"downstream_gem_port_attribute_list"`
}

func (t *TechProfileMgr) SetKVClient() *model.Backend {
	addr := t.config.KVStoreHost + ":" + strconv.Itoa(t.config.KVStorePort)
	kvClient, err := newKVClient(t.config.KVStoreType, addr, t.config.KVStoreTimeout)
	if err != nil {
		log.Errorw("failed-to-create-kv-client",
			log.Fields{
				"type": t.config.KVStoreType, "host": t.config.KVStoreHost, "port": t.config.KVStorePort,
				"timeout": t.config.KVStoreTimeout, "prefix": t.config.TPKVPathPrefix,
				"error": err.Error(),
			})
		return nil
	}
	return &model.Backend{
		Client:     kvClient,
		StoreType:  t.config.KVStoreType,
		Host:       t.config.KVStoreHost,
		Port:       t.config.KVStorePort,
		Timeout:    t.config.KVStoreTimeout,
		PathPrefix: t.config.TPKVPathPrefix}

	/* TODO : Make sure direct call to NewBackend is working fine with backend , currently there is some
	            issue between kv store and backend , core is not calling NewBackend directly
		   kv := model.NewBackend(t.config.KVStoreType, t.config.KVStoreHost, t.config.KVStorePort,
										t.config.KVStoreTimeout,  kvStoreTechProfilePathPrefix)
	*/
}

func newKVClient(storeType string, address string, timeout int) (kvstore.Client, error) {

	log.Infow("kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("unsupported-kv-store")
}

func NewTechProfile(resourceMgr iPonResourceMgr) (*TechProfileMgr, error) {
	var techprofileObj TechProfileMgr
	log.Debug("Initializing techprofile Manager")
	techprofileObj.config = NewTechProfileFlags()
	techprofileObj.config.KVBackend = techprofileObj.SetKVClient()
	if techprofileObj.config.KVBackend == nil {
		log.Error("Failed to initialize KV backend\n")
		return nil, errors.New("KV backend init failed")
	}
	techprofileObj.resourceMgr = resourceMgr
	log.Debug("Initializing techprofile object instance success")
	return &techprofileObj, nil
}

func (t *TechProfileMgr) GetTechProfileInstanceKVPath(techProfiletblID uint32, uniPortName string) string {
	return fmt.Sprintf(t.config.TPInstanceKVPath, t.resourceMgr.GetTechnology(), techProfiletblID, uniPortName)
}

func (t *TechProfileMgr) GetTPInstanceFromKVStore(techProfiletblID uint32, path string) (*TechProfile, error) {
	var KvTpIns TechProfile
	var resPtr *TechProfile = &KvTpIns
	var err error
	/*path := t.GetTechProfileInstanceKVPath(techProfiletblID, uniPortName)*/
	log.Infow("Getting tech profile instance from KV store", log.Fields{"path": path})
	kvresult, err := t.config.KVBackend.Get(path)
	if err != nil {
		log.Errorw("Error while fetching tech-profile instance  from KV backend", log.Fields{"key": path})
		return nil, err
	}
	if kvresult == nil {
		log.Infow("Tech profile does not exist in KV store", log.Fields{"key": path})
		resPtr = nil
	} else {
		if value, err := kvstore.ToByte(kvresult.Value); err == nil {
			if err = json.Unmarshal(value, resPtr); err != nil {
				log.Errorw("Error while unmarshal KV result", log.Fields{"key": path, "value": value})
			}
		}
	}
	return resPtr, err
}

func (t *TechProfileMgr) addTechProfInstanceToKVStore(techProfiletblID uint32, uniPortName string, tpInstance *TechProfile) error {
	path := t.GetTechProfileInstanceKVPath(techProfiletblID, uniPortName)
	log.Debugw("Adding techprof instance to kvstore", log.Fields{"key": path, "tpinstance": tpInstance})
	tpInstanceJson, err := json.Marshal(*tpInstance)
	if err == nil {
		// Backend will convert JSON byte array into string format
		log.Debugw("Storing tech profile instance to KV Store", log.Fields{"key": path, "val": tpInstanceJson})
		err = t.config.KVBackend.Put(path, tpInstanceJson)
	} else {
		log.Errorw("Error in marshaling into Json format", log.Fields{"key": path, "tpinstance": tpInstance})
	}
	return err
}
func (t *TechProfileMgr) getTPFromKVStore(techProfiletblID uint32) *DefaultTechProfile {
	var kvtechprofile DefaultTechProfile
	key := fmt.Sprintf(t.config.TPFileKVPath, t.resourceMgr.GetTechnology(), techProfiletblID)
	log.Debugw("Getting techprofile from KV store", log.Fields{"techProfiletblID": techProfiletblID, "Key": key})
	kvresult, err := t.config.KVBackend.Get(key)
	if err != nil {
		log.Errorw("Error while fetching value from KV store", log.Fields{"key": key})
		return nil
	}
	if kvresult != nil {
		/* Backend will return Value in string format,needs to be converted to []byte before unmarshal*/
		if value, err := kvstore.ToByte(kvresult.Value); err == nil {
			if err = json.Unmarshal(value, &kvtechprofile); err == nil {
				log.Debugw("Success fetched techprofile from KV store", log.Fields{"techProfiletblID": techProfiletblID, "value": kvtechprofile})
				return &kvtechprofile
			}
		}
	}
	return nil
}
func (t *TechProfileMgr) CreateTechProfInstance(techProfiletblID uint32, uniPortName string, intfId uint32) *TechProfile {
	var tpInstance *TechProfile
	log.Infow("Creating tech profile instance ", log.Fields{"tableid": techProfiletblID, "uni": uniPortName, "intId": intfId})
	tp := t.getTPFromKVStore(techProfiletblID)
	if tp != nil {
		log.Infow("Creating tech profile instance with profile from KV store", log.Fields{"tpid": techProfiletblID})
	} else {
		tp = t.getDefaultTechProfile()
		log.Infow("Creating tech profile instance with default values", log.Fields{"tpid": techProfiletblID})
	}
	tpInstance = t.allocateTPInstance(uniPortName, tp, intfId, t.config.DefaultNumTconts)
	if err := t.addTechProfInstanceToKVStore(techProfiletblID, uniPortName, tpInstance); err != nil {
		log.Errorw("Error in adding tech profile instance to KV ", log.Fields{"tableid": techProfiletblID, "uni": uniPortName})
		return nil
	}
	log.Infow("Added tech profile instance to KV store successfully ",
		log.Fields{"tpid": techProfiletblID, "uni": uniPortName, "intfId": intfId})
	return tpInstance
}

func (t *TechProfileMgr) DeleteTechProfileInstance(techProfiletblID uint32, uniPortName string) error {
	path := t.GetTechProfileInstanceKVPath(techProfiletblID, uniPortName)
	return t.config.KVBackend.Delete(path)
}

func (t *TechProfileMgr) allocateTPInstance(uniPortName string, tp *DefaultTechProfile, intfId uint32, numOfTconts uint32) *TechProfile {

	var usGemPortAttributeList []iGemPortAttribute
	var dsGemPortAttributeList []iGemPortAttribute
	var tcontIDs []uint32
	var gemPorts []uint32
	var err error

	log.Infow("Allocating TechProfileMgr instance from techprofile template", log.Fields{"uniPortName": uniPortName, "intfId": intfId, "numOfTconts": numOfTconts, "numGem": tp.NumGemPorts})
	if numOfTconts > 1 {
		log.Errorw("Multiple Tconts not supported currently", log.Fields{"uniPortName": uniPortName, "intfId": intfId})
		return nil
	}
	if tcontIDs, err = t.resourceMgr.GetResourceID(intfId, t.resourceMgr.GetResourceTypeAllocID(), numOfTconts); err != nil {
		log.Errorw("Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId, "numTconts": numOfTconts})
		return nil
	}
	log.Debugw("Num GEM ports in TP:", log.Fields{"NumGemPorts": tp.NumGemPorts})
	if gemPorts, err = t.resourceMgr.GetResourceID(intfId, t.resourceMgr.GetResourceTypeGemPortID(), tp.NumGemPorts); err != nil {
		log.Errorw("Error getting gemport ids from rsrcrMgr", log.Fields{"intfId": intfId, "numGemports": tp.NumGemPorts})
		return nil
	}
	log.Infow("Allocated tconts and GEM ports successfully", log.Fields{"tconts": tcontIDs, "gemports": gemPorts})
	for index := 0; index < int(tp.NumGemPorts); index++ {
		usGemPortAttributeList = append(usGemPortAttributeList,
			iGemPortAttribute{GemportID: gemPorts[index],
				MaxQueueSize:     tp.UpstreamGemPortAttributeList[index].MaxQueueSize,
				PbitMap:          tp.UpstreamGemPortAttributeList[index].PbitMap,
				AesEncryption:    tp.UpstreamGemPortAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.UpstreamGemPortAttributeList[index].SchedulingPolicy,
				PriorityQueue:    tp.UpstreamGemPortAttributeList[index].PriorityQueue,
				Weight:           tp.UpstreamGemPortAttributeList[index].Weight,
				DiscardPolicy:    tp.UpstreamGemPortAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.UpstreamGemPortAttributeList[index].DiscardConfig})
		dsGemPortAttributeList = append(dsGemPortAttributeList,
			iGemPortAttribute{GemportID: gemPorts[index],
				MaxQueueSize:     tp.DownstreamGemPortAttributeList[index].MaxQueueSize,
				PbitMap:          tp.DownstreamGemPortAttributeList[index].PbitMap,
				AesEncryption:    tp.DownstreamGemPortAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
				PriorityQueue:    tp.DownstreamGemPortAttributeList[index].PriorityQueue,
				Weight:           tp.DownstreamGemPortAttributeList[index].Weight,
				DiscardPolicy:    tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.DownstreamGemPortAttributeList[index].DiscardConfig})
	}
	return &TechProfile{
		SubscriberIdentifier: uniPortName,
		Name:                 tp.Name,
		ProfileType:          tp.ProfileType,
		Version:              tp.Version,
		NumGemPorts:          tp.NumGemPorts,
		NumTconts:            numOfTconts,
		InstanceCtrl:         tp.InstanceCtrl,
		UsScheduler: iScheduler{
			AllocID:      tcontIDs[0],
			Direction:    tp.UsScheduler.Direction,
			AdditionalBw: tp.UsScheduler.AdditionalBw,
			Priority:     tp.UsScheduler.Priority,
			Weight:       tp.UsScheduler.Weight,
			QSchedPolicy: tp.UsScheduler.QSchedPolicy},
		DsScheduler: iScheduler{
			AllocID:      tcontIDs[0],
			Direction:    tp.DsScheduler.Direction,
			AdditionalBw: tp.DsScheduler.AdditionalBw,
			Priority:     tp.DsScheduler.Priority,
			Weight:       tp.DsScheduler.Weight,
			QSchedPolicy: tp.DsScheduler.QSchedPolicy},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

func (t *TechProfileMgr) getDefaultTechProfile() *DefaultTechProfile {

	var usGemPortAttributeList []GemPortAttribute
	var dsGemPortAttributeList []GemPortAttribute

	for _, pbit := range t.config.DefaultPbits {
		usGemPortAttributeList = append(usGemPortAttributeList,
			GemPortAttribute{
				MaxQueueSize:     defaultMaxQueueSize,
				PbitMap:          pbit,
				AesEncryption:    defaultAESEncryption,
				SchedulingPolicy: SchedulingPolicy_name[defaultSchedulePolicy],
				PriorityQueue:    defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    DiscardPolicy_name[defaultdropPolicy],
				DiscardConfig: DiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability}})
		dsGemPortAttributeList = append(dsGemPortAttributeList,
			GemPortAttribute{
				MaxQueueSize:     defaultMaxQueueSize,
				PbitMap:          pbit,
				AesEncryption:    defaultAESEncryption,
				SchedulingPolicy: SchedulingPolicy_name[defaultSchedulePolicy],
				PriorityQueue:    defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    DiscardPolicy_name[defaultdropPolicy],
				DiscardConfig: DiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability}})
	}
	return &DefaultTechProfile{
		Name:        t.config.DefaultTPName,
		ProfileType: t.resourceMgr.GetTechnology(),
		Version:     t.config.TPVersion,
		NumGemPorts: uint32(len(usGemPortAttributeList)),
		InstanceCtrl: InstanceControl{
			Onu:               defaultOnuInstance,
			Uni:               defaultUniInstance,
			MaxGemPayloadSize: defaultGemPayloadSize},
		UsScheduler: Scheduler{
			Direction:    Direction_name[Direction_UPSTREAM],
			AdditionalBw: AdditionalBW_name[defaultAddtionalBw],
			Priority:     defaultPriority,
			Weight:       defaultWeight,
			QSchedPolicy: SchedulingPolicy_name[defaultQueueSchedPolicy]},
		DsScheduler: Scheduler{
			Direction:    Direction_name[Direction_DOWNSTREAM],
			AdditionalBw: AdditionalBW_name[defaultAddtionalBw],
			Priority:     defaultPriority,
			Weight:       defaultWeight,
			QSchedPolicy: SchedulingPolicy_name[defaultQueueSchedPolicy]},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

func (t *TechProfileMgr) GetprotoBufParamValue(paramType string, paramKey string) int32 {
	var result int32 = -1

	if paramType == "direction" {
		for key, val := range openolt_pb.Direction_value {
			if key == paramKey {
				result = val
			}
		}
	} else if paramType == "discard_policy" {
		for key, val := range openolt_pb.DiscardPolicy_value {
			if key == paramKey {
				result = val
			}
		}
	} else if paramType == "sched_policy" {
		for key, val := range openolt_pb.SchedulingPolicy_value {
			if key == paramKey {
				log.Debugw("Got value in proto", log.Fields{"key": key, "value": val})
				result = val
			}
		}
	} else if paramType == "additional_bw" {
		for key, val := range openolt_pb.AdditionalBW_value {
			if key == paramKey {
				result = val
			}
		}
	} else {
		log.Error("Could not find proto parameter", log.Fields{"paramType": paramType, "key": paramKey})
		return -1
	}
	log.Debugw("Got value in proto", log.Fields{"key": paramKey, "value": result})
	return result
}

func (t *TechProfileMgr) GetUsScheduler(tpInstance *TechProfile) *openolt_pb.Scheduler {
	dir := openolt_pb.Direction(t.GetprotoBufParamValue("direction", tpInstance.UsScheduler.Direction))
	if dir == -1 {
		log.Fatal("Error in getting Proto for direction for upstream scheduler")
		return nil
	}
	bw := openolt_pb.AdditionalBW(t.GetprotoBufParamValue("additional_bw", tpInstance.UsScheduler.AdditionalBw))
	if bw == -1 {
		log.Fatal("Error in getting Proto for bandwidth for upstream scheduler")
		return nil
	}
	policy := openolt_pb.SchedulingPolicy(t.GetprotoBufParamValue("sched_policy", tpInstance.UsScheduler.QSchedPolicy))
	if policy == -1 {
		log.Fatal("Error in getting Proto for scheduling policy for upstream scheduler")
		return nil
	}
	return &openolt_pb.Scheduler{
		Direction:    dir,
		AdditionalBw: bw,
		Priority:     tpInstance.UsScheduler.Priority,
		Weight:       tpInstance.UsScheduler.Weight,
		SchedPolicy:  policy}
}

func (t *TechProfileMgr) GetDsScheduler(tpInstance *TechProfile) *openolt_pb.Scheduler {

	dir := openolt_pb.Direction(t.GetprotoBufParamValue("direction", tpInstance.DsScheduler.Direction))
	if dir == -1 {
		log.Fatal("Error in getting Proto for direction for downstream scheduler")
		return nil
	}
	bw := openolt_pb.AdditionalBW(t.GetprotoBufParamValue("additional_bw", tpInstance.DsScheduler.AdditionalBw))
	if bw == -1 {
		log.Fatal("Error in getting Proto for bandwidth for downstream scheduler")
		return nil
	}
	policy := openolt_pb.SchedulingPolicy(t.GetprotoBufParamValue("sched_policy", tpInstance.DsScheduler.QSchedPolicy))
	if policy == -1 {
		log.Fatal("Error in getting Proto for scheduling policy for downstream scheduler")
		return nil
	}

	return &openolt_pb.Scheduler{
		Direction:    dir,
		AdditionalBw: bw,
		Priority:     tpInstance.DsScheduler.Priority,
		Weight:       tpInstance.DsScheduler.Weight,
		SchedPolicy:  policy}
}

func (t *TechProfileMgr) GetTconts(tpInstance *TechProfile, usSched *openolt_pb.Scheduler, dsSched *openolt_pb.Scheduler) []*openolt_pb.Tcont {
	if usSched == nil {
		if usSched = t.GetUsScheduler(tpInstance); usSched == nil {
			log.Fatal("Error in getting upstream scheduler from techprofile")
			return nil
		}
	}
	if dsSched == nil {
		if dsSched = t.GetDsScheduler(tpInstance); dsSched == nil {
			log.Fatal("Error in getting downstream scheduler from techprofile")
			return nil
		}
	}
	tconts := []*openolt_pb.Tcont{}
	// upstream scheduler
	tcont_us := &openolt_pb.Tcont{
		Direction: usSched.Direction,
		AllocId:   tpInstance.UsScheduler.AllocID,
		Scheduler: usSched} /*TrafficShapingInfo: ? */
	tconts = append(tconts, tcont_us)

	// downstream scheduler
	tcont_ds := &openolt_pb.Tcont{
		Direction: dsSched.Direction,
		AllocId:   tpInstance.DsScheduler.AllocID,
		Scheduler: dsSched}

	tconts = append(tconts, tcont_ds)
	return tconts
}
