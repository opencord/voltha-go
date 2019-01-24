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
	"io"
	"fmt"
	"net"
	"sync"
	"time"
	"errors"
	"strconv"
	"strings"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/connectivity"
	"github.com/opencord/voltha-go/common/log"
)



const (
	BE_ACTIVE_ACTIVE = 1 // Backend type active/active
	BE_SERVER = 2        // Backend type single server
	BE_SEQ_RR = 0        // Backend sequence round robin
	AS_NONE = 0          // Association strategy: none
	AS_SERIAL_NO = 1     // Association strategy: serial number
	AL_NONE = 0          // Association location: none
	AL_HEADER = 1        // Association location: header
	AL_PROTOBUF = 2      // Association location: protobuf
)


var beTypeNames = []string{"","active_active","server"}
var asTypeNames = []string{"","serial_number"}
var alTypeNames = []string{"","header","protobuf"}

var bClusters map[string]*backendCluster = make(map[string]*backendCluster)

type backendCluster struct {
	name string
	//backends map[string]*backend
	backends []*backend
	beRvMap map[*backend]int
	serialNoSource chan uint64
}

type backend struct {
	lck sync.Mutex
	name string
	beType int
	activeAssoc assoc
	connFailCallback func(string, *backend)bool
	connections map[string]*beConnection
	opnConns int
}

type assoc struct {
	strategy int
	location int
	field string // Used only if location is protobuf
}

type beConnection struct {
	lck sync.Mutex
	cncl context.CancelFunc
	name string
	addr string
	port string
	gConn *gConnection
	bknd *backend
}

// This structure should never be referred to
// by any routine outside of *beConnection
// routines.
type gConnection struct {
	lck sync.Mutex
	state connectivity.State
	conn *grpc.ClientConn
	cncl context.CancelFunc
}

type beClStrm struct {
	strm grpc.ClientStream
	ctxt context.Context
	cncl context.CancelFunc
	c2sRtrn chan error
	s2cRtrn error
}

type beClStrms struct {
	lck sync.Mutex
	actvStrm *beClStrm
	strms map[string]*beClStrm
}

//***************************************************************//
//****************** BackendCluster Functions *******************//
//***************************************************************//

//TODO: Move the backend type (active/active etc) to the cluster
// level. All backends should really be of the same type.
// Create a new backend cluster
func NewBackendCluster(conf *BackendClusterConfig) (*backendCluster, error) {
	var err error = nil
	var rtrn_err bool = false
	var be *backend
	log.Debug("Creating a backend cluster with %v", conf)
	// Validate the configuration
	if conf.Name == "" {
		log.Error("A backend cluster must have a name")
		rtrn_err = true
	}
	//bc :=  &backendCluster{name:conf.Name,backends:make(map[string]*backend)}
	bc :=  &backendCluster{name:conf.Name, beRvMap:make(map[*backend]int)}
	bClusters[bc.name] = bc
	bc.startSerialNumberSource() // Serial numberere for active/active backends
	idx := 0
	for _, bec := range conf.Backends {
		if bec.Name == "" {
			log.Errorf("A backend must have a name in cluster %s\n", conf.Name)
			rtrn_err = true
		}
		if be,err = newBackend(&bec, conf.Name); err != nil {
			log.Errorf("Error creating backend %s", bec.Name)
			rtrn_err = true
		}
		bc.backends = append(bc.backends, be)
		bc.beRvMap[bc.backends[idx]] = idx
		idx++
	}
	if rtrn_err {
		return nil, errors.New("Error creating backend(s)")
	}
	return bc, nil
}

func (bc * backendCluster) getBackend(name string) *backend {
	for _,v := range bc.backends {
		if v.name == name {
			return v
		}
	}
	return nil
}

func (bc *backendCluster) startSerialNumberSource() {
	bc.serialNoSource = make(chan uint64)
	var counter uint64 = 0
	// This go routine only dies on exit, it is not a leak
	go func() {
		for {
			bc.serialNoSource <- counter
			counter++
		}
	}()
}

