module github.com/opencord/voltha-go

go 1.16

replace (
	github.com/coreos/bbolt v1.3.4 => go.etcd.io/bbolt v1.3.4
	github.com/opencord/voltha-lib-go/v7 => ../voltha-lib-go
	github.com/opencord/voltha-protos/v5 => ../voltha-protos
	go.etcd.io/bbolt v1.3.4 => github.com/coreos/bbolt v1.3.4
	google.golang.org/grpc => google.golang.org/grpc v1.25.1
)

require (
	github.com/Shopify/sarama v1.29.1
	github.com/buraksezer/consistent v0.0.0-20191006190839-693edf70fd72
	github.com/cenkalti/backoff/v3 v3.2.2
	github.com/cespare/xxhash v1.1.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/mock v1.6.0
	github.com/golang/protobuf v1.5.2
	github.com/google/uuid v1.3.0
	github.com/opencord/voltha-lib-go/v7 v7.0.4
	github.com/opencord/voltha-protos/v5 v5.0.0
	github.com/opentracing/opentracing-go v1.2.0
	github.com/phayes/freeport v0.0.0-20180830031419-95f893ade6f2
	github.com/stretchr/testify v1.7.0
	github.com/uber/jaeger-client-go v2.29.1+incompatible
	google.golang.org/grpc v1.41.0
)
