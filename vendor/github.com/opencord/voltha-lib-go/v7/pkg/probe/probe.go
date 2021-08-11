/*
 * Copyright 2019-present Open Networking Foundation
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
package probe

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/opencord/voltha-lib-go/v7/pkg/log"
)

// ProbeContextKey used to fetch the Probe instance from a context
type ProbeContextKeyType string

// ServiceStatus typed values for service status
type ServiceStatus int

const (
	// ServiceStatusUnknown initial state of services
	ServiceStatusUnknown ServiceStatus = iota

	// ServiceStatusPreparing to optionally be used for prep, such as connecting
	ServiceStatusPreparing

	// ServiceStatusPrepared to optionally be used when prep is complete, but before run
	ServiceStatusPrepared

	// ServiceStatusRunning service is functional
	ServiceStatusRunning

	// ServiceStatusStopped service has stopped, but not because of error
	ServiceStatusStopped

	// ServiceStatusFailed service has stopped because of an error
	ServiceStatusFailed

	// ServiceStatusNotReady service has started but is unable to accept requests
	ServiceStatusNotReady
)

const (
	// ProbeContextKey value of context key to fetch probe
	ProbeContextKey = ProbeContextKeyType("status-update-probe")
)

// String convert ServiceStatus values to strings
func (s ServiceStatus) String() string {
	switch s {
	default:
		fallthrough
	case ServiceStatusUnknown:
		return "Unknown"
	case ServiceStatusPreparing:
		return "Preparing"
	case ServiceStatusPrepared:
		return "Prepared"
	case ServiceStatusRunning:
		return "Running"
	case ServiceStatusStopped:
		return "Stopped"
	case ServiceStatusFailed:
		return "Failed"
	case ServiceStatusNotReady:
		return "NotReady"
	}
}

// ServiceStatusUpdate status update event
type ServiceStatusUpdate struct {
	Name   string
	Status ServiceStatus
}

// Probe reciever on which to implement probe capabilities
type Probe struct {
	readyFunc  func(map[string]ServiceStatus) bool
	healthFunc func(map[string]ServiceStatus) bool

	mutex     sync.RWMutex
	status    map[string]ServiceStatus
	isReady   bool
	isHealthy bool
}

// WithReadyFunc override the default ready calculation function
func (p *Probe) WithReadyFunc(readyFunc func(map[string]ServiceStatus) bool) *Probe {
	p.readyFunc = readyFunc
	return p
}

// WithHealthFunc override the default health calculation function
func (p *Probe) WithHealthFunc(healthFunc func(map[string]ServiceStatus) bool) *Probe {
	p.healthFunc = healthFunc
	return p
}

// RegisterService register one or more service names with the probe, status will be track against service name
func (p *Probe) RegisterService(ctx context.Context, names ...string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.status == nil {
		p.status = make(map[string]ServiceStatus)
	}
	for _, name := range names {
		if _, ok := p.status[name]; !ok {
			p.status[name] = ServiceStatusUnknown
			logger.Debugw(ctx, "probe-service-registered", log.Fields{"service-name": name})
		}
	}

	if p.readyFunc != nil {
		p.isReady = p.readyFunc(p.status)
	} else {
		p.isReady = defaultReadyFunc(p.status)
	}

	if p.healthFunc != nil {
		p.isHealthy = p.healthFunc(p.status)
	} else {
		p.isHealthy = defaultHealthFunc(p.status)
	}
}

// UpdateStatus utility function to send a service update to the probe
func (p *Probe) UpdateStatus(ctx context.Context, name string, status ServiceStatus) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.status == nil {
		p.status = make(map[string]ServiceStatus)
	}

	// if status hasn't changed, avoid doing useless work
	existingStatus, ok := p.status[name]
	if ok && (existingStatus == status) {
		return
	}

	p.status[name] = status
	if p.readyFunc != nil {
		p.isReady = p.readyFunc(p.status)
	} else {
		p.isReady = defaultReadyFunc(p.status)
	}

	if p.healthFunc != nil {
		p.isHealthy = p.healthFunc(p.status)
	} else {
		p.isHealthy = defaultHealthFunc(p.status)
	}
	logger.Debugw(ctx, "probe-service-status-updated",
		log.Fields{
			"service-name": name,
			"status":       status.String(),
			"ready":        p.isReady,
			"health":       p.isHealthy,
		})
}

func (p *Probe) GetStatus(name string) ServiceStatus {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.status == nil {
		p.status = make(map[string]ServiceStatus)
	}

	currentStatus, ok := p.status[name]
	if ok {
		return currentStatus
	}

	return ServiceStatusUnknown
}

func GetProbeFromContext(ctx context.Context) *Probe {
	if ctx != nil {
		if value := ctx.Value(ProbeContextKey); value != nil {
			if p, ok := value.(*Probe); ok {
				return p
			}
		}
	}
	return nil
}

// UpdateStatusFromContext a convenience function to pull the Probe reference from the
// Context, if it exists, and then calling UpdateStatus on that Probe reference. If Context
// is nil or if a Probe reference is not associated with the ProbeContextKey then nothing
// happens
func UpdateStatusFromContext(ctx context.Context, name string, status ServiceStatus) {
	p := GetProbeFromContext(ctx)
	if p != nil {
		p.UpdateStatus(ctx, name, status)
	}
}

// pulled out to a function to help better enable unit testing
func (p *Probe) readzFunc(w http.ResponseWriter, req *http.Request) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.isReady {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusTeapot)
	}
}
func (p *Probe) healthzFunc(w http.ResponseWriter, req *http.Request) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.isHealthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusTeapot)
	}
}
func (p *Probe) detailzFunc(w http.ResponseWriter, req *http.Request) {
	ctx := context.Background()
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte("{")); err != nil {
		logger.Errorw(ctx, "write-response", log.Fields{"error": err})
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	comma := ""
	for c, s := range p.status {
		if _, err := w.Write([]byte(fmt.Sprintf("%s\"%s\": \"%s\"", comma, c, s.String()))); err != nil {
			logger.Errorw(ctx, "write-response", log.Fields{"error": err})
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		comma = ", "
	}
	if _, err := w.Write([]byte("}")); err != nil {
		logger.Errorw(ctx, "write-response", log.Fields{"error": err})
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ListenAndServe implements 3 HTTP endpoints on the given port for healthz, readz, and detailz. Returns only on error
func (p *Probe) ListenAndServe(ctx context.Context, address string) {
	mux := http.NewServeMux()

	// Returns the result of the readyFunc calculation
	mux.HandleFunc("/readz", p.readzFunc)

	// Returns the result of the healthFunc calculation
	mux.HandleFunc("/healthz", p.healthzFunc)

	// Returns the details of the services and their status as JSON
	mux.HandleFunc("/detailz", p.detailzFunc)
	s := &http.Server{
		Addr:    address,
		Handler: mux,
	}
	logger.Fatal(ctx, s.ListenAndServe())
}

func (p *Probe) IsReady() bool {
	return p.isReady
}

// defaultReadyFunc if all services are running then ready, else not
func defaultReadyFunc(services map[string]ServiceStatus) bool {
	if len(services) == 0 {
		return false
	}
	for _, status := range services {
		if status != ServiceStatusRunning {
			return false
		}
	}
	return true
}

// defaultHealthFunc if no service is stopped or failed, then healthy, else not.
// service is start as unknown, so they are considered healthy
func defaultHealthFunc(services map[string]ServiceStatus) bool {
	if len(services) == 0 {
		return false
	}
	for _, status := range services {
		if status == ServiceStatusStopped || status == ServiceStatusFailed {
			return false
		}
	}
	return true
}
