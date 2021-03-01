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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/opencord/voltha-lib-go/v4/pkg/db"

	"github.com/opencord/voltha-lib-go/v4/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	tp_pb "github.com/opencord/voltha-protos/v4/go/tech_profile"
)

// Interface to pon resource manager APIs
type iPonResourceMgr interface {
	GetResourceID(ctx context.Context, IntfID uint32, ResourceType string, NumIDs uint32) ([]uint32, error)
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
var uniPortNameFormat = regexp.MustCompile(`^olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}$`)

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
	defaultAESEncryption     = "True"
	defaultPriorityQueue     = 0
	defaultQueueWeight       = 0
	defaultMaxQueueSize      = "auto"
	defaultdropPolicy        = DiscardPolicy_TailDrop
	defaultSchedulePolicy    = SchedulingPolicy_WRR
	defaultIsMulticast       = "False"
	defaultAccessControlList = "224.0.0.0-239.255.255.255"
	defaultMcastGemID        = 4069
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
	IsMulticast      string        `json:"is_multicast"`
	DControlList     string        `json:"dynamic_access_control_list"`
	SControlList     string        `json:"static_access_control_list"`
	McastGemID       uint32        `json:"multicast_gem_id"`
}

// Instance of Scheduler
type IScheduler struct {
	AllocID      uint32 `json:"alloc_id"`
	Direction    string `json:"direction"`
	AdditionalBw string `json:"additional_bw"`
	Priority     uint32 `json:"priority"`
	Weight       uint32 `json:"weight"`
	QSchedPolicy string `json:"q_sched_policy"`
}

// Instance of GemPortAttribute
type IGemPortAttribute struct {
	GemportID        uint32        `json:"gemport_id"`
	MaxQueueSize     string        `json:"max_q_size"`
	PbitMap          string        `json:"pbit_map"`
	AesEncryption    string        `json:"aes_encryption"`
	SchedulingPolicy string        `json:"scheduling_policy"`
	PriorityQueue    uint32        `json:"priority_q"`
	Weight           uint32        `json:"weight"`
	DiscardPolicy    string        `json:"discard_policy"`
	DiscardConfig    DiscardConfig `json:"discard_config"`
	IsMulticast      string        `json:"is_multicast"`
	DControlList     string        `json:"dynamic_access_control_list"`
	SControlList     string        `json:"static_access_control_list"`
	McastGemID       uint32        `json:"multicast_gem_id"`
}

type TechProfileMgr struct {
	config            *TechProfileFlags
	resourceMgr       iPonResourceMgr
	GemPortIDMgmtLock sync.RWMutex
	AllocIDMgmtLock   sync.RWMutex
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
	UsScheduler                    IScheduler          `json:"us_scheduler"`
	DsScheduler                    IScheduler          `json:"ds_scheduler"`
	UpstreamGemPortAttributeList   []IGemPortAttribute `json:"upstream_gem_port_attribute_list"`
	DownstreamGemPortAttributeList []IGemPortAttribute `json:"downstream_gem_port_attribute_list"`
}

// QThresholds struct for EPON
type QThresholds struct {
	QThreshold1 uint32 `json:"q_threshold1"`
	QThreshold2 uint32 `json:"q_threshold2"`
	QThreshold3 uint32 `json:"q_threshold3"`
	QThreshold4 uint32 `json:"q_threshold4"`
	QThreshold5 uint32 `json:"q_threshold5"`
	QThreshold6 uint32 `json:"q_threshold6"`
	QThreshold7 uint32 `json:"q_threshold7"`
}

// UpstreamQueueAttribute struct for EPON
type UpstreamQueueAttribute struct {
	MaxQueueSize              string        `json:"max_q_size"`
	PbitMap                   string        `json:"pbit_map"`
	AesEncryption             string        `json:"aes_encryption"`
	TrafficType               string        `json:"traffic_type"`
	UnsolicitedGrantSize      uint32        `json:"unsolicited_grant_size"`
	NominalInterval           uint32        `json:"nominal_interval"`
	ToleratedPollJitter       uint32        `json:"tolerated_poll_jitter"`
	RequestTransmissionPolicy uint32        `json:"request_transmission_policy"`
	NumQueueSet               uint32        `json:"num_q_sets"`
	QThresholds               QThresholds   `json:"q_thresholds"`
	SchedulingPolicy          string        `json:"scheduling_policy"`
	PriorityQueue             uint32        `json:"priority_q"`
	Weight                    uint32        `json:"weight"`
	DiscardPolicy             string        `json:"discard_policy"`
	DiscardConfig             DiscardConfig `json:"discard_config"`
}

// Default EPON constants
const (
	defaultPakageType = "B"
)
const (
	defaultTrafficType               = "BE"
	defaultUnsolicitedGrantSize      = 0
	defaultNominalInterval           = 0
	defaultToleratedPollJitter       = 0
	defaultRequestTransmissionPolicy = 0
	defaultNumQueueSet               = 2
)
const (
	defaultQThreshold1 = 5500
	defaultQThreshold2 = 0
	defaultQThreshold3 = 0
	defaultQThreshold4 = 0
	defaultQThreshold5 = 0
	defaultQThreshold6 = 0
	defaultQThreshold7 = 0
)

// DownstreamQueueAttribute struct for EPON
type DownstreamQueueAttribute struct {
	MaxQueueSize     string        `json:"max_q_size"`
	PbitMap          string        `json:"pbit_map"`
	AesEncryption    string        `json:"aes_encryption"`
	SchedulingPolicy string        `json:"scheduling_policy"`
	PriorityQueue    uint32        `json:"priority_q"`
	Weight           uint32        `json:"weight"`
	DiscardPolicy    string        `json:"discard_policy"`
	DiscardConfig    DiscardConfig `json:"discard_config"`
}

// iUpstreamQueueAttribute struct for EPON
type iUpstreamQueueAttribute struct {
	GemportID                 uint32        `json:"q_id"`
	MaxQueueSize              string        `json:"max_q_size"`
	PbitMap                   string        `json:"pbit_map"`
	AesEncryption             string        `json:"aes_encryption"`
	TrafficType               string        `json:"traffic_type"`
	UnsolicitedGrantSize      uint32        `json:"unsolicited_grant_size"`
	NominalInterval           uint32        `json:"nominal_interval"`
	ToleratedPollJitter       uint32        `json:"tolerated_poll_jitter"`
	RequestTransmissionPolicy uint32        `json:"request_transmission_policy"`
	NumQueueSet               uint32        `json:"num_q_sets"`
	QThresholds               QThresholds   `json:"q_thresholds"`
	SchedulingPolicy          string        `json:"scheduling_policy"`
	PriorityQueue             uint32        `json:"priority_q"`
	Weight                    uint32        `json:"weight"`
	DiscardPolicy             string        `json:"discard_policy"`
	DiscardConfig             DiscardConfig `json:"discard_config"`
}

// iDownstreamQueueAttribute struct for EPON
type iDownstreamQueueAttribute struct {
	GemportID        uint32        `json:"q_id"`
	MaxQueueSize     string        `json:"max_q_size"`
	PbitMap          string        `json:"pbit_map"`
	AesEncryption    string        `json:"aes_encryption"`
	SchedulingPolicy string        `json:"scheduling_policy"`
	PriorityQueue    uint32        `json:"priority_q"`
	Weight           uint32        `json:"weight"`
	DiscardPolicy    string        `json:"discard_policy"`
	DiscardConfig    DiscardConfig `json:"discard_config"`
}

// EponAttribute struct for EPON
type EponAttribute struct {
	PackageType string `json:"pakage_type"`
}

