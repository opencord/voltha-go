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
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-lib-go/v4/pkg/probe"
	"github.com/opencord/voltha-lib-go/v4/pkg/version"
)

func waitForExit(ctx context.Context) int {
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
		logger.Infow(ctx, "closing-signal-received", log.Fields{"signal": s})
		return 0
	default:
		logger.Infow(ctx, "unexpected-signal-received", log.Fields{"signal": s})
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

	ctx := context.Background()

	cf := config.NewRWCoreFlags()
	cf.ParseCommandArguments()

	// Set the instance ID as the hostname
	var instanceID string
	hostName := utils.GetHostName()
	if len(hostName) > 0 {
		instanceID = hostName
	} else {
		logger.Fatal(ctx, "HOSTNAME not set")
	}

	realMain()

	logLevel, err := log.StringToLogLevel(cf.LogLevel)
	if err != nil {
		panic(err)
	}

	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, logLevel, log.Fields{"instanceId": instanceID}); err != nil {
		logger.With(log.Fields{"error": err}).Fatal(ctx, "Cannot setup logging")
	}

	// Update all loggers (provisioned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": instanceID}); err != nil {
		logger.With(log.Fields{"error": err}).Fatal(ctx, "Cannot setup logging")
	}

	// Update all loggers to log level specified as input parameter
	log.SetAllLogLevel(logLevel)

	defer func() {
		err := log.CleanUp()
		if err != nil {
			logger.Errorw(ctx, "unable-to-flush-any-buffered-log-entries", log.Fields{"error": err})
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

	logger.Infow(ctx, "rw-core-config", log.Fields{"config": *cf})

	// Create a context adding the status update channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	/*
	 * Create and start the liveness and readiness container management probes. This
	 * is done in the main function so just in case the main starts multiple other
	 * objects there can be a single probe end point for the process.
	 */
	p := &probe.Probe{}
	go p.ListenAndServe(ctx, cf.ProbeAddress)

	// Add the probe to the context to pass to all the services started
	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, p)

	closer, err := log.GetGlobalLFM().InitTracingAndLogCorrelation(cf.TraceEnabled, cf.TraceAgentAddress, cf.LogCorrelationEnabled)
	if err != nil {
		logger.Warnw(ctx, "unable-to-initialize-tracing-and-log-correlation-module", log.Fields{"error": err})
	} else {
		defer closer.Close()
	}

	// create and start the core
	core := c.NewCore(probeCtx, instanceID, cf)

	code := waitForExit(ctx)
	logger.Infow(ctx, "received-a-closing-signal", log.Fields{"code": code})

	// Cleanup before leaving
	core.Stop()

	elapsed := time.Since(start)
	logger.Infow(ctx, "rw-core-run-time", log.Fields{"core": instanceID, "time": elapsed / time.Second})
}
