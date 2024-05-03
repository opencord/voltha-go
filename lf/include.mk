# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2023-2024 Open Networking Foundation Contributors
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
# -----------------------------------------------------------------------
# SPDX-FileCopyrightText: 2023-2024 Open Networking Foundation Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

## -----------------------------------------------------------------------
## Infer path to cloned sandbox root
## [TODO] Deprecate TOP=
## -----------------------------------------------------------------------
lf-sbx-root   := $(abspath $(lastword $(MAKEFILE_LIST)))
lf-sbx-root   := $(subst /lf/include.mk,$(null),$(lf-sbx-root))

## -----------------------------------------------------------------------
## Define vars based on relative import (normalize symlinks)
## Usage: include makefiles/onf/include.mk
## -----------------------------------------------------------------------
onf-mk-abs    := $(abspath $(lastword $(MAKEFILE_LIST)))
onf-mk-top    := $(subst /include.mk,$(null),$(onf-mk-abs))
onf-mk-lib    := $(onf-mk-top)/onf-make/makefiles
onf-mk-loc    := $(onf-mk-top)/local

TOP           ?= $(patsubst %/makefiles/include.mk,%,$(onf-mk-abs))

## -----------------------------------------------------------------------
## This variable is a bridge to help transition away from legacy makefiles.
## -----------------------------------------------------------------------
legacy-mk     := $(lf-sbx-root)/makefiles

## ------------------------------------------------------
## Two distinct vars needed to access library or project
## ------------------------------------------------------
ONF_MAKEDIR ?= $(onf-mk-lib)
MAKEDIR     ?= $(onf-mk-loc)

# -----------------------------------------------------------------------
# Load per-repository conditionals
# Load late else alter MAKEFILE_LIST
# NOTE: config.mk can be removed if dynamic feature detection by
#       file extension is added.
# -----------------------------------------------------------------------
include $(wildcard $(lf-sbx-root)/config.mk $(lf-sbx-root)/lf/config.mk)

## -----------------------------------------------------------------------
## Load makefiles in order:
##   1) Library constants and logic loaded first
##   2) Parameterize and augment targets from local (repo specific)
## -----------------------------------------------------------------------
include $(onf-mk-lib)/include.mk
include $(onf-mk-loc)/include.mk

## Define late so caller (?- env --ignore-environment -?)
GIT ?= /usr/bin/env git

## -----------------------------------------------------------------------
## Intent: This target will update dependent git-submodule to the latest
##         version available from the remote repository.  Subsequently
##         a checkin will be needed to make the submodule update permanent.
## -----------------------------------------------------------------------
.PHONY: update-git-submodules
update-git-submodules:
	$(GIT) submodule foreach git pull

## -----------------------------------------------------------------------
## Intent: On-demand cloning of git submodule(s).
## -----------------------------------------------------------------------
## Trigger: include $(onf-mk-lib)/include.mk
##   - When the make command attempts to include a makefile from the
##     repo:onf-make submodule, this target/dependency will initialize
##     and checkout all submodules the current repository depends on.
## -----------------------------------------------------------------------
.PHONY: git-submodules
git-submodules : $(onf-mk-lib)/include.mk

$(onf-mk-lib)/include.mk:

	$(call banner-enter,(Checkout git submodules))

	$(GIT) submodule update --init --recursive

	$(call banner-leave,(Checkout git submodules))

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help-git :
	@printf '  %-33.33s %s\n' 'git-submodules' \
	  'Init and recursive checkout of git submodule(s)'
	@printf '  %-33.33s %s\n' 'update-git-submodules' \
	  'Update git submodule(s) to the latest version'

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
