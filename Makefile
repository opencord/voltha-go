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

# set default shell
SHELL = bash -e -o pipefail

# Variables
VERSION                    ?= $(shell cat ./VERSION)

DOCKER_LABEL_VCS_DIRTY     = false
ifneq ($(shell git ls-files --others --modified --exclude-standard 2>/dev/null | wc -l | sed -e 's/ //g'),0)
    DOCKER_LABEL_VCS_DIRTY = true
endif
## Docker related
DOCKER_EXTRA_ARGS          ?=
DOCKER_REGISTRY            ?=
DOCKER_REPOSITORY          ?=
DOCKER_TAG                 ?= ${VERSION}$(shell [[ ${DOCKER_LABEL_VCS_DIRTY} == "true" ]] && echo "-dirty" || true)
RWCORE_IMAGENAME           := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-rw-core
ROCORE_IMAGENAME           := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-ro-core
AFROUTER_IMAGENAME         := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-afrouter
AFROUTERTEST_IMAGENAME     := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-afroutertest
AFROUTERD_IMAGENAME        := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-afrouterd
SIMULATEDOLT_IMAGENAME     := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-adapter-simulated-olt
SIMULATEDONU_IMAGENAME     := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-adapter-simulated-onu
OFAGENT_IMAGENAME          := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-ofagent
CLI_IMAGENAME              := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-cli
PONSIMOLT_IMAGENAME        := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-adapter-ponsim-olt
PONSIMONU_IMAGENAME        := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-adapter-ponsim-onu

## Docker labels. Only set ref and commit date if committed
DOCKER_LABEL_VCS_URL       ?= $(shell git remote get-url $(shell git remote))
DOCKER_LABEL_VCS_REF       = $(shell git rev-parse HEAD)
DOCKER_LABEL_BUILD_DATE    ?= $(shell date -u "+%Y-%m-%dT%H:%M:%SZ")
DOCKER_LABEL_COMMIT_DATE   = $(shell git show -s --format=%cd --date=iso-strict HEAD)

DOCKER_BUILD_ARGS ?= \
	${DOCKER_EXTRA_ARGS} \
	--build-arg org_label_schema_version="${VERSION}" \
	--build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
	--build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
	--build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
	--build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
	--build-arg org_opencord_vcs_dirty="${DOCKER_LABEL_VCS_DIRTY}"

DOCKER_BUILD_ARGS_LOCAL ?= ${DOCKER_BUILD_ARGS} \
	--build-arg LOCAL_PYVOLTHA=${LOCAL_PYVOLTHA} \
	--build-arg LOCAL_PROTOS=${LOCAL_PROTOS}

.PHONY: rw_core ro_core simulated_olt simulated_onu afrouter afrouterd local-protos local-pyvoltha

# This should to be the first and default target in this Makefile
help:
	@echo "Usage: make [<target>]"
	@echo "where available targets are:"
	@echo
	@echo "build                : Build the docker images."
	@echo "                         - If this is the first time you are building, choose 'make build' option."
	@echo "rw_core              : Build the rw_core docker image"
	@echo "ro_core              : Build the ro_core docker image"
	@echo "afrouter             : Build the afrouter docker image"
	@echo "afrouterTest         : Build the afrouterTest docker image"
	@echo "afrouterd            : Build the afrouterd docker image"
	@echo "simulated_olt        : Build the simulated_olt docker image"
	@echo "simulated_onu        : Build the simulated_onu docker image"
	@echo "ofagent              : Build the openflow agent docker image"
	@echo "cli                  : Build the voltha CLI docker image"
	@echo "adapter_ponsim_olt   : Build the ponsim olt adapter docker image"
	@echo "adapter_ponsim_onu   : Build the ponsim onu adapter docker image"
	@echo "venv                 : Build local Python virtualenv"
	@echo "clean                : Remove files created by the build and tests"
	@echo "distclean            : Remove venv directory"
	@echo "docker-push          : Push the docker images to an external repository"
	@echo "lint-dockerfile      : Perform static analysis on Dockerfiles"
	@echo "lint-style           : Verify code is properly gofmt-ed"
	@echo "lint-sanity          : Verify that 'go vet' doesn't report any issues"
	@echo "lint-dep             : Verify the integrity of the 'dep' files"
	@echo "lint                 : Shorthand for lint-style & lint-sanity"
	@echo "test                 : Generate reports for all go tests"
	@echo

