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
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	"github.com/opencord/voltha-lib-go/v7/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	tp_pb "github.com/opencord/voltha-protos/v5/go/tech_profile"
)

// Interface to pon resource manager APIs
type iPonResourceMgr interface {
	GetResourceID(ctx context.Context, intfID uint32, resourceType string, numIDs uint32) ([]uint32, error)
	FreeResourceID(ctx context.Context, intfID uint32, resourceType string, ReleaseContent []uint32) error
	GetResourceTypeAllocID() string
	GetResourceTypeGemPortID() string
	GetResourceTypeOnuID() string
	GetTechnology() string
}

type SchedulingPolicy int32

const (
	SchedulingPolicy_WRR            SchedulingPolicy = 0
	SchedulingPolicy_StrictPriority SchedulingPolicy = 1
	SchedulingPolicy_Hybrid         SchedulingPolicy = 2
)

type AdditionalBW int32

const (
	AdditionalBW_AdditionalBW_None       AdditionalBW = 0
	AdditionalBW_AdditionalBW_NA         AdditionalBW = 1
	AdditionalBW_AdditionalBW_BestEffort AdditionalBW = 2
	AdditionalBW_AdditionalBW_Auto       AdditionalBW = 3
)

type DiscardPolicy int32

const (
	DiscardPolicy_TailDrop  DiscardPolicy = 0
	DiscardPolicy_WTailDrop DiscardPolicy = 1
	DiscardPolicy_Red       DiscardPolicy = 2
	DiscardPolicy_WRed      DiscardPolicy = 3
)

// Required uniPortName format
var uniPortNameFormatRegexp = regexp.MustCompile(`^olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}$`)

// instance control defaults
const (
	defaultOnuInstance    = "multi-instance"
	defaultUniInstance    = "single-instance"
	defaultGemPayloadSize = "auto"
)

// default discard config constants
const (
	defaultMinThreshold   = 0
	defaultMaxThreshold   = 0
	defaultMaxProbability = 0
)

// default scheduler contants
const (
	defaultPriority = 0
	defaultWeight   = 0
)

// default GEM attribute constants
const (
	defaultAESEncryption     = "True"
	defaultPriorityQueue     = 0
	defaultQueueWeight       = 0
	defaultMaxQueueSize      = "auto"
	defaultIsMulticast       = "False"
	defaultAccessControlList = "224.0.0.0-239.255.255.255"
	defaultMcastGemID        = 4069
)

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

const (
	xgspon = "XGS-PON"
	xgpon  = "XGPON"
	gpon   = "GPON"
	epon   = "EPON"
)

const (
	MaxUniPortPerOnu = 16 // TODO: Adapter uses its own constant for MaxUniPort. How to synchronize this and have a single source of truth?
)

type TechProfileMgr struct {
	config                *TechProfileFlags
	resourceMgr           iPonResourceMgr
	OnuIDMgmtLock         sync.RWMutex
	GemPortIDMgmtLock     sync.RWMutex
	AllocIDMgmtLock       sync.RWMutex
	tpInstanceMap         map[string]*tp_pb.TechProfileInstance // Map of tp path to tp instance
	tpInstanceMapLock     sync.RWMutex
	eponTpInstanceMap     map[string]*tp_pb.EponTechProfileInstance // Map of tp path to epon tp instance
	epontpInstanceMapLock sync.RWMutex
	tpMap                 map[uint32]*tp_pb.TechProfile // Map of tp id to tp
	tpMapLock             sync.RWMutex
	eponTpMap             map[uint32]*tp_pb.EponTechProfile // map of tp id to epon tp
	eponTpMapLock         sync.RWMutex
}

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
	 kv := model.NewBackend(t.config.kvStoreType, t.config.KVStoreHost, t.config.KVStorePort,
								  t.config.KVStoreTimeout,  kvStoreTechProfilePathPrefix)
	*/
}

func NewTechProfile(ctx context.Context, resourceMgr iPonResourceMgr, kvStoreType string, kvStoreAddress string, basePathKvStore string) (*TechProfileMgr, error) {
	var techprofileObj TechProfileMgr
	logger.Debug(ctx, "initializing-techprofile-mananger")
	techprofileObj.config = NewTechProfileFlags(kvStoreType, kvStoreAddress, basePathKvStore)
	techprofileObj.config.KVBackend = techprofileObj.SetKVClient(ctx, techprofileObj.config.TPKVPathPrefix)
	techprofileObj.config.DefaultTpKVBackend = techprofileObj.SetKVClient(ctx, techprofileObj.config.defaultTpKvPathPrefix)
	if techprofileObj.config.KVBackend == nil {
		logger.Error(ctx, "failed-to-initialize-backend")
		return nil, errors.New("kv-backend-init-failed")
	}
	techprofileObj.config.ResourceInstanceKVBacked = techprofileObj.SetKVClient(ctx, techprofileObj.config.ResourceInstanceKVPathPrefix)
	if techprofileObj.config.ResourceInstanceKVBacked == nil {
		logger.Error(ctx, "failed-to-initialize-resource-instance-kv-backend")
		return nil, errors.New("resource-instance-kv-backend-init-failed")
	}
	techprofileObj.resourceMgr = resourceMgr
	techprofileObj.tpInstanceMap = make(map[string]*tp_pb.TechProfileInstance)
	techprofileObj.eponTpInstanceMap = make(map[string]*tp_pb.EponTechProfileInstance)
	techprofileObj.tpMap = make(map[uint32]*tp_pb.TechProfile)
	techprofileObj.eponTpMap = make(map[uint32]*tp_pb.EponTechProfile)
	logger.Debug(ctx, "reconcile-tp-instance-cache-start")
	if err := techprofileObj.reconcileTpInstancesToCache(ctx); err != nil {
		logger.Errorw(ctx, "failed-to-reconcile-tp-instances", log.Fields{"err": err})
		return nil, err
	}
	logger.Debug(ctx, "reconcile-tp-instance-cache-end")
	logger.Debug(ctx, "initializing-tech-profile-manager-object-success")
	return &techprofileObj, nil
}

// GetTechProfileInstanceKey returns the tp instance key that is used to reference TP Instance Map
func (t *TechProfileMgr) GetTechProfileInstanceKey(ctx context.Context, tpID uint32, uniPortName string) string {
	logger.Debugw(ctx, "get-tp-instance-kv-key", log.Fields{
		"uniPortName": uniPortName,
		"tpId":        tpID,
	})
	// Make sure the uniPortName is as per format olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
	if !uniPortNameFormatRegexp.Match([]byte(uniPortName)) {
		logger.Warnw(ctx, "uni-port-name-not-confirming-to-format", log.Fields{"uniPortName": uniPortName})
	}
	// The key path prefix (like service/voltha/technology_profiles or service/voltha_voltha/technology_profiles)
	// is expected to be attached by the components that use this path as part of the KVBackend configuration.
	resourceInstanceKvPathSuffix := "%s/%d/%s" // <technology>/<tpID>/<uni-port-name>
	// <uni-port-name> must be of the format pon-{\d+}/onu-{\d+}/uni-{\d+}
	return fmt.Sprintf(resourceInstanceKvPathSuffix, t.resourceMgr.GetTechnology(), tpID, uniPortName)
}

// GetTPInstance gets TP instance from cache if found
func (t *TechProfileMgr) GetTPInstance(ctx context.Context, path string) (interface{}, error) {
	tech := t.resourceMgr.GetTechnology()
	switch tech {
	case xgspon, xgpon, gpon:
		t.tpInstanceMapLock.RLock()
		defer t.tpInstanceMapLock.RUnlock()
		tpInst, ok := t.tpInstanceMap[path]
		if !ok {
			return nil, fmt.Errorf("tp-instance-not-found-tp-path-%v", path)
		}
		return tpInst, nil
	case epon:
		t.epontpInstanceMapLock.RLock()
		defer t.epontpInstanceMapLock.RUnlock()
		tpInst, ok := t.eponTpInstanceMap[path]
		if !ok {
			return nil, fmt.Errorf("tp-instance-not-found-tp-path-%v", path)
		}
		return tpInst, nil
	default:
		logger.Errorw(ctx, "unknown-tech", log.Fields{"tech": tech})
		return nil, fmt.Errorf("unknown-tech-%s-tp-path-%v", tech, path)
	}
}

