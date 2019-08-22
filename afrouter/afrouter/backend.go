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

package afrouter

// Backend manager handles redundant connections per backend

import (
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// backend represents a collection of backends in a HA configuration
type backend struct {
	mutex             sync.Mutex
	name              string
	beType            backendType
	activeAssociation association
	connFailCallback  func(string, *backend) bool
	connections       map[string]*connection
	openConns         map[*connection]*grpc.ClientConn
	activeRequests    map[*request]struct{}
}

type association struct {
	strategy associationStrategy
	location associationLocation
	field    string // Used only if location is protobuf
	key      string
}

// splitActiveStreamsUnsafe expects the caller to have already locked the backend mutex
func (be *backend) splitActiveStreamsUnsafe(cn *connection, conn *grpc.ClientConn) {
	if len(be.activeRequests) != 0 {
		log.Debugf("Creating new streams for %d existing requests", len(be.activeRequests))
	}
	for r := range be.activeRequests {
		r.mutex.Lock()
		if _, have := r.streams[cn.name]; !have {
			log.Debugf("Opening southbound stream for existing request '%s'", r.methodInfo.method)
			if stream, err := grpc.NewClientStream(r.ctx, clientStreamDescForProxying, conn, r.methodInfo.all); err != nil {
				log.Debugf("Failed to create a client stream '%s', %v", cn.name, err)
			} else {
				go r.catchupRequestStreamThenForwardResponseStream(cn.name, stream)
				// new thread will unlock the request mutex
				continue
			}
		}
		r.mutex.Unlock()
	}
}

// openSouthboundStreams sets up a connection to each southbound frame
func (be *backend) openSouthboundStreams(srv interface{}, serverStream grpc.ServerStream, nf *requestFrame, sf *responseFrame) (*request, error) {
	be.mutex.Lock()
	defer be.mutex.Unlock()

	isStreamingRequest, isStreamingResponse := nf.router.IsStreaming(nf.methodInfo.method)

	// Get the metadata from the incoming message on the server
	md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
		return nil, errors.New("could not get a server stream metadata")
	}

	r := &request{
		// Create an outgoing context that includes the incoming metadata and that will cancel if the server's context is canceled
		ctx: metadata.AppendToOutgoingContext(metadata.NewOutgoingContext(serverStream.Context(), md.Copy()), "voltha_serial_number", strconv.FormatUint(nf.serialNo, 10)),

		streams:         make(map[string]grpc.ClientStream),
		responseErrChan: make(chan error, 1),

		backend:             be,
		serverStream:        serverStream,
		methodInfo:          nf.methodInfo,
		requestFrame:        nf,
		responseFrame:       sf,
		isStreamingRequest:  isStreamingRequest,
		isStreamingResponse: isStreamingResponse,
	}

	log.Debugf("Opening southbound request for method '%s'", nf.methodInfo.method)

	// TODO: Need to check if this is an active/active backend cluster
	// with a serial number in the header.
	log.Debugf("Serial number for transaction allocated: %d", nf.serialNo)
	// If even one stream can be created then proceed. If none can be
	// created then report an error because both the primary and redundant
	// connections are non-existent.
	var atLeastOne = false
	var errStr strings.Builder

	log.Debugf("There are %d/%d streams to open", len(be.openConns), len(be.connections))
	for cn, conn := range be.openConns {
		// If source-router was used, it will indicate a specific connection to be used
		if nf.connection != nil && nf.connection != cn {
			log.Debugf("Skipping connection %s. Looking for %s", cn.name, nf.connection.name)
			continue
		}

		log.Debugf("Opening stream for connection '%s'", cn.name)
		if stream, err := grpc.NewClientStream(r.ctx, clientStreamDescForProxying, conn, r.methodInfo.all); err != nil {
			log.Debugf("Failed to create a client stream '%s', %v", cn.name, err)
		} else {
			r.streams[cn.name] = stream
			go r.forwardResponseStream(cn.name, stream)
			atLeastOne = true
		}
	}
	if atLeastOne {
		be.activeRequests[r] = struct{}{}
		return r, nil
	}
	fmt.Fprintf(&errStr, "{{No open connections for backend '%s' unable to send}} ", be.name)
	log.Error(errStr.String())
	return nil, errors.New(errStr.String())
}

func (be *backend) handler(srv interface{}, serverStream grpc.ServerStream, nf *requestFrame, sf *responseFrame) error {
	// Set up streams for each open connection
	request, err := be.openSouthboundStreams(srv, serverStream, nf, sf)
	if err != nil {
		log.Errorf("openStreams failed: %v", err)
		return err
	}

	log.Debug("Starting request stream forwarding")
	if s2cErr := request.forwardRequestStream(serverStream); s2cErr != nil {
		// exit with an error to the stack
		return grpc.Errorf(codes.Internal, "failed proxying s2c: %v", s2cErr)
	}
	// wait for response stream to complete
	return <-request.responseErrChan
}

