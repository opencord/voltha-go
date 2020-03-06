module github.com/opencord/voltha-go

go 1.13

require (
	github.com/gogo/protobuf v1.3.0
	github.com/golang/protobuf v1.3.2
	github.com/google/uuid v1.1.1
	github.com/opencord/voltha-lib-go/v3 v3.0.15
	github.com/opencord/voltha-protos/v3 v3.2.6
	github.com/opentracing/opentracing-go v1.1.0
	github.com/phayes/freeport v0.0.0-20180830031419-95f893ade6f2
	github.com/stretchr/testify v1.4.0
	github.com/uber/jaeger-client-go v2.22.1+incompatible
	google.golang.org/grpc v1.24.0
)

replace github.com/opencord/voltha-lib-go/v3 => /Users/hwchiu/onf/github.com/opencord/voltha-lib-go

replace github.com/opencord/voltha-lib-go/v3/pkg => /Users/hwchiu/onf/github.com/opencord/voltha-lib-go/pkg
