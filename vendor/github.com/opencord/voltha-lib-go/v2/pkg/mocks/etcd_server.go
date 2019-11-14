/*
 * Copyright 2019-present Open Networking Foundation

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
package mocks

import (
	"go.etcd.io/etcd/embed"
	"log"
	"os"
	"time"
)

const (
	serverStartUpTimeout   = 10 * time.Second // Maximum time allowed to wait for the Etcd server to be ready
	localPersistentStorage = "voltha.embed.etcd"
)

//EtcdServer represents an embedded Etcd server.  It is used for testing only.
type EtcdServer struct {
	server *embed.Etcd
}

//getDefaultCfg specifies the default config
func getDefaultCfg() *embed.Config {
	cfg := embed.NewConfig()
	cfg.Dir = localPersistentStorage
	cfg.Logger = "zap"
	cfg.LogLevel = "error"
	return cfg
}

//StartEtcdServer creates and starts an embedded Etcd server.  A local directory to store data is created for the
//embedded server lifetime (for the duration of a unit test.  The server runs at localhost:2379.
func StartEtcdServer(cfg *embed.Config) *EtcdServer {
	// If the server is already running, just return
	if cfg == nil {
		cfg = getDefaultCfg()
	}
	// Remove the local directory as
	// a safeguard for the case where a prior test failed
	if err := os.RemoveAll(localPersistentStorage); err != nil {
		log.Fatalf("Failure removing local directory %s", localPersistentStorage)
	}
	e, err := embed.StartEtcd(cfg)
	if err != nil {
		log.Fatal(err)
	}
	select {
	case <-e.Server.ReadyNotify():
		log.Printf("Embedded Etcd server is ready!")
	case <-time.After(serverStartUpTimeout):
		e.Server.HardStop() // trigger a shutdown
		e.Close()
		log.Fatal("Embedded Etcd server took too long to start!")
	case err := <-e.Err():
		e.Server.HardStop() // trigger a shutdown
		e.Close()
		log.Fatalf("Embedded Etcd server errored out - %s", err)
	}
	return &EtcdServer{server: e}
}

//Stop closes the embedded Etcd server and removes the local data directory as well
func (es *EtcdServer) Stop() {
	if es != nil {
		es.server.Server.HardStop()
		es.server.Close()
		if err := os.RemoveAll(localPersistentStorage); err != nil {
			log.Fatalf("Failure removing local directory %s", es.server.Config().Dir)
		}
	}
}
