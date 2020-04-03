# How to Build and Develop VOLTHA

Please refer to this guide: https://docs.voltha.org/master/overview/dev_virtual.html

## Building natively on MAC OS X

For advanced developers this may provide a more comfortable developer environment
(e.g. by allowing IDE-assisted debugging), but setting it up can be a bit more challenging.

### Prerequisites

* git installed
* Docker-for-Mac installed
* Go 1.12
* Python 2.7
* virtualenv
* brew (or macports if you prefer)
* protoc


Get the Voltha-go repository.  Its assumed in these notes that voltha-go is checked out into ~/repos
Other notes refer to ~/source being used.  Update paths as needed.
```sh
mkdir ~/repos
cd ~/repos
git clone https://gerrit.opencord.org/voltha-go
cd voltha-go
```



### Setting up the Go environment

After installing Go on the MAC, the GOPATH environment variable should be set to ~/go.
Create a symbolic link in the $GOPATH/src tree to the voltha-go repository:
```sh
mkdir -p $GOPATH/src/github.com/opencord
ln -s ~/repos/voltha-go $GOPATH/src/github.com/opencord/voltha-go
```



### Installing VOLTHA dependencies

Install dep for fetching go protos
```sh
mkdir -p $GOPATH/bin
curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
```

Pull and build dependencies for the project.  This may take a while.  This may likely update versions of 3rd party packages in the vendor folder.   This may introduce unexpected changes.   If everything still works feel free to check in the vendor updates as needed.
```sh
cd $GOPATH/src/github.com/opencord/voltha-go/
dep ensure
```

### Building and Running VOLTHA Core locally


VOLTHA can now be built and run directly at the shell prompt:
```sh
go build rw_core/main.go
go run rw_core/main.go
```

Or build a docker image:
```sh
make rw_core
```



A fatal error can possibly occur if VOLTHA is built and executed at this stage:

```sh
go run rw_core/main.go

panic: /debug/requests is already registered.
You may have two independent copies of golang.org/x/net/trace in your binary, trying to maintain separate state.
This may involve a vendored copy of golang.org/x/net/trace.
```
Fix this by removing directory ~/go/src/go.etcd.io/etcd/vendor/golang.org/x/net/trace.



### Building and Running VOLTHA Core in a container

We are using ```dep``` (https://github.com/golang/dep) for package management.  This means all the necessary dependencies are located under the vendor directory and checked in.  Whenever a new package is added to the project, please run "dep ensure" to update the appropriate deb files as well as the vendor library.

To build the voltha core and other containers:
```sh
make build
```

To run the voltha core (example below uses docker compose to run the core locally in a container - replace `````<Host IP>`````
with your host IP):

```sh
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/rw_core.yml up -d
```

### Building with a Local Copy of `voltha-protos` or `voltha-lib-go`

If you want to build/test using a local copy or `voltha-protos` or `voltha-lib-go`
this can be accomplished by using the environment variables `LOCAL_PROTOS` and
`LOCAL_LIB_GO`. These environment variables should be set to the filesystem
path where the local source is located, e.g.

```bash
LOCAL\_PROTOS=$HOME/src/voltha-protos
LOCAL\_LIB\_GO=$HOME/src/voltha-lib-go
```

When these environment variables are set the vendored versions of these packages
will be removed from the `vendor` directory and replaced by coping the files from
the specified locations to the `vendor` directory. *NOTE:* _this means that
the files in the `vendor` directory are no longer what is in the `git` repository
and it will take manual `git` intervention to put the original files back._


### Building and running Ponsim OLT and ONU Adapters

Please refer to the README.md file under the ```python``` directory



### Building Simulated OLT and ONU Adapters

Simulated OLT, ONU and rw_core can be build together:
```sh
make build
```
or they via be individually built:
```sh
make rw_core
make simulated_olt
make simulated_onu
```



### Running rw_core, Simulated OLT and ONU Adapters

In the example below we are using the docker-compose command to run these containers locally.
```sh
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/docker-compose-zk-kafka-test.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/docker-compose-etcd.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/rw_core.yml up -d
DOCKER_HOST_IP=<Host IP> docker-compose -f compose/adapters-simulated.yml up -d
```



You should see the following containers up and running

```sh
CONTAINER ID        IMAGE                           COMMAND                  CREATED              STATUS              PORTS                                                                      NAMES
338fd67c2029        voltha-adapter-simulated-onu    "/app/simulated_onu …"   37 seconds ago       Up 36 seconds                                                                                  compose_adapter_simulated_onu_1_a39b1a9d27d5
15b159bab626        voltha-adapter-simulated-olt    "/app/simulated_olt …"   37 seconds ago       Up 36 seconds                                                                                  compose_adapter_simulated_olt_1_b5407c23b483
401128a1755f        voltha-rw-core                  "/app/rw_core -kv_st…"   About a minute ago   Up About a minute   0.0.0.0:50057->50057/tcp                                                   compose_rw_core_1_36cd5e255edf
ba4eb9384f5b        quay.io/coreos/etcd:v3.2.9      "etcd --name=etcd0 -…"   About a minute ago   Up About a minute   0.0.0.0:2379->2379/tcp, 0.0.0.0:32775->2380/tcp, 0.0.0.0:32774->4001/tcp   compose_etcd_1_368cd0bc1421
55f74277a530        wurstmeister/kafka:2.11-2.0.1   "start-kafka.sh"         2 minutes ago        Up 2 minutes        0.0.0.0:9092->9092/tcp                                                     compose_kafka_1_a8631e438fe2
fb60076d8b3e        wurstmeister/zookeeper:latest   "/bin/sh -c '/usr/sb…"   2 minutes ago        Up 2 minutes        22/tcp, 2888/tcp, 3888/tcp, 0.0.0.0:2181->2181/tcp                         compose_zookeeper_1_7ff68af103cf
```

