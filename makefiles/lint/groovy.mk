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
# -----------------------------------------------------------------------

##-------------------##
##---]  GLOBALS  [---##
##-------------------##

groovy-check      := npm-groovy-lint

groovy-check-args := $(null)
# groovy-check-args += --loglevel info
# groovy-check-args += --ignorepattern
# groovy-check-args += --verbose

##-------------------##
##---]  TARGETS  [---##
##-------------------##
ifndef NO-LINT-GROOVY
  lint : lint-groovy
endif

## -----------------------------------------------------------------------
## Intent: Perform a lint check on command line script sources
## -----------------------------------------------------------------------
lint-groovy:
	$(groovy-check) --version
	@echo
	$(HIDE)$(env-clean) find . -iname '*.groovy' -print0 \
  | $(xargs-n1) $(groovy-check) $(groovy-check-args)

## -----------------------------------------------------------------------
## Intent: Display command help
## -----------------------------------------------------------------------
help-summary ::
	@echo '  lint-groovy          Syntax check groovy sources'

# [EOF]
