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

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/opencord/voltha-go/ro_core/config"
	c "github.com/opencord/voltha-go/ro_core/core"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	"github.com/opencord/voltha-lib-go/v3/pkg/version"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
)

type roCore struct {
	kvClient    kvstore.Client
	config      *config.ROCoreFlags
	halted      bool
	exitChannel chan int
	core        *c.Core
	//For test
	receiverChannels []<-chan *ic.InterContainerMessage
}

func init() {
	_, err := log.AddPackage(log.JSON, log.DebugLevel, nil)
	if err != nil {
		log.Errorw("unable-to-register-package-to-the-log-map", log.Fields{"error": err})
	}
}

func newKVClient(storeType string, address string, timeout int) (kvstore.Client, error) {

	log.Infow("kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout)
	}
	return nil, errors.New("unsupported-kv-store")
}

func newROCore(cf *config.ROCoreFlags) *roCore {
	var roCore roCore
	roCore.config = cf
	roCore.halted = false
	roCore.exitChannel = make(chan int, 1)
	roCore.receiverChannels = make([]<-chan *ic.InterContainerMessage, 0)
	return &roCore
}

func (ro *roCore) setKVClient() error {
	addr := ro.config.KVStoreHost + ":" + strconv.Itoa(ro.config.KVStorePort)
	client, err := newKVClient(ro.config.KVStoreType, addr, ro.config.KVStoreTimeout)
	if err != nil {
		ro.kvClient = nil
		log.Error(err)
		return err
	}
	ro.kvClient = client
	return nil
}

func (ro *roCore) start(ctx context.Context) {
	log.Info("Starting RW Core components")

	// If the context has a probe then fetch it and register our services
	var p *probe.Probe
	if value := ctx.Value(probe.ProbeContextKey); value != nil {
		if _, ok := value.(*probe.Probe); ok {
			p = value.(*probe.Probe)
			p.RegisterService(
				"kv-store",
				"device-manager",
				"logical-device-manager",
				"grpc-service",
			)
		}
	}

	// Setup KV Client
	log.Debugw("create-kv-client", log.Fields{"kvstore": ro.config.KVStoreType})

	if err := ro.setKVClient(); err != nil {
		log.Fatalw("failed-to-connect-kv-client", log.Fields{"error": err})
		return
	}

	// Create the core service
	ro.core = c.NewCore(ctx, ro.config.InstanceID, ro.config, ro.kvClient)

	if p != nil {
		p.UpdateStatus("kv-store", probe.ServiceStatusPrepared)
	}

	// start the core
	if err := ro.core.Start(ctx); err != nil {
		log.Fatalf("failed-to-start-rocore", log.Fields{"error": err})
	}
}

func (ro *roCore) stop(ctx context.Context) {
	// Stop leadership tracking
	ro.halted = true

	// send exit signal
	ro.exitChannel <- 0

	// Cleanup - applies only if we had a kvClient
	if ro.kvClient != nil {
		// Release all reservations
		if err := ro.kvClient.ReleaseAllReservations(ctx); err != nil {
			log.Infow("fail-to-release-all-reservations", log.Fields{"error": err})
		}
		// Close the DB connection
		ro.kvClient.Close()
	}

	ro.core.Stop(ctx)
}

func waitForExit() int {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	exitChannel := make(chan int)

	go func() {
		s := <-signalChannel
		switch s {
		case syscall.SIGHUP,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT:
			log.Infow("closing-signal-received", log.Fields{"signal": s})
			exitChannel <- 0
		default:
			log.Infow("unexpected-signal-received", log.Fields{"signal": s})
			exitChannel <- 1
		}
	}()

	code := <-exitChannel
	return code
}

func printBanner() {
	fmt.Println()
	fmt.Println(" ____   ___   ____               ")
	fmt.Println("|  _ \\ / _ \\ / ___|___  _ __ ___ ")
	fmt.Println("| |_) | | | | |   / _ \\| '__/ _ \\")
	fmt.Println("|  _ <| |_| | |__| (_) | | |  __/")
	fmt.Println("|_| \\_\\\\___/ \\____\\___/|_|  \\___|")
	fmt.Println()

}

func printVersion() {
	fmt.Println("VOLTHA Read-Only Core")
	fmt.Println(version.VersionInfo.String("  "))
}

func main() {
	start := time.Now()

	cf := config.NewROCoreFlags()
	cf.ParseCommandArguments()

	//// Setup logging

	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, cf.LogLevel, log.Fields{"instanceID": cf.InstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers (provisionned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceID": cf.InstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	log.SetPackageLogLevel("github.com/opencord/voltha-go/ro_core/core", log.DebugLevel)

	defer func() {
		err := log.CleanUp()
		if err != nil {
			log.Errorw("unable-to-flush-any-buffered-log-entries", log.Fields{"error": err})
		}
	}()

	// Print verison / build information and exit
	if cf.DisplayVersionOnly {
		printVersion()
		return
	}

	// Print banner if specified
	if cf.Banner {
		printBanner()
	}

	log.Infow("ro-core-config", log.Fields{"config": *cf})

	// Create the RO Core
	ro := newROCore(cf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	/*
	 * Create and start the liveness and readiness container management probes. This
	 * is done in the main function so just in case the main starts multiple other
	 * objects there can be a single probe end point for the process.
	 */
	p := &probe.Probe{}
	go p.ListenAndServe(fmt.Sprintf("%s:%d", ro.config.ProbeHost, ro.config.ProbePort))

	// Add the probe to the context to pass to all the services started
	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, p)

	// Start the RO core
	go ro.start(probeCtx)

	code := waitForExit()
	log.Infow("received-a-closing-signal", log.Fields{"code": code})

	// Cleanup before leaving
	ro.stop(probeCtx)

	elapsed := time.Since(start)
	log.Infow("ro-core-run-time", log.Fields{"core": ro.config.InstanceID, "time": elapsed / time.Second})
}
