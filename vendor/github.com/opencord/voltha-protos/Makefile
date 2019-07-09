# Copyright 2019-present Open Networking Foundation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Makefile for voltha-protos
default: test

# set default shell options
SHELL = bash -e -o pipefail

# Function to extract the last path component from go_package line in .proto files
define go_package_path
$(shell grep go_package $(1) | sed -n 's/.*\/\(.*\)";/\1/p')
endef

# Variables
PROTO_FILES := $(sort $(wildcard protos/voltha_protos/*.proto))

PROTO_PYTHON_DEST_DIR := python/voltha_protos
PROTO_PYTHON_PB2 := $(foreach f, $(PROTO_FILES), $(patsubst protos/voltha_protos/%.proto,$(PROTO_PYTHON_DEST_DIR)/%_pb2.py,$(f)))
PROTO_PYTHON_PB2_GRPC := $(foreach f, $(PROTO_FILES), $(patsubst protos/voltha_protos/%.proto,$(PROTO_PYTHON_DEST_DIR)/%_pb2_grpc.py,$(f)))
PROTO_GO_DEST_DIR := go
PROTO_GO_PB:= $(foreach f, $(PROTO_FILES), $(patsubst protos/voltha_protos/%.proto,$(PROTO_GO_DEST_DIR)/$(call go_package_path,$(f))/%.pb.go,$(f)))

# Force pb file to be regenrated every time.  Otherwise the make process assumes generated version is still valid
.PHONY: go/voltha.pb protoc_check

print:
	@echo "Proto files: $(PROTO_FILES)"
	@echo "Python PB2 files: $(PROTO_PYTHON_PB2)"
	@echo "Go PB files: $(PROTO_GO_PB)"

# Generic targets
protos: python-protos go-protos

build: protos python-build go-protos

test: python-test go-test

clean: python-clean go-clean

# Python targets
python-protos: protoc_check $(PROTO_PYTHON_PB2)

venv_protos:
	virtualenv $@;\
	source ./$@/bin/activate ; set -u ;\
	pip install grpcio-tools googleapis-common-protos

$(PROTO_PYTHON_DEST_DIR)/%_pb2.py: protos/voltha_protos/%.proto Makefile venv_protos
	source ./venv_protos/bin/activate ; set -u ;\
	python -m grpc_tools.protoc \
    -I protos \
    --python_out=python \
    --grpc_python_out=python \
    --descriptor_set_out=$(PROTO_PYTHON_DEST_DIR)/$(basename $(notdir $<)).desc \
    --include_imports \
    --include_source_info \
    $<

python-build: setup.py python-protos
	rm -rf dist/
	python ./setup.py sdist

python-test: tox.ini setup.py python-protos
	tox

python-clean:
	find python/ -name '*.pyc' | xargs rm -f
	rm -rf \
    .coverage \
    .tox \
    coverage.xml \
    dist \
    nose2-results.xml \
    python/__pycache__ \
    python/test/__pycache__ \
    python/voltha_protos.egg-info \
    venv_protos \
    $(PROTO_PYTHON_DEST_DIR)/*.desc \
    $(PROTO_PYTHON_PB2) \
    $(PROTO_PYTHON_PB2_GRPC)

# Go targets
go-protos: protoc_check $(PROTO_GO_PB) go/voltha.pb

go_temp:
	mkdir -p go_temp

$(PROTO_GO_PB): $(PROTO_FILES) go_temp
	@echo "Creating $@"
	cd protos && protoc \
    --go_out=MAPS=Mgoogle/protobuf/descriptor.proto=github.com/golang/protobuf/protoc-gen-go/descriptor,plugins=grpc,paths=source_relative:../go_temp \
    -I . voltha_protos/$$(echo $@ | sed -n 's/.*\/\(.*\).pb.go/\1.proto/p' )
	mkdir -p $(dir $@)
	mv go_temp/voltha_protos/$(notdir $@) $@

go/voltha.pb: ${PROTO_FILES}
	@echo "Creating $@"
	protoc -I protos -I protos/google/api \
    --include_imports --include_source_info \
    --descriptor_set_out=$@ \
    ${PROTO_FILES}

go-test: protoc_check
	test/test-go-proto-consistency.sh

go-clean:
	rm -rf go_temp

# Protobuf compiler helper functions
protoc_check:
ifeq ("", "$(shell which protoc)")
	@echo "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
	@echo "It looks like you don't have a version of protocol buffer tools."
	@echo "To install the protocol buffer toolchain on Linux, you can run:"
	@echo "    make install-protoc"
	@echo "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
	exit 1
endif
ifneq ("libprotoc 3.7.0", "$(shell protoc --version)")
	@echo "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
	@echo "You have the wrong version of protoc installed"
	@echo "Please install version 3.7.0"
	@echo "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
	exit 1
endif

install-protoc:
	@echo "Downloading and installing protocol buffer support (Linux amd64 only)"
ifneq ("Linux", "$(shell uname -s)")
	@echo "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
	@echo "Automated installation of protoc not supported on $(shell uname -s)"
	@echo "Please install protoc v3.7.0 from:"
	@echo "  https://github.com/protocolbuffers/protobuf/releases"
	@echo "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
	exit 1
endif
	@echo "Installation will require sudo priviledges"
	@echo "This will take a few minutes."
	@echo "Asking for sudo credentials now so we can install at the end"
	sudo echo "Thanks"; \
    PROTOC_VERSION="3.7.0" ;\
    PROTOC_SHA256SUM="a1b8ed22d6dc53c5b8680a6f1760a305b33ef471bece482e92728f00ba2a2969" ;\
    curl -L -o /tmp/protoc-$${PROTOC_VERSION}-linux-x86_64.zip https://github.com/google/protobuf/releases/download/v$${PROTOC_VERSION}/protoc-$${PROTOC_VERSION}-linux-x86_64.zip ;\
    echo "$${PROTOC_SHA256SUM}  /tmp/protoc-$${PROTOC_VERSION}-linux-x86_64.zip" | sha256sum -c - ;\
    unzip /tmp/protoc-$${PROTOC_VERSION}-linux-x86_64.zip -d /tmp/protoc3 ;\
    sudo mv /tmp/protoc3/bin/* /usr/local/bin/ ;\
    sudo mv /tmp/protoc3/include/* /usr/local/include/ ;\
    sudo chmod -R a+rx /usr/local/bin/* ;\
    sudo chmod -R a+rX /usr/local/include/
