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
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opencord/voltha-go/rw_core/config"
	c "github.com/opencord/voltha-go/rw_core/core"
	"github.com/opencord/voltha-go/rw_core/utils"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	"github.com/opencord/voltha-lib-go/v3/pkg/version"
)

func waitForExit() int {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	s := <-signalChannel
	switch s {
	case syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT:
		logger.Infow("closing-signal-received", log.Fields{"signal": s})
		return 0
	default:
		logger.Infow("unexpected-signal-received", log.Fields{"signal": s})
		return 1
	}
}

func printBanner() {
	fmt.Println(`                                    `)
	fmt.Println(` ______        ______               `)
	fmt.Println(`|  _ \ \      / / ___|___  _ __ ___ `)
	fmt.Println(`| |_) \ \ /\ / / |   / _ \| '__/ _ \`)
	fmt.Println(`|  _ < \ V  V /| |__| (_) | | |  __/`)
	fmt.Println(`|_| \_\ \_/\_/  \____\___/|_|  \___|`)
	fmt.Println(`                                    `)
}

func printVersion() {
	fmt.Println("VOLTHA Read-Write Core")
	fmt.Println(version.VersionInfo.String("  "))
}

func main() {
	start := time.Now()

	cf := config.NewRWCoreFlags()
	cf.ParseCommandArguments()

	// Set the instance ID as the hostname
	var instanceID string
	hostName := utils.GetHostName()
	if len(hostName) > 0 {
		instanceID = hostName
	} else {
		logger.Fatal("HOSTNAME not set")
	}

	realMain()

	logLevel, err := log.StringToLogLevel(cf.LogLevel)
	if err != nil {
		panic(err)
	}

	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, logLevel, log.Fields{"instanceId": instanceID}); err != nil {
		logger.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers (provisioned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": instanceID}); err != nil {
		logger.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers to log level specified as input parameter
	log.SetAllLogLevel(logLevel)

	//log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)

	defer func() {
		err := log.CleanUp()
		if err != nil {
			logger.Errorw("unable-to-flush-any-buffered-log-entries", log.Fields{"error": err})
		}
	}()

	// Print version / build information and exit
	if cf.DisplayVersionOnly {
		printVersion()
		return
	}

	// Print banner if specified
	if cf.Banner {
		printBanner()
	}

	logger.Infow("rw-core-config", log.Fields{"config": *cf})

	// Create a context adding the status update channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	/*
	 * Create and start the liveness and readiness container management probes. This
	 * is done in the main function so just in case the main starts multiple other
	 * objects there can be a single probe end point for the process.
	 */
	p := &probe.Probe{}
	go p.ListenAndServe(cf.ProbeAddress)

	// Add the probe to the context to pass to all the services started
	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, p)

	// create and start the core
	core := c.NewCore(probeCtx, instanceID, cf)

	code := waitForExit()
	logger.Infow("received-a-closing-signal", log.Fields{"code": code})

	// Cleanup before leaving
	core.Stop()

	elapsed := time.Since(start)
	logger.Infow("rw-core-run-time", log.Fields{"core": instanceID, "time": elapsed / time.Second})
}
