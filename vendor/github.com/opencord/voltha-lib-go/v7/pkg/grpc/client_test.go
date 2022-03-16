/*
 * Copyright 2021-present Open Networking Foundation

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
package grpc_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

const (
	testGrpcServer  = "test-grpc-server"
	initialInterval = 100 * time.Millisecond
	maxInterval     = 5000 * time.Millisecond
	maxElapsedTime  = 0 * time.Millisecond
	monitorInterval = 2 * time.Second
	timeout         = 10 * time.Second
)

var testForNoActivityCh = make(chan time.Time, 20)
var testKeepAliveCh = make(chan time.Time, 20)

type testCoreServer struct {
	apiEndPoint string
	server      *vgrpc.GrpcServer
	probe       *probe.Probe
	coreService *vgrpc.MockCoreServiceHandler
}

func newTestCoreServer(apiEndpoint string) *testCoreServer {
	return &testCoreServer{
		apiEndPoint: apiEndpoint,
		probe:       &probe.Probe{},
	}
}

func (s *testCoreServer) registerService(ctx context.Context, t *testing.T) {
	assert.NotEqual(t, "", s.apiEndPoint)

	probePort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	probeEndpoint := ":" + strconv.Itoa(probePort)
	go s.probe.ListenAndServe(ctx, probeEndpoint)
	s.probe.RegisterService(ctx, testGrpcServer)

	s.server = vgrpc.NewGrpcServer(s.apiEndPoint, nil, false, s.probe)
	s.coreService = vgrpc.NewMockCoreServiceHandler()

	s.server.AddService(func(server *grpc.Server) {
		core_service.RegisterCoreServiceServer(server, s.coreService)
	})
}

func (s *testCoreServer) start(ctx context.Context, t *testing.T) {
	assert.NotNil(t, s.server)
	assert.NotEqual(t, "", s.apiEndPoint)

	s.probe.UpdateStatus(ctx, testGrpcServer, probe.ServiceStatusRunning)
	s.coreService.Start()
	s.server.Start(ctx)
	s.probe.UpdateStatus(ctx, testGrpcServer, probe.ServiceStatusStopped)
}

func (s *testCoreServer) stop() {
	s.coreService.Stop()
	if s.server != nil {
		s.server.Stop()
	}
}

type testClient struct {
	apiEndPoint string
	probe       *probe.Probe
	client      *vgrpc.Client
}

func serverRestarted(ctx context.Context, endPoint string) error {
	logger.Infow(ctx, "remote-restarted", log.Fields{"endpoint": endPoint})
	return nil
}

func newTestClient(apiEndpoint string, handler vgrpc.RestartedHandler) *testClient {
	tc := &testClient{
		apiEndPoint: apiEndpoint,
		probe:       &probe.Probe{},
	}
	// Set the environment variables that this client will use
	var err error
	err = os.Setenv("GRPC_BACKOFF_INITIAL_INTERVAL", initialInterval.String())
	if err != nil {
		logger.Warnw(context.Background(), "setting-env-variable-failed", log.Fields{"error": err, "variable": "GRPC_BACKOFF_INITIAL_INTERVAL"})
		return nil
	}
	err = os.Setenv("GRPC_BACKOFF_MAX_INTERVAL", maxInterval.String())
	if err != nil {
		logger.Warnw(context.Background(), "setting-env-variable-failed", log.Fields{"error": err, "variable": "GRPC_BACKOFF_MAX_INTERVAL"})
		return nil
	}
	err = os.Setenv("GRPC_BACKOFF_MAX_ELAPSED_TIME", maxElapsedTime.String())
	if err != nil {
		logger.Warnw(context.Background(), "setting-env-variable-failed", log.Fields{"error": err, "variable": "GRPC_BACKOFF_MAX_ELAPSED_TIME"})
		return nil
	}

	err = os.Setenv("GRPC_MONITOR_INTERVAL", monitorInterval.String())
	if err != nil {
		logger.Warnw(context.Background(), "setting-env-variable-failed", log.Fields{"error": err, "variable": "GRPC_MONITOR_INTERVAL"})
		return nil
	}

	tc.client, err = vgrpc.NewClient(
		"test-endpoint",
		apiEndpoint,
		"core_service.CoreService",
		handler)
	if err != nil {
		return nil
	}
	return tc
}

func getCoreServiceHandler(ctx context.Context, conn *grpc.ClientConn) interface{} {
	if conn == nil {
		return nil
	}
	return core_service.NewCoreServiceClient(conn)
}

func idleConnectionTest(ctx context.Context, conn *grpc.ClientConn) interface{} {
	if conn == nil {
		return nil
	}
	svc := core_service.NewCoreServiceClient(conn)

	testForNoActivityCh <- time.Now()
	return svc
}

func (c *testClient) start(ctx context.Context, t *testing.T, handler vgrpc.GetServiceClient) {
	assert.NotNil(t, c.client)

	probePort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	probeEndpoint := "127.0.0.1:" + strconv.Itoa(probePort)
	go c.probe.ListenAndServe(ctx, probeEndpoint)

	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, c.probe)
	c.client.Start(probeCtx, handler)
}

func (c *testClient) getClient(t *testing.T) core_service.CoreServiceClient {
	gc, err := c.client.GetClient()
	assert.Nil(t, err)
	coreClient, ok := gc.(core_service.CoreServiceClient)
	assert.True(t, ok)
	return coreClient
}

func serverStartsFirstTest(t *testing.T) {
	// Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start the test server
	grpcPort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	apiEndpoint := "127.0.0.1:" + strconv.Itoa(grpcPort)
	ts := newTestCoreServer(apiEndpoint)
	ts.registerService(ctx, t)
	go ts.start(ctx, t)

	// Create the test client and start it
	tc := newTestClient(apiEndpoint, serverRestarted)
	assert.NotNil(t, tc)
	go tc.start(ctx, t, getCoreServiceHandler)

	// Test 1: Verify that probe status shows ready eventually
	var servicesReady isConditionSatisfied = func() bool {
		return ts.probe.IsReady() && tc.probe.IsReady()
	}
	err = waitUntilCondition(timeout, servicesReady)
	assert.Nil(t, err)

	// Test 2: Verify we get a valid client and can make grpc requests with it
	coreClient := tc.getClient(t)
	assert.NotNil(t, coreClient)

	device, err := coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, "test-1234", device.Type)
}

func clientStartsFirstTest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a grpc endpoint for the server
	grpcPort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	apiEndpoint := "127.0.0.1:" + strconv.Itoa(grpcPort)

	// Create the test client and start it
	tc := newTestClient(apiEndpoint, serverRestarted)
	assert.NotNil(t, tc)
	go tc.start(ctx, t, getCoreServiceHandler)

	// Verify client is not ready
	var clientNotReady isConditionSatisfied = func() bool {
		serviceStatus := tc.probe.GetStatus(apiEndpoint)
		return serviceStatus == probe.ServiceStatusNotReady ||
			serviceStatus == probe.ServiceStatusPreparing ||
			serviceStatus == probe.ServiceStatusFailed
	}
	err = waitUntilCondition(timeout, clientNotReady)
	assert.Nil(t, err)

	// Create and start the test server
	ts := newTestCoreServer(apiEndpoint)
	ts.registerService(ctx, t)
	go ts.start(ctx, t)

	// Test 1: Verify that probe status shows ready eventually
	var servicesReady isConditionSatisfied = func() bool {
		return ts.probe.IsReady() && tc.probe.IsReady()
	}
	err = waitUntilCondition(timeout, servicesReady)
	assert.Nil(t, err)

	// Test 2: Verify we get a valid client and can make grpc requests with it
	coreClient := tc.getClient(t)
	assert.NotNil(t, coreClient)

	device, err := coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, "test-1234", device.Type)
}

// Liveness function
func livessness(timestamp time.Time) {
	logger.Debugw(context.Background(), "received-liveness", log.Fields{"timestamp": timestamp})
}

func serverRestarts(t *testing.T, numRestartRuns int) {
	// Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start the test server
	grpcPort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	apiEndpoint := "127.0.0.1:" + strconv.Itoa(grpcPort)
	ts := newTestCoreServer(apiEndpoint)
	ts.registerService(ctx, t)
	go ts.start(ctx, t)

	// Create the test client and start it
	tc := newTestClient(apiEndpoint, serverRestarted)
	assert.NotNil(t, tc)

	// Subscribe for liveness
	tc.client.SubscribeForLiveness(livessness)
	go tc.start(ctx, t, getCoreServiceHandler)

	// Test 1: Verify that probe status shows ready eventually
	var servicesReady isConditionSatisfied = func() bool {
		return ts.probe.IsReady() && tc.probe.IsReady()
	}
	err = waitUntilCondition(timeout, servicesReady)
	assert.Nil(t, err)

	// Test 2: Verify we get a valid client and can make grpc requests with it
	coreClient := tc.getClient(t)
	assert.NotNil(t, coreClient)

	device, err := coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, "test-1234", device.Type)

	for i := 1; i <= numRestartRuns; i++ {
		//Test 3: Stop server and verify server status
		ts.stop()
		var serverDown isConditionSatisfied = func() bool {
			return ts.probe.GetStatus(testGrpcServer) == probe.ServiceStatusStopped
		}
		err = waitUntilCondition(timeout, serverDown)
		assert.Nil(t, err)

		// Wait until the client service shows as not ready. A wait is not needed.  It's just to verify that the
		// client changes connection state.
		var clientNotReady isConditionSatisfied = func() bool {
			serviceStatus := tc.probe.GetStatus(apiEndpoint)
			return serviceStatus == probe.ServiceStatusNotReady ||
				serviceStatus == probe.ServiceStatusPreparing ||
				serviceStatus == probe.ServiceStatusFailed
		}
		err = waitUntilCondition(timeout, clientNotReady)

		assert.Nil(t, err)

		// Test 4: Re-create the server and verify the server is back online
		ts = newTestCoreServer(apiEndpoint)
		ts.registerService(ctx, t)
		go ts.start(ctx, t)
		err = waitUntilCondition(timeout, servicesReady)
		assert.Nil(t, err)

		// Test 5: verify we can pull new device with a new client instance
		coreClient = tc.getClient(t)
		assert.NotNil(t, coreClient)
		device, err := coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
		assert.Nil(t, err)
		assert.Equal(t, "test-1234", device.Type)
	}
	// Stop the server
	ts.stop()
}

// Liveness function
func keepAliveMonitor(timestamp time.Time) {
	logger.Debugw(context.Background(), "received-liveness", log.Fields{"timestamp": timestamp})
	testKeepAliveCh <- timestamp
}

func testKeepAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a grpc endpoint for the server
	grpcPort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	apiEndpoint := "127.0.0.1:" + strconv.Itoa(grpcPort)

	// Create the test client and start it
	tc := newTestClient(apiEndpoint, serverRestarted)
	assert.NotNil(t, tc)
	go tc.start(ctx, t, idleConnectionTest)

	// Create and start the test server
	ts := newTestCoreServer(apiEndpoint)
	tc.client.SubscribeForLiveness(keepAliveMonitor)
	ts.registerService(ctx, t)
	go ts.start(ctx, t)
	defer tc.client.Stop(context.Background())

	// Test 1: Verify that probe status shows ready eventually
	var servicesReady isConditionSatisfied = func() bool {
		return ts.probe.IsReady() && tc.probe.IsReady()
	}
	err = waitUntilCondition(timeout, servicesReady)
	assert.Nil(t, err)

	// Test 2: Verify we get a valid client and can make grpc requests with it
	coreClient := tc.getClient(t)
	assert.NotNil(t, coreClient)

	device, err := coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, "test-1234", device.Type)

	start := time.Now()
	numChecks := 3 // Test for 3 checks
	// Wait on the the idle channel - on no activity a connection probe will be attempted by the client
	timer := time.NewTimer((monitorInterval + 1*time.Second) * time.Duration(numChecks))
	defer timer.Stop()
	count := 0
loop:
	for {
		select {
		case timestamp := <-testKeepAliveCh:
			if timestamp.After(start) {
				count += 1
				if count > numChecks {
					break loop
				}
			}
		case <-timer.C:
			t.Fatal("no activity on the idle channel")
		}
	}
}

func testClientFailure(t *testing.T, numClientRestarts int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create a grpc endpoint for the server
	grpcPort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	apiEndpoint := "127.0.0.1:" + strconv.Itoa(grpcPort)
	// Create the test client and start it
	tc := newTestClient(apiEndpoint, serverRestarted)
	assert.NotNil(t, tc)
	go tc.start(ctx, t, idleConnectionTest)
	// Create and start the test server
	ts := newTestCoreServer(apiEndpoint)
	ts.registerService(ctx, t)
	go ts.start(ctx, t)
	defer ts.stop()
	// Test 1: Verify that probe status shows ready eventually
	var servicesReady isConditionSatisfied = func() bool {
		return ts.probe.IsReady() && tc.probe.IsReady()
	}
	err = waitUntilCondition(timeout, servicesReady)
	assert.Nil(t, err)
	// Test 2: Verify we get a valid client and can make grpc requests with it
	coreClient := tc.getClient(t)
	assert.NotNil(t, coreClient)
	device, err := coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
	assert.Nil(t, err)
	assert.NotNil(t, device)
	assert.Equal(t, "test-1234", device.Type)
	for i := 1; i <= numClientRestarts; i++ {
		// Kill grpc client
		tc.client.Stop(context.Background())
		var clientNotReady isConditionSatisfied = func() bool {
			return !tc.probe.IsReady()
		}
		err = waitUntilCondition(timeout, clientNotReady)
		assert.Nil(t, err)

		// Create a new client
		tc.client, err = vgrpc.NewClient(
			"test-endpoint",
			apiEndpoint,
			"core_service.CoreService",
			serverRestarted)
		assert.Nil(t, err)
		probeCtx := context.WithValue(ctx, probe.ProbeContextKey, tc.probe)
		go tc.client.Start(probeCtx, idleConnectionTest)

		//Verify that probe status shows ready eventually
		err = waitUntilCondition(timeout, servicesReady)
		assert.Nil(t, err)

		// Verify we get a valid client and can make grpc requests with it
		coreClient = tc.getClient(t)
		assert.NotNil(t, coreClient)

		device, err = coreClient.GetDevice(context.Background(), &common.ID{Id: "1234"})
		assert.Nil(t, err)
		assert.NotNil(t, device)
		assert.Equal(t, "test-1234", device.Type)
	}
	tc.client.Stop(context.Background())
}

func testServerLimit(t *testing.T) {
	t.Skip() // Not needed for regular unit tests

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a grpc endpoint for the server
	grpcPort, err := freeport.GetFreePort()
	assert.Nil(t, err)
	apiEndpoint := "127.0.0.1:" + strconv.Itoa(grpcPort)

	// Create the test client and start it
	tc := newTestClient(apiEndpoint, serverRestarted)
	assert.NotNil(t, tc)
	go tc.start(ctx, t, idleConnectionTest)

	// Create and start the test server
	ts := newTestCoreServer(apiEndpoint)
	ts.registerService(ctx, t)
	go ts.start(ctx, t)

	// Test 1: Verify that probe status shows ready eventually
	var servicesReady isConditionSatisfied = func() bool {
		return ts.probe.IsReady() && tc.probe.IsReady()
	}
	err = waitUntilCondition(timeout, servicesReady)
	assert.Nil(t, err)

	// Test 2: Verify we get a valid client and can make grpc requests with it
	coreClient := tc.getClient(t)
	assert.NotNil(t, coreClient)

	var lock sync.RWMutex
	bad := []time.Duration{}
	bad_err := []string{}
	good := []time.Duration{}
	var wg sync.WaitGroup
	numRPCs := 10
	total_good := time.Duration(0)
	max_good := time.Duration(0)
	total_bad := time.Duration(0)
	max_bad := time.Duration(0)
	order_received := []uint32{}
	for i := 1; i <= numRPCs; i++ {
		wg.Add(1)
		go func(seq int) {
			local := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 10000*time.Millisecond)
			defer cancel()
			var err error
			var d *voltha.Device
			d, err = coreClient.GetDevice(ctx, &common.ID{Id: strconv.Itoa(seq)})
			if err != nil {
				lock.Lock()
				bad = append(bad, time.Since(local))
				bad_err = append(bad_err, err.Error())
				total_bad += time.Since(local)
				if time.Since(local) > max_bad {
					max_bad = time.Since(local)
				}
				logger.Errorw(ctx, "error produced", log.Fields{"error": err})
				lock.Unlock()
			} else {
				lock.Lock()
				good = append(good, time.Since(local))
				total_good += time.Since(local)
				if time.Since(local) > max_good {
					max_good = time.Since(local)
				}
				if d != nil {
					order_received = append(order_received, d.Vlan)
				}
				lock.Unlock()
			}
			wg.Done()
		}(i)
	}
	wg.Wait()
	assert.Equal(t, 0, len(bad))
	assert.Equal(t, numRPCs, len(good))
	//fmt.Println("Bad:", bad[:10])
	if len(bad_err) > 0 {
		fmt.Println("Bad Err Last:", bad_err[len(bad_err)-1:])
		fmt.Println("Bad Err First:", bad_err[:1])
	}
	fmt.Println("Good:", good[len(good)-10:])
	fmt.Println("Good average time:", total_good.Milliseconds()/int64(numRPCs))
	fmt.Println("Bad average time:", total_bad.Milliseconds()/int64(numRPCs))
	fmt.Println("Bad Max:", max_bad)
	fmt.Println("Good Max:", max_good)
	//fmt.Println("Order received:", order_received)

	prev := order_received[0]

	for i := 1; i < len(order_received); i++ {
		if order_received[i] < prev {
			fmt.Println("Prev:", prev, " curr:", order_received[i])
		}
		prev = order_received[i]
	}
}

func TestSuite(t *testing.T) {
	// Setup
	log.SetAllLogLevel(volthaTestLogLevel)

	// Test starting server before client
	serverStartsFirstTest(t)

	// Test starting client before server
	clientStartsFirstTest(t)

	// Test server restarts
	serverRestarts(t, 10)

	// Test that the client test the grpc connection on no activity
	testKeepAlive(t)

	// Test client queueing with server limit
	testServerLimit(t)

	// // Test the scenario where a client restarts
	testClientFailure(t, 10)
}
