#
# Copyright 2016 the original author or authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

SHELL=/bin/bash -o pipefail

ifeq ($(TAG),)
TAG := latest
endif

ifeq ($(TARGET_TAG),)
TARGET_TAG := latest
endif

# If no DOCKER_HOST_IP is specified grab a v4 IP address associated with
# the default gateway
ifeq ($(DOCKER_HOST_IP),)
DOCKER_HOST_IP := $(shell ifconfig $$(netstat -rn | grep -E '^(default|0.0.0.0)' | head -1 | awk '{print $$NF}') | grep inet | awk '{print $$2}' | sed -e 's/addr://g')
endif


ifneq ($(http_proxy)$(https_proxy),)
# Include proxies from the environment
DOCKER_PROXY_ARGS = \
       --build-arg http_proxy=$(http_proxy) \
       --build-arg https_proxy=$(https_proxy) \
       --build-arg ftp_proxy=$(ftp_proxy) \
       --build-arg no_proxy=$(no_proxy) \
       --build-arg HTTP_PROXY=$(HTTP_PROXY) \
       --build-arg HTTPS_PROXY=$(HTTPS_PROXY) \
       --build-arg FTP_PROXY=$(FTP_PROXY) \
       --build-arg NO_PROXY=$(NO_PROXY)
endif

DOCKER_BUILD_ARGS = \
	--build-arg TAG=$(TAG) \
	--build-arg REGISTRY=$(REGISTRY) \
	--build-arg REPOSITORY=$(REPOSITORY) \
	$(DOCKER_PROXY_ARGS) $(DOCKER_CACHE_ARG) \
	 --rm --force-rm \
	$(DOCKER_BUILD_EXTRA_ARGS)

DOCKER_IMAGE_LIST = \
	rw_core


.PHONY: $(DIRS) $(DIRS_CLEAN) $(DIRS_FLAKE8) rw_core ro_core protos kafka db tests python simulators k8s afrouter arouterd base

# This should to be the first and default target in this Makefile
help:
	@echo "Usage: make [<target>]"
	@echo "where available targets are:"
	@echo
	@echo "build         : Build the docker images."
	@echo "                  - If this is the first time you are building, choose 'make build' option."
	@echo "rw_core       : Build the rw_core docker container"
	@echo "ro_core       : Build the ro_core docker container"
	@echo "afrouter      : Build the afrouter docker container"
	@echo "afrouterTest  : Build the afrouterTest docker container"
	@echo "afrouterd     : Build the afrouterd docker container"
	@echo "simulated_olt : Build the simulated_olt docker container"
	@echo "simulated_onu : Build the simulated_onu docker container"
	@echo "lint-style    : Verify code is properly gofmt-ed"
	@echo "lint-sanity   : Verify that 'go vet' doesn't report any issues"
	@echo "lint-dep      : Verify the integrity of the `dep` files"
	@echo "lint          : Shorthand for lint-style & lint-sanity"
	@echo "test          : Generate reports for all go tests"
	@echo


# Parallel Build
$(DIRS):
	@echo "    MK $@"
	$(Q)$(MAKE) -C $@

# Parallel Clean
DIRS_CLEAN = $(addsuffix .clean,$(DIRS))
$(DIRS_CLEAN):
	@echo "    CLEAN $(basename $@)"
	$(Q)$(MAKE) -C $(basename $@) clean

build: containers

containers: base rw_core ro_core simulated_olt simulated_onu afrouter arouterd

base:
ifdef LOCAL_PROTOS
	mkdir -p vendor/github.com/opencord/voltha-protos/go
	cp -r ${GOPATH}/src/github.com/opencord/voltha-protos/go/* vendor/github.com/opencord/voltha-protos/go
endif
	docker build $(DOCKER_BUILD_ARGS) -t base:latest -f docker/Dockerfile.base .

afrouter: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}afrouter:${TAG} -f docker/Dockerfile.arouter .

afrouterTest: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}afroutertest:${TAG} -f docker/Dockerfile.arouterTest .

arouterd: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}afrouterd:${TAG} -f docker/Dockerfile.arouterd .

rw_core: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-rw-core:${TAG} -f docker/Dockerfile.rw_core .

ro_core: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-ro-core:${TAG} -f docker/Dockerfile.ro_core .

simulated_olt: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-adapter-simulated-olt:${TAG} -f docker/Dockerfile.simulated_olt .

simulated_onu: base
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-adapter-simulated-onu:${TAG} -f docker/Dockerfile.simulated_onu .

lint-style:
	hash gofmt > /dev/null 2>&1; if [ $$? -ne 0 ]; then \
	  go get -u github.com/golang/go/src/cmd/gofmt; \
	fi

	if [[ "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*') | tee /dev/tty)" ]]; then \
	  echo "Lint failed on one or more files ^; run 'go fmt' to fix."; \
	  exit 1; \
	fi

lint-sanity:
	go vet ./...

lint-dep:
	dep check

lint: lint-style lint-sanity lint-dep

test:
	hash go-junit-report > /dev/null 2>&1; if [ $$? -ne 0 ]; then \
	  go get -u github.com/jstemmer/go-junit-report; \
	fi
	hash gocover-cobertura > /dev/null 2>&1; if [ $$? -ne 0 ]; then \
	  go get -u github.com/t-yuki/gocover-cobertura; \
	fi

	mkdir -p ./tests/results
	go test -v -coverprofile ./tests/results/go-test-coverage.out -covermode count ./... 2>&1 | tee /dev/tty | go-junit-report > ./tests/results/go-test-results.xml; \
	RETURN=$$?; \
	gocover-cobertura < ./tests/results/go-test-coverage.out > ./tests/results/go-test-coverage.xml && exit $$RETURN

# end file
