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

package ponresourcemanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bitmap "github.com/boljen/go-bitmap"
	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	"github.com/opencord/voltha-lib-go/v7/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
)

const (
	//Constants to identify resource pool
	UNI_ID     = "UNI_ID"
	ONU_ID     = "ONU_ID"
	ALLOC_ID   = "ALLOC_ID"
	GEMPORT_ID = "GEMPORT_ID"
	FLOW_ID    = "FLOW_ID"

	//Constants for passing command line arguments
	OLT_MODEL_ARG = "--olt_model"

	PATH_PREFIX = "%s/resource_manager/{%s}"

	/*The path under which configuration data is stored is defined as technology/device agnostic.
	  That means the path does not include any specific technology/device variable. Using technology/device
	  agnostic path also makes northbound applications, that need to write to this path,
	  technology/device agnostic.

	  Default kv client of PonResourceManager reads from/writes to PATH_PREFIX defined above.
	  That is why, an additional kv client (named KVStoreForConfig) is defined to read from the config path.
	*/

	PATH_PREFIX_FOR_CONFIG = "%s/resource_manager/config"
	/*The resource ranges for a given device model should be placed
	  at 'resource_manager/<technology>/resource_ranges/<olt_model_type>'
	  path on the KV store.
	  If Resource Range parameters are to be read from the external KV store,
	  they are expected to be stored in the following format.
	  Note: All parameters are MANDATORY for now.
	  constants used as keys to reference the resource range parameters from
	  and external KV store.
	*/
	UNI_ID_START_IDX      = "uni_id_start"
	UNI_ID_END_IDX        = "uni_id_end"
	ONU_ID_START_IDX      = "onu_id_start"
	ONU_ID_END_IDX        = "onu_id_end"
	ONU_ID_SHARED_IDX     = "onu_id_shared"
	ALLOC_ID_START_IDX    = "alloc_id_start"
	ALLOC_ID_END_IDX      = "alloc_id_end"
	ALLOC_ID_SHARED_IDX   = "alloc_id_shared"
	GEMPORT_ID_START_IDX  = "gemport_id_start"
	GEMPORT_ID_END_IDX    = "gemport_id_end"
	GEMPORT_ID_SHARED_IDX = "gemport_id_shared"
	FLOW_ID_START_IDX     = "flow_id_start"
	FLOW_ID_END_IDX       = "flow_id_end"
	FLOW_ID_SHARED_IDX    = "flow_id_shared"
	NUM_OF_PON_PORT       = "pon_ports"

	/*
	   The KV store backend is initialized with a path prefix and we need to
	   provide only the suffix.
	*/
	PON_RESOURCE_RANGE_CONFIG_PATH = "resource_ranges/%s"

	//resource path suffix
	//Path on the KV store for storing alloc id ranges and resource pool for a given interface
	//Format: <device_id>/alloc_id_pool/<pon_intf_id>
	ALLOC_ID_POOL_PATH = "{%s}/alloc_id_pool/{%d}"
	//Path on the KV store for storing gemport id ranges and resource pool for a given interface
	//Format: <device_id>/gemport_id_pool/<pon_intf_id>
	GEMPORT_ID_POOL_PATH = "{%s}/gemport_id_pool/{%d}"
	//Path on the KV store for storing onu id ranges and resource pool for a given interface
	//Format: <device_id>/onu_id_pool/<pon_intf_id>
	ONU_ID_POOL_PATH = "{%s}/onu_id_pool/{%d}"
	//Path on the KV store for storing flow id ranges and resource pool for a given interface
	//Format: <device_id>/flow_id_pool/<pon_intf_id>
	FLOW_ID_POOL_PATH = "{%s}/flow_id_pool/{%d}"

	//Path on the KV store for storing list of alloc IDs for a given ONU
	//Format: <device_id>/<(pon_intf_id, onu_id)>/alloc_ids
	ALLOC_ID_RESOURCE_MAP_PATH = "{%s}/{%s}/alloc_ids"

	//Path on the KV store for storing list of gemport IDs for a given ONU
	//Format: <device_id>/<(pon_intf_id, onu_id)>/gemport_ids
	GEMPORT_ID_RESOURCE_MAP_PATH = "{%s}/{%s}/gemport_ids"

	//Path on the KV store for storing list of Flow IDs for a given ONU
	//Format: <device_id>/<(pon_intf_id, onu_id)>/flow_ids
	FLOW_ID_RESOURCE_MAP_PATH = "{%s}/{%s}/flow_ids"

	//Flow Id info: Use to store more metadata associated with the flow_id
	FLOW_ID_INFO_PATH_PREFIX = "{%s}/flow_id_info"
	//Format: <device_id>/flow_id_info/<(pon_intf_id, onu_id)>
	FLOW_ID_INFO_PATH_INTF_ONU_PREFIX = "{%s}/flow_id_info/{%s}"
	//Format: <device_id>/flow_id_info/<(pon_intf_id, onu_id)><flow_id>
	FLOW_ID_INFO_PATH = FLOW_ID_INFO_PATH_PREFIX + "/{%s}/{%d}"

	//Constants for internal usage.
	PON_INTF_ID     = "pon_intf_id"
	START_IDX       = "start_idx"
	END_IDX         = "end_idx"
	POOL            = "pool"
	NUM_OF_PON_INTF = 16

	KVSTORE_RETRY_TIMEOUT = 5 * time.Second
	//Path on the KV store for storing reserved gem ports
	//Format: reserved_gemport_ids
	RESERVED_GEMPORT_IDS_PATH = "reserved_gemport_ids"
)

//type ResourceTypeIndex string
//type ResourceType string

type PONResourceManager struct {
	//Implements APIs to initialize/allocate/release alloc/gemport/onu IDs.
	Technology       string
	DeviceType       string
	DeviceID         string
	Backend          string // ETCD only currently
	Address          string // address of the KV store
	OLTModel         string
	KVStore          *db.Backend
	KVStoreForConfig *db.Backend

	// Below attribute, pon_resource_ranges, should be initialized
	// by reading from KV store.
	PonResourceRanges  map[string]interface{}
	SharedResourceMgrs map[string]*PONResourceManager
	SharedIdxByType    map[string]string
	IntfIDs            []uint32 // list of pon interface IDs
	Globalorlocal      string
}