// DefaultTechProfile struct for EPON
type DefaultEponProfile struct {
	Name                         string                     `json:"name"`
	ProfileType                  string                     `json:"profile_type"`
	Version                      int                        `json:"version"`
	NumGemPorts                  uint32                     `json:"num_gem_ports"`
	InstanceCtrl                 InstanceControl            `json:"instance_control"`
	EponAttribute                EponAttribute              `json:"epon_attribute"`
	UpstreamQueueAttributeList   []UpstreamQueueAttribute   `json:"upstream_queue_attribute_list"`
	DownstreamQueueAttributeList []DownstreamQueueAttribute `json:"downstream_queue_attribute_list"`
}

// TechProfile struct for EPON
type EponProfile struct {
	Name                         string                      `json:"name"`
	SubscriberIdentifier         string                      `json:"subscriber_identifier"`
	ProfileType                  string                      `json:"profile_type"`
	Version                      int                         `json:"version"`
	NumGemPorts                  uint32                      `json:"num_gem_ports"`
	InstanceCtrl                 InstanceControl             `json:"instance_control"`
	EponAttribute                EponAttribute               `json:"epon_attribute"`
	AllocID                      uint32                      `json:"llid"`
	UpstreamQueueAttributeList   []iUpstreamQueueAttribute   `json:"upstream_queue_attribute_list"`
	DownstreamQueueAttributeList []iDownstreamQueueAttribute `json:"downstream_queue_attribute_list"`
}

const (
	xgspon = "XGS-PON"
	gpon   = "GPON"
	epon   = "EPON"
)

func (t *TechProfileMgr) SetKVClient(ctx context.Context, pathPrefix string) *db.Backend {
	kvClient, err := newKVClient(ctx, t.config.KVStoreType, t.config.KVStoreAddress, t.config.KVStoreTimeout)
	if err != nil {
		logger.Errorw(ctx, "failed-to-create-kv-client",
			log.Fields{
				"type": t.config.KVStoreType, "address": t.config.KVStoreAddress,
				"timeout": t.config.KVStoreTimeout, "prefix": pathPrefix,
				"error": err.Error(),
			})
		return nil
	}
	return &db.Backend{
		Client:     kvClient,
		StoreType:  t.config.KVStoreType,
		Address:    t.config.KVStoreAddress,
		Timeout:    t.config.KVStoreTimeout,
		PathPrefix: pathPrefix}

	/* TODO : Make sure direct call to NewBackend is working fine with backend , currently there is some
		  issue between kv store and backend , core is not calling NewBackend directly
	 kv := model.NewBackend(t.config.KVStoreType, t.config.KVStoreHost, t.config.KVStorePort,
								  t.config.KVStoreTimeout,  kvStoreTechProfilePathPrefix)
	*/
}

func newKVClient(ctx context.Context, storeType string, address string, timeout time.Duration) (kvstore.Client, error) {

	logger.Infow(ctx, "kv-store", log.Fields{"storeType": storeType, "address": address})
	switch storeType {
	case "etcd":
		return kvstore.NewEtcdClient(ctx, address, timeout, log.WarnLevel)
	}
	return nil, errors.New("unsupported-kv-store")
}

func NewTechProfile(ctx context.Context, resourceMgr iPonResourceMgr, KVStoreType string, KVStoreAddress string, basePathKvStore string) (*TechProfileMgr, error) {
	var techprofileObj TechProfileMgr
	logger.Debug(ctx, "Initializing techprofile Manager")
	techprofileObj.config = NewTechProfileFlags(KVStoreType, KVStoreAddress, basePathKvStore)
	techprofileObj.config.KVBackend = techprofileObj.SetKVClient(ctx, techprofileObj.config.TPKVPathPrefix)
	techprofileObj.config.DefaultTpKVBackend = techprofileObj.SetKVClient(ctx, techprofileObj.config.defaultTpKvPathPrefix)
	if techprofileObj.config.KVBackend == nil {
		logger.Error(ctx, "Failed to initialize KV backend\n")
		return nil, errors.New("KV backend init failed")
	}
	techprofileObj.resourceMgr = resourceMgr
	logger.Debug(ctx, "Initializing techprofile object instance success")
	return &techprofileObj, nil
}

func (t *TechProfileMgr) GetTechProfileInstanceKVPath(ctx context.Context, techProfiletblID uint32, uniPortName string) string {
	logger.Debugw(ctx, "get-tp-instance-kv-path", log.Fields{
		"uniPortName": uniPortName,
		"tpId":        techProfiletblID,
	})
	return fmt.Sprintf(t.config.TPInstanceKVPath, t.resourceMgr.GetTechnology(), techProfiletblID, uniPortName)
}

func (t *TechProfileMgr) GetTPInstanceFromKVStore(ctx context.Context, techProfiletblID uint32, path string) (interface{}, error) {
	var err error
	var kvResult *kvstore.KVPair
	var KvTpIns TechProfile
	var KvEponIns EponProfile
	var resPtr interface{}
	// For example:
	// tpInstPath like "XGS-PON/64/uni_port_name"
	// is broken into ["XGS-PON" "64" ...]
	pathSlice := regexp.MustCompile(`/`).Split(path, -1)
	switch pathSlice[0] {
	case xgspon, gpon:
		resPtr = &KvTpIns
	case epon:
		resPtr = &KvEponIns
	default:
		logger.Errorw(ctx, "unknown-tech", log.Fields{"tech": pathSlice[0]})
		return nil, fmt.Errorf("unknown-tech-%s", pathSlice[0])
	}

	kvResult, _ = t.config.KVBackend.Get(ctx, path)
	if kvResult == nil {
		logger.Infow(ctx, "tp-instance-not-found-on-kv", log.Fields{"key": path})
		return nil, nil
	} else {
		if value, err := kvstore.ToByte(kvResult.Value); err == nil {
			if err = json.Unmarshal(value, resPtr); err != nil {
				logger.Errorw(ctx, "error-unmarshal-kv-result", log.Fields{"key": path, "value": value})
				return nil, errors.New("error-unmarshal-kv-result")
			} else {
				return resPtr, nil
			}
		}
	}
	return nil, err
}

func (t *TechProfileMgr) addTechProfInstanceToKVStore(ctx context.Context, techProfiletblID uint32, uniPortName string, tpInstance *TechProfile) error {
	path := t.GetTechProfileInstanceKVPath(ctx, techProfiletblID, uniPortName)
	logger.Debugw(ctx, "Adding techprof instance to kvstore", log.Fields{"key": path, "tpinstance": tpInstance})
	tpInstanceJson, err := json.Marshal(*tpInstance)
	if err == nil {
		// Backend will convert JSON byte array into string format
		logger.Debugw(ctx, "Storing tech profile instance to KV Store", log.Fields{"key": path, "val": tpInstanceJson})
		err = t.config.KVBackend.Put(ctx, path, tpInstanceJson)
	} else {
		logger.Errorw(ctx, "Error in marshaling into Json format", log.Fields{"key": path, "tpinstance": tpInstance})
	}
	return err
}

func (t *TechProfileMgr) addEponProfInstanceToKVStore(ctx context.Context, techProfiletblID uint32, uniPortName string, tpInstance *EponProfile) error {
	path := t.GetTechProfileInstanceKVPath(ctx, techProfiletblID, uniPortName)
	logger.Debugw(ctx, "Adding techprof instance to kvstore", log.Fields{"key": path, "tpinstance": tpInstance})
	tpInstanceJson, err := json.Marshal(*tpInstance)
	if err == nil {
		// Backend will convert JSON byte array into string format
		logger.Debugw(ctx, "Storing tech profile instance to KV Store", log.Fields{"key": path, "val": tpInstanceJson})
		err = t.config.KVBackend.Put(ctx, path, tpInstanceJson)
	} else {
		logger.Errorw(ctx, "Error in marshaling into Json format", log.Fields{"key": path, "tpinstance": tpInstance})
	}
	return err
}