// CreateTechProfileInstance creates a new TP instance.
func (t *TechProfileMgr) CreateTechProfileInstance(ctx context.Context, tpID uint32, uniPortName string, intfID uint32) (interface{}, error) {
	var tpInstance *tp_pb.TechProfileInstance
	var eponTpInstance *tp_pb.EponTechProfileInstance

	logger.Infow(ctx, "creating-tp-instance", log.Fields{"tpID": tpID, "uni": uniPortName, "intId": intfID})

	// Make sure the uniPortName is as per format olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
	if !uniPortNameFormatRegexp.Match([]byte(uniPortName)) {
		logger.Errorw(ctx, "uni-port-name-not-confirming-to-format", log.Fields{"uniPortName": uniPortName})
		return nil, fmt.Errorf("uni-port-name-not-confirming-to-format-%s", uniPortName)
	}
	tpInstancePathSuffix := t.GetTechProfileInstanceKey(ctx, tpID, uniPortName)

	if t.resourceMgr.GetTechnology() == epon {
		tp := t.getEponTPFromKVStore(ctx, tpID)
		if tp != nil {
			if err := t.validateInstanceControlAttr(ctx, *tp.InstanceControl); err != nil {
				logger.Error(ctx, "invalid-instance-ctrl-attr-using-default-tp")
				tp = t.getDefaultEponProfile(ctx)
			} else {
				logger.Infow(ctx, "using-specified-tp-from-kv-store", log.Fields{"tpID": tpID})
			}
		} else {
			logger.Info(ctx, "tp-not-found-on-kv--creating-default-tp")
			tp = t.getDefaultEponProfile(ctx)
		}
		// Store TP in cache
		t.eponTpMapLock.Lock()
		t.eponTpMap[tpID] = tp
		t.eponTpMapLock.Unlock()

		if eponTpInstance = t.allocateEponTPInstance(ctx, uniPortName, tp, intfID, tpInstancePathSuffix); eponTpInstance == nil {
			logger.Error(ctx, "tp-instance-allocation-failed")
			return nil, errors.New("tp-instance-allocation-failed")
		}
		t.epontpInstanceMapLock.Lock()
		t.eponTpInstanceMap[tpInstancePathSuffix] = eponTpInstance
		t.epontpInstanceMapLock.Unlock()
		resInst := tp_pb.ResourceInstance{
			TpId:                 tpID,
			ProfileType:          eponTpInstance.ProfileType,
			SubscriberIdentifier: eponTpInstance.SubscriberIdentifier,
			AllocId:              eponTpInstance.AllocId,
		}
		for _, usQAttr := range eponTpInstance.UpstreamQueueAttributeList {
			resInst.GemportIds = append(resInst.GemportIds, usQAttr.GemportId)
		}

		logger.Infow(ctx, "epon-tp-instance-created-successfully",
			log.Fields{"tpID": tpID, "uni": uniPortName, "intfID": intfID})
		if err := t.addResourceInstanceToKVStore(ctx, tpID, uniPortName, resInst); err != nil {
			logger.Errorw(ctx, "failed-to-update-resource-instance-to-kv-store--freeing-up-resources", log.Fields{"err": err, "tpID": tpID, "uniPortName": uniPortName})
			allocIDs := make([]uint32, 0)
			allocIDs = append(allocIDs, resInst.AllocId)
			errList := make([]error, 0)
			errList = append(errList, t.FreeResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeAllocID(), allocIDs))
			errList = append(errList, t.FreeResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeGemPortID(), resInst.GemportIds))
			if len(errList) > 0 {
				logger.Errorw(ctx, "failed-to-free-up-resources-on-kv-store--system-behavior-has-become-erratic", log.Fields{"tpID": tpID, "uniPortName": uniPortName, "errList": errList})
			}
			return nil, err
		}
		return eponTpInstance, nil
	} else {
		tp := t.getTPFromKVStore(ctx, tpID)
		if tp != nil {
			if err := t.validateInstanceControlAttr(ctx, *tp.InstanceControl); err != nil {
				logger.Error(ctx, "invalid-instance-ctrl-attr--using-default-tp")
				tp = t.getDefaultTechProfile(ctx)
			} else {
				logger.Infow(ctx, "using-specified-tp-from-kv-store", log.Fields{"tpID": tpID})
			}
		} else {
			logger.Info(ctx, "tp-not-found-on-kv--creating-default-tp")
			tp = t.getDefaultTechProfile(ctx)
		}
		// Store TP in cache
		t.tpMapLock.Lock()
		t.tpMap[tpID] = tp
		t.tpMapLock.Unlock()

		if tpInstance = t.allocateTPInstance(ctx, uniPortName, tp, intfID, tpInstancePathSuffix); tpInstance == nil {
			logger.Error(ctx, "tp-instance-allocation-failed")
			return nil, errors.New("tp-instance-allocation-failed")
		}
		t.tpInstanceMapLock.Lock()
		t.tpInstanceMap[tpInstancePathSuffix] = tpInstance
		t.tpInstanceMapLock.Unlock()

		resInst := tp_pb.ResourceInstance{
			TpId:                 tpID,
			ProfileType:          tpInstance.ProfileType,
			SubscriberIdentifier: tpInstance.SubscriberIdentifier,
			AllocId:              tpInstance.UsScheduler.AllocId,
		}
		for _, usQAttr := range tpInstance.UpstreamGemPortAttributeList {
			resInst.GemportIds = append(resInst.GemportIds, usQAttr.GemportId)
		}

		logger.Infow(ctx, "tp-instance-created-successfully",
			log.Fields{"tpID": tpID, "uni": uniPortName, "intfID": intfID})
		if err := t.addResourceInstanceToKVStore(ctx, tpID, uniPortName, resInst); err != nil {
			logger.Errorw(ctx, "failed-to-update-resource-instance-to-kv-store--freeing-up-resources", log.Fields{"err": err, "tpID": tpID, "uniPortName": uniPortName})
			allocIDs := make([]uint32, 0)
			allocIDs = append(allocIDs, resInst.AllocId)
			errList := make([]error, 0)
			errList = append(errList, t.FreeResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeAllocID(), allocIDs))
			errList = append(errList, t.FreeResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeGemPortID(), resInst.GemportIds))
			if len(errList) > 0 {
				logger.Fatalw(ctx, "failed-to-free-up-resources-on-kv-store--system-behavior-has-become-erratic", log.Fields{"err": err, "tpID": tpID, "uniPortName": uniPortName})
			}
			return nil, err
		}

		logger.Infow(ctx, "resource-instance-added-to-kv-store-successfully",
			log.Fields{"tpID": tpID, "uni": uniPortName, "intfID": intfID})
		return tpInstance, nil
	}
}

// DeleteTechProfileInstance deletes the TP instance from the local cache as well as deletes the corresponding
// resource instance from the KV store.
func (t *TechProfileMgr) DeleteTechProfileInstance(ctx context.Context, tpID uint32, uniPortName string) error {
	// Make sure the uniPortName is as per format olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
	if !uniPortNameFormatRegexp.Match([]byte(uniPortName)) {
		logger.Errorw(ctx, "uni-port-name-not-confirming-to-format", log.Fields{"uniPortName": uniPortName})
		return fmt.Errorf("uni-port-name-not-confirming-to-format--%s", uniPortName)
	}
	path := t.GetTechProfileInstanceKey(ctx, tpID, uniPortName)
	logger.Infow(ctx, "delete-tp-instance-from-cache", log.Fields{"key": path})
	t.tpInstanceMapLock.Lock()
	delete(t.tpInstanceMap, path)
	t.tpInstanceMapLock.Unlock()
	if err := t.removeResourceInstanceFromKVStore(ctx, tpID, uniPortName); err != nil {
		return err
	}
	return nil
}

func (t *TechProfileMgr) GetMulticastTrafficQueues(ctx context.Context, tp *tp_pb.TechProfileInstance) []*tp_pb.TrafficQueue {
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
			Direction:     tp_pb.Direction_DOWNSTREAM,
			GemportId:     tp.DownstreamGemPortAttributeList[Count].MulticastGemId,
			PbitMap:       tp.DownstreamGemPortAttributeList[Count].PbitMap,
			AesEncryption: encryp,
			SchedPolicy:   tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy,
			Priority:      tp.DownstreamGemPortAttributeList[Count].PriorityQ,
			Weight:        tp.DownstreamGemPortAttributeList[Count].Weight,
			DiscardPolicy: tp.DownstreamGemPortAttributeList[Count].DiscardPolicy,
		})
	}
	logger.Debugw(ctx, "Downstream Multicast Traffic queue list ", log.Fields{"queuelist": mcastTrafficQueues})
	return mcastTrafficQueues
}

func (t *TechProfileMgr) GetGemportForPbit(ctx context.Context, tp interface{}, dir tp_pb.Direction, pbit uint32) interface{} {
	/*
	  Function to get the Gemport mapped to a pbit.
	*/
	switch tp := tp.(type) {
	case *tp_pb.TechProfileInstance:
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
							logger.Debugw(ctx, "Found-US-GEMport-for-Pcp", log.Fields{"pbit": pbit, "GEMport": tp.UpstreamGemPortAttributeList[gemCnt].GemportId})
							return tp.UpstreamGemPortAttributeList[gemCnt]
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
							logger.Debugw(ctx, "Found-DS-GEMport-for-Pcp", log.Fields{"pbit": pbit, "GEMport": tp.DownstreamGemPortAttributeList[gemCnt].GemportId})
							return tp.DownstreamGemPortAttributeList[gemCnt]
						}
					}
				}
			}
		}
		logger.Errorw(ctx, "No-GemportId-Found-For-Pcp", log.Fields{"pcpVlan": pbit})
	case *tp_pb.EponTechProfileInstance:
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
							logger.Debugw(ctx, "Found-US-Queue-for-Pcp", log.Fields{"pbit": pbit, "Queue": tp.UpstreamQueueAttributeList[gemCnt].GemportId})
							return tp.UpstreamQueueAttributeList[gemCnt]
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
							logger.Debugw(ctx, "Found-DS-Queue-for-Pcp", log.Fields{"pbit": pbit, "Queue": tp.DownstreamQueueAttributeList[gemCnt].GemportId})
							return tp.DownstreamQueueAttributeList[gemCnt]
						}
					}
				}
			}
		}
		logger.Errorw(ctx, "No-QueueId-Found-For-Pcp", log.Fields{"pcpVlan": pbit})
	default:
		logger.Errorw(ctx, "unknown-tech", log.Fields{"tp": tp})
	}
	return nil
}

