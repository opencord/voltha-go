# -*- makefile -*-
# -----------------------------------------------------------------------#
# Copyright 2016-2023 Open Networking Foundation (ONF) and the ONF Contributors
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
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

.DEFAULT_GOAL := help

$(if $(VERBOSE),$(eval export VERBOSE=$(VERBOSE))) # visible to include(s)

##--------------------##
##---]  INCLUDES  [---##
##--------------------##
-include config.mk
include makefiles/include.mk

# Variables
VERSION                    ?= $(shell cat ./VERSION)

# Packages
PACKAGES                   = $(shell go list ./...)

DOCKER_LABEL_VCS_DIRTY     = false
ifneq ($(shell git ls-files --others --modified --exclude-standard 2>/dev/null | wc -l | sed -e 's/ //g'),0)
    DOCKER_LABEL_VCS_DIRTY = true
endif

## Docker related
DOCKER_EXTRA_ARGS          ?=
DOCKER_REGISTRY            ?=
DOCKER_REPOSITORY          ?=
DOCKER_TAG                 ?= ${VERSION}$(shell [[ ${DOCKER_LABEL_VCS_DIRTY} == "true" ]] && echo "-dirty" || true)
DOCKER_TARGET              ?= prod
RWCORE_IMAGENAME           := ${DOCKER_REGISTRY}${DOCKER_REPOSITORY}voltha-rw-core
TYPE                       ?= minimal

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
	--build-arg LOCAL_PROTOS=${LOCAL_PROTOS}

.PHONY: docker-build local-protos local-lib-go help
.DEFAULT_GOAL := help


