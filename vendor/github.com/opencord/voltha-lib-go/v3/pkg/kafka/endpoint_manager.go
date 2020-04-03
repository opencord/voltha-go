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
	//All the values below can be tuned to get optimal data distribution.  The numbers below seems to work well when
	// supporting 1000-10000 devices and 1 - 20 replicas of a service

	// Keys are distributed among partitions. Prime numbers are good to distribute keys uniformly.
	DefaultPartitionCount = 1117

	// Represents how many times a node is replicated on the consistent ring.
	DefaultReplicationFactor = 117

	// Load is used to calculate average load.
	DefaultLoad = 1.1
)

type Endpoint string // Endpoint of a service instance.  When using kafka, this is the topic name of a service
type ReplicaID int32 // A replication serviceID of a service instance

type EndpointManager interface {
	// GetEndpointByDeviceType is called to get the endpoint to communicate with for a specific device and deviceType.  For
	// now this will return the topic name
	GetEndpoint(deviceID string, deviceType string) (Endpoint, error)

	// GetEndpointByService is called to get the endpoint to communicate with for a specific device and deviceType.  For
	// now this will return the topic name
	GetEndpointByService(deviceID string, serviceType string) (Endpoint, error)

	// IsDeviceOwnedByService is invoked when a specific service (serviceID + replicaNumber) is restarted and
	// devices owned by that service need to be reconciled
	IsDeviceOwnedByService(deviceID string, serviceType string, replicaNumber int32) (bool, error)

	// getReplicaAssignment returns the replica number of the service that owns the deviceID.  This is used by the
	// test only
	getReplicaAssignment(deviceID string, serviceType string) (ReplicaID, error)
}

type service struct {
	id             string // Name of the service.  The same name is used for all replicas
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

func (ep *endpointManager) GetEndpoint(deviceID string, deviceType string) (Endpoint, error) {
	// Get the service that is handle this device type
	serv, err := ep.getServiceByDeviceType(deviceType)
	if err != nil {
		return "", err
	}
	key := []byte(ep.makeKey(deviceID, deviceType, serv.id))
	owner := serv.consistentRing.LocateKey(key)
	if owner.String() == "" {
		return "", status.Errorf(codes.Unavailable, "endpoint-not-set-%s", serv.id)
	}
	return Endpoint(owner.String()), nil
}

func (ep *endpointManager) GetEndpointByService(deviceID string, serviceType string) (Endpoint, error) {
	logger.Debugw("GetEndpointByService", log.Fields{"device-id": deviceID, "service-id": serviceType})
	owner, err := ep.getOwner(deviceID, serviceType)
	if err != nil {
		logger.Errorw("cannot-find-owner-for-device", log.Fields{
			"deviceId": deviceID,
			"adapterType": serviceType,
			"err": err,
		})
		return "", err
	}
	logger.Info("found-owner", log.Fields{
		"deviceId": deviceID,
		"adapterType": serviceType,
		"owner": owner,
	})
	m, ok := owner.(Member)
	if !ok {
		logger.Errorw("invalid-member", log.Fields{
			"deviceId": deviceID,
			"adapterType": serviceType,
			"owner": owner,
		})
		return "", status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}

	if m.GetEndPoint() == "" {
		return "", status.Errorf(codes.Unavailable, "endpoint-not-set-%s", serviceType)
	}

	logger.Info("found-member", log.Fields{
		"deviceId": deviceID,
		"adapterType": serviceType,
		"endpoint": m.GetEndPoint(),
		"serviceId": m.GetServiceType(),
		"replicaId": m.GetReplica(),
	})

	return m.GetEndPoint(), nil
}

func (ep *endpointManager) IsDeviceOwnedByService(deviceID string, serviceType string, replicaNumber int32) (bool, error) {
	owner, err := ep.getOwner(deviceID, serviceType)
	if err != nil {
		return false, nil
	}
	m, ok := owner.(Member)
	if !ok {
		return false, status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	return m.GetReplica() == ReplicaID(replicaNumber), nil
}

func (ep *endpointManager) getReplicaAssignment(deviceID string, serviceType string) (ReplicaID, error) {
	owner, err := ep.getOwner(deviceID, serviceType)
	if err != nil {
		return 0, nil
	}
	m, ok := owner.(Member)
	if !ok {
		return 0, status.Errorf(codes.Aborted, "invalid-member-%v", owner)
	}
	return m.GetReplica(), nil
}

func (ep *endpointManager) getOwner(deviceID string, serviceType string) (consistent.Member, error) {
	serv, err := ep.getServiceByType(serviceType)
	if err != nil {
		return nil, err
	}
	dType, err := ep.getDeviceTypeByServiceType(serviceType)
	if err != nil {
		return nil, err
	}
	key := []byte(ep.makeKey(deviceID, dType, serviceType))
	return serv.consistentRing.LocateKey(key), nil
}

func (ep *endpointManager) getServiceByDeviceType(deviceType string) (*service, error) {
	// First get the service that supports this device type
	ep.deviceTypeServiceMapLock.RLock()
	serviceID, deviceTypeExist := ep.deviceTypeServiceMap[deviceType]
	ep.deviceTypeServiceMapLock.RUnlock()

	var serv *service
	var serviceExist bool
	if deviceTypeExist {
		// Check whether service exist
		ep.servicesLock.RLock()
		serv, serviceExist = ep.services[serviceID]
		ep.servicesLock.RUnlock()
	}

	if !deviceTypeExist || !serviceExist || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
		if err := ep.loadServices(); err != nil {
			return nil, err
		}

		// Check whether the deviceType exists now
		ep.deviceTypeServiceMapLock.RLock()
		serviceID, deviceTypeExist = ep.deviceTypeServiceMap[deviceType]
		ep.deviceTypeServiceMapLock.RUnlock()
		if !deviceTypeExist {
			return nil, status.Errorf(codes.NotFound, "device-type-%s", deviceType)
		}

		// Check whether the service exists now
		ep.servicesLock.RLock()
		serv, serviceExist = ep.services[serviceID]
		ep.servicesLock.RUnlock()
		if !serviceExist || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
			return nil, status.Errorf(codes.NotFound, "service-%s", serviceID)
		}
	}
	return serv, nil
}

