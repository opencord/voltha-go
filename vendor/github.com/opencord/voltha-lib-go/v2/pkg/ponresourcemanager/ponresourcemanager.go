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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/boljen/go-bitmap"
	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-lib-go/v2/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	tp "github.com/opencord/voltha-lib-go/v2/pkg/techprofile"
)

const (
	//Constants to identify resource pool
	UNI_ID     = "UNI_ID"
	ONU_ID     = "ONU_ID"
	ALLOC_ID   = "ALLOC_ID"
	GEMPORT_ID = "GEMPORT_ID"
	FLOW_ID    = "FLOW_ID"

	//Constants for passing command line arugments
	OLT_MODEL_ARG = "--olt_model"
	PATH_PREFIX   = "service/voltha/resource_manager/{%s}"
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
	//Format: <device_id>/<(pon_intf_id, onu_id)>/flow_id_info/<flow_id>
	FLOW_ID_INFO_PATH = "{%s}/{%s}/flow_id_info/{%d}"

	//path on the kvstore to store onugem info map
	//format: <device-id>/onu_gem_info/<intfid>
	ONU_GEM_INFO_PATH = "{%s}/onu_gem_info/{%d}" // onu_gem/<(intfid)>

	//Constants for internal usage.
	PON_INTF_ID     = "pon_intf_id"
	START_IDX       = "start_idx"
	END_IDX         = "end_idx"
	POOL            = "pool"
	NUM_OF_PON_INTF = 16

	KVSTORE_RETRY_TIMEOUT = 5
)

//type ResourceTypeIndex string
//type ResourceType string

