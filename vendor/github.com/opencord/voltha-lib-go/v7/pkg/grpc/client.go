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
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
	"github.com/opencord/voltha-protos/v5/go/common"
	"github.com/opencord/voltha-protos/v5/go/core_service"
	"github.com/opencord/voltha-protos/v5/go/olt_inter_adapter_service"
	"github.com/opencord/voltha-protos/v5/go/onu_inter_adapter_service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

type event byte
type state byte
type SetAndTestServiceHandler func(context.Context, *grpc.ClientConn, *common.Connection) interface{}
type RestartedHandler func(ctx context.Context, endPoint string) error

type contextKey string

func (c contextKey) String() string {
	return string(c)
}

var (
	grpcMonitorContextKey = contextKey("grpc-monitor")
)

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
	connectionErrorSubString  = "SubConns are in TransientFailure"
	connectionClosedSubstring = "client connection is closing"
	connectionError           = "connection error"
	connectionSystemNotReady  = "system is not ready"
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
	serverEndPoint         string
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
	livenessCallback       func(timestamp time.Time)
}

type ClientOption func(*Client)

func NewClient(clientEndpoint, serverEndpoint string, onRestart RestartedHandler, opts ...ClientOption) (*Client, error) {
	c := &Client{
		clientEndpoint:         clientEndpoint,
		serverEndPoint:         serverEndpoint,
		onRestart:              onRestart,
		events:                 make(chan event, 1),
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

func (c *Client) Reset(ctx context.Context) {
	logger.Debugw(ctx, "resetting-client-connection", log.Fields{"endpoint": c.serverEndPoint})
	c.stateLock.Lock()
	defer c.stateLock.Unlock()
	if c.state == stateConnected {
		c.state = stateDisconnected
		c.events <- eventDisconnected
	}
}

func (c *Client) clientInterceptor(ctx context.Context, method string, req interface{}, reply interface{},
	cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	// Nothing to do before intercepting the call
	err := invoker(ctx, method, req, reply, cc, opts...)
	// On connection failure, start the reconnect process depending on the error response
	if err != nil {
		logger.Errorw(ctx, "received-error", log.Fields{"error": err, "context": ctx, "endpoint": c.serverEndPoint})
		if strings.Contains(err.Error(), connectionErrorSubString) ||
			strings.Contains(err.Error(), connectionError) ||
			strings.Contains(err.Error(), connectionSystemNotReady) ||
			isGrpcMonitorKeyPresentInContext(ctx) {
			c.stateLock.Lock()
			if c.state == stateConnected {
				c.state = stateDisconnected
				logger.Warnw(context.Background(), "sending-disconnect-event", log.Fields{"endpoint": c.serverEndPoint, "error": err, "curr-state": stateConnected, "new-state": c.state})
				c.events <- eventDisconnected
			}
			c.stateLock.Unlock()
		} else if strings.Contains(err.Error(), connectionClosedSubstring) {
			logger.Errorw(context.Background(), "invalid-client-connection-closed", log.Fields{"endpoint": c.serverEndPoint, "error": err})
		}
		return err
	}
	// Update activity on success only
	c.updateActivity(ctx)
	return nil
}

// updateActivity updates the liveness channel
func (c *Client) updateActivity(ctx context.Context) {
	logger.Debugw(ctx, "update-activity", log.Fields{"api-endpoint": c.serverEndPoint})

	// Update liveness only in connected state
	if c.livenessCallback != nil {
		c.stateLock.RLock()
		if c.state == stateConnected {
			c.livenessCallback(time.Now())
		}
		c.stateLock.RUnlock()
	}
}

func WithGrpcMonitorContext(ctx context.Context, name string) context.Context {
	ctx = context.WithValue(ctx, grpcMonitorContextKey, name)
	return ctx
}

func isGrpcMonitorKeyPresentInContext(ctx context.Context) bool {
	if ctx != nil {
		_, present := ctx.Value(grpcMonitorContextKey).(string)
		return present
	}
	return false
}

// monitorActivity monitors the activity on the gRPC connection.   If there are no activity after a specified
// timeout, it will send a default API request on that connection.   If the connection is good then nothing
// happens.  If it's bad this will trigger reconnection attempts.
func (c *Client) monitorActivity(ctx context.Context, handler SetAndTestServiceHandler) {
	logger.Infow(ctx, "start-activity-monitor", log.Fields{"endpoint": c.serverEndPoint})

	grpcMonitorCheckRunning := false
	var grpcMonitorCheckRunningLock sync.RWMutex

	// Interval to wait for no activity before probing the connection
	timeout := c.monitorInterval
loop:
	for {
		timeoutTimer := time.NewTimer(timeout)
		select {

		case <-ctx.Done():
			// Stop and drain timer
			if !timeoutTimer.Stop() {
				select {
				case <-timeoutTimer.C:
				default:
				}
			}
			break loop

		case <-timeoutTimer.C:
			// Trigger an activity check if the state is connected.  If the state is not connected then there is already
			// a backoff retry mechanism in place to retry establishing connection.
			c.stateLock.RLock()
			grpcMonitorCheckRunningLock.RLock()
			runCheck := (c.state == stateConnected) && !grpcMonitorCheckRunning
			grpcMonitorCheckRunningLock.RUnlock()
			c.stateLock.RUnlock()
			if runCheck {
				go func() {
					grpcMonitorCheckRunningLock.Lock()
					if grpcMonitorCheckRunning {
						grpcMonitorCheckRunningLock.Unlock()
						logger.Debugw(ctx, "connection-check-already-in-progress", log.Fields{"api-endpoint": c.serverEndPoint})
						return
					}
					grpcMonitorCheckRunning = true
					grpcMonitorCheckRunningLock.Unlock()

					logger.Debugw(ctx, "connection-check-start", log.Fields{"api-endpoint": c.serverEndPoint})
					subCtx, cancel := context.WithTimeout(ctx, c.backoffMaxInterval)
					defer cancel()
					subCtx = WithGrpcMonitorContext(subCtx, "grpc-monitor")
					c.connectionLock.RLock()
					defer c.connectionLock.RUnlock()
					if c.connection != nil {
						response := handler(subCtx, c.connection, &common.Connection{Endpoint: c.clientEndpoint, KeepAliveInterval: int64(c.monitorInterval)})
						logger.Debugw(ctx, "connection-check-response", log.Fields{"api-endpoint": c.serverEndPoint, "up": response != nil})
					}
					grpcMonitorCheckRunningLock.Lock()
					grpcMonitorCheckRunning = false
					grpcMonitorCheckRunningLock.Unlock()
				}()
			}
		}
	}
	logger.Infow(ctx, "activity-monitor-stopping", log.Fields{"endpoint": c.serverEndPoint})
}

// Start kicks off the adapter agent by trying to connect to the adapter
func (c *Client) Start(ctx context.Context, handler SetAndTestServiceHandler) {
	logger.Debugw(ctx, "Starting GRPC - Client", log.Fields{"api-endpoint": c.serverEndPoint})

	// If the context contains a k8s probe then register services
	p := probe.GetProbeFromContext(ctx)
	if p != nil {
		p.RegisterService(ctx, c.serverEndPoint)
	}

	// Enable activity check
	go c.monitorActivity(ctx, handler)

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

				// Try to connect again
				c.events <- eventConnecting

			case eventStopped:
				logger.Debugw(ctx, "endPoint-stopped", log.Fields{"adapter": c.serverEndPoint})
				go func() {
					if err := c.closeConnection(ctx, p); err != nil {
						logger.Errorw(ctx, "endpoint-closing-connection-failed", log.Fields{"endpoint": c.serverEndPoint, "error": err})
					}
				}()
				break loop
			case eventError:
				logger.Errorw(ctx, "endpoint-error-event", log.Fields{"endpoint": c.serverEndPoint})
			default:
				logger.Errorw(ctx, "endpoint-unknown-event", log.Fields{"endpoint": c.serverEndPoint, "error": event})
			}
		}
	}
	logger.Infow(ctx, "endpoint-stopped", log.Fields{"endpoint": c.serverEndPoint})
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
		grpc.WithUnaryInterceptor(c.clientInterceptor),
		// Set keealive parameter - use default grpc values
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
	logger.Infow(ctx, "client-stopped", log.Fields{"endpoint": c.serverEndPoint})
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
