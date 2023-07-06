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
## Intent: Revert sandbox into a pristine checkout stage
## -----------------------------------------------------------------------
##   Note: Sterile target behavior differs from clean around handling of
##         persistent content.  For ex removal of a python virtualenv adds
##         extra overhead to development iteration:
##           make clean   - preserve a virtual env
##           make sterile - force reinstallation
## -----------------------------------------------------------------------
.PHONY: sterile
sterile :: clean

## -----------------------------------------------------------------------
## -----------------------------------------------------------------------
help-verbose += help-sterile
help-sterile ::
	@echo
	@echo '[MAKE: sterile]'
	@echo '  sterile             make clean, also remove persistent content (~venv)'

$(if $(DEBUG),$(warning LEAVE))

# [EOF]
