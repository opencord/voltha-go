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

# Include variables.mk after library makefiles have been included

ifdef VERBOSE
  help :: help-variables
else
  help ::
	@echo
	@echo '[VARIABLES] - Conditional makefile behavior'
	@echo '  see also: help-variables'
endif

help-variables:
	@echo
	@echo '[VARIABLES] - Conditional makefile behavior'
	@echo '  NO_PATCHES=           Do not apply patches to the python virtualenv'
	@echo '  NO_OTHER_REPO_DOCS=   No foreign repos, only apply target to local sources.'
	@echo '  VERBOSE=              Display extended help topics'

# [EOF]