// FindAllTpInstances returns all TechProfile instances for a given TechProfile table-id, pon interface ID and onu ID.
func (t *TechProfileMgr) FindAllTpInstances(ctx context.Context, oltDeviceID string, tpID uint32, intfID uint32, onuID uint32) interface{} {
	onuTpInstancePathSuffix := fmt.Sprintf("%s/%d/olt-{%s}/pon-{%d}/onu-{%d}", t.resourceMgr.GetTechnology(), tpID, oltDeviceID, intfID, onuID)
	tech := t.resourceMgr.GetTechnology()
	if tech == xgspon || tech == xgpon || tech == gpon {
		t.tpInstanceMapLock.RLock()
		defer t.tpInstanceMapLock.RUnlock()
		tpInstancesTech := make([]tp_pb.TechProfileInstance, 0)
		for i := 0; i < MaxUniPortPerOnu; i++ {
			key := onuTpInstancePathSuffix + fmt.Sprintf("/uni-{%d}", i)
			if tpInst, ok := t.tpInstanceMap[key]; ok {
				tpInstancesTech = append(tpInstancesTech, *tpInst)
			}
		}
		return tpInstancesTech
	} else if tech == epon {
		t.epontpInstanceMapLock.RLock()
		defer t.epontpInstanceMapLock.RUnlock()
		tpInstancesTech := make([]tp_pb.EponTechProfileInstance, 0)
		for i := 0; i < MaxUniPortPerOnu; i++ {
			key := onuTpInstancePathSuffix + fmt.Sprintf("/uni-{%d}", i)
			if tpInst, ok := t.eponTpInstanceMap[key]; ok {
				tpInstancesTech = append(tpInstancesTech, *tpInst)
			}
		}
		return tpInstancesTech
	} else {
		logger.Errorw(ctx, "unknown-tech", log.Fields{"tech": tech, "tpID": tpID, "onuID": onuID, "intfID": intfID})
	}
	return nil
}

func (t *TechProfileMgr) GetResourceID(ctx context.Context, intfID uint32, resourceType string, numIDs uint32) ([]uint32, error) {
	logger.Debugw(ctx, "getting-resource-id", log.Fields{
		"intf-id":       intfID,
		"resource-type": resourceType,
		"num":           numIDs,
	})
	var err error
	var ids []uint32
	switch resourceType {
	case t.resourceMgr.GetResourceTypeAllocID():
		t.AllocIDMgmtLock.Lock()
		ids, err = t.resourceMgr.GetResourceID(ctx, intfID, resourceType, numIDs)
		t.AllocIDMgmtLock.Unlock()
	case t.resourceMgr.GetResourceTypeGemPortID():
		t.GemPortIDMgmtLock.Lock()
		ids, err = t.resourceMgr.GetResourceID(ctx, intfID, resourceType, numIDs)
		t.GemPortIDMgmtLock.Unlock()
	case t.resourceMgr.GetResourceTypeOnuID():
		t.OnuIDMgmtLock.Lock()
		ids, err = t.resourceMgr.GetResourceID(ctx, intfID, resourceType, numIDs)
		t.OnuIDMgmtLock.Unlock()
	default:
		return nil, fmt.Errorf("resourceType %s not supported", resourceType)
	}
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (t *TechProfileMgr) FreeResourceID(ctx context.Context, intfID uint32, resourceType string, ReleaseContent []uint32) error {
	logger.Debugw(ctx, "freeing-resource-id", log.Fields{
		"intf-id":         intfID,
		"resource-type":   resourceType,
		"release-content": ReleaseContent,
	})
	var err error
	switch resourceType {
	case t.resourceMgr.GetResourceTypeAllocID():
		t.AllocIDMgmtLock.Lock()
		err = t.resourceMgr.FreeResourceID(ctx, intfID, resourceType, ReleaseContent)
		t.AllocIDMgmtLock.Unlock()
	case t.resourceMgr.GetResourceTypeGemPortID():
		t.GemPortIDMgmtLock.Lock()
		err = t.resourceMgr.FreeResourceID(ctx, intfID, resourceType, ReleaseContent)
		t.GemPortIDMgmtLock.Unlock()
	case t.resourceMgr.GetResourceTypeOnuID():
		t.OnuIDMgmtLock.Lock()
		err = t.resourceMgr.FreeResourceID(ctx, intfID, resourceType, ReleaseContent)
		t.OnuIDMgmtLock.Unlock()
	default:
		return fmt.Errorf("resourceType %s not supported", resourceType)
	}
	if err != nil {
		return err
	}
	return nil
}

func (t *TechProfileMgr) GetUsScheduler(tpInstance *tp_pb.TechProfileInstance) *tp_pb.SchedulerConfig {
	return &tp_pb.SchedulerConfig{
		Direction:    tpInstance.UsScheduler.Direction,
		AdditionalBw: tpInstance.UsScheduler.AdditionalBw,
		Priority:     tpInstance.UsScheduler.Priority,
		Weight:       tpInstance.UsScheduler.Weight,
		SchedPolicy:  tpInstance.UsScheduler.QSchedPolicy}
}

func (t *TechProfileMgr) GetDsScheduler(tpInstance *tp_pb.TechProfileInstance) *tp_pb.SchedulerConfig {
	return &tp_pb.SchedulerConfig{
		Direction:    tpInstance.DsScheduler.Direction,
		AdditionalBw: tpInstance.DsScheduler.AdditionalBw,
		Priority:     tpInstance.DsScheduler.Priority,
		Weight:       tpInstance.DsScheduler.Weight,
		SchedPolicy:  tpInstance.DsScheduler.QSchedPolicy}
}

func (t *TechProfileMgr) GetTrafficScheduler(tpInstance *tp_pb.TechProfileInstance, SchedCfg *tp_pb.SchedulerConfig,
	ShapingCfg *tp_pb.TrafficShapingInfo) *tp_pb.TrafficScheduler {

	tSched := &tp_pb.TrafficScheduler{
		Direction:          SchedCfg.Direction,
		AllocId:            tpInstance.UsScheduler.AllocId,
		TrafficShapingInfo: ShapingCfg,
		Scheduler:          SchedCfg}

	return tSched
}

func (t *TechProfileMgr) GetTrafficQueues(ctx context.Context, tp *tp_pb.TechProfileInstance, direction tp_pb.Direction) ([]*tp_pb.TrafficQueue, error) {

	var encryp bool
	if direction == tp_pb.Direction_UPSTREAM {
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
				Direction:     direction,
				GemportId:     tp.UpstreamGemPortAttributeList[Count].GemportId,
				PbitMap:       tp.UpstreamGemPortAttributeList[Count].PbitMap,
				AesEncryption: encryp,
				SchedPolicy:   tp.UpstreamGemPortAttributeList[Count].SchedulingPolicy,
				Priority:      tp.UpstreamGemPortAttributeList[Count].PriorityQ,
				Weight:        tp.UpstreamGemPortAttributeList[Count].Weight,
				DiscardPolicy: tp.UpstreamGemPortAttributeList[Count].DiscardPolicy,
			})
		}
		logger.Debugw(ctx, "Upstream Traffic queue list ", log.Fields{"queuelist": GemPorts})
		return GemPorts, nil
	} else if direction == tp_pb.Direction_DOWNSTREAM {
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

			GemPorts = append(GemPorts, &tp_pb.TrafficQueue{
				Direction:     direction,
				GemportId:     tp.DownstreamGemPortAttributeList[Count].GemportId,
				PbitMap:       tp.DownstreamGemPortAttributeList[Count].PbitMap,
				AesEncryption: encryp,
				SchedPolicy:   tp.DownstreamGemPortAttributeList[Count].SchedulingPolicy,
				Priority:      tp.DownstreamGemPortAttributeList[Count].PriorityQ,
				Weight:        tp.DownstreamGemPortAttributeList[Count].Weight,
				DiscardPolicy: tp.DownstreamGemPortAttributeList[Count].DiscardPolicy,
			})
		}
		logger.Debugw(ctx, "Downstream Traffic queue list ", log.Fields{"queuelist": GemPorts})
		return GemPorts, nil
	}

	logger.Errorf(ctx, "Unsupported direction %s used for generating Traffic Queue list", direction)
	return nil, fmt.Errorf("downstream gem port traffic queue creation failed due to unsupported direction %s", direction)
}

