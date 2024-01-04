# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2017-2024 Open Networking Foundation (ONF) and the ONF Contributors
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
xargs-n1-local := $(subst -t,$(null),$(xargs-n1))#   inhibit cmd display

# Gather sources to check
# TODO: implement deps, only check modified files
make-check-find := find . -name 'vendor' -prune
make-check-find += -o \( -iname makefile -o -name '*.mk' \)
make-check-find += -type f -print0

make-check      := $(MAKE)

make-check-args += --dry-run
make-check-args += --keep-going
make-check-args += --warn-undefined-variables
make-check-args += --no-print-directory

# Quiet internal undef vars
make-check-args += DEBUG=

##-------------------##
##---]  TARGETS  [---##
##-------------------##
ifndef NO-LINT-MAKEFILE
  lint : lint-make
endif

## -----------------------------------------------------------------------
## Intent: Perform a lint check on makefile sources
## -----------------------------------------------------------------------
lint-make-ignore += JSON_FILES=
lint-make-ignore += YAML_FILES=
lint-make:
	@echo
	@echo "** -----------------------------------------------------------------------"
	@echo "** Makefile syntax checking"
	@echo "** -----------------------------------------------------------------------"
	$(HIDE)$(env-clean) $(make-check-find) \
	    | $(xargs-n1-local) $(make-check) $(make-check-args) $(lint-make-ignore)

## -----------------------------------------------------------------------
## Intent: Display command help
## -----------------------------------------------------------------------
help-summary ::
	@echo '  lint-make           Syntax check [Mm]akefile and *.mk'

# [EOF]
