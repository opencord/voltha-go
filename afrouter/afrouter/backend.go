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

// Backend manager handles redundant connections per backend

import (
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"io"
	"net"
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
	openConns         int
}

type association struct {
	strategy associationStrategy
	location associationLocation
	field    string // Used only if location is protobuf
	key      string
}

func (be *backend) openSouthboundStreams(srv interface{}, serverStream grpc.ServerStream, f *nbFrame) (*streams, error) {

	rtrn := &streams{streams: make(map[string]*stream), activeStream: nil}

	log.Debugf("Opening southbound streams for method '%s'", f.methodInfo.method)
	// Get the metadata from the incoming message on the server
	md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
		return nil, errors.New("Could not get a server stream metadata")
	}

	// TODO: Need to check if this is an active/active backend cluster
	// with a serial number in the header.
	log.Debugf("Serial number for transaction allocated: %d", f.serialNo)
	// If even one stream can be created then proceed. If none can be
	// created then report an error becase both the primary and redundant
	// connections are non-existant.
	var atLeastOne = false
	var errStr strings.Builder
	log.Debugf("There are %d connections to open", len(be.connections))
	for _, cn := range be.connections {
		// Copy in the metadata
		if cn.getState() == connectivity.Ready && cn.getConn() != nil {
			log.Debugf("Opening southbound stream for connection '%s'", cn.name)
			// Create an outgoing context that includes the incoming metadata
			// and that will cancel if the server's context is canceled
			clientCtx, clientCancel := context.WithCancel(serverStream.Context())
			clientCtx = metadata.NewOutgoingContext(clientCtx, md.Copy())
			//TODO: Same check here, only add the serial number if necessary
			clientCtx = metadata.AppendToOutgoingContext(clientCtx, "voltha_serial_number",
				strconv.FormatUint(f.serialNo, 10))
			// Create the client stream
			if clientStream, err := grpc.NewClientStream(clientCtx, clientStreamDescForProxying,
				cn.getConn(), f.methodInfo.all); err != nil {
				log.Debugf("Failed to create a client stream '%s', %v", cn.name, err)
				fmt.Fprintf(&errStr, "{{Failed to create a client stream '%s', %v}} ", cn.name, err)
				rtrn.streams[cn.name] = nil
			} else {
				rtrn.streams[cn.name] = &stream{stream: clientStream, ctxt: clientCtx,
					cancel: clientCancel, s2cReturn: nil,
					ok2Close:  make(chan struct{}),
					c2sReturn: make(chan error, 1)}
				atLeastOne = true
			}
		} else if cn.getConn() == nil {
			err := errors.New(fmt.Sprintf("Connection '%s' is closed", cn.name))
			fmt.Fprint(&errStr, err.Error())
			log.Debug(err)
		} else {
			err := errors.New(fmt.Sprintf("Connection '%s' isn't ready", cn.name))
			fmt.Fprint(&errStr, err.Error())
			log.Debug(err)
		}
	}
	if atLeastOne {
		rtrn.sortStreams()
		return rtrn, nil
	}
	fmt.Fprintf(&errStr, "{{No streams available for backend '%s' unable to send}} ", be.name)
	log.Error(errStr.String())
	return nil, errors.New(errStr.String())
}

