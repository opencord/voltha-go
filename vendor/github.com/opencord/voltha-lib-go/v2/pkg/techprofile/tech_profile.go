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
	"regexp"
	"strconv"

	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	tp_pb "github.com/opencord/voltha-protos/v2/go/tech_profile"
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

// Required uniPortName format
var uniPortNameFormat = regexp.MustCompile(`^pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}$`)

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
	defaultAdditionalBw     = AdditionalBW_AdditionalBW_BestEffort
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
	PriorityQueue    uint32        `json:"priority_q"`
	Weight           uint32        `json:"weight"`
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
	PriorityQueue    uint32        `json:"priority_q"`
	Weight           uint32        `json:"weight"`
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
	InstanceCtrl                   InstanceControl     `json:"instance_control"`
	UsScheduler                    iScheduler          `json:"us_scheduler"`
	DsScheduler                    iScheduler          `json:"ds_scheduler"`
	UpstreamGemPortAttributeList   []iGemPortAttribute `json:"upstream_gem_port_attribute_list"`
	DownstreamGemPortAttributeList []iGemPortAttribute `json:"downstream_gem_port_attribute_list"`
}

func (t *TechProfileMgr) SetKVClient() *db.Backend {
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
	return &db.Backend{
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

	log.Infow("kv-store", log.Fields{"storeType": storeType, "address": address})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("unsupported-kv-store")
}

func NewTechProfile(resourceMgr iPonResourceMgr, KVStoreType string, KVStoreHost string, KVStorePort int) (*TechProfileMgr, error) {
	var techprofileObj TechProfileMgr
	log.Debug("Initializing techprofile Manager")
	techprofileObj.config = NewTechProfileFlags(KVStoreType, KVStoreHost, KVStorePort)
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
	var tp *DefaultTechProfile = nil
	var kvResult *kvstore.KVPair
	if tp = t.getTPFromKVStore(techProfiletblID); tp != nil {
		if err := t.validateInstanceControlAttr(tp.InstanceCtrl); err != nil {
			return nil, errors.New("invalid-instance-ctl-attr")
		}
	} else {
		return nil, errors.New("tp-not-found-on-kv-store")
	}
	if tp.InstanceCtrl.Onu == "multi-instance" {
		// When InstanceCtrl.Onu is "multi-instance" there can be multiple instance of the same
		// TP across different UNIs. We either find a pre-existing TP Instance on that UNI or
		// create a new one.
		log.Infow("Getting tech profile instance from KV store", log.Fields{"path": path})
		kvResult, err = t.config.KVBackend.Get(path)
	} else { // "single-instance"
		// When InstanceCtrl.Onu is "single-instance" there can be only one instance of the
		// TP across all UNIs. The TP instances for the given TP ID will have the same alloc_id,
		// but different gemport-ids (per UNI).
		// We do the following
		// 1. Find a pre-existing TP Instance for the given TP ID and on the given UNI.
		//    If exists, return, else step 2.
		// 2. Find TP instance for the given TP ID and on any other UNI on that ONU.
		//    If exists, make a copy of the TP instance, replace the gem-port IDs, place it
		//    in the the current UNIs TP instance path.
		//    If no other UNI have TP instance too, then return nil (a new TP instance will
		//    get created for the given TP ID).
		kvResult, err = t.config.KVBackend.Get(path)
		if kvResult == nil {
			if resPtr, err = t.findAndAssignTpInstance(path); resPtr != nil {
				log.Infow("successfully-found-and-assigned-tp-instance", log.Fields{"tpPath": path})
				return resPtr, err
			}
		}
	}

	if err != nil {
		log.Errorw("Error while fetching tech-profile instance  from KV backend", log.Fields{"key": path})
		return nil, err
	}
	if kvResult == nil {
		log.Infow("Tech profile does not exist in KV store", log.Fields{"key": path})
		resPtr = nil
	} else {
		if value, err := kvstore.ToByte(kvResult.Value); err == nil {
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

	// Make sure the uniPortName is as per format pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
	if !uniPortNameFormat.Match([]byte(uniPortName)) {
		log.Errorw("uni-port-name-not-confirming-to-format", log.Fields{"uniPortName": uniPortName})
		return nil
	}

	tp := t.getTPFromKVStore(techProfiletblID)
	if tp != nil {
		log.Infow("Creating tech profile instance with profile from KV store", log.Fields{"tpid": techProfiletblID})
	} else {
		tp = t.getDefaultTechProfile()
		log.Infow("Creating tech profile instance with default values", log.Fields{"tpid": techProfiletblID})
	}
	tpInstance = t.allocateTPInstance(uniPortName, tp, intfId)
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

func (t *TechProfileMgr) validateInstanceControlAttr(instCtl InstanceControl) error {
	if instCtl.Onu != "single-instance" && instCtl.Onu != "multi-instance" {
		log.Errorw("invalid-onu-instance-control-attribute", log.Fields{"onu-inst": instCtl.Onu})
		return errors.New("invalid-onu-instance-ctl-attr")
	}

	if instCtl.Uni != "single-instance" && instCtl.Uni != "multi-instance" {
		log.Errorw("invalid-uni-instance-control-attribute", log.Fields{"uni-inst": instCtl.Uni})
		return errors.New("invalid-uni-instance-ctl-attr")
	}

	if instCtl.Uni == "multi-instance" {
		log.Error("uni-multi-instance-tp-not-supported")
		return errors.New("uni-multi-instance-tp-not-supported")
	}

	return nil
}

func (t *TechProfileMgr) allocateTPInstance(uniPortName string, tp *DefaultTechProfile, intfId uint32) *TechProfile {

	var usGemPortAttributeList []iGemPortAttribute
	var dsGemPortAttributeList []iGemPortAttribute
	var tcontIDs []uint32
	var gemPorts []uint32
	var err error

	log.Infow("Allocating TechProfileMgr instance from techprofile template", log.Fields{"uniPortName": uniPortName, "intfId": intfId, "numGem": tp.NumGemPorts})

	err = t.validateInstanceControlAttr(tp.InstanceCtrl)
	if err != nil {
		log.Error("invalid-tp-instance-control-attributes")
		return nil
	}

	if tcontIDs, err = t.resourceMgr.GetResourceID(intfId, t.resourceMgr.GetResourceTypeAllocID(), 1); err != nil {
		log.Errorw("Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
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

// findAndAssignTpInstance finds out if there is another TpInstance for an ONU on a different
// uni port for the same TP ID.
// If it finds one:
// 1. It will make a copy of the TpInstance
// 2. Retain the AllocID
// 3. Replace the GemPort IDs
// 4. Copy the new TpInstance on the given tpPath
// ** NOTE ** : This is to be used only when the instance control attribute is as below
// uni: single-instance, onu: single-instance
func (t *TechProfileMgr) findAndAssignTpInstance(tpPath string) (*TechProfile, error) {
	var tpInst TechProfile
	var foundValidTpInst = false
	var gemPortIDs []uint32
	var intfID uint64
	var err error

	// For example:
	// tpPath like "service/voltha/technology_profiles/xgspon/64/pon-{0}/onu-{1}/uni-{1}"
	// is broken into ["service/voltha/technology_profiles/xgspon/64/pon-{0}/onu-{1}" ""]
	uniPathSlice := regexp.MustCompile(`/uni-{[0-9]+}$`).Split(tpPath, 2)
	kvPairs, _ := t.config.KVBackend.List(uniPathSlice[0])

	// Find the PON interface ID from the TP Path
	var tpPathRgx = regexp.MustCompile(`pon-{([0-9]+)}`)
	intfStrMatch := tpPathRgx.FindStringSubmatch(tpPath)
	if intfStrMatch == nil {
		log.Error("could-not-find-pon-intf-id-in-tp-path")
		return nil, errors.New("could-not-find-pon-intf-id-in-tp-path")
	} else {
		if intfID, err = strconv.ParseUint(intfStrMatch[1], 10, 64); err != nil {
			log.Errorw("error-converting-pon-intfid-str-to-unint", log.Fields{"intfIdStr": intfStrMatch[1]})
			return nil, errors.New("error-converting-pon-intfid-str-to-unint")
		}
	}

	// Find a valid TP Instance among all the UNIs of that ONU for the given TP ID
	for keyPath, kvPair := range kvPairs {
		if value, err := kvstore.ToByte(kvPair.Value); err == nil {
			if err = json.Unmarshal(value, &tpInst); err != nil {
				log.Errorw("error-unmarshal-kv-pair", log.Fields{"keyPath": keyPath, "value": value})
				return nil, errors.New("error-unmarshal-kv-pair")
			} else {
				log.Debugw("found-valid-tp-instance-on-another-uni", log.Fields{"keyPath": keyPath})
				foundValidTpInst = true
				break
			}
		}
	}
	if foundValidTpInst {
		// Get new GemPort IDs
		if gemPortIDs, err = t.resourceMgr.GetResourceID(uint32(intfID), t.resourceMgr.GetResourceTypeGemPortID(), tpInst.NumGemPorts); err != nil {
			log.Errorw("gem-port-assignment-failed", log.Fields{"intfId": intfID, "numGemports": tpInst.NumGemPorts})
			return nil, errors.New("gem-port-assignment-failed")
		}
		// Update the new GemPort IDs to the TpInstance
		for i := 0; i < int(tpInst.NumGemPorts); i++ {
			tpInst.DownstreamGemPortAttributeList[i].GemportID = gemPortIDs[i]
			tpInst.UpstreamGemPortAttributeList[i].GemportID = gemPortIDs[i]
		}

		tpInstanceJson, err := json.Marshal(tpInst)
		if err == nil {
			// Backend will convert JSON byte array into string format
			log.Debugw("store-tp-instance", log.Fields{"tpPath": tpPath, "val": tpInstanceJson})
			if err = t.config.KVBackend.Put(tpPath, tpInstanceJson); err != nil {
				return nil, errors.New("error-store-instance-on-kv")
			}
			// We have succesfully placed the new TP Instance if we land here.
			log.Debugw("successfully-placed-single-instance-tp-for-tp-path", log.Fields{"tpPath": tpPath})
			return &tpInst, nil
		} else {
			log.Errorw("error-marshal-tp-to-json", log.Fields{"tpPath": tpPath, "tpInstance": tpInst})
			return nil, errors.New("error-marshal-tp-to-json")
		}
	}
	log.Debug("no-pre-existing-tp-instance-found-on-another-uni")
	return nil, nil
}

func (t *TechProfileMgr) getDefaultTechProfile() *DefaultTechProfile {

	var usGemPortAttributeList []GemPortAttribute
	var dsGemPortAttributeList []GemPortAttribute

	for _, pbit := range t.config.DefaultPbits {
		log.Debugw("Creating GEM port", log.Fields{"pbit": pbit})
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
			AdditionalBw: AdditionalBW_name[defaultAdditionalBw],
			Priority:     defaultPriority,
			Weight:       defaultWeight,
			QSchedPolicy: SchedulingPolicy_name[defaultQueueSchedPolicy]},
		DsScheduler: Scheduler{
			Direction:    Direction_name[Direction_DOWNSTREAM],
			AdditionalBw: AdditionalBW_name[defaultAdditionalBw],
			Priority:     defaultPriority,
			Weight:       defaultWeight,
			QSchedPolicy: SchedulingPolicy_name[defaultQueueSchedPolicy]},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

func (t *TechProfileMgr) GetprotoBufParamValue(paramType string, paramKey string) int32 {
	var result int32 = -1

	if paramType == "direction" {
		for key, val := range tp_pb.Direction_value {
			if key == paramKey {
				result = val
			}
		}
	} else if paramType == "discard_policy" {
		for key, val := range tp_pb.DiscardPolicy_value {
			if key == paramKey {
				result = val
			}
		}
	} else if paramType == "sched_policy" {
		for key, val := range tp_pb.SchedulingPolicy_value {
			if key == paramKey {
				log.Debugw("Got value in proto", log.Fields{"key": key, "value": val})
				result = val
			}
		}
	} else if paramType == "additional_bw" {
		for key, val := range tp_pb.AdditionalBW_value {
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

func (t *TechProfileMgr) GetUsScheduler(tpInstance *TechProfile) *tp_pb.SchedulerConfig {
	dir := tp_pb.Direction(t.GetprotoBufParamValue("direction", tpInstance.UsScheduler.Direction))
	if dir == -1 {
		log.Fatal("Error in getting Proto for direction for upstream scheduler")
		return nil
	}
	bw := tp_pb.AdditionalBW(t.GetprotoBufParamValue("additional_bw", tpInstance.UsScheduler.AdditionalBw))
	if bw == -1 {
		log.Fatal("Error in getting Proto for bandwidth for upstream scheduler")
		return nil
	}
	policy := tp_pb.SchedulingPolicy(t.GetprotoBufParamValue("sched_policy", tpInstance.UsScheduler.QSchedPolicy))
	if policy == -1 {
		log.Fatal("Error in getting Proto for scheduling policy for upstream scheduler")
		return nil
	}
	return &tp_pb.SchedulerConfig{
		Direction:    dir,
		AdditionalBw: bw,
		Priority:     tpInstance.UsScheduler.Priority,
		Weight:       tpInstance.UsScheduler.Weight,
		SchedPolicy:  policy}
}

func (t *TechProfileMgr) GetDsScheduler(tpInstance *TechProfile) *tp_pb.SchedulerConfig {

	dir := tp_pb.Direction(t.GetprotoBufParamValue("direction", tpInstance.DsScheduler.Direction))
	if dir == -1 {
		log.Fatal("Error in getting Proto for direction for downstream scheduler")
		return nil
	}
	bw := tp_pb.AdditionalBW(t.GetprotoBufParamValue("additional_bw", tpInstance.DsScheduler.AdditionalBw))
	if bw == -1 {
		log.Fatal("Error in getting Proto for bandwidth for downstream scheduler")
		return nil
	}
	policy := tp_pb.SchedulingPolicy(t.GetprotoBufParamValue("sched_policy", tpInstance.DsScheduler.QSchedPolicy))
	if policy == -1 {
		log.Fatal("Error in getting Proto for scheduling policy for downstream scheduler")
		return nil
	}

	return &tp_pb.SchedulerConfig{
		Direction:    dir,
		AdditionalBw: bw,
		Priority:     tpInstance.DsScheduler.Priority,
		Weight:       tpInstance.DsScheduler.Weight,
		SchedPolicy:  policy}
}

func (t *TechProfileMgr) GetTrafficScheduler(tpInstance *TechProfile, SchedCfg *tp_pb.SchedulerConfig,
	ShapingCfg *tp_pb.TrafficShapingInfo) *tp_pb.TrafficScheduler {

	tSched := &tp_pb.TrafficScheduler{
		Direction:          SchedCfg.Direction,
		AllocId:            tpInstance.UsScheduler.AllocID,
		TrafficShapingInfo: ShapingCfg,
		Scheduler:          SchedCfg}

	return tSched
}

func (tpm *TechProfileMgr) GetTrafficQueues(tp *TechProfile, Dir tp_pb.Direction) []*tp_pb.TrafficQueue {

	var encryp bool
	if Dir == tp_pb.Direction_UPSTREAM {
		// upstream GEM ports
		NumGemPorts := len(tp.UpstreamGemPortAttributeList)
		GemPorts := make([]*tp_pb.TrafficQueue, 0)
		for Count := 0; Count < NumGemPorts; Count++ {
			if tp.UpstreamGemPortAttributeList[Count].AesEncryption == "True" {
				encryp = true
			} else {
				encryp = false
			}
			GemPorts = append(GemPorts, &tp_pb.TrafficQueue{
				Direction:     tp_pb.Direction(tpm.GetprotoBufParamValue("direction", tp.UsScheduler.Direction)),
				GemportId:     tp.UpstreamGemPortAttributeList[Count].GemportID,
				PbitMap:       tp.UpstreamGemPortAttributeList[Count].PbitMap,
				AesEncryption: encryp,
				SchedPolicy:   tp_pb.SchedulingPolicy(tpm.GetprotoBufParamValue("sched_policy", tp.UpstreamGemPortAttributeList[Count].SchedulingPolicy)),
				Priority:      tp.UpstreamGemPortAttributeList[Count].PriorityQueue,
				Weight:        tp.UpstreamGemPortAttributeList[Count].Weight,
				DiscardPolicy: tp_pb.DiscardPolicy(tpm.GetprotoBufParamValue("discard_policy", tp.UpstreamGemPortAttributeList[Count].DiscardPolicy)),
			})
		}
		log.Debugw("Upstream Traffic queue list ", log.Fields{"queuelist": GemPorts})
		return GemPorts
	} else if Dir == tp_pb.Direction_DOWNSTREAM {
		//downstream GEM ports
		NumGemPorts := len(tp.DownstreamGemPortAttributeList)
		GemPorts := make([]*tp_pb.TrafficQueue, 0)
		for Count := 0; Count < NumGemPorts; Count++ {
			if tp.DownstreamGemPortAttributeList[Count].AesEncryption == "True" {
				encryp = true
			} else {
				encryp = false
			}
			GemPorts = append(GemPorts, &tp_pb.TrafficQueue{
				Direction:     tp_pb.Direction(tpm.GetprotoBufParamValue("direction", tp.DsScheduler.Direction)),
				GemportId:     tp.DownstreamGemPortAttributeList[Count].GemportID,
				PbitMap:       tp.DownstreamGemPortAttributeList[Count].PbitMap,
				AesEncryption: encryp,
				SchedPolicy:   tp_pb.SchedulingPolicy(tpm.GetprotoBufParamValue("sched_policy", tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy)),
				Priority:      tp.DownstreamGemPortAttributeList[Count].PriorityQueue,
				Weight:        tp.DownstreamGemPortAttributeList[Count].Weight,
				DiscardPolicy: tp_pb.DiscardPolicy(tpm.GetprotoBufParamValue("discard_policy", tp.DownstreamGemPortAttributeList[Count].DiscardPolicy)),
			})
		}
		log.Debugw("Downstream Traffic queue list ", log.Fields{"queuelist": GemPorts})
		return GemPorts
	}
	return nil
}

func (tpm *TechProfileMgr) GetUsTrafficScheduler(tp *TechProfile) *tp_pb.TrafficScheduler {
	UsScheduler := tpm.GetUsScheduler(tp)

	return &tp_pb.TrafficScheduler{Direction: UsScheduler.Direction,
		AllocId:   tp.UsScheduler.AllocID,
		Scheduler: UsScheduler}
}

func (t *TechProfileMgr) GetGemportIDForPbit(tp *TechProfile, Dir tp_pb.Direction, pbit uint32) uint32 {
	/*
	   Function to get the Gemport ID mapped to a pbit.
	*/
	if Dir == tp_pb.Direction_UPSTREAM {
		// upstream GEM ports
		NumGemPorts := len(tp.UpstreamGemPortAttributeList)
		for Count := 0; Count < NumGemPorts; Count++ {
			NumPbitMaps := len(tp.UpstreamGemPortAttributeList[Count].PbitMap)
			for ICount := 2; ICount < NumPbitMaps; ICount++ {
				if p, err := strconv.Atoi(string(tp.UpstreamGemPortAttributeList[Count].PbitMap[ICount])); err == nil {
					if uint32(ICount-2) == pbit && p == 1 { // Check this p-bit is set
						log.Debugw("Found-US-GEMport-for-Pcp", log.Fields{"pbit": pbit, "GEMport": tp.UpstreamGemPortAttributeList[Count].GemportID})
						return tp.UpstreamGemPortAttributeList[Count].GemportID
					}
				}
			}
		}
	} else if Dir == tp_pb.Direction_DOWNSTREAM {
		//downstream GEM ports
		NumGemPorts := len(tp.DownstreamGemPortAttributeList)
		for Count := 0; Count < NumGemPorts; Count++ {
			NumPbitMaps := len(tp.DownstreamGemPortAttributeList[Count].PbitMap)
			for ICount := 2; ICount < NumPbitMaps; ICount++ {
				if p, err := strconv.Atoi(string(tp.DownstreamGemPortAttributeList[Count].PbitMap[ICount])); err == nil {
					if uint32(ICount-2) == pbit && p == 1 { // Check this p-bit is set
						log.Debugw("Found-DS-GEMport-for-Pcp", log.Fields{"pbit": pbit, "GEMport": tp.DownstreamGemPortAttributeList[Count].GemportID})
						return tp.DownstreamGemPortAttributeList[Count].GemportID
					}
				}
			}
		}
	}
	log.Errorw("No-GemportId-Found-For-Pcp", log.Fields{"pcpVlan": pbit})
	return 0
}

// FindAllTpInstances returns all TechProfile instances for a given TechProfile table-id, pon interface ID and onu ID.
func (t *TechProfileMgr) FindAllTpInstances(techProfiletblID uint32, ponIntf uint32, onuID uint32) []TechProfile {
	var tp TechProfile
	onuTpInstancePath := fmt.Sprintf("%d/%s/pon-{%d}/onu-{%d}", techProfiletblID, t.resourceMgr.GetTechnology(), ponIntf, onuID)

	if kvPairs, _ := t.config.KVBackend.List(onuTpInstancePath); kvPairs != nil {
		tpInstances := make([]TechProfile, 0, len(kvPairs))
		for kvPath, kvPair := range kvPairs {
			if value, err := kvstore.ToByte(kvPair.Value); err == nil {
				if err = json.Unmarshal(value, &tp); err != nil {
					log.Errorw("error-unmarshal-kv-pair", log.Fields{"kvPath": kvPath, "value": value})
					continue
				} else {
					tpInstances = append(tpInstances, tp)
				}
			}
		}
		return tpInstances
	}
	return nil
}
