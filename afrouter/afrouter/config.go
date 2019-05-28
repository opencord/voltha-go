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

package afrouter

// Command line parameters and parsing
import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"io/ioutil"
	"os"
	"path"
)

func ParseCmd() (*Configuration, error) {
	config := &Configuration{}
	cmdParse := flag.NewFlagSet(path.Base(os.Args[0]), flag.ContinueOnError)
	config.ConfigFile = cmdParse.String("config", "arouter.json", "The configuration file for the affinity router")
	config.LogLevel = cmdParse.Int("logLevel", 0, "The log level for the affinity router")
	config.GrpcLog = cmdParse.Bool("grpclog", false, "Enable GRPC logging")
	config.Version = cmdParse.Bool("version", false, "Print version information and exit")

	err := cmdParse.Parse(os.Args[1:])
	if err != nil {
		//return err
		return nil, errors.New("Error parsing the command line")
	}
	//if(!cmdParse.Parsed()) {
	//}
	return config, nil
}

// Configuration file loading and parsing
type Configuration struct {
	ConfigFile         *string
	LogLevel           *int
	GrpcLog            *bool
	Version            *bool
	Servers            []ServerConfig         `json:"servers"`
	Ports              PortConfig             `json:"ports"`
	ServerCertificates ServerCertConfig       `json:"serverCertificates"`
	ClientCertificates ClientCertConfig       `json:"clientCertificates"`
	BackendClusters    []BackendClusterConfig `json:"backend_clusters"`
	Routers            []RouterConfig         `json:"routers"`
	Api                ApiConfig
}

type RouterConfig struct {
	Name         string        `json:"name"`
	ProtoService string        `json:"service"`
	ProtoPackage string        `json:"package"`
	Routes       []RouteConfig `json:"routes"`
}

type RouteConfig struct {
	Name             string           `json:"name"`
	Type             routeType        `json:"type"`
	ProtoFile        string           `json:"proto_descriptor"`
	Association      associationType  `json:"association"`
	RouteField       string           `json:"routing_field"`
	Methods          []string         `json:"methods"` // The GRPC methods to route using the route field
	NbBindingMethods []string         `json:"nb_binding_methods"`
	BackendCluster   string           `json:"backend_cluster"`
	Binding          BindingConfig    `json:"binding"`
	Overrides        []OverrideConfig `json:"overrides"`
	backendCluster   *BackendClusterConfig
}

type BindingConfig struct {
	Type        string          `json:"type"`
	Field       string          `json:"field"`
	Method      string          `json:"method"`
	Association associationType `json:"association"`
}

type OverrideConfig struct {
	Methods    []string `json:"methods"`
	Method     string   `json:"method"`
	RouteField string   `json:"routing_field"`
}

// Backend configuration

type BackendClusterConfig struct {
	Name     string          `json:"name"`
	Backends []BackendConfig `json:"backends"`
}

type BackendConfig struct {
	Name        string             `json:"name"`
	Type        backendType        `json:"type"`
	Association AssociationConfig  `json:"association"`
	Connections []ConnectionConfig `json:"connections"`
}

type AssociationConfig struct {
	Strategy associationStrategy `json:"strategy"`
	Location associationLocation `json:"location"`
	Field    string              `json:"field"`
	Key      string              `json:"key"`
}

type ConnectionConfig struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Port string `json:"port"`
}

// Server configuration

type ServerConfig struct {
	Name    string          `json:"name"`
	Port    uint            `json:"port"`
	Addr    string          `json:"address"`
	Type    string          `json:"type"`
	Routers []RouterPackage `json:"routers"`
	routers map[string]*RouterConfig
}

type RouterPackage struct {
	Router  string `json:"router"`
	Package string `json:"package"`
}

// Port configuration
type PortConfig struct {
	GrpcPort             uint `json:"grpcPort"`
	StreamingGrpcPort    uint `json:"streamingGrpcPort"`
	TlsGrpcPort          uint `json:"tlsGrpcPort"`
	TlsStreamingGrpcPort uint `json:"tlsStreamingGrpcPort"`
	ControlPort          uint `json:"controlPort"`
}

