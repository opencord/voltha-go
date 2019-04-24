/*
 * Copyright 2018-present Open Networking Foundation

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
// gRPC affinity router with active/active backends

package afrouter

// This file implements the ArouterPoxy struct and its
// functions. The ArouterProxy is the top level object
// for the affinity router.

import (
	"github.com/opencord/voltha-go/common/log"
)

type nbi int

const (
	GRPC_NBI           nbi = 1
	GRPC_STREAMING_NBI nbi = 2
	GRPC_CONTROL_NBI   nbi = 3
)

// String names for display in error messages.
var arpxyNames = [...]string{"grpc_nbi", "grpc_streaming_nbi", "grpc_control_nbi"}
var arProxy *ArouterProxy = nil

type ArouterProxy struct {
	servers map[string]*server // Defined in handler.go
	api     *ArouterApi
}

// Create the routing proxy
func NewArouterProxy(conf *Configuration) (*ArouterProxy, error) {
	arProxy = &ArouterProxy{servers: make(map[string]*server)}
	// Create all the servers listed in the configuration
	for _, s := range conf.Servers {
		if ns, err := newServer(&s); err != nil {
			log.Error("Configuration failed")
			return nil, err
		} else {
			arProxy.servers[ns.Name()] = ns
		}
	}

	// TODO: The API is not mandatory, check if it's even in the config before
	// trying to create it. If it isn't then don't bother but log a warning.
	if api, err := newApi(&conf.Api, arProxy); err != nil {
		return nil, err
	} else {
		arProxy.api = api
	}

	return arProxy, nil
}

// Start serving
func (ap *ArouterProxy) ListenAndServe() error {

	for _, srvr := range ap.servers {
		ap.serve(srvr)
	}
	ap.api.serve()

	// Just wait until we're done which only happens
	// on a signal or an error.
	err := <-doneChan

	return err
}

func (ap *ArouterProxy) serve(srvr *server) {

	// Start a serving thread
	go func() {
		srvr.running = true
		if err := srvr.proxyServer.Serve(srvr.proxyListener); err != nil {
			srvr.running = false
			log.Error(err)
			errChan <- err
		}
	}()
}
