package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-go/kafka"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"github.com/opencord/voltha-go/rw_core/config"
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
	kmp         *kafka.KafkaMessagingProxy
	//For test
	receiverChannels []<-chan *ca.InterContainerMessage
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

func newRWCore(cf *config.RWCoreFlags) *rwCore {
	var rwCore rwCore
	rwCore.config = cf
	rwCore.halted = false
	rwCore.exitChannel = make(chan int, 1)
	rwCore.receiverChannels = make([]<-chan *ca.InterContainerMessage, 0)
	return &rwCore
}

func (core *rwCore) setKVClient() error {
	addr := core.config.KVStoreHost + ":" + strconv.Itoa(core.config.KVStorePort)
	client, err := newKVClient(core.config.KVStoreType, addr, core.config.KVStoreTimeout)
	if err != nil {
		log.Error(err)
		return err
	}
	core.kvClient = client
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


func (core *rwCore) start(ctx context.Context) {
	log.Info("Starting RW Core components")
	// Setup GRPC Server

	// Setup KV Client

	// Setup Kafka messaging services
	var err error
	if core.kmp, err = kafka.NewKafkaMessagingProxy(
		kafka.KafkaHost("10.100.198.220"),
		kafka.KafkaPort(9092),
		kafka.DefaultTopic(&kafka.Topic{Name: "Adapter"})); err != nil {
		log.Errorw("fail-to-create-kafka-proxy", log.Fields{"error": err})
		return
	}
	// Start the kafka messaging service - synchronous call to ensure
	if err = core.kmp.Start(); err != nil {
		log.Fatalw("error-starting-messaging-proxy", log.Fields{"error": err})
	}
}

func (core *rwCore) stop() {
	// Stop leadership tracking
	core.halted = true

	// Stop the Kafka messaging service
	if core.kmp != nil {
		core.kmp.Stop()
	}

	// send exit signal
	core.exitChannel <- 0

	// Cleanup - applies only if we had a kvClient
	if core.kvClient != nil {
		// Release all reservations
		if err := core.kvClient.ReleaseAllReservations(); err != nil {
			log.Infow("fail-to-release-all-reservations", log.Fields{"error": err})
		}
		// Close the DB connection
		core.kvClient.Close()
	}
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

	// Setup logging
	if _, err := log.SetLogger(log.JSON, cf.LogLevel, log.Fields{"instanceId": cf.InstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	defer log.CleanUp()

	// Print banner if specified
	if cf.Banner {
		printBanner()
	}

	log.Infow("rw-core-config", log.Fields{"config": *cf})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	core := newRWCore(cf)
	go core.start(ctx)

	code := waitForExit()
	log.Infow("received-a-closing-signal", log.Fields{"code": code})

	// Cleanup before leaving
	core.stop()

	elapsed := time.Since(start)
	log.Infow("rw-core-run-time", log.Fields{"core": core.config.InstanceID, "time": elapsed / time.Second})
}