func (t *TechProfileMgr) validateInstanceControlAttr(ctx context.Context, instCtl tp_pb.InstanceControl) error {
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

// allocateTPInstance for GPON, XGPON and XGS-PON technology
func (t *TechProfileMgr) allocateTPInstance(ctx context.Context, uniPortName string, tp *tp_pb.TechProfile, intfID uint32, tpInstPathSuffix string) *tp_pb.TechProfileInstance {

	var usGemPortAttributeList []*tp_pb.GemPortAttributes
	var dsGemPortAttributeList []*tp_pb.GemPortAttributes
	var dsMulticastGemAttributeList []*tp_pb.GemPortAttributes
	var dsUnicastGemAttributeList []*tp_pb.GemPortAttributes
	var tcontIDs []uint32
	var gemPorts []uint32
	var err error

	logger.Infow(ctx, "Allocating TechProfileMgr instance from techprofile template", log.Fields{"uniPortName": uniPortName, "intfID": intfID, "numGem": tp.NumGemPorts})

	if tp.InstanceControl.Onu == "multi-instance" {
		tcontIDs, err = t.GetResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeAllocID(), 1)
		if err != nil {
			logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"err": err, "intfID": intfID})
			return nil
		}
	} else { // "single-instance"
		if tpInst := t.getSingleInstanceTp(ctx, tpInstPathSuffix); tpInst == nil {
			// No "single-instance" tp found on one any uni port for the given TP ID
			// Allocate a new TcontID or AllocID
			tcontIDs, err = t.GetResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeAllocID(), 1)
			if err != nil {
				logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"err": err, "intfID": intfID})
				return nil
			}
		} else {
			// Use the alloc-id from the existing TpInstance
			tcontIDs = append(tcontIDs, tpInst.UsScheduler.AllocId)
		}
	}
	logger.Debugw(ctx, "Num GEM ports in TP:", log.Fields{"NumGemPorts": tp.NumGemPorts})
	gemPorts, err = t.GetResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeGemPortID(), tp.NumGemPorts)
	if err != nil {
		logger.Errorw(ctx, "Error getting gemport ids from rsrcrMgr", log.Fields{"err": err, "intfID": intfID, "numGemports": tp.NumGemPorts})
		return nil
	}
	logger.Infow(ctx, "Allocated tconts and GEM ports successfully", log.Fields{"tconts": tcontIDs, "gemports": gemPorts})
	for index := 0; index < int(tp.NumGemPorts); index++ {
		usGemPortAttributeList = append(usGemPortAttributeList,
			&tp_pb.GemPortAttributes{GemportId: gemPorts[index],
				MaxQSize:         tp.UpstreamGemPortAttributeList[index].MaxQSize,
				PbitMap:          tp.UpstreamGemPortAttributeList[index].PbitMap,
				AesEncryption:    tp.UpstreamGemPortAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.UpstreamGemPortAttributeList[index].SchedulingPolicy,
				PriorityQ:        tp.UpstreamGemPortAttributeList[index].PriorityQ,
				Weight:           tp.UpstreamGemPortAttributeList[index].Weight,
				DiscardPolicy:    tp.UpstreamGemPortAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.UpstreamGemPortAttributeList[index].DiscardConfig})
	}

	logger.Info(ctx, "length of DownstreamGemPortAttributeList", len(tp.DownstreamGemPortAttributeList))
	//put multicast and unicast downstream GEM port attributes in different lists first
	for index := 0; index < len(tp.DownstreamGemPortAttributeList); index++ {
		if isMulticastGem(tp.DownstreamGemPortAttributeList[index].IsMulticast) {
			dsMulticastGemAttributeList = append(dsMulticastGemAttributeList,
				&tp_pb.GemPortAttributes{
					MulticastGemId:           tp.DownstreamGemPortAttributeList[index].MulticastGemId,
					MaxQSize:                 tp.DownstreamGemPortAttributeList[index].MaxQSize,
					PbitMap:                  tp.DownstreamGemPortAttributeList[index].PbitMap,
					AesEncryption:            tp.DownstreamGemPortAttributeList[index].AesEncryption,
					SchedulingPolicy:         tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
					PriorityQ:                tp.DownstreamGemPortAttributeList[index].PriorityQ,
					Weight:                   tp.DownstreamGemPortAttributeList[index].Weight,
					DiscardPolicy:            tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
					DiscardConfig:            tp.DownstreamGemPortAttributeList[index].DiscardConfig,
					IsMulticast:              tp.DownstreamGemPortAttributeList[index].IsMulticast,
					DynamicAccessControlList: tp.DownstreamGemPortAttributeList[index].DynamicAccessControlList,
					StaticAccessControlList:  tp.DownstreamGemPortAttributeList[index].StaticAccessControlList})
		} else {
			dsUnicastGemAttributeList = append(dsUnicastGemAttributeList,
				&tp_pb.GemPortAttributes{
					MaxQSize:         tp.DownstreamGemPortAttributeList[index].MaxQSize,
					PbitMap:          tp.DownstreamGemPortAttributeList[index].PbitMap,
					AesEncryption:    tp.DownstreamGemPortAttributeList[index].AesEncryption,
					SchedulingPolicy: tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
					PriorityQ:        tp.DownstreamGemPortAttributeList[index].PriorityQ,
					Weight:           tp.DownstreamGemPortAttributeList[index].Weight,
					DiscardPolicy:    tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
					DiscardConfig:    tp.DownstreamGemPortAttributeList[index].DiscardConfig})
		}
	}
	//add unicast downstream GEM ports to dsGemPortAttributeList
	if dsUnicastGemAttributeList != nil {
		for index := 0; index < int(tp.NumGemPorts); index++ {
			dsGemPortAttributeList = append(dsGemPortAttributeList,
				&tp_pb.GemPortAttributes{GemportId: gemPorts[index],
					MaxQSize:         dsUnicastGemAttributeList[index].MaxQSize,
					PbitMap:          dsUnicastGemAttributeList[index].PbitMap,
					AesEncryption:    dsUnicastGemAttributeList[index].AesEncryption,
					SchedulingPolicy: dsUnicastGemAttributeList[index].SchedulingPolicy,
					PriorityQ:        dsUnicastGemAttributeList[index].PriorityQ,
					Weight:           dsUnicastGemAttributeList[index].Weight,
					DiscardPolicy:    dsUnicastGemAttributeList[index].DiscardPolicy,
					DiscardConfig:    dsUnicastGemAttributeList[index].DiscardConfig})
		}
	}
	//add multicast GEM ports to dsGemPortAttributeList afterwards
	for k := range dsMulticastGemAttributeList {
		dsGemPortAttributeList = append(dsGemPortAttributeList, dsMulticastGemAttributeList[k])
	}

	return &tp_pb.TechProfileInstance{
		SubscriberIdentifier: uniPortName,
		Name:                 tp.Name,
		ProfileType:          tp.ProfileType,
		Version:              tp.Version,
		NumGemPorts:          tp.NumGemPorts,
		InstanceControl:      tp.InstanceControl,
		UsScheduler: &tp_pb.SchedulerAttributes{
			AllocId:      tcontIDs[0],
			Direction:    tp.UsScheduler.Direction,
			AdditionalBw: tp.UsScheduler.AdditionalBw,
			Priority:     tp.UsScheduler.Priority,
			Weight:       tp.UsScheduler.Weight,
			QSchedPolicy: tp.UsScheduler.QSchedPolicy},
		DsScheduler: &tp_pb.SchedulerAttributes{
			AllocId:      tcontIDs[0],
			Direction:    tp.DsScheduler.Direction,
			AdditionalBw: tp.DsScheduler.AdditionalBw,
			Priority:     tp.DsScheduler.Priority,
			Weight:       tp.DsScheduler.Weight,
			QSchedPolicy: tp.DsScheduler.QSchedPolicy},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

// allocateTPInstance function for EPON
func (t *TechProfileMgr) allocateEponTPInstance(ctx context.Context, uniPortName string, tp *tp_pb.EponTechProfile, intfID uint32, tpInstPath string) *tp_pb.EponTechProfileInstance {

	var usQueueAttributeList []*tp_pb.EPONQueueAttributes
	var dsQueueAttributeList []*tp_pb.EPONQueueAttributes
	var tcontIDs []uint32
	var gemPorts []uint32
	var err error

	logger.Infow(ctx, "allocating-tp-instance-from-tp-template", log.Fields{"uniPortName": uniPortName, "intfID": intfID, "numGem": tp.NumGemPorts})

	if tp.InstanceControl.Onu == "multi-instance" {
		if tcontIDs, err = t.GetResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeAllocID(), 1); err != nil {
			logger.Errorw(ctx, "Error getting alloc id from rsrcrMgr", log.Fields{"err": err, "intfID": intfID})
			return nil
		}
	} else { // "single-instance"
		if tpInst := t.getSingleInstanceEponTp(ctx, tpInstPath); tpInst == nil {
			// No "single-instance" tp found on one any uni port for the given TP ID
			// Allocate a new TcontID or AllocID
			if tcontIDs, err = t.GetResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeAllocID(), 1); err != nil {
				logger.Errorw(ctx, "error-getting-alloc-id-from-resource-mgr", log.Fields{"err": err, "intfID": intfID})
				return nil
			}
		} else {
			// Use the alloc-id from the existing TpInstance
			tcontIDs = append(tcontIDs, tpInst.AllocId)
		}
	}
	logger.Debugw(ctx, "Num GEM ports in TP:", log.Fields{"NumGemPorts": tp.NumGemPorts})
	if gemPorts, err = t.GetResourceID(ctx, intfID, t.resourceMgr.GetResourceTypeGemPortID(), tp.NumGemPorts); err != nil {
		logger.Errorw(ctx, "error-getting-gemport-id-from-resource-mgr", log.Fields{"err": err, "intfID": intfID, "numGemports": tp.NumGemPorts})
		return nil
	}
	logger.Infow(ctx, "allocated-alloc-id-and-gemport-successfully", log.Fields{"tconts": tcontIDs, "gemports": gemPorts})
	for index := 0; index < int(tp.NumGemPorts); index++ {
		usQueueAttributeList = append(usQueueAttributeList,
			&tp_pb.EPONQueueAttributes{GemportId: gemPorts[index],
				MaxQSize:                  tp.UpstreamQueueAttributeList[index].MaxQSize,
				PbitMap:                   tp.UpstreamQueueAttributeList[index].PbitMap,
				AesEncryption:             tp.UpstreamQueueAttributeList[index].AesEncryption,
				TrafficType:               tp.UpstreamQueueAttributeList[index].TrafficType,
				UnsolicitedGrantSize:      tp.UpstreamQueueAttributeList[index].UnsolicitedGrantSize,
				NominalInterval:           tp.UpstreamQueueAttributeList[index].NominalInterval,
				ToleratedPollJitter:       tp.UpstreamQueueAttributeList[index].ToleratedPollJitter,
				RequestTransmissionPolicy: tp.UpstreamQueueAttributeList[index].RequestTransmissionPolicy,
				NumQSets:                  tp.UpstreamQueueAttributeList[index].NumQSets,
				QThresholds:               tp.UpstreamQueueAttributeList[index].QThresholds,
				SchedulingPolicy:          tp.UpstreamQueueAttributeList[index].SchedulingPolicy,
				PriorityQ:                 tp.UpstreamQueueAttributeList[index].PriorityQ,
				Weight:                    tp.UpstreamQueueAttributeList[index].Weight,
				DiscardPolicy:             tp.UpstreamQueueAttributeList[index].DiscardPolicy,
				DiscardConfig:             tp.UpstreamQueueAttributeList[index].DiscardConfig})
	}

	logger.Info(ctx, "length-of-downstream-gemport-attribute-list", len(tp.DownstreamQueueAttributeList))
	for index := 0; index < int(tp.NumGemPorts); index++ {
		dsQueueAttributeList = append(dsQueueAttributeList,
			&tp_pb.EPONQueueAttributes{GemportId: gemPorts[index],
				MaxQSize:         tp.DownstreamQueueAttributeList[index].MaxQSize,
				PbitMap:          tp.DownstreamQueueAttributeList[index].PbitMap,
				AesEncryption:    tp.DownstreamQueueAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.DownstreamQueueAttributeList[index].SchedulingPolicy,
				PriorityQ:        tp.DownstreamQueueAttributeList[index].PriorityQ,
				Weight:           tp.DownstreamQueueAttributeList[index].Weight,
				DiscardPolicy:    tp.DownstreamQueueAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.DownstreamQueueAttributeList[index].DiscardConfig})
	}

	return &tp_pb.EponTechProfileInstance{
		SubscriberIdentifier:         uniPortName,
		Name:                         tp.Name,
		ProfileType:                  tp.ProfileType,
		Version:                      tp.Version,
		NumGemPorts:                  tp.NumGemPorts,
		InstanceControl:              tp.InstanceControl,
		PackageType:                  tp.PackageType,
		AllocId:                      tcontIDs[0],
		UpstreamQueueAttributeList:   usQueueAttributeList,
		DownstreamQueueAttributeList: dsQueueAttributeList}
}

