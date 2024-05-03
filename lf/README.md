Library makefiles
=================

$(sandbox-root)/include.mk
    Source this makefile to import all makefile logic.

$(sandbox-root)/lf/onf-make/
$(sandbox-root)/lf/onf-make/include.mk
    repo:onf-make contains common library makefile logic.
    tag based checkout (frozen) as a git submodule.

$(sandbox-root)/lf/local/
$(sandbox-root)/lf/local/include.mk
    per-repository targets and logic to customize makefile behavior.

<!--

# -----------------------------------------------------------------------
# Copyright 2024 Open Networking Foundation Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# -----------------------------------------------------------------------
# SPDX-FileCopyrightText: 2024 Open Networking Foundation Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------
# Intent:
# -----------------------------------------------------------------------

-->
