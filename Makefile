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
OFAGENT_IMAGENAME          := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-ofagent
CLI_IMAGENAME              := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-cli

## Docker labels. Only set ref and commit date if committed
DOCKER_LABEL_VCS_URL       ?= $(shell git remote get-url $(shell git remote))
DOCKER_LABEL_VCS_REF       = $(shell git rev-parse HEAD)
DOCKER_LABEL_BUILD_DATE    ?= $(shell date -u "+%Y-%m-%dT%H:%M:%SZ")
DOCKER_LABEL_COMMIT_DATE   = $(shell git show -s --format=%cd --date=iso-strict HEAD)

# Default is GO111MODULE=auto, which will refuse to use go mod if running
# go less than 1.13.0 and this repository is checked out in GOPATH. For now,
# force module usage. This affects commands executed from this Makefile, but
# not the environment inside the Docker build (which does not build from
# inside a GOPATH).
export GO111MODULE=on

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

.PHONY: rw_core ro_core local-protos local-pyvoltha

# This should to be the first and default target in this Makefile
help:
	@echo "Usage: make [<target>]"
	@echo "where available targets are:"
	@echo
	@echo "build                : Build the docker images."
	@echo "                         - If this is the first time you are building, choose 'make build' option."
	@echo "rw_core              : Build the rw_core docker image"
	@echo "ro_core              : Build the ro_core docker image"
	@echo "ofagent              : Build the openflow agent docker image"
	@echo "cli                  : Build the voltha CLI docker image"
	@echo "venv                 : Build local Python virtualenv"
	@echo "clean                : Remove files created by the build and tests"
	@echo "distclean            : Remove venv directory"
	@echo "docker-push          : Push the docker images to an external repository"
	@echo "lint-dockerfile      : Perform static analysis on Dockerfiles"
	@echo "lint-style           : Verify code is properly gofmt-ed"
	@echo "lint-sanity          : Verify that 'go vet' doesn't report any issues"
	@echo "lint-mod             : Verify the integrity of the 'mod' files"
	@echo "lint                 : Shorthand for lint-style & lint-sanity"
	@echo "sca                  : Runs various SCA through golangci-lint tool"
	@echo "test                 : Generate reports for all go tests"
	@echo

## Local Development Helpers
local-protos:
	@mkdir -p python/local_imports
