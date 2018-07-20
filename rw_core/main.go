package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"
	"errors"
	"strconv"
	"github.com/opencord/voltha-go/db/kvstore"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/rw_core/config"
	"syscall"
)

type rwCore struct {
	kvClient     kvstore.Client
	config       *config.RWCoreFlags
	halted		bool
	exitChannel  chan int
}

func newKVClient(storeType string, address string, timeout int) (kvstore.Client, error) {

	log.Infow("kv-store-type", log.Fields{"store":storeType})
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


func (core *rwCore) start() {
	log.Info("core-starting")

	//First set the KV client.  Some client immediately tries to connect to the KV store (etcd) while others does
	// not create the connection until a request to the store is made (consul)
	tick := time.Tick(kvstore.GetDuration(core.config.KVStoreTimeout))
	connected := false
KVStoreConnectLoop:
	for {
		if err := core.setKVClient(); err != nil {
			log.Warn("cannot-create-kv-client-retrying")
			select {
			case <-tick:
				log.Debug("kv-client-retry")
				continue
			case <-core.exitChannel:
				log.Info("exit-request-received")
				break KVStoreConnectLoop
			}
		} else {
			log.Debug("got-kv-client.")
			connected = true
			break
		}
	}
	// Connected is true only if there is a valid KV store connection and no exit request has been received
	if connected {
		log.Info("core-started")
	} else {
		log.Info("core-ended")
	}
}


func (core *rwCore) stop() {
	// Stop leadership tracking
	core.halted = true

	// send exit signal
	core.exitChannel <- 0

	// Cleanup - applies only if we had a kvClient
	if core.kvClient != nil {
		// Release all reservations
		if err := core.kvClient.ReleaseAllReservations(); err != nil {
			log.Infow("fail-to-release-all-reservations", log.Fields{"error":err})
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
			log.Infow("closing-signal-received", log.Fields{"signal":s})
			exitChannel <- 0
		default:
			log.Infow("unexpected-signal-received", log.Fields{"signal":s})
			exitChannel <- 1
		}
	}()

	code := <-exitChannel
	return code
}

func main() {
	start := time.Now()

	cf := config.NewRWCoreFlags()
	cf.ParseCommandArguments()

	// Setup logging
	if _, err := log.SetLogger(log.JSON, log.DebugLevel, log.Fields{"instanceId":cf.InstanceID}); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	defer log.CleanUp()

	log.Infow("rw-core-config", log.Fields{"config":*cf})

	core := newRWCore(cf)
	go core.start()

	code := waitForExit()
	log.Infow("received-a-closing-signal", log.Fields{"code":code})

	// Cleanup before leaving
	core.stop()

	elapsed := time.Since(start)
	log.Infow("rw-core-run-time", log.Fields{"core":core.config.InstanceID, "time":elapsed/time.Second})
}