func (ep *endpointManager) getServiceByType(serviceType string) (*service, error) {
	// Check whether service exist
	ep.servicesLock.RLock()
	serv, serviceExist := ep.services[serviceType]
	ep.servicesLock.RUnlock()

	if !serviceExist || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
		logger.Info("get-service-by-id-reloading-services")

		if err := ep.loadServices(); err != nil {
			return nil, err
		}

		// Check whether the service exists now
		ep.servicesLock.RLock()
		serv, serviceExist = ep.services[serviceType]
		ep.servicesLock.RUnlock()
		logger.Infow("got-service-after-cache-update", log.Fields{
			"serviceType": serviceType,
			"service": serv,
			"serviceExist": serviceExist,
		})
		if !serviceExist || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
			return nil, status.Errorf(codes.NotFound, "service-%s", serviceType)
		}
	}
	logger.Infow("found-service-by-id", log.Fields{
		"serviceType": serviceType,
		"service": serv,
	})
	return serv, nil
}

func (ep *endpointManager) getDeviceTypeByServiceType(serviceType string) (string, error) {
	// Check whether service exist
	ep.servicesLock.RLock()
	serv, serviceExist := ep.services[serviceType]
	ep.servicesLock.RUnlock()

	if !serviceExist || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
		if err := ep.loadServices(); err != nil {
			return "", err
		}

		// Check whether the service exists now
		ep.servicesLock.RLock()
		_, serviceExist = ep.services[serviceType]
		ep.servicesLock.RUnlock()
		if !serviceExist || int(serv.totalReplicas) != len(serv.consistentRing.GetMembers()) {
			return "", status.Errorf(codes.NotFound, "service-%s", serviceType)
		}
	}

	for dType, sID := range ep.deviceTypeServiceMap {
		if sID == serviceType {
			return dType, nil
		}
	}
	return "", status.Errorf(codes.NotFound, "service-%s", serviceType)
}