func (bc *backendCluster) nextBackend(be *backend, seq int) (*backend,error) {
	switch seq {
		case BE_SEQ_RR: // Round robin
			in := be
			// If no backend is found having a connection
			// then return nil.
			if be == nil {
				log.Debug("Previous backend is nil")
				 be = bc.backends[0]
				 in = be
				 if be.opnConns != 0 {
					return be,nil
				 }
			}
			for {
				log.Debugf("Requesting a new backend starting from %s", be.name)
				cur := bc.beRvMap[be]
				cur++
				if cur >= len(bc.backends) {
					cur = 0
				}
				log.Debugf("Next backend is %d:%s", cur, bc.backends[cur].name)
				if bc.backends[cur].opnConns > 0 {
					return bc.backends[cur], nil
				}
				if bc.backends[cur] == in {
					err := fmt.Errorf("No backend with open connections found")
					log.Debug(err);
					return nil,err
				}
				be = bc.backends[cur]
				log.Debugf("Backend '%s' has no open connections, trying next", bc.backends[cur].name)
			}
		default: // Invalid, defalt to routnd robin
			log.Errorf("Invalid backend sequence %d. Defaulting to round robin", seq)
			return bc.nextBackend(be, BE_SEQ_RR)
	}
}

func (bec *backendCluster) handler(srv interface{}, serverStream grpc.ServerStream, r Router, mthdSlice []string,
				mk string, mv string) error {
//func (bec *backendCluster) handler(nbR * nbRequest) error {

	// The final backend cluster needs to be determined here. With non-affinity routed backends it could
	// just be determined here and for affinity routed backends the first message must be received
	// before the backend is determined. In order to keep things simple, the same approach is taken for
	// now.

	// Get the backend to use.
	// Allocate the nbFrame here since it holds the "context" of this communication
	nf := &nbFrame{router:r, mthdSlice:mthdSlice, serNo:bec.serialNoSource, metaKey:mk, metaVal:mv}
	log.Debugf("Nb frame allocate with method %s", nf.mthdSlice[REQ_METHOD])

	if be,err := bec.assignBackend(serverStream, nf); err != nil {
		// At this point, no backend streams have been initiated
		// so just return the error.
		return err
	} else {
		log.Debugf("Backend '%s' selected", be.name)
		// Allocate a sbFrame here because it might be needed for return value intercept
		sf := &sbFrame{router:r, be:be, method:nf.mthdSlice[REQ_METHOD], metaKey:mk, metaVal:mv}
		log.Debugf("Sb frame allocated with router %s",r.Name())
		return be.handler(srv, serverStream, nf, sf)
	}
}

