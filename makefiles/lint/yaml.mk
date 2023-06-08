# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2017-2023 Open Networking Foundation (ONF) and the ONF Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License")
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

$(if $(DEBUG),$(warning ENTER))

##-------------------##
##---]  GLOBALS  [---##
##-------------------##
.PHONY: lint-yaml lint-yaml-all lint-yaml-modified

have-yaml-files := $(if $(strip $(YAML_FILES)),true)
YAML_FILES      ?= $(error YAML_FILES= is required)

## -----------------------------------------------------------------------
## Intent: Use the yaml command to perform syntax checking.
##   o If UNSTABLE=1 syntax check all sources
##   o else only check sources modified by the developer.
## Usage:
##   % make lint UNSTABLE=1
##   % make lint-yaml-all
## -----------------------------------------------------------------------
lint-yaml-mode := $(if $(have-yaml-files),modified,all)
lint-yaml : lint-yaml-$(lint-yaml-mode)

ifndef NO-LINT-YAML
  lint : lint-yaml#     # Enable as a default lint target
endif# NO-LINT-YAML

## -----------------------------------------------------------------------
## Intent: exhaustive yaml syntax checking
## -----------------------------------------------------------------------
lint-yaml-all:
	$(HIDE)$(MAKE) --no-print-directory lint-yaml-install

	find . \( -iname '*.yaml' -o -iname '*.yml' \) -print0 \
	    | $(xargs-n1-clean) yamllint --strict

## -----------------------------------------------------------------------
## Intent: check deps for format and python3 cleanliness
## Note:
##   yaml --py3k option no longer supported
## -----------------------------------------------------------------------
lint-yaml-modified:
	$(HIDE)$(MAKE) --no-print-directory lint-yaml-install
	yamllint -s $(YAML_FILES)

## -----------------------------------------------------------------------
## Intent:
## -----------------------------------------------------------------------
lint-yaml-install:
	@echo
	@echo "** -----------------------------------------------------------------------"
	@echo "** yaml syntax checking"
	@echo "** -----------------------------------------------------------------------"
	yamllint --version
	@echo

## -----------------------------------------------------------------------
## Intent: Display command usage
## -----------------------------------------------------------------------
help::
	@echo '  lint-yaml          Syntax check python using the yaml command'
  ifdef VERBOSE
	@echo '  lint-yaml-all       yaml checking: exhaustive'
	@echo '  lint-yaml-modified  yaml checking: only locally modified'
  endif

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
