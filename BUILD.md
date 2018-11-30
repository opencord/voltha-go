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
```

### Building the protobufs
```
cd voltha-go
protos/scripts/build_protos.sh protos
```

### Building and running Voltha
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
