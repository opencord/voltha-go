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
	@echo "build        : Build the docker images.\n\
               If this is the first time you are building, choose \"make build\" option."
	@echo "rw_core      : Build the rw_core docker container"
	@echo "ro_core      : Build the ro_core docker container"
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
	mkdir -p vendor/github.com/opencord/voltha-protos
	cp -Lfr ${GOPATH}/src/github.com/opencord/voltha-protos vendor/github.com/opencord
	docker build $(DOCKER_BUILD_ARGS) -t base:latest -f docker/Dockerfile.base .

afrouter:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}afrouter:${TAG} -f docker/Dockerfile.arouter .

afrouterTest:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}afroutertest:${TAG} -f docker/Dockerfile.arouterTest .

arouterd:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}afrouterd:${TAG} -f docker/Dockerfile.arouterd .

rw_core:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-rw-core:${TAG} -f docker/Dockerfile.rw_core .

ro_core:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-ro-core:${TAG} -f docker/Dockerfile.ro_core .

simulated_olt:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-adapter-simulated-olt:${TAG} -f docker/Dockerfile.simulated_olt .

simulated_onu:
	docker build $(DOCKER_BUILD_ARGS) -t ${REGISTRY}${REPOSITORY}voltha-adapter-simulated-onu:${TAG} -f docker/Dockerfile.simulated_onu .

# end file
