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
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v4/pkg/db"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-lib-go/v4/pkg/mocks/etcd"
	"github.com/opencord/voltha-protos/v4/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
	"strconv"
	"testing"
	"time"
)

type EPTest struct {
	etcdServer  *etcd.EtcdServer
	backend     *db.Backend
	maxReplicas int
	minReplicas int
}

func newEPTest(minReplicas, maxReplicas int) *EPTest {
	ctx := context.Background()
	test := &EPTest{
		minReplicas: minReplicas,
		maxReplicas: maxReplicas,
	}

	// Create backend
	if err := test.initBackend(); err != nil {
		logger.Fatalw(ctx, "setting-backend-failed", log.Fields{"error": err})
	}

	// Populate backend with data
	if err := test.populateBackend(); err != nil {
		logger.Fatalw(ctx, "populating-db-failed", log.Fields{"error": err})
	}
	return test
}

func (ep *EPTest) initBackend() error {
	ctx := context.Background()
	configName := "voltha-lib.kafka.ep.test"
	storageDir := "voltha-lib.kafka.ep.etcd"
	logLevel := "error"
	timeout := 5 * time.Second

	kvClientPort, err := freeport.GetFreePort()
	if err != nil {
		return err
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		return err
	}
	ep.etcdServer = etcd.StartEtcdServer(ctx, etcd.MKConfig(ctx, configName, kvClientPort, peerPort, storageDir, logLevel))
	if ep.etcdServer == nil {
		return status.Error(codes.Internal, "Embedded server failed to start")
	}

	ep.backend = db.NewBackend(ctx, "etcd", "127.0.0.1"+":"+strconv.Itoa(kvClientPort), timeout, "service/voltha")
	return nil
}

func (ep *EPTest) stopAll() {
	if ep.etcdServer != nil {
		ep.etcdServer.Stop(context.Background())
	}
}

func (ep *EPTest) populateBackend() error {
	// Add an adapter with multiple replicas
	adapterPrefix := "adapter_brcm_openomci_onu"
	numReplicas := ep.maxReplicas
	for i := 0; i < numReplicas; i++ {
		adapter := &voltha.Adapter{
			Id:             fmt.Sprintf("%s_%d", adapterPrefix, i),
			Vendor:         "VOLTHA OpenONU",
			Version:        "2.4.0-dev0",
			Type:           adapterPrefix,
			CurrentReplica: int32(i),
			TotalReplicas:  int32(numReplicas),
			Endpoint:       fmt.Sprintf("%s_%d", adapterPrefix, i),
		}
		adapterKVKey := fmt.Sprintf("%s/%d", adapterPrefix, i)
		blob, err := proto.Marshal(adapter)
		if err != nil {
			return err
		}
		if err := ep.backend.Put(context.Background(), "adapters/"+adapterKVKey, blob); err != nil {
			return err
		}
	}

	// Add an adapter with minreplicas
	adapterPrefix = "adapter_openolt"
	numReplicas = ep.minReplicas
	for i := 0; i < numReplicas; i++ {
		adapter := &voltha.Adapter{
			Id:             fmt.Sprintf("%s_%d", adapterPrefix, i),
			Vendor:         "VOLTHA OpenOLT",
			Version:        "2.3.1-dev",
			Type:           adapterPrefix,
			CurrentReplica: int32(i),
			TotalReplicas:  int32(numReplicas),
			Endpoint:       fmt.Sprintf("%s_%d", adapterPrefix, i),
		}
		adapterKVKey := fmt.Sprintf("%s/%d", adapterPrefix, i)
		blob, err := proto.Marshal(adapter)
		if err != nil {
			return err
		}
		if err := ep.backend.Put(context.Background(), "adapters/"+adapterKVKey, blob); err != nil {
			return err
		}
	}

	// Add the brcm_openomci_onu device type
	dType := "brcm_openomci_onu"
	adapterName := "adapter_brcm_openomci_onu"
	deviceType := &voltha.DeviceType{
		Id:                          dType,
		VendorIds:                   []string{"OPEN", "ALCL", "BRCM", "TWSH", "ALPH", "ISKT", "SFAA", "BBSM", "SCOM", "ARPX", "DACM", "ERSN", "HWTC", "CIGG"},
		Adapter:                     adapterName,
		AcceptsAddRemoveFlowUpdates: true,
	}
	blob, err := proto.Marshal(deviceType)
	if err != nil {
		return err
	}
	if err := ep.backend.Put(context.Background(), "device_types/"+deviceType.Id, blob); err != nil {
		return err
	}

	// Add the openolt device type
	dType = "openolt"
	adapterName = "adapter_openolt"
	deviceType = &voltha.DeviceType{
		Id:                          dType,
		Adapter:                     adapterName,
		AcceptsAddRemoveFlowUpdates: true,
	}
	blob, err = proto.Marshal(deviceType)
	if err != nil {
		return err
	}
	if err := ep.backend.Put(context.Background(), "device_types/"+deviceType.Id, blob); err != nil {
		return err
	}
	return nil
}