func newKVClient(ctx context.Context, storeType string, address string, timeout time.Duration) (kvstore.Client, error) {
	logger.Infow(ctx, "kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "etcd":
		return kvstore.NewEtcdClient(ctx, address, timeout, log.WarnLevel)
	}
	return nil, errors.New("unsupported-kv-store")
}

func SetKVClient(ctx context.Context, Technology string, Backend string, Addr string, configClient bool, basePathKvStore string) *db.Backend {
	// TODO : Make sure direct call to NewBackend is working fine with backend , currently there is some
	// issue between kv store and backend , core is not calling NewBackend directly
	kvClient, err := newKVClient(ctx, Backend, Addr, KVSTORE_RETRY_TIMEOUT)
	if err != nil {
		logger.Fatalw(ctx, "Failed to init KV client\n", log.Fields{"err": err})
		return nil
	}

	var pathPrefix string
	if configClient {
		pathPrefix = fmt.Sprintf(PATH_PREFIX_FOR_CONFIG, basePathKvStore)
	} else {
		pathPrefix = fmt.Sprintf(PATH_PREFIX, basePathKvStore, Technology)
	}

	kvbackend := &db.Backend{
		Client:     kvClient,
		StoreType:  Backend,
		Address:    Addr,
		Timeout:    KVSTORE_RETRY_TIMEOUT,
		PathPrefix: pathPrefix}

	return kvbackend
}

// NewPONResourceManager creates a new PON resource manager.
func NewPONResourceManager(ctx context.Context, Technology string, DeviceType string, DeviceID string, Backend string, Address string, basePathKvStore string) (*PONResourceManager, error) {
	var PONMgr PONResourceManager
	PONMgr.Technology = Technology
	PONMgr.DeviceType = DeviceType
	PONMgr.DeviceID = DeviceID
	PONMgr.Backend = Backend
	PONMgr.Address = Address
	PONMgr.KVStore = SetKVClient(ctx, Technology, Backend, Address, false, basePathKvStore)
	if PONMgr.KVStore == nil {
		logger.Error(ctx, "KV Client initilization failed")
		return nil, errors.New("Failed to init KV client")
	}
	// init kv client to read from the config path
	PONMgr.KVStoreForConfig = SetKVClient(ctx, Technology, Backend, Address, true, basePathKvStore)
	if PONMgr.KVStoreForConfig == nil {
		logger.Error(ctx, "KV Config Client initilization failed")
		return nil, errors.New("Failed to init KV Config client")
	}

	PONMgr.PonResourceRanges = make(map[string]interface{})
	PONMgr.SharedResourceMgrs = make(map[string]*PONResourceManager)
	PONMgr.SharedIdxByType = make(map[string]string)
	PONMgr.SharedIdxByType[ONU_ID] = ONU_ID_SHARED_IDX
	PONMgr.SharedIdxByType[ALLOC_ID] = ALLOC_ID_SHARED_IDX
	PONMgr.SharedIdxByType[GEMPORT_ID] = GEMPORT_ID_SHARED_IDX
	PONMgr.SharedIdxByType[FLOW_ID] = FLOW_ID_SHARED_IDX
	PONMgr.IntfIDs = make([]uint32, NUM_OF_PON_INTF)
	PONMgr.OLTModel = DeviceType
	return &PONMgr, nil
}

/*
  Initialize PON resource ranges with config fetched from kv store.
  return boolean: True if PON resource ranges initialized else false
  Try to initialize the PON Resource Ranges from KV store based on the
  OLT model key, if available
*/

func (PONRMgr *PONResourceManager) InitResourceRangesFromKVStore(ctx context.Context) bool {
	//Initialize PON resource ranges with config fetched from kv store.
	//:return boolean: True if PON resource ranges initialized else false
	// Try to initialize the PON Resource Ranges from KV store based on the
	// OLT model key, if available
	if PONRMgr.OLTModel == "" {
		logger.Error(ctx, "Failed to get OLT model")
		return false
	}
	Path := fmt.Sprintf(PON_RESOURCE_RANGE_CONFIG_PATH, PONRMgr.OLTModel)
	//get resource from kv store
	Result, err := PONRMgr.KVStore.Get(ctx, Path)
	if err != nil {
		logger.Debugf(ctx, "Error in fetching resource %s from KV strore", Path)
		return false
	}
	if Result == nil {
		logger.Debug(ctx, "There may be no resources in the KV store in case of fresh bootup, return true")
		return false
	}
	//update internal ranges from kv ranges. If there are missing
	// values in the KV profile, continue to use the defaults
	Value, err := ToByte(Result.Value)
	if err != nil {
		logger.Error(ctx, "Failed to convert kvpair to byte string")
		return false
	}
	if err := json.Unmarshal(Value, &PONRMgr.PonResourceRanges); err != nil {
		logger.Error(ctx, "Failed to Unmarshal json byte")
		return false
	}
	logger.Debug(ctx, "Init resource ranges from kvstore success")
	return true
}

func (PONRMgr *PONResourceManager) UpdateRanges(ctx context.Context, StartIDx string, StartID uint32, EndIDx string, EndID uint32,
	SharedIDx string, SharedPoolID uint32, RMgr *PONResourceManager) {
	/*
	   Update the ranges for all reosurce type in the intermnal maps
	   param: resource type start index
	   param: start ID
	   param: resource type end index
	   param: end ID
	   param: resource type shared index
	   param: shared pool id
	   param: global resource manager
	*/
	logger.Debugf(ctx, "update ranges for %s, %d", StartIDx, StartID)

	if StartID != 0 {
		if (PONRMgr.PonResourceRanges[StartIDx] == nil) || (PONRMgr.PonResourceRanges[StartIDx].(uint32) < StartID) {
			PONRMgr.PonResourceRanges[StartIDx] = StartID
		}
	}
	if EndID != 0 {
		if (PONRMgr.PonResourceRanges[EndIDx] == nil) || (PONRMgr.PonResourceRanges[EndIDx].(uint32) > EndID) {
			PONRMgr.PonResourceRanges[EndIDx] = EndID
		}
	}
	//if SharedPoolID != 0 {
	PONRMgr.PonResourceRanges[SharedIDx] = SharedPoolID
	//}
	if RMgr != nil {
		PONRMgr.SharedResourceMgrs[SharedIDx] = RMgr
	}
}