func (t *TechProfileMgr) getTPFromKVStore(ctx context.Context, techProfiletblID uint32) *DefaultTechProfile {
	var kvtechprofile DefaultTechProfile
	key := fmt.Sprintf(t.config.TPFileKVPath, t.resourceMgr.GetTechnology(), techProfiletblID)
	logger.Debugw(ctx, "Getting techprofile from KV store", log.Fields{"techProfiletblID": techProfiletblID, "Key": key})
	kvresult, err := t.config.DefaultTpKVBackend.Get(ctx, key)
	if err != nil {
		logger.Errorw(ctx, "Error while fetching value from KV store", log.Fields{"key": key})
		return nil
	}
	if kvresult != nil {
		/* Backend will return Value in string format,needs to be converted to []byte before unmarshal*/
		if value, err := kvstore.ToByte(kvresult.Value); err == nil {
			if err = json.Unmarshal(value, &kvtechprofile); err != nil {
				logger.Errorw(ctx, "Error unmarshaling techprofile fetched from KV store", log.Fields{"techProfiletblID": techProfiletblID, "error": err, "techprofile_json": value})
				return nil
			}

			logger.Debugw(ctx, "Success fetched techprofile from KV store", log.Fields{"techProfiletblID": techProfiletblID, "value": kvtechprofile})
			return &kvtechprofile
		}
	}
	return nil
}

func (t *TechProfileMgr) getEponTPFromKVStore(ctx context.Context, techProfiletblID uint32) *DefaultEponProfile {
	var kvtechprofile DefaultEponProfile
	key := fmt.Sprintf(t.config.TPFileKVPath, t.resourceMgr.GetTechnology(), techProfiletblID)
	logger.Debugw(ctx, "Getting techprofile from KV store", log.Fields{"techProfiletblID": techProfiletblID, "Key": key})
	kvresult, err := t.config.DefaultTpKVBackend.Get(ctx, key)
	if err != nil {
		logger.Errorw(ctx, "Error while fetching value from KV store", log.Fields{"key": key})
		return nil
	}
	if kvresult != nil {
		/* Backend will return Value in string format,needs to be converted to []byte before unmarshal*/
		if value, err := kvstore.ToByte(kvresult.Value); err == nil {
			if err = json.Unmarshal(value, &kvtechprofile); err != nil {
				logger.Errorw(ctx, "Error unmarshaling techprofile fetched from KV store", log.Fields{"techProfiletblID": techProfiletblID, "error": err, "techprofile_json": value})
				return nil
			}

			logger.Debugw(ctx, "Success fetched techprofile from KV store", log.Fields{"techProfiletblID": techProfiletblID, "value": kvtechprofile})
			return &kvtechprofile
		}
	}
	return nil
}

func (t *TechProfileMgr) CreateTechProfInstance(ctx context.Context, techProfiletblID uint32, uniPortName string, intfId uint32) (interface{}, error) {
	var tpInstance *TechProfile
	var tpEponInstance *EponProfile

	logger.Infow(ctx, "creating-tp-instance", log.Fields{"tableid": techProfiletblID, "uni": uniPortName, "intId": intfId})

	// Make sure the uniPortName is as per format pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
	if !uniPortNameFormat.Match([]byte(uniPortName)) {
		logger.Errorw(ctx, "uni-port-name-not-confirming-to-format", log.Fields{"uniPortName": uniPortName})
		return nil, errors.New("uni-port-name-not-confirming-to-format")
	}
	tpInstancePath := t.GetTechProfileInstanceKVPath(ctx, techProfiletblID, uniPortName)
	// For example:
	// tpInstPath like "XGS-PON/64/uni_port_name"
	// is broken into ["XGS-PON" "64" ...]
	pathSlice := regexp.MustCompile(`/`).Split(tpInstancePath, -1)
	if pathSlice[0] == epon {
		tp := t.getEponTPFromKVStore(ctx, techProfiletblID)
		if tp != nil {
			if err := t.validateInstanceControlAttr(ctx, tp.InstanceCtrl); err != nil {
				logger.Error(ctx, "invalid-instance-ctrl-attr--using-default-tp")
				tp = t.getDefaultEponProfile(ctx)
			} else {
				logger.Infow(ctx, "using-specified-tp-from-kv-store", log.Fields{"tpid": techProfiletblID})
			}
		} else {
			logger.Info(ctx, "tp-not-found-on-kv--creating-default-tp")
			tp = t.getDefaultEponProfile(ctx)
		}

		if tpEponInstance = t.allocateEponTPInstance(ctx, uniPortName, tp, intfId, tpInstancePath); tpEponInstance == nil {
			logger.Error(ctx, "tp-intance-allocation-failed")
			return nil, errors.New("tp-intance-allocation-failed")
		}
		if err := t.addEponProfInstanceToKVStore(ctx, techProfiletblID, uniPortName, tpEponInstance); err != nil {
			logger.Errorw(ctx, "error-adding-tp-to-kv-store", log.Fields{"tableid": techProfiletblID, "uni": uniPortName})
			return nil, errors.New("error-adding-tp-to-kv-store")
		}
		logger.Infow(ctx, "tp-added-to-kv-store-successfully",
			log.Fields{"tpid": techProfiletblID, "uni": uniPortName, "intfId": intfId})
		return tpEponInstance, nil
	} else {
		tp := t.getTPFromKVStore(ctx, techProfiletblID)
		if tp != nil {
			if err := t.validateInstanceControlAttr(ctx, tp.InstanceCtrl); err != nil {
				logger.Error(ctx, "invalid-instance-ctrl-attr--using-default-tp")
				tp = t.getDefaultTechProfile(ctx)
			} else {
				logger.Infow(ctx, "using-specified-tp-from-kv-store", log.Fields{"tpid": techProfiletblID})
			}
		} else {
			logger.Info(ctx, "tp-not-found-on-kv--creating-default-tp")
			tp = t.getDefaultTechProfile(ctx)
		}

		if tpInstance = t.allocateTPInstance(ctx, uniPortName, tp, intfId, tpInstancePath); tpInstance == nil {
			logger.Error(ctx, "tp-intance-allocation-failed")
			return nil, errors.New("tp-intance-allocation-failed")
		}
		if err := t.addTechProfInstanceToKVStore(ctx, techProfiletblID, uniPortName, tpInstance); err != nil {
			logger.Errorw(ctx, "error-adding-tp-to-kv-store", log.Fields{"tableid": techProfiletblID, "uni": uniPortName})
			return nil, errors.New("error-adding-tp-to-kv-store")
		}
		logger.Infow(ctx, "tp-added-to-kv-store-successfully",
			log.Fields{"tpid": techProfiletblID, "uni": uniPortName, "intfId": intfId})
		return tpInstance, nil
	}
}

func (t *TechProfileMgr) DeleteTechProfileInstance(ctx context.Context, techProfiletblID uint32, uniPortName string) error {
	path := t.GetTechProfileInstanceKVPath(ctx, techProfiletblID, uniPortName)
	return t.config.KVBackend.Delete(ctx, path)
}

func (t *TechProfileMgr) validateInstanceControlAttr(ctx context.Context, instCtl InstanceControl) error {
	if instCtl.Onu != "single-instance" && instCtl.Onu != "multi-instance" {
		logger.Errorw(ctx, "invalid-onu-instance-control-attribute", log.Fields{"onu-inst": instCtl.Onu})
		return errors.New("invalid-onu-instance-ctl-attr")
	}

	if instCtl.Uni != "single-instance" && instCtl.Uni != "multi-instance" {
		logger.Errorw(ctx, "invalid-uni-instance-control-attribute", log.Fields{"uni-inst": instCtl.Uni})
		return errors.New("invalid-uni-instance-ctl-attr")
	}

	if instCtl.Uni == "multi-instance" {
		logger.Error(ctx, "uni-multi-instance-tp-not-supported")
		return errors.New("uni-multi-instance-tp-not-supported")
	}

	return nil
}

