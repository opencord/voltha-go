package config

import (
	//"context"
	"flag"
	"fmt"
	//dt "github.com/docker/docker/api/types"
	//dc "github.com/docker/docker/client"
	"os"
	"time"
)

// Constants used to differentiate between the KV stores
const (
	ConsulStoreName string = "consul"
	EtcdStoreName   string = "etcd"
)

// CoordinatorFlags represents the set of configurations used by the coordinator
type RWCoreFlags struct {
	// Command line parameters
	InstanceID     string
	KVStoreType    string
	KVStoreTimeout int // in seconds
	KVStoreHost    string
	KVStorePort    int
	LogLevel       string
}

// NewRWCoreFlags returns a new coordinator config
func NewRWCoreFlags() *RWCoreFlags {
	var rwCoreFlag = RWCoreFlags{ // Default values
		InstanceID:     "rw_coreInstance001",
		KVStoreType:    ConsulStoreName,
		KVStoreTimeout: 5,
		KVStoreHost:    "10.100.198.240",
		//KVStorePort:                 2379,
		KVStorePort: 8500,
		LogLevel:    "info",
	}
	return &rwCoreFlag
}

// ParseCommandArguments parses the arguments when running coordinator
func (cf *RWCoreFlags) ParseCommandArguments() {
	flag.IntVar(&(cf.KVStoreTimeout),
		"kv-store-request-timeout",
		cf.KVStoreTimeout,
		"The default timeout when making a kv store request")

	flag.StringVar(&(cf.KVStoreType),
		"kv-store-type",
		cf.KVStoreType,
		"KV store type")

	flag.StringVar(&(cf.KVStoreHost),
		"kv-store-host",
		cf.KVStoreHost,
		"KV store host")

	flag.IntVar(&(cf.KVStorePort),
		"kv-store-port",
		cf.KVStorePort,
		"KV store port")

	flag.StringVar(&(cf.LogLevel),
		"log-level",
		cf.LogLevel,
		"Log level")

	flag.Parse()

	// Update the necessary keys with the prefixes
	start := time.Now()
	containerName := getContainerInfo()
	fmt.Println("container name:", containerName)
	if len(containerName) > 0 {
		cf.InstanceID = containerName
	}

	fmt.Println("Inside config:", cf)
	elapsed := time.Since(start)
	fmt.Println("time:", elapsed/time.Second)
}

func getContainerInfo() string {
	return os.Getenv("HOSTNAME")
}
