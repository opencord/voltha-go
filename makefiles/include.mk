# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2022-2023 Open Networking Foundation (ONF) and the ONF Contributors
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
#
# SPDX-FileCopyrightText: 2022-2023 Open Networking Foundation (ONF) and the ONF Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------
# https://gerrit.opencord.org/plugins/gitiles/onf-make
# ONF.makefile.version = 1.0
# -----------------------------------------------------------------------

ifndef mk-include--onf-make # single-include guard macor

$(if $(DEBUG),$(warning ENTER))

## -----------------------------------------------------------------------
## Define vars based on relative import (normalize symlinks)
## Usage: include makefiles/onf/include.mk
## -----------------------------------------------------------------------
onf-mk-abs    ?= $(abspath $(lastword $(MAKEFILE_LIST)))
onf-mk-top    := $(subst /include.mk,$(null),$(onf-mk-abs))
ONF_MAKEDIR   := $(onf-mk-top)

include $(ONF_MAKEDIR)/consts.mk
include $(ONF_MAKEDIR)/help/include.mk       # render target help
include $(ONF_MAKEDIR)/utils/include.mk      # dependency-less helper macros
include $(ONF_MAKEDIR)/etc/include.mk        # banner macros

include $(ONF_MAKEDIR)/virtualenv.mk#        # lint-{jjb,python} depends on venv
include $(ONF_MAKEDIR)/lint/include.mk
# include $(ONF_MAKEDIR)/git-submodules.mk
# include $(ONF_MAKEDIR)/gerrit/include.mk

include $(ONF_MAKEDIR)/todo.mk
include $(ONF_MAKEDIR)/help/variables.mk

##---------------------##
##---]  ON_DEMAND  [---##
##---------------------##
$(if $(USE_DOCKER_MK),$(eval $(ONF_MAKEDIR)/docker/include.mk))

##-------------------##
##---]  TARGETS  [---##
##-------------------##
include $(ONF_MAKEDIR)/targets/clean.mk
# include $(ONF_MAKEDIR)/targets/check.mk
include $(ONF_MAKEDIR)/targets/sterile.mk
# include $(ONF_MAKEDIR)/targets/test.mk

$(if $(DEBUG),$(warning LEAVE))

## --------------------------------------------------------------------------
## structure to support pre/post target handling w/o inlining in Makefile (?)
## --------------------------------------------------------------------------
##   include makefiles/include.mk
##     include makefiles/main/enter.mk
##     [... include *.mk ...]
##     include makefiles/main/leave.mk

mk-include--onf-make := true

endif # mk-include--onf-make

# [EOF]