func (t *TechProfileMgr) allocateTPInstance(ctx context.Context, uniPortName string, tp *DefaultTechProfile, intfId uint32, tpInstPath string) *TechProfile {

	var usGemPortAttributeList []IGemPortAttribute
	var dsGemPortAttributeList []IGemPortAttribute
	var dsMulticastGemAttributeList []IGemPortAttribute
	var dsUnicastGemAttributeList []IGemPortAttribute
	var tcontIDs []uint32
	var gemPorts []uint32
	var err error

	logger.Infow(ctx, "Allocating TechProfileMgr instance from techprofile template", log.Fields{"uniPortName": uniPortName, "intfId": intfId, "numGem": tp.NumGemPorts})

	if tp.InstanceCtrl.Onu == "multi-instance" {
		t.AllocIDMgmtLock.Lock()
		tcontIDs, err = t.resourceMgr.GetResourceID(ctx, intfId, t.resourceMgr.GetResourceTypeAllocID(), 1)
		t.AllocIDMgmtLock.Unlock()
		if err != nil {
			logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
			return nil
		}
	} else { // "single-instance"
		if tpInst, err := t.getSingleInstanceTp(ctx, tpInstPath); err != nil {
			logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
			return nil
		} else if tpInst == nil {
			// No "single-instance" tp found on one any uni port for the given TP ID
			// Allocate a new TcontID or AllocID
			t.AllocIDMgmtLock.Lock()
			tcontIDs, err = t.resourceMgr.GetResourceID(ctx, intfId, t.resourceMgr.GetResourceTypeAllocID(), 1)
			t.AllocIDMgmtLock.Unlock()
			if err != nil {
				logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
				return nil
			}
		} else {
			// Use the alloc-id from the existing TpInstance
			tcontIDs = append(tcontIDs, tpInst.UsScheduler.AllocID)
		}
	}
	logger.Debugw(ctx, "Num GEM ports in TP:", log.Fields{"NumGemPorts": tp.NumGemPorts})
	t.GemPortIDMgmtLock.Lock()
	gemPorts, err = t.resourceMgr.GetResourceID(ctx, intfId, t.resourceMgr.GetResourceTypeGemPortID(), tp.NumGemPorts)
	t.GemPortIDMgmtLock.Unlock()
	if err != nil {
		logger.Errorw(ctx, "Error getting gemport ids from rsrcrMgr", log.Fields{"intfId": intfId, "numGemports": tp.NumGemPorts})
		return nil
	}
	logger.Infow(ctx, "Allocated tconts and GEM ports successfully", log.Fields{"tconts": tcontIDs, "gemports": gemPorts})
	for index := 0; index < int(tp.NumGemPorts); index++ {
		usGemPortAttributeList = append(usGemPortAttributeList,
			IGemPortAttribute{GemportID: gemPorts[index],
				MaxQueueSize:     tp.UpstreamGemPortAttributeList[index].MaxQueueSize,
				PbitMap:          tp.UpstreamGemPortAttributeList[index].PbitMap,
				AesEncryption:    tp.UpstreamGemPortAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.UpstreamGemPortAttributeList[index].SchedulingPolicy,
				PriorityQueue:    tp.UpstreamGemPortAttributeList[index].PriorityQueue,
				Weight:           tp.UpstreamGemPortAttributeList[index].Weight,
				DiscardPolicy:    tp.UpstreamGemPortAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.UpstreamGemPortAttributeList[index].DiscardConfig})
	}

	logger.Info(ctx, "length of DownstreamGemPortAttributeList", len(tp.DownstreamGemPortAttributeList))
	//put multicast and unicast downstream GEM port attributes in different lists first
	for index := 0; index < int(len(tp.DownstreamGemPortAttributeList)); index++ {
		if isMulticastGem(tp.DownstreamGemPortAttributeList[index].IsMulticast) {
			dsMulticastGemAttributeList = append(dsMulticastGemAttributeList,
				IGemPortAttribute{
					McastGemID:       tp.DownstreamGemPortAttributeList[index].McastGemID,
					MaxQueueSize:     tp.DownstreamGemPortAttributeList[index].MaxQueueSize,
					PbitMap:          tp.DownstreamGemPortAttributeList[index].PbitMap,
					AesEncryption:    tp.DownstreamGemPortAttributeList[index].AesEncryption,
					SchedulingPolicy: tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
					PriorityQueue:    tp.DownstreamGemPortAttributeList[index].PriorityQueue,
					Weight:           tp.DownstreamGemPortAttributeList[index].Weight,
					DiscardPolicy:    tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
					DiscardConfig:    tp.DownstreamGemPortAttributeList[index].DiscardConfig,
					IsMulticast:      tp.DownstreamGemPortAttributeList[index].IsMulticast,
					DControlList:     tp.DownstreamGemPortAttributeList[index].DControlList,
					SControlList:     tp.DownstreamGemPortAttributeList[index].SControlList})
		} else {
			dsUnicastGemAttributeList = append(dsUnicastGemAttributeList,
				IGemPortAttribute{
					MaxQueueSize:     tp.DownstreamGemPortAttributeList[index].MaxQueueSize,
					PbitMap:          tp.DownstreamGemPortAttributeList[index].PbitMap,
					AesEncryption:    tp.DownstreamGemPortAttributeList[index].AesEncryption,
					SchedulingPolicy: tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
					PriorityQueue:    tp.DownstreamGemPortAttributeList[index].PriorityQueue,
					Weight:           tp.DownstreamGemPortAttributeList[index].Weight,
					DiscardPolicy:    tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
					DiscardConfig:    tp.DownstreamGemPortAttributeList[index].DiscardConfig})
		}
	}
	//add unicast downstream GEM ports to dsGemPortAttributeList
	for index := 0; index < int(tp.NumGemPorts); index++ {
		dsGemPortAttributeList = append(dsGemPortAttributeList,
			IGemPortAttribute{GemportID: gemPorts[index],
				MaxQueueSize:     dsUnicastGemAttributeList[index].MaxQueueSize,
				PbitMap:          dsUnicastGemAttributeList[index].PbitMap,
				AesEncryption:    dsUnicastGemAttributeList[index].AesEncryption,
				SchedulingPolicy: dsUnicastGemAttributeList[index].SchedulingPolicy,
				PriorityQueue:    dsUnicastGemAttributeList[index].PriorityQueue,
				Weight:           dsUnicastGemAttributeList[index].Weight,
				DiscardPolicy:    dsUnicastGemAttributeList[index].DiscardPolicy,
				DiscardConfig:    dsUnicastGemAttributeList[index].DiscardConfig})
	}
	//add multicast GEM ports to dsGemPortAttributeList afterwards
	for k := range dsMulticastGemAttributeList {
		dsGemPortAttributeList = append(dsGemPortAttributeList, dsMulticastGemAttributeList[k])
	}

	return &TechProfile{
		SubscriberIdentifier: uniPortName,
		Name:                 tp.Name,
		ProfileType:          tp.ProfileType,
		Version:              tp.Version,
		NumGemPorts:          tp.NumGemPorts,
		InstanceCtrl:         tp.InstanceCtrl,
		UsScheduler: IScheduler{
			AllocID:      tcontIDs[0],
			Direction:    tp.UsScheduler.Direction,
			AdditionalBw: tp.UsScheduler.AdditionalBw,
			Priority:     tp.UsScheduler.Priority,
			Weight:       tp.UsScheduler.Weight,
			QSchedPolicy: tp.UsScheduler.QSchedPolicy},
		DsScheduler: IScheduler{
			AllocID:      tcontIDs[0],
			Direction:    tp.DsScheduler.Direction,
			AdditionalBw: tp.DsScheduler.AdditionalBw,
			Priority:     tp.DsScheduler.Priority,
			Weight:       tp.DsScheduler.Weight,
			QSchedPolicy: tp.DsScheduler.QSchedPolicy},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

// allocateTPInstance function for EPON
func (t *TechProfileMgr) allocateEponTPInstance(ctx context.Context, uniPortName string, tp *DefaultEponProfile, intfId uint32, tpInstPath string) *EponProfile {

	var usQueueAttributeList []iUpstreamQueueAttribute
	var dsQueueAttributeList []iDownstreamQueueAttribute
	var tcontIDs []uint32
	var gemPorts []uint32
	var err error

	logger.Infow(ctx, "Allocating TechProfileMgr instance from techprofile template", log.Fields{"uniPortName": uniPortName, "intfId": intfId, "numGem": tp.NumGemPorts})

	if tp.InstanceCtrl.Onu == "multi-instance" {
		if tcontIDs, err = t.resourceMgr.GetResourceID(ctx, intfId, t.resourceMgr.GetResourceTypeAllocID(), 1); err != nil {
			logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
			return nil
		}
	} else { // "single-instance"
		if tpInst, err := t.getSingleInstanceEponTp(ctx, tpInstPath); err != nil {
			logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
			return nil
		} else if tpInst == nil {
			// No "single-instance" tp found on one any uni port for the given TP ID
			// Allocate a new TcontID or AllocID
			if tcontIDs, err = t.resourceMgr.GetResourceID(ctx, intfId, t.resourceMgr.GetResourceTypeAllocID(), 1); err != nil {
				logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"intfId": intfId})
				return nil
			}
		} else {
			// Use the alloc-id from the existing TpInstance
			tcontIDs = append(tcontIDs, tpInst.AllocID)
		}
	}
	logger.Debugw(ctx, "Num GEM ports in TP:", log.Fields{"NumGemPorts": tp.NumGemPorts})
	if gemPorts, err = t.resourceMgr.GetResourceID(ctx, intfId, t.resourceMgr.GetResourceTypeGemPortID(), tp.NumGemPorts); err != nil {
		logger.Errorw(ctx, "Error getting gemport ids from rsrcrMgr", log.Fields{"intfId": intfId, "numGemports": tp.NumGemPorts})
		return nil
	}
	logger.Infow(ctx, "Allocated tconts and GEM ports successfully", log.Fields{"tconts": tcontIDs, "gemports": gemPorts})
	for index := 0; index < int(tp.NumGemPorts); index++ {
		usQueueAttributeList = append(usQueueAttributeList,
			iUpstreamQueueAttribute{GemportID: gemPorts[index],
				MaxQueueSize:              tp.UpstreamQueueAttributeList[index].MaxQueueSize,
				PbitMap:                   tp.UpstreamQueueAttributeList[index].PbitMap,
				AesEncryption:             tp.UpstreamQueueAttributeList[index].AesEncryption,
				TrafficType:               tp.UpstreamQueueAttributeList[index].TrafficType,
				UnsolicitedGrantSize:      tp.UpstreamQueueAttributeList[index].UnsolicitedGrantSize,
				NominalInterval:           tp.UpstreamQueueAttributeList[index].NominalInterval,
				ToleratedPollJitter:       tp.UpstreamQueueAttributeList[index].ToleratedPollJitter,
				RequestTransmissionPolicy: tp.UpstreamQueueAttributeList[index].RequestTransmissionPolicy,
				NumQueueSet:               tp.UpstreamQueueAttributeList[index].NumQueueSet,
				QThresholds:               tp.UpstreamQueueAttributeList[index].QThresholds,
				SchedulingPolicy:          tp.UpstreamQueueAttributeList[index].SchedulingPolicy,
				PriorityQueue:             tp.UpstreamQueueAttributeList[index].PriorityQueue,
				Weight:                    tp.UpstreamQueueAttributeList[index].Weight,
				DiscardPolicy:             tp.UpstreamQueueAttributeList[index].DiscardPolicy,
				DiscardConfig:             tp.UpstreamQueueAttributeList[index].DiscardConfig})
	}

	logger.Info(ctx, "length of DownstreamGemPortAttributeList", len(tp.DownstreamQueueAttributeList))
	for index := 0; index < int(tp.NumGemPorts); index++ {
		dsQueueAttributeList = append(dsQueueAttributeList,
			iDownstreamQueueAttribute{GemportID: gemPorts[index],
				MaxQueueSize:     tp.DownstreamQueueAttributeList[index].MaxQueueSize,
				PbitMap:          tp.DownstreamQueueAttributeList[index].PbitMap,
				AesEncryption:    tp.DownstreamQueueAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.DownstreamQueueAttributeList[index].SchedulingPolicy,
				PriorityQueue:    tp.DownstreamQueueAttributeList[index].PriorityQueue,
				Weight:           tp.DownstreamQueueAttributeList[index].Weight,
				DiscardPolicy:    tp.DownstreamQueueAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.DownstreamQueueAttributeList[index].DiscardConfig})
	}

	return &EponProfile{
		SubscriberIdentifier:         uniPortName,
		Name:                         tp.Name,
		ProfileType:                  tp.ProfileType,
		Version:                      tp.Version,
		NumGemPorts:                  tp.NumGemPorts,
		InstanceCtrl:                 tp.InstanceCtrl,
		EponAttribute:                tp.EponAttribute,
		AllocID:                      tcontIDs[0],
		UpstreamQueueAttributeList:   usQueueAttributeList,
		DownstreamQueueAttributeList: dsQueueAttributeList}
}