## Local Development Helpers
local-protos: ## Copies a local version of the voltha-protos dependency into the vendor directory
ifdef LOCAL_PROTOS
	$(RM) -r vendor/github.com/opencord/voltha-protos/v5/go
	mkdir -p vendor/github.com/opencord/voltha-protos/v5/go
	cp -r ${LOCAL_PROTOS}/go/* vendor/github.com/opencord/voltha-protos/v5/go
endif

## Local Development Helpers
local-lib-go: ## Copies a local version of the voltha-lib-go dependency into the vendor directory
ifdef LOCAL_LIB_GO
	$(RM) -r vendor/github.com/opencord/voltha-lib-go/v7/pkg
	mkdir -p vendor/github.com/opencord/voltha-lib-go/v7/pkg
	cp -r ${LOCAL_LIB_GO}/pkg/* vendor/github.com/opencord/voltha-lib-go/v7/pkg/
endif

## -----------------------------------------------------------------------
## Docker targets
## -----------------------------------------------------------------------
build: docker-build ## Alias for 'docker-build'

docker-build-args += --debug
docker-build-args += --log-level 'debug'
docker-build-args := $(null)# comment line for debug mode

docker-build: local-protos local-lib-go ## Build core docker image (set BUILD_PROFILED=true to also build the profiled image)

	$(call banner-enter,$@)
	$(MAKE) --no-print-directory test-coverage-init

	docker $(docker-build-args) build $(DOCKER_BUILD_ARGS) -t ${RWCORE_IMAGENAME}:${DOCKER_TAG} --target ${DOCKER_TARGET} -f docker/Dockerfile.rw_core .
ifdef BUILD_PROFILED
# Force target to dev as profile build must be built with dynamic linking
	docker build $(DOCKER_BUILD_ARGS) --target dev --build-arg EXTRA_GO_BUILD_TAGS="-tags profile" --build-arg CGO_PARAMETER="CGO_ENABLED=1" -t ${RWCORE_IMAGENAME}:${DOCKER_TAG}-profile -f docker/Dockerfile.rw_core .
endif
ifdef BUILD_RACE
# Force target to dev as race detection build must be built with dynamic linking
	docker build $(DOCKER_BUILD_ARGS) --target dev --build-arg GOLANG_IMAGE=golang:1.13.8-buster --build-arg CGO_PARAMETER="CGO_ENABLED=1" --build-arg DEPLOY_IMAGE=debian:buster-slim --build-arg EXTRA_GO_BUILD_TAGS="--race" -t ${RWCORE_IMAGENAME}:${DOCKER_TAG}-rd -f docker/Dockerfile.rw_core .
endif

	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## Intent:
## -----------------------------------------------------------------------
docker-push: ## Push the docker images to an external repository

	$(call banner-enter,$@)

	docker push ${RWCORE_IMAGENAME}:${DOCKER_TAG}
ifdef BUILD_PROFILED
	docker push ${RWCORE_IMAGENAME}:${DOCKER_TAG}-profile
endif
ifdef BUILD_RACE
	docker push ${RWCORE_IMAGENAME}:${DOCKER_TAG}-rd
endif
docker-kind-load: ## Load docker images into a KinD cluster
	@if [ "`kind get clusters | grep voltha-$(TYPE)`" = '' ]; then echo "no voltha-$(TYPE) cluster found" && exit 1; fi
	kind load docker-image ${RWCORE_IMAGENAME}:${DOCKER_TAG} --name=voltha-$(TYPE) --nodes $(shell kubectl get nodes --template='{{range .items}}{{.metadata.name}},{{end}}' | rev | cut -c 2- | rev)

	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## lint and unit tests
## -----------------------------------------------------------------------

# [TODO]
#   o Merge lint-* targets with repo:openolt-adapter/Makefile

lint-dockerfile: ## Perform static analysis on Dockerfile
	@echo "Running Dockerfile lint check..."
	@${HADOLINT} $$(find . -name "Dockerfile.*")
	@echo "Dockerfile lint check OK"

lint-mod: ## Verify the Go dependencies
	@echo "Running dependency check..."
	@${GO} mod verify
	@echo "Dependency check OK. Running vendor check..."
	@git status > /dev/null
	@git diff-index --quiet HEAD -- go.mod go.sum vendor || (echo "ERROR: Staged or modified files must be committed before running this test" && git status -- go.mod go.sum vendor && exit 1)
	@[[ `git ls-files --exclude-standard --others go.mod go.sum vendor` == "" ]] || (echo "ERROR: Untracked files must be cleaned up before running this test" && git status -- go.mod go.sum vendor && exit 1)

	$(MAKE) mod-update

	@git status > /dev/null
	@git diff-index --quiet HEAD -- go.mod go.sum vendor || (echo "ERROR: Modified files detected after running go mod tidy / go mod vendor" && git status -- go.mod go.sum vendor && git checkout -- go.mod go.sum vendor && exit 1)
	@[[ `git ls-files --exclude-standard --others go.mod go.sum vendor` == "" ]] || (echo "ERROR: Untracked files detected after running go mod tidy / go mod vendor" && git status -- go.mod go.sum vendor && git checkout -- go.mod go.sum vendor && exit 1)
	@echo "Vendor check OK."

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
lint: lint-mod lint-dockerfile ## Run all lint targets

include $(MAKEDIR)/analysis/include.mk

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
tests-dir      := ./tests/results
tests-coverage := $(tests-dir)/go-test-coverage
tests-results  := $(tests-dir)/go-test-results

test :: test-coverage ## Run unit tests
test-coverage : local-lib-go

clean :: distclean ## Removes any local filesystem artifacts generated by a build

distclean sterile :: ## Removes any local filesystem artifacts generated by a build or test run
	$(RM) -r ./sca-report

fmt: ## Formats the soure code to go best practice style
#	gofmt -s -w $(PACKAGES)
	@go fmt ${PACKAGES}

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
.PHONY: mod-update
mod-update: mod-tidy mod-vendor

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
.PHONY: mod-tidy
mod-tidy:
	$(call banner-enter,$@)
	${GO} mod tidy
	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## Intent: Refresh vendor/ directory package source
## -----------------------------------------------------------------------
##   Note: This target is destructive, vendor/ directory will be removed.
##   Todo: Update logic to checkout version on demand VS checkin a static
##         copy of vendor/ sources then augment.  Logically removal of
##         files under revision control is strange.
## -----------------------------------------------------------------------
.PHONY: mod-vendor
mod-vendor:
	$(call banner-enter,$@)
	@$(if $(LOCAL_FIX_PERMS),chmod 777 .)
	${GO} mod vendor
	@$(if $(LOCAL_FIX_PERMS),chmod 755 .)
	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help ::
	@echo '[MOD UPDATE]'
	@echo '  mod-update'
	@echo '    LOCAL_FIX_PERMS=1    Hack to fix docker filesystem access problems'
	@echo '  mod-tidy'
	@echo '  mod-vendor'

## ---------------------------------------------------------------------------
# For each makefile target, add ## <description> on the target line and it will be listed by 'make help'
## ---------------------------------------------------------------------------
help :: ## Print help for each Makefile target
	@echo "Usage: make [<target>]"
	@echo "  help        This message"
	@echo "  todo        Future enhancements"
	@echo "  versions    Display version-by-tool used while building"
  ifdef VERBOSE
	@echo
  endif
	@echo
	@grep --no-filename '^[[:alpha:]_-]*:.* ##' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS=":.* ## "}; {printf "%-25s : %s\n", $$1, $$2};'

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
