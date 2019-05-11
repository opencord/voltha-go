#
# Copyright 2018 the original author or authors.
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
VERSION                  ?= $(shell cat ../VERSION)
OFAGENT_NAME             ?= voltha-ofagent
CLI_NAME                 ?= voltha-cli
PONSIMOLT_NAME           ?= voltha-adapter-ponsim-olt
PONSIMONU_NAME           ?= voltha-adapter-ponsim-onu

## Docker related
DOCKER_REGISTRY          ?=
DOCKER_REPOSITORY        ?=
DOCKER_BUILD_ARGS        ?=
DOCKER_TAG               ?= ${VERSION}
OFAGENT_IMAGENAME        := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}${OFAGENT_NAME}:${DOCKER_TAG}
CLI_IMAGENAME            := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}${CLI_NAME}:${DOCKER_TAG}
PONSIMOLT_IMAGENAME      := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}${PONSIMOLT_NAME}:${DOCKER_TAG}
PONSIMONU_IMAGENAME      := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}${PONSIMONU_NAME}:${DOCKER_TAG}

## Docker labels. Only set ref and commit date if committed
DOCKER_LABEL_VCS_URL     ?= $(shell git remote get-url $(shell git remote))
DOCKER_LABEL_VCS_REF     ?= $(shell git diff-index --quiet HEAD -- && git rev-parse HEAD || echo "unknown")
DOCKER_LABEL_COMMIT_DATE ?= $(shell git diff-index --quiet HEAD -- && git show -s --format=%cd --date=iso-strict HEAD || echo "unknown" )
DOCKER_LABEL_BUILD_DATE  ?= $(shell date -u "+%Y-%m-%dT%H:%M:%SZ")

.PHONY: venv ofagent cli adapter_ponsim_olt adapter_ponsim_onu local-protos local-pyvoltha

# This should to be the first and default target in this Makefile
help:
	@echo "Usage: make [<target>]"
	@echo "where available targets are:"
	@echo
	@echo "build                : Build all docker images"
	@echo "                         - If this is the first time you are building, choose 'make build' option."
	@echo "help                 : Print this help"
	@echo "ofagent              : Build the openflow agent docker image"
	@echo "cli                  : Build the voltha CLI docker image"
	@echo "adapter_ponsim_olt   : Build the ponsim olt adapter docker image"
	@echo "adapter_ponsim_onu   : Build the ponsim onu adapter docker image"
	@echo "docker-push   : Push the docker images to an external repository"
	@echo "venv                 : Build local Python virtualenv"
	@echo "clean                : Remove files created by the build and tests"
	@echo "distclean            : Remove venv directory"
	@echo


## Docker targets

build: docker-build

docker-build: ofagent cli adapter_ponsim_olt adapter_ponsim_onu

local-protos:
	mkdir -p local_imports
ifdef LOCAL_PROTOS
	mkdir -p local_imports/voltha-protos/dist
	cp ../../voltha-protos/dist/*.tar.gz local_imports/voltha-protos/dist/
endif

local-pyvoltha:
	mkdir -p local_imports
ifdef LOCAL_PYVOLTHA
	mkdir -p local_imports/pyvoltha/dist
	cp ../../pyvoltha/dist/*.tar.gz local_imports/pyvoltha/dist/
endif

ofagent: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${OFAGENT_IMAGENAME} \
    --build-arg LOCAL_PYVOLTHA=$(LOCAL_PYVOLTHA) \
    --build-arg LOCAL_PROTOS=$(LOCAL_PROTOS) \
    --build-arg org_label_schema_name="${OFAGENT_NAME}" \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.ofagent .

cli: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${CLI_IMAGENAME} \
    --build-arg LOCAL_PYVOLTHA=$(LOCAL_PYVOLTHA) \
    --build-arg LOCAL_PROTOS=$(LOCAL_PROTOS) \
    --build-arg org_label_schema_name="${CLI_NAME}" \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.cli .

adapter_ponsim_olt: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${PONSIMOLT_IMAGENAME} \
    --build-arg LOCAL_PYVOLTHA=$(LOCAL_PYVOLTHA) \
    --build-arg LOCAL_PROTOS=$(LOCAL_PROTOS) \
    --build-arg org_label_schema_name="${PONSIMOLT_NAME}" \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.adapter_ponsim_olt .

adapter_ponsim_onu: local-protos local-pyvoltha
	docker build $(DOCKER_BUILD_ARGS) \
    -t ${PONSIMONU_IMAGENAME} \
    --build-arg LOCAL_PYVOLTHA=$(LOCAL_PYVOLTHA) \
    --build-arg LOCAL_PROTOS=$(LOCAL_PROTOS) \
    --build-arg org_label_schema_name="${PONSIMONU_NAME}" \
    --build-arg org_label_schema_version="${VERSION}" \
    --build-arg org_label_schema_vcs_url="${DOCKER_LABEL_VCS_URL}" \
    --build-arg org_label_schema_vcs_ref="${DOCKER_LABEL_VCS_REF}" \
    --build-arg org_label_schema_build_date="${DOCKER_LABEL_BUILD_DATE}" \
    --build-arg org_opencord_vcs_commit_date="${DOCKER_LABEL_COMMIT_DATE}" \
    -f docker/Dockerfile.adapter_ponsim_onu .

docker-push:
	docker push ${OFAGENT_IMAGENAME}
	docker push ${CLI_IMAGENAME}
	docker push ${PONSIMOLT_IMAGENAME}
	docker push ${PONSIMONU_IMAGENAME}


## Python venv dev environment

VENVDIR := venv-volthago

venv: distclean local-protos local-pyvoltha
	virtualenv ${VENVDIR};\
        source ./${VENVDIR}/bin/activate ; set -u ;\
	rm ${VENVDIR}/local/bin ${VENVDIR}/local/lib ${VENVDIR}/local/include ;\
        pip install -r requirements.txt

ifdef LOCAL_PYVOLTHA
	source ./${VENVDIR}/bin/activate ; set -u ;\
	pip install local_imports/pyvoltha/dist/*.tar.gz
endif
ifdef LOCAL_PROTOS
	source ./${VENVDIR}/bin/activate ; set -u ;\
	pip install local_imports/voltha-protos/dist/*.tar.gz
endif

clean:
	rm -rf local_imports
	find . -name '*.pyc' | xargs rm -f

distclean: clean
	rm -rf ${VENVDIR}

# end file