// getSingleInstanceTp returns another TpInstance for an ONU on a different
// uni port for the same TP ID, if it finds one, else nil.
func (t *TechProfileMgr) getSingleInstanceTp(ctx context.Context, tpPath string) (*TechProfile, error) {
	var tpInst TechProfile

	// For example:
	// tpPath like "service/voltha/technology_profiles/xgspon/64/pon-{0}/onu-{1}/uni-{1}"
	// is broken into ["service/voltha/technology_profiles/xgspon/64/pon-{0}/onu-{1}" ""]
	uniPathSlice := regexp.MustCompile(`/uni-{[0-9]+}$`).Split(tpPath, 2)
	kvPairs, _ := t.config.KVBackend.List(ctx, uniPathSlice[0])

	// Find a valid TP Instance among all the UNIs of that ONU for the given TP ID
	for keyPath, kvPair := range kvPairs {
		if value, err := kvstore.ToByte(kvPair.Value); err == nil {
			if err = json.Unmarshal(value, &tpInst); err != nil {
				logger.Errorw(ctx, "error-unmarshal-kv-pair", log.Fields{"keyPath": keyPath, "value": value})
				return nil, errors.New("error-unmarshal-kv-pair")
			} else {
				logger.Debugw(ctx, "found-valid-tp-instance-on-another-uni", log.Fields{"keyPath": keyPath})
				return &tpInst, nil
			}
		}
	}
	return nil, nil
}