func (PONRMgr *PONResourceManager) InitDefaultPONResourceRanges(ctx context.Context,
	ONUIDStart uint32,
	ONUIDEnd uint32,
	ONUIDSharedPoolID uint32,
	AllocIDStart uint32,
	AllocIDEnd uint32,
	AllocIDSharedPoolID uint32,
	GEMPortIDStart uint32,
	GEMPortIDEnd uint32,
	GEMPortIDSharedPoolID uint32,
	FlowIDStart uint32,
	FlowIDEnd uint32,
	FlowIDSharedPoolID uint32,
	UNIIDStart uint32,
	UNIIDEnd uint32,
	NoOfPONPorts uint32,
	IntfIDs []uint32) bool {

	/*Initialize default PON resource ranges

	  :param onu_id_start_idx: onu id start index
	  :param onu_id_end_idx: onu id end index
	  :param onu_id_shared_pool_id: pool idx for id shared by all intfs or None for no sharing
	  :param alloc_id_start_idx: alloc id start index
	  :param alloc_id_end_idx: alloc id end index
	  :param alloc_id_shared_pool_id: pool idx for alloc id shared by all intfs or None for no sharing
	  :param gemport_id_start_idx: gemport id start index
	  :param gemport_id_end_idx: gemport id end index
	  :param gemport_id_shared_pool_id: pool idx for gemport id shared by all intfs or None for no sharing
	  :param flow_id_start_idx: flow id start index
	  :param flow_id_end_idx: flow id end index
	  :param flow_id_shared_pool_id: pool idx for flow id shared by all intfs or None for no sharing
	  :param num_of_pon_ports: number of PON ports
	  :param intf_ids: interfaces serviced by this manager
	*/
	PONRMgr.UpdateRanges(ctx, ONU_ID_START_IDX, ONUIDStart, ONU_ID_END_IDX, ONUIDEnd, ONU_ID_SHARED_IDX, ONUIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(ctx, ALLOC_ID_START_IDX, AllocIDStart, ALLOC_ID_END_IDX, AllocIDEnd, ALLOC_ID_SHARED_IDX, AllocIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(ctx, GEMPORT_ID_START_IDX, GEMPortIDStart, GEMPORT_ID_END_IDX, GEMPortIDEnd, GEMPORT_ID_SHARED_IDX, GEMPortIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(ctx, FLOW_ID_START_IDX, FlowIDStart, FLOW_ID_END_IDX, FlowIDEnd, FLOW_ID_SHARED_IDX, FlowIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(ctx, UNI_ID_START_IDX, UNIIDStart, UNI_ID_END_IDX, UNIIDEnd, "", 0, nil)
	logger.Debug(ctx, "Initialize default range values")
	var i uint32
	if IntfIDs == nil {
		for i = 0; i < NoOfPONPorts; i++ {
			PONRMgr.IntfIDs = append(PONRMgr.IntfIDs, i)
		}
	} else {
		PONRMgr.IntfIDs = IntfIDs
	}
	return true
}

func (PONRMgr *PONResourceManager) InitDeviceResourcePool(ctx context.Context) error {

	//Initialize resource pool for all PON ports.

	logger.Debug(ctx, "Init resource ranges")

	var err error
	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[ONU_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if err = PONRMgr.InitResourceIDPool(ctx, Intf, ONU_ID,
			PONRMgr.PonResourceRanges[ONU_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[ONU_ID_END_IDX].(uint32)); err != nil {
			logger.Error(ctx, "Failed to init ONU ID resource pool")
			return err
		}
		if SharedPoolID != 0 {
			break
		}
	}

	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[ALLOC_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if err = PONRMgr.InitResourceIDPool(ctx, Intf, ALLOC_ID,
			PONRMgr.PonResourceRanges[ALLOC_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[ALLOC_ID_END_IDX].(uint32)); err != nil {
			logger.Error(ctx, "Failed to init ALLOC ID resource pool ")
			return err
		}
		if SharedPoolID != 0 {
			break
		}
	}
	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[GEMPORT_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if err = PONRMgr.InitResourceIDPool(ctx, Intf, GEMPORT_ID,
			PONRMgr.PonResourceRanges[GEMPORT_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[GEMPORT_ID_END_IDX].(uint32)); err != nil {
			logger.Error(ctx, "Failed to init GEMPORT ID resource pool")
			return err
		}
		if SharedPoolID != 0 {
			break
		}
	}

	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[FLOW_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if err = PONRMgr.InitResourceIDPool(ctx, Intf, FLOW_ID,
			PONRMgr.PonResourceRanges[FLOW_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[FLOW_ID_END_IDX].(uint32)); err != nil {
			logger.Error(ctx, "Failed to init FLOW ID resource pool")
			return err
		}
		if SharedPoolID != 0 {
			break
		}
	}
	return err
}

func (PONRMgr *PONResourceManager) InitDeviceResourcePoolForIntf(ctx context.Context, intfID uint32) error {

	logger.Debug(ctx, "Init resource ranges for intf %d", intfID)

	var err error

	if err = PONRMgr.InitResourceIDPool(ctx, intfID, ONU_ID,
		PONRMgr.PonResourceRanges[ONU_ID_START_IDX].(uint32),
		PONRMgr.PonResourceRanges[ONU_ID_END_IDX].(uint32)); err != nil {
		logger.Error(ctx, "Failed to init ONU ID resource pool")
		return err
	}

	if err = PONRMgr.InitResourceIDPool(ctx, intfID, ALLOC_ID,
		PONRMgr.PonResourceRanges[ALLOC_ID_START_IDX].(uint32),
		PONRMgr.PonResourceRanges[ALLOC_ID_END_IDX].(uint32)); err != nil {
		logger.Error(ctx, "Failed to init ALLOC ID resource pool ")
		return err
	}

	if err = PONRMgr.InitResourceIDPool(ctx, intfID, GEMPORT_ID,
		PONRMgr.PonResourceRanges[GEMPORT_ID_START_IDX].(uint32),
		PONRMgr.PonResourceRanges[GEMPORT_ID_END_IDX].(uint32)); err != nil {
		logger.Error(ctx, "Failed to init GEMPORT ID resource pool")
		return err
	}

	if err = PONRMgr.InitResourceIDPool(ctx, intfID, FLOW_ID,
		PONRMgr.PonResourceRanges[FLOW_ID_START_IDX].(uint32),
		PONRMgr.PonResourceRanges[FLOW_ID_END_IDX].(uint32)); err != nil {
		logger.Error(ctx, "Failed to init FLOW ID resource pool")
		return err
	}

	return nil
}

func (PONRMgr *PONResourceManager) ClearDeviceResourcePool(ctx context.Context) error {

	//Clear resource pool for all PON ports.

	logger.Debug(ctx, "Clear resource ranges")

	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[ONU_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if status := PONRMgr.ClearResourceIDPool(ctx, Intf, ONU_ID); !status {
			logger.Error(ctx, "Failed to clear ONU ID resource pool")
			return errors.New("Failed to clear ONU ID resource pool")
		}
		if SharedPoolID != 0 {
			break
		}
	}

	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[ALLOC_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if status := PONRMgr.ClearResourceIDPool(ctx, Intf, ALLOC_ID); !status {
			logger.Error(ctx, "Failed to clear ALLOC ID resource pool ")
			return errors.New("Failed to clear ALLOC ID resource pool")
		}
		if SharedPoolID != 0 {
			break
		}
	}
	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[GEMPORT_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if status := PONRMgr.ClearResourceIDPool(ctx, Intf, GEMPORT_ID); !status {
			logger.Error(ctx, "Failed to clear GEMPORT ID resource pool")
			return errors.New("Failed to clear GEMPORT ID resource pool")
		}
		if SharedPoolID != 0 {
			break
		}
	}

	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[FLOW_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if status := PONRMgr.ClearResourceIDPool(ctx, Intf, FLOW_ID); !status {
			logger.Error(ctx, "Failed to clear FLOW ID resource pool")
			return errors.New("Failed to clear FLOW ID resource pool")
		}
		if SharedPoolID != 0 {
			break
		}
	}
	return nil
}

func (PONRMgr *PONResourceManager) ClearDeviceResourcePoolForIntf(ctx context.Context, intfID uint32) error {

	logger.Debugf(ctx, "Clear resource ranges for intf %d", intfID)

	if status := PONRMgr.ClearResourceIDPool(ctx, intfID, ONU_ID); !status {
		logger.Error(ctx, "Failed to clear ONU ID resource pool")
		return errors.New("Failed to clear ONU ID resource pool")
	}

	if status := PONRMgr.ClearResourceIDPool(ctx, intfID, ALLOC_ID); !status {
		logger.Error(ctx, "Failed to clear ALLOC ID resource pool ")
		return errors.New("Failed to clear ALLOC ID resource pool")
	}

	if status := PONRMgr.ClearResourceIDPool(ctx, intfID, GEMPORT_ID); !status {
		logger.Error(ctx, "Failed to clear GEMPORT ID resource pool")
		return errors.New("Failed to clear GEMPORT ID resource pool")
	}

	if status := PONRMgr.ClearResourceIDPool(ctx, intfID, FLOW_ID); !status {
		logger.Error(ctx, "Failed to clear FLOW ID resource pool")
		return errors.New("Failed to clear FLOW ID resource pool")
	}

	return nil
}

func (PONRMgr *PONResourceManager) InitResourceIDPool(ctx context.Context, Intf uint32, ResourceType string, StartID uint32, EndID uint32) error {

	/*Initialize Resource ID pool for a given Resource Type on a given PON Port

	  :param pon_intf_id: OLT PON interface id
	  :param resource_type: String to identify type of resource
	  :param start_idx: start index for onu id pool
	  :param end_idx: end index for onu id pool
	  :return boolean: True if resource id pool initialized else false
	*/

	// delegate to the master instance if sharing enabled across instances
	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.InitResourceIDPool(ctx, Intf, ResourceType, StartID, EndID)
	}

	Path := PONRMgr.GetPath(ctx, Intf, ResourceType)
	if Path == "" {
		logger.Errorf(ctx, "Failed to get path for resource type %s", ResourceType)
		return fmt.Errorf("Failed to get path for resource type %s", ResourceType)
	}

	//In case of adapter reboot and reconciliation resource in kv store
	//checked for its presence if not kv store update happens
	Res, err := PONRMgr.GetResource(ctx, Path)
	if (err == nil) && (Res != nil) {
		logger.Debugf(ctx, "Resource %s already present in store ", Path)
		return nil
	} else {
		var excluded []uint32
		if ResourceType == GEMPORT_ID {
			//get gem port ids defined in the KV store, if any, and exclude them from the gem port id pool
			if reservedGemPortIds, defined := PONRMgr.getReservedGemPortIdsFromKVStore(ctx); defined {
				excluded = reservedGemPortIds
				logger.Debugw(ctx, "Excluding some ports from GEM port id pool", log.Fields{"excluded gem ports": excluded})
			}
		}
		FormatResult, err := PONRMgr.FormatResource(ctx, Intf, StartID, EndID, excluded)
		if err != nil {
			logger.Errorf(ctx, "Failed to format resource")
			return err
		}
		// Add resource as json in kv store.
		err = PONRMgr.KVStore.Put(ctx, Path, FormatResult)
		if err == nil {
			logger.Debug(ctx, "Successfuly posted to kv store")
			return err
		}
	}

	logger.Debug(ctx, "Error initializing pool")

	return err
}

func (PONRMgr *PONResourceManager) getReservedGemPortIdsFromKVStore(ctx context.Context) ([]uint32, bool) {
	var reservedGemPortIds []uint32
	// read reserved gem ports from the config path
	KvPair, err := PONRMgr.KVStoreForConfig.Get(ctx, RESERVED_GEMPORT_IDS_PATH)
	if err != nil {
		logger.Errorw(ctx, "Unable to get reserved GEM port ids from the kv store", log.Fields{"err": err})
		return reservedGemPortIds, false
	}
	if KvPair == nil || KvPair.Value == nil {
		//no reserved gem port defined in the store
		return reservedGemPortIds, false
	}
	Val, err := kvstore.ToByte(KvPair.Value)
	if err != nil {
		logger.Errorw(ctx, "Failed to convert reserved gem port ids into byte array", log.Fields{"err": err})
		return reservedGemPortIds, false
	}
	if err = json.Unmarshal(Val, &reservedGemPortIds); err != nil {
		logger.Errorw(ctx, "Failed to unmarshal reservedGemPortIds", log.Fields{"err": err})
		return reservedGemPortIds, false
	}
	return reservedGemPortIds, true
}

func (PONRMgr *PONResourceManager) FormatResource(ctx context.Context, IntfID uint32, StartIDx uint32, EndIDx uint32,
	Excluded []uint32) ([]byte, error) {
	/*
	   Format resource as json.
	   :param pon_intf_id: OLT PON interface id
	   :param start_idx: start index for id pool
	   :param end_idx: end index for id pool
	   :Id values to be Excluded from the pool
	   :return dictionary: resource formatted as map
	*/
	// Format resource as json to be stored in backend store
	Resource := make(map[string]interface{})
	Resource[PON_INTF_ID] = IntfID
	Resource[START_IDX] = StartIDx
	Resource[END_IDX] = EndIDx
	/*
	   Resource pool stored in backend store as binary string.
	   Tracking the resource allocation will be done by setting the bits \
	   in the byte array. The index set will be the resource number allocated.
	*/
	var TSData *bitmap.Threadsafe
	if TSData = bitmap.NewTS(int(EndIDx)); TSData == nil {
		logger.Error(ctx, "Failed to create a bitmap")
		return nil, errors.New("Failed to create bitmap")
	}
	for _, excludedID := range Excluded {
		if excludedID < StartIDx || excludedID > EndIDx {
			logger.Warnf(ctx, "Cannot reserve %d. It must be in the range of [%d, %d]", excludedID,
				StartIDx, EndIDx)
			continue
		}
		PONRMgr.reserveID(ctx, TSData, StartIDx, excludedID)
	}
	Resource[POOL] = TSData.Data(false) //we pass false so as the TSData lib api does not do a copy of the data and return

	Value, err := json.Marshal(Resource)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshall resource")
		return nil, err
	}
	return Value, err
}
func (PONRMgr *PONResourceManager) GetResource(ctx context.Context, Path string) (map[string]interface{}, error) {
	/*
	   Get resource from kv store.

	   :param path: path to get resource
	   :return: resource if resource present in kv store else None
	*/
	//get resource from kv store

	var Value []byte
	Result := make(map[string]interface{})
	var Str string

	Resource, err := PONRMgr.KVStore.Get(ctx, Path)
	if (err != nil) || (Resource == nil) {
		logger.Debugf(ctx, "Resource  unavailable at %s", Path)
		return nil, err
	}

	Value, err = ToByte(Resource.Value)
	if err != nil {
		return nil, err
	}

	// decode resource fetched from backend store to dictionary
	err = json.Unmarshal(Value, &Result)
	if err != nil {
		logger.Error(ctx, "Failed to decode resource")
		return Result, err
	}
	/*
	   resource pool in backend store stored as binary string whereas to
	   access the pool to generate/release IDs it need to be converted
	   as BitArray
	*/
	Str, err = ToString(Result[POOL])
	if err != nil {
		logger.Error(ctx, "Failed to conver to kv pair to string")
		return Result, err
	}
	Decode64, _ := base64.StdEncoding.DecodeString(Str)
	Result[POOL], err = ToByte(Decode64)
	if err != nil {
		logger.Error(ctx, "Failed to convert resource pool to byte")
		return Result, err
	}

	return Result, err
}

func (PONRMgr *PONResourceManager) GetPath(ctx context.Context, IntfID uint32, ResourceType string) string {
	/*
	   Get path for given resource type.
	   :param pon_intf_id: OLT PON interface id
	   :param resource_type: String to identify type of resource
	   :return: path for given resource type
	*/

	/*
	   Get the shared pool for the given resource type.
	   all the resource ranges and the shared resource maps are initialized during the init.
	*/
	SharedPoolID := PONRMgr.PonResourceRanges[PONRMgr.SharedIdxByType[ResourceType]].(uint32)
	if SharedPoolID != 0 {
		IntfID = SharedPoolID
	}
	var Path string
	if ResourceType == ONU_ID {
		Path = fmt.Sprintf(ONU_ID_POOL_PATH, PONRMgr.DeviceID, IntfID)
	} else if ResourceType == ALLOC_ID {
		Path = fmt.Sprintf(ALLOC_ID_POOL_PATH, PONRMgr.DeviceID, IntfID)
	} else if ResourceType == GEMPORT_ID {
		Path = fmt.Sprintf(GEMPORT_ID_POOL_PATH, PONRMgr.DeviceID, IntfID)
	} else if ResourceType == FLOW_ID {
		Path = fmt.Sprintf(FLOW_ID_POOL_PATH, PONRMgr.DeviceID, IntfID)
	} else {
		logger.Error(ctx, "Invalid resource pool identifier")
	}
	return Path
}

func (PONRMgr *PONResourceManager) GetResourceID(ctx context.Context, IntfID uint32, ResourceType string, NumIDs uint32) ([]uint32, error) {
	/*
	   Create alloc/gemport/onu/flow id for given OLT PON interface.
	   :param pon_intf_id: OLT PON interface id
	   :param resource_type: String to identify type of resource
	   :param num_of_id: required number of ids
	   :return list/uint32/None: list, uint32 or None if resource type is
	    alloc_id/gemport_id, onu_id or invalid type respectively
	*/

	logger.Debugw(ctx, "getting-resource-id", log.Fields{
		"intf-id":       IntfID,
		"resource-type": ResourceType,
		"num":           NumIDs,
	})

	if NumIDs < 1 {
		logger.Error(ctx, "Invalid number of resources requested")
		return nil, fmt.Errorf("Invalid number of resources requested %d", NumIDs)
	}
	// delegate to the master instance if sharing enabled across instances

	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.GetResourceID(ctx, IntfID, ResourceType, NumIDs)
	}
	logger.Debugf(ctx, "Fetching resource from %s rsrc mgr for resource %s", PONRMgr.Globalorlocal, ResourceType)

	Path := PONRMgr.GetPath(ctx, IntfID, ResourceType)
	if Path == "" {
		logger.Errorf(ctx, "Failed to get path for resource type %s", ResourceType)
		return nil, fmt.Errorf("Failed to get path for resource type %s", ResourceType)
	}
	logger.Debugf(ctx, "Get resource for type %s on path %s", ResourceType, Path)
	var Result []uint32
	var NextID uint32
	Resource, err := PONRMgr.GetResource(ctx, Path)
	if (err == nil) && (ResourceType == ONU_ID) || (ResourceType == FLOW_ID) {
		if NextID, err = PONRMgr.GenerateNextID(ctx, Resource); err != nil {
			logger.Error(ctx, "Failed to Generate ID")
			return Result, err
		}
		Result = append(Result, NextID)
	} else if (err == nil) && ((ResourceType == GEMPORT_ID) || (ResourceType == ALLOC_ID)) {
		if NumIDs == 1 {
			if NextID, err = PONRMgr.GenerateNextID(ctx, Resource); err != nil {
				logger.Error(ctx, "Failed to Generate ID")
				return Result, err
			}
			Result = append(Result, NextID)
		} else {
			for NumIDs > 0 {
				if NextID, err = PONRMgr.GenerateNextID(ctx, Resource); err != nil {
					logger.Error(ctx, "Failed to Generate ID")
					return Result, err
				}
				Result = append(Result, NextID)
				NumIDs--
			}
		}
	} else {
		logger.Error(ctx, "get resource failed")
		return Result, err
	}

	//Update resource in kv store
	if PONRMgr.UpdateResource(ctx, Path, Resource) != nil {
		logger.Errorf(ctx, "Failed to update resource %s", Path)
		return nil, fmt.Errorf("Failed to update resource %s", Path)
	}
	return Result, nil
}

func checkValidResourceType(ResourceType string) bool {
	KnownResourceTypes := []string{ONU_ID, ALLOC_ID, GEMPORT_ID, FLOW_ID}

	for _, v := range KnownResourceTypes {
		if v == ResourceType {
			return true
		}
	}
	return false
}

func (PONRMgr *PONResourceManager) FreeResourceID(ctx context.Context, IntfID uint32, ResourceType string, ReleaseContent []uint32) error {
	/*
	  Release alloc/gemport/onu/flow id for given OLT PON interface.
	  :param pon_intf_id: OLT PON interface id
	  :param resource_type: String to identify type of resource
	  :param release_content: required number of ids
	  :return boolean: True if all IDs in given release_content release else False
	*/

	logger.Debugw(ctx, "freeing-resource-id", log.Fields{
		"intf-id":         IntfID,
		"resource-type":   ResourceType,
		"release-content": ReleaseContent,
	})

	if !checkValidResourceType(ResourceType) {
		err := fmt.Errorf("Invalid resource type: %s", ResourceType)
		logger.Error(ctx, err.Error())
		return err
	}
	if ReleaseContent == nil {
		err := fmt.Errorf("Nothing to release")
		logger.Debug(ctx, err.Error())
		return err
	}
	// delegate to the master instance if sharing enabled across instances
	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.FreeResourceID(ctx, IntfID, ResourceType, ReleaseContent)
	}
	Path := PONRMgr.GetPath(ctx, IntfID, ResourceType)
	if Path == "" {
		err := fmt.Errorf("Failed to get path for IntfId %d and ResourceType %s", IntfID, ResourceType)
		logger.Error(ctx, err.Error())
		return err
	}
	Resource, err := PONRMgr.GetResource(ctx, Path)
	if err != nil {
		logger.Error(ctx, err.Error())
		return err
	}
	for _, Val := range ReleaseContent {
		PONRMgr.ReleaseID(ctx, Resource, Val)
	}
	if PONRMgr.UpdateResource(ctx, Path, Resource) != nil {
		err := fmt.Errorf("Free resource for %s failed", Path)
		logger.Errorf(ctx, err.Error())
		return err
	}
	return nil
}

func (PONRMgr *PONResourceManager) UpdateResource(ctx context.Context, Path string, Resource map[string]interface{}) error {
	/*
	   Update resource in resource kv store.
	   :param path: path to update resource
	   :param resource: resource need to be updated
	   :return boolean: True if resource updated in kv store else False
	*/
	// TODO resource[POOL] = resource[POOL].bin
	Value, err := json.Marshal(Resource)
	if err != nil {
		logger.Error(ctx, "failed to Marshal")
		return err
	}
	err = PONRMgr.KVStore.Put(ctx, Path, Value)
	if err != nil {
		logger.Error(ctx, "failed to put data to kv store %s", Path)
		return err
	}
	return nil
}

func (PONRMgr *PONResourceManager) ClearResourceIDPool(ctx context.Context, contIntfID uint32, ResourceType string) bool {
	/*
	   Clear Resource Pool for a given Resource Type on a given PON Port.
	   :return boolean: True if removed else False
	*/

	// delegate to the master instance if sharing enabled across instances
	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.ClearResourceIDPool(ctx, contIntfID, ResourceType)
	}
	Path := PONRMgr.GetPath(ctx, contIntfID, ResourceType)
	if Path == "" {
		logger.Error(ctx, "Failed to get path")
		return false
	}

	if err := PONRMgr.KVStore.Delete(ctx, Path); err != nil {
		logger.Errorf(ctx, "Failed to delete resource %s", Path)
		return false
	}
	logger.Debugf(ctx, "Cleared resource %s", Path)
	return true
}

func (PONRMgr PONResourceManager) InitResourceMap(ctx context.Context, PONIntfONUID string) {
	/*
	   Initialize resource map
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	*/
	// initialize pon_intf_onu_id tuple to alloc_ids map
	AllocIDPath := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	var AllocIDs []byte
	Result := PONRMgr.KVStore.Put(ctx, AllocIDPath, AllocIDs)
	if Result != nil {
		logger.Error(ctx, "Failed to update the KV store")
		return
	}
	// initialize pon_intf_onu_id tuple to gemport_ids map
	GEMPortIDPath := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	var GEMPortIDs []byte
	Result = PONRMgr.KVStore.Put(ctx, GEMPortIDPath, GEMPortIDs)
	if Result != nil {
		logger.Error(ctx, "Failed to update the KV store")
		return
	}
}

func (PONRMgr PONResourceManager) RemoveResourceMap(ctx context.Context, PONIntfONUID string) bool {
	/*
	   Remove resource map
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	*/
	// remove pon_intf_onu_id tuple to alloc_ids map
	var err error
	AllocIDPath := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	if err = PONRMgr.KVStore.Delete(ctx, AllocIDPath); err != nil {
		logger.Errorf(ctx, "Failed to remove resource %s", AllocIDPath)
		return false
	}
	// remove pon_intf_onu_id tuple to gemport_ids map
	GEMPortIDPath := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	err = PONRMgr.KVStore.Delete(ctx, GEMPortIDPath)
	if err != nil {
		logger.Errorf(ctx, "Failed to remove resource %s", GEMPortIDPath)
		return false
	}

	FlowIDPath := fmt.Sprintf(FLOW_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	if FlowIDs, err := PONRMgr.KVStore.List(ctx, FlowIDPath); err != nil {
		for _, Flow := range FlowIDs {
			FlowIDInfoPath := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, PONIntfONUID, Flow.Value)
			if err = PONRMgr.KVStore.Delete(ctx, FlowIDInfoPath); err != nil {
				logger.Errorf(ctx, "Failed to remove resource %s", FlowIDInfoPath)
				return false
			}
		}
	}

	if err = PONRMgr.KVStore.Delete(ctx, FlowIDPath); err != nil {
		logger.Errorf(ctx, "Failed to remove resource %s", FlowIDPath)
		return false
	}

	return true
}

func (PONRMgr *PONResourceManager) GetCurrentAllocIDForOnu(ctx context.Context, IntfONUID string) []uint32 {
	/*
	   Get currently configured alloc ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :return list: List of alloc_ids if available, else None
	*/
	Path := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)

	var Data []uint32
	Value, err := PONRMgr.KVStore.Get(ctx, Path)
	if err == nil {
		if Value != nil {
			Val, err := ToByte(Value.Value)
			if err != nil {
				logger.Errorw(ctx, "Failed to convert into byte array", log.Fields{"error": err})
				return Data
			}
			if err = json.Unmarshal(Val, &Data); err != nil {
				logger.Error(ctx, "Failed to unmarshal", log.Fields{"error": err})
				return Data
			}
		}
	}
	return Data
}

func (PONRMgr *PONResourceManager) GetCurrentGEMPortIDsForOnu(ctx context.Context, IntfONUID string) []uint32 {
	/*
	   Get currently configured gemport ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :return list: List of gemport IDs if available, else None
	*/

	Path := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	logger.Debugf(ctx, "Getting current gemports for %s", Path)
	var Data []uint32
	Value, err := PONRMgr.KVStore.Get(ctx, Path)
	if err == nil {
		if Value != nil {
			Val, _ := ToByte(Value.Value)
			if err = json.Unmarshal(Val, &Data); err != nil {
				logger.Errorw(ctx, "Failed to unmarshal", log.Fields{"error": err})
				return Data
			}
		}
	} else {
		logger.Errorf(ctx, "Failed to get data from kvstore for %s", Path)
	}
	return Data
}

func (PONRMgr *PONResourceManager) GetCurrentFlowIDsForOnu(ctx context.Context, IntfONUID string) []uint32 {
	/*
	   Get currently configured flow ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :return list: List of Flow IDs if available, else None
	*/

	Path := fmt.Sprintf(FLOW_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)

	var Data []uint32
	Value, err := PONRMgr.KVStore.Get(ctx, Path)
	if err == nil {
		if Value != nil {
			Val, _ := ToByte(Value.Value)
			if err = json.Unmarshal(Val, &Data); err != nil {
				logger.Error(ctx, "Failed to unmarshal")
				return Data
			}
		}
	}
	return Data
}

func (PONRMgr *PONResourceManager) GetFlowIDInfo(ctx context.Context, IntfONUID string, FlowID uint32, Data interface{}) error {
	/*
	   Get flow details configured for the ONU.
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param flow_id: Flow Id reference
	   :param Data: Result
	   :return error: nil if no error in getting from KV store
	*/

	Path := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, IntfONUID, FlowID)

	Value, err := PONRMgr.KVStore.Get(ctx, Path)
	if err == nil {
		if Value != nil {
			Val, err := ToByte(Value.Value)
			if err != nil {
				logger.Errorw(ctx, "Failed to convert flowinfo into byte array", log.Fields{"error": err})
				return err
			}
			if err = json.Unmarshal(Val, Data); err != nil {
				logger.Errorw(ctx, "Failed to unmarshal", log.Fields{"error": err})
				return err
			}
		}
	}
	return err
}

func (PONRMgr *PONResourceManager) RemoveFlowIDInfo(ctx context.Context, IntfONUID string, FlowID uint32) bool {
	/*
	   Get flow_id details configured for the ONU.
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param flow_id: Flow Id reference
	*/
	Path := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, IntfONUID, FlowID)

	if err := PONRMgr.KVStore.Delete(ctx, Path); err != nil {
		logger.Errorf(ctx, "Falied to remove resource %s", Path)
		return false
	}
	return true
}

