/*
 * Copyright 2020-2023 Open Networking Foundation (ONF) and the ONF Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package core Common Logger initialization
package test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ca "github.com/opencord/voltha-protos/v5/go/core_adapter"

	"math/rand"

	"github.com/opencord/voltha-go/rw_core/config"
	cm "github.com/opencord/voltha-go/rw_core/mocks"
	"github.com/opencord/voltha-lib-go/v7/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	mock_etcd "github.com/opencord/voltha-lib-go/v7/pkg/mocks/etcd"
	"github.com/opencord/voltha-lib-go/v7/pkg/version"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	OltAdapter = iota
	OnuAdapter
)

type AdapterInfo struct {
	TotalReplica    int32
	Vendor          string
	DeviceType      string
	ChildDeviceType string
	ChildVendor     string
}

// prettyPrintDeviceUpdateLog is used just for debugging and exploring the Core logs
func prettyPrintDeviceUpdateLog(inputFile string, deviceID string) {
	file, err := os.Open(filepath.Clean(inputFile))
	if err != nil {
		logger.Fatal(context.Background(), err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Errorw(context.Background(), "file-close-error", log.Fields{"error": err})
		}
	}()

	var logEntry struct {
		Level       string `json:"level"`
		Ts          string `json:"ts"`
		Caller      string `json:"caller"`
		Msg         string `json:"msg"`
		RPC         string `json:"rpc"`
		DeviceID    string `json:"device-id"`
		RequestedBy string `json:"requested-by"`
		StateChange string `json:"state-change"`
		Status      string `json:"status"`
		Description string `json:"description"`
	}

	scanner := bufio.NewScanner(file)
	fmt.Println("Timestamp\t\t\tDeviceId\t\t\t\tStatus\t\t\tRPC\t\t\tRequestedBy\t\t\tStateChange\t\t\tDescription")
	for scanner.Scan() {
		input := scanner.Text()
		// Look for device update logs only
		if !strings.Contains(input, "device-operation") || !strings.Contains(input, "requested-by") {
			continue
		}
		// Check if deviceID is required
		if deviceID != "" {
			if !strings.Contains(input, deviceID) {
				continue
			}
		}
		if err := json.Unmarshal([]byte(input), &logEntry); err != nil {
			logger.Fatal(context.Background(), err)
		}
		fmt.Println(
			fmt.Sprintf(
				"%s\t%s\t%s\t%-30.30q\t%-16.16s\t%-25.25s\t%s",
				logEntry.Ts,
				logEntry.DeviceID,
				logEntry.Status,
				logEntry.RPC,
				logEntry.RequestedBy,
				logEntry.StateChange,
				logEntry.Description))
	}
}

func omciLog(inputFile string, deviceID string) {
	file, err := os.Open(filepath.Clean(inputFile))
	if err != nil {
		logger.Fatal(context.Background(), err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Errorw(context.Background(), "file-close-error", log.Fields{"error": err})
		}
	}()

	var logEntry struct {
		Level         string `json:"level"`
		Ts            string `json:"ts"`
		Caller        string `json:"caller"`
		Msg           string `json:"msg"`
		InstanceID    string `json:"instanceId"`
		ChildDeviceID string `json:"child-device-id"`
		OmciMsg       string `json:"omciMsg"`
		IntfID        string `json:"intf-id"`
		OnuID         string `json:"onu-id"`
		OmciTrns      int    `json:"omciTransactionID"`
	}

	scanner := bufio.NewScanner(file)
	uniqueTnsIDs := map[int]int{}
	for scanner.Scan() {
		input := scanner.Text()
		// Look for device update logs only
		if !strings.Contains(input, "sent-omci-msg") {
			continue
		}
		// Check if deviceID is required
		if deviceID != "" {
			if !strings.Contains(input, deviceID) {
				continue
			}
		}
		if err := json.Unmarshal([]byte(input), &logEntry); err != nil {
			logger.Fatal(context.Background(), err)
		}
		uniqueTnsIDs[logEntry.OmciTrns]++
	}
	repeatedTrnsID := []int{}
	for k, v := range uniqueTnsIDs {
		if v != 1 {
			repeatedTrnsID = append(repeatedTrnsID, k)
		}
	}
	fmt.Println("RepeatedIDs", repeatedTrnsID, "TransID:", len(uniqueTnsIDs))
}

// CreateMockAdapter creates mock OLT and ONU adapters - this will automatically the grpc service hosted by that
// adapter
func CreateMockAdapter(
	ctx context.Context,
	adapterType int,
	coreEndpoint string,
	deviceType string,
	vendor string,
	childDeviceType string,
	childVendor string,
) (interface{}, error) {

	var adpt interface{}
	switch adapterType {
	case OltAdapter:
		adpt = cm.NewOLTAdapter(ctx, coreEndpoint, deviceType, vendor, childDeviceType, childVendor)
	case OnuAdapter:
		adpt = cm.NewONUAdapter(ctx, coreEndpoint, deviceType, vendor)
	default:
		logger.Fatalf(ctx, "invalid-adapter-type-%d", adapterType)
	}
	return adpt, nil
}

// CreateAndRegisterAdapters creates mock ONU and OLT adapters and registers them to rw-core
func CreateAndRegisterAdapters(
	ctx context.Context,
	t *testing.T,
	oltAdapters map[string]*AdapterInfo,
	onuAdapters map[string]*AdapterInfo,
	coreEndpoint string,
) (map[string][]*cm.OLTAdapter, map[string][]*cm.ONUAdapter) {
	// Setup the ONU adapter first in this unit test environment.  This makes it easier to test whether the
	// Core is ready to send grpc requests to the adapters.  The unit test uses grpc to communicate with the
	// Core and as such it does not have inside knowledge when the adapters are ready.

	// Setup the ONU Adapters
	onuAdaptersMap := make(map[string][]*cm.ONUAdapter)
	for adapterType, adapterInfo := range onuAdapters {
		for replica := int32(1); replica <= adapterInfo.TotalReplica; replica++ {
			adpt, err := CreateMockAdapter(ctx, OnuAdapter, coreEndpoint, adapterInfo.DeviceType, adapterInfo.Vendor, adapterInfo.ChildDeviceType, adapterInfo.ChildVendor)
			assert.Nil(t, err)
			onuAdapter, ok := adpt.(*cm.ONUAdapter)
			assert.True(t, ok)
			assert.NotNil(t, onuAdapter)
			//	Register the adapter
			adapterID := fmt.Sprintf("%s-%d", adapterType, replica)
			adapterToRegister := &voltha.Adapter{
				Id:             adapterID,
				Vendor:         adapterInfo.Vendor,
				Version:        version.VersionInfo.Version,
				Type:           adapterType,
				CurrentReplica: replica,
				TotalReplicas:  adapterInfo.TotalReplica,
				Endpoint:       onuAdapter.GetEndPoint(),
			}
			types := []*voltha.DeviceType{{Id: adapterInfo.DeviceType, AdapterType: adapterType, AcceptsAddRemoveFlowUpdates: true}}
			deviceTypes := &voltha.DeviceTypes{Items: types}
			coreClient, err := onuAdapter.GetCoreClient()
			assert.Nil(t, err)
			assert.NotNil(t, coreClient)
			if _, err := coreClient.RegisterAdapter(ctx, &ca.AdapterRegistration{
				Adapter: adapterToRegister,
				DTypes:  deviceTypes}); err != nil {
				logger.Errorw(ctx, "failed-to-register-adapter", log.Fields{"error": err, "adapter": adapterToRegister.Id})
				assert.NotNil(t, err)
			}
			if _, ok := onuAdaptersMap[adapterType]; !ok {
				onuAdaptersMap[adapterType] = []*cm.ONUAdapter{}
			}
			onuAdaptersMap[adapterType] = append(onuAdaptersMap[adapterType], onuAdapter)
		}
	}

	// Setup the OLT Adapters
	oltAdaptersMap := make(map[string][]*cm.OLTAdapter)
	for adapterType, adapterInfo := range oltAdapters {
		for replica := int32(1); replica <= adapterInfo.TotalReplica; replica++ {
			adpt, err := CreateMockAdapter(ctx, OltAdapter, coreEndpoint, adapterInfo.DeviceType, adapterInfo.Vendor, adapterInfo.ChildDeviceType, adapterInfo.ChildVendor)
			assert.Nil(t, err)
			oltAdapter, ok := adpt.(*cm.OLTAdapter)
			assert.True(t, ok)
			assert.NotNil(t, oltAdapter)

			//	Register the adapter
			adapterID := fmt.Sprintf("%s-%d", adapterType, replica)
			adapterToRegister := &voltha.Adapter{
				Id:             adapterID,
				Vendor:         adapterInfo.Vendor,
				Version:        version.VersionInfo.Version,
				Type:           adapterType,
				CurrentReplica: replica,
				TotalReplicas:  adapterInfo.TotalReplica,
				Endpoint:       oltAdapter.GetEndPoint(),
			}
			types := []*voltha.DeviceType{{Id: adapterInfo.DeviceType, AdapterType: adapterType, AcceptsAddRemoveFlowUpdates: true}}
			deviceTypes := &voltha.DeviceTypes{Items: types}
			coreClient, err := oltAdapter.GetCoreClient()
			assert.Nil(t, err)
			assert.NotNil(t, coreClient)

			if _, err := coreClient.RegisterAdapter(ctx, &ca.AdapterRegistration{
				Adapter: adapterToRegister,
				DTypes:  deviceTypes}); err != nil {
				logger.Errorw(ctx, "failed-to-register-adapter", log.Fields{"error": err, "adapter": adapterToRegister.Id})
				assert.NotNil(t, err)
			}
			if _, ok := oltAdaptersMap[adapterType]; !ok {
				oltAdaptersMap[adapterType] = []*cm.OLTAdapter{}
			}
			oltAdaptersMap[adapterType] = append(oltAdaptersMap[adapterType], oltAdapter)
		}
	}

	return oltAdaptersMap, onuAdaptersMap
}

// StartEmbeddedEtcdServer creates and starts an Embedded etcd server locally.
func StartEmbeddedEtcdServer(ctx context.Context, configName, storageDir, logLevel string) (*mock_etcd.EtcdServer, int, error) {
	kvClientPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	peerPort, err := freeport.GetFreePort()
	if err != nil {
		return nil, 0, err
	}
	etcdServer := mock_etcd.StartEtcdServer(ctx, mock_etcd.MKConfig(ctx, configName, kvClientPort, peerPort, storageDir, logLevel))
	if etcdServer == nil {
		return nil, 0, status.Error(codes.Internal, "Embedded server failed to start")
	}
	return etcdServer, kvClientPort, nil
}

// StopEmbeddedEtcdServer stops the embedded etcd server
func StopEmbeddedEtcdServer(ctx context.Context, server *mock_etcd.EtcdServer) {
	if server != nil {
		server.Stop(ctx)
	}
}

// SetupKVClient creates a new etcd client
func SetupKVClient(ctx context.Context, cf *config.RWCoreFlags, coreInstanceID string) kvstore.Client {
	client, err := kvstore.NewEtcdClient(ctx, cf.KVStoreAddress, cf.KVStoreTimeout, log.FatalLevel)
	if err != nil {
		panic("no kv client")
	}
	return client
}

// getRandomMacAddress returns a random mac address
func getRandomMacAddress() string {
	rand.Seed(time.Now().UnixNano() / int64(rand.Intn(255)+1))
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
	)
}
# [EOF] - delta:force