func (t *TechProfileMgr) getSingleInstanceEponTp(ctx context.Context, tpPath string) (*EponProfile, error) {
	var tpInst EponProfile

	// For example:
	// tpPath like "service/voltha/technology_profiles/xgspon/64/pon-{0}/onu-{1}/uni-{1}"
	// is broken into ["service/voltha/technology_profiles/xgspon/64/pon-{0}/onu-{1}" ""]
	uniPathSlice := regexp.MustCompile(`/uni-{[0-9]+}$`).Split(tpPath, 2)
	kvPairs, _ := t.config.KVBackend.List(ctx, uniPathSlice[0])

	// Find a valid TP Instance among all the UNIs of that ONU for the given TP ID
	for keyPath, kvPair := range kvPairs {
		if value, err := kvstore.ToByte(kvPair.Value); err == nil {
			if err = json.Unmarshal(value, &tpInst); err != nil {
				logger.Errorw(ctx, "error-unmarshal-kv-pair", log.Fields{"keyPath": keyPath, "value": value})
				return nil, errors.New("error-unmarshal-kv-pair")
			} else {
				logger.Debugw(ctx, "found-valid-tp-instance-on-another-uni", log.Fields{"keyPath": keyPath})
				return &tpInst, nil
			}
		}
	}
	return nil, nil
}

func (t *TechProfileMgr) getDefaultTechProfile(ctx context.Context) *DefaultTechProfile {
	var usGemPortAttributeList []GemPortAttribute
	var dsGemPortAttributeList []GemPortAttribute

	for _, pbit := range t.config.DefaultPbits {
		logger.Debugw(ctx, "Creating GEM port", log.Fields{"pbit": pbit})
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
					MaxProbability: defaultMaxProbability},
				IsMulticast:  defaultIsMulticast,
				DControlList: defaultAccessControlList,
				SControlList: defaultAccessControlList,
				McastGemID:   defaultMcastGemID})
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

// getDefaultTechProfile function for EPON
func (t *TechProfileMgr) getDefaultEponProfile(ctx context.Context) *DefaultEponProfile {

	var usQueueAttributeList []UpstreamQueueAttribute
	var dsQueueAttributeList []DownstreamQueueAttribute

	for _, pbit := range t.config.DefaultPbits {
		logger.Debugw(ctx, "Creating Queue", log.Fields{"pbit": pbit})
		usQueueAttributeList = append(usQueueAttributeList,
			UpstreamQueueAttribute{
				MaxQueueSize:              defaultMaxQueueSize,
				PbitMap:                   pbit,
				AesEncryption:             defaultAESEncryption,
				TrafficType:               defaultTrafficType,
				UnsolicitedGrantSize:      defaultUnsolicitedGrantSize,
				NominalInterval:           defaultNominalInterval,
				ToleratedPollJitter:       defaultToleratedPollJitter,
				RequestTransmissionPolicy: defaultRequestTransmissionPolicy,
				NumQueueSet:               defaultNumQueueSet,
				QThresholds: QThresholds{
					QThreshold1: defaultQThreshold1,
					QThreshold2: defaultQThreshold2,
					QThreshold3: defaultQThreshold3,
					QThreshold4: defaultQThreshold4,
					QThreshold5: defaultQThreshold5,
					QThreshold6: defaultQThreshold6,
					QThreshold7: defaultQThreshold7},
				SchedulingPolicy: SchedulingPolicy_name[defaultSchedulePolicy],
				PriorityQueue:    defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    DiscardPolicy_name[defaultdropPolicy],
				DiscardConfig: DiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability}})
		dsQueueAttributeList = append(dsQueueAttributeList,
			DownstreamQueueAttribute{
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
	return &DefaultEponProfile{
		Name:        t.config.DefaultTPName,
		ProfileType: t.resourceMgr.GetTechnology(),
		Version:     t.config.TPVersion,
		NumGemPorts: uint32(len(usQueueAttributeList)),
		InstanceCtrl: InstanceControl{
			Onu:               defaultOnuInstance,
			Uni:               defaultUniInstance,
			MaxGemPayloadSize: defaultGemPayloadSize},
		EponAttribute: EponAttribute{
			PackageType: defaultPakageType},
		UpstreamQueueAttributeList:   usQueueAttributeList,
		DownstreamQueueAttributeList: dsQueueAttributeList}
}

func (t *TechProfileMgr) GetprotoBufParamValue(ctx context.Context, paramType string, paramKey string) int32 {
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
				logger.Debugw(ctx, "Got value in proto", log.Fields{"key": key, "value": val})
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
		logger.Error(ctx, "Could not find proto parameter", log.Fields{"paramType": paramType, "key": paramKey})
		return -1
	}
	logger.Debugw(ctx, "Got value in proto", log.Fields{"key": paramKey, "value": result})
	return result
}

func (t *TechProfileMgr) GetUsScheduler(ctx context.Context, tpInstance *TechProfile) (*tp_pb.SchedulerConfig, error) {
	dir := tp_pb.Direction(t.GetprotoBufParamValue(ctx, "direction", tpInstance.UsScheduler.Direction))
	if dir == -1 {
		logger.Errorf(ctx, "Error in getting proto id for direction %s for upstream scheduler", tpInstance.UsScheduler.Direction)
		return nil, fmt.Errorf("unable to get proto id for direction %s for upstream scheduler", tpInstance.UsScheduler.Direction)
	}

	bw := tp_pb.AdditionalBW(t.GetprotoBufParamValue(ctx, "additional_bw", tpInstance.UsScheduler.AdditionalBw))
	if bw == -1 {
		logger.Errorf(ctx, "Error in getting proto id for bandwidth %s for upstream scheduler", tpInstance.UsScheduler.AdditionalBw)
		return nil, fmt.Errorf("unable to get proto id for bandwidth %s for upstream scheduler", tpInstance.UsScheduler.AdditionalBw)
	}

	policy := tp_pb.SchedulingPolicy(t.GetprotoBufParamValue(ctx, "sched_policy", tpInstance.UsScheduler.QSchedPolicy))
	if policy == -1 {
		logger.Errorf(ctx, "Error in getting proto id for scheduling policy %s for upstream scheduler", tpInstance.UsScheduler.QSchedPolicy)
		return nil, fmt.Errorf("unable to get proto id for scheduling policy %s for upstream scheduler", tpInstance.UsScheduler.QSchedPolicy)
	}

	return &tp_pb.SchedulerConfig{
		Direction:    dir,
		AdditionalBw: bw,
		Priority:     tpInstance.UsScheduler.Priority,
		Weight:       tpInstance.UsScheduler.Weight,
		SchedPolicy:  policy}, nil
}

func (t *TechProfileMgr) GetDsScheduler(ctx context.Context, tpInstance *TechProfile) (*tp_pb.SchedulerConfig, error) {

	dir := tp_pb.Direction(t.GetprotoBufParamValue(ctx, "direction", tpInstance.DsScheduler.Direction))
	if dir == -1 {
		logger.Errorf(ctx, "Error in getting proto id for direction %s for downstream scheduler", tpInstance.DsScheduler.Direction)
		return nil, fmt.Errorf("unable to get proto id for direction %s for downstream scheduler", tpInstance.DsScheduler.Direction)
	}

	bw := tp_pb.AdditionalBW(t.GetprotoBufParamValue(ctx, "additional_bw", tpInstance.DsScheduler.AdditionalBw))
	if bw == -1 {
		logger.Errorf(ctx, "Error in getting proto id for bandwidth %s for downstream scheduler", tpInstance.DsScheduler.AdditionalBw)
		return nil, fmt.Errorf("unable to get proto id for bandwidth %s for downstream scheduler", tpInstance.DsScheduler.AdditionalBw)
	}

	policy := tp_pb.SchedulingPolicy(t.GetprotoBufParamValue(ctx, "sched_policy", tpInstance.DsScheduler.QSchedPolicy))
	if policy == -1 {
		logger.Errorf(ctx, "Error in getting proto id for scheduling policy %s for downstream scheduler", tpInstance.DsScheduler.QSchedPolicy)
		return nil, fmt.Errorf("unable to get proto id for scheduling policy %s for downstream scheduler", tpInstance.DsScheduler.QSchedPolicy)
	}

	return &tp_pb.SchedulerConfig{
		Direction:    dir,
		AdditionalBw: bw,
		Priority:     tpInstance.DsScheduler.Priority,
		Weight:       tpInstance.DsScheduler.Weight,
		SchedPolicy:  policy}, nil
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

func (tpm *TechProfileMgr) GetTrafficQueues(ctx context.Context, tp *TechProfile, Dir tp_pb.Direction) ([]*tp_pb.TrafficQueue, error) {

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

			schedPolicy := tpm.GetprotoBufParamValue(ctx, "sched_policy", tp.UpstreamGemPortAttributeList[Count].SchedulingPolicy)
			if schedPolicy == -1 {
				logger.Errorf(ctx, "Error in getting Proto Id for scheduling policy %s for Upstream Gem Port %d", tp.UpstreamGemPortAttributeList[Count].SchedulingPolicy, Count)
				return nil, fmt.Errorf("upstream gem port traffic queue creation failed due to unrecognized scheduling policy %s", tp.UpstreamGemPortAttributeList[Count].SchedulingPolicy)
			}

			discardPolicy := tpm.GetprotoBufParamValue(ctx, "discard_policy", tp.UpstreamGemPortAttributeList[Count].DiscardPolicy)
			if discardPolicy == -1 {
				logger.Errorf(ctx, "Error in getting Proto Id for discard policy %s for Upstream Gem Port %d", tp.UpstreamGemPortAttributeList[Count].DiscardPolicy, Count)
				return nil, fmt.Errorf("upstream gem port traffic queue creation failed due to unrecognized discard policy %s", tp.UpstreamGemPortAttributeList[Count].DiscardPolicy)
			}

			GemPorts = append(GemPorts, &tp_pb.TrafficQueue{
				Direction:     tp_pb.Direction(tpm.GetprotoBufParamValue(ctx, "direction", tp.UsScheduler.Direction)),
				GemportId:     tp.UpstreamGemPortAttributeList[Count].GemportID,
				PbitMap:       tp.UpstreamGemPortAttributeList[Count].PbitMap,
				AesEncryption: encryp,
				SchedPolicy:   tp_pb.SchedulingPolicy(schedPolicy),
				Priority:      tp.UpstreamGemPortAttributeList[Count].PriorityQueue,
				Weight:        tp.UpstreamGemPortAttributeList[Count].Weight,
				DiscardPolicy: tp_pb.DiscardPolicy(discardPolicy),
			})
		}
		logger.Debugw(ctx, "Upstream Traffic queue list ", log.Fields{"queuelist": GemPorts})
		return GemPorts, nil
	} else if Dir == tp_pb.Direction_DOWNSTREAM {
		//downstream GEM ports
		NumGemPorts := len(tp.DownstreamGemPortAttributeList)
		GemPorts := make([]*tp_pb.TrafficQueue, 0)
		for Count := 0; Count < NumGemPorts; Count++ {
			if isMulticastGem(tp.DownstreamGemPortAttributeList[Count].IsMulticast) {
				//do not take multicast GEM ports. They are handled separately.
				continue
			}
			if tp.DownstreamGemPortAttributeList[Count].AesEncryption == "True" {
				encryp = true
			} else {
				encryp = false
			}

			schedPolicy := tpm.GetprotoBufParamValue(ctx, "sched_policy", tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy)
			if schedPolicy == -1 {
				logger.Errorf(ctx, "Error in getting Proto Id for scheduling policy %s for Downstream Gem Port %d", tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy, Count)
				return nil, fmt.Errorf("downstream gem port traffic queue creation failed due to unrecognized scheduling policy %s", tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy)
			}

			discardPolicy := tpm.GetprotoBufParamValue(ctx, "discard_policy", tp.DownstreamGemPortAttributeList[Count].DiscardPolicy)
			if discardPolicy == -1 {
				logger.Errorf(ctx, "Error in getting Proto Id for discard policy %s for Downstream Gem Port %d", tp.DownstreamGemPortAttributeList[Count].DiscardPolicy, Count)
				return nil, fmt.Errorf("downstream gem port traffic queue creation failed due to unrecognized discard policy %s", tp.DownstreamGemPortAttributeList[Count].DiscardPolicy)
			}

			GemPorts = append(GemPorts, &tp_pb.TrafficQueue{
				Direction:     tp_pb.Direction(tpm.GetprotoBufParamValue(ctx, "direction", tp.DsScheduler.Direction)),
				GemportId:     tp.DownstreamGemPortAttributeList[Count].GemportID,
				PbitMap:       tp.DownstreamGemPortAttributeList[Count].PbitMap,
				AesEncryption: encryp,
				SchedPolicy:   tp_pb.SchedulingPolicy(schedPolicy),
				Priority:      tp.DownstreamGemPortAttributeList[Count].PriorityQueue,
				Weight:        tp.DownstreamGemPortAttributeList[Count].Weight,
				DiscardPolicy: tp_pb.DiscardPolicy(discardPolicy),
			})
		}
		logger.Debugw(ctx, "Downstream Traffic queue list ", log.Fields{"queuelist": GemPorts})
		return GemPorts, nil
	}

	logger.Errorf(ctx, "Unsupported direction %s used for generating Traffic Queue list", Dir)
	return nil, fmt.Errorf("downstream gem port traffic queue creation failed due to unsupported direction %s", Dir)
}

