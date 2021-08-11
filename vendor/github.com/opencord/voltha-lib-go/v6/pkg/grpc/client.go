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
	"github.com/opencord/voltha-lib-go/v6/pkg/log"
	"github.com/opencord/voltha-lib-go/v6/pkg/probe"
	"github.com/opencord/voltha-protos/v4/go/adapter_services"
	"github.com/opencord/voltha-protos/v4/go/core"
	"google.golang.org/grpc"
)

type event byte
type state byte
type SetAndTestServiceHandler func(context.Context, *grpc.ClientConn) interface{}
type RestartedHandler func(ctx context.Context, endPoint string) error

const (
	grpcBackoffInitialInterval = "GRPC_BACKOFF_INITIAL_INTERVAL"
	grpcBackoffMaxInterval     = "GRPC_BACKOFF_MAX_INTERVAL"
	grpcBackoffMaxElapsedTime  = "GRPC_BACKOFF_MAX_ELAPSED_TIME"
)

const (
	DefaultBackoffInitialInterval = 100 * time.Millisecond
	DefaultBackoffMaxInterval     = 5 * time.Second
	DefaultBackoffMaxElapsedTime  = 0 * time.Second // No time limit
	DefaultActivityProbeInterval  = 30 * time.Second
)

const (
	connectionErrorSubString  = "SubConns are in TransientFailure"
	connectionClosedSubstring = "client connection is closing"
	connectionError           = "connection error"
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
	apiEndPoint            string
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
	activityProbeInterval  time.Duration
	activeCh               chan struct{}
	activeChMutex          sync.RWMutex
	done                   bool
	livenessCallback       func(timestamp time.Time)
}

type ClientOption func(*Client)

func ActivityCheck(enable bool) ClientOption {
	return func(args *Client) {
		args.activityCheck = enable
	}
}

func ActivityProbeInterval(interval time.Duration) ClientOption {
	return func(args *Client) {
		args.activityProbeInterval = interval
	}
}

func NewClient(endpoint string, onRestart RestartedHandler, opts ...ClientOption) (*Client, error) {
	c := &Client{
		apiEndPoint:            endpoint,
		onRestart:              onRestart,
		events:                 make(chan event, 1),
		state:                  stateDisconnected,
		backoffInitialInterval: DefaultBackoffInitialInterval,
		backoffMaxInterval:     DefaultBackoffMaxInterval,
		backoffMaxElapsedTime:  DefaultBackoffMaxElapsedTime,
		activityProbeInterval:  DefaultActivityProbeInterval,
	}
	for _, option := range opts {
		option(c)
	}

	// Check for environment variables
	if err := SetFromEnvVariable(grpcBackoffInitialInterval, &c.backoffInitialInterval); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcBackoffInitialInterval})
	}

	if err := SetFromEnvVariable(grpcBackoffMaxInterval, &c.backoffInitialInterval); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcBackoffMaxInterval})
	}

	if err := SetFromEnvVariable(grpcBackoffMaxElapsedTime, &c.backoffMaxElapsedTime); err != nil {
		logger.Warnw(context.Background(), "failure-reading-env-variable", log.Fields{"error": err, "variable": grpcBackoffMaxElapsedTime})
	}

	logger.Infow(context.Background(), "initialized-client", log.Fields{"client": c})

	// Sanity check
	if c.backoffInitialInterval > c.backoffMaxInterval {
		return nil, fmt.Errorf("minimum retry delay %v is greater than maximum retry delay %v", c.backoffInitialInterval, c.backoffMaxInterval)
	}

	return c, nil
}

func (c *Client) GetClient() (interface{}, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no connection to %s", c.apiEndPoint)
	}
	return c.service, nil
}

// GetCoreServiceClient is a helper function that returns a concrete service instead of the GetClient() API
// which returns an interface
func (c *Client) GetCoreServiceClient() (core.CoreServiceClient, error) {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()
	if c.service == nil {
		return nil, fmt.Errorf("no core connection to %s", c.apiEndPoint)
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
		return nil, fmt.Errorf("no child adapter connection to %s", c.apiEndPoint)
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
		return nil, fmt.Errorf("no parent adapter connection to %s", c.apiEndPoint)
	}
	client, ok := c.service.(adapter_services.OltInterAdapterServiceClient)
	if ok {
		return client, nil
	}
	return nil, fmt.Errorf("invalid-service-%s", reflect.TypeOf(c.service))
}

