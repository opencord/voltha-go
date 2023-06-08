# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2017-2023 Open Networking Foundation (ONF) and the ONF Contributors
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
# limitations under the License.d
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

VOLTHA_TOOLS_VERSION ?= 2.4.0

include $(MAKEDIR)/docker/versions.mk

# ---------------------------
# Macros: command refactoring
# ---------------------------
docker-iam     ?= --user $$(id -u):$$(id -g)#          # override for local use
docker-run     = docker run --rm $(docker-iam)#        # Docker command stem
docker-run-is  = $(docker-run) $(is-stdin)             # Attach streams when interactive
docker-run-app = $(docker-run-is) -v ${CURDIR}:/app#   # w/filesystem mount

# -----------------------------------------------------------------------
# --interactive: Attach streams when stdout (fh==0) defined
# --tty        : Always create a pseudo-tty else jenkins:docker is silent
# -----------------------------------------------------------------------
is-stdin       = $(shell test -t 0 && { echo '--interactive'; })
is-stdin       += --tty

voltha-protos-v5 ?= /go/src/github.com/opencord/voltha-protos/v5

# Docker volume mounts: container:/app/release <=> localhost:{pwd}/release
vee-golang     = -v gocache-${VOLTHA_TOOLS_VERSION}:/go/pkg
vee-citools    = voltha/voltha-ci-tools:${VOLTHA_TOOLS_VERSION}

# ---------------
# Tool Containers
# ---------------
docker-go-stem = $(docker-run-app) -v gocache:/.cache $(vee-golang) $(vee-citools)-golang

# Usage: GO := $(call get-docker-go,./my.env.temp)
get-docker-go = $(docker-go-stem) go
GO            ?= $(call get-docker-go)

# Usage: GO_SH := $(call get-docker-go-sh,./my.env.temp)
get-docker-go-sh = $(docker-go-stem) $(if $(1),--env-file $(1)) sh -c
GO_SH            ?= $(call get-docker-go-sh,./my.env.temp)

# Usage: PROTOC := $(call get-docker-protoc)
get-docker-protoc = $(docker-run-app) $(vee-citools)-protoc protoc
PROTOC            ?= $(call get-docker-protoc)

# get-docker-protoc-sh = $(strip )
PROTOC_SH = $(docker-run-is)
ifdef voltha-protos-v5
   PROTOC_SH += -v ${CURDIR}:$(voltha-protos-v5)
   PROTOC_SH += --workdir=$(voltha-protos-v5)
endif
PROTOC_SH += $(vee-citools)-protoc sh -c

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
