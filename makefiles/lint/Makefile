# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2017-2022 Open Networking Foundation (ONF) and the ONF Contributors
# SPDX-FileCopyrightText: 2022-present Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------

.DEFAULT_GOAL := test

HIDE        ?= @
SHELL       := bash -e -o pipefail

dot         ?= .
TOP         ?= $(dot)
MAKEDIR     ?= $(TOP)/makefiles

env-clean = /usr/bin/env --ignore-environment

jq          = $(env-clean) jq
jq-args     += --exit-status

YAMLLINT      = $(shell which yamllint)
yamllint      := $(env-clean) $(YAMLLINT)
yamllint-args := -c .yamllint

##-------------------##
##---]  TARGETS  [---##
##-------------------##
all:

lint += lint-json
lint += lint-yaml

lint : $(lint)
test : lint

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
lint-yaml yaml-lint:
ifeq ($(null),$(shell which yamllint))
	$(error "Please install yamllint to run linting")
endif
	$(HIDE)$(env-clean) find . -name '*.yaml' -type f -print0 \
	    | xargs -0 -t -n1 $(yamllint) $(yamllint-args)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
lint-json:
	$(HIDE)$(env-clean) find . -name '*.json' -type f -print0 \
	    | xargs -0 -t -n1 $(jq) $(jq-args) $(dot) >/dev/null

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
pre-check:
	@echo "[REQUIRED] Checking for linting tools"
	$(HIDE)which jq
	$(HIDE)which yamllint
	@echo

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
clean:

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
todo:
	@echo
	@echo "USAGE: $(MAKE)"
	@echo "[TODO]"
	@echo "  o Update to support standard makefile target behavior:"
	@echo "    all taget is test not default behavior for automation."
	@echo "  o Change lint target dep from test to check -or smoke"
	@echo "    target test sould be more involved with content validation"
	@echo "  o Refactor lint target(s) with voltha-system-tests/makefiles"
	@echo "  o Linting should be dependency driven,"
	@echo "    only check when sources are modified."
	@echo

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help:
	@echo
	@echo "USAGE: $(MAKE)"
	@echo "  lint        perform syntax checks on source"
	@echo "  test        perform syntax checks on source"
	@echo "  pre-check   Verify tools and deps are available for testing"
	@echo
	@echo "[LINT]"
	@echo "  lint-json   Syntax check .json sources"
	@echo "  lint-yaml   Syntax check .yaml sources"
	@echo
# [EOF]
