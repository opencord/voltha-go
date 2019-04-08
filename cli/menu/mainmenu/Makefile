VOLTHA_PROTO_PATH := protos/voltha_protos
VOLTHA_PROTO_FILES := $(wildcard protos/voltha_protos/*.proto)
VOLTHA_PROTO_GO_FILES := $(foreach f,$(VOLTHA_PROTO_FILES),$(subst .proto,.pb.go,$(f)))
VOLTHA_PROTO_DESC_FILES := $(foreach f,$(VOLTHA_PROTO_FILES),$(subst .proto,.desc,$(f)))

voltha-api: $(VOLTHA_PROTO_GO_FILES)

%.pb.go: %.proto
	@protoc -I ${VOLTHA_PROTO_PATH} \
                --go_out=plugins=grpc:${VOLTHA_PROTO_PATH} \
                -I${GOPATH}/src \
                -I${GOPATH}/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
                $<
