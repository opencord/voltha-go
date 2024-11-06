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

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
sca:
	$(call banner-enter,$@)

	@$(RM) -r ./sca-report
	@mkdir -p ./sca-report
	@echo "Running static code analysis..."
	@${GOLANGCI_LINT} run -vv --out-format junit-xml ./... | tee ./sca-report/sca-report.xml
	@echo ""
	@echo "Static code analysis OK"

	$(call banner-leave,$@)

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
clean-sca :
	@$(RM) -r ./sca-report
	$(RM) ./sca-report/sca-report.xml

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
clean :: clean-sca

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help ::
	@echo '  sca              Runs static code analysis with the golangci-lint tool'

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
