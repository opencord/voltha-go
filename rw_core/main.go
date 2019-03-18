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
	grpcserver "github.com/opencord/voltha-go/common/grpc"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-go/kafka"
	ic "github.com/opencord/voltha-go/protos/inter_container"
	"github.com/opencord/voltha-go/rw_core/config"
	c "github.com/opencord/voltha-go/rw_core/core"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type rwCore struct {
	kvClient    kvstore.Client
	config      *config.RWCoreFlags
	halted      bool
	exitChannel chan int
	//kmp         *kafka.KafkaMessagingProxy
	grpcServer  *grpcserver.GrpcServer
	kafkaClient kafka.Client
	core        *c.Core
	//For test
	receiverChannels []<-chan *ic.InterContainerMessage
}

func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
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

func newKafkaClient(clientType string, host string, port int, instanceID string) (kafka.Client, error) {

	log.Infow("kafka-client-type", log.Fields{"client": clientType})
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
			kafka.ProducerRetryBackoff(time.Millisecond*30)), nil
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

func (rw *rwCore) setKVClient() error {
	addr := rw.config.KVStoreHost + ":" + strconv.Itoa(rw.config.KVStorePort)
	client, err := newKVClient(rw.config.KVStoreType, addr, rw.config.KVStoreTimeout)
	if err != nil {
		rw.kvClient = nil
		log.Error(err)
		return err
	}
	rw.kvClient = client
	return nil
}

func toString(value interface{}) (string, error) {
	switch t := value.(type) {
	case []byte:
		return string(value.([]byte)), nil
	case string:
		return value.(string), nil
	default:
		return "", fmt.Errorf("unexpected-type-%T", t)
	}
}

func (rw *rwCore) start(ctx context.Context) {
	log.Info("Starting RW Core components")

	// Setup KV Client
	log.Debugw("create-kv-client", log.Fields{"kvstore": rw.config.KVStoreType})
	err := rw.setKVClient()
	if err == nil {
		// Setup KV transaction context
		txnPrefix := rw.config.KVStoreDataPrefix + "/transactions/"
		if err = c.SetTransactionContext(rw.config.InstanceID,
			txnPrefix,
			rw.kvClient,
			rw.config.KVStoreTimeout,
			rw.config.KVTxnKeyDelTime,
			10); err != nil {
			log.Fatal("creating-transaction-context-failed")
		}
	}

	// Setup Kafka Client
	if rw.kafkaClient, err = newKafkaClient("sarama", rw.config.KafkaAdapterHost, rw.config.KafkaAdapterPort, rw.config.InstanceID); err != nil {
		log.Fatal("Unsupported-kafka-client")
	}

	// Create the core service
	rw.core = c.NewCore(rw.config.InstanceID, rw.config, rw.kvClient, rw.kafkaClient)

	// start the core
	rw.core.Start(ctx)
}

func (rw *rwCore) stop() {
	// Stop leadership tracking
	rw.halted = true

	// send exit signal
	rw.exitChannel <- 0

	// Cleanup - applies only if we had a kvClient
	if rw.kvClient != nil {
		// Release all reservations
		if err := rw.kvClient.ReleaseAllReservations(); err != nil {
			log.Infow("fail-to-release-all-reservations", log.Fields{"error": err})
		}
		// Close the DB connection
		rw.kvClient.Close()
	}

	rw.core.Stop(nil)

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
	fmt.Println("                                            ")
	fmt.Println(" ______        ______                       ")
	fmt.Println("|  _ \\ \\      / / ___|___  _ __ ___       ")
	fmt.Println("| |_) \\ \\ /\\ / / |   / _ \\| '__/ _ \\   ")
	fmt.Println("|  _ < \\ V  V /| |__| (_) | | |  __/       ")
	fmt.Println("|_| \\_\\ \\_/\\_/  \\____\\___/|_|  \\___| ")
	fmt.Println("                                            ")
}

func main() {
	start := time.Now()

	cf := config.NewRWCoreFlags()
	cf.ParseCommandArguments()

	//// Setup logging

	//Setup default logger - applies for packages that do not have specific logger set
	if _, err := log.SetDefaultLogger(log.JSON, cf.LogLevel, log.Fields{"instanceId": cf.InstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	// Update all loggers (provisionned via init) with a common field
	if err := log.UpdateAllLoggers(log.Fields{"instanceId": cf.InstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	log.SetPackageLogLevel("github.com/opencord/voltha-go/rw_core/core", log.DebugLevel)
	log.SetPackageLogLevel("github.com/opencord/voltha-go/kafka", log.DebugLevel)

	defer log.CleanUp()

	// Print banner if specified
	if cf.Banner {
		printBanner()
	}

	log.Infow("rw-core-config", log.Fields{"config": *cf})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rw := newRWCore(cf)
	go rw.start(ctx)

	code := waitForExit()
	log.Infow("received-a-closing-signal", log.Fields{"code": code})

	// Cleanup before leaving
	rw.stop()

	elapsed := time.Since(start)
	log.Infow("rw-core-run-time", log.Fields{"core": rw.config.InstanceID, "time": elapsed / time.Second})
}