// getSingleInstanceTp returns another TpInstance (GPON, XGPON, XGS-PON) for an ONU on a different
// uni port for the same TP ID, if it finds one, else nil.
func (t *TechProfileMgr) getSingleInstanceTp(ctx context.Context, tpPathSuffix string) *tp_pb.TechProfileInstance {

	// For example:
	// tpPathSuffix like "XGS-PON/64/olt-{1234}/pon-{0}/onu-{1}/uni-{1}"
	// is broken into ["XGS-PON/64/olt-{1234}/pon-{0}/onu-{1}" ""]
	uniPathSlice := regexp.MustCompile(`/uni-{[0-9]+}$`).Split(tpPathSuffix, 2)

	t.tpInstanceMapLock.RLock()
	defer t.tpInstanceMapLock.RUnlock()
	for i := 0; i < MaxUniPortPerOnu; i++ {
		key := fmt.Sprintf(uniPathSlice[0]+"/uni-{%d}", i)
		if tpInst, ok := t.tpInstanceMap[key]; ok {
			logger.Debugw(ctx, "found-single-instance-tp", log.Fields{"key": key})
			return tpInst
		}
	}
	return nil
}

// getSingleInstanceTp returns another TpInstance (EPON) for an ONU on a different
// uni port for the same TP ID, if it finds one, else nil.
func (t *TechProfileMgr) getSingleInstanceEponTp(ctx context.Context, tpPathSuffix string) *tp_pb.EponTechProfileInstance {
	// For example:
	// tpPathSuffix like "EPON/64/olt-{1234}/pon-{0}/onu-{1}/uni-{1}"
	// is broken into ["EPON/64/-{1234}/pon-{0}/onu-{1}" ""]
	uniPathSlice := regexp.MustCompile(`/uni-{[0-9]+}$`).Split(tpPathSuffix, 2)

	t.epontpInstanceMapLock.RLock()
	defer t.epontpInstanceMapLock.RUnlock()
	for i := 0; i < MaxUniPortPerOnu; i++ {
		key := fmt.Sprintf(uniPathSlice[0]+"/uni-{%d}", i)
		if tpInst, ok := t.eponTpInstanceMap[key]; ok {
			logger.Debugw(ctx, "found-single-instance-tp", log.Fields{"key": key})
			return tpInst
		}
	}
	return nil
}

