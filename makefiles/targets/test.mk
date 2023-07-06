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
#
# SPDX-FileCopyrightText: 2022-2023 Open Networking Foundation (ONF) and the ONF Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------

$(if $(DEBUG),$(warning ENTER))

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
.PHONY: test
test: $(venv-activate-script) $(JOBCONFIG_DIR)
	$(activate) \
	&& pipdeptree \
	&& jenkins-jobs -l DEBUG test --recursive --config-xml -o "$(JOBCONFIG_DIR)" jjb/ ;

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help-verbose += help-test
help-test ::
	@echo
	@echo '[MAKE: test]'
	@echo '  test                Perform testing that a jenkins job pull request will invoke'

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
