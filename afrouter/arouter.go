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
	"fmt"
	"github.com/opencord/voltha-go/afrouter/afrouter"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/common/version"
	"google.golang.org/grpc/grpclog"
	slog "log"
	"os"
)

func main() {

	conf, err := afrouter.ParseCmd()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, *conf.LogLevel, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	defer log.CleanUp()

	if *conf.DisplayVersionOnly {
		fmt.Println("VOLTHA API Server (afrouter)")
		fmt.Println(version.VersionInfo.String("  "))
		return
	}

	// Parse the config file
	err = conf.LoadConfig()
	if err != nil {
		log.Error(err)
		return
	}
	log.With(log.Fields{"config": *conf}).Debug("Configuration loaded")

	// Enable grpc logging
	if *conf.GrpcLog {
		grpclog.SetLogger(slog.New(os.Stderr, "grpc: ", slog.LstdFlags))
		//grpclog.SetLoggerV2(lgr)
	}

	// Install the signal and error handlers.
	afrouter.InitExitHandler()

	// Create the affinity router proxy...
	if ap, err := afrouter.NewArouterProxy(conf); err != nil {
		log.Errorf("Failed to create the arouter proxy, exiting:%v", err)
		return
		// and start it.
		// This function never returns unless an error
		// occurs or a signal is caught.
	} else if err := ap.ListenAndServe(); err != nil {
		log.Errorf("Exiting on error %v", err)
	}

}