func (be *backend) handler(srv interface{}, serverStream grpc.ServerStream, nf *nbFrame, sf *sbFrame) error {

	// Set up and launch each individual southbound stream
	var beStrms *streams
	var rtrn error = nil
	var s2cOk = false
	var c2sOk = false

	beStrms, err := be.openSouthboundStreams(srv, serverStream, nf)
	if err != nil {
		log.Errorf("openStreams failed: %v", err)
		return err
	}
	// If we get here, there has to be AT LEAST ONE open stream

	// *Do not explicitly close* s2cErrChan and c2sErrChan, otherwise the select below will not terminate.
	// Channels do not have to be closed, it is just a control flow mechanism, see
	// https://groups.google.com/forum/#!msg/golang-nuts/pZwdYRGxCIk/qpbHxRRPJdUJ

	log.Debug("Starting server to client forwarding")
	s2cErrChan := beStrms.forwardServerToClient(serverStream, nf)

	log.Debug("Starting client to server forwarding")
	c2sErrChan := beStrms.forwardClientToServer(serverStream, sf)

	// We don't know which side is going to stop sending first, so we need a select between the two.
	for i := 0; i < 2; i++ {
		select {
		case s2cErr := <-s2cErrChan:
			s2cOk = true
			log.Debug("Processing s2cErr")
			if s2cErr == io.EOF {
				log.Debug("s2cErr reporting EOF")
				// this is the successful case where the sender has encountered io.EOF, and won't be sending anymore./
				// the clientStream>serverStream may continue sending though.
				defer beStrms.closeSend()
				if c2sOk {
					return rtrn
				}
			} else {
				log.Debugf("s2cErr reporting %v", s2cErr)
				// however, we may have gotten a receive error (stream disconnected, a read error etc) in which case we need
				// to cancel the clientStream to the backend, let all of its goroutines be freed up by the CancelFunc and
				// exit with an error to the stack
				beStrms.clientCancel()
				return grpc.Errorf(codes.Internal, "failed proxying s2c: %v", s2cErr)
			}
		case c2sErr := <-c2sErrChan:
			c2sOk = true
			log.Debug("Processing c2sErr")
			// This happens when the clientStream has nothing else to offer (io.EOF), returned a gRPC error. In those two
			// cases we may have received Trailers as part of the call. In case of other errors (stream closed) the trailers
			// will be nil.
			serverStream.SetTrailer(beStrms.trailer())
			// c2sErr will contain RPC error from client code. If not io.EOF return the RPC error as server stream error.
			// NOTE!!! with redundant backends, it's likely that one of the backends
			// returns a response before all the data has been sent southbound and
			// the southbound streams are closed. Should this happen one of the
			// backends may not get the request.
			if c2sErr != io.EOF {
				rtrn = c2sErr
			}
			log.Debug("c2sErr reporting EOF")
			if s2cOk {
				return rtrn
			}
		}
	}
	return grpc.Errorf(codes.Internal, "gRPC proxying should never reach this stage.")
}

func newBackend(conf *BackendConfig, clusterName string) (*backend, error) {
	var rtrn_err bool = false

	log.Debugf("Configuring the backend with %v", *conf)
	// Validate the conifg and configure the backend
	be := &backend{name: conf.Name, connections: make(map[string]*connection), openConns: 0}
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
			gc := &gConnection{conn: nil, cancel: nil, state: connectivity.Idle}
			be.connections[cnConf.Name] = &connection{name: cnConf.Name, addr: cnConf.Addr, port: cnConf.Port, backend: be, gConn: gc}
			if cnConf.Addr != "" { // This connection will be specified later.
				if ip := net.ParseIP(cnConf.Addr); ip == nil {
					log.Errorf("The IP address for connection %s in backend %s in cluster %s is invalid",
						cnConf.Name, conf.Name, clusterName)
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
	}

	if rtrn_err {
		return nil, errors.New("Connection configuration failed")
	}
	// All is well start the backend cluster connections
	be.connectAll()

	return be, nil
}

func (be *backend) incConn() {
	be.mutex.Lock()
	defer be.mutex.Unlock()
	be.openConns++
}

func (be *backend) decConn() {
	be.mutex.Lock()
	defer be.mutex.Unlock()
	be.openConns--
	if be.openConns < 0 {
		log.Error("Internal error, number of open connections less than 0")
		be.openConns = 0
	}
}

// Attempts to establish all the connections for a backend
// any failures result in an abort. This should only be called
// on a first attempt to connect. Individual connections should be
// handled after that.
func (be *backend) connectAll() {
	for _, cn := range be.connections {
		cn.connect()
	}
}

// Set a callback for connection failure notification
// This is currently not used.
func (be *backend) setConnFailCallback(cb func(string, *backend) bool) {
	be.connFailCallback = cb
}
