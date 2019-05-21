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

import (
	"context"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"sync"
	"time"
)

// connection represents a connection to a single backend
type connection struct {
	mutex   sync.Mutex
	name    string
	addr    string
	port    string
	gConn   *gConnection
	backend *backend
}

// This structure should never be referred to
// by any routine outside of *connection
// routines.
type gConnection struct {
	mutex  sync.Mutex
	state  connectivity.State
	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

func (cn *connection) connect() {
	if cn.addr != "" && cn.getConn() == nil {
		log.Infof("Connecting to connection %s with addr: %s and port %s", cn.name, cn.addr, cn.port)
		// Dial doesn't block, it just returns and continues connecting in the background.
		// Check back later to confirm and increase the connection count.
		ctx, cnclFnc := context.WithCancel(context.Background()) // Context for canceling the connection
		cn.setCancel(cnclFnc)
		if conn, err := grpc.Dial(cn.addr+":"+cn.port, grpc.WithCodec(Codec()), grpc.WithInsecure()); err != nil {
			log.Errorf("Dialng connection %v:%v", cn, err)
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

func (cn *connection) waitAndTryAgain(ctx context.Context) {
	go func(ctx context.Context) {
		ctxTm, cnclTm := context.WithTimeout(context.Background(), 10*time.Second)
		select {
		case <-ctxTm.Done():
			cnclTm()
			log.Debugf("Trying to connect '%s'", cn.name)
			// Connect creates a new context so cancel this one.
			cn.cancel()
			cn.connect()
			return
		case <-ctx.Done():
			cnclTm()
			return
		}
	}(ctx)
}

func (cn *connection) cancel() {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
	log.Debugf("Canceling connection %s", cn.name)
	if cn.gConn != nil {
		if cn.gConn.cancel != nil {
			cn.gConn.cancel()
		} else {
			log.Errorf("Internal error, attempt to cancel a nil context for connection '%s'", cn.name)
		}
	} else {
		log.Errorf("Internal error, attempting to cancel on a nil connection object: '%s'", cn.name)
	}
}

func (cn *connection) setCancel(cancel context.CancelFunc) {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
	if cn.gConn != nil {
		cn.gConn.cancel = cancel
	} else {
		log.Errorf("Internal error, attempting to set a cancel function on a nil connection object: '%s'", cn.name)
	}
}

func (cn *connection) setConn(conn *grpc.ClientConn) {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
	if cn.gConn != nil {
		cn.gConn.conn = conn
	} else {
		log.Errorf("Internal error, attempting to set a connection on a nil connection object: '%s'", cn.name)
	}
}

func (cn *connection) getConn() *grpc.ClientConn {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
	if cn.gConn != nil {
		return cn.gConn.conn
	}
	return nil
}

func (cn *connection) close() {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
	log.Debugf("Closing connection %s", cn.name)
	if cn.gConn != nil && cn.gConn.conn != nil {
		if cn.gConn.conn.GetState() == connectivity.Ready {
			cn.backend.decConn() // Decrease the connection reference
		}
		if cn.gConn.cancel != nil {
			cn.gConn.cancel() // Cancel the context first to force monitor functions to exit
		} else {
			log.Errorf("Internal error, attempt to cancel a nil context for connection '%s'", cn.name)
		}
		cn.gConn.conn.Close() // Close the connection
		// Now replace the gConn object with a new one as this one just
		// fades away as references to it are released after the close
		// finishes in the background.
		cn.gConn = &gConnection{conn: nil, cancel: nil, state: connectivity.TransientFailure}
	} else {
		log.Errorf("Internal error, attempt to close a nil connection object for '%s'", cn.name)
	}

}

func (cn *connection) setState(st connectivity.State) {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
	if cn.gConn != nil {
		cn.gConn.state = st
	} else {
		log.Errorf("Internal error, attempting to set connection state on a nil connection object: '%s'", cn.name)
	}
}

func (cn *connection) getState() connectivity.State {
	cn.mutex.Lock()
	defer cn.mutex.Unlock()
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

func (cn *connection) monitor(ctx context.Context) {
	be := cn.backend
	log.Debugf("Setting up monitoring for backend %s", be.name)
	go func(ctx context.Context) {
		var delay time.Duration = 100 //ms
		for {
			//log.Debugf("****** Monitoring connection '%s' on backend '%s', %v", cn.name, be.name, cn.conn)
			if cn.getState() == connectivity.Ready {
				log.Debugf("connection '%s' on backend '%s' becomes ready", cn.name, be.name)
				cn.setState(connectivity.Ready)
				be.incConn()
				if cn.getConn() != nil && !cn.getConn().WaitForStateChange(ctx, connectivity.Ready) {
					// The context was canceled. This is done by the close function
					// so just exit the routine
					log.Debugf("Contxt canceled for connection '%s' on backend '%s'", cn.name, be.name)
					return
				}
				if cs := cn.getConn(); cs != nil {
					switch cs := cn.getState(); cs {
					case connectivity.TransientFailure:
						cn.setState(cs)
						be.decConn()
						log.Infof("Transient failure for  connection '%s' on backend '%s'", cn.name, be.name)
						delay = 100
					case connectivity.Shutdown:
						//The connection was closed. The assumption here is that the closer
						// will manage the connection count and setting the conn to nil.
						// Exit the routine
						log.Infof("Shutdown for connection '%s' on backend '%s'", cn.name, be.name)
						return
					case connectivity.Idle:
						// This can only happen if the server sends a GoAway. This can
						// only happen if the server has modified MaxConnectionIdle from
						// its default of infinity. The only solution here is to close the
						// connection and keepTrying()?
						//TODO: Read the grpc source code to see if there's a different approach
						log.Errorf("Server sent 'GoAway' on connection '%s' on backend '%s'", cn.name, be.name)
						cn.close()
						cn.connect()
						return
					}
				} else { // A nil means something went horribly wrong, error and exit.
					log.Errorf("Somthing horrible happned, the connection is nil and shouldn't be for connection %s", cn.name)
					return
				}
			} else {
				log.Debugf("Waiting for connection '%s' on backend '%s' to become ready", cn.name, be.name)
				ctxTm, cnclTm := context.WithTimeout(context.Background(), delay*time.Millisecond)
				if delay < 30000 {
					delay += delay
				}
				select {
				case <-ctxTm.Done():
					cnclTm() // Doubt this is required but it's harmless.
					// Do nothing but let the loop continue
				case <-ctx.Done():
					cnclTm()
					// Context was closed, close and exit routine
					//cn.close() NO! let the close be managed externally!
					return
				}
			}
		}
	}(ctx)
}