func (PONRMgr *PONResourceManager) RemoveAllFlowIDInfo(ctx context.Context, IntfONUID string) bool {
	/*
	    Remove flow_id_info details configured for the ONU.
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	*/
	Path := fmt.Sprintf(FLOW_ID_INFO_PATH_INTF_ONU_PREFIX, PONRMgr.DeviceID, IntfONUID)

	if err := PONRMgr.KVStore.DeleteWithPrefix(ctx, Path); err != nil {
		logger.Errorf(ctx, "Falied to remove resource %s", Path)
		return false
	}
	return true
}

func (PONRMgr *PONResourceManager) UpdateAllocIdsForOnu(ctx context.Context, IntfONUID string, AllocIDs []uint32) error {
	/*
	   Update currently configured alloc ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param alloc_ids: list of alloc ids
	*/
	var Value []byte
	var err error
	Path := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	if AllocIDs == nil {
		// No more alloc ids associated with the key. Delete the key entirely
		if err = PONRMgr.KVStore.Delete(ctx, Path); err != nil {
			logger.Errorf(ctx, "Failed to delete key %s", Path)
			return err
		}
	} else {
		Value, err = json.Marshal(AllocIDs)
		if err != nil {
			logger.Error(ctx, "failed to Marshal")
			return err
		}

		if err = PONRMgr.KVStore.Put(ctx, Path, Value); err != nil {
			logger.Errorf(ctx, "Failed to update resource %s", Path)
			return err
		}
	}
	return err
}

