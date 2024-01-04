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

# Gather sources to check
# TODO: implement deps, only check modified files
python-check-find := find . -name '*venv*' -prune\
  -o \( -name '*.py' \)\
  -type f -print0

# python-check    := $(env-clean) pylint
python-check    := pylint

# python-check-args += --dry-run

##-------------------##
##---]  TARGETS  [---##
##-------------------##
ifndef NO-LINT-PYTHON
  lint : lint-python
endif

## -----------------------------------------------------------------------
## Intent: Perform a lint check on makefile sources
## -----------------------------------------------------------------------
lint-python:
	$(HIDE)$(env-clean) $(python-check-find) \
	    | $(xargs-n1) $(python-check) $(python-check-args)

## -----------------------------------------------------------------------
## Intent: Display command help
## -----------------------------------------------------------------------
help-summary ::
	@echo '  lint-python         Syntax check python sources (*.py)'

# [EOF]
