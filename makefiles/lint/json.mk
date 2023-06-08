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
.PHONY: lint-json lint-json-all lint-json-modified

have-json-files := $(if $(strip $(JSON_FILES)),true)
JSON_FILES      ?= $(error JSON_FILES= required)

## -----------------------------------------------------------------------
## Intent: Use the json command to perform syntax checking.
##   o If UNSTABLE=1 syntax check all sources
##   o else only check sources modified by the developer.
## Usage:
##   % make lint UNSTABLE=1
##   % make lint-json-all
## -----------------------------------------------------------------------
ifndef NO-LINT-JSON
  lint-json-mode := $(if $(have-json-files),modified,all)
  lint : lint-json-$(lint-json-mode)
endif# NO-LINT-JSON

## -----------------------------------------------------------------------
## Intent: exhaustive json syntax checking
## -----------------------------------------------------------------------
json-find-args := $(null)
json-find-args += -name '$(venv-name)'
lint-json-all:	
	$(HIDE)$(MAKE) --no-print-directory lint-json-install

	$(activate)\
 && find . \( $(json-find-args) \) -prune -o -name '*.json' -print0 \
	| $(xargs-n1) python -m json.tool > /dev/null ;\

## -----------------------------------------------------------------------
## Intent: check deps for format and python3 cleanliness
## Note:
##   json --py3k option no longer supported
## -----------------------------------------------------------------------
lint-json-modified: $(venv-activate-script)
	$(HIDE)$(MAKE) --no-print-directory lint-json-install

	$(activate)\
 && for jsonfile in $(JSON_FILES); do \
        echo "Validating json file: $$jsonfile" ;\
        python -m json.tool $$jsonfile > /dev/null ;\
    done

## -----------------------------------------------------------------------
## Intent:
## -----------------------------------------------------------------------
.PHONY: lint-json-install
lint-json-install: $(venv-activate-script)
	@echo
	@echo "** -----------------------------------------------------------------------"
	@echo "** json syntax checking"
	@echo "** -----------------------------------------------------------------------"
#	$(activate) && pip install --upgrade json.tool
#       $(activate) && python -m json.tool --version (?-howto-?)
	@echo

## -----------------------------------------------------------------------
## Intent: Display command usage
## -----------------------------------------------------------------------
help::
	@echo '  lint-json          Syntax check python using the json command'
  ifdef VERBOSE
	@echo '  lint-json-all       json checking: exhaustive'
	@echo '  lint-json-modified  json checking: only modified'
  endif

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