func (c *Client) clientInterceptor(ctx context.Context, method string, req interface{}, reply interface{},
	cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	// Nothing to do before intercepting the call
	err := invoker(ctx, method, req, reply, cc, opts...)
	// On connection failure, start the reconnect process depending on the error response
	if err != nil {
		if strings.Contains(err.Error(), connectionErrorSubString) || strings.Contains(err.Error(), connectionError) {
			c.stateLock.Lock()
			if c.state == stateConnected {
				logger.Warnw(context.Background(), "sending-disconnect-event", log.Fields{"endpoint": c.apiEndPoint, "error": err})
				c.state = stateDisconnected
				c.events <- eventDisconnected
			}
			c.stateLock.Unlock()
		} else if strings.Contains(err.Error(), connectionClosedSubstring) {
			logger.Errorw(context.Background(), "invalid-client-connection-closed", log.Fields{"endpoint": c.apiEndPoint, "error": err})
		}
	}
	c.updateActivity(ctx)
	return err
}

// updateActivity pushes an activity indication on the channel so that the monitoring routine does not validate
// the gRPC connection when the connection is being used. Note that this update is done both when the connection
// is alive or a connection error is returned. A separate routine takes care of doing the re-connect.
func (c *Client) updateActivity(ctx context.Context) {
	c.activeChMutex.RLock()
	defer c.activeChMutex.RUnlock()
	if c.activeCh != nil {
		logger.Debugw(ctx, "update-activity", log.Fields{"api-endpoint": c.apiEndPoint})
		c.activeCh <- struct{}{}

		// Update liveness only in connected state
		if c.livenessCallback != nil {
			c.stateLock.RLock()
			if c.state == stateConnected {
				c.livenessCallback(time.Now())
			}
			c.stateLock.RUnlock()
		}
	}
}

// monitorActivity monitors the activity on the gRPC connection.   If there are no activity after a specified
// timeout, it will send a default API request on that connection.   If the connection is good then nothing
// happens.  If it's bad this will trigger reconnection attempts.
func (c *Client) monitorActivity(ctx context.Context, handler SetAndTestServiceHandler) {
	logger.Infow(ctx, "start-activity-monitor", log.Fields{"endpoint": c.apiEndPoint})

	// Create an activity monitor channel
	c.activeChMutex.Lock()
	c.activeCh = make(chan struct{}, 10)
	c.activeChMutex.Unlock()

	// Interval to wait for no activity before probing the connection
	timeout := c.activityProbeInterval
loop:
	for {
		timeoutTimer := time.NewTimer(timeout)
		select {

		case <-c.activeCh:
			logger.Debugw(ctx, "received-active-notification", log.Fields{"endpoint": c.apiEndPoint})

			// Reset timer
			if !timeoutTimer.Stop() {
				<-timeoutTimer.C
			}

		case <-ctx.Done():
			break loop

		case <-timeoutTimer.C:
			logger.Infow(ctx, "idle-timeout", log.Fields{"endpoint": c.apiEndPoint})

			// Trigger an activity check if the state is connected.  If the state is not connected then there is already
			// a backoff retry mechanism in place to retry establishing connection.
			c.stateLock.RLock()
			runCheck := c.state == stateConnected
			c.stateLock.RUnlock()
			if runCheck {
				go func() {
					logger.Infow(ctx, "checking-connection", log.Fields{"api-endpoint": c.apiEndPoint})

					subCtx, cancel := context.WithTimeout(ctx, c.backoffInitialInterval)
					defer cancel()
					c.connectionLock.RLock()
					defer c.connectionLock.RUnlock()
					if c.connection != nil {
						handler(subCtx, c.connection)
					}
				}()
			}
		}
	}
	logger.Infow(ctx, "activity-monitor-stopping", log.Fields{"endpoint": c.apiEndPoint})
}

