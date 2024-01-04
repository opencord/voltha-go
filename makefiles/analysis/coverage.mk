# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2016-2024 Open Networking Foundation (ONF) and the ONF Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

# tests-dir      := ./tests/results
# tests-coverage := $(tests-dir)/go-test-coverage
# tests-results  := $(tests-dir)/go-test-results

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
test-coverage-init:
	$(RM) -r tests/results
	@mkdir -p ./tests/results

        # Chicken-n-egg problem: See Dockerfile.rw_core
        #   required by testing
        #   does not exist during build
	@touch ./tests/results/go-test-coverage.out

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
test-coverage: test-coverage-init

	$(call banner-enter,$@)

	$(RM) -r tests/results
	@mkdir -p ./tests/results
	@touch $(tests-coverage).out

	@$(if $(LOCAL_FIX_PERMS),chmod 777 tests/results)

	$(HIDE) $(MAKE) --no-print-directory test-go-coverage
	$(HIDE) $(MAKE) --no-print-directory test-junit
	$(HIDE) $(MAKE) --no-print-directory test-cobertura

	@$(if $(LOCAL_FIX_PERMS),chmod 775 tests/results) # yes this may not run

	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
test-go-coverage:
	$(call banner-enter,$@)
	@$(if $(LOCAL_FIX_PERMS),chmod 777 tests/results)

        # Cannot simply tee output else go exit status lost
	(\
  set -euo pipefail\
    && ${GO} test -mod=vendor -v -coverprofile "./tests/results/go-test-coverage.out" -covermode count ./... 2>&1\
) | tee ./tests/results/go-test-results.out

	@$(if $(LOCAL_FIX_PERMS),chmod 775 tests/results)
	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
test-junit:
	$(call banner-enter,$@)
	@$(if $(LOCAL_FIX_PERMS),chmod 777 tests/results)

	${GO_JUNIT_REPORT} \
	    < ./tests/results/go-test-results.out \
	    > ./tests/results/go-test-results.xml

	@$(if $(LOCAL_FIX_PERMS),chmod 775 tests/results)
	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
test-cobertura:
	$(call banner-enter,$@)
	@$(if $(LOCAL_FIX_PERMS),chmod 777 tests/results)

	${GOCOVER_COBERTURA} \
	    < ./tests/results/go-test-coverage.out \
	    > ./tests/results/go-test-coverage.xml

	@$(if $(LOCAL_FIX_PERMS),chmod 775 tests/results)
	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help ::
	@echo '[TEST: coverage]'
	@echo '  coverage               Generate test coverage reports'
	@echo '  test-go-coverage       Generate a coverage report for vendor/'
	@echo '  test-junit             Digest go coverage, generate junit'
	@echo '  test-cobertura         Digest coverage and junit reports'


## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
clean-coverage :
	$(RM) -r ./tests/results

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
clean :: clean-coverage

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