// Server Certificate configuration
type ServerCertConfig struct {
	GrpcCert string `json:"grpcCertificate"` // File path to the certificate file
	GrpcKey  string `json:"grpcKey"`         // File path to the key file
	GrpcCsr  string `json:"grpcCsr"`         // File path to the CSR file
}

// Client Certificate configuration
type ClientCertConfig struct {
	GrpcCert string `json:"grpcCertificate"` // File path to the certificate file
	GrpcKey  string `json:"grpcKey"`         // File path to the key file
	GrpcCsr  string `json:"grpcCsr"`         // File path to the CSR file
}

// Api configuration
type ApiConfig struct {
	Addr string `json:"address"`
	Port uint   `json:"port"`
}

func (conf *Configuration) LoadConfig() error {

	configF, err := os.Open(*conf.ConfigFile)
	log.Info("Loading configuration from: ", *conf.ConfigFile)
	if err != nil {
		log.Error(err)
		return err
	}

	defer configF.Close()

	configBytes, err := ioutil.ReadAll(configF)
	if err != nil {
		log.Error(err)
		return err
	}

	if err := json.Unmarshal(configBytes, conf); err != nil {
		log.Errorf("Unmarshaling of the configuratino file failed: %v", err)
		return err
	}

	// Now resolve references to different config objects in the
	// config file. Currently there are 2 possible references
	// to resolve: referecnes to routers in the servers, and
	// references to backend_cluster in the routers.

	// Resolve router references for the servers
	log.Debug("Resolving references in the config file")
	for k := range conf.Servers {
		//s.routers =make(map[string]*RouterConfig)
		conf.Servers[k].routers = make(map[string]*RouterConfig)
		for _, rPkg := range conf.Servers[k].Routers {
			var found = false
			// Locate the router "r" in the top lever Routers array
			log.Debugf("Resolving router reference to router '%s' from server '%s'", rPkg.Router, conf.Servers[k].Name)
			for rk := range conf.Routers {
				if conf.Routers[rk].Name == rPkg.Router && !found {
					log.Debugf("Reference to router '%s' found for package '%s'", rPkg.Router, rPkg.Package)
					conf.Servers[k].routers[rPkg.Package] = &conf.Routers[rk]
					found = true
				} else if conf.Routers[rk].Name == rPkg.Router && found {
					if _, ok := conf.Servers[k].routers[rPkg.Package]; !ok {
						log.Debugf("Reference to router '%s' found for package '%s'", rPkg.Router, rPkg.Package)
						conf.Servers[k].routers[rPkg.Package] = &conf.Routers[rk]
					} else {
						err := errors.New(fmt.Sprintf("Duplicate router '%s' defined for package '%s'", rPkg.Router, rPkg.Package))
						log.Error(err)
						return err
					}
				}
			}
			if !found {
				err := errors.New(fmt.Sprintf("Router %s for server %s not found in config", conf.Servers[k].Name, rPkg.Router))
				log.Error(err)
				return err
			}
		}
	}

	// Resolve backend references for the routers
	for rk, rv := range conf.Routers {
		for rtk, rtv := range rv.Routes {
			var found = false
			log.Debugf("Resolving backend reference to %s from router %s", rtv.BackendCluster, rv.Name)
			for bek, bev := range conf.BackendClusters {
				log.Debugf("Checking cluster %s", conf.BackendClusters[bek].Name)
				if rtv.BackendCluster == bev.Name && !found {
					conf.Routers[rk].Routes[rtk].backendCluster = &conf.BackendClusters[bek]
					found = true
				} else if rtv.BackendCluster == bev.Name && found {
					err := errors.New(fmt.Sprintf("Duplicate backend defined, %s", conf.BackendClusters[bek].Name))
					log.Error(err)
					return err
				}
			}
			if !found {
				err := errors.New(fmt.Sprintf("Backend %s for router %s not found in config",
					rtv.BackendCluster, rv.Name))
				log.Error(err)
				return err
			}
		}
	}

	return nil
}
