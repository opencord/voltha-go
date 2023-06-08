# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2023 Open Networking Foundation (ONF) and the ONF Contributors
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
.PHONY: lint-venv

##-------------------##
##---]  TARGETS  [---##
##-------------------##
ifndef NO-LINT-JJB
  lint : lint-jjb
endif

## -----------------------------------------------------------------------
## Intent: Construct command line for linting jenkins-job-builder config
## -----------------------------------------------------------------------

ifdef DEBUG
  lint-jjb-args += --log_level DEBUG#         # verbosity: high
else
  lint-jjb-args += --log_level INFO#          # verbosity: default
endif
lint-jjb-args += --ignore-cache
lint-jjb-args += test#                        # command action
lint-jjb-args += -o archives/job-configs#     # Generated jobs written here
lint-jjb-args += --recursive
lint-jjb-args += --config-xml#                # JJB v3.0 write to OUTPUT/jobname/config.xml
lint-jjb-args += jjb/#                        # JJB config sources (input)

lint-jjb-deps := $(null)
lint-jjb-deps += $(venv-activate-script) 
lint-jjb-deps += checkout-ci-management-sub-modules
lint-jjb: $(lint-jjb-deps)
	$(activate) && { jenkins-jobs $(lint-jjb-args); }

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help ::
	@echo '  lint-jjb               Validate jjb job generation'
ifdef VERBOSE
	@echo '    DEBUG=1                lint-jjb --log_level=DEBUG'
endif

# [EOF]