func (PONRMgr *PONResourceManager) UpdateGEMPortIDsForOnu(ctx context.Context, IntfONUID string, GEMPortIDs []uint32) error {
	/*
	   Update currently configured gemport ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param gemport_ids: list of gem port ids
	*/

	var Value []byte
	var err error
	Path := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	logger.Debugf(ctx, "Updating gemport ids for %s", Path)
	if GEMPortIDs == nil {
		// No more gemport ids associated with the key. Delete the key entirely
		if err = PONRMgr.KVStore.Delete(ctx, Path); err != nil {
			logger.Errorf(ctx, "Failed to delete key %s", Path)
			return err
		}
	} else {
		Value, err = json.Marshal(GEMPortIDs)
		if err != nil {
			logger.Error(ctx, "failed to Marshal")
			return err
		}

		if err = PONRMgr.KVStore.Put(ctx, Path, Value); err != nil {
			logger.Errorf(ctx, "Failed to update resource %s", Path)
			return err
		}
	}
	return err
}

func checkForFlowIDInList(FlowIDList []uint32, FlowID uint32) (bool, uint32) {
	/*
	   Check for a flow id in a given list of flow IDs.
	   :param FLowIDList: List of Flow IDs
	   :param FlowID: Flowd to check in the list
	   : return true and the index if present false otherwise.
	*/

	for idx := range FlowIDList {
		if FlowID == FlowIDList[idx] {
			return true, uint32(idx)
		}
	}
	return false, 0
}

