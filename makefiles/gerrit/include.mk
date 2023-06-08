# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2021-2023 Open Networking Foundation (ONF) and the ONF Contributors
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

##--------------------##
##---]  INCLUDES  [---##
##--------------------##
include $(MAKEDIR)/gerrit/help.mk

# -----------------------------------------------------------------------
# -----------------------------------------------------------------------
replication-status:
	ssh gerrit.opencord.org replication list --detail

# -----------------------------------------------------------------------
# NOTE: Gerrit ssh targets assume use of ~/.ssh config files
#       port, login, etc are 
# -----------------------------------------------------------------------
# % ssh -p 29418 <username>@gerrit.opencord.org replication list --detail
# % ssh gerrit.opencord.org replication list --detail
# -----------------------------------------------------------------------
# Host gerrit.opencord.org
#  Hostname gerrit.opencord.org
#  IdentityFile ~/.ssh/gerrit.opencord.org/{ssh_keyfile}
#  IdentitiesOnly yes
#  AddKeysToAgent yes
#  Port 29418
#  User tux@opennetworking.org
# -----------------------------------------------------------------------

# [EOF]