// getDefaultTechProfile returns a default TechProfile for GPON, XGPON, XGS-PON
func (t *TechProfileMgr) getDefaultTechProfile(ctx context.Context) *tp_pb.TechProfile {
	var usGemPortAttributeList []*tp_pb.GemPortAttributes
	var dsGemPortAttributeList []*tp_pb.GemPortAttributes

	for _, pbit := range t.config.DefaultPbits {
		logger.Debugw(ctx, "creating-gem-port-profile-profile", log.Fields{"pbit": pbit})
		usGemPortAttributeList = append(usGemPortAttributeList,
			&tp_pb.GemPortAttributes{
				MaxQSize:         defaultMaxQueueSize,
				PbitMap:          pbit,
				AesEncryption:    defaultAESEncryption,
				SchedulingPolicy: tp_pb.SchedulingPolicy_WRR,
				PriorityQ:        defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    tp_pb.DiscardPolicy_TailDrop,
				DiscardConfigV2: &tp_pb.DiscardConfig{
					DiscardPolicy: tp_pb.DiscardPolicy_Red,
					DiscardConfig: &tp_pb.DiscardConfig_RedDiscardConfig{
						RedDiscardConfig: &tp_pb.RedDiscardConfig{
							MinThreshold:   defaultMinThreshold,
							MaxThreshold:   defaultMaxThreshold,
							MaxProbability: defaultMaxProbability,
						},
					},
				},
				DiscardConfig: &tp_pb.RedDiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability,
				},
			})
		dsGemPortAttributeList = append(dsGemPortAttributeList,
			&tp_pb.GemPortAttributes{
				MaxQSize:         defaultMaxQueueSize,
				PbitMap:          pbit,
				AesEncryption:    defaultAESEncryption,
				SchedulingPolicy: tp_pb.SchedulingPolicy_WRR,
				PriorityQ:        defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    tp_pb.DiscardPolicy_TailDrop,
				DiscardConfigV2: &tp_pb.DiscardConfig{
					DiscardPolicy: tp_pb.DiscardPolicy_Red,
					DiscardConfig: &tp_pb.DiscardConfig_RedDiscardConfig{
						RedDiscardConfig: &tp_pb.RedDiscardConfig{
							MinThreshold:   defaultMinThreshold,
							MaxThreshold:   defaultMaxThreshold,
							MaxProbability: defaultMaxProbability,
						},
					},
				},
				DiscardConfig: &tp_pb.RedDiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability,
				},
				IsMulticast:              defaultIsMulticast,
				DynamicAccessControlList: defaultAccessControlList,
				StaticAccessControlList:  defaultAccessControlList,
				MulticastGemId:           defaultMcastGemID})
	}
	return &tp_pb.TechProfile{
		Name:        t.config.DefaultTPName,
		ProfileType: t.resourceMgr.GetTechnology(),
		Version:     t.config.TPVersion,
		NumGemPorts: uint32(len(usGemPortAttributeList)),
		InstanceControl: &tp_pb.InstanceControl{
			Onu:               defaultOnuInstance,
			Uni:               defaultUniInstance,
			MaxGemPayloadSize: defaultGemPayloadSize},
		UsScheduler: &tp_pb.SchedulerAttributes{
			Direction:    tp_pb.Direction_UPSTREAM,
			AdditionalBw: tp_pb.AdditionalBW_AdditionalBW_BestEffort,
			Priority:     defaultPriority,
			Weight:       defaultWeight,
			QSchedPolicy: tp_pb.SchedulingPolicy_Hybrid},
		DsScheduler: &tp_pb.SchedulerAttributes{
			Direction:    tp_pb.Direction_DOWNSTREAM,
			AdditionalBw: tp_pb.AdditionalBW_AdditionalBW_BestEffort,
			Priority:     defaultPriority,
			Weight:       defaultWeight,
			QSchedPolicy: tp_pb.SchedulingPolicy_Hybrid},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

// getDefaultEponProfile returns a default TechProfile for EPON
func (t *TechProfileMgr) getDefaultEponProfile(ctx context.Context) *tp_pb.EponTechProfile {

	var usQueueAttributeList []*tp_pb.EPONQueueAttributes
	var dsQueueAttributeList []*tp_pb.EPONQueueAttributes

	for _, pbit := range t.config.DefaultPbits {
		logger.Debugw(ctx, "Creating Queue", log.Fields{"pbit": pbit})
		usQueueAttributeList = append(usQueueAttributeList,
			&tp_pb.EPONQueueAttributes{
				MaxQSize:                  defaultMaxQueueSize,
				PbitMap:                   pbit,
				AesEncryption:             defaultAESEncryption,
				TrafficType:               defaultTrafficType,
				UnsolicitedGrantSize:      defaultUnsolicitedGrantSize,
				NominalInterval:           defaultNominalInterval,
				ToleratedPollJitter:       defaultToleratedPollJitter,
				RequestTransmissionPolicy: defaultRequestTransmissionPolicy,
				NumQSets:                  defaultNumQueueSet,
				QThresholds: &tp_pb.QThresholds{
					QThreshold1: defaultQThreshold1,
					QThreshold2: defaultQThreshold2,
					QThreshold3: defaultQThreshold3,
					QThreshold4: defaultQThreshold4,
					QThreshold5: defaultQThreshold5,
					QThreshold6: defaultQThreshold6,
					QThreshold7: defaultQThreshold7},
				SchedulingPolicy: tp_pb.SchedulingPolicy_WRR,
				PriorityQ:        defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    tp_pb.DiscardPolicy_TailDrop,
				DiscardConfigV2: &tp_pb.DiscardConfig{
					DiscardPolicy: tp_pb.DiscardPolicy_Red,
					DiscardConfig: &tp_pb.DiscardConfig_RedDiscardConfig{
						RedDiscardConfig: &tp_pb.RedDiscardConfig{
							MinThreshold:   defaultMinThreshold,
							MaxThreshold:   defaultMaxThreshold,
							MaxProbability: defaultMaxProbability,
						},
					},
				},
				DiscardConfig: &tp_pb.RedDiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability,
				}})
		dsQueueAttributeList = append(dsQueueAttributeList,
			&tp_pb.EPONQueueAttributes{
				MaxQSize:         defaultMaxQueueSize,
				PbitMap:          pbit,
				AesEncryption:    defaultAESEncryption,
				SchedulingPolicy: tp_pb.SchedulingPolicy_WRR,
				PriorityQ:        defaultPriorityQueue,
				Weight:           defaultQueueWeight,
				DiscardPolicy:    tp_pb.DiscardPolicy_TailDrop,
				DiscardConfigV2: &tp_pb.DiscardConfig{
					DiscardPolicy: tp_pb.DiscardPolicy_Red,
					DiscardConfig: &tp_pb.DiscardConfig_RedDiscardConfig{
						RedDiscardConfig: &tp_pb.RedDiscardConfig{
							MinThreshold:   defaultMinThreshold,
							MaxThreshold:   defaultMaxThreshold,
							MaxProbability: defaultMaxProbability,
						},
					},
				},
				DiscardConfig: &tp_pb.RedDiscardConfig{
					MinThreshold:   defaultMinThreshold,
					MaxThreshold:   defaultMaxThreshold,
					MaxProbability: defaultMaxProbability,
				}})
	}
	return &tp_pb.EponTechProfile{
		Name:        t.config.DefaultTPName,
		ProfileType: t.resourceMgr.GetTechnology(),
		Version:     t.config.TPVersion,
		NumGemPorts: uint32(len(usQueueAttributeList)),
		InstanceControl: &tp_pb.InstanceControl{
			Onu:               defaultOnuInstance,
			Uni:               defaultUniInstance,
			MaxGemPayloadSize: defaultGemPayloadSize},
		PackageType:                  defaultPakageType,
		UpstreamQueueAttributeList:   usQueueAttributeList,
		DownstreamQueueAttributeList: dsQueueAttributeList}
}

//isMulticastGem returns true if isMulticast attribute value of a GEM port is true; false otherwise
func isMulticastGem(isMulticastAttrValue string) bool {
	return isMulticastAttrValue != "" &&
		(isMulticastAttrValue == "True" || isMulticastAttrValue == "true" || isMulticastAttrValue == "TRUE")
}

func (t *TechProfileMgr) addResourceInstanceToKVStore(ctx context.Context, tpID uint32, uniPortName string, resInst tp_pb.ResourceInstance) error {
	logger.Debugw(ctx, "adding-resource-instance-to-kv-store", log.Fields{"tpID": tpID, "uniPortName": uniPortName, "resInst": resInst})
	val, err := proto.Marshal(&resInst)
	if err != nil {
		logger.Errorw(ctx, "failed-to-marshall-resource-instance", log.Fields{"err": err, "tpID": tpID, "uniPortName": uniPortName, "resInst": resInst})
		return err
	}
	err = t.config.ResourceInstanceKVBacked.Put(ctx, fmt.Sprintf("%s/%d/%s", t.resourceMgr.GetTechnology(), tpID, uniPortName), val)
	return err
}

func (t *TechProfileMgr) removeResourceInstanceFromKVStore(ctx context.Context, tpID uint32, uniPortName string) error {
	logger.Debugw(ctx, "removing-resource-instance-to-kv-store", log.Fields{"tpID": tpID, "uniPortName": uniPortName})
	if err := t.config.ResourceInstanceKVBacked.Delete(ctx, fmt.Sprintf("%s/%d/%s", t.resourceMgr.GetTechnology(), tpID, uniPortName)); err != nil {
		logger.Errorw(ctx, "error-removing-resource-instance-to-kv-store", log.Fields{"err": err, "tpID": tpID, "uniPortName": uniPortName})
		return err
	}
	return nil
}

func (t *TechProfileMgr) getTPFromKVStore(ctx context.Context, tpID uint32) *tp_pb.TechProfile {
	var tp *tp_pb.TechProfile
	t.tpMapLock.RLock()
	tp, ok := t.tpMap[tpID]
	t.tpMapLock.RUnlock()
	if ok {
		logger.Debugw(ctx, "found-tp-in-cache", log.Fields{"tpID": tpID})
		return tp
	}
	key := fmt.Sprintf(t.config.TPFileKVPath, t.resourceMgr.GetTechnology(), tpID)
	logger.Debugw(ctx, "getting-tp-from-kv-store", log.Fields{"tpID": tpID, "Key": key})
	kvresult, err := t.config.DefaultTpKVBackend.Get(ctx, key)
	if err != nil {
		logger.Errorw(ctx, "error-fetching-from-kv-store", log.Fields{"err": err, "key": key})
		return nil
	}
	if kvresult != nil {
		/* Backend will return Value in string format,needs to be converted to []byte before unmarshal*/
		if value, err := kvstore.ToByte(kvresult.Value); err == nil {
			lTp := &tp_pb.TechProfile{}
			reader := bytes.NewReader(value)
			if err = jsonpb.Unmarshal(reader, lTp); err != nil {
				logger.Errorw(ctx, "error-unmarshalling-tp-from-kv-store", log.Fields{"err": err, "tpID": tpID, "error": err})
				return nil
			}

			logger.Debugw(ctx, "success-fetched-tp-from-kv-store", log.Fields{"tpID": tpID, "value": *lTp})
			return lTp
		} else {
			logger.Errorw(ctx, "error-decoding-tp", log.Fields{"err": err, "tpID": tpID})
			// We we create a default profile in this case.
		}
	}

	return nil
}

