# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2024 Open Networking Foundation Contributors
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
# SPDX-FileCopyrightText: 2024 Open Networking Foundation Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

##-------------------##
##---]  GLOBALS  [---##
##-------------------## 
lf-sbx-root   := $(abspath $(lastword $(MAKEFILE_LIST)))
lf-sbx-root   := $(subst /lf/transition.mk,$(null),$(lf-sbx-root))

legacy-mk   ?= $(lf-sbx-root)/makefiles
onf-mk-dir  ?= $(lf-sbx-root)/lf/onf-make/makefiles

sandbox-root := $(lf-sbx-root)

## -----------------------------------------------------------------------
## Intent: on-demand submodule checkout when make targets are invoked
## -----------------------------------------------------------------------
GIT ?= /usr/bin/env git

git-submodule-deps += $(onf-mk-dir)/consts.mk
git-submodule-deps += $(onf-mk-dir)/etc/features.mk         # $(call banner-enter)
git-submodule-deps += $(onf-mk-dir)/virtualenv/include.mk   # make venv
git-submodule-deps += $(onf-mk-dir)/commands/pre-commit/include.mk
git-submodule-deps += $(onf-mk-dir)/lint/shell/include.mk
git-submodule-deps += $(onf-mk-dir)/lint/yaml/include.mk

# git-submodule-deps    : $(git-submodule-deps)

$(git-submodule-deps) :

	$(call banner-enter,(Checkout git submodules))
	$(GIT) submodule update --init --recursive
	$(call banner-leave,(Checkout git submodules))

##--------------------##
##---]  INCLUDES  [---##
##--------------------##
include $(lf-sbx-root)/lf/config.mk
include $(onf-mk-dir)/consts.mk
# include.mk depends on ONF_MAKEDIR
include $(onf-mk-dir)/etc/features.mk
include $(onf-mk-dir)/virtualenv/include.mk
include $(onf-mk-dir)/commands/pre-commit/include.mk

include $(legacy-mk)/include.mk


# dependency of virtualenv::sterile
clean ::

# [EOF]
