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
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"net/http"
	"sync"
)

// ProbeContextKey used to fetch the Probe instance from a context
type ProbeContextKeyType string

// ServiceStatus typed values for service status
type ServiceStatus int

const (
	// ServiceStatusUnknow initial state of services
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
)

const (
	ProbeQueryReady ProbeQueryType = iota
	ProbeQueryHealth
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
	updates    chan ServiceStatusUpdate
	register   chan string
	query      chan string
	status     map[string]ServiceStatus
	isReady    bool
	isHealthy  bool
}

// NewProbe create a probe and initialize implementation structure
func NewProbe() *Probe {
	return &Probe{
		status:     make(map[string]ServiceStatus),
		updates:    make(chan ServiceStatusUpdate, 10),
		register:   make(chan string, 10),
		query:      make(chan ProbeQuery, 10),
		readyFunc:  defaultReadyFunc,
		healthFunc: defaultHealthFunc,
	}
}

// WithFuncs override the default ready and health calculation functions
func (p *Probe) WithFuncs(readyFunc, healthFunc func(map[string]ServiceStatus) bool) *Probe {
	if readyFunc != nil {
		p.readyFunc = readyFunc
	}
	if healthFunc != nil {
		p.healthFunc = healthFunc
	}
	return p
}

// WithUpdateChannel override the default channel used for updates
func (p *Probe) WithUpdateChannel(updates chan ServiceStatusUpdate) *Probe {
	if updates != nil {
		p.updates = updates
	}
	return p
}

// RegisterService register one or more service names with the probe, status will be track against service name
func (p *Probe) RegisterService(names ...string) {
	for _, name := range names {
		if _, ok := p.status[name]; !ok {
			p.status[name] = ServiceStatusUnknown
		}
	}
}

// UpdateStatus utility function to send a service update to the probe
func (p *Probe) UpdateStatus(name string, status ServiceStatus) {
	p.updates <- ServiceStatusUpdate{name, status}
}

// ListenAndServe implements 3 HTTP endpoints on the given port for healthz, readz, and detailz. Returns only on error
func (p *Probe) ListenAndServe(port int) {
	mux := http.NewServeMux()

	// Returns the result of the readyFunc calculation
	mux.HandleFunc("/readz", func(w http.ResponseWriter, req *http.Request) {
		value := p.isReady()
		if value {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	// Returns the result of the healthFunc calculation
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		value := p.isHealthy()
		if value {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	// Returns the details of the services and their status as JSON
	mux.HandleFunc("/detailz", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("{"))
		comma := ""
		for c, s := range p.status {
			w.Write([]byte(fmt.Sprintf("%s\"%s\": \"%s\"", comma, c, s.String())))
			comma = ", "
		}
		w.Write([]byte("}"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})
	s := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	log.Fatal(s.ListenAndServe())
}

// UpdateListener listens for status updates from the services and recalculates the health and ready values. Does not return.
func (p *Probe) UpdateListener() {
	for {
		select {
		case update := <-p.updates:

			p.status[update.Name] = update.Status

			if p.readyFunc != nil {
				p.isReady = p.readyFunc(p.status)
			} else {
				p.isReady = true
			}

			if p.healthFunc != nil {
				p.isHealthy = p.healthFunc(p.status)
			} else {
				p.isHealthy = true
			}
		}
	}
}

// defaultReadyFunc if all services are running then ready, else not
func defaultReadyFunc(services map[string]ServiceStatus) bool {
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
	for _, status := range services {
		if status == ServiceStatusStopped || status == ServiceStatusFailed {
			return false
		}
	}
	return true
}
