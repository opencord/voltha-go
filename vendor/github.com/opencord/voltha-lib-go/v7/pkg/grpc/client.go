/*
 * Copyright 2021-present Open Networking Foundation
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
package grpc

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/adapter_service"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/olt_inter_adapter_service"
	"github.com/opencord/voltha-protos/v5/go/onu_inter_adapter_service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
)

type event byte
type state byte
type SetAndTestServiceHandler func(context.Context, *grpc.ClientConn, *common.Connection) interface{}
type RestartedHandler func(ctx context.Context, endPoint string) error

const (
	grpcBackoffInitialInterval = "GRPC_BACKOFF_INITIAL_INTERVAL"
	grpcBackoffMaxInterval     = "GRPC_BACKOFF_MAX_INTERVAL"
	grpcBackoffMaxElapsedTime  = "GRPC_BACKOFF_MAX_ELAPSED_TIME"
	grpcMonitorInterval        = "GRPC_MONITOR_INTERVAL"
)

const (
	DefaultBackoffInitialInterval = 100 * time.Millisecond
	DefaultBackoffMaxInterval     = 5 * time.Second
	DefaultBackoffMaxElapsedTime  = 0 * time.Second // No time limit
	DefaultGRPCMonitorInterval    = 5 * time.Second
)

const (
	eventConnecting = event(iota)
	eventConnected
	eventDisconnected
	eventStopped
	eventError

	stateConnected = state(iota)
	stateConnecting
	stateDisconnected
)

type Client struct {
	clientEndpoint         string
	clientContextData      string
	serverEndPoint         string
	remoteServiceName      string
	connection             *grpc.ClientConn
	connectionLock         sync.RWMutex
	stateLock              sync.RWMutex
	state                  state
	service                interface{}
	events                 chan event
	onRestart              RestartedHandler
	backoffInitialInterval time.Duration
	backoffMaxInterval     time.Duration
	backoffMaxElapsedTime  time.Duration
	monitorInterval        time.Duration
	done                   bool
	livenessLock           sync.RWMutex
	livenessCallback       func(timestamp time.Time)
}

type ClientOption func(*Client)

func ClientContextData(data string) ClientOption {
	return func(args *Client) {
		args.clientContextData = data
	}
}

func NewClient(clientEndpoint, serverEndpoint, remoteServiceName string, onRestart RestartedHandler,
	opts ...ClientOption) (*Client, error) {
	c := &Client{
		clientEndpoint:         clientEndpoint,
		serverEndPoint:         serverEndpoint,
		remoteServiceName:      remoteServiceName,
		onRestart:              onRestart,
		events:                 make(chan event, 5),
		state:                  stateDisconnected,
		backoffInitialInterval: DefaultBackoffInitialInterval,
		backoffMaxInterval:     DefaultBackoffMaxInterval,
		backoffMaxElapsedTime:  DefaultBackoffMaxElapsedTime,
		monitorInterval:        DefaultGRPCMonitorInterval,
	}
	for _, option := range opts {
		option(c)
	}

	// Check for environment variables
	if err := SetFromEnvVariable(grpcBackoffInitialInterval, &c.backoffInitialInterval); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcBackoffInitialInterval})
	}

	if err := SetFromEnvVariable(grpcBackoffMaxInterval, &c.backoffMaxInterval); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcBackoffMaxInterval})
	}

	if err := SetFromEnvVariable(grpcBackoffMaxElapsedTime, &c.backoffMaxElapsedTime); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcBackoffMaxElapsedTime})
	}

	if err := SetFromEnvVariable(grpcMonitorInterval, &c.monitorInterval); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcMonitorInterval})
	}

	logger.Infow(context.Background(), "initialized-client", log.Fields{"client": c})

	// Sanity check
	if c.backoffInitialInterval > c.backoffMaxInterval {
		return nil, fmt.Errorf("initial retry delay %v is greater than maximum retry delay %v", c.backoffInitialInterval, c.backoffMaxInterval)
	}

	return c, nil
}

func (c *Client) GetClient() (interface{}, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no connection to %s", c.serverEndPoint)
	}
	return c.service, nil
}

// GetCoreServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetCoreServiceClient() (core_service.CoreServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no core connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(core_service.CoreServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

// GetOnuAdapterServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetOnuInterAdapterServiceClient() (onu_inter_adapter_service.OnuInterAdapterServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no child adapter connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(onu_inter_adapter_service.OnuInterAdapterServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

// GetOltAdapterServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetOltInterAdapterServiceClient() (olt_inter_adapter_service.OltInterAdapterServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no parent adapter connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(olt_inter_adapter_service.OltInterAdapterServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

// GetAdapterServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetAdapterServiceClient() (adapter_service.AdapterServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no adapter service connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(adapter_service.AdapterServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

func (c *Client) Reset(ctx context.Context) {
	logger.Debugw(ctx, "resetting-client-connection", log.Fields{"endpoint": c.serverEndPoint})
	c.stateLock.Lock()
	defer c.stateLock.Unlock()
	if c.state == stateConnected {
		c.state = stateDisconnected
		c.events <- eventDisconnected
	}
}

// SendWithTimeout runs f and returns its error.  If the deadline d elapses first,
// it returns a grpc DeadlineExceeded error instead.
func SendWithTimeout(f func(*common.Connection) error, conn *common.Connection, d time.Duration) error {
	errChan := make(chan error, 1)
	go func() {
		err := f(conn)
		logger.Debugw(context.Background(), "message-sent", log.Fields{"error": err})
		errChan <- err
		close(errChan)
	}()
	t := time.NewTimer(d)
	select {
	case <-t.C:
		return status.Errorf(codes.DeadlineExceeded, "timeout-on-sending-message")
	case err := <-errChan:
		if !t.Stop() {
			<-t.C
		}
		return err
	}
}

func (c *Client) monitorConnection(ctx context.Context) {
	logger.Infow(ctx, "monitor-connection-started", log.Fields{"endpoint": c.serverEndPoint})

	// If we exit, assume disconnected
	defer func() {
		c.stateLock.Lock()
		if !c.done && c.state == stateConnected {
			c.state = stateDisconnected
			logger.Warnw(ctx, "sending-disconnect-event", log.Fields{"endpoint": c.serverEndPoint, "curr-state": stateConnected, "new-state": c.state})
			c.events <- eventDisconnected
		} else {
			logger.Debugw(ctx, "no-state-change-needed", log.Fields{"endpoint": c.serverEndPoint, "state": c.state, "client-done": c.done})
		}
		logger.Debugw(ctx, "monitor-connection-ended", log.Fields{"endpoint": c.serverEndPoint})
		c.stateLock.Unlock()
	}()

	c.connectionLock.RLock()
	conn := c.connection
	c.connectionLock.RUnlock()
	if conn == nil {
		logger.Errorw(ctx, "connection-nil", log.Fields{"endpoint": c.serverEndPoint})
		return
	}

	// Get a new client using reflection. The server can implement any grpc service, but it
	// needs to also implement the "StartKeepAliveStream" API
	grpcReflectClient := grpcreflect.NewClient(ctx, rpb.NewServerReflectionClient(conn))
	if grpcReflectClient == nil {
		logger.Errorw(ctx, "grpc-reflect-client-nil", log.Fields{"endpoint": c.serverEndPoint})
		return
	}
	// defer grpcReflectClient.Reset()

	// Get the list of services - there should be 2 services: a server reflection and the voltha service we are interested in
	services, err := grpcReflectClient.ListServices()
	if err != nil {
		logger.Errorw(ctx, "list-services-error", log.Fields{"endpoint": c.serverEndPoint, "error": err})
		return
	}

	// Filter out the service
	logger.Debugw(ctx, "services", log.Fields{"services": services})
	serviceOfInterest := ""
	for _, service := range services {
		if strings.Contains(service, c.remoteServiceName) {
			serviceOfInterest = service
		}
	}
	if serviceOfInterest == "" {
		logger.Errorw(ctx, "no-service-found", log.Fields{"endpoint": c.serverEndPoint, "services": services})
		return
	}

	// Resolve the service
	resolvedService, err := grpcReflectClient.ResolveService(serviceOfInterest)
	if err != nil {
		logger.Errorw(ctx, "service-error", log.Fields{"endpoint": c.serverEndPoint, "service": resolvedService, "error": err})
		return
	}

	// Find the method of interest
	method := resolvedService.FindMethodByName("KeepAlive")
	if method == nil {
		logger.Errorw(ctx, "nil-method", log.Fields{"endpoint": c.serverEndPoint, "service": resolvedService})
		return
	}
	logger.Debugw(ctx, "resolved-to-method", log.Fields{"service": resolvedService.GetName(), "method": method.GetName()})

	// Get a dynamic connection
	dynamicConn := grpcdynamic.NewStub(conn)

	// Get the stream and send this client information
	streamCtx, streamDone := context.WithCancel(log.WithSpanFromContext(context.Background(), ctx))
	defer streamDone()
	stream, err := dynamicConn.InvokeRpcClientStream(streamCtx, method)

	// stream, err := dynamicConn.InvokeRpcServerStream(streamCtx, method, myInfo)
	// stream, err := dynamicConn.InvokeRpcServerStream(ctx, method, myInfo)
	if err != nil {
		logger.Errorw(ctx, "stream-error", log.Fields{"endpoint": c.serverEndPoint, "service": resolvedService, "error": err})
		return
	}

	myInfo := &common.Connection{
		Endpoint:          c.clientEndpoint,
		ContextInfo:       c.clientContextData,
		KeepAliveInterval: int64(c.monitorInterval),
	}

loop:
	for {
		// Let's send a keep alive message with our info
		err := SendWithTimeout(func(conn *common.Connection) error { return stream.SendMsg(conn) }, myInfo, c.monitorInterval)
		if err != nil {
			// Any error means the far end is gone
			logger.Errorw(ctx, "sending-stream-error", log.Fields{"error": err, "remote": c.serverEndPoint, "context": stream.Context().Err()})
			break loop
		}
		logger.Debugw(ctx, "stream-data-sent", log.Fields{"remote": c.serverEndPoint})
		// Update liveness, if configured
		c.livenessLock.RLock()
		if c.livenessCallback != nil {
			go c.livenessCallback(time.Now())
		}
		c.livenessLock.RUnlock()

		// Wait to send the next keep alive
		keepAliveTimer := time.NewTimer(time.Duration(myInfo.KeepAliveInterval))
		select {
		case <-ctx.Done():
			logger.Warnw(ctx, "context-done", log.Fields{"remote": c.serverEndPoint})
			break loop
		case <-stream.Context().Done():
			logger.Debugw(ctx, "stream-context-done", log.Fields{"remote": c.serverEndPoint, "stream-info": stream.Context()})
			break loop
		case <-keepAliveTimer.C:
			continue
		}
	}
}

// Start kicks off the adapter agent by trying to connect to the adapter
func (c *Client) Start(ctx context.Context, handler SetAndTestServiceHandler) {
	logger.Debugw(ctx, "Starting GRPC - Client", log.Fields{"api-endpoint": c.serverEndPoint})

	// If the context contains a k8s probe then register services
	p := probe.GetProbeFromContext(ctx)
	if p != nil {
		p.RegisterService(ctx, c.serverEndPoint)
	}

	var monitorConnectionCtx context.Context
	var monitorConnectionDone func()

	initialConnection := true
	c.events <- eventConnecting
	backoff := NewBackoff(c.backoffInitialInterval, c.backoffMaxInterval, c.backoffMaxElapsedTime)
	attempt := 1
loop:
	for {
		select {
		case <-ctx.Done():
			logger.Debugw(ctx, "context-closing", log.Fields{"endpoint": c.serverEndPoint})
			break loop
		case event := <-c.events:
			logger.Debugw(ctx, "received-event", log.Fields{"event": event, "endpoint": c.serverEndPoint})
			c.connectionLock.RLock()
			// On a client stopped, just allow the stop event to go through
			if c.done && event != eventStopped {
				c.connectionLock.RUnlock()
				logger.Debugw(ctx, "ignoring-event-on-client-stop", log.Fields{"event": event, "endpoint": c.serverEndPoint})
				continue
			}
			c.connectionLock.RUnlock()
			switch event {
			case eventConnecting:
				c.stateLock.Lock()
				logger.Debugw(ctx, "connection-start", log.Fields{"endpoint": c.serverEndPoint, "attempts": attempt, "curr-state": c.state})
				if c.state == stateConnected {
					c.state = stateDisconnected
				}
				if c.state != stateConnecting {
					c.state = stateConnecting
					go func() {
						if err := c.connectToEndpoint(ctx, handler, p); err != nil {
							c.stateLock.Lock()
							c.state = stateDisconnected
							c.stateLock.Unlock()
							logger.Errorw(ctx, "connection-failed", log.Fields{"endpoint": c.serverEndPoint, "attempt": attempt, "error": err})

							// Retry connection after a delay
							if err = backoff.Backoff(ctx); err != nil {
								// Context has closed or reached maximum elapsed time, if set
								logger.Errorw(ctx, "retry-aborted", log.Fields{"endpoint": c.serverEndPoint, "error": err})
								return
							}
							attempt += 1
							c.connectionLock.RLock()
							if !c.done {
								c.events <- eventConnecting
							}
							c.connectionLock.RUnlock()
						} else {
							backoff.Reset()
						}
					}()
				}
				c.stateLock.Unlock()

			case eventConnected:
				attempt = 1
				c.stateLock.Lock()
				logger.Debugw(ctx, "endpoint-connected", log.Fields{"endpoint": c.serverEndPoint, "curr-state": c.state})
				if c.state != stateConnected {
					c.state = stateConnected
					monitorConnectionCtx, monitorConnectionDone = context.WithCancel(context.Background())
					go c.monitorConnection(monitorConnectionCtx)
					if initialConnection {
						logger.Debugw(ctx, "initial-endpoint-connection", log.Fields{"endpoint": c.serverEndPoint})
						initialConnection = false
					} else {
						logger.Debugw(ctx, "endpoint-reconnection", log.Fields{"endpoint": c.serverEndPoint})
						// Trigger any callback on a restart
						go func() {
							err := c.onRestart(log.WithSpanFromContext(context.Background(), ctx), c.serverEndPoint)
							if err != nil {
								logger.Errorw(ctx, "unable-to-restart-endpoint", log.Fields{"error": err, "endpoint": c.serverEndPoint})
							}
						}()
					}
				}
				c.stateLock.Unlock()

			case eventDisconnected:
				if p != nil {
					p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusNotReady)
				}
				c.stateLock.RLock()
				logger.Debugw(ctx, "endpoint-disconnected", log.Fields{"endpoint": c.serverEndPoint, "curr-state": c.state, "connectiondDone": monitorConnectionDone})
				c.stateLock.RUnlock()

				// Stop the streaming connection
				if monitorConnectionDone != nil {
					monitorConnectionDone()
					monitorConnectionDone = nil
				}

				// Try to connect again
				c.events <- eventConnecting

			case eventStopped:
				logger.Debugw(ctx, "endpoint-stopped", log.Fields{"adapter": c.serverEndPoint})

				if monitorConnectionDone != nil {
					monitorConnectionDone()
					monitorConnectionDone = nil
				}
				if err := c.closeConnection(ctx, p); err != nil {
					logger.Errorw(ctx, "endpoint-closing-connection-failed", log.Fields{"endpoint": c.serverEndPoint, "error": err})
				}
				break loop
			case eventError:
				logger.Errorw(ctx, "endpoint-error-event", log.Fields{"endpoint": c.serverEndPoint})
			default:
				logger.Errorw(ctx, "endpoint-unknown-event", log.Fields{"endpoint": c.serverEndPoint, "error": event})
			}
		}
	}

	// Stop the streaming connection
	if monitorConnectionDone != nil {
		logger.Debugw(ctx, "closing the connection monitoring", log.Fields{"endpoint": c.serverEndPoint})
		monitorConnectionDone()
	}

	logger.Infow(ctx, "client-stopped", log.Fields{"endpoint": c.serverEndPoint})
}

func (c *Client) connectToEndpoint(ctx context.Context, handler SetAndTestServiceHandler, p *probe.Probe) error {
	if p != nil {
		p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusPreparing)
	}

	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()

	if c.connection != nil {
		_ = c.connection.Close()
		c.connection = nil
	}

	c.service = nil

	// Use Interceptors to:
	// 1. automatically inject
	// 2. publish Open Tracing Spans by this GRPC Client
	// 3. detect connection failure on client calls such that the reconnection process can begin
	conn, err := grpc.Dial(c.serverEndPoint,
		grpc.WithInsecure(),
		grpc.WithStreamInterceptor(grpc_middleware.ChainStreamClient(
			grpc_opentracing.StreamClientInterceptor(grpc_opentracing.WithTracer(log.ActiveTracerProxy{})),
		)),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			grpc_opentracing.UnaryClientInterceptor(grpc_opentracing.WithTracer(log.ActiveTracerProxy{})),
		)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                c.monitorInterval,
			Timeout:             c.backoffMaxInterval,
			PermitWithoutStream: true,
		}),
	)

	if err == nil {
		subCtx, cancel := context.WithTimeout(ctx, c.backoffMaxInterval)
		defer cancel()
		svc := handler(subCtx, conn, &common.Connection{Endpoint: c.clientEndpoint, KeepAliveInterval: int64(c.monitorInterval)})
		if svc != nil {
			c.connection = conn
			c.service = svc
			if p != nil {
				p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusRunning)
			}
			logger.Infow(ctx, "connected-to-endpoint", log.Fields{"endpoint": c.serverEndPoint})
			c.events <- eventConnected
			return nil
		} else {
			logger.Warnw(ctx, "getting-service-from-conn-failed", log.Fields{"endpoint": c.serverEndPoint})
		}
	} else {
		logger.Warnw(ctx, "no-connection-to-endpoint", log.Fields{"endpoint": c.serverEndPoint, "error": err})
	}

	if p != nil {
		p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusFailed)
	}
	return fmt.Errorf("no connection to endpoint %s", c.serverEndPoint)
}

func (c *Client) closeConnection(ctx context.Context, p *probe.Probe) error {
	if p != nil {
		p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusStopped)
	}
	logger.Infow(ctx, "client-closing-connection", log.Fields{"endpoint": c.serverEndPoint})

	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()

	if c.connection != nil {
		err := c.connection.Close()
		c.service = nil
		c.connection = nil
		return err
	}

	return nil
}

func (c *Client) Stop(ctx context.Context) {
	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()
	if !c.done {
		c.done = true
		c.events <- eventStopped
		close(c.events)
	}
	logger.Infow(ctx, "client-stop-request-event-sent", log.Fields{"endpoint": c.serverEndPoint})
}

// SetService is used for testing only
func (c *Client) SetService(srv interface{}) {
	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()
	c.service = srv
}

func (c *Client) SubscribeForLiveness(callback func(timestamp time.Time)) {
	c.livenessLock.Lock()
	defer c.livenessLock.Unlock()
	c.livenessCallback = callback
}
