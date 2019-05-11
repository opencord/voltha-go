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
VERSION                  ?= $(shell cat ./VERSION)

## Docker related
DOCKER_REGISTRY          ?=
DOCKER_REPOSITORY        ?=
DOCKER_BUILD_ARGS        ?=
DOCKER_TAG               ?= ${VERSION}
RWCORE_IMAGENAME         := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-rw-core:${DOCKER_TAG}
ROCORE_IMAGENAME         := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-ro-core:${DOCKER_TAG}
AFROUTER_IMAGENAME       := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}afrouter:${DOCKER_TAG}
AFROUTERTEST_IMAGENAME   := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}afroutertest:${DOCKER_TAG}
AFROUTERD_IMAGENAME      := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}afrouterd:${DOCKER_TAG}
SIMULATEDOLT_IMAGENAME   := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-adapter-simulated-olt:${DOCKER_TAG}
SIMULATEDONU_IMAGENAME   := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-adapter-simulated-onu:${DOCKER_TAG}

## Docker labels. Only set ref and commit date if committed
DOCKER_LABEL_VCS_URL     ?= $(shell git remote get-url $(shell git remote))
DOCKER_LABEL_VCS_REF     ?= $(shell git diff-index --quiet HEAD -- && git rev-parse HEAD || echo "unknown")
DOCKER_LABEL_COMMIT_DATE ?= $(shell git diff-index --quiet HEAD -- && git show -s --format=%cd --date=iso-strict HEAD || echo "unknown" )
DOCKER_LABEL_BUILD_DATE  ?= $(shell date -u "+%Y-%m-%dT%H:%M:%SZ")

.PHONY: rw_core ro_core simulated_olt simulated_onu afrouter arouterd local-protos

# This should to be the first and default target in this Makefile
help:
	@echo "Usage: make [<target>]"
	@echo "where available targets are:"
	@echo
	@echo "build         : Build the docker images."
	@echo "                  - If this is the first time you are building, choose 'make build' option."
	@echo "rw_core       : Build the rw_core docker image"
	@echo "ro_core       : Build the ro_core docker image"
	@echo "afrouter      : Build the afrouter docker image"
	@echo "afrouterTest  : Build the afrouterTest docker image"
	@echo "afrouterd     : Build the afrouterd docker image"
	@echo "simulated_olt : Build the simulated_olt docker image"
	@echo "simulated_onu : Build the simulated_onu docker image"
	@echo "docker-push   : Push the docker images to an external repository"
	@echo "lint-style    : Verify code is properly gofmt-ed"
	@echo "lint-sanity   : Verify that 'go vet' doesn't report any issues"
	@echo "lint-dep      : Verify the integrity of the 'dep' files"
	@echo "lint          : Shorthand for lint-style & lint-sanity"
	@echo "test          : Generate reports for all go tests"
	@echo


## Docker targets

build: docker-build

docker-build: rw_core ro_core simulated_olt simulated_onu afrouter afrouterd

local-protos:
ifdef LOCAL_PROTOS
	mkdir -p vendor/github.com/opencord/voltha-protos/go
	cp -r ${GOPATH}/src/github.com/opencord/voltha-protos/go/* vendor/github.com/opencord/voltha-protos/go
endif

afrouter: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${AFROUTER_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.arouter .

afrouterTest: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${AFROUTERTEST_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.arouterTest .

afrouterd: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${AFROUTERD_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.arouterd .

rw_core: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${RWCORE_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.rw_core .

ro_core: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${ROCORE_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.ro_core .

simulated_olt: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${SIMULATEDOLT_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.simulated_olt .

simulated_onu: local-protos
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${SIMULATEDONU_IMAGENAME} \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.simulated_onu .

docker-push:
	docker push ${AFROUTER_IMAGENAME}
	docker push ${AFROUTERTEST_IMAGENAME}
	docker push ${AFROUTERD_IMAGENAME}
	docker push ${RWCORE_IMAGENAME}
	docker push ${ROCORE_IMAGENAME}
	docker push ${SIMULATEDOLT_IMAGENAME}
	docker push ${SIMULATEDONU_IMAGENAME}


## lint and unit tests

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

lint: lint-style lint-sanity lint-dep

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

# end file
