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

// This file implements an exit handler that tries to shut down all the
// running servers before finally exiting. There are 2 triggers to this
// clean exit thread: signals and an exit channel.

package afrouter

import (
	"os"
	"syscall"
	"os/signal"
	"github.com/opencord/voltha-go/common/log"
)

var errChan = make(chan error)
var doneChan = make(chan error)
var holdChan = make(chan int)


func InitExitHandler() error {

	// Start the signal handler
	go signalHandler()
	// Start the error handler
	go errHandler()

	return nil
}

func signalHandler() {
	// Make signal channel and register notifiers for Interupt and Terminate
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt)
	signal.Notify(sigchan, syscall.SIGTERM)

	// Block until we receive a signal on the channel
	<-sigchan

	log.Info("shutting down on signal as requested")

	cleanExit(nil)
}

func errHandler() {

	err := <-errChan

	cleanExit(err)
}

func cleanExit(err error) {
	// Log the shutdown
	if arProxy != nil {
		for _, srvr := range arProxy.servers {
			if srvr.running {
				log.With(log.Fields{"server":srvr.name}).Debug("Closing server")
				srvr.proxyServer.GracefulStop();
				srvr.proxyListener.Close();
			}
		}
	}
	for _,cl := range bClusters {
		for _, bknd := range cl.backends {
			log.Debugf("Closing backend %s", bknd.name)
			for _,conn := range bknd.connections {
				log.Debugf("Closing connection %s", conn.name)
				conn.close()
			}
		}
	}
	doneChan <- err
	//os.Exit(0)
}