func (be *backend) openSouthboundStreams(srv interface{}, serverStream grpc.ServerStream, f * nbFrame) (*beClStrms, error) {

	rtrn := &beClStrms{strms:make(map[string]*beClStrm),actvStrm:nil}

	log.Debugf("Opening southbound streams for method '%s'", f.mthdSlice[REQ_METHOD])
	// Get the metadata from the incoming message on the server
	md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
		return nil, errors.New("Could not get a server stream metadata")
	}

	// TODO: Need to check if this is an active/active backend cluster
	// with a serial number in the header.
	serialNo := <-f.serNo
	log.Debugf("Serial number for transaction allocated: %d", serialNo)
	// If even one stream can be created then proceed. If none can be
	// created then report an error becase both the primary and redundant
	// connections are non-existant.
	var atLeastOne bool = false
	var errStr strings.Builder
	log.Debugf("There are %d connections to open", len(be.connections))
	for cnk,cn := range be.connections {
		// TODO: THIS IS A HACK to suspend redundancy for binding routers for all calls
		// and its very specific to a use case. There should really be a per method
		// mechanism to select non-redundant calls for all router types. This needs
		// to be fixed ASAP. The overrides should be used for this, the implementation
		// is simple, and it can be done here.
		if atLeastOne == true && f.metaKey != NoMeta {
			// Don't open any more southbound streams
			log.Debugf("Not opening any more SB streams, metaKey = %s", f.metaKey)
			rtrn.strms[cnk] = nil
			continue
		}
		// Copy in the metadata
		if cn.getState() == connectivity.Ready  && cn.getConn() != nil {
			log.Debugf("Opening southbound stream for connection '%s'", cnk)
			// Create an outgoing context that includes the incoming metadata
			// and that will cancel if the server's context is canceled
			clientCtx, clientCancel := context.WithCancel(serverStream.Context())
			clientCtx = metadata.NewOutgoingContext(clientCtx, md.Copy())
			//TODO: Same check here, only add the serial number if necessary
			clientCtx = metadata.AppendToOutgoingContext(clientCtx, "voltha_serial_number",
															strconv.FormatUint(serialNo,10))
			// Create the client stream
			if clientStream, err := grpc.NewClientStream(clientCtx, clientStreamDescForProxying,
														cn.getConn(), f.mthdSlice[REQ_ALL]); err !=nil {
				log.Debug("Failed to create a client stream '%s', %v",cn.name,err)
				fmt.Fprintf(&errStr, "{{Failed to create a client stream '%s', %v}} ", cn.name, err)
				rtrn.strms[cnk] = nil
			} else {
				rtrn.strms[cnk] = &beClStrm{strm:clientStream, ctxt:clientCtx, cncl:clientCancel, s2cRtrn:nil,
											c2sRtrn:make(chan error, 1)}
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
	if atLeastOne == true {
		return rtrn,nil
	}
	fmt.Fprintf(&errStr, "{{No streams available for backend '%s' unable to send}} ",be.name)
	log.Error(errStr.String())
	return nil, errors.New(errStr.String())
}

func (be *backend) handler(srv interface{}, serverStream grpc.ServerStream, nf * nbFrame, sf * sbFrame) error {

	// Set up and launch each individual southbound stream 
	var beStrms *beClStrms

	beStrms, err := be.openSouthboundStreams(srv,serverStream,nf)
	if err != nil {
		log.Errorf("openStreams failed: %v",err)
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
			log.Debug("Processing s2cErr")
			if s2cErr == io.EOF {
				log.Debug("s2cErr reporting EOF")
				// this is the successful case where the sender has encountered io.EOF, and won't be sending anymore./
				// the clientStream>serverStream may continue sending though.
				beStrms.closeSend()
				break
			} else {
				log.Debugf("s2cErr reporting %v",s2cErr)
				// however, we may have gotten a receive error (stream disconnected, a read error etc) in which case we need
				// to cancel the clientStream to the backend, let all of its goroutines be freed up by the CancelFunc and
				// exit with an error to the stack
				beStrms.clientCancel()
				return grpc.Errorf(codes.Internal, "failed proxying s2c: %v", s2cErr)
			}
		case c2sErr := <-c2sErrChan:
			log.Debug("Processing c2sErr")
			// This happens when the clientStream has nothing else to offer (io.EOF), returned a gRPC error. In those two
			// cases we may have received Trailers as part of the call. In case of other errors (stream closed) the trailers
			// will be nil.
			serverStream.SetTrailer(beStrms.trailer())
			// c2sErr will contain RPC error from client code. If not io.EOF return the RPC error as server stream error.
			if c2sErr != io.EOF {
				return c2sErr
			}
			return nil
		}
	}
	return grpc.Errorf(codes.Internal, "gRPC proxying should never reach this stage.")
}

func (strms *beClStrms) clientCancel() {
	for _,strm := range strms.strms {
		if strm != nil {
			strm.cncl()
		}
	}
}

func (strms *beClStrms) closeSend() {
	for _,strm := range strms.strms {
		if strm != nil {
			log.Debug("Closing southbound stream")
			strm.strm.CloseSend()
		}
	}
}

func (strms *beClStrms) trailer() metadata.MD {
	return strms.actvStrm.strm.Trailer()
}

func (bec *backendCluster) assignBackend(src grpc.ServerStream, f *nbFrame) (*backend, error) {
	// Receive the first message from the server. This calls the assigned codec in which
	// Unmarshal gets executed. That will use the assigned router to select a backend
	// and add it to the frame
	if err := src.RecvMsg(f); err != nil {
		return nil, err
	}
	// Check that the backend was routable and actually has connections open.
	// If it doesn't then return a nil backend to indicate this
	if f.be == nil {
		err := fmt.Errorf("Unable to route method '%s'", f.mthdSlice[REQ_METHOD])
		log.Error(err)
		return nil, err
	} else if f.be.opnConns == 0 {
		err := fmt.Errorf("No open connections on backend '%s'", f.be.name)
		log.Error(err)
		return f.be, err
	}
	return f.be, nil
}

func (strms * beClStrms) getActive() *beClStrm {
	strms.lck.Lock()
	defer strms.lck.Unlock()
	return strms.actvStrm
}

func (strms *beClStrms) setThenGetActive(strm *beClStrm) (*beClStrm) {
	strms.lck.Lock()
	defer strms.lck.Unlock()
	if strms.actvStrm == nil {
		strms.actvStrm = strm
	}
	return strms.actvStrm
}

func (src *beClStrms) forwardClientToServer(dst grpc.ServerStream, f *sbFrame) chan error {
	fc2s := func(srcS *beClStrm) {
		for i := 0; ; i++ {
			if err := srcS.strm.RecvMsg(f); err != nil {
				if src.setThenGetActive(srcS) == srcS {
					srcS.c2sRtrn <- err // this can be io.EOF which is the success case
				} else {
					srcS.c2sRtrn <- nil // Inactive responder
				}
				break
			}
			if src.setThenGetActive(srcS) != srcS {
				srcS.c2sRtrn <- nil
				break
			}
			if i == 0 {
				// This is a bit of a hack, but client to server headers are only readable after first client msg is
				// received but must be written to server stream before the first msg is flushed.
				// This is the only place to do it nicely.
				md, err := srcS.strm.Header()
				if err != nil {
					srcS.c2sRtrn <- err
					break
				}
				// Update the metadata for the response.
				if f.metaKey != NoMeta {
					if f.metaVal == "" {
						// We could also alsways just do this
						md.Set(f.metaKey, f.be.name)
					} else {
						md.Set(f.metaKey, f.metaVal)
					}
				}
				if err := dst.SendHeader(md); err != nil {
					srcS.c2sRtrn <- err
					break
				}
			}
			log.Debugf("Northbound frame %v", f.payload)
			if err := dst.SendMsg(f); err != nil {
				srcS.c2sRtrn <- err
				break
			}
		}
	}

	// There should be AT LEAST one open stream at this point
	// if there isn't its a grave error in the code and it will
	// cause this thread to block here so check for it and
	// don't let the lock up happen but report the error
	ret := make(chan error, 1)
	agg := make(chan *beClStrm)
	atLeastOne := false
	for _,strm := range src.strms {
		if strm != nil {
			go fc2s(strm)
			go func(s *beClStrm) { // Wait on result and aggregate
				r := <-s.c2sRtrn // got the return code
				if r == nil {
					return  // We're the redundat stream, just die
				}
				s.c2sRtrn <- r // put it back to pass it along
				agg <- s // send the stream to the aggregator
			} (strm)
			atLeastOne = true
		}
	}
	if atLeastOne == true {
		go func() { // Wait on aggregated result
			s := <-agg
			ret <- <-s.c2sRtrn
		}()
	} else {
		err := errors.New("There are no open streams. Unable to forward message.")
		log.Error(err)
		ret <- err
	}
	return ret
}

func (strms *beClStrms) sendAll(f *nbFrame) error {
	var rtrn error

	atLeastOne := false
	for _,strm := range strms.strms {
		if strm != nil {
			if err := strm.strm.SendMsg(f); err != nil {
				strm.s2cRtrn = err
			}
			atLeastOne = true
		}
	}
	// If one of the streams succeeded, declare success
	// if none did pick an error and return it.
	if atLeastOne == true {
		for _,strm := range strms.strms {
			if strm != nil {
				rtrn = strm.s2cRtrn
				if rtrn == nil {
					return rtrn
				}
			}
		}
		return rtrn
	} else {
		rtrn = errors.New("There are no open streams, this should never happen")
		log.Error(rtrn)
	}
	return rtrn;
}

func (dst *beClStrms) forwardServerToClient(src grpc.ServerStream, f *nbFrame) chan error {
	ret := make(chan error, 1)
	go func() {
		// The frame buffer already has the results of a first
		// RecvMsg in it so the first thing to do is to 
		// send it to the list of client streams and only
		// then read some more.
		for i := 0; ; i++ {
			// Send the message to each of the backend streams
			if err := dst.sendAll(f); err != nil {
				ret <- err
				break
			}
			log.Debugf("Southbound frame %v", f.payload)
			if err := src.RecvMsg(f); err != nil {
				ret <- err // this can be io.EOF which is happy case
				break
			}
		}
	}()
	return ret
}

func newBackend(conf *BackendConfig, clusterName string) (*backend, error) {
	var rtrn_err bool = false

	log.Debugf("Configuring the backend with %v", *conf)
	// Validate the conifg and configure the backend
	be:=&backend{name:conf.Name,connections:make(map[string]*beConnection),opnConns:0}
	idx := strIndex([]string(beTypeNames),conf.Type)
	if idx == 0 {
		log.Error("Invalid type specified for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.beType = idx

	idx = strIndex(asTypeNames, conf.Association.Strategy)
	if idx == 0 && be.beType == BE_ACTIVE_ACTIVE {
		log.Errorf("An association strategy must be provided if the backend "+
					"type is active/active for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssoc.strategy = idx

	idx = strIndex(alTypeNames, conf.Association.Location)
	if idx == 0 && be.beType == BE_ACTIVE_ACTIVE {
		log.Errorf("An association location must be provided if the backend "+
					"type is active/active for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssoc.location = idx

	if conf.Association.Field == "" && be.activeAssoc.location == AL_PROTOBUF {
		log.Errorf("An association field must be provided if the backend "+
					"type is active/active and the location is set to protobuf "+
					"for backend %s in cluster %s", conf.Name, clusterName)
		rtrn_err = true
	}
	be.activeAssoc.field = conf.Association.Field
	if rtrn_err {
		return nil, errors.New("Backend configuration failed")
	}
	// Configure the connections
	// Connections can consist of just a name. This allows for dynamic configuration
	// at a later time.
	// TODO: validate that there is one connection for all but active/active backends
	for _,cnConf := range conf.Connections {
		if cnConf.Name == "" {
			log.Errorf("A connection must have a name for backend %s in cluster %s",
						conf.Name, clusterName)
		} else {
			gc:=&gConnection{conn:nil,cncl:nil,state:connectivity.Idle}
			be.connections[cnConf.Name] = &beConnection{name:cnConf.Name,addr:cnConf.Addr,port:cnConf.Port,bknd:be,gConn:gc}
			if cnConf.Addr != "" { // This connection will be specified later.
				if ip := net.ParseIP(cnConf.Addr); ip == nil {
					log.Errorf("The IP address for connection %s in backend %s in cluster %s is invalid",
								cnConf.Name, conf.Name, clusterName)
					rtrn_err = true
				}
				// Validate the port number. This just validtes that it's a non 0 integer
				if n,err := strconv.Atoi(cnConf.Port); err != nil || n <= 0 || n > 65535 {
					log.Errorf("Port %s for connection %s in backend %s in cluster %s is invalid",
						cnConf.Port, cnConf.Name, conf.Name, clusterName)
					rtrn_err = true
				} else {
					if n <=0 && n > 65535 {
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

//***************************************************************//
//********************* Backend Functions ***********************//
//***************************************************************//

func (be *backend) incConn() {
	be.lck.Lock()
	defer be.lck.Unlock()
	be.opnConns++
}

func (be *backend) decConn() {
	be.lck.Lock()
	defer be.lck.Unlock()
	be.opnConns--
	if be.opnConns < 0 {
		log.Error("Internal error, number of open connections less than 0")
		be.opnConns = 0
	}
}

// Attempts to establish all the connections for a backend
// any failures result in an abort. This should only be called
// on a first attempt to connect. Individual connections should be
// handled after that.
func (be *backend) connectAll() {
	for _,cn := range be.connections {
		cn.connect()
	}
}

func (cn *beConnection) connect() {
	if cn.addr != "" && cn.getConn() == nil {
		log.Infof("Connecting to connection %s with addr: %s and port %s", cn.name,cn.addr,cn.port)
		// Dial doesn't block, it just returns and continues connecting in the background.
		// Check back later to confirm and increase the connection count.
		ctx, cnclFnc := context.WithCancel(context.Background()) // Context for canceling the connection
		cn.setCncl(cnclFnc)
		if conn, err := grpc.Dial(cn.addr+":"+cn.port, grpc.WithCodec(Codec()), grpc.WithInsecure()); err != nil {
			log.Errorf("Dialng connection %v:%v",cn,err)
			cn.waitAndTryAgain(ctx)
		} else {
			cn.setConn(conn)
			log.Debugf("Starting the connection monitor for '%s'", cn.name)
			cn.monitor(ctx)
		}
	} else if cn.addr == "" {
		log.Infof("No address supplied for connection '%s', not connecting for now", cn.name)
	} else {
		log.Debugf("Connection '%s' is already connected, ignoring", cn.name)
	}
}

func (cn *beConnection) waitAndTryAgain(ctx context.Context) {
	go func(ctx context.Context) {
			ctxTm,cnclTm := context.WithTimeout(context.Background(), 10 * time.Second)
			select {
				case  <-ctxTm.Done():
					cnclTm()
					log.Debugf("Trying to connect '%s'",cn.name)
					// Connect creates a new context so cancel this one.
					cn.cancel()
					cn.connect()
					return
				case  <-ctx.Done():
					cnclTm()
					return
			}
	}(ctx)
}

func (cn *beConnection) cancel() {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	log.Debugf("Canceling connection %s", cn.name)
	if cn.gConn != nil{
		if cn.gConn.cncl != nil {
			cn.cncl()
		} else {
			log.Errorf("Internal error, attempt to cancel a nil context for connection '%s'", cn.name)
		}
	} else {
		log.Errorf("Internal error, attempting to cancel on a nil connection object: '%s'", cn.name)
	}
}

func (cn *beConnection) setCncl(cncl context.CancelFunc) {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	if cn.gConn != nil {
		cn.gConn.cncl = cncl
	} else {
		log.Errorf("Internal error, attempting to set a cancel function on a nil connection object: '%s'", cn.name)
	}
}

func (cn *beConnection) setConn(conn *grpc.ClientConn) {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	if cn.gConn != nil {
		cn.gConn.conn = conn
	} else {
		log.Errorf("Internal error, attempting to set a connection on a nil connection object: '%s'", cn.name)
	}
}

func (cn *beConnection) getConn() *grpc.ClientConn {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	if cn.gConn != nil {
		return cn.gConn.conn
	}
	return nil
}

func (cn *beConnection) close() {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	log.Debugf("Closing connection %s", cn.name)
	if cn.gConn != nil && cn.gConn.conn != nil {
		if cn.gConn.conn.GetState() == connectivity.Ready {
			cn.bknd.decConn() // Decrease the connection reference
		}
		if cn.gConn.cncl != nil {
			cn.gConn.cncl() // Cancel the context first to force monitor functions to exit
		} else {
			log.Errorf("Internal error, attempt to cancel a nil context for connection '%s'", cn.name)
		}
		cn.gConn.conn.Close() // Close the connection
		// Now replace the gConn object with a new one as this one just
		// fades away as references to it are released after the close
		// finishes in the background.
		cn.gConn = &gConnection{conn:nil,cncl:nil,state:connectivity.TransientFailure}
	} else {
		log.Errorf("Internal error, attempt to close a nil connection object for '%s'", cn.name)
	}

}

func (cn *beConnection) setState(st connectivity.State) {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	if cn.gConn != nil {
		cn.gConn.state = st
	} else {
		log.Errorf("Internal error, attempting to set connection state on a nil connection object: '%s'", cn.name)
	}
}

func (cn *beConnection) getState() (connectivity.State) {
	cn.lck.Lock()
	defer cn.lck.Unlock()
	if cn.gConn != nil {
		if cn.gConn.conn != nil {
			return cn.gConn.conn.GetState()
		} else {
			log.Errorf("Internal error, attempting to get connection state on a nil connection: '%s'", cn.name)
		}
	} else {
		log.Errorf("Internal error, attempting to get connection state on a nil connection object: '%s'", cn.name)
	}
	// For lack of a better state to use. The logs will help determine what happened here.
	return connectivity.TransientFailure
}


func (cn *beConnection) monitor(ctx context.Context) {
	bp := cn.bknd
	log.Debugf("Setting up monitoring for backend %s", bp.name)
	go func(ctx context.Context) {
		var delay time.Duration = 100 //ms
		for {
			//log.Debugf("****** Monitoring connection '%s' on backend '%s', %v", cn.name, bp.name, cn.conn)
			if  cn.getState() == connectivity.Ready	{
				log.Debugf("connection '%s' on backend '%s' becomes ready", cn.name, bp.name)
				cn.setState(connectivity.Ready)
				bp.incConn()
				if cn.getConn() != nil && cn.getConn().WaitForStateChange(ctx, connectivity.Ready) == false {
					// The context was canceled. This is done by the close function
					// so just exit the routine
					log.Debugf("Contxt canceled for connection '%s' on backend '%s'",cn.name, bp.name)
					return
				}
				if cs := cn.getConn(); cs != nil {
					switch cs := cn.getState(); cs {
						case connectivity.TransientFailure:
							cn.setState(cs)
							bp.decConn()
							log.Infof("Transient failure for  connection '%s' on backend '%s'",cn.name, bp.name)
							delay = 100
						case connectivity.Shutdown:
							//The connection was closed. The assumption here is that the closer
							// will manage the connection count and setting the conn to nil.
							// Exit the routine
							log.Infof("Shutdown for connection '%s' on backend '%s'",cn.name, bp.name)
							return
						case connectivity.Idle:
							// This can only happen if the server sends a GoAway. This can
							// only happen if the server has modified MaxConnectionIdle from
							// its default of infinity. The only solution here is to close the
							// connection and keepTrying()?
							//TODO: Read the grpc source code to see if there's a different approach
							log.Errorf("Server sent 'GoAway' on connection '%s' on backend '%s'",cn.name, bp.name)
							cn.close()
							cn.connect()
							return
					}
				} else { // A nil means something went horribly wrong, error and exit.
					log.Errorf("Somthing horrible happned, the connection is nil and shouldn't be for connection %s",cn.name)
					return
				}
			} else {
				log.Debugf("Waiting for connection '%s' on backend '%s' to become ready", cn.name, bp.name)
				ctxTm, cnclTm := context.WithTimeout(context.Background(), delay * time.Millisecond)
				if delay < 30000 {
					delay += delay 
				}
				select {
					case  <-ctxTm.Done():
						cnclTm() // Doubt this is required but it's harmless.
						// Do nothing but let the loop continue
					case  <-ctx.Done():
						// Context was closed, close and exit routine
						//cn.close() NO! let the close be managed externally!
						return
				}
			}
		}
	}(ctx)
}

// Set a callback for connection failure notification
// This is currently not used.
func (bp * backend) setConnFailCallback(cb func(string, *backend)bool) {
	bp.connFailCallback = cb
}

