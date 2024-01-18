# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2022-2024 Open Networking Foundation (ONF) and the ONF Contributors
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
# SPDX-FileCopyrightText: 2024 Open Networking Foundation (ONF) and the ONF Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------
# https://gerrit.opencord.org/plugins/gitiles/onf-make
# ONF.makefile.version = 1.0
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

# include makefiles/constants.mk
export dot          :=.
export null         :=#
export space        := $(null) $(null)
export quote-single := $(null)'$(null)#'
export quote-double := $(null)"$(null)#"

# [DEBUG] make {target} HIDE=
HIDE           ?= @

env-clean      ?= /usr/bin/env --ignore-environment
xargs-n1       := xargs -0 -t -n1 --no-run-if-empty
xargs-n1-clean := $(env-clean) $(xargs-n1)

## -----------------------------------------------------------------------
## Intent: NOP command for targets whose dependencies do all heavy lifting
## -----------------------------------------------------------------------
## usage: foo bar tans
## <tab>$(nop-command)
## -----------------------------------------------------------------------
nop-cmd        := :

## -----------------------------------------------------------------------
## Default shell:
##   o set -e            enable error checking
##   o set -u            report undefined errors
##   o set -o pipefail   propogate shell pipeline failures.
## -----------------------------------------------------------------------
SHELL ?= /bin/bash
have-shell-bash := $(filter bash,$(subst /,$(space),$(SHELL)))
$(if $(have-shell-bash),$(null),\
  $(eval export SHELL := bash -euo pipefail))

export SHELL ?= bash -euo pipefail

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