## Local Development Helpers
local-protos:
	@mkdir -p python/local_imports
ifdef LOCAL_PROTOS
	mkdir -p vendor/github.com/opencord/voltha-protos/go
	cp -r ${GOPATH}/src/github.com/opencord/voltha-protos/go/* vendor/github.com/opencord/voltha-protos/go
	rm -rf python/local_imports/voltha-protos
	mkdir -p python/local_imports/voltha-protos/dist
	cp ../voltha-protos/dist/*.tar.gz python/local_imports/voltha-protos/dist/
endif

local-pyvoltha:
	@mkdir -p python/local_imports
ifdef LOCAL_PYVOLTHA
	rm -rf python/local_imports/pyvoltha
	mkdir -p python/local_imports/pyvoltha/dist
	cp ../pyvoltha/dist/*.tar.gz python/local_imports/pyvoltha/dist/
endif

## Python venv dev environment

VENVDIR := python/venv-volthago

venv: distclean local-protos local-pyvoltha
	virtualenv ${VENVDIR};\
	source ./${VENVDIR}/bin/activate ; set -u ;\
	rm -f ${VENVDIR}/local/bin ${VENVDIR}/local/lib ${VENVDIR}/local/include ;\
	pip install -r python/requirements.txt
ifdef LOCAL_PYVOLTHA
	source ./${VENVDIR}/bin/activate ; set -u ;\
	pip install python/local_imports/pyvoltha/dist/*.tar.gz
endif
ifdef LOCAL_PROTOS
	source ./${VENVDIR}/bin/activate ; set -u ;\
	pip install python/local_imports/voltha-protos/dist/*.tar.gz
endif

## Docker targets

build: docker-build

docker-build: rw_core ro_core simulated_olt simulated_onu afrouter afrouterd ofagent cli adapter_ponsim_olt adapter_ponsim_onu

afrouter: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${AFROUTER_IMAGENAME}:${DOCKER_TAG} -t ${AFROUTER_IMAGENAME}:latest -f docker/Dockerfile.afrouter .

afrouterTest: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${AFROUTERTEST_IMAGENAME}:${DOCKER_TAG} -t ${AFROUTERTEST_IMAGENAME}:latest -f docker/Dockerfile.afrouterTest .

afrouterd: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${AFROUTERD_IMAGENAME}:${DOCKER_TAG} -t ${AFROUTERD_IMAGENAME}:latest -f docker/Dockerfile.afrouterd .

rw_core: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${RWCORE_IMAGENAME}:${DOCKER_TAG} -t ${RWCORE_IMAGENAME}:latest -f docker/Dockerfile.rw_core .

ro_core: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${ROCORE_IMAGENAME}:${DOCKER_TAG} -t ${ROCORE_IMAGENAME}:latest -f docker/Dockerfile.ro_core .

simulated_olt: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${SIMULATEDOLT_IMAGENAME}:${DOCKER_TAG} -t ${SIMULATEDOLT_IMAGENAME}:latest -f docker/Dockerfile.simulated_olt .

simulated_onu: local-protos
	docker build $(DOCKER_BUILD_ARGS) -t ${SIMULATEDONU_IMAGENAME}:${DOCKER_TAG} -t ${SIMULATEDONU_IMAGENAME}:latest -f docker/Dockerfile.simulated_onu .

ofagent: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS_LOCAL) -t ${OFAGENT_IMAGENAME}:${DOCKER_TAG} -t ${OFAGENT_IMAGENAME}:latest -f python/docker/Dockerfile.ofagent python

cli: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS_LOCAL) -t ${CLI_IMAGENAME}:${DOCKER_TAG} -t ${CLI_IMAGENAME}:latest -f python/docker/Dockerfile.cli python

adapter_ponsim_olt: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS_LOCAL) -t ${PONSIMOLT_IMAGENAME}:${DOCKER_TAG} -t ${PONSIMOLT_IMAGENAME}:latest -f python/docker/Dockerfile.adapter_ponsim_olt python

adapter_ponsim_onu: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS_LOCAL) -t ${PONSIMONU_IMAGENAME}:${DOCKER_TAG} -t ${PONSIMONU_IMAGENAME}:latest -f python/docker/Dockerfile.adapter_ponsim_onu python

docker-push:
	docker push ${AFROUTER_IMAGENAME}:${DOCKER_TAG}
	docker push ${AFROUTERD_IMAGENAME}:${DOCKER_TAG}
	docker push ${RWCORE_IMAGENAME}:${DOCKER_TAG}
	docker push ${ROCORE_IMAGENAME}:${DOCKER_TAG}
	docker push ${SIMULATEDOLT_IMAGENAME}:${DOCKER_TAG}
	docker push ${SIMULATEDONU_IMAGENAME}:${DOCKER_TAG}
	docker push ${OFAGENT_IMAGENAME}:${DOCKER_TAG}
	docker push ${CLI_IMAGENAME}:${DOCKER_TAG}
	docker push ${PONSIMOLT_IMAGENAME}:${DOCKER_TAG}
	docker push ${PONSIMONU_IMAGENAME}:${DOCKER_TAG}

## lint and unit tests

PATH:=$(GOPATH)/bin:$(PATH)
HADOLINT=$(shell PATH=$(GOPATH):$(PATH) which hadolint)
lint-dockerfile:
ifeq (,$(shell PATH=$(GOPATH):$(PATH) which hadolint))
	mkdir -p $(GOPATH)/bin
	curl -o $(GOPATH)/bin/hadolint -sNSL https://github.com/hadolint/hadolint/releases/download/v1.17.1/hadolint-$(shell uname -s)-$(shell uname -m)
	chmod 755 $(GOPATH)/bin/hadolint
endif
	@echo "Running Dockerfile lint check ..."
	@hadolint $$(find . -name "Dockerfile.*")
	@echo "Dockerfile lint check OK"

lint-style:
ifeq (,$(shell which gofmt))
	go get -u github.com/golang/go/src/cmd/gofmt
endif
	@echo "Running style check..."
	@gofmt_out="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" ;\
	if [ ! -z "$$gofmt_out" ]; then \
	  echo "$$gofmt_out" ;\
	  echo "Style check failed on one or more files ^, run 'go fmt' to fix." ;\
	  exit 1 ;\
	fi
	@echo "Style check OK"

lint-sanity:
	@echo "Running sanity check..."
	@go vet ./...
	@echo "Sanity check OK"

lint-dep:
	@echo "Running dependency check..."
	@dep check
	@echo "Dependency check OK"

lint: lint-style lint-sanity lint-dep lint-dockerfile

GO_JUNIT_REPORT:=$(shell which go-junit-report)
GOCOVER_COBERTURA:=$(shell which gocover-cobertura)

test:
ifeq (,$(GO_JUNIT_REPORT))
	go get -u github.com/jstemmer/go-junit-report
	@GO_JUNIT_REPORT=$(GOPATH)/bin/go-junit-report
endif
ifeq (,$(GOCOVER_COBERTURA))
	go get -u github.com/t-yuki/gocover-cobertura
	@GOCOVER_COBERTURA=$(GOPATH)/bin/gocover-cobertura
endif
	@mkdir -p ./tests/results
	@go test -v -coverprofile ./tests/results/go-test-coverage.out -covermode count ./... 2>&1 | tee ./tests/results/go-test-results.out ;\
	RETURN=$$? ;\
	$(GO_JUNIT_REPORT) < ./tests/results/go-test-results.out > ./tests/results/go-test-results.xml ;\
	$(GOCOVER_COBERTURA) < ./tests/results/go-test-coverage.out > ./tests/results/go-test-coverage.xml ;\
	exit $$RETURN

clean:
	rm -rf python/local_imports
	find python -name '*.pyc' | xargs rm -f

distclean: clean
	rm -rf ${VENVDIR}

# end file