func (t *TechProfileMgr) getEponTPFromKVStore(ctx context.Context, tpID uint32) *tp_pb.EponTechProfile {
	var eponTp *tp_pb.EponTechProfile
	t.eponTpMapLock.RLock()
	eponTp, ok := t.eponTpMap[tpID]
	t.eponTpMapLock.RUnlock()
	if ok {
		logger.Debugw(ctx, "found-tp-in-cache", log.Fields{"tpID": tpID})
		return eponTp
	}
	key := fmt.Sprintf(t.config.TPFileKVPath, t.resourceMgr.GetTechnology(), tpID)
	logger.Debugw(ctx, "getting-epon-tp-from-kv-store", log.Fields{"tpID": tpID, "Key": key})
	kvresult, err := t.config.DefaultTpKVBackend.Get(ctx, key)
	if err != nil {
		logger.Errorw(ctx, "error-fetching-from-kv-store", log.Fields{"err": err, "key": key})
		return nil
	}
	if kvresult != nil {
		/* Backend will return Value in string format,needs to be converted to []byte before unmarshal*/
		if value, err := kvstore.ToByte(kvresult.Value); err == nil {
			lEponTp := &tp_pb.EponTechProfile{}
			reader := bytes.NewReader(value)
			if err = jsonpb.Unmarshal(reader, lEponTp); err != nil {
				logger.Errorw(ctx, "error-unmarshalling-epon-tp-from-kv-store", log.Fields{"err": err, "tpID": tpID, "error": err})
				return nil
			}

			logger.Debugw(ctx, "success-fetching-epon-tp-from-kv-store", log.Fields{"tpID": tpID, "value": *lEponTp})
			return lEponTp
		}
	}
	return nil
}

func newKVClient(ctx context.Context, storeType string, address string, timeout time.Duration) (kvstore.Client, error) {

	logger.Infow(ctx, "kv-store", log.Fields{"storeType": storeType, "address": address})
	switch storeType {
	case "etcd":
		return kvstore.NewEtcdClient(ctx, address, timeout, log.WarnLevel)
	}
	return nil, errors.New("unsupported-kv-store")
}

// buildTpInstanceFromResourceInstance for GPON, XGPON and XGS-PON technology - build TpInstance from TechProfile template and ResourceInstance
func (t *TechProfileMgr) buildTpInstanceFromResourceInstance(ctx context.Context, tp *tp_pb.TechProfile, resInst *tp_pb.ResourceInstance) *tp_pb.TechProfileInstance {

	var usGemPortAttributeList []*tp_pb.GemPortAttributes
	var dsGemPortAttributeList []*tp_pb.GemPortAttributes
	var dsMulticastGemAttributeList []*tp_pb.GemPortAttributes
	var dsUnicastGemAttributeList []*tp_pb.GemPortAttributes

	if len(resInst.GemportIds) != int(tp.NumGemPorts) {
		logger.Errorw(ctx, "mismatch-in-number-of-gemports-between-template-and-resource-instance",
			log.Fields{"tpID": resInst.TpId, "totalResInstGemPortIDs": len(resInst.GemportIds), "totalTpTemplateGemPorts": tp.NumGemPorts})
		return nil
	}
	for index := 0; index < int(tp.NumGemPorts); index++ {
		usGemPortAttributeList = append(usGemPortAttributeList,
			&tp_pb.GemPortAttributes{GemportId: resInst.GemportIds[index],
				MaxQSize:         tp.UpstreamGemPortAttributeList[index].MaxQSize,
				PbitMap:          tp.UpstreamGemPortAttributeList[index].PbitMap,
				AesEncryption:    tp.UpstreamGemPortAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.UpstreamGemPortAttributeList[index].SchedulingPolicy,
				PriorityQ:        tp.UpstreamGemPortAttributeList[index].PriorityQ,
				Weight:           tp.UpstreamGemPortAttributeList[index].Weight,
				DiscardPolicy:    tp.UpstreamGemPortAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.UpstreamGemPortAttributeList[index].DiscardConfig})
	}

	//put multicast and unicast downstream GEM port attributes in different lists first
	for index := 0; index < len(tp.DownstreamGemPortAttributeList); index++ {
		if isMulticastGem(tp.DownstreamGemPortAttributeList[index].IsMulticast) {
			dsMulticastGemAttributeList = append(dsMulticastGemAttributeList,
				&tp_pb.GemPortAttributes{
					MulticastGemId:           tp.DownstreamGemPortAttributeList[index].MulticastGemId,
					MaxQSize:                 tp.DownstreamGemPortAttributeList[index].MaxQSize,
					PbitMap:                  tp.DownstreamGemPortAttributeList[index].PbitMap,
					AesEncryption:            tp.DownstreamGemPortAttributeList[index].AesEncryption,
					SchedulingPolicy:         tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
					PriorityQ:                tp.DownstreamGemPortAttributeList[index].PriorityQ,
					Weight:                   tp.DownstreamGemPortAttributeList[index].Weight,
					DiscardPolicy:            tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
					DiscardConfig:            tp.DownstreamGemPortAttributeList[index].DiscardConfig,
					IsMulticast:              tp.DownstreamGemPortAttributeList[index].IsMulticast,
					DynamicAccessControlList: tp.DownstreamGemPortAttributeList[index].DynamicAccessControlList,
					StaticAccessControlList:  tp.DownstreamGemPortAttributeList[index].StaticAccessControlList})
		} else {
			dsUnicastGemAttributeList = append(dsUnicastGemAttributeList,
				&tp_pb.GemPortAttributes{
					MaxQSize:         tp.DownstreamGemPortAttributeList[index].MaxQSize,
					PbitMap:          tp.DownstreamGemPortAttributeList[index].PbitMap,
					AesEncryption:    tp.DownstreamGemPortAttributeList[index].AesEncryption,
					SchedulingPolicy: tp.DownstreamGemPortAttributeList[index].SchedulingPolicy,
					PriorityQ:        tp.DownstreamGemPortAttributeList[index].PriorityQ,
					Weight:           tp.DownstreamGemPortAttributeList[index].Weight,
					DiscardPolicy:    tp.DownstreamGemPortAttributeList[index].DiscardPolicy,
					DiscardConfig:    tp.DownstreamGemPortAttributeList[index].DiscardConfig})
		}
	}
	//add unicast downstream GEM ports to dsGemPortAttributeList
	if dsUnicastGemAttributeList != nil {
		for index := 0; index < int(tp.NumGemPorts); index++ {
			dsGemPortAttributeList = append(dsGemPortAttributeList,
				&tp_pb.GemPortAttributes{GemportId: resInst.GemportIds[index],
					MaxQSize:         dsUnicastGemAttributeList[index].MaxQSize,
					PbitMap:          dsUnicastGemAttributeList[index].PbitMap,
					AesEncryption:    dsUnicastGemAttributeList[index].AesEncryption,
					SchedulingPolicy: dsUnicastGemAttributeList[index].SchedulingPolicy,
					PriorityQ:        dsUnicastGemAttributeList[index].PriorityQ,
					Weight:           dsUnicastGemAttributeList[index].Weight,
					DiscardPolicy:    dsUnicastGemAttributeList[index].DiscardPolicy,
					DiscardConfig:    dsUnicastGemAttributeList[index].DiscardConfig})
		}
	}
	//add multicast GEM ports to dsGemPortAttributeList afterwards
	for k := range dsMulticastGemAttributeList {
		dsGemPortAttributeList = append(dsGemPortAttributeList, dsMulticastGemAttributeList[k])
	}

	return &tp_pb.TechProfileInstance{
		SubscriberIdentifier: resInst.SubscriberIdentifier,
		Name:                 tp.Name,
		ProfileType:          tp.ProfileType,
		Version:              tp.Version,
		NumGemPorts:          tp.NumGemPorts,
		InstanceControl:      tp.InstanceControl,
		UsScheduler: &tp_pb.SchedulerAttributes{
			AllocId:      resInst.AllocId,
			Direction:    tp.UsScheduler.Direction,
			AdditionalBw: tp.UsScheduler.AdditionalBw,
			Priority:     tp.UsScheduler.Priority,
			Weight:       tp.UsScheduler.Weight,
			QSchedPolicy: tp.UsScheduler.QSchedPolicy},
		DsScheduler: &tp_pb.SchedulerAttributes{
			AllocId:      resInst.AllocId,
			Direction:    tp.DsScheduler.Direction,
			AdditionalBw: tp.DsScheduler.AdditionalBw,
			Priority:     tp.DsScheduler.Priority,
			Weight:       tp.DsScheduler.Weight,
			QSchedPolicy: tp.DsScheduler.QSchedPolicy},
		UpstreamGemPortAttributeList:   usGemPortAttributeList,
		DownstreamGemPortAttributeList: dsGemPortAttributeList}
}

