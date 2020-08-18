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
package kafka

import (
	"context"
	"fmt"
	"github.com/buraksezer/consistent"
	"github.com/cespare/xxhash"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/db"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

const (
	// All the values below can be tuned to get optimal data distribution.  The numbers below seems to work well when
	// supporting 1000-10000 devices and 1 - 20 replicas of a service

	// Keys are distributed among partitions. Prime numbers are good to distribute keys uniformly.
	DefaultPartitionCount = 1117

	// Represents how many times a node is replicated on the consistent ring.
	DefaultReplicationFactor = 117

	// Load is used to calculate average load.
	DefaultLoad = 1.1
)

type Endpoint string // Endpoint of a service instance.  When using kafka, this is the topic name of a service
type ReplicaID int32 // The replication ID of a service instance

type EndpointManager interface {

	// GetEndpoint is called to get the endpoint to communicate with for a specific device and service type.  For
	// now this will return the topic name
	GetEndpoint(ctx context.Context, deviceID string, serviceType string) (Endpoint, error)

	// IsDeviceOwnedByService is invoked when a specific service (service type + replicaNumber) is restarted and
	// devices owned by that service need to be reconciled
	IsDeviceOwnedByService(ctx context.Context, deviceID string, serviceType string, replicaNumber int32) (bool, error)

	// GetReplicaAssignment returns the replica number of the service that owns the deviceID.  This is used by the
	// test only
	GetReplicaAssignment(ctx context.Context, deviceID string, serviceType string) (ReplicaID, error)
}

type service struct {
	id             string // Id of the service.  The same id is used for all replicas
	totalReplicas  int32
	replicas       map[ReplicaID]Endpoint
	consistentRing *consistent.Consistent
}

type endpointManager struct {
	partitionCount           int
	replicationFactor        int
	load                     float64
	backend                  *db.Backend
	services                 map[string]*service
	servicesLock             sync.RWMutex
	deviceTypeServiceMap     map[string]string
	deviceTypeServiceMapLock sync.RWMutex
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
		partitionCount:       DefaultPartitionCount,
		replicationFactor:    DefaultReplicationFactor,
		load:                 DefaultLoad,
		backend:              backend,
		services:             make(map[string]*service),
		deviceTypeServiceMap: make(map[string]string),
	}

	for _, option := range opts {
		option(tm)
	}
	return tm
}

func NewEndpointManager(backend *db.Backend, opts ...EndpointManagerOption) EndpointManager {
	return newEndpointManager(backend, opts...)
}