// Start kicks off the adapter agent by trying to connect to the adapter
func (c *Client) Start(ctx context.Context, handler SetAndTestServiceHandler) {
	logger.Debugw(ctx, "Starting GRPC - Client", log.Fields{"api-endpoint": c.apiEndPoint})

	// If the context contains a k8s probe then register services
	p := probe.GetProbeFromContext(ctx)
	if p != nil {
		p.RegisterService(ctx, c.apiEndPoint)
	}

	// Enable activity check, if required
	if c.activityCheck {
		go c.monitorActivity(ctx, handler)
	}

	initialConnection := true
	c.events <- eventConnecting
	backoff := NewBackoff(c.backoffInitialInterval, c.backoffMaxInterval, c.backoffMaxElapsedTime)
	attempt := 1
loop:
	for {
		select {
		case <-ctx.Done():
			logger.Debugw(ctx, "context-closing", log.Fields{"endpoint": c.apiEndPoint})
			return
		case event := <-c.events:
			logger.Debugw(ctx, "received-event", log.Fields{"event": event, "endpoint": c.apiEndPoint})
			switch event {
			case eventConnecting:
				logger.Debugw(ctx, "connection-start", log.Fields{"endpoint": c.apiEndPoint, "attempts": attempt})

				c.stateLock.Lock()
				if c.state == stateConnected {
					c.state = stateDisconnected
				}
				if c.state != stateConnecting {
					c.state = stateConnecting
					go func() {
						if err := c.connectToEndpoint(ctx, handler, p); err != nil {
							c.state = stateDisconnected
							logger.Errorw(ctx, "connection-failed", log.Fields{"endpoint": c.apiEndPoint, "attempt": attempt, "error": err})

							// Retry connection after a delay
							if err = backoff.Backoff(ctx); err != nil {
								// Context has closed or reached maximum elapsed time, if set
								logger.Errorw(ctx, "retry-aborted", log.Fields{"endpoint": c.apiEndPoint, "error": err})
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
				logger.Debugw(ctx, "endpoint-connected", log.Fields{"endpoint": c.apiEndPoint})
				attempt = 1
				c.stateLock.Lock()
				if c.state != stateConnected {
					c.state = stateConnected
					if initialConnection {
						logger.Debugw(ctx, "initial-endpoint-connection", log.Fields{"endpoint": c.apiEndPoint})
						initialConnection = false
					} else {
						logger.Debugw(ctx, "endpoint-reconnection", log.Fields{"endpoint": c.apiEndPoint})
						// Trigger any callback on a restart
						go func() {
							err := c.onRestart(log.WithSpanFromContext(context.Background(), ctx), c.apiEndPoint)
							if err != nil {
								logger.Errorw(ctx, "unable-to-restart-endpoint", log.Fields{"error": err, "endpoint": c.apiEndPoint})
							}
						}()
					}
				}
				c.stateLock.Unlock()

			case eventDisconnected:
				if p != nil {
					p.UpdateStatus(ctx, c.apiEndPoint, probe.ServiceStatusNotReady)
				}
				logger.Debugw(ctx, "endpoint-disconnected", log.Fields{"endpoint": c.apiEndPoint, "status": c.state})

				// Try to connect again
				c.events <- eventConnecting

			case eventStopped:
				logger.Debugw(ctx, "endPoint-stopped", log.Fields{"adapter": c.apiEndPoint})
				go func() {
					if err := c.closeConnection(ctx, p); err != nil {
						logger.Errorw(ctx, "endpoint-closing-connection-failed", log.Fields{"endpoint": c.apiEndPoint, "error": err})
					}
				}()
				break loop
			case eventError:
				logger.Errorw(ctx, "endpoint-error-event", log.Fields{"endpoint": c.apiEndPoint})
			default:
				logger.Errorw(ctx, "endpoint-unknown-event", log.Fields{"endpoint": c.apiEndPoint, "error": event})
			}
		}
	}
	logger.Infow(ctx, "endpoint-stopped", log.Fields{"endpoint": c.apiEndPoint})
}

func (c *Client) connectToEndpoint(ctx context.Context, handler SetAndTestServiceHandler, p *probe.Probe) error {
	if p != nil {
		p.UpdateStatus(ctx, c.apiEndPoint, probe.ServiceStatusPreparing)
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
	conn, err := grpc.Dial(c.apiEndPoint,
		grpc.WithInsecure(),
		grpc.WithStreamInterceptor(grpc_middleware.ChainStreamClient(
			grpc_opentracing.StreamClientInterceptor(grpc_opentracing.WithTracer(log.ActiveTracerProxy{})),
		)),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			grpc_opentracing.UnaryClientInterceptor(grpc_opentracing.WithTracer(log.ActiveTracerProxy{})),
		)),
		grpc.WithUnaryInterceptor(c.clientInterceptor))

	if err == nil {
		subCtx, cancel := context.WithTimeout(ctx, c.backoffMaxInterval)
		defer cancel()
		svc := handler(subCtx, conn)
		if svc != nil {
			c.connection = conn
			c.service = svc
			if p != nil {
				p.UpdateStatus(ctx, c.apiEndPoint, probe.ServiceStatusRunning)
			}
			logger.Infow(ctx, "connected-to-endpoint", log.Fields{"endpoint": c.apiEndPoint})
			c.events <- eventConnected
			return nil
		}
	}
	logger.Warnw(ctx, "Failed to connect to endpoint",
		log.Fields{
			"endpoint": c.apiEndPoint,
			"error":    err,
		})

	if p != nil {
		p.UpdateStatus(ctx, c.apiEndPoint, probe.ServiceStatusFailed)
	}
	return fmt.Errorf("no connection to endpoint %s", c.apiEndPoint)
}

func (c *Client) closeConnection(ctx context.Context, p *probe.Probe) error {
	if p != nil {
		p.UpdateStatus(ctx, c.apiEndPoint, probe.ServiceStatusStopped)
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
	if !c.done {
		c.events <- eventStopped
		close(c.events)
		c.done = true
	}
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