ifdef LOCAL_PROTOS
	mkdir -p vendor/github.com/opencord/voltha-protos/v2/go
	cp -r ${LOCAL_PROTOS}/go/* vendor/github.com/opencord/voltha-protos/v2/go
	rm -rf python/local_imports/voltha-protos
	mkdir -p python/local_imports/voltha-protos/dist
	cp ${LOCAL_PROTOS}/dist/*.tar.gz python/local_imports/voltha-protos/dist/
endif

## Local Development Helpers
local-lib-go:
ifdef LOCAL_LIB_GO
	mkdir -p vendor/github.com/opencord/voltha-lib-go/v2/pkg
	cp -r ${LOCAL_LIB_GO}/pkg/* vendor/github.com/opencord/voltha-lib-go/v2/pkg/
endif

local-pyvoltha:
	@mkdir -p python/local_imports
ifdef LOCAL_PYVOLTHA
	rm -rf python/local_imports/pyvoltha
	mkdir -p python/local_imports/pyvoltha/dist
	cp ${LOCAL_PYVOLTHA}/dist/*.tar.gz python/local_imports/pyvoltha/dist/
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

docker-build: rw_core ro_core ofagent cli

rw_core: local-protos local-lib-go
	docker build $(DOCKER_BUILD_ARGS) -t ${RWCORE_IMAGENAME}:${DOCKER_TAG} -t ${RWCORE_IMAGENAME}:latest -f docker/Dockerfile.rw_core .

ro_core: local-protos local-lib-go
	docker build $(DOCKER_BUILD_ARGS) -t ${ROCORE_IMAGENAME}:${DOCKER_TAG} -t ${ROCORE_IMAGENAME}:latest -f docker/Dockerfile.ro_core .

ofagent: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS_LOCAL) -t ${OFAGENT_IMAGENAME}:${DOCKER_TAG} -t ${OFAGENT_IMAGENAME}:latest -f python/docker/Dockerfile.ofagent python

cli: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS_LOCAL) -t ${CLI_IMAGENAME}:${DOCKER_TAG} -t ${CLI_IMAGENAME}:latest -f python/docker/Dockerfile.cli python

docker-push:
	docker push ${RWCORE_IMAGENAME}:${DOCKER_TAG}
	docker push ${ROCORE_IMAGENAME}:${DOCKER_TAG}
	docker push ${OFAGENT_IMAGENAME}:${DOCKER_TAG}
	docker push ${CLI_IMAGENAME}:${DOCKER_TAG}

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
	@go vet -mod=vendor ./...
	@echo "Sanity check OK"

lint-mod:
	@echo "Running dependency check..."
	@go mod verify
	@echo "Dependency check OK. Running vendor check..."
	@git status > /dev/null
	@git diff-index --quiet HEAD -- go.mod go.sum vendor || (echo "ERROR: Staged or modified files must be committed before running this test" && echo "`git status`" && exit 1)
	@[[ `git ls-files --exclude-standard --others go.mod go.sum vendor` == "" ]] || (echo "ERROR: Untracked files must be cleaned up before running this test" && echo "`git status`" && exit 1)
	go mod tidy
	go mod vendor
	@git status > /dev/null
	@git diff-index --quiet HEAD -- go.mod go.sum vendor || (echo "ERROR: Modified files detected after running go mod tidy / go mod vendor" && echo "`git status`" && exit 1)
	@[[ `git ls-files --exclude-standard --others go.mod go.sum vendor` == "" ]] || (echo "ERROR: Untracked files detected after running go mod tidy / go mod vendor" && echo "`git status`" && exit 1)
	@echo "Vendor check OK."


lint: lint-style lint-sanity lint-mod lint-dockerfile

# Rules to automatically install golangci-lint
GOLANGCI_LINT_TOOL?=$(shell which golangci-lint)
ifeq (,$(GOLANGCI_LINT_TOOL))
GOLANGCI_LINT_TOOL=$(GOPATH)/bin/golangci-lint
golangci_lint_tool_install:
	# Same version as installed by Jenkins ci-management
	# Note that install using `go get` is not recommended as per https://github.com/golangci/golangci-lint
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(GOPATH)/bin v1.17.0
else
golangci_lint_tool_install:
endif

# Rules to automatically install go-junit-report
GO_JUNIT_REPORT?=$(shell which go-junit-report)
ifeq (,$(GO_JUNIT_REPORT))
GO_JUNIT_REPORT=$(GOPATH)/bin/go-junit-report
go_junit_install:
	go get -u github.com/jstemmer/go-junit-report
else
go_junit_install:
endif

# Rules to automatically install gocover-covertura
GOCOVER_COBERTURA?=$(shell which gocover-cobertura)
ifeq (,$(GOCOVER_COBERTURA))
	@GOCOVER_COBERTURA=$(GOPATH)/bin/gocover-cobertura
gocover_cobertura_install:
	go get -u github.com/t-yuki/gocover-cobertura
else
gocover_cobertura_install:
endif

sca: golangci_lint_tool_install
	rm -rf ./sca-report
	@mkdir -p ./sca-report
	$(GOLANGCI_LINT_TOOL) run -E golint -D structcheck --out-format junit-xml ./cli/... ./rw_core/... ./ro_core/... ./tests/... ./common/... ./db/... 2>&1 | tee ./sca-report/sca-report.xml

test: go_junit_install gocover_cobertura_install local-lib-go
	@mkdir -p ./tests/results
	@go test -mod=vendor -v -coverprofile ./tests/results/go-test-coverage.out -covermode count ./... 2>&1 | tee ./tests/results/go-test-results.out ;\
	RETURN=$$? ;\
	$(GO_JUNIT_REPORT) < ./tests/results/go-test-results.out > ./tests/results/go-test-results.xml ;\
	$(GOCOVER_COBERTURA) < ./tests/results/go-test-coverage.out > ./tests/results/go-test-coverage.xml ;\
	exit $$RETURN

clean:
	rm -rf python/local_imports
	find python -name '*.pyc' | xargs rm -f

distclean: clean
	rm -rf ${VENVDIR} ./sca_report

mod-update:
	go mod tidy
	go mod vendor

# end file
