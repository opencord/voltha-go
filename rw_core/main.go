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

	"github.com/opencord/voltha-go/rw_core/config"
	c "github.com/opencord/voltha-go/rw_core/core"
	"github.com/opencord/voltha-go/rw_core/utils"
	conf "github.com/opencord/voltha-lib-go/v3/pkg/config"
	"github.com/opencord/voltha-lib-go/v3/pkg/db/kvstore"
	"github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	"github.com/opencord/voltha-lib-go/v3/pkg/probe"
	"github.com/opencord/voltha-lib-go/v3/pkg/version"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
)

type rwCore struct {
	kvClient    kvstore.Client
	config      *config.RWCoreFlags
	halted      bool
	exitChannel chan int
	//kmp         *kafka.KafkaMessagingProxy
	kafkaClient kafka.Client
	core        *c.Core
	//For test
	receiverChannels []<-chan *ic.InterContainerMessage
}

func newKVClient(storeType string, address string, timeout int) (kvstore.Client, error) {

	logger.Infow("kv-store-type", log.Fields{"store": storeType})
	switch storeType {
	case "consul":
		return kvstore.NewConsulClient(address, timeout)
	case "etcd":
		return kvstore.NewEtcdClient(address, timeout, kvstore.DefaultLogLevel)
	}
	return nil, errors.New("unsupported-kv-store")
}

func newKafkaClient(clientType string, host string, port int, instanceID string, livenessChannelInterval time.Duration) (kafka.Client, error) {

	logger.Infow("kafka-client-type", log.Fields{"client": clientType})
	switch clientType {
	case "sarama":
		return kafka.NewSaramaClient(
			kafka.Host(host),
			kafka.Port(port),
			kafka.ConsumerType(kafka.GroupCustomer),
			kafka.ProducerReturnOnErrors(true),
			kafka.ProducerReturnOnSuccess(true),
			kafka.ProducerMaxRetries(6),
			kafka.NumPartitions(3),
			kafka.ConsumerGroupName(instanceID),
			kafka.ConsumerGroupPrefix(instanceID),
			kafka.AutoCreateTopic(true),
			kafka.ProducerFlushFrequency(5),
			kafka.ProducerRetryBackoff(time.Millisecond*30),
			kafka.LivenessChannelInterval(livenessChannelInterval),
		), nil
	}
	return nil, errors.New("unsupported-client-type")
}

func newRWCore(cf *config.RWCoreFlags) *rwCore {
	var rwCore rwCore
	rwCore.config = cf
	rwCore.halted = false
	rwCore.exitChannel = make(chan int, 1)
	rwCore.receiverChannels = make([]<-chan *ic.InterContainerMessage, 0)
	return &rwCore
}

func (rw *rwCore) start(ctx context.Context, instanceID string) {
	logger.Info("Starting RW Core components")

	// Setup KV Client
	logger.Debugw("create-kv-client", log.Fields{"kvstore": rw.config.KVStoreType})
	var err error
	if rw.kvClient, err = newKVClient(
		rw.config.KVStoreType,
		rw.config.KVStoreHost+":"+strconv.Itoa(rw.config.KVStorePort),
		rw.config.KVStoreTimeout); err != nil {
		logger.Fatal(err)
	}
	cm := conf.NewConfigManager(rw.kvClient, rw.config.KVStoreType, rw.config.KVStoreHost, rw.config.KVStorePort, rw.config.KVStoreTimeout)
	go conf.StartLogLevelConfigProcessing(cm, ctx)

	// Setup Kafka Client
	if rw.kafkaClient, err = newKafkaClient("sarama",
		rw.config.KafkaAdapterHost,
		rw.config.KafkaAdapterPort,
		instanceID,
		rw.config.LiveProbeInterval/2); err != nil {
		logger.Fatal("Unsupported-kafka-client")
	}

	// Create the core service
	rw.core = c.NewCore(ctx, instanceID, rw.config, rw.kvClient, rw.kafkaClient)

	// start the core
	err = rw.core.Start(ctx)
	if err != nil {
		logger.Fatalf("failed-to-start-rwcore", log.Fields{"error": err})
	}
}

func (rw *rwCore) stop(ctx context.Context) {
	// Stop leadership tracking
	rw.halted = true

	// send exit signal
	rw.exitChannel <- 0

	// Cleanup - applies only if we had a kvClient
	if rw.kvClient != nil {
		// Release all reservations
		if err := rw.kvClient.ReleaseAllReservations(ctx); err != nil {
			logger.Infow("fail-to-release-all-reservations", log.Fields{"error": err})
		}
		// Close the DB connection
		rw.kvClient.Close()
	}

	rw.core.Stop(ctx)

	//if rw.kafkaClient != nil {
	//	rw.kafkaClient.Stop()
	//}
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
			logger.Infow("closing-signal-received", log.Fields{"signal": s})
			exitChannel <- 0
		default:
			logger.Infow("unexpected-signal-received", log.Fields{"signal": s})
			exitChannel <- 1
		}
	}()

	code := <-exitChannel
	return code
}

func printBanner() {
	fmt.Println("                                            ")
	fmt.Println(" ______        ______                       ")
	fmt.Println("|  _ \\ \\      / / ___|___  _ __ ___       ")
	fmt.Println("| |_) \\ \\ /\\ / / |   / _ \\| '__/ _ \\   ")
	fmt.Println("|  _ < \\ V  V /| |__| (_) | | |  __/       ")
	fmt.Println("|_| \\_\\ \\_/\\_/  \\____\\___/|_|  \\___| ")
	fmt.Println("                                            ")
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

	// Create the core
	rw := newRWCore(cf)

	// Create a context adding the status update channel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	/*
	 * Create and start the liveness and readiness container management probes. This
	 * is done in the main function so just in case the main starts multiple other
	 * objects there can be a single probe end point for the process.
	 */
	p := &probe.Probe{}
	go p.ListenAndServe(fmt.Sprintf("%s:%d", rw.config.ProbeHost, rw.config.ProbePort))

	// Add the probe to the context to pass to all the services started
	probeCtx := context.WithValue(ctx, probe.ProbeContextKey, p)

	// Start the core
	go rw.start(probeCtx, instanceID)

	code := waitForExit()
	logger.Infow("received-a-closing-signal", log.Fields{"code": code})

	// Cleanup before leaving
	rw.stop(probeCtx)

	elapsed := time.Since(start)
	logger.Infow("rw-core-run-time", log.Fields{"core": instanceID, "time": elapsed / time.Second})
}
