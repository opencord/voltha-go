# How to Build and Develop VOLTHA

## Building natively on MAC OS X

For advanced developers this may provide a more comfortable developer environment
(e.g. by allowing IDE-assisted debugging), but setting it up can be a bit more challenging.

### Prerequisites

* git installed
* Docker-for-Mac installed
* Go 1.9
* Python 2.7
* virtualenv
* brew (or macports if you prefer)
* protoc

Get the Voltha-go repository:
```
git clone https://gerrit.opencord.org/voltha-go
cd voltha-go
```

### Setting up the Go environment

After installing Go on the MAC, the GOPATH environment variable should be set to ~/go.
Create a symbolic link in the $GOPATH/src tree to the voltha-go repository:

```
mkdir $GOPATH/src/github.com/opencord
ln -s ~/repos/voltha-go $GOPATH/src/github.com/opencord/voltha-go
```

### Installing Voltha dependencies

```
go get -u google.golang.org/grpc   # gRPC
go get -u github.com/golang-collections/go-datastructures/queue
go get -u github.com/golang/protobuf/protoc-gen-go   # protoc plugin for Go
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/gogo/protobuf/proto   # Clone function
go get -u go.uber.org/zap   # logger
go get -u gopkg.in/Shopify/sarama.v1   # kafka client
go get -u github.com/bsm/sarama-cluster
go get -u github.com/google/uuid
go get -u github.com/cevaris/ordered_map
go get -u github.com/gyuho/goraph
go get -u go.etcd.io/etcd   # etcd client
go install ./vendor/github.com/golang/protobuf/protoc-gen-go 
git clone https://github.com/googleapis/googleapis.git /usr/local/include/googleapis
```

### Building the protobufs
```
cd voltha-go
protos/scripts/build_protos.sh protos
```

### Building and Running Voltha Core locally
A fatal error occurs if Voltha is built and executed at this stage:
```
> go run rw_core/main.go
panic: /debug/requests is already registered.
You may have two independent copies of golang.org/x/net/trace in your binary, trying to maintain separate state.
This may involve a vendored copy of golang.org/x/net/trace.
```
Fix this by removing directory ~/go/src/go.etcd.io/etcd/vendor/golang.org/x/net/trace.

Voltha can now be run directly at the shell prompt:
```
go run rw_core/main.go
```
or from a docker image built via:
```
make rw_core
```

### Building and Running Voltha Core in a container
We are using ```dep``` (https://github.com/golang/dep) for package management.  This means all the 
necessary dependencies are located under the vendor directory.   Whenever, a new package is added to the 
project, please run "dep ensure" to update the appropriate deb files as well as the vendor library.

*Note*: For some reasons (to be investigated) deb does not detect the ```github.com/cores/etcd``` dependency
correctly.  It had to be added manually.  This means everytime a ```dep ensure``` is executed the etcd dependency will 
be removed along as with the directory under the vendor directory.   Until this issue is resolved, please
run ```dep ensure -add  github.com/coreos/etcd```  everytime a "dep ensure" is executed.

To build the voltha core:
```
make rw_core
```

To run the voltha core (example below uses docker compose to run the core locally in a container - replace `````<Host IP>````` 
with your host IP):
```
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/rw_core.yml up -d
```

### Building and running Ponsim OLT and ONU Adapters
Please refer to the README.md file under the ```python``` directory


### Building Simulated OLT and ONU Adapters
Simulated OLT, ONU and rw_core can be build together:
```
make build
```
or they via be individually built:
```
make rw_core
make simulated_olt
make simulated_onu
```

### Running rw_core, Simulated OLT and ONU Adapters
In the example below we are using the docker-compose command to run these containers locally.
```
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/docker-compose-zk-kafka-test.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/docker-compose-etcd.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/rw_core.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/compose/adapters-simulated.yml up -d
```

You should see the following containers up and running

```$xslt
CONTAINER ID        IMAGE                           COMMAND                  CREATED              STATUS              PORTS                                                                      NAMES
338fd67c2029        voltha-adapter-simulated-onu    "/app/simulated_onu …"   37 seconds ago       Up 36 seconds                                                                                  compose_adapter_simulated_onu_1_a39b1a9d27d5
15b159bab626        voltha-adapter-simulated-olt    "/app/simulated_olt …"   37 seconds ago       Up 36 seconds                                                                                  compose_adapter_simulated_olt_1_b5407c23b483
401128a1755f        voltha-rw-core                  "/app/rw_core -kv_st…"   About a minute ago   Up About a minute   0.0.0.0:50057->50057/tcp                                                   compose_rw_core_1_36cd5e255edf
ba4eb9384f5b        quay.io/coreos/etcd:v3.2.9      "etcd --name=etcd0 -…"   About a minute ago   Up About a minute   0.0.0.0:2379->2379/tcp, 0.0.0.0:32775->2380/tcp, 0.0.0.0:32774->4001/tcp   compose_etcd_1_368cd0bc1421
55f74277a530        wurstmeister/kafka:2.11-2.0.1   "start-kafka.sh"         2 minutes ago        Up 2 minutes        0.0.0.0:9092->9092/tcp                                                     compose_kafka_1_a8631e438fe2
fb60076d8b3e        wurstmeister/zookeeper:latest   "/bin/sh -c '/usr/sb…"   2 minutes ago        Up 2 minutes        22/tcp, 2888/tcp, 3888/tcp, 0.0.0.0:2181->2181/tcp                         compose_zookeeper_1_7ff68af103cf
```




