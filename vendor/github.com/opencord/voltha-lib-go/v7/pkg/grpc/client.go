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
	"github.com/opencord/voltha-protos/v5/go/adapter_services"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/core"
	"google.golang.org/grpc"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

type event byte
type state byte
type SetAndTestServiceHandler func(context.Context, *grpc.ClientConn) interface{}
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
	localEndpoint          string
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
	activityCheck          bool
	monitorInterval        time.Duration
	done                   bool
	livenessCallback       func(timestamp time.Time)
}

type ClientOption func(*Client)

func ActivityCheck(enable bool) ClientOption {
	return func(args *Client) {
		args.activityCheck = enable
	}
}

func NewClient(localEndpoint, serverEndpoint, remoteServiceName string, onRestart RestartedHandler, opts ...ClientOption) (*Client, error) {
	c := &Client{
		localEndpoint:          localEndpoint,
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
func (c *Client) GetCoreServiceClient() (core.CoreServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no core connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(core.CoreServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

// GetOnuAdapterServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetOnuInterAdapterServiceClient() (adapter_services.OnuInterAdapterServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no child adapter connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(adapter_services.OnuInterAdapterServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

// GetOltAdapterServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetOltInterAdapterServiceClient() (adapter_services.OltInterAdapterServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no parent adapter connection to %s", c.serverEndPoint)
	}
	client, ok := c.service.(adapter_services.OltInterAdapterServiceClient)
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

func (c *Client) monitorConnection(ctx context.Context) {
	span, ctx := log.CreateChildSpan(ctx, "monitor-connection")
	defer span.Finish()

	logger.Debugw(ctx, "monitor-connection-started", log.Fields{"endpoint": c.serverEndPoint})

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

	// c.connectionLock.RLock()
	// conn := c.connection
	// c.connectionLock.RUnlock()
	// if conn == nil {
	// 	logger.Errorw(ctx, "connection-nil", log.Fields{"endpoint": c.serverEndPoint})
	// 	return
	// }
	localConn, err := grpc.Dial(c.serverEndPoint,
		grpc.WithInsecure(),
	)
	if err != nil {
		logger.Errorw(ctx, "still-no-conn", log.Fields{"endpoint": c.serverEndPoint, "error": err})
		return
	}
	defer localConn.Close()

	// Get a new client using reflection. The server can implement any grpc service, but it
	// needs to also implement the "KeepAliveConnection" API
	grpcReflectClient := grpcreflect.NewClient(ctx, rpb.NewServerReflectionClient(localConn))
	if grpcReflectClient == nil {
		logger.Errorw(ctx, "grpc-reflect-client-nil", log.Fields{"endpoint": c.serverEndPoint})
		return
	}

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
		logger.Errorw(ctx, "serviuce-error", log.Fields{"endpoint": c.serverEndPoint, "service": resolvedService, "error": err})
		return
	}

	// Find the method of interest
	method := resolvedService.FindMethodByName("KeepAliveConnection")
	if method == nil {
		logger.Errorw(ctx, "nil-method", log.Fields{"endpoint": c.serverEndPoint, "service": resolvedService})
		return
	}
	logger.Debugw(ctx, "resolved-to-method", log.Fields{"service": resolvedService.GetName(), "method": method.GetName()})

	// Get a dynamic connection
	dynamicConn := grpcdynamic.NewStub(localConn)

	// Get the stream and send this client information
	myInfo := &common.Connection{
		Endpoint:          c.localEndpoint,
		KeepAliveInterval: int64(c.monitorInterval),
	}
	streamCtx, streamDone := context.WithCancel(log.WithSpanFromContext(context.Background(), ctx))
	defer streamDone()
	stream, err := dynamicConn.InvokeRpcServerStream(streamCtx, method, myInfo)
	if err != nil {
		logger.Errorw(ctx, "stream-error", log.Fields{"endpoint": c.serverEndPoint, "service": resolvedService, "error": err})
		return
	}
loop:
	for {
		select {
		case <-ctx.Done():
			logger.Warn(ctx, "context-done", log.Fields{"remote": c.serverEndPoint})
			break loop
		case <-stream.Context().Done():
			logger.Debugw(ctx, "stream-context-done", log.Fields{"remote": c.serverEndPoint, "stream-info": stream.Context()})
		default:
			logger.Debugw(ctx, "waiting-to-receive-stream-data", log.Fields{"remote": c.serverEndPoint, "stream-info": stream.Context()})
			data, err := stream.RecvMsg()
			if err != nil {
				// Anything error means the far end is gone
				logger.Errorw(ctx, "received-stream-error", log.Fields{"error": err, "data": data, "remote": c.serverEndPoint})
				break loop
			}
			// Update liveness, if configured
			if c.livenessCallback != nil {
				c.livenessCallback(time.Now())
			}

			logger.Debugw(ctx, "received-stream-data", log.Fields{"data": data})
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

	var connectionCtx context.Context
	var connectionDone func()

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
			// On a client stopped, just allow the stop event to go through
			c.connectionLock.RLock()
			if c.done && event != eventStopped {
				c.connectionLock.RUnlock()
				logger.Errorw(ctx, "ignoring-event-on-stop", log.Fields{"event": event, "endpoint": c.serverEndPoint})
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
							c.events <- eventConnecting
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
					connectionCtx, connectionDone = context.WithCancel(context.Background())
					go c.monitorConnection(connectionCtx)
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
				logger.Debugw(ctx, "endpoint-disconnected", log.Fields{"endpoint": c.serverEndPoint, "curr-state": c.state})
				c.stateLock.RUnlock()

				// Stop the streaming connection
				if connectionDone != nil {
					connectionDone()
				}

				// Try to connect again
				c.events <- eventConnecting

			case eventStopped:
				logger.Debugw(ctx, "endPoint-stopped", log.Fields{"adapter": c.serverEndPoint})
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
	if connectionDone != nil {
		connectionDone()
	}

	logger.Infow(ctx, "endpoint-client-ended", log.Fields{"endpoint": c.serverEndPoint})
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
	)

	if err == nil {
		subCtx, cancel := context.WithTimeout(ctx, c.backoffMaxInterval)
		defer cancel()
		svc := handler(subCtx, conn)
		if svc != nil {
			c.connection = conn
			c.service = svc
			if p != nil {
				p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusRunning)
			}
			logger.Infow(ctx, "connected-to-endpoint", log.Fields{"endpoint": c.serverEndPoint})
			c.events <- eventConnected
			return nil
		}
	}
	logger.Warnw(ctx, "Failed to connect to endpoint",
		log.Fields{
			"endpoint": c.serverEndPoint,
			"error":    err,
		})

	if p != nil {
		p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusFailed)
	}
	return fmt.Errorf("no connection to endpoint %s", c.serverEndPoint)
}

func (c *Client) closeConnection(ctx context.Context, p *probe.Probe) error {
	if p != nil {
		p.UpdateStatus(ctx, c.serverEndPoint, probe.ServiceStatusStopped)
	}

	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()

	if c.connection != nil {
		err := c.connection.Close()
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
	logger.Debug(ctx, ".............. Stop done ..........")
}

// SetService is used for testing only
func (c *Client) SetService(srv interface{}) {
	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()
	c.service = srv
}

func (c *Client) SubscribeForLiveness(callback func(timestamp time.Time)) {
	c.livenessCallback = callback
}