func newBackend(conf *BackendConfig, clusterName string) (*backend, error) {
	var rtrn_err bool = false

	log.Debugf("Configuring the backend with %v", *conf)
	// Validate the conifg and configure the backend
	be := &backend{
		name:           conf.Name,
		connections:    make(map[string]*connection),
		openConns:      make(map[*connection]*grpc.ClientConn),
		activeRequests: make(map[*request]struct{}),
	}
	if conf.Type == BackendUndefined {
		log.Error("Invalid type specified for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.beType = conf.Type

	if conf.Association.Strategy == AssociationStrategyUndefined && be.beType == BackendActiveActive {
		log.Errorf("An association strategy must be provided if the backend "+
			"type is active/active for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssociation.strategy = conf.Association.Strategy

	if conf.Association.Location == AssociationLocationUndefined && be.beType == BackendActiveActive {
		log.Errorf("An association location must be provided if the backend "+
			"type is active/active for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssociation.location = conf.Association.Location

	if conf.Association.Field == "" && be.activeAssociation.location == AssociationLocationProtobuf {
		log.Errorf("An association field must be provided if the backend "+
			"type is active/active and the location is set to protobuf "+
			"for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssociation.field = conf.Association.Field

	if conf.Association.Key == "" && be.activeAssociation.location == AssociationLocationHeader {
		log.Errorf("An association key must be provided if the backend "+
			"type is active/active and the location is set to header "+
			"for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssociation.key = conf.Association.Key
	if rtrn_err {
		return nil, errors.New("Backend configuration failed")
	}
	// Configure the connections
	// Connections can consist of just a name. This allows for dynamic configuration
	// at a later time.
	// TODO: validate that there is one connection for all but active/active backends
	if len(conf.Connections) > 1 && be.beType != BackendActiveActive {
		log.Errorf("Only one connection must be specified if the association " +
			"strategy is not set to 'active_active'")
		rtrn_err = true
	}
	if len(conf.Connections) == 0 {
		log.Errorf("At least one connection must be specified")
		rtrn_err = true
	}
	for _, cnConf := range conf.Connections {
		if cnConf.Name == "" {
			log.Errorf("A connection must have a name for backend %s in cluster %s",
				conf.Name, clusterName)
		} else {
			ctx, cancelFunc := context.WithCancel(context.Background())
			be.connections[cnConf.Name] = &connection{name: cnConf.Name, addr: cnConf.Addr, port: cnConf.Port, backend: be, ctx: ctx, close: cancelFunc}
			if _, err := url.Parse(cnConf.Addr); err != nil {
				log.Errorf("The address for connection %s in backend %s in cluster %s is invalid: %s",
					cnConf.Name, conf.Name, clusterName, err)
				rtrn_err = true
			}
			// Validate the port number. This just validtes that it's a non 0 integer
			if n, err := strconv.Atoi(cnConf.Port); err != nil || n <= 0 || n > 65535 {
				log.Errorf("Port %s for connection %s in backend %s in cluster %s is invalid",
					cnConf.Port, cnConf.Name, conf.Name, clusterName)
				rtrn_err = true
			} else {
				if n <= 0 && n > 65535 {
					log.Errorf("Port %s for connection %s in backend %s in cluster %s is invalid",
						cnConf.Port, cnConf.Name, conf.Name, clusterName)
					rtrn_err = true
				}
			}
		}
	}

	if rtrn_err {
		return nil, errors.New("Connection configuration failed")
	}
	// All is well start the backend cluster connections
	be.connectAll()

	return be, nil
}

func (be *backend) incConn(cn *connection, conn *grpc.ClientConn) {
	be.mutex.Lock()
	defer be.mutex.Unlock()

	be.openConns[cn] = conn
	be.splitActiveStreamsUnsafe(cn, conn)
}

func (be *backend) decConn(cn *connection) {
	be.mutex.Lock()
	defer be.mutex.Unlock()

	delete(be.openConns, cn)
}

func (be *backend) NumOpenConnections() int {
	be.mutex.Lock()
	defer be.mutex.Unlock()

	return len(be.openConns)
}

// Attempts to establish all the connections for a backend
// any failures result in an abort. This should only be called
// on a first attempt to connect. Individual connections should be
// handled after that.
func (be *backend) connectAll() {
	for _, cn := range be.connections {
		go cn.connect()
	}
}

// Set a callback for connection failure notification
// This is currently not used.
func (be *backend) setConnFailCallback(cb func(string, *backend) bool) {
	be.connFailCallback = cb
}
