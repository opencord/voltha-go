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
# -----------------------------------------------------------------------
# https://gerrit.opencord.org/plugins/gitiles/onf-make
# ONF.makefile.version = 1.0
# -----------------------------------------------------------------------

##-------------------##
##---]  GLOBALS  [---##
##-------------------##

# Gather sources to check
# TODO: implement deps, only check modified files
shell-check-find := find .
# vendor scripts but they really should be lintable
shell-check-find += -name 'vendor' -prune
shell-check-find += -o \( -name '*.sh' \)
shell-check-find += -type f -print0

# shell-check    := $(env-clean) pylint
shell-check      := shellcheck

shell-check-args += --check-sourced
shell-check-args += --extenal-sources

##-------------------##
##---]  TARGETS  [---##
##-------------------##
ifndef NO-LINT-SHELL
  lint : lint-shell
endif

## -----------------------------------------------------------------------
## Intent: Perform a lint check on command line script sources
## -----------------------------------------------------------------------
lint-shell:
	$(shell-check) -V
	@echo
	$(HIDE)$(env-clean) $(shell-check-find) \
	    | $(xargs-n1) $(shell-check) $(shell-check-args)

## -----------------------------------------------------------------------
## Intent: Display command help
## -----------------------------------------------------------------------
help-summary ::
	@echo '  lint-shell          Syntax check shell sources'

# [SEE ALSO]
# -----------------------------------------------------------------------
#   o https://www.shellcheck.net/wiki/Directive

# [EOF]