func (PONRMgr *PONResourceManager) UpdateFlowIDForOnu(ctx context.Context, IntfONUID string, FlowID uint32, Add bool) error {
	/*
	   Update the flow_id list of the ONU (add or remove flow_id from the list)
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param flow_id: flow ID
	   :param add: Boolean flag to indicate whether the flow_id should be
	               added or removed from the list. Defaults to adding the flow.
	*/
	var Value []byte
	var err error
	var RetVal bool
	var IDx uint32
	Path := fmt.Sprintf(FLOW_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	FlowIDs := PONRMgr.GetCurrentFlowIDsForOnu(ctx, IntfONUID)

	if Add {
		if RetVal, _ = checkForFlowIDInList(FlowIDs, FlowID); RetVal {
			return nil
		}
		FlowIDs = append(FlowIDs, FlowID)
	} else {
		if RetVal, IDx = checkForFlowIDInList(FlowIDs, FlowID); !RetVal {
			return nil
		}
		// delete the index and shift
		FlowIDs = append(FlowIDs[:IDx], FlowIDs[IDx+1:]...)
	}
	Value, err = json.Marshal(FlowIDs)
	if err != nil {
		logger.Error(ctx, "Failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(ctx, Path, Value); err != nil {
		logger.Errorf(ctx, "Failed to update resource %s", Path)
		return err
	}
	return err
}

func (PONRMgr *PONResourceManager) UpdateFlowIDInfoForOnu(ctx context.Context, IntfONUID string, FlowID uint32, FlowData interface{}) error {
	/*
	   Update any metadata associated with the flow_id. The flow_data could be json
	   or any of other data structure. The resource manager doesnt care
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param flow_id: Flow ID
	   :param flow_data: Flow data blob
	*/
	var Value []byte
	var err error
	Path := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, IntfONUID, FlowID)
	Value, err = json.Marshal(FlowData)
	if err != nil {
		logger.Error(ctx, "failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(ctx, Path, Value); err != nil {
		logger.Errorf(ctx, "Failed to update resource %s", Path)
		return err
	}
	return err
}

func (PONRMgr *PONResourceManager) GenerateNextID(ctx context.Context, Resource map[string]interface{}) (uint32, error) {
	/*
	   Generate unique id having OFFSET as start
	   :param resource: resource used to generate ID
	   :return uint32: generated id
	*/
	ByteArray, err := ToByte(Resource[POOL])
	if err != nil {
		return 0, err
	}
	Data := bitmap.TSFromData(ByteArray, false)
	if Data == nil {
		return 0, errors.New("Failed to get data from byte array")
	}

	Len := Data.Len()
	var Idx int
	for Idx = 0; Idx < Len; Idx++ {
		if !Data.Get(Idx) {
			break
		}
	}
	if Idx == Len {
		return 0, errors.New("resource-exhausted--no-free-id-in-the-pool")
	}
	Data.Set(Idx, true)
	res := uint32(Resource[START_IDX].(float64))
	Resource[POOL] = Data.Data(false)
	logger.Debugf(ctx, "Generated ID for %d", (uint32(Idx) + res))
	return (uint32(Idx) + res), err
}

func (PONRMgr *PONResourceManager) ReleaseID(ctx context.Context, Resource map[string]interface{}, Id uint32) bool {
	/*
	   Release unique id having OFFSET as start index.
	   :param resource: resource used to release ID
	   :param unique_id: id need to be released
	*/
	ByteArray, err := ToByte(Resource[POOL])
	if err != nil {
		logger.Error(ctx, "Failed to convert resource to byte array")
		return false
	}
	Data := bitmap.TSFromData(ByteArray, false)
	if Data == nil {
		logger.Error(ctx, "Failed to get resource pool")
		return false
	}
	Idx := Id - uint32(Resource[START_IDX].(float64))
	if Idx >= uint32(Data.Len()) {
		logger.Errorf(ctx, "ID %d is out of the boundaries of the pool", Id)
		return false
	}
	Data.Set(int(Idx), false)
	Resource[POOL] = Data.Data(false)

	return true
}

/* Reserves a unique id in the specified resource pool.
:param Resource: resource used to reserve ID
:param Id: ID to be reserved
*/
func (PONRMgr *PONResourceManager) reserveID(ctx context.Context, TSData *bitmap.Threadsafe, StartIndex uint32, Id uint32) bool {
	Data := bitmap.TSFromData(TSData.Data(false), false)
	if Data == nil {
		logger.Error(ctx, "Failed to get resource pool")
		return false
	}
	Idx := Id - StartIndex
	if Idx >= uint32(Data.Len()) {
		logger.Errorf(ctx, "Reservation failed. ID %d is out of the boundaries of the pool", Id)
		return false
	}
	Data.Set(int(Idx), true)
	return true
}

func (PONRMgr *PONResourceManager) GetTechnology() string {
	return PONRMgr.Technology
}

func (PONRMgr *PONResourceManager) GetResourceTypeAllocID() string {
	return ALLOC_ID
}

func (PONRMgr *PONResourceManager) GetResourceTypeGemPortID() string {
	return GEMPORT_ID
}

func (PONRMgr *PONResourceManager) GetResourceTypeOnuID() string {
	return ONU_ID
}

// ToByte converts an interface value to a []byte.  The interface should either be of
// a string type or []byte.  Otherwise, an error is returned.
func ToByte(value interface{}) ([]byte, error) {
	switch t := value.(type) {
	case []byte:
		return value.([]byte), nil
	case string:
		return []byte(value.(string)), nil
	default:
		return nil, fmt.Errorf("unexpected-type-%T", t)
	}
}

// ToString converts an interface value to a string.  The interface should either be of
// a string type or []byte.  Otherwise, an error is returned.
func ToString(value interface{}) (string, error) {
	switch t := value.(type) {
	case []byte:
		return string(value.([]byte)), nil
	case string:
		return value.(string), nil
	default:
		return "", fmt.Errorf("unexpected-type-%T", t)
	}
}