func getMeanAndStdDeviation(val []int, replicas int) (float64, float64) {
	var sum, mean, sd float64
	for i := 0; i < replicas; i++ {
		sum += float64(val[i])
	}
	mean = sum / float64(replicas)

	for j := 0; j < replicas; j++ {
		sd += math.Pow(float64(val[j])-mean, 2)
	}
	sd = math.Sqrt(sd / float64(replicas))
	return mean, sd
}

func (ep *EPTest) testEndpointManagerAPIs(t *testing.T, tm EndpointManager, serviceType string, deviceType string, replicas int) {
	ctx := context.Background()
	// Map of device ids to topic
	deviceIDs := make(map[string]Endpoint)
	numDevices := 1000
	total := make([]int, replicas)
	for i := 0; i < numDevices; i++ {
		deviceID := uuid.New().String()
		endpoint, err := tm.GetEndpoint(ctx, deviceID, serviceType)
		if err != nil {
			logger.Fatalw(ctx, "error-getting-endpoint", log.Fields{"error": err})
		}
		deviceIDs[deviceID] = endpoint
		replicaID, err := tm.GetReplicaAssignment(ctx, deviceID, serviceType)
		if err != nil {
			logger.Fatalw(ctx, "error-getting-endpoint", log.Fields{"error": err})
		}
		total[replicaID] += 1
	}

	mean, sdtDev := getMeanAndStdDeviation(total, replicas)
	fmt.Println(fmt.Sprintf("Device distributions => devices:%d service_replicas:%d mean:%d standard_deviation:%d, distributions:%v", numDevices, replicas, int(mean), int(sdtDev), total))

	// Verify that we get the same topic for a given device ID, irrespective of the number of iterations
	numIterations := 10
	for i := 0; i < numIterations; i++ {
		for deviceID, expectedEndpoint := range deviceIDs {
			endpointByServiceType, err := tm.GetEndpoint(ctx, deviceID, serviceType)
			if err != nil {
				logger.Fatalw(ctx, "error-getting-endpoint", log.Fields{"error": err})
			}
			assert.Equal(t, expectedEndpoint, endpointByServiceType)
		}
	}

	// Verify that a device belong to the correct node
	for deviceID := range deviceIDs {
		replicaID, err := tm.GetReplicaAssignment(ctx, deviceID, serviceType)
		if err != nil {
			logger.Fatalw(ctx, "error-getting-topic", log.Fields{"error": err})
		}
		for k := 0; k < replicas; k++ {
			owned, err := tm.IsDeviceOwnedByService(ctx, deviceID, serviceType, int32(k))
			if err != nil {
				logger.Fatalw(ctx, "error-verifying-device-ownership", log.Fields{"error": err})
			}
			assert.Equal(t, ReplicaID(k) == replicaID, owned)
		}
	}
}

func TestEndpointManagerSuite(t *testing.T) {
	tmt := newEPTest(1, 10)
	assert.NotNil(t, tmt)

	tm := NewEndpointManager(
		tmt.backend,
		PartitionCount(1117),
		ReplicationFactor(200),
		Load(1.1))

	defer tmt.stopAll()

	//1. Test APIs with multiple replicas
	tmt.testEndpointManagerAPIs(t, tm, "adapter_brcm_openomci_onu", "brcm_openomci_onu", tmt.maxReplicas)

	//2. Test APIs with single replica
	tmt.testEndpointManagerAPIs(t, tm, "adapter_openolt", "openolt", tmt.minReplicas)
}