//isMulticastGem returns true if isMulticast attribute value of a GEM port is true; false otherwise
func isMulticastGem(isMulticastAttrValue string) bool {
	return isMulticastAttrValue != "" &&
		(isMulticastAttrValue == "True" || isMulticastAttrValue == "true" || isMulticastAttrValue == "TRUE")
}

func (tpm *TechProfileMgr) GetMulticastTrafficQueues(ctx context.Context, tp *TechProfile) []*tp_pb.TrafficQueue {
	var encryp bool
	NumGemPorts := len(tp.DownstreamGemPortAttributeList)
	mcastTrafficQueues := make([]*tp_pb.TrafficQueue, 0)
	for Count := 0; Count < NumGemPorts; Count++ {
		if !isMulticastGem(tp.DownstreamGemPortAttributeList[Count].IsMulticast) {
			continue
		}
		if tp.DownstreamGemPortAttributeList[Count].AesEncryption == "True" {
			encryp = true
		} else {
			encryp = false
		}
		mcastTrafficQueues = append(mcastTrafficQueues, &tp_pb.TrafficQueue{
			Direction:     tp_pb.Direction(tpm.GetprotoBufParamValue(ctx, "direction", tp.DsScheduler.Direction)),
			GemportId:     tp.DownstreamGemPortAttributeList[Count].McastGemID,
			PbitMap:       tp.DownstreamGemPortAttributeList[Count].PbitMap,
			AesEncryption: encryp,
			SchedPolicy:   tp_pb.SchedulingPolicy(tpm.GetprotoBufParamValue(ctx, "sched_policy", tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy)),
			Priority:      tp.DownstreamGemPortAttributeList[Count].PriorityQueue,
			Weight:        tp.DownstreamGemPortAttributeList[Count].Weight,
			DiscardPolicy: tp_pb.DiscardPolicy(tpm.GetprotoBufParamValue(ctx, "discard_policy", tp.DownstreamGemPortAttributeList[Count].DiscardPolicy)),
		})
	}
	logger.Debugw(ctx, "Downstream Multicast Traffic queue list ", log.Fields{"queuelist": mcastTrafficQueues})
	return mcastTrafficQueues
}

func (tpm *TechProfileMgr) GetUsTrafficScheduler(ctx context.Context, tp *TechProfile) *tp_pb.TrafficScheduler {
	UsScheduler, _ := tpm.GetUsScheduler(ctx, tp)

	return &tp_pb.TrafficScheduler{Direction: UsScheduler.Direction,
		AllocId:   tp.UsScheduler.AllocID,
		Scheduler: UsScheduler}
}