// buildEponTpInstanceFromResourceInstance for EPON technology - build EponTpInstance from EponTechProfile template and ResourceInstance
func (t *TechProfileMgr) buildEponTpInstanceFromResourceInstance(ctx context.Context, tp *tp_pb.EponTechProfile, resInst *tp_pb.ResourceInstance) *tp_pb.EponTechProfileInstance {

	var usQueueAttributeList []*tp_pb.EPONQueueAttributes
	var dsQueueAttributeList []*tp_pb.EPONQueueAttributes

	if len(resInst.GemportIds) != int(tp.NumGemPorts) {
		logger.Errorw(ctx, "mismatch-in-number-of-gemports-between-epon-tp-template-and-resource-instance",
			log.Fields{"tpID": resInst.TpId, "totalResInstGemPortIDs": len(resInst.GemportIds), "totalTpTemplateGemPorts": tp.NumGemPorts})
		return nil
	}

	for index := 0; index < int(tp.NumGemPorts); index++ {
		usQueueAttributeList = append(usQueueAttributeList,
			&tp_pb.EPONQueueAttributes{GemportId: resInst.GemportIds[index],
				MaxQSize:                  tp.UpstreamQueueAttributeList[index].MaxQSize,
				PbitMap:                   tp.UpstreamQueueAttributeList[index].PbitMap,
				AesEncryption:             tp.UpstreamQueueAttributeList[index].AesEncryption,
				TrafficType:               tp.UpstreamQueueAttributeList[index].TrafficType,
				UnsolicitedGrantSize:      tp.UpstreamQueueAttributeList[index].UnsolicitedGrantSize,
				NominalInterval:           tp.UpstreamQueueAttributeList[index].NominalInterval,
				ToleratedPollJitter:       tp.UpstreamQueueAttributeList[index].ToleratedPollJitter,
				RequestTransmissionPolicy: tp.UpstreamQueueAttributeList[index].RequestTransmissionPolicy,
				NumQSets:                  tp.UpstreamQueueAttributeList[index].NumQSets,
				QThresholds:               tp.UpstreamQueueAttributeList[index].QThresholds,
				SchedulingPolicy:          tp.UpstreamQueueAttributeList[index].SchedulingPolicy,
				PriorityQ:                 tp.UpstreamQueueAttributeList[index].PriorityQ,
				Weight:                    tp.UpstreamQueueAttributeList[index].Weight,
				DiscardPolicy:             tp.UpstreamQueueAttributeList[index].DiscardPolicy,
				DiscardConfig:             tp.UpstreamQueueAttributeList[index].DiscardConfig})
	}

	for index := 0; index < int(tp.NumGemPorts); index++ {
		dsQueueAttributeList = append(dsQueueAttributeList,
			&tp_pb.EPONQueueAttributes{GemportId: resInst.GemportIds[index],
				MaxQSize:         tp.DownstreamQueueAttributeList[index].MaxQSize,
				PbitMap:          tp.DownstreamQueueAttributeList[index].PbitMap,
				AesEncryption:    tp.DownstreamQueueAttributeList[index].AesEncryption,
				SchedulingPolicy: tp.DownstreamQueueAttributeList[index].SchedulingPolicy,
				PriorityQ:        tp.DownstreamQueueAttributeList[index].PriorityQ,
				Weight:           tp.DownstreamQueueAttributeList[index].Weight,
				DiscardPolicy:    tp.DownstreamQueueAttributeList[index].DiscardPolicy,
				DiscardConfig:    tp.DownstreamQueueAttributeList[index].DiscardConfig})
	}

	return &tp_pb.EponTechProfileInstance{
		SubscriberIdentifier:         resInst.SubscriberIdentifier,
		Name:                         tp.Name,
		ProfileType:                  tp.ProfileType,
		Version:                      tp.Version,
		NumGemPorts:                  tp.NumGemPorts,
		InstanceControl:              tp.InstanceControl,
		PackageType:                  tp.PackageType,
		AllocId:                      resInst.AllocId,
		UpstreamQueueAttributeList:   usQueueAttributeList,
		DownstreamQueueAttributeList: dsQueueAttributeList}
}

func (t *TechProfileMgr) getTpInstanceFromResourceInstance(ctx context.Context, resInst *tp_pb.ResourceInstance) *tp_pb.TechProfileInstance {
	if resInst == nil {
		logger.Error(ctx, "resource-instance-nil")
		return nil
	}
	tp := t.getTPFromKVStore(ctx, resInst.TpId)
	if tp == nil {
		logger.Warnw(ctx, "tp-not-found-on-kv--creating-default-tp", log.Fields{"tpID": resInst.TpId})
		tp = t.getDefaultTechProfile(ctx)
	}
	return t.buildTpInstanceFromResourceInstance(ctx, tp, resInst)
}

func (t *TechProfileMgr) getEponTpInstanceFromResourceInstance(ctx context.Context, resInst *tp_pb.ResourceInstance) *tp_pb.EponTechProfileInstance {
	if resInst == nil {
		logger.Error(ctx, "resource-instance-nil")
		return nil
	}
	eponTp := t.getEponTPFromKVStore(ctx, resInst.TpId)
	if eponTp == nil {
		logger.Warnw(ctx, "tp-not-found-on-kv--creating-default-tp", log.Fields{"tpID": resInst.TpId})
		eponTp = t.getDefaultEponProfile(ctx)
	}
	return t.buildEponTpInstanceFromResourceInstance(ctx, eponTp, resInst)
}

func (t *TechProfileMgr) reconcileTpInstancesToCache(ctx context.Context) error {

	tech := t.resourceMgr.GetTechnology()
	newCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	kvPairs, _ := t.config.ResourceInstanceKVBacked.List(newCtx, tech)

	if tech == xgspon || tech == xgpon || tech == gpon {
		for keyPath, kvPair := range kvPairs {
			logger.Debugw(ctx, "attempting-to-reconcile-tp-instance-from-resource-instance", log.Fields{"resourceInstPath": keyPath})
			if value, err := kvstore.ToByte(kvPair.Value); err == nil {
				var resInst tp_pb.ResourceInstance
				if err = proto.Unmarshal(value, &resInst); err != nil {
					logger.Errorw(ctx, "error-unmarshal-kv-pair", log.Fields{"err": err, "keyPath": keyPath, "value": value})
					continue
				} else {
					if tpInst := t.getTpInstanceFromResourceInstance(ctx, &resInst); tpInst != nil {
						// Trim the kv path by removing the default prefix part and get only the suffix part to reference the internal cache
						keySuffixSlice := regexp.MustCompile(t.config.ResourceInstanceKVPathPrefix+"/").Split(keyPath, 2)
						if len(keySuffixSlice) == 2 {
							keySuffixFormatRegexp := regexp.MustCompile(`^[a-zA-Z\-]+/[0-9]+/olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}$`)
							// Make sure the keySuffixSlice is as per format [a-zA-Z-+]/[\d+]/olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
							if !keySuffixFormatRegexp.Match([]byte(keySuffixSlice[1])) {
								logger.Errorw(ctx, "kv-path-not-confirming-to-format", log.Fields{"kvPath": keySuffixSlice[1]})
								continue
							}
						} else {
							logger.Errorw(ctx, "kv-instance-key-path-not-in-the-expected-format", log.Fields{"kvPath": keyPath})
							continue
						}
						t.tpInstanceMapLock.Lock()
						t.tpInstanceMap[keySuffixSlice[1]] = tpInst
						t.tpInstanceMapLock.Unlock()
						logger.Debugw(ctx, "reconciled-tp-success", log.Fields{"keyPath": keyPath})
					}
				}
			} else {
				logger.Errorw(ctx, "error-converting-kv-pair-value-to-byte", log.Fields{"err": err})
			}
		}
	} else if tech == epon {
		for keyPath, kvPair := range kvPairs {
			logger.Debugw(ctx, "attempting-to-reconcile-epon-tp-instance", log.Fields{"keyPath": keyPath})
			if value, err := kvstore.ToByte(kvPair.Value); err == nil {
				var resInst tp_pb.ResourceInstance
				if err = proto.Unmarshal(value, &resInst); err != nil {
					logger.Errorw(ctx, "error-unmarshal-kv-pair", log.Fields{"keyPath": keyPath, "value": value})
					continue
				} else {
					if eponTpInst := t.getEponTpInstanceFromResourceInstance(ctx, &resInst); eponTpInst != nil {
						// Trim the kv path by removing the default prefix part and get only the suffix part to reference the internal cache
						keySuffixSlice := regexp.MustCompile(t.config.ResourceInstanceKVPathPrefix+"/").Split(keyPath, 2)
						if len(keySuffixSlice) == 2 {
							keySuffixFormatRegexp := regexp.MustCompile(`^[a-zA-Z\-]+/[0-9]+/olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}$`)
							// Make sure the keySuffixSlice is as per format [a-zA-Z-+]/[\d+]/olt-{[a-z0-9\-]+}/pon-{[0-9]+}/onu-{[0-9]+}/uni-{[0-9]+}
							if !keySuffixFormatRegexp.Match([]byte(keySuffixSlice[1])) {
								logger.Errorw(ctx, "kv-path-not-confirming-to-format", log.Fields{"kvPath": keySuffixSlice[1]})
								continue
							}
						} else {
							logger.Errorw(ctx, "kv-instance-key-path-not-in-the-expected-format", log.Fields{"kvPath": keyPath})
							continue
						}
						t.epontpInstanceMapLock.Lock()
						t.eponTpInstanceMap[keySuffixSlice[1]] = eponTpInst
						t.epontpInstanceMapLock.Unlock()
						logger.Debugw(ctx, "reconciled-epon-tp-success", log.Fields{"keyPath": keyPath})
					}
				}
			} else {
				logger.Errorw(ctx, "error-converting-kv-pair-value-to-byte", log.Fields{"err": err})
			}
		}
	} else {
		logger.Errorw(ctx, "unknown-tech", log.Fields{"tech": tech})
		return fmt.Errorf("unknown-tech-%v", tech)
	}

	return nil
}
