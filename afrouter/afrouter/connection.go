/*
 * Copyright 2019-present Open Networking Foundation

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
	"time"
)

// connection represents a connection to a single backend
type connection struct {
	backend *backend
	name    string
	addr    string
	port    string
	conn    *grpc.ClientConn
	ctx     context.Context
	close   context.CancelFunc
}

func (cn *connection) connect() {
	ready := make(chan struct{})
	go func() {
		for {
			log.Infof("Connecting to %s with addr: %s and port %s", cn.name, cn.addr, cn.port)
			// Dial doesn't block, it just returns and continues connecting in the background.
			// Check back later to confirm and increase the connection count.

			var err error
			cn.conn, err = grpc.Dial(cn.addr+":"+cn.port, grpc.WithCodec(Codec()), grpc.WithInsecure(), grpc.WithBackoffMaxDelay(time.Second*15))
			if err != nil {
				log.Fatalf("Dialing connection %v:%v", cn, err)
			}

			// cn.conn is defined, connection is ready
			close(ready)

			log.Debugf("Starting the connection monitor for '%s'", cn.name)
			cn.monitor(cn.conn)
			cn.conn.Close()

			select {
			case <-cn.ctx.Done():
				return
			default:
			}
		}
	}()
	<-ready
}

func (cn *connection) getConn() (*grpc.ClientConn, bool) {
	conn := cn.conn
	return conn, conn.GetState() == connectivity.Ready
}

func (cn *connection) monitor(conn *grpc.ClientConn) {
	be := cn.backend
	log.Debugf("Setting up monitoring for backend %s", be.name)
	state := connectivity.Idle
monitorLoop:
	for {
		if !conn.WaitForStateChange(cn.ctx, state) {
			log.Debugf("Context canceled for connection '%s' on backend '%s'", cn.name, be.name)
			break monitorLoop // connection closed
		}

		if newState := conn.GetState(); newState != state {
			previousState := state
			state = newState

			if previousState == connectivity.Ready {
				be.decConn()
				log.Infof("Lost connection '%s' on backend '%s'", cn.name, be.name)
			}

			switch state {
			case connectivity.Ready:
				log.Infof("Connection '%s' on backend '%s' becomes ready", cn.name, be.name)
				be.incConn()
				//cn.backend.splitActiveStreams(cn)
				//RestorePendingStreams(cn, conn)
			case connectivity.TransientFailure, connectivity.Connecting:
				// we don't log these, to avoid spam
			case connectivity.Shutdown:
				// the connection was closed
				log.Infof("Shutdown for connection '%s' on backend '%s'", cn.name, be.name)
				break monitorLoop
			case connectivity.Idle:
				// This can only happen if the server sends a GoAway. This can
				// only happen if the server has modified MaxConnectionIdle from
				// its default of infinity. The only solution here is to close the
				// connection and keepTrying()?
				//TODO: Read the grpc source code to see if there's a different approach
				log.Errorf("Server sent 'GoAway' on connection '%s' on backend '%s'", cn.name, be.name)
				break monitorLoop
			}
		}
	}
}