type PONResourceManager struct {
	//Implements APIs to initialize/allocate/release alloc/gemport/onu IDs.
	Technology     string
	DeviceType     string
	DeviceID       string
	Backend        string // ETCD, or consul
	Host           string // host ip of the KV store
	Port           int    // port number for the KV store
	OLTModel       string
	KVStore        *db.Backend
	TechProfileMgr tp.TechProfileIf // create object of *tp.TechProfileMgr

	// Below attribute, pon_resource_ranges, should be initialized
	// by reading from KV store.
	PonResourceRanges  map[string]interface{}
	SharedResourceMgrs map[string]*PONResourceManager
	SharedIdxByType    map[string]string
	IntfIDs            []uint32 // list of pon interface IDs
	Globalorlocal      string
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

func SetKVClient(Technology string, Backend string, Host string, Port int) *db.Backend {
	addr := Host + ":" + strconv.Itoa(Port)
	// TODO : Make sure direct call to NewBackend is working fine with backend , currently there is some
	// issue between kv store and backend , core is not calling NewBackend directly
	kvClient, err := newKVClient(Backend, addr, KVSTORE_RETRY_TIMEOUT)
	if err != nil {
		log.Fatalw("Failed to init KV client\n", log.Fields{"err": err})
		return nil
	}
	kvbackend := &db.Backend{
		Client:     kvClient,
		StoreType:  Backend,
		Host:       Host,
		Port:       Port,
		Timeout:    KVSTORE_RETRY_TIMEOUT,
		PathPrefix: fmt.Sprintf(PATH_PREFIX, Technology)}

	return kvbackend
}

// NewPONResourceManager creates a new PON resource manager.
func NewPONResourceManager(Technology string, DeviceType string, DeviceID string, Backend string, Host string, Port int) (*PONResourceManager, error) {
	var PONMgr PONResourceManager
	PONMgr.Technology = Technology
	PONMgr.DeviceType = DeviceType
	PONMgr.DeviceID = DeviceID
	PONMgr.Backend = Backend
	PONMgr.Host = Host
	PONMgr.Port = Port
	PONMgr.KVStore = SetKVClient(Technology, Backend, Host, Port)
	if PONMgr.KVStore == nil {
		log.Error("KV Client initilization failed")
		return nil, errors.New("Failed to init KV client")
	}
	// Initialize techprofile for this technology
	if PONMgr.TechProfileMgr, _ = tp.NewTechProfile(&PONMgr, Backend, Host, Port); PONMgr.TechProfileMgr == nil {
		log.Error("Techprofile initialization failed")
		return nil, errors.New("Failed to init tech profile")
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

func (PONRMgr *PONResourceManager) InitResourceRangesFromKVStore() bool {
	//Initialize PON resource ranges with config fetched from kv store.
	//:return boolean: True if PON resource ranges initialized else false
	// Try to initialize the PON Resource Ranges from KV store based on the
	// OLT model key, if available
	if PONRMgr.OLTModel == "" {
		log.Error("Failed to get OLT model")
		return false
	}
	Path := fmt.Sprintf(PON_RESOURCE_RANGE_CONFIG_PATH, PONRMgr.OLTModel)
	//get resource from kv store
	Result, err := PONRMgr.KVStore.Get(Path)
	if err != nil {
		log.Debugf("Error in fetching resource %s from KV strore", Path)
		return false
	}
	if Result == nil {
		log.Debug("There may be no resources in the KV store in case of fresh bootup, return true")
		return false
	}
	//update internal ranges from kv ranges. If there are missing
	// values in the KV profile, continue to use the defaults
	Value, err := ToByte(Result.Value)
	if err != nil {
		log.Error("Failed to convert kvpair to byte string")
		return false
	}
	if err := json.Unmarshal(Value, &PONRMgr.PonResourceRanges); err != nil {
		log.Error("Failed to Unmarshal json byte")
		return false
	}
	log.Debug("Init resource ranges from kvstore success")
	return true
}

func (PONRMgr *PONResourceManager) UpdateRanges(StartIDx string, StartID uint32, EndIDx string, EndID uint32,
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
	log.Debugf("update ranges for %s, %d", StartIDx, StartID)

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

func (PONRMgr *PONResourceManager) InitDefaultPONResourceRanges(ONUIDStart uint32,
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
	PONRMgr.UpdateRanges(ONU_ID_START_IDX, ONUIDStart, ONU_ID_END_IDX, ONUIDEnd, ONU_ID_SHARED_IDX, ONUIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(ALLOC_ID_START_IDX, AllocIDStart, ALLOC_ID_END_IDX, AllocIDEnd, ALLOC_ID_SHARED_IDX, AllocIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(GEMPORT_ID_START_IDX, GEMPortIDStart, GEMPORT_ID_END_IDX, GEMPortIDEnd, GEMPORT_ID_SHARED_IDX, GEMPortIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(FLOW_ID_START_IDX, FlowIDStart, FLOW_ID_END_IDX, FlowIDEnd, FLOW_ID_SHARED_IDX, FlowIDSharedPoolID, nil)
	PONRMgr.UpdateRanges(UNI_ID_START_IDX, UNIIDStart, UNI_ID_END_IDX, UNIIDEnd, "", 0, nil)
	log.Debug("Initialize default range values")
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

func (PONRMgr *PONResourceManager) InitDeviceResourcePool() error {

	//Initialize resource pool for all PON ports.

	log.Debug("Init resource ranges")

	var err error
	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[ONU_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if err = PONRMgr.InitResourceIDPool(Intf, ONU_ID,
			PONRMgr.PonResourceRanges[ONU_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[ONU_ID_END_IDX].(uint32)); err != nil {
			log.Error("Failed to init ONU ID resource pool")
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
		if err = PONRMgr.InitResourceIDPool(Intf, ALLOC_ID,
			PONRMgr.PonResourceRanges[ALLOC_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[ALLOC_ID_END_IDX].(uint32)); err != nil {
			log.Error("Failed to init ALLOC ID resource pool ")
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
		if err = PONRMgr.InitResourceIDPool(Intf, GEMPORT_ID,
			PONRMgr.PonResourceRanges[GEMPORT_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[GEMPORT_ID_END_IDX].(uint32)); err != nil {
			log.Error("Failed to init GEMPORT ID resource pool")
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
		if err = PONRMgr.InitResourceIDPool(Intf, FLOW_ID,
			PONRMgr.PonResourceRanges[FLOW_ID_START_IDX].(uint32),
			PONRMgr.PonResourceRanges[FLOW_ID_END_IDX].(uint32)); err != nil {
			log.Error("Failed to init FLOW ID resource pool")
			return err
		}
		if SharedPoolID != 0 {
			break
		}
	}
	return err
}

func (PONRMgr *PONResourceManager) ClearDeviceResourcePool() error {

	//Clear resource pool for all PON ports.

	log.Debug("Clear resource ranges")

	for _, Intf := range PONRMgr.IntfIDs {
		SharedPoolID := PONRMgr.PonResourceRanges[ONU_ID_SHARED_IDX].(uint32)
		if SharedPoolID != 0 {
			Intf = SharedPoolID
		}
		if status := PONRMgr.ClearResourceIDPool(Intf, ONU_ID); status != true {
			log.Error("Failed to clear ONU ID resource pool")
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
		if status := PONRMgr.ClearResourceIDPool(Intf, ALLOC_ID); status != true {
			log.Error("Failed to clear ALLOC ID resource pool ")
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
		if status := PONRMgr.ClearResourceIDPool(Intf, GEMPORT_ID); status != true {
			log.Error("Failed to clear GEMPORT ID resource pool")
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
		if status := PONRMgr.ClearResourceIDPool(Intf, FLOW_ID); status != true {
			log.Error("Failed to clear FLOW ID resource pool")
			return errors.New("Failed to clear FLOW ID resource pool")
		}
		if SharedPoolID != 0 {
			break
		}
	}
	return nil
}

func (PONRMgr *PONResourceManager) InitResourceIDPool(Intf uint32, ResourceType string, StartID uint32, EndID uint32) error {

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
		return SharedResourceMgr.InitResourceIDPool(Intf, ResourceType, StartID, EndID)
	}

	Path := PONRMgr.GetPath(Intf, ResourceType)
	if Path == "" {
		log.Errorf("Failed to get path for resource type %s", ResourceType)
		return errors.New(fmt.Sprintf("Failed to get path for resource type %s", ResourceType))
	}

	//In case of adapter reboot and reconciliation resource in kv store
	//checked for its presence if not kv store update happens
	Res, err := PONRMgr.GetResource(Path)
	if (err == nil) && (Res != nil) {
		log.Debugf("Resource %s already present in store ", Path)
		return nil
	} else {
		FormatResult, err := PONRMgr.FormatResource(Intf, StartID, EndID)
		if err != nil {
			log.Errorf("Failed to format resource")
			return err
		}
		// Add resource as json in kv store.
		err = PONRMgr.KVStore.Put(Path, FormatResult)
		if err == nil {
			log.Debug("Successfuly posted to kv store")
			return err
		}
	}

	log.Debug("Error initializing pool")

	return err
}

func (PONRMgr *PONResourceManager) FormatResource(IntfID uint32, StartIDx uint32, EndIDx uint32) ([]byte, error) {
	/*
	   Format resource as json.
	   :param pon_intf_id: OLT PON interface id
	   :param start_idx: start index for id pool
	   :param end_idx: end index for id pool
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
		log.Error("Failed to create a bitmap")
		return nil, errors.New("Failed to create bitmap")
	}
	Resource[POOL] = TSData.Data(false) //we pass false so as the TSData lib api does not do a copy of the data and return

	Value, err := json.Marshal(Resource)
	if err != nil {
		log.Errorf("Failed to marshall resource")
		return nil, err
	}
	return Value, err
}
func (PONRMgr *PONResourceManager) GetResource(Path string) (map[string]interface{}, error) {
	/*
	   Get resource from kv store.

	   :param path: path to get resource
	   :return: resource if resource present in kv store else None
	*/
	//get resource from kv store

	var Value []byte
	Result := make(map[string]interface{})
	var Str string

	Resource, err := PONRMgr.KVStore.Get(Path)
	if (err != nil) || (Resource == nil) {
		log.Debugf("Resource  unavailable at %s", Path)
		return nil, err
	}

	Value, err = ToByte(Resource.Value)

	// decode resource fetched from backend store to dictionary
	err = json.Unmarshal(Value, &Result)
	if err != nil {
		log.Error("Failed to decode resource")
		return Result, err
	}
	/*
	   resource pool in backend store stored as binary string whereas to
	   access the pool to generate/release IDs it need to be converted
	   as BitArray
	*/
	Str, err = ToString(Result[POOL])
	if err != nil {
		log.Error("Failed to conver to kv pair to string")
		return Result, err
	}
	Decode64, _ := base64.StdEncoding.DecodeString(Str)
	Result[POOL], err = ToByte(Decode64)
	if err != nil {
		log.Error("Failed to convert resource pool to byte")
		return Result, err
	}

	return Result, err
}

func (PONRMgr *PONResourceManager) GetPath(IntfID uint32, ResourceType string) string {
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
		log.Error("Invalid resource pool identifier")
	}
	return Path
}

func (PONRMgr *PONResourceManager) GetResourceID(IntfID uint32, ResourceType string, NumIDs uint32) ([]uint32, error) {
	/*
	   Create alloc/gemport/onu/flow id for given OLT PON interface.
	   :param pon_intf_id: OLT PON interface id
	   :param resource_type: String to identify type of resource
	   :param num_of_id: required number of ids
	   :return list/uint32/None: list, uint32 or None if resource type is
	    alloc_id/gemport_id, onu_id or invalid type respectively
	*/
	if NumIDs < 1 {
		log.Error("Invalid number of resources requested")
		return nil, errors.New(fmt.Sprintf("Invalid number of resources requested %d", NumIDs))
	}
	// delegate to the master instance if sharing enabled across instances

	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.GetResourceID(IntfID, ResourceType, NumIDs)
	}
	log.Debugf("Fetching resource from %s rsrc mgr for resource %s", PONRMgr.Globalorlocal, ResourceType)

	Path := PONRMgr.GetPath(IntfID, ResourceType)
	if Path == "" {
		log.Errorf("Failed to get path for resource type %s", ResourceType)
		return nil, errors.New(fmt.Sprintf("Failed to get path for resource type %s", ResourceType))
	}
	log.Debugf("Get resource for type %s on path %s", ResourceType, Path)
	var Result []uint32
	var NextID uint32
	Resource, err := PONRMgr.GetResource(Path)
	if (err == nil) && (ResourceType == ONU_ID) || (ResourceType == FLOW_ID) {
		if NextID, err = PONRMgr.GenerateNextID(Resource); err != nil {
			log.Error("Failed to Generate ID")
			return Result, err
		}
		Result = append(Result, NextID)
	} else if (err == nil) && ((ResourceType == GEMPORT_ID) || (ResourceType == ALLOC_ID)) {
		if NumIDs == 1 {
			if NextID, err = PONRMgr.GenerateNextID(Resource); err != nil {
				log.Error("Failed to Generate ID")
				return Result, err
			}
			Result = append(Result, NextID)
		} else {
			for NumIDs > 0 {
				if NextID, err = PONRMgr.GenerateNextID(Resource); err != nil {
					log.Error("Failed to Generate ID")
					return Result, err
				}
				Result = append(Result, NextID)
				NumIDs--
			}
		}
	} else {
		log.Error("get resource failed")
		return Result, err
	}

	//Update resource in kv store
	if PONRMgr.UpdateResource(Path, Resource) != nil {
		log.Errorf("Failed to update resource %s", Path)
		return nil, errors.New(fmt.Sprintf("Failed to update resource %s", Path))
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

func (PONRMgr *PONResourceManager) FreeResourceID(IntfID uint32, ResourceType string, ReleaseContent []uint32) bool {
	/*
	   Release alloc/gemport/onu/flow id for given OLT PON interface.
	   :param pon_intf_id: OLT PON interface id
	   :param resource_type: String to identify type of resource
	   :param release_content: required number of ids
	   :return boolean: True if all IDs in given release_content release else False
	*/
	if checkValidResourceType(ResourceType) == false {
		log.Error("Invalid resource type")
		return false
	}
	if ReleaseContent == nil {
		log.Debug("Nothing to release")
		return true
	}
	// delegate to the master instance if sharing enabled across instances
	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.FreeResourceID(IntfID, ResourceType, ReleaseContent)
	}
	Path := PONRMgr.GetPath(IntfID, ResourceType)
	if Path == "" {
		log.Error("Failed to get path")
		return false
	}
	Resource, err := PONRMgr.GetResource(Path)
	if err != nil {
		log.Error("Failed to get resource")
		return false
	}
	for _, Val := range ReleaseContent {
		PONRMgr.ReleaseID(Resource, Val)
	}
	if PONRMgr.UpdateResource(Path, Resource) != nil {
		log.Errorf("Free resource for %s failed", Path)
		return false
	}
	return true
}

func (PONRMgr *PONResourceManager) UpdateResource(Path string, Resource map[string]interface{}) error {
	/*
	   Update resource in resource kv store.
	   :param path: path to update resource
	   :param resource: resource need to be updated
	   :return boolean: True if resource updated in kv store else False
	*/
	// TODO resource[POOL] = resource[POOL].bin
	Value, err := json.Marshal(Resource)
	if err != nil {
		log.Error("failed to Marshal")
		return err
	}
	err = PONRMgr.KVStore.Put(Path, Value)
	if err != nil {
		log.Error("failed to put data to kv store %s", Path)
		return err
	}
	return nil
}

func (PONRMgr *PONResourceManager) ClearResourceIDPool(IntfID uint32, ResourceType string) bool {
	/*
	   Clear Resource Pool for a given Resource Type on a given PON Port.
	   :return boolean: True if removed else False
	*/

	// delegate to the master instance if sharing enabled across instances
	SharedResourceMgr := PONRMgr.SharedResourceMgrs[PONRMgr.SharedIdxByType[ResourceType]]
	if SharedResourceMgr != nil && PONRMgr != SharedResourceMgr {
		return SharedResourceMgr.ClearResourceIDPool(IntfID, ResourceType)
	}
	Path := PONRMgr.GetPath(IntfID, ResourceType)
	if Path == "" {
		log.Error("Failed to get path")
		return false
	}

	if err := PONRMgr.KVStore.Delete(Path); err != nil {
		log.Errorf("Failed to delete resource %s", Path)
		return false
	}
	log.Debugf("Cleared resource %s", Path)
	return true
}

func (PONRMgr PONResourceManager) InitResourceMap(PONIntfONUID string) {
	/*
	   Initialize resource map
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	*/
	// initialize pon_intf_onu_id tuple to alloc_ids map
	AllocIDPath := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	var AllocIDs []byte
	Result := PONRMgr.KVStore.Put(AllocIDPath, AllocIDs)
	if Result != nil {
		log.Error("Failed to update the KV store")
		return
	}
	// initialize pon_intf_onu_id tuple to gemport_ids map
	GEMPortIDPath := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	var GEMPortIDs []byte
	Result = PONRMgr.KVStore.Put(GEMPortIDPath, GEMPortIDs)
	if Result != nil {
		log.Error("Failed to update the KV store")
		return
	}
}

func (PONRMgr PONResourceManager) RemoveResourceMap(PONIntfONUID string) bool {
	/*
	   Remove resource map
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	*/
	// remove pon_intf_onu_id tuple to alloc_ids map
	var err error
	AllocIDPath := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	if err = PONRMgr.KVStore.Delete(AllocIDPath); err != nil {
		log.Errorf("Failed to remove resource %s", AllocIDPath)
		return false
	}
	// remove pon_intf_onu_id tuple to gemport_ids map
	GEMPortIDPath := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	err = PONRMgr.KVStore.Delete(GEMPortIDPath)
	if err != nil {
		log.Errorf("Failed to remove resource %s", GEMPortIDPath)
		return false
	}

	FlowIDPath := fmt.Sprintf(FLOW_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, PONIntfONUID)
	if FlowIDs, err := PONRMgr.KVStore.List(FlowIDPath); err != nil {
		for _, Flow := range FlowIDs {
			FlowIDInfoPath := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, PONIntfONUID, Flow.Value)
			if err = PONRMgr.KVStore.Delete(FlowIDInfoPath); err != nil {
				log.Errorf("Failed to remove resource %s", FlowIDInfoPath)
				return false
			}
		}
	}

	if err = PONRMgr.KVStore.Delete(FlowIDPath); err != nil {
		log.Errorf("Failed to remove resource %s", FlowIDPath)
		return false
	}

	return true
}

func (PONRMgr *PONResourceManager) GetCurrentAllocIDForOnu(IntfONUID string) []uint32 {
	/*
	   Get currently configured alloc ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :return list: List of alloc_ids if available, else None
	*/
	Path := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)

	var Data []uint32
	Value, err := PONRMgr.KVStore.Get(Path)
	if err == nil {
		if Value != nil {
			Val, err := ToByte(Value.Value)
			if err != nil {
				log.Errorw("Failed to convert into byte array", log.Fields{"error": err})
				return Data
			}
			if err = json.Unmarshal(Val, &Data); err != nil {
				log.Error("Failed to unmarshal", log.Fields{"error": err})
				return Data
			}
		}
	}
	return Data
}

func (PONRMgr *PONResourceManager) GetCurrentGEMPortIDsForOnu(IntfONUID string) []uint32 {
	/*
	   Get currently configured gemport ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :return list: List of gemport IDs if available, else None
	*/

	Path := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	log.Debugf("Getting current gemports for %s", Path)
	var Data []uint32
	Value, err := PONRMgr.KVStore.Get(Path)
	if err == nil {
		if Value != nil {
			Val, _ := ToByte(Value.Value)
			if err = json.Unmarshal(Val, &Data); err != nil {
				log.Errorw("Failed to unmarshal", log.Fields{"error": err})
				return Data
			}
		}
	} else {
		log.Errorf("Failed to get data from kvstore for %s", Path)
	}
	return Data
}

func (PONRMgr *PONResourceManager) GetCurrentFlowIDsForOnu(IntfONUID string) []uint32 {
	/*
	   Get currently configured flow ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :return list: List of Flow IDs if available, else None
	*/

	Path := fmt.Sprintf(FLOW_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)

	var Data []uint32
	Value, err := PONRMgr.KVStore.Get(Path)
	if err == nil {
		if Value != nil {
			Val, _ := ToByte(Value.Value)
			if err = json.Unmarshal(Val, &Data); err != nil {
				log.Error("Failed to unmarshal")
				return Data
			}
		}
	}
	return Data
}

func (PONRMgr *PONResourceManager) GetFlowIDInfo(IntfONUID string, FlowID uint32, Data interface{}) error {
	/*
	   Get flow details configured for the ONU.
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param flow_id: Flow Id reference
	   :param Data: Result
	   :return error: nil if no error in getting from KV store
	*/

	Path := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, IntfONUID, FlowID)

	Value, err := PONRMgr.KVStore.Get(Path)
	if err == nil {
		if Value != nil {
			Val, err := ToByte(Value.Value)
			if err != nil {
				log.Errorw("Failed to convert flowinfo into byte array", log.Fields{"error": err})
				return err
			}
			if err = json.Unmarshal(Val, Data); err != nil {
				log.Errorw("Failed to unmarshal", log.Fields{"error": err})
				return err
			}
		}
	}
	return err
}

func (PONRMgr *PONResourceManager) RemoveFlowIDInfo(IntfONUID string, FlowID uint32) bool {
	/*
	   Get flow_id details configured for the ONU.
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param flow_id: Flow Id reference
	*/
	Path := fmt.Sprintf(FLOW_ID_INFO_PATH, PONRMgr.DeviceID, IntfONUID, FlowID)

	if err := PONRMgr.KVStore.Delete(Path); err != nil {
		log.Errorf("Falied to remove resource %s", Path)
		return false
	}
	return true
}

func (PONRMgr *PONResourceManager) UpdateAllocIdsForOnu(IntfONUID string, AllocIDs []uint32) error {
	/*
	   Update currently configured alloc ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param alloc_ids: list of alloc ids
	*/
	var Value []byte
	var err error
	Path := fmt.Sprintf(ALLOC_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	Value, err = json.Marshal(AllocIDs)
	if err != nil {
		log.Error("failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(Path, Value); err != nil {
		log.Errorf("Failed to update resource %s", Path)
		return err
	}
	return err
}

func (PONRMgr *PONResourceManager) UpdateGEMPortIDsForOnu(IntfONUID string, GEMPortIDs []uint32) error {
	/*
	   Update currently configured gemport ids for given pon_intf_onu_id
	   :param pon_intf_onu_id: reference of PON interface id and onu id
	   :param gemport_ids: list of gem port ids
	*/

	var Value []byte
	var err error
	Path := fmt.Sprintf(GEMPORT_ID_RESOURCE_MAP_PATH, PONRMgr.DeviceID, IntfONUID)
	log.Debugf("Updating gemport ids for %s", Path)
	Value, err = json.Marshal(GEMPortIDs)
	if err != nil {
		log.Error("failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(Path, Value); err != nil {
		log.Errorf("Failed to update resource %s", Path)
		return err
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

	for idx, _ := range FlowIDList {
		if FlowID == FlowIDList[idx] {
			return true, uint32(idx)
		}
	}
	return false, 0
}

func (PONRMgr *PONResourceManager) UpdateFlowIDForOnu(IntfONUID string, FlowID uint32, Add bool) error {
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
	FlowIDs := PONRMgr.GetCurrentFlowIDsForOnu(IntfONUID)

	if Add {
		if RetVal, IDx = checkForFlowIDInList(FlowIDs, FlowID); RetVal == true {
			return err
		}
		FlowIDs = append(FlowIDs, FlowID)
	} else {
		if RetVal, IDx = checkForFlowIDInList(FlowIDs, FlowID); RetVal == false {
			return err
		}
		// delete the index and shift
		FlowIDs = append(FlowIDs[:IDx], FlowIDs[IDx+1:]...)
	}
	Value, err = json.Marshal(FlowIDs)
	if err != nil {
		log.Error("Failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(Path, Value); err != nil {
		log.Errorf("Failed to update resource %s", Path)
		return err
	}
	return err
}

func (PONRMgr *PONResourceManager) UpdateFlowIDInfoForOnu(IntfONUID string, FlowID uint32, FlowData interface{}) error {
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
		log.Error("failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(Path, Value); err != nil {
		log.Errorf("Failed to update resource %s", Path)
		return err
	}
	return err
}

func (PONRMgr *PONResourceManager) GenerateNextID(Resource map[string]interface{}) (uint32, error) {
	/*
	   Generate unique id having OFFSET as start
	   :param resource: resource used to generate ID
	   :return uint32: generated id
	*/
	ByteArray, err := ToByte(Resource[POOL])
	if err != nil {
		log.Error("Failed to convert resource to byte array")
		return 0, err
	}
	Data := bitmap.TSFromData(ByteArray, false)
	if Data == nil {
		log.Error("Failed to get data from byte array")
		return 0, errors.New("Failed to get data from byte array")
	}

	Len := Data.Len()
	var Idx int
	for Idx = 0; Idx < Len; Idx++ {
		Val := Data.Get(Idx)
		if Val == false {
			break
		}
	}
	Data.Set(Idx, true)
	res := uint32(Resource[START_IDX].(float64))
	Resource[POOL] = Data.Data(false)
	log.Debugf("Generated ID for %d", (uint32(Idx) + res))
	return (uint32(Idx) + res), err
}

func (PONRMgr *PONResourceManager) ReleaseID(Resource map[string]interface{}, Id uint32) bool {
	/*
	   Release unique id having OFFSET as start index.
	   :param resource: resource used to release ID
	   :param unique_id: id need to be released
	*/
	ByteArray, err := ToByte(Resource[POOL])
	if err != nil {
		log.Error("Failed to convert resource to byte array")
		return false
	}
	Data := bitmap.TSFromData(ByteArray, false)
	if Data == nil {
		log.Error("Failed to get resource pool")
		return false
	}
	var Idx uint32
	Idx = Id - uint32(Resource[START_IDX].(float64))
	Data.Set(int(Idx), false)
	Resource[POOL] = Data.Data(false)

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

func (PONRMgr *PONResourceManager) AddOnuGemInfo(intfID uint32, onuGemData interface{}) error {
	/*
	   Update onugem info map,
	   :param pon_intf_id: reference of PON interface id
	   :param onuegmdata: onugem info map
	*/
	var Value []byte
	var err error
	Path := fmt.Sprintf(ONU_GEM_INFO_PATH, PONRMgr.DeviceID, intfID)
	Value, err = json.Marshal(onuGemData)
	if err != nil {
		log.Error("failed to Marshal")
		return err
	}

	if err = PONRMgr.KVStore.Put(Path, Value); err != nil {
		log.Errorf("Failed to update resource %s", Path)
		return err
	}
	return err
}

func (PONRMgr *PONResourceManager) GetOnuGemInfo(IntfId uint32, onuGemInfo interface{}) error {
	/*
	  Get onugeminfo map from kvstore
	  :param intfid: refremce pon intfid
	  :param onuGemInfo: onugem info to return from kv strore.
	*/
	var Val []byte

	path := fmt.Sprintf(ONU_GEM_INFO_PATH, PONRMgr.DeviceID, IntfId)
	value, err := PONRMgr.KVStore.Get(path)
	if err != nil {
		log.Errorw("Failed to get from kv store", log.Fields{"path": path})
		return err
	} else if value == nil {
		log.Debug("No onuinfo for path", log.Fields{"path": path})
		return nil // returning nil as this could happen if there are no onus for the interface yet
	}
	if Val, err = kvstore.ToByte(value.Value); err != nil {
		log.Error("Failed to convert to byte array")
		return err
	}

	if err = json.Unmarshal(Val, &onuGemInfo); err != nil {
		log.Error("Failed to unmarshall")
		return err
	}
	log.Debugw("found onuinfo from path", log.Fields{"path": path, "onuinfo": onuGemInfo})
	return err
}

func (PONRMgr *PONResourceManager) DelOnuGemInfoForIntf(intfId uint32) error {
	/*
	   delete onugem info for an interface from kvstore
	   :param intfid: refremce pon intfid
	*/

	path := fmt.Sprintf(ONU_GEM_INFO_PATH, PONRMgr.DeviceID, intfId)
	if err := PONRMgr.KVStore.Delete(path); err != nil {
		log.Errorf("Falied to remove resource %s", path)
		return err
	}
	return nil
}