func (ep *endpointManager) makeKey(deviceID string, deviceType string, serviceType string) []byte {
	return []byte(fmt.Sprintf("%s_%s_%s", serviceType, deviceType, deviceID))
}

func (ep *endpointManager) getConsistentConfig() consistent.Config {
	return consistent.Config{
		PartitionCount:    ep.partitionCount,
		ReplicationFactor: ep.replicationFactor,
		Load:              ep.load,
		Hasher:            hasher{},
	}
}

// loadServices loads the services into memory. For now we only loads the adapter service
func (ep *endpointManager) loadServices() error {
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
	blobs, err := ep.backend.List(context.Background(), "adapters")
	if err != nil {
		return err
	}

	log.Infow("load-services-adapter-list", log.Fields{
		"adapters": blobs,
	})

	// Data is marshalled as proto bytes in the data store
	for _, blob := range blobs {
		data := blob.Value.([]byte)
		adapter := &voltha.Adapter{}
		if err := proto.Unmarshal(data, adapter); err != nil {
			return err
		}

		log.Infow("load-services-single-adapter", log.Fields{
			"adapter": adapter,
		})
		// A valid adapter should have the vendorID set
		if adapter.Vendor != "" {
			log.Infow("load-services-adapter-has-vendor", log.Fields{
				"adapter": adapter,
			})
			if _, ok := ep.services[adapter.Type]; !ok {
				ep.services[adapter.Type] = &service{
					id:             adapter.Type,
					totalReplicas:  adapter.TotalReplicas,
					replicas:       make(map[ReplicaID]Endpoint),
					consistentRing: consistent.New(nil, ep.getConsistentConfig()),
				}
				log.Infow("load-services-adapter-added", log.Fields{
					"adapter": adapter,
					"registered": ep.services,
				})

			}
			currentReplica := ReplicaID(adapter.CurrentReplica)
			endpoint := Endpoint(adapter.Endpoint)

			log.Infow("load-services-adapter-add-to-ring", log.Fields{
				"adapterReplica": adapter.CurrentReplica,
				"adapter": adapter.Id,
				"endpoing": adapter.Endpoint,
				"registered": ep.services,
			})

			ep.services[adapter.Type].replicas[currentReplica] = endpoint
			ep.services[adapter.Type].consistentRing.Add(newMember(adapter.Id, adapter.Type, adapter.Vendor, endpoint, adapter.Version, currentReplica))

		}
	}
	// Load the device types
	blobs, err = ep.backend.List(context.Background(), "device_types")
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

	for key, val := range ep.services {
		members := val.consistentRing.GetMembers()
		logger.Debugw("service", log.Fields{"service": key, "expected-replica": val.totalReplicas, "replicas": len(val.consistentRing.GetMembers())})
		for _, m := range members {
			n := m.(Member)
			logger.Debugw("service-loaded", log.Fields{"serviceId": n.GetID(), "serviceType": n.GetServiceType(), "replica": n.GetReplica(), "endpoint": n.GetEndPoint()})
		}
	}
	logger.Debugw("device-types-loaded", log.Fields{"device-types": ep.deviceTypeServiceMap})
	return nil
}

// The consistent package requires a hasher function
type hasher struct{}

// Sum64 provides the hasher function.
func (h hasher) Sum64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// Member represents a member on the consistent ring
type Member interface {
	String() string
	GetReplica() ReplicaID
	GetEndPoint() Endpoint
	GetID() string
	GetServiceType() string
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

func (m *member) GetReplica() ReplicaID {
	return m.replica
}

func (m *member) GetEndPoint() Endpoint {
	return m.endpoint
}

func (m *member) GetID() string {
	return m.id
}

func (m *member) GetServiceType() string {
	return m.serviceType
}