func (t *TechProfileMgr) GetGemportIDForPbit(ctx context.Context, tp interface{}, dir tp_pb.Direction, pbit uint32) uint32 {
	/*
	  Function to get the Gemport ID mapped to a pbit.
	*/
	switch tp := tp.(type) {
	case *TechProfile:
		if dir == tp_pb.Direction_UPSTREAM {
			// upstream GEM ports
			numGemPorts := len(tp.UpstreamGemPortAttributeList)
			for gemCnt := 0; gemCnt < numGemPorts; gemCnt++ {
				lenOfPbitMap := len(tp.UpstreamGemPortAttributeList[gemCnt].PbitMap)
				for pbitMapIdx := 2; pbitMapIdx < lenOfPbitMap; pbitMapIdx++ {
					// Given a sample pbit map string "0b00000001", lenOfPbitMap is 10
					// "lenOfPbitMap - pbitMapIdx + 1" will give pbit-i th value from LSB position in the pbit map string
					if p, err := strconv.Atoi(string(tp.UpstreamGemPortAttributeList[gemCnt].PbitMap[lenOfPbitMap-pbitMapIdx+1])); err == nil {
						if uint32(pbitMapIdx-2) == pbit && p == 1 { // Check this p-bit is set
							logger.Debugw(ctx, "Found-US-GEMport-for-Pcp", log.Fields{"pbit": pbit, "GEMport": tp.UpstreamGemPortAttributeList[gemCnt].GemportID})
							return tp.UpstreamGemPortAttributeList[gemCnt].GemportID
						}
					}
				}
			}
		} else if dir == tp_pb.Direction_DOWNSTREAM {
			//downstream GEM ports
			numGemPorts := len(tp.DownstreamGemPortAttributeList)
			for gemCnt := 0; gemCnt < numGemPorts; gemCnt++ {
				lenOfPbitMap := len(tp.DownstreamGemPortAttributeList[gemCnt].PbitMap)
				for pbitMapIdx := 2; pbitMapIdx < lenOfPbitMap; pbitMapIdx++ {
					// Given a sample pbit map string "0b00000001", lenOfPbitMap is 10
					// "lenOfPbitMap - pbitMapIdx + 1" will give pbit-i th value from LSB position in the pbit map string
					if p, err := strconv.Atoi(string(tp.DownstreamGemPortAttributeList[gemCnt].PbitMap[lenOfPbitMap-pbitMapIdx+1])); err == nil {
						if uint32(pbitMapIdx-2) == pbit && p == 1 { // Check this p-bit is set
							logger.Debugw(ctx, "Found-DS-GEMport-for-Pcp", log.Fields{"pbit": pbit, "GEMport": tp.DownstreamGemPortAttributeList[gemCnt].GemportID})
							return tp.DownstreamGemPortAttributeList[gemCnt].GemportID
						}
					}
				}
			}
		}
		logger.Errorw(ctx, "No-GemportId-Found-For-Pcp", log.Fields{"pcpVlan": pbit})
	case *EponProfile:
		if dir == tp_pb.Direction_UPSTREAM {
			// upstream GEM ports
			numGemPorts := len(tp.UpstreamQueueAttributeList)
			for gemCnt := 0; gemCnt < numGemPorts; gemCnt++ {
				lenOfPbitMap := len(tp.UpstreamQueueAttributeList[gemCnt].PbitMap)
				for pbitMapIdx := 2; pbitMapIdx < lenOfPbitMap; pbitMapIdx++ {
					// Given a sample pbit map string "0b00000001", lenOfPbitMap is 10
					// "lenOfPbitMap - pbitMapIdx + 1" will give pbit-i th value from LSB position in the pbit map string
					if p, err := strconv.Atoi(string(tp.UpstreamQueueAttributeList[gemCnt].PbitMap[lenOfPbitMap-pbitMapIdx+1])); err == nil {
						if uint32(pbitMapIdx-2) == pbit && p == 1 { // Check this p-bit is set
							logger.Debugw(ctx, "Found-US-Queue-for-Pcp", log.Fields{"pbit": pbit, "Queue": tp.UpstreamQueueAttributeList[gemCnt].GemportID})
							return tp.UpstreamQueueAttributeList[gemCnt].GemportID
						}
					}
				}
			}
		} else if dir == tp_pb.Direction_DOWNSTREAM {
			//downstream GEM ports
			numGemPorts := len(tp.DownstreamQueueAttributeList)
			for gemCnt := 0; gemCnt < numGemPorts; gemCnt++ {
				lenOfPbitMap := len(tp.DownstreamQueueAttributeList[gemCnt].PbitMap)
				for pbitMapIdx := 2; pbitMapIdx < lenOfPbitMap; pbitMapIdx++ {
					// Given a sample pbit map string "0b00000001", lenOfPbitMap is 10
					// "lenOfPbitMap - pbitMapIdx + 1" will give pbit-i th value from LSB position in the pbit map string
					if p, err := strconv.Atoi(string(tp.DownstreamQueueAttributeList[gemCnt].PbitMap[lenOfPbitMap-pbitMapIdx+1])); err == nil {
						if uint32(pbitMapIdx-2) == pbit && p == 1 { // Check this p-bit is set
							logger.Debugw(ctx, "Found-DS-Queue-for-Pcp", log.Fields{"pbit": pbit, "Queue": tp.DownstreamQueueAttributeList[gemCnt].GemportID})
							return tp.DownstreamQueueAttributeList[gemCnt].GemportID
						}
					}
				}
			}
		}
		logger.Errorw(ctx, "No-QueueId-Found-For-Pcp", log.Fields{"pcpVlan": pbit})
	default:
		logger.Errorw(ctx, "unknown-tech", log.Fields{"tp": tp})
	}
	return 0
}

// FindAllTpInstances returns all TechProfile instances for a given TechProfile table-id, pon interface ID and onu ID.
func (t *TechProfileMgr) FindAllTpInstances(ctx context.Context, techProfiletblID uint32, ponIntf uint32, onuID uint32) interface{} {
	var tpTech TechProfile
	var tpEpon EponProfile

	onuTpInstancePath := fmt.Sprintf("%s/%d/pon-{%d}/onu-{%d}", t.resourceMgr.GetTechnology(), techProfiletblID, ponIntf, onuID)

	if kvPairs, _ := t.config.KVBackend.List(ctx, onuTpInstancePath); kvPairs != nil {
		tech := t.resourceMgr.GetTechnology()
		tpInstancesTech := make([]TechProfile, 0, len(kvPairs))
		tpInstancesEpon := make([]EponProfile, 0, len(kvPairs))

		for kvPath, kvPair := range kvPairs {
			if value, err := kvstore.ToByte(kvPair.Value); err == nil {
				if tech == xgspon || tech == gpon {
					if err = json.Unmarshal(value, &tpTech); err != nil {
						logger.Errorw(ctx, "error-unmarshal-kv-pair", log.Fields{"kvPath": kvPath, "value": value})
						continue
					} else {
						tpInstancesTech = append(tpInstancesTech, tpTech)
					}
				} else if tech == epon {
					if err = json.Unmarshal(value, &tpEpon); err != nil {
						logger.Errorw(ctx, "error-unmarshal-kv-pair", log.Fields{"kvPath": kvPath, "value": value})
						continue
					} else {
						tpInstancesEpon = append(tpInstancesEpon, tpEpon)
					}
				}
			}
		}

		switch tech {
		case xgspon, gpon:
			return tpInstancesTech
		case epon:
			return tpInstancesEpon
		default:
			logger.Errorw(ctx, "unknown-technology", log.Fields{"tech": tech})
			return nil
		}
	}
	return nil
}