func (ep *endpointManager) GetEndpoint(ctx context.Context, deviceID string, serviceType string) (Endpoint, error) {
	logger.Debugw(ctx, "getting-endpoint", log.Fields{"device-id": deviceID, "service": serviceType})
	owner, err := ep.getOwner(ctx, deviceID, serviceType)
	if err != nil {
		return "", err
	}
	m, ok := owner.(Member)
	if !ok {
		return "", status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	endpoint := m.getEndPoint()
	if endpoint == "" {
		return "", status.Errorf(codes.Unavailable, "endpoint-not-set-%s", serviceType)
	}
	logger.Debugw(ctx, "returning-endpoint", log.Fields{"device-id": deviceID, "service": serviceType, "endpoint": endpoint})
	return endpoint, nil
}

func (ep *endpointManager) IsDeviceOwnedByService(ctx context.Context, deviceID string, serviceType string, replicaNumber int32) (bool, error) {
	logger.Debugw(ctx, "device-ownership", log.Fields{"device-id": deviceID, "service": serviceType, "replica-number": replicaNumber})
	owner, err := ep.getOwner(ctx, deviceID, serviceType)
	if err != nil {
		return false, nil
	}
	m, ok := owner.(Member)
	if !ok {
		return false, status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	return m.getReplica() == ReplicaID(replicaNumber), nil
}

func (ep *endpointManager) GetReplicaAssignment(ctx context.Context, deviceID string, serviceType string) (ReplicaID, error) {
	owner, err := ep.getOwner(ctx, deviceID, serviceType)
	if err != nil {
		return 0, nil
	}
	m, ok := owner.(Member)
	if !ok {
		return 0, status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	return m.getReplica(), nil
}

func (ep *endpointManager) getOwner(ctx context.Context, deviceID string, serviceType string) (consistent.Member, error) {
	serv, dType, err := ep.getServiceAndDeviceType(ctx, serviceType)
	if err != nil {
		return nil, err
	}
	key := ep.makeKey(deviceID, dType, serviceType)
	return serv.consistentRing.LocateKey(key), nil
}

func (ep *endpointManager) getServiceAndDeviceType(ctx context.Context, serviceType string) (*service, string, error) {
	// Check whether service exist
	ep.servicesLock.RLock()
	serv, serviceExist := ep.services[serviceType]
	ep.servicesLock.RUnlock()

	// Load the service and device types if needed
	if !serviceExist || serv == nil || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
		if err := ep.loadServices(ctx); err != nil {
			return nil, "", err
		}

		// Check whether the service exists now
		ep.servicesLock.RLock()
		serv, serviceExist = ep.services[serviceType]
		ep.servicesLock.RUnlock()
		if !serviceExist || serv == nil || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
			return nil, "", status.Errorf(codes.NotFound, "service-%s", serviceType)
		}
	}

	ep.deviceTypeServiceMapLock.RLock()
	defer ep.deviceTypeServiceMapLock.RUnlock()
	for dType, sType := range ep.deviceTypeServiceMap {
		if sType == serviceType {
			return serv, dType, nil
		}
	}
	return nil, "", status.Errorf(codes.NotFound, "service-%s", serviceType)
}

func (ep *endpointManager) getConsistentConfig() consistent.Config {
	return consistent.Config{
		PartitionCount:    ep.partitionCount,
		ReplicationFactor: ep.replicationFactor,
		Load:              ep.load,
		Hasher:            hasher{},
	}
}

// loadServices loads the services (adapters) and device types in memory. Because of the small size of the data and
// the data format in the dB being binary protobuf then it is better to load all the data if inconsistency is detected,
// instead of watching for updates in the dB and acting on it.
func (ep *endpointManager) loadServices(ctx context.Context) error {
	ep.servicesLock.Lock()
	defer ep.servicesLock.Unlock()
	ep.deviceTypeServiceMapLock.Lock()
	defer ep.deviceTypeServiceMapLock.Unlock()

	if ep.backend == nil {
		return status.Error(codes.Aborted, "backend-not-set")
	}
	ep.services = make(map[string]*service)
	ep.deviceTypeServiceMap = make(map[string]string)

	// Load the adapters
	blobs, err := ep.backend.List(log.WithSpanFromContext(context.Background(), ctx), "adapters")
	if err != nil {
		return err
	}

	// Data is marshalled as proto bytes in the data store
	for _, blob := range blobs {
		data := blob.Value.([]byte)
		adapter := &voltha.Adapter{}
		if err := proto.Unmarshal(data, adapter); err != nil {
			return err
		}
		// A valid adapter should have the vendorID set
		if adapter.Vendor != "" {
			if _, ok := ep.services[adapter.Type]; !ok {
				ep.services[adapter.Type] = &service{
					id:             adapter.Type,
					totalReplicas:  adapter.TotalReplicas,
					replicas:       make(map[ReplicaID]Endpoint),
					consistentRing: consistent.New(nil, ep.getConsistentConfig()),
				}

			}
			currentReplica := ReplicaID(adapter.CurrentReplica)
			endpoint := Endpoint(adapter.Endpoint)
			ep.services[adapter.Type].replicas[currentReplica] = endpoint
			ep.services[adapter.Type].consistentRing.Add(newMember(adapter.Id, adapter.Type, adapter.Vendor, endpoint, adapter.Version, currentReplica))
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
		if err := proto.Unmarshal(data, deviceType); err != nil {
			return err
		}
		if _, ok := ep.deviceTypeServiceMap[deviceType.Id]; !ok {
			ep.deviceTypeServiceMap[deviceType.Id] = deviceType.Adapter
		}
	}

	// Log the loaded data in debug mode to facilitate trouble shooting
	if logger.V(log.DebugLevel) {
		for key, val := range ep.services {
			members := val.consistentRing.GetMembers()
			logger.Debugw(ctx, "service", log.Fields{"service": key, "expected-replica": val.totalReplicas, "replicas": len(val.consistentRing.GetMembers())})
			for _, m := range members {
				n := m.(Member)
				logger.Debugw(ctx, "service-loaded", log.Fields{"serviceId": n.getID(), "serviceType": n.getServiceType(), "replica": n.getReplica(), "endpoint": n.getEndPoint()})
			}
		}
		logger.Debugw(ctx, "device-types-loaded", log.Fields{"device-types": ep.deviceTypeServiceMap})
	}
	return nil
}

// makeKey creates the string that the hash function uses to create the hash
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
	getServiceType() string
}

// member implements the Member interface
type member struct {
	id          string
	serviceType string
	vendor      string
	version     string
	replica     ReplicaID
	endpoint    Endpoint
}

func newMember(ID string, serviceType string, vendor string, endPoint Endpoint, version string, replica ReplicaID) Member {
	return &member{
		id:          ID,
		serviceType: serviceType,
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

func (m *member) getServiceType() string {
	return m.serviceType
}
