/*
 * Copyright 2021-2024 Open Networking Foundation (ONF) and the ONF Contributors

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

package adapter

import (
	"context"
	"fmt"
	"sync"

	"github.com/buraksezer/consistent"
	"github.com/cespare/xxhash"

	// nolint:staticcheck
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v7/pkg/db"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// All the values below can be tuned to get optimal data distribution.  The numbers below seems to work well when
	// supporting 1000-10000 devices and 1 - 20 replicas of an adapter

	// Keys are distributed among partitions. Prime numbers are good to distribute keys uniformly.
	DefaultPartitionCount = 1117

	// Represents how many times a node is replicated on the consistent ring.
	DefaultReplicationFactor = 117

	// Load is used to calculate average load.
	DefaultLoad = 1.1
)

type Endpoint string // The gRPC endpoint of an adapter instance
type ReplicaID int32 // The replication ID of an adapter instance

type EndpointManager interface {

	// Registers an adapter
	RegisterAdapter(ctx context.Context, adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) error

	// GetEndpoint is called to get the endpoint to communicate with for a specific device and device type.
	GetEndpoint(ctx context.Context, deviceID string, deviceType string) (Endpoint, error)

	// IsDeviceOwnedByAdapter is invoked when a specific adapter (adapter type + replicaNumber) is restarted and
	// devices owned by that adapter need to be reconciled
	IsDeviceOwnedByAdapter(ctx context.Context, deviceID string, adapterType string, replicaNumber int32) (bool, error)

	// GetReplicaAssignment returns the replica number of the adapter that owns the deviceID.  This is used by the
	// test only
	GetReplicaAssignment(ctx context.Context, deviceID string, adapterType string) (ReplicaID, error)
}

type adapterService struct {
	replicas       map[ReplicaID]Endpoint
	consistentRing *consistent.Consistent
	adapterType    string // Type of the adapter.  The same type applies for all replicas of that adapter
	totalReplicas  int32
}

type endpointManager struct {
	backend                           *db.Backend
	adapterServices                   map[string]*adapterService
	deviceTypeToAdapterServiceMap     map[string]string
	partitionCount                    int
	replicationFactor                 int
	load                              float64
	adapterServicesLock               sync.RWMutex
	deviceTypeToAdapterServiceMapLock sync.RWMutex
}

type EndpointManagerOption func(*endpointManager)

func PartitionCount(count int) EndpointManagerOption {
	return func(args *endpointManager) {
		args.partitionCount = count
	}
}

func ReplicationFactor(replicas int) EndpointManagerOption {
	return func(args *endpointManager) {
		args.replicationFactor = replicas
	}
}

func Load(load float64) EndpointManagerOption {
	return func(args *endpointManager) {
		args.load = load
	}
}

func newEndpointManager(backend *db.Backend, opts ...EndpointManagerOption) EndpointManager {
	tm := &endpointManager{
		partitionCount:                DefaultPartitionCount,
		replicationFactor:             DefaultReplicationFactor,
		load:                          DefaultLoad,
		backend:                       backend,
		adapterServices:               make(map[string]*adapterService),
		deviceTypeToAdapterServiceMap: make(map[string]string),
	}

	for _, option := range opts {
		option(tm)
	}
	return tm
}

func NewEndpointManager(backend *db.Backend, opts ...EndpointManagerOption) EndpointManager {
	return newEndpointManager(backend, opts...)
}

func (ep *endpointManager) GetEndpoint(ctx context.Context, deviceID string, deviceType string) (Endpoint, error) {
	logger.Debugw(ctx, "getting-endpoint", log.Fields{"device-id": deviceID, "device-type": deviceType})
	owner, err := ep.getOwnerByDeviceType(ctx, deviceID, deviceType)
	if err != nil {
		return "", err
	}
	m, ok := owner.(Member)
	if !ok {
		return "", status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	endpoint := m.getEndPoint()
	if endpoint == "" {
		return "", status.Errorf(codes.Unavailable, "endpoint-not-set-%s", deviceType)
	}
	logger.Debugw(ctx, "returning-endpoint", log.Fields{"device-id": deviceID, "device-type": deviceType, "endpoint": endpoint})
	return endpoint, nil
}

func (ep *endpointManager) IsDeviceOwnedByAdapter(ctx context.Context, deviceID string, adapterType string, replicaNumber int32) (bool, error) {
	logger.Debugw(ctx, "device-ownership", log.Fields{"device-id": deviceID, "adapter-type": adapterType, "replica-number": replicaNumber})

	serv, err := ep.getOwnerByAdapterType(ctx, deviceID, adapterType)
	if err != nil {
		return false, err
	}
	m, ok := serv.(Member)
	if !ok {
		return false, status.Errorf(codes.Aborted, "invalid-member-%v", serv)
	}
	return m.getReplica() == ReplicaID(replicaNumber), nil
}

func (ep *endpointManager) GetReplicaAssignment(ctx context.Context, deviceID string, adapterType string) (ReplicaID, error) {
	owner, err := ep.getOwnerByAdapterType(ctx, deviceID, adapterType)
	if err != nil {
		return 0, nil
	}
	m, ok := owner.(Member)
	if !ok {
		return 0, status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	return m.getReplica(), nil
}

func (ep *endpointManager) getOwnerByDeviceType(ctx context.Context, deviceID string, deviceType string) (consistent.Member, error) {
	serv, err := ep.getAdapterService(ctx, deviceType)
	if err != nil {
		return nil, err
	}
	key := ep.makeKey(deviceID, deviceType, serv.adapterType)
	return serv.consistentRing.LocateKey(key), nil
}

func (ep *endpointManager) getOwnerByAdapterType(ctx context.Context, deviceID string, adapterType string) (consistent.Member, error) {
	// Check whether the adapter exist
	ep.adapterServicesLock.RLock()
	serv, adapterExist := ep.adapterServices[adapterType]
	ep.adapterServicesLock.RUnlock()

	if !adapterExist {
		// Sync from the dB
		if err := ep.loadAdapterServices(ctx); err != nil {
			return nil, err
		}
		// Check again
		ep.adapterServicesLock.RLock()
		serv, adapterExist = ep.adapterServices[adapterType]
		ep.adapterServicesLock.RUnlock()
		if !adapterExist {
			return nil, fmt.Errorf("adapter-type-not-exist-%s", adapterType)
		}
	}

	// Get the device type
	deviceType := ""
	ep.deviceTypeToAdapterServiceMapLock.RLock()
	for dType, aType := range ep.deviceTypeToAdapterServiceMap {
		if aType == adapterType {
			deviceType = dType
			break
		}
	}
	ep.deviceTypeToAdapterServiceMapLock.RUnlock()

	if deviceType == "" {
		return nil, fmt.Errorf("device-type-not-exist-for-adapter-type-%s", adapterType)
	}

	owner := serv.consistentRing.LocateKey(ep.makeKey(deviceID, deviceType, serv.adapterType))
	m, ok := owner.(Member)
	if !ok {
		return nil, status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	return m, nil
}

func (ep *endpointManager) getAdapterService(ctx context.Context, deviceType string) (*adapterService, error) {
	// First get the adapter type for that device type
	adapterType := ""
	ep.deviceTypeToAdapterServiceMapLock.RLock()
	for dType, aType := range ep.deviceTypeToAdapterServiceMap {
		if dType == deviceType {
			adapterType = aType
			break
		}
	}
	ep.deviceTypeToAdapterServiceMapLock.RUnlock()

	// Check whether the adapter exist
	adapterExist := false
	var aServ *adapterService
	if adapterType != "" {
		ep.adapterServicesLock.RLock()
		aServ, adapterExist = ep.adapterServices[adapterType]
		ep.adapterServicesLock.RUnlock()
	}

	// Load the service and device types if not found, i.e. sync up with the dB
	if !adapterExist || aServ == nil || int(aServ.totalReplicas) != len(aServ.consistentRing.GetMembers()) {
		if err := ep.loadAdapterServices(ctx); err != nil {
			return nil, err
		}

		// Get the adapter type if it was empty before
		if adapterType == "" {
			ep.deviceTypeToAdapterServiceMapLock.RLock()
			for dType, aType := range ep.deviceTypeToAdapterServiceMap {
				if dType == deviceType {
					adapterType = aType
					break
				}
			}
			ep.deviceTypeToAdapterServiceMapLock.RUnlock()
		}
		// Error put if the adapter type is not set
		if adapterType == "" {
			return nil, fmt.Errorf("adapter-service-not-found-for-device-type-%s", deviceType)
		}

		// Get the service
		ep.adapterServicesLock.RLock()
		aServ, adapterExist = ep.adapterServices[adapterType]
		ep.adapterServicesLock.RUnlock()
	}

	// Sanity check
	if !adapterExist || aServ == nil || int(aServ.totalReplicas) != len(aServ.consistentRing.GetMembers()) {
		return nil, fmt.Errorf("adapter-service-not-found-for-device-type-%s", deviceType)
	}

	return aServ, nil
}

func (ep *endpointManager) getConsistentConfig() consistent.Config {
	return consistent.Config{
		PartitionCount:    ep.partitionCount,
		ReplicationFactor: ep.replicationFactor,
		Load:              ep.load,
		Hasher:            hasher{},
	}
}

// loadAdapterServices loads the services (adapters) and device types in memory. Because of the small size of the data and
// the data format in the dB being binary protobuf then it is better to load all the data if inconsistency is detected,
// instead of watching for updates in the dB and acting on it.
func (ep *endpointManager) loadAdapterServices(ctx context.Context) error {
	ep.adapterServicesLock.Lock()
	defer ep.adapterServicesLock.Unlock()
	ep.deviceTypeToAdapterServiceMapLock.Lock()
	defer ep.deviceTypeToAdapterServiceMapLock.Unlock()

	if ep.backend == nil {
		return status.Error(codes.Aborted, "backend-not-set")
	}

	ep.adapterServices = make(map[string]*adapterService)
	ep.deviceTypeToAdapterServiceMap = make(map[string]string)

	// Load the adapters
	blobs, err := ep.backend.List(log.WithSpanFromContext(context.Background(), ctx), "adapters")
	if err != nil {
		return err
	}

	// Data is marshaled as proto bytes in the data store
	for _, blob := range blobs {
		data := blob.Value.([]byte)
		adapter := &voltha.Adapter{}
		if err = proto.Unmarshal(data, adapter); err != nil {
			return err
		}
		// A valid adapter should have the vendorID set
		if err = ep.setupAdapterWithLock(ctx, adapter); err != nil {
			logger.Errorw(ctx, "missing vendor id", log.Fields{"adapter": adapter})
		}
	}
	// Load the device types
	blobs, err = ep.backend.List(log.WithSpanFromContext(context.Background(), ctx), "device_types")
	if err != nil {
		return err
	}
	for _, blob := range blobs {
		data := blob.Value.([]byte)
		deviceType := &voltha.DeviceType{}
		if err = proto.Unmarshal(data, deviceType); err != nil {
			return err
		}
		ep.addDeviceTypeWithLock(deviceType)
	}

	ep.printServices(ctx)
	return nil
}

func (ep *endpointManager) printServices(ctx context.Context) {
	if logger.V(log.DebugLevel) {
		for key, val := range ep.adapterServices {
			members := val.consistentRing.GetMembers()
			logger.Debugw(ctx, "adapter-service", log.Fields{"service": key, "expected-replica": val.totalReplicas, "replicas": len(val.consistentRing.GetMembers())})
			for _, m := range members {
				n := m.(Member)
				logger.Debugw(ctx, "adapter-instance-registered", log.Fields{"service-id": n.getID(), "adapter-type": n.getAdapterType(), "replica": n.getReplica(), "endpoint": n.getEndPoint()})
			}
		}
		logger.Debugw(ctx, "device-types", log.Fields{"device-types": ep.deviceTypeToAdapterServiceMap})
	}
}

func (ep *endpointManager) RegisterAdapter(ctx context.Context, adapter *voltha.Adapter, deviceTypes *voltha.DeviceTypes) error {
	ep.adapterServicesLock.Lock()
	defer ep.adapterServicesLock.Unlock()
	ep.deviceTypeToAdapterServiceMapLock.Lock()
	defer ep.deviceTypeToAdapterServiceMapLock.Unlock()

	if err := ep.setupAdapterWithLock(ctx, adapter); err != nil {
		return err
	}
	ep.addDeviceTypesWithLock(deviceTypes)
	ep.printServices(ctx)
	return nil
}

func (ep *endpointManager) setupAdapterWithLock(ctx context.Context, adapter *voltha.Adapter) error {
	// Build the consistent ring for that adapter
	if adapter.Vendor != "" {
		if _, ok := ep.adapterServices[adapter.Type]; !ok {
			ep.adapterServices[adapter.Type] = &adapterService{
				adapterType:    adapter.Type,
				totalReplicas:  adapter.TotalReplicas,
				replicas:       make(map[ReplicaID]Endpoint),
				consistentRing: consistent.New(nil, ep.getConsistentConfig()),
			}

		}
		currentReplica := ReplicaID(adapter.CurrentReplica)
		endpoint := Endpoint(adapter.Endpoint)
		ep.adapterServices[adapter.Type].replicas[currentReplica] = endpoint
		ep.adapterServices[adapter.Type].consistentRing.Add(newMember(adapter.Id, adapter.Type, adapter.Vendor, endpoint, adapter.Version, currentReplica))
	} else {
		logger.Errorw(ctx, "missing-vendor-id", log.Fields{"adapter": adapter})
		return fmt.Errorf("missing vendor id for %s adapter", adapter.Id)
	}
	return nil
}

func (ep *endpointManager) addDeviceTypesWithLock(deviceTypes *voltha.DeviceTypes) {
	// Update the device types
	for _, deviceType := range deviceTypes.Items {
		if _, ok := ep.deviceTypeToAdapterServiceMap[deviceType.Id]; !ok {
			ep.deviceTypeToAdapterServiceMap[deviceType.Id] = deviceType.AdapterType
		}
	}
}

func (ep *endpointManager) addDeviceTypeWithLock(deviceType *voltha.DeviceType) {
	if _, ok := ep.deviceTypeToAdapterServiceMap[deviceType.Id]; !ok {
		ep.deviceTypeToAdapterServiceMap[deviceType.Id] = deviceType.AdapterType
	}
}

// makeKey creates the string that the hash function uses to create the hash
// In most cases, a deviceType is the same as a serviceType.  It is being differentiated here to allow a
// serviceType to support multiple device types
func (ep *endpointManager) makeKey(deviceID string, deviceType string, serviceType string) []byte {
	return []byte(fmt.Sprintf("%s_%s_%s", serviceType, deviceType, deviceID))
}

// The consistent package requires a hasher function
type hasher struct{}

// Sum64 provides the hasher function.  Based upon numerous testing scenarios, the xxhash package seems to provide the
// best distribution compare to other hash packages
func (h hasher) Sum64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// Member represents a member on the consistent ring
type Member interface {
	String() string
	getReplica() ReplicaID
	getEndPoint() Endpoint
	getID() string
	getAdapterType() string
}

// member implements the Member interface
type member struct {
	id          string
	adapterType string
	vendor      string
	version     string
	endpoint    Endpoint
	replica     ReplicaID
}

func newMember(id string, adapterType string, vendor string, endPoint Endpoint, version string, replica ReplicaID) Member {
	return &member{
		id:          id,
		adapterType: adapterType,
		vendor:      vendor,
		version:     version,
		replica:     replica,
		endpoint:    endPoint,
	}
}

func (m *member) String() string {
	return string(m.endpoint)
}

func (m *member) getReplica() ReplicaID {
	return m.replica
}

func (m *member) getEndPoint() Endpoint {
	return m.endpoint
}

func (m *member) getID() string {
	return m.id
}

func (m *member) getAdapterType() string {
	return m.adapterType
}
